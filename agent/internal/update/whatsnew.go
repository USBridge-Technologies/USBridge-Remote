package update

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// whatsNewMaxBytes bounds the release-notes download before it is verified.
const whatsNewMaxBytes = 256 << 10

const whatsNewTimeout = 8 * time.Second

func whatsNewAssetName() string { return "whatsnew-" + appName + ".json" }

func whatsNewCachePath() string {
	dir, err := os.UserCacheDir()
	if err != nil || dir == "" {
		return ""
	}
	return filepath.Join(dir, "USBridge", whatsNewAssetName())
}

// verifyWhatsNew checks body against the detached base64 Ed25519 signature
// with the same key as the update manifest. Notes are only text for the
// What's new card, but they are signed so a hijacked CDN can't put its own
// words or links into the app.
func verifyWhatsNew(body, sigText []byte) error {
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sigText)))
	if err != nil {
		return fmt.Errorf("decode whatsnew signature: %w", err)
	}
	if !ed25519.Verify(publicKey, body, sig) {
		return fmt.Errorf("whatsnew signature verification failed")
	}
	return nil
}

// FetchWhatsNew downloads and verifies the release notes JSON of the
// selected channel's release ("latest" for stable, the newest test-*
// prerelease for test) and caches it for offline launches. The caller
// parses the returned bytes.
func FetchWhatsNew(ctx context.Context) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, whatsNewTimeout)
	defer cancel()
	client := &http.Client{Timeout: whatsNewTimeout}
	tag := ""
	if GetChannel() == ChannelTest {
		var err error
		if tag, err = latestTestTag(ctx, client); err != nil {
			return nil, err
		}
	}
	name := whatsNewAssetName()
	body, err := fetchBytesMax(ctx, client, assetURL(tag, name), whatsNewMaxBytes)
	if err != nil {
		return nil, fmt.Errorf("fetch whatsnew: %w", err)
	}
	sigText, err := fetchBytesMax(ctx, client, assetURL(tag, name+".sig"), manifestMaxBytes)
	if err != nil {
		return nil, fmt.Errorf("fetch whatsnew signature: %w", err)
	}
	if err := verifyWhatsNew(body, sigText); err != nil {
		return nil, err
	}
	if p := whatsNewCachePath(); p != "" {
		if os.MkdirAll(filepath.Dir(p), 0o755) == nil {
			_ = os.WriteFile(p+".sig", sigText, 0o644)
			_ = os.WriteFile(p, body, 0o644)
		}
	}
	return body, nil
}

// CachedWhatsNew returns the last verified notes FetchWhatsNew stored, or
// nil. The signature is checked again, so a tampered cache file is ignored.
func CachedWhatsNew() []byte {
	p := whatsNewCachePath()
	if p == "" {
		return nil
	}
	body, err := os.ReadFile(p)
	if err != nil || len(body) > whatsNewMaxBytes {
		return nil
	}
	sigText, err := os.ReadFile(p + ".sig")
	if err != nil || verifyWhatsNew(body, sigText) != nil {
		return nil
	}
	return body
}
