// Package forkrelease downloads streamers published in
// USBridge-Technologies/Streamers-Forks releases -- today punktfunk-host --
// and verifies them before anything is put in place: the release's
// manifest.json must carry a valid Ed25519 signature (manifest.json.sig)
// from the agent's own update key (update.VerifySignature, the same trust
// anchor the agent's self-update and scripts/verify_release_manifest.go
// use), and the downloaded archive must match the SHA-256 that signed
// manifest lists for it. Plain HTTPS to GitHub alone would trust whatever a
// compromised release edit put there.
//
// Always the repo's latest release (releases/latest/download/...): the
// manifest, its signature and the asset all come from that one release, and
// the version is the signed manifest's.
package forkrelease

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"usbridge_agent/internal/update"
)

const (
	repo          = "USBridge-Technologies/Streamers-Forks"
	manifestApp   = "streamers-forks"
	manifestName  = "manifest.json"
	maxManifest   = 1 << 20
	downloadLimit = 10 * time.Minute
)

// baseURL is the latest release's download prefix; a variable for tests.
var baseURL = "https://github.com/" + repo + "/releases/latest/download/"

var httpClient = &http.Client{Timeout: downloadLimit}

// ProgressFunc reports download progress; total is -1 when unknown.
type ProgressFunc func(downloaded, total int64)

type manifest struct {
	App     string `json:"app"`
	Version string `json:"version"`
	Assets  map[string]struct {
		SHA256 string `json:"sha256"`
	} `json:"assets"`
}

// PunktfunkAssetName is this platform's punktfunk-host release asset, or ""
// where Streamers-Forks publishes none (Punktfunk has Linux and Windows
// hosts only).
func PunktfunkAssetName() string {
	switch runtime.GOOS + "/" + runtime.GOARCH {
	case "windows/amd64":
		return "punktfunk-host-Windows-x64.zip"
	case "linux/amd64":
		return "punktfunk-host-Linux-x86_64.tar.gz"
	}
	return ""
}

// PunktfunkDir is where the downloaded punktfunk-host lives:
// <stateDir>/punktfunk (streamhost looks there, see
// streamhost.SetPunktfunkStageDir). The stateDir, not next to the agent's
// executable: an installed agent can't write there, and an AppImage's mount
// point changes every run.
func PunktfunkDir(stateDir string) string { return filepath.Join(stateDir, "punktfunk") }

func punktfunkBinaryName() string {
	if runtime.GOOS == "windows" {
		return "punktfunk-host.exe"
	}
	return "punktfunk-host"
}

// PunktfunkStaged reports whether a downloaded punktfunk-host is in place.
func PunktfunkStaged(stateDir string) bool {
	_, err := os.Stat(filepath.Join(PunktfunkDir(stateDir), punktfunkBinaryName()))
	return err == nil
}

