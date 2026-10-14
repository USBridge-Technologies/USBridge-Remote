package forkrelease

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
)

// Sunshine from Streamers-Forks' latest release, the same way as
// punktfunk-host (signed manifest, SHA-256 per asset). Where the release has
// a build for this platform the agent no longer ships Sunshine: it downloads
// it the first time Sunshine is the streamer and updates it from then on.
// An older agent's bundled Sunshine is still used until the first download.

// SunshineAssetName is this platform's Sunshine asset, "" where the release
// has none (Intel Macs, Linux arm64) and the agent still bundles Sunshine.
// Linux KMS capture still goes through a root-owned copy of the tree (see
// streamerlaunch.SunshineDir), made from the downloaded one on the grant.
func SunshineAssetName() string {
	switch runtime.GOOS + "/" + runtime.GOARCH {
	case "windows/amd64":
		return "Sunshine-Windows-x86_64-portable.zip"
	case "linux/amd64":
		return "Sunshine-Linux-x86_64.tar.gz"
	case "darwin/arm64":
		return "Sunshine-macOS-arm64.dmg"
	}
	return ""
}

// SunshineDir is where the staged Sunshine lives: a tree laid out like the
// bundled one (Windows: sunshine.exe, assets/, tools/; Linux: usr/bin/sunshine,
// usr/local/assets, usr/lib; macOS: Sunshine.app). Not <stateDir>/sunshine: that is Sunshine's
// config dir.
func SunshineDir(stateDir string) string { return filepath.Join(stateDir, "sunshine-host") }

// SunshineBinary is the staged Sunshine's executable, "" off the supported
// platforms.
func SunshineBinary(stateDir string) string {
	switch runtime.GOOS {
	case "windows":
		return filepath.Join(SunshineDir(stateDir), "sunshine.exe")
	case "linux":
		return filepath.Join(SunshineDir(stateDir), "usr", "bin", "sunshine")
	case "darwin":
		return filepath.Join(SunshineDir(stateDir), "Sunshine.app", "Contents", "MacOS", "Sunshine")
	}
	return ""
}

// SunshineStaged reports whether a staged Sunshine is in place.
func SunshineStaged(stateDir string) bool {
	bin := SunshineBinary(stateDir)
	if bin == "" {
		return false
	}
	_, err := os.Stat(bin)
	return err == nil
}

