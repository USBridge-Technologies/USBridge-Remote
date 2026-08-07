// Command sign_update_manifest builds and Ed25519-signs the auto-update
// manifest CI publishes alongside a release's platform build artifacts.
//
// It has no dependencies beyond the Go standard library on purpose — it's
// invoked via `go run scripts/sign_update_manifest.go ...` straight from a
// GitHub Actions step, no `go build`/module vendoring required.
//
// The manifest schema here must stay in lockstep with the Manifest struct
// in client/internal/update/manifest.go and agent/internal/update/manifest.go
// — those are what actually parses and (more importantly) what verifies
// the signature this program produces.
//
// Usage:
//
//	UPDATE_SIGNING_KEY=<base64 ed25519 seed> \
//	  go run scripts/sign_update_manifest.go \
//	    -app client -version 2.1.9 -dir out \
//	    -manifest-out out/manifest-client.json \
//	    -sig-out out/manifest-client.json.sig
//
// -dir is scanned for every file matching this app's asset naming
// convention (USBridgeClient-* / USBridgeAgent-*); each match's exact
// SHA-256 is recorded. iOS assets (*.ipa) are always excluded — iOS has no
// self-update path, Apple's App Store owns updates there.
package main

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type platformAsset struct {
	Asset  string `json:"asset"`
	SHA256 string `json:"sha256"`
}

type manifest struct {
	App       string                   `json:"app"`
	Version   string                   `json:"version"`
	Platforms map[string]platformAsset `json:"platforms"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "sign_update_manifest:", err)
		os.Exit(1)
	}
}

func run() error {
	app := flag.String("app", "", `app to build the manifest for: "client" or "agent"`)
	version := flag.String("version", "", "version string to publish (e.g. 2.1.9)")
	dir := flag.String("dir", "", "directory containing the already-built, version-stripped release assets")
	manifestOut := flag.String("manifest-out", "", "path to write the JSON manifest to")
	sigOut := flag.String("sig-out", "", "path to write the base64 Ed25519 signature to")
	keyEnv := flag.String("key-env", "UPDATE_SIGNING_KEY", "environment variable holding the base64 Ed25519 private key seed")
	flag.Parse()

	if *app != "client" && *app != "agent" {
		return fmt.Errorf(`-app must be "client" or "agent", got %q`, *app)
	}
	if *version == "" || *dir == "" || *manifestOut == "" || *sigOut == "" {
		return fmt.Errorf("-version, -dir, -manifest-out, and -sig-out are all required")
	}

	seedB64 := os.Getenv(*keyEnv)
	if seedB64 == "" {
		return fmt.Errorf("environment variable %s is empty — is the GitHub secret wired into this step's env:?", *keyEnv)
	}
	seed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(seedB64))
	if err != nil || len(seed) != ed25519.SeedSize {
		return fmt.Errorf("%s does not decode to a %d-byte Ed25519 seed", *keyEnv, ed25519.SeedSize)
	}
	priv := ed25519.NewKeyFromSeed(seed)

	platforms, err := collectPlatforms(*dir, *app)
	if err != nil {
		return err
	}
	if len(platforms) == 0 {
		return fmt.Errorf("no %s-* release assets found under %s — refusing to publish an empty manifest", assetPrefix(*app), *dir)
	}

	m := manifest{App: *app, Version: *version, Platforms: platforms}
	body, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	body = append(body, '\n')

	if err := os.WriteFile(*manifestOut, body, 0o644); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}

	// Sign the *exact* bytes just written — the verifier checks the raw
	// HTTP response body against this signature, so signing anything else
	// (e.g. re-marshaling later) would silently produce a manifest that
	// fails verification for every installed copy.
	sig := ed25519.Sign(priv, body)
	sigB64 := base64.StdEncoding.EncodeToString(sig)
	if err := os.WriteFile(*sigOut, []byte(sigB64+"\n"), 0o644); err != nil {
		return fmt.Errorf("write signature: %w", err)
	}

	keys := make([]string, 0, len(platforms))
	for k := range platforms {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	fmt.Printf("signed %s manifest v%s: %v -> %s (+ %s)\n", *app, *version, keys, *manifestOut, *sigOut)
	return nil
}

func assetPrefix(app string) string {
	if app == "client" {
		return "USBridgeClient"
	}
	return "USBridgeAgent"
}

// collectPlatforms scans dir for this app's release assets and derives a
// "<goos>-<goarch>" platform key from each filename, matching the naming
// convention client/agent's build_*.sh scripts and
// .github/workflows/*.yml's "Flatten artifacts" steps produce (version
// suffix already stripped, e.g. "USBridgeClient-Windows-x86_64.zip").
func collectPlatforms(dir, app string) (map[string]platformAsset, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", dir, err)
	}

	prefix := assetPrefix(app)
	platforms := make(map[string]platformAsset)

	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), prefix+"-") {
			continue
		}
		// iOS has no self-update path (App Store only) — never include it
		// even if an .ipa somehow ends up alongside the other client assets.
		if strings.Contains(e.Name(), "-iOS-") || strings.HasSuffix(e.Name(), ".ipa") {
			continue
		}
		// Same story for the Android "market" flavor (Play Store/F-Droid
		// build, client/scripts/build_android_gradle.sh's default): it has
		// no self-update code compiled in at all (see
		// client/internal/update/update_disabled.go), and would otherwise
		// collide with the "direct" flavor's
		// USBridgeClient-Android-arm64-selfupdate-<version>.apk under the
		// same "android-arm64" platform key below. .aab is never a
		// self-update asset either way — Android app bundles aren't
		// sideloadable.
		if strings.Contains(e.Name(), "-market-") || strings.HasSuffix(e.Name(), ".aab") {
			continue
		}

		key, err := platformKeyFor(e.Name())
		if err != nil {
			return nil, fmt.Errorf("%s: %w", e.Name(), err)
		}
		if _, dup := platforms[key]; dup {
			return nil, fmt.Errorf("two assets both map to platform %q (one is %s) — naming convention ambiguity", key, e.Name())
		}

		sum, err := sha256File(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("hash %s: %w", e.Name(), err)
		}

		platforms[key] = platformAsset{Asset: e.Name(), SHA256: sum}
	}

	return platforms, nil
}

func platformKeyFor(name string) (string, error) {
	var goos string
	switch {
	case strings.Contains(name, "-Windows-"):
		goos = "windows"
	case strings.Contains(name, "-Linux-"):
		goos = "linux"
	case strings.Contains(name, "-macOS-"):
		goos = "darwin"
	case strings.Contains(name, "-Android-"):
		goos = "android"
	default:
		return "", fmt.Errorf("filename doesn't identify a known OS (expected -Windows-/-Linux-/-macOS-/-Android-)")
	}

	var goarch string
	switch {
	case strings.Contains(name, "arm64"):
		goarch = "arm64"
	case strings.Contains(name, "x86_64"), strings.Contains(name, "amd64"):
		goarch = "amd64"
	default:
		return "", fmt.Errorf("filename doesn't identify a known architecture (expected arm64/x86_64)")
	}

	return goos + "-" + goarch, nil
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
