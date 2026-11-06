package forkrelease

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Vibepollo (Nonary's Sunshine fork, with the USBridge changes from Streamers-Forks'
// vibepollo/ directory: the USB broker bridge, web_bind_address, the client's cursor
// setting) from Streamers-Forks' latest release, exactly like Sunshine: signed manifest,
// SHA-256 per asset, unpacked beside the live dir and swapped in whole. Windows x64 (the
// portable zip) and Linux x86_64 (the install tree built on Ubuntu 22.04 with its libraries
// bundled, for any distro -- not only the Arch packages upstream publishes). On Linux KMS
// capture goes through the launcher (streamerlaunch --run-vibepollo, a root-owned copy of
// this tree made on the grant); KWin capture needs no grant.

// VibepolloAssetName is this platform's Vibepollo asset, "" where the release has none.
func VibepolloAssetName() string {
	switch runtime.GOOS + "/" + runtime.GOARCH {
	case "windows/amd64":
		return "Vibepollo-Windows-x86_64-portable.zip"
	case "linux/amd64":
		return "Vibepollo-Linux-x86_64.tar.gz"
	}
	return ""
}

// VibepolloDir is where the staged Vibepollo lives (Windows: sunshine.exe, assets/, tools/;
// Linux: usr/bin/vibepollo, usr/share/vibepollo, usr/lib). Not
// <stateDir>/vibepollo: that name stays free for per-backend data.
func VibepolloDir(stateDir string) string { return filepath.Join(stateDir, "vibepollo-host") }

// VibepolloBinary is the staged Vibepollo's executable, "" off the supported platforms.
// (Vibepollo's executable keeps the name sunshine.exe on Windows.)
func VibepolloBinary(stateDir string) string {
	switch runtime.GOOS {
	case "windows":
		return filepath.Join(VibepolloDir(stateDir), "sunshine.exe")
	case "linux":
		return filepath.Join(VibepolloDir(stateDir), vibepolloLinuxBin)
	}
	return ""
}

// vibepolloLinuxBin is the executable inside the Linux tarball's install tree.
const vibepolloLinuxBin = "usr/bin/vibepollo"

// VibepolloStaged reports whether a staged Vibepollo is in place.
func VibepolloStaged(stateDir string) bool {
	bin := VibepolloBinary(stateDir)
	if bin == "" {
		return false
	}
	_, err := os.Stat(bin)
	return err == nil
}

// VibepolloStagedVersion is the staged Vibepollo's release tag, "" if none.
func VibepolloStagedVersion(stateDir string) string {
	b, err := os.ReadFile(filepath.Join(VibepolloDir(stateDir), "VERSION"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// LatestVibepolloVersion asks the latest release which Vibepollo it carries
// (signature-verified). "" with a nil error when it has none for this platform.
func LatestVibepolloVersion(ctx context.Context) (string, error) {
	asset := VibepolloAssetName()
	if asset == "" {
		return "", fmt.Errorf("no Vibepollo build for %s/%s", runtime.GOOS, runtime.GOARCH)
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

// PreparedVibepollo is a downloaded, verified Vibepollo not yet put in place.
type PreparedVibepollo struct {
	stateDir   string
	nextDir    string
	releaseDir string // the signed release kept for the launcher, "" off Linux
	Version    string
}

// PrepareVibepollo downloads the latest Vibepollo for this platform, verifies the manifest
// signature and the archive's SHA-256, and unpacks it into a sibling directory. Nothing
// live is touched until Commit.
func PrepareVibepollo(ctx context.Context, stateDir string, onProgress ProgressFunc) (*PreparedVibepollo, error) {
	asset := VibepolloAssetName()
	if asset == "" {
		return nil, fmt.Errorf("no Vibepollo build for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	m, sm, err := fetchManifest(ctx)
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

	live := VibepolloDir(stateDir)
	if err := os.MkdirAll(filepath.Dir(live), 0o755); err != nil {
		return nil, err
	}
	next := live + ".next"
	_ = os.RemoveAll(next)
	if err := os.MkdirAll(next, 0o755); err != nil {
		return nil, err
	}
	p := &PreparedVibepollo{stateDir: stateDir, nextDir: next, Version: m.Version}
	exe := "sunshine.exe"
	if runtime.GOOS == "linux" {
		// The install tree at the tarball's root, symlinks (usr/lib's .so links) kept.
		exe = vibepolloLinuxBin
		err = extractTarGzTree(archive, next)
	} else {
		// The portable zip holds everything under one top directory (cpack's package name).
		err = extractZipTree(archive, next, zipTopDir(archive))
	}
	if err == nil {
		if _, statErr := os.Stat(filepath.Join(next, exe)); statErr != nil {
			err = fmt.Errorf("%s has no %s", asset, exe)
		}
	}
	if err == nil {
		err = os.WriteFile(filepath.Join(next, "VERSION"), []byte(m.Version+"\n"), 0o644)
	}
	if err == nil && keepsSignedRelease() {
		p.releaseDir = ReleaseDir(live) + ".next"
		err = stageSignedRelease(p.releaseDir, sm, asset, archive)
	}
	if err != nil {
		_ = os.RemoveAll(next)
		discardSignedRelease(p.releaseDir)
		return nil, err
	}
	return p, nil
}

// Commit swaps the prepared tree in whole. Vibepollo must not be running from it: the
// caller stops it first.
func (p *PreparedVibepollo) Commit() error {
	live := VibepolloDir(p.stateDir)
	old := live + ".old"
	_ = os.RemoveAll(old)
	// A config\ Vibepollo wrote next to itself (its own state: vibeshine_state.json) goes
	// along with the update.
	if _, err := os.Stat(filepath.Join(p.nextDir, "config")); os.IsNotExist(err) {
		if _, err := os.Stat(filepath.Join(live, "config")); err == nil {
			_ = copyTree(filepath.Join(live, "config"), filepath.Join(p.nextDir, "config"))
		}
	}
	if _, err := os.Stat(live); err == nil {
		if err := renameRetry(live, old); err != nil {
			return fmt.Errorf("move the old Vibepollo aside: %w", err)
		}
	}
	if err := renameRetry(p.nextDir, live); err != nil {
		_ = os.Rename(old, live)
		return fmt.Errorf("put the new Vibepollo in place: %w", err)
	}
	_ = os.RemoveAll(old)
	commitSignedRelease(p.releaseDir, live)
	return nil
}

// Discard drops a prepared build that won't be committed.
func (p *PreparedVibepollo) Discard() {
	_ = os.RemoveAll(p.nextDir)
	discardSignedRelease(p.releaseDir)
}