// PunktfunkStagedVersion is the release version of the staged build, "" if
// none (or staged before versions were recorded).
func PunktfunkStagedVersion(stateDir string) string {
	b, err := os.ReadFile(filepath.Join(PunktfunkDir(stateDir), "VERSION"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// signedManifest is a verified manifest's exact bytes and signature, kept
// beside a Linux KMS tree for the launcher's own root-side check (see
// stageSignedRelease).
type signedManifest struct{ body, sig []byte }

// fetchManifest downloads and verifies the latest release's signed manifest.
func fetchManifest(ctx context.Context) (*manifest, signedManifest, error) {
	body, err := fetch(ctx, baseURL+manifestName)
	if err != nil {
		return nil, signedManifest{}, fmt.Errorf("fetch %s: %w", manifestName, err)
	}
	sig, err := fetch(ctx, baseURL+manifestName+".sig")
	if err != nil {
		return nil, signedManifest{}, fmt.Errorf("fetch %s.sig: %w", manifestName, err)
	}
	if err := update.VerifySignature(body, sig); err != nil {
		return nil, signedManifest{}, fmt.Errorf("%s: %w", manifestName, err)
	}
	var m manifest
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, signedManifest{}, fmt.Errorf("parse %s: %w", manifestName, err)
	}
	if m.App != manifestApp {
		return nil, signedManifest{}, fmt.Errorf("%s is for app %q, expected %q", manifestName, m.App, manifestApp)
	}
	return &m, signedManifest{body, sig}, nil
}

// LatestPunktfunkVersion asks the latest release which punktfunk-host
// version it carries (signature-verified). "" with a nil error when that
// release has no build for this platform.
func LatestPunktfunkVersion(ctx context.Context) (string, error) {
	asset := PunktfunkAssetName()
	if asset == "" {
		return "", fmt.Errorf("no punktfunk-host build for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	m, _, err := fetchManifest(ctx)
	if err != nil {
		return "", err
	}
	if _, ok := m.Assets[asset]; !ok {
		return "", nil
	}
	return m.Version, nil
}

// PreparedPunktfunk is a downloaded and verified build, unpacked next to the
// live directory but not yet in place (Commit does that).
type PreparedPunktfunk struct {
	stateDir string
	nextDir  string
	Version  string
}

// PreparePunktfunk downloads the latest punktfunk-host for this platform,
// verifies the manifest signature and the archive's SHA-256, and unpacks it
// into a sibling directory. Nothing live is touched: the running build (if
// any) keeps running until Commit.
func PreparePunktfunk(ctx context.Context, stateDir string, onProgress ProgressFunc) (*PreparedPunktfunk, error) {
	asset := PunktfunkAssetName()
	if asset == "" {
		return nil, fmt.Errorf("no punktfunk-host build for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	m, _, err := fetchManifest(ctx)
	if err != nil {
		return nil, err
	}
	entry, ok := m.Assets[asset]
	if !ok || len(entry.SHA256) != 64 {
		return nil, fmt.Errorf("release %s has no signed %s", m.Version, asset)
	}
	archive, err := download(ctx, baseURL+asset, entry.SHA256, onProgress)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", asset, err)
	}
	defer os.Remove(archive)

	live := PunktfunkDir(stateDir)
	if err := os.MkdirAll(filepath.Dir(live), 0o755); err != nil {
		return nil, err
	}
	next := live + ".next"
	_ = os.RemoveAll(next)
	if err := os.MkdirAll(next, 0o755); err != nil {
		return nil, err
	}
	if strings.HasSuffix(asset, ".zip") {
		err = extractZip(archive, next)
	} else {
		err = extractTarGz(archive, next)
	}
	if err == nil {
		if _, statErr := os.Stat(filepath.Join(next, punktfunkBinaryName())); statErr != nil {
			err = fmt.Errorf("%s has no %s", asset, punktfunkBinaryName())
		}
	}
	if err == nil {
		err = os.WriteFile(filepath.Join(next, "VERSION"), []byte(m.Version+"\n"), 0o644)
	}
	if err != nil {
		_ = os.RemoveAll(next)
		return nil, err
	}
	return &PreparedPunktfunk{stateDir: stateDir, nextDir: next, Version: m.Version}, nil
}

// Commit puts the prepared build's files (punktfunk-host, the Linux encode
// worker, VERSION) into the live dir, one file at a time.
//
// The live dir is also punktfunk-host's config dir (the agent's
// PUNKTFUNK_CONFIG_DIR): identity keys, pairings, settings. Swapping the whole
// dir, as this used to, deleted all of that with the old build -- and on
// Windows the rename failed with "Access is denied" whenever punktfunk-host
// was running from it. A file in the way is renamed to <name>.old first:
// Windows lets a running .exe be renamed though not overwritten, so this also
// works while the old host is still shutting down. VERSION goes last, so a
// failure part way leaves the old version recorded and the next check retries.
func (p *PreparedPunktfunk) Commit() error {
	live := PunktfunkDir(p.stateDir)
	if err := os.MkdirAll(live, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(p.nextDir)
	if err != nil {
		return err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && e.Name() != "VERSION" {
			names = append(names, e.Name())
		}
	}
	names = append(names, "VERSION")
	for _, name := range names {
		src := filepath.Join(p.nextDir, name)
		if _, err := os.Stat(src); err != nil {
			continue
		}
		dst := filepath.Join(live, name)
		old := dst + ".old"
		_ = os.Remove(old)
		if _, err := os.Stat(dst); err == nil {
			if err := renameRetry(dst, old); err != nil {
				return fmt.Errorf("move the old %s aside: %w", name, err)
			}
		}
		if err := renameRetry(src, dst); err != nil {
			_ = os.Rename(old, dst)
			return fmt.Errorf("put the new %s in place: %w", name, err)
		}
		// Fails while the old .exe still runs; the next Commit clears it.
		_ = os.Remove(old)
	}
	_ = os.RemoveAll(p.nextDir)
	return nil
}

// Discard drops a prepared build that won't be committed.
func (p *PreparedPunktfunk) Discard() { _ = os.RemoveAll(p.nextDir) }

func renameRetry(from, to string) error {
	var err error
	for i := 0; i < 20; i++ {
		if err = os.Rename(from, to); err == nil {
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	return err
}

func fetch(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "usbridge-agent-forkrelease")
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxManifest))
}

func download(ctx context.Context, url, wantSHA256 string, onProgress ProgressFunc) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "usbridge-agent-forkrelease")
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	f, err := os.CreateTemp("", "punktfunk-*.download")
	if err != nil {
		return "", err
	}
	h := sha256.New()
	var done int64
	total := resp.ContentLength
	buf := make([]byte, 256*1024)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				f.Close()
				os.Remove(f.Name())
				return "", werr
			}
			h.Write(buf[:n])
			done += int64(n)
			if onProgress != nil {
				onProgress(done, total)
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			f.Close()
			os.Remove(f.Name())
			return "", rerr
		}
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	if got := hex.EncodeToString(h.Sum(nil)); !strings.EqualFold(got, wantSHA256) {
		os.Remove(f.Name())
		return "", fmt.Errorf("SHA-256 mismatch: got %s, the signed manifest says %s -- refusing it", got, wantSHA256)
	}
	return f.Name(), nil
}

// safeName keeps only plain file names: the release archives are flat, and a
// path component would let an archive write outside dest.
func safeName(name string) (string, bool) {
	name = strings.TrimPrefix(path.Clean("/"+strings.ReplaceAll(name, "\\", "/")), "/")
	base := path.Base(name)
	if name == "" || base == "." || base == ".." {
		return "", false
	}
	return base, true
}

func extractZip(archive, dest string) error {
	zr, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer zr.Close()
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		name, ok := safeName(f.Name)
		if !ok {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		err = writeFile(filepath.Join(dest, name), rc, 0o755)
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func extractTarGz(archive, dest string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		name, ok := safeName(hdr.Name)
		if !ok {
			continue
		}
		if err := writeFile(filepath.Join(dest, name), tr, os.FileMode(hdr.Mode)&0o755|0o600); err != nil {
			return err
		}
	}
}

func writeFile(p string, r io.Reader, mode os.FileMode) error {
	out, err := os.OpenFile(p, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, r); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
