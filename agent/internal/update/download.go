package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
)

// downloadArtifact streams the given URL into a new temp file inside dir
// while hashing it, and rejects the result if the digest doesn't match
// wantSHA256Hex exactly. wantSHA256Hex comes from the already
// signature-verified manifest (see manifest.go's doc comment) — this is
// the second half of the MITM defense: even a transport that serves a
// completely different file than the one CI published gets caught here
// before the caller ever executes or installs anything.
//
// On any failure the partial temp file is removed and the error is
// returned; callers must not apply an update whose download didn't
// succeed cleanly.
func downloadArtifact(ctx context.Context, client *http.Client, url, wantSHA256Hex string) (path string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "USBridge-"+appName+"-updater")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("download %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download %s: unexpected status %d", url, resp.StatusCode)
	}

	f, err := os.CreateTemp("", "usbridge-"+appName+"-update-*")
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
	if _, err = io.Copy(io.MultiWriter(f, h), resp.Body); err != nil {
		return "", fmt.Errorf("download %s: %w", url, err)
	}
	if err = f.Sync(); err != nil {
		return "", err
	}

	gotSHA256Hex := hex.EncodeToString(h.Sum(nil))
	if gotSHA256Hex != wantSHA256Hex {
		err = fmt.Errorf("downloaded artifact hash mismatch (got %s, manifest says %s) — refusing to apply, this download may have been tampered with", gotSHA256Hex, wantSHA256Hex)
		return "", err
	}

	return tmpPath, nil
}