// SunshineStagedVersion is the staged Sunshine's release tag, "" if none.
func SunshineStagedVersion(stateDir string) string {
	b, err := os.ReadFile(filepath.Join(SunshineDir(stateDir), "VERSION"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// LatestSunshineVersion asks the latest release which Sunshine it carries
// (signature-verified). "" with a nil error when it has none for this
// platform.
func LatestSunshineVersion(ctx context.Context) (string, error) {
	asset := SunshineAssetName()
	if asset == "" {
		return "", fmt.Errorf("no Sunshine build for %s/%s", runtime.GOOS, runtime.GOARCH)
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

// PreparedSunshine is a downloaded, verified Sunshine not yet put in place.
type PreparedSunshine struct {
	stateDir string
	nextDir  string
	Version  string
}

// PrepareSunshine downloads the latest Sunshine for this platform, verifies
// the manifest signature and the archive's SHA-256, and unpacks it into a
// sibling directory. Nothing live is touched until Commit.
func PrepareSunshine(ctx context.Context, stateDir string, onProgress ProgressFunc) (*PreparedSunshine, error) {
	asset := SunshineAssetName()
	if asset == "" {
		return nil, fmt.Errorf("no Sunshine build for %s/%s", runtime.GOOS, runtime.GOARCH)
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

	live := SunshineDir(stateDir)
	if err := os.MkdirAll(filepath.Dir(live), 0o755); err != nil {
		return nil, err
	}
	next := live + ".next"
	_ = os.RemoveAll(next)
	if err := os.MkdirAll(next, 0o755); err != nil {
		return nil, err
	}
	p := &PreparedSunshine{stateDir: stateDir, nextDir: next, Version: m.Version}
	switch {
	case strings.HasSuffix(asset, ".zip"):
		// The portable zip holds everything under Sunshine/.
		err = extractZipTree(archive, next, "Sunshine/")
	case strings.HasSuffix(asset, ".dmg"):
		err = extractDMGApp(ctx, archive, filepath.Join(next, "Sunshine.app"))
	default:
		err = extractTarGzTree(archive, next)
	}
	if err == nil {
		if _, statErr := os.Stat(p.binary()); statErr != nil {
			err = fmt.Errorf("%s has no Sunshine binary", asset)
		}
	}
	if err == nil {
		err = os.WriteFile(filepath.Join(next, "VERSION"), []byte(m.Version+"\n"), 0o644)
	}
	if err != nil {
		_ = os.RemoveAll(next)
		return nil, err
	}
	return p, nil
}

func (p *PreparedSunshine) binary() string {
	rel, _ := filepath.Rel(SunshineDir(p.stateDir), SunshineBinary(p.stateDir))
	return filepath.Join(p.nextDir, rel)
}

// Commit swaps the prepared tree in whole. Unlike Punktfunk's dir it holds
// nothing but the build (Sunshine's config is <stateDir>/sunshine). Sunshine
// must not be running from it: the caller stops it first.
func (p *PreparedSunshine) Commit() error {
	live := SunshineDir(p.stateDir)
	old := live + ".old"
	_ = os.RemoveAll(old)
	// Sunshine is given its config paths explicitly, but a config\ it
	// wrote next to itself anyway goes along with the update.
	if _, err := os.Stat(filepath.Join(p.nextDir, "config")); os.IsNotExist(err) {
		if _, err := os.Stat(filepath.Join(live, "config")); err == nil {
			_ = copyTree(filepath.Join(live, "config"), filepath.Join(p.nextDir, "config"))
		}
	}
	if _, err := os.Stat(live); err == nil {
		if err := renameRetry(live, old); err != nil {
			return fmt.Errorf("move the old Sunshine aside: %w", err)
		}
	}
	if err := renameRetry(p.nextDir, live); err != nil {
		_ = os.Rename(old, live)
		return fmt.Errorf("put the new Sunshine in place: %w", err)
	}
	_ = os.RemoveAll(old)
	return nil
}

// copyTree copies the regular files under src into dst.
func copyTree(src, dst string) error {
	return filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		defer f.Close()
		return writeFile(target, f, info.Mode().Perm())
	})
}

// Discard drops a prepared build that won't be committed.
func (p *PreparedSunshine) Discard() { _ = os.RemoveAll(p.nextDir) }

// safeRelPath turns an archive member name into a path under the extraction
// root, or false for anything that would land outside it.
func safeRelPath(name string) (string, bool) {
	name = strings.ReplaceAll(name, "\\", "/")
	if strings.HasPrefix(name, "/") || (len(name) > 1 && name[1] == ':') {
		return "", false
	}
	clean := path.Clean(name)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", false
	}
	return filepath.FromSlash(clean), true
}

// extractZipTree unpacks a zip keeping its directories, dropping strip from
// the front of every name (members outside it are skipped).
func extractZipTree(archive, dest, strip string) error {
	zr, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer zr.Close()
	for _, f := range zr.File {
		name := strings.ReplaceAll(f.Name, "\\", "/")
		if strip != "" {
			if !strings.HasPrefix(name, strip) {
				continue
			}
			name = strings.TrimPrefix(name, strip)
		}
		rel, ok := safeRelPath(name)
		if !ok {
			continue
		}
		target := filepath.Join(dest, rel)
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		err = writeFile(target, rc, 0o755)
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

// extractTarGzTree unpacks a tar.gz keeping directories and symlinks (the
// Linux build's usr/lib has versioned .so links). A link must point inside
// the tree.
func extractTarGzTree(archive, dest string) error {
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
		rel, ok := safeRelPath(hdr.Name)
		if !ok {
			continue
		}
		target := filepath.Join(dest, rel)
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			if err := writeFile(target, tr, os.FileMode(hdr.Mode)&0o755|0o600); err != nil {
				return err
			}
		case tar.TypeSymlink:
			if path.IsAbs(hdr.Linkname) {
				continue
			}
			resolved := path.Join(path.Dir(strings.ReplaceAll(hdr.Name, "\\", "/")), hdr.Linkname)
			if _, ok := safeRelPath(resolved); !ok {
				continue
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			_ = os.Remove(target)
			if err := os.Symlink(hdr.Linkname, target); err != nil && runtime.GOOS != "windows" {
				return err
			}
		}
	}
}

// zipTopDir is the one directory every member of the zip sits under, as extractZipTree's strip
// argument ("Vibepollo/"), or "" when the members have no single common top directory.
func zipTopDir(archive string) string {
	zr, err := zip.OpenReader(archive)
	if err != nil {
		return ""
	}
	defer zr.Close()
	top := ""
	for _, f := range zr.File {
		name := strings.ReplaceAll(f.Name, "\\", "/")
		i := strings.Index(name, "/")
		if i <= 0 {
			return ""
		}
		dir := name[:i+1]
		if top == "" {
			top = dir
		} else if top != dir {
			return ""
		}
	}
	return top
}
