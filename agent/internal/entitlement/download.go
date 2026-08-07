package entitlement

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// ProgressFunc reports cumulative bytes downloaded (not extracted) so far
// and the total, mirroring internal/update.ProgressFunc's exact contract —
// same nil-is-fine, same "0 total means unknown" convention, same
// UI-thread-hopping obligation on the caller. Kept as a separate type
// (not literally update.ProgressFunc) so this package has no import
// dependency on internal/update at all — different trust root, different
// URL source, nothing to gain from coupling the two.
type ProgressFunc func(downloaded, total int64)

const progressInterval = 100 * time.Millisecond
const downloadTimeout = 5 * time.Minute

// binaryName is bin/gamestream-server's build output name (its Cargo.toml
// package name), mirrored from streamhost.rustshineBackend's own
// unexported binaryName() -- duplicated rather than imported so this
// package stays self-contained (see its doc comment).
func binaryName() string {
	if runtime.GOOS == "windows" {
		return "gamestream-server.exe"
	}
	return "gamestream-server"
}

// StagePath is exactly what streamhost.rustshineBackend.BinaryPath()
// resolves to (see rustshine_backend.go) -- staging here means zero
// changes needed on that side once a download completes.
//
// Deliberately under stateDir (e.g. ~/Library/Application Support/
// usbridge-agent on macOS), never under exeDir/the app's own install
// directory: on macOS exeDir is inside the signed, notarized .app bundle
// (Contents/MacOS), and writing a new file into it post-install breaks the
// bundle's code signature seal (`codesign --verify` reports "a sealed
// resource is missing or invalid" the moment a staged binary lands there --
// confirmed live). A bundle with a broken seal is treated as tampered by
// macOS's TCC subsystem, which silently denies every permission check
// (Screen Recording, etc.) for it and everything it launches, no matter how
// many times the user grants access in System Settings -- this was the
// actual root cause behind RustShine's ScreenCaptureKit captures always
// failing, not a missing permission. stateDir is never part of any signed
// bundle on any platform, so this can't happen there. Same directory
// TokenFilePath already uses, for the same reason.
func StagePath(stateDir string) string {
	return filepath.Join(stateDir, "rustshine", binaryName())
}

// StageRustShine resolves, downloads, verifies, and extracts the RustShine
// build for this platform, atomically replacing whatever's already staged
// at StagePath(stateDir). entitlementToken must already have passed Verify
// locally; ResolveDownload also independently re-checks it against the
// backend, which is the actual authority (a token can be locally
// well-formed and unexpired but the backend may still refuse to serve a
// download for other reasons).
func StageRustShine(ctx context.Context, stateDir, entitlementToken string, onProgress ProgressFunc) error {
	platform := Platform()
	if platform == "" {
		return fmt.Errorf("entitlement: no RustShine build for this platform (%s/%s)", runtime.GOOS, runtime.GOARCH)
	}

	info, err := ResolveDownload(ctx, entitlementToken, platform)
	if err != nil {
		return fmt.Errorf("entitlement: resolve download: %w", err)
	}

	dlCtx, cancel := context.WithTimeout(ctx, downloadTimeout)
	defer cancel()
	archivePath, err := downloadArchive(dlCtx, info.URL, info.SHA256, onProgress)
	if err != nil {
		return err
	}
	defer os.Remove(archivePath)

	dest := StagePath(stateDir)
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return fmt.Errorf("entitlement: create rustshine dir: %w", err)
	}
	if runtime.GOOS == "windows" {
		return extractFromZip(archivePath, binaryName(), dest)
	}
	return extractFromTarGz(archivePath, binaryName(), dest)
}

func downloadArchive(ctx context.Context, url, wantSHA256Hex string, onProgress ProgressFunc) (path string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "usbridge-agent-entitlement")

	resp, err := httpClient().Do(req)
	if err != nil {
		return "", fmt.Errorf("entitlement: download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("entitlement: download: unexpected status %d", resp.StatusCode)
	}

	f, err := os.CreateTemp("", "usbridge-rustshine-*")
	if err != nil {
		return "", err
	}
	tmpPath := f.Name()
	defer func() {
		f.Close()
		if err != nil {
			os.Remove(tmpPath)
		}
	}()

	h := sha256.New()
	dst := io.MultiWriter(f, h)
	if onProgress != nil {
		pw := &progressWriter{total: resp.ContentLength, onProgress: onProgress}
		onProgress(0, resp.ContentLength)
		dst = io.MultiWriter(dst, pw)
	}
	if _, err = io.Copy(dst, resp.Body); err != nil {
		return "", fmt.Errorf("entitlement: download: %w", err)
	}
	if err = f.Sync(); err != nil {
		return "", err
	}
	if onProgress != nil && resp.ContentLength > 0 {
		onProgress(resp.ContentLength, resp.ContentLength)
	}

	got := hex.EncodeToString(h.Sum(nil))
	if got != wantSHA256Hex {
		err = fmt.Errorf("entitlement: downloaded archive hash mismatch (got %s, backend says %s) — refusing to stage, this download may have been tampered with", got, wantSHA256Hex)
		return "", err
	}
	return tmpPath, nil
}

type progressWriter struct {
	total      int64
	written    int64
	lastReport time.Time
	onProgress ProgressFunc
}

func (w *progressWriter) Write(p []byte) (int, error) {
	w.written += int64(len(p))
	if now := time.Now(); now.Sub(w.lastReport) >= progressInterval {
		w.lastReport = now
		w.onProgress(w.written, w.total)
	}
	return len(p), nil
}

// extractFromTarGz pulls the single entry named wantBase (matched by
// filepath.Base, ignoring whatever directory prefix the CI packaging step
// wrapped it in -- see rust-shine's release-gamestream-server.yml, which
// always wraps the binary in a same-named directory before archiving) out
// of a .tar.gz and atomically installs it at dest.
func extractFromTarGz(archivePath, wantBase, dest string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("entitlement: open gzip: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return fmt.Errorf("entitlement: archive has no entry named %q", wantBase)
		}
		if err != nil {
			return fmt.Errorf("entitlement: read tar: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg || filepath.Base(hdr.Name) != wantBase {
			continue
		}
		return writeAtomic(dest, tr, 0o755)
	}
}

func extractFromZip(archivePath, wantBase, dest string) error {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("entitlement: open zip: %w", err)
	}
	defer zr.Close()

	for _, zf := range zr.File {
		if zf.FileInfo().IsDir() || filepath.Base(zf.Name) != wantBase {
			continue
		}
		rc, err := zf.Open()
		if err != nil {
			return fmt.Errorf("entitlement: open zip entry: %w", err)
		}
		defer rc.Close()
		return writeAtomic(dest, rc, 0o755)
	}
	return fmt.Errorf("entitlement: archive has no entry named %q", wantBase)
}

// writeAtomic streams src into a temp file next to dest, then renames it
// into place -- so a crash or concurrent read mid-write never leaves a
// truncated binary at dest (same-directory rename is atomic on every OS
// this agent ships for, as long as the temp file and dest share a
// filesystem, which they do here).
func writeAtomic(dest string, src io.Reader, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(dest), ".rustshine-download-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	ok := false
	defer func() {
		tmp.Close()
		if !ok {
			os.Remove(tmpPath)
		}
	}()

	if _, err := io.Copy(tmp, src); err != nil {
		return fmt.Errorf("entitlement: write staged binary: %w", err)
	}
	if err := tmp.Chmod(perm); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, dest); err != nil {
		return fmt.Errorf("entitlement: install staged binary: %w", err)
	}
	ok = true
	return nil
}
