// Package update implements USBridge Agent's self-update mechanism for
// Windows, Linux, and macOS (the only platforms the agent ships for).
//
// TODO(rustshine): this only covers the open-source Sunshine-based agent
// build that's actually published via GitHub Releases today (see the
// bundled sunshine/Sunshine.app in scripts/build_macos.sh and
// internal/streamhost/sunshine_*.go). A closed-source "Rustshine"
// streaming-host variant is not yet published on GitHub, so it has no
// release channel for this package to check against — revisit this file
// once that build gets its own distribution/signing pipeline instead of
// assuming it can reuse this one as-is.
//
// The update channel is signature-gated end to end so it stays safe even if
// the transport itself is compromised (a MITM proxy, a poisoned DNS answer,
// a compromised CDN edge in front of GitHub, ...):
//
//  1. CI (scripts/sign_update_manifest.go) builds manifest-agent.json
//     listing every platform artifact's exact SHA-256, then signs those
//     exact bytes with an Ed25519 private key that only exists as a GitHub
//     Actions secret.
//  2. This package fetches manifest-agent.json + manifest-agent.json.sig
//     from the latest stable GitHub Release and verifies the signature
//     against the public key compiled into this binary (pubkey.go) before
//     trusting a single byte of it.
//  3. Only once that signature checks out does the SHA-256 it names for
//     this platform get used to validate the downloaded update artifact.
//
// A network attacker who can intercept the download can at best serve
// stale bytes (which fail their own recorded hash) or an entirely
// unrelated file (same); they cannot make this binary accept anything that
// wasn't produced by whoever holds the private key, because nothing here
// is trusted until step 2 passes.
package update

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
)

// manifestMaxBytes bounds how much of the manifest/signature responses we'll
// read, so a malicious or misbehaving server can't OOM us before signature
// verification even happens.
const manifestMaxBytes = 1 << 20 // 1 MiB

// PlatformAsset is one platform's entry in the signed manifest.
type PlatformAsset struct {
	Asset  string `json:"asset"`
	SHA256 string `json:"sha256"`
}

// Manifest is the signed, versioned update descriptor CI publishes.
type Manifest struct {
	App       string                   `json:"app"`
	Version   string                   `json:"version"`
	Platforms map[string]PlatformAsset `json:"platforms"`
}

// releaseAssetURL points at a fixed, version-independent asset name under
// the repo's /releases/latest — GitHub resolves this to whichever release
// currently has make_latest set, i.e. the newest stable (non-prerelease)
// build. Test/prerelease builds (release-all-test.yml's "test-*" tags)
// never set make_latest, so they're structurally invisible here — the
// update channel only ever sees stable releases.
func releaseAssetURL(name string) string {
	return fmt.Sprintf("https://github.com/%s/%s/releases/latest/download/%s", repoOwner, repoName, name)
}

// fetchManifest downloads and signature-verifies this app's update
// manifest. It returns an error for any transport problem, a bad
// signature, or a manifest whose "app" field doesn't match this binary —
// callers must treat all of those as "no update available right now"
// rather than surfacing them as fatal.
func fetchManifest(ctx context.Context, client *http.Client) (*Manifest, error) {
	manifestName := "manifest-" + appName + ".json"
	body, err := fetchBytes(ctx, client, releaseAssetURL(manifestName))
	if err != nil {
		return nil, fmt.Errorf("fetch manifest: %w", err)
	}
	sigText, err := fetchBytes(ctx, client, releaseAssetURL(manifestName+".sig"))
	if err != nil {
		return nil, fmt.Errorf("fetch manifest signature: %w", err)
	}
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sigText)))
	if err != nil {
		return nil, fmt.Errorf("decode manifest signature: %w", err)
	}

	if !ed25519.Verify(publicKey, body, sig) {
		return nil, fmt.Errorf("manifest signature verification failed — refusing to trust it")
	}

	var m Manifest
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	if m.App != appName {
		return nil, fmt.Errorf("manifest is for app %q, expected %q", m.App, appName)
	}
	return &m, nil
}

func fetchBytes(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "USBridge-"+appName+"-updater")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d for %s", resp.StatusCode, url)
	}
	return io.ReadAll(io.LimitReader(resp.Body, manifestMaxBytes))
}

// isNewerVersion reports whether remote is a strictly newer dotted version
// than local (e.g. "2.1.9" > "2.1.8"). Both are expected to be plain
// numeric dotted versions — that's the only format ./VERSION / -ldflags
// -X main.version ever produce in this repo, so no pre-release/build-
// metadata suffix handling is needed. Missing/non-numeric components
// compare as 0, and a shorter version is padded with zeros.
func isNewerVersion(remote, local string) bool {
	r, l := splitVersion(remote), splitVersion(local)
	n := len(r)
	if len(l) > n {
		n = len(l)
	}
	for i := 0; i < n; i++ {
		var rv, lv int
		if i < len(r) {
			rv = r[i]
		}
		if i < len(l) {
			lv = l[i]
		}
		if rv != lv {
			return rv > lv
		}
	}
	return false
}

func splitVersion(v string) []int {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	parts := strings.Split(v, ".")
	out := make([]int, len(parts))
	for i, p := range parts {
		// A non-numeric component (shouldn't happen for this repo's
		// versioning) just contributes 0 rather than aborting the compare.
		n, _ := strconv.Atoi(strings.TrimSpace(p))
		out[i] = n
	}
	return out
}

// sortedPlatformKeys is only used for logging (deterministic output),
// never for anything security-relevant.
func sortedPlatformKeys(m map[string]PlatformAsset) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
