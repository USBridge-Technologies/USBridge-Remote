// Package update implements USBridge's self-update mechanism for every
// platform except iOS (Apple's App Store is the only update path there).
//
// The update channel is signature-gated end to end so it stays safe even if
// the transport itself is compromised (a MITM proxy, a poisoned DNS answer,
// a compromised CDN edge in front of GitHub, ...):
//
//  1. CI (scripts/sign_update_manifest.go) builds manifest-client.json
//     listing every platform artifact's exact SHA-256, then signs those
//     exact bytes with an Ed25519 private key that only exists as a GitHub
//     Actions secret.
//  2. This package fetches manifest-client.json + manifest-client.json.sig
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

// releaseListMaxBytes caps the GitHub releases listing, which carries every
// asset of every release (~90 KB each) and so overflowed manifestMaxBytes,
// truncating the JSON ("unexpected end of JSON input").
const releaseListMaxBytes = 8 << 20

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

	// Tag is the GitHub release this manifest was fetched from; empty means
	// /releases/latest. Not part of the signed bytes.
	Tag string `json:"-"`
}

// releaseAssetURL points at a fixed, version-independent asset name under
// the repo's /releases/latest — GitHub resolves this to whichever release
// currently has make_latest set, i.e. the newest stable (non-prerelease)
// build. Test/prerelease builds (release-all-test.yml's "test-*" tags)
// never set make_latest, so they're structurally invisible here — the
// update channel only ever sees stable releases.
func releaseAssetURL(name string) string {
	return assetURL("", name)
}

// assetURL is releaseAssetURL for a specific release tag ("" = latest).
func assetURL(tag, name string) string {
	if tag == "" {
		return fmt.Sprintf("https://github.com/%s/%s/releases/latest/download/%s", repoOwner, repoName, name)
	}
	return fmt.Sprintf("https://github.com/%s/%s/releases/download/%s/%s", repoOwner, repoName, tag, name)
}

// latestTestTag finds the newest "test-*" prerelease (the API lists newest
// first). Only picks which release to read; the manifest signature is
// still what gates trust.
func latestTestTag(ctx context.Context, client *http.Client) (string, error) {
	api := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases?per_page=15", repoOwner, repoName)
	body, err := fetchBytesMax(ctx, client, api, releaseListMaxBytes)
	if err != nil {
		return "", fmt.Errorf("list releases: %w", err)
	}
	var releases []struct {
		Tag        string `json:"tag_name"`
		Prerelease bool   `json:"prerelease"`
		Draft      bool   `json:"draft"`
	}
	if err := json.Unmarshal(body, &releases); err != nil {
		return "", fmt.Errorf("parse releases: %w", err)
	}
	for _, r := range releases {
		if r.Prerelease && !r.Draft && strings.HasPrefix(r.Tag, testTagPrefix) {
			return r.Tag, nil
		}
	}
	return "", fmt.Errorf("no test pre-release published")
}

// fetchManifest downloads and signature-verifies this app's update
// manifest. It returns an error for any transport problem, a bad
// signature, or a manifest whose "app" field doesn't match this binary —
// callers must treat all of those as "no update available right now"
// rather than surfacing them as fatal.
func fetchManifest(ctx context.Context, client *http.Client) (*Manifest, error) {
	manifestName := "manifest-" + appName + ".json"
	tag := ""
	if GetChannel() == ChannelTest {
		var err error
		if tag, err = latestTestTag(ctx, client); err != nil {
			return nil, err
		}
	}
	body, err := fetchBytes(ctx, client, assetURL(tag, manifestName))
	if err != nil {
		return nil, fmt.Errorf("fetch manifest: %w", err)
	}
	sigText, err := fetchBytes(ctx, client, assetURL(tag, manifestName+".sig"))
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
	m.Tag = tag
	return &m, nil
}

func fetchBytes(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	return fetchBytesMax(ctx, client, url, manifestMaxBytes)
}

func fetchBytesMax(ctx context.Context, client *http.Client, url string, max int64) ([]byte, error) {
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
	return io.ReadAll(io.LimitReader(resp.Body, max))
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
