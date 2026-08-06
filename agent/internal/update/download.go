package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// ProgressFunc is called periodically while an update artifact downloads,
// with bytes downloaded so far and the total size. total is 0 if the
// server didn't send a Content-Length (rare for a GitHub release asset,
// but callers should treat 0 as "unknown", not "0%"). Called from
// whatever goroutine DownloadAndApply runs on — callers that touch a GUI
// from it must hop back to the UI thread themselves (e.g. fyne.Do).
type ProgressFunc func(downloaded, total int64)

// progressInterval throttles ProgressFunc calls so a fast local network
// doesn't flood the UI thread with hundreds of updates a second — this is
// purely a UI refresh rate, unrelated to the download itself.
const progressInterval = 100 * time.Millisecond

// downloadArtifact streams the given URL into a new temp file inside dir
// while hashing it, and rejects the result if the digest doesn't match
// wantSHA256Hex exactly. wantSHA256Hex comes from the already
// signature-verified manifest (see manifest.go's doc comment) — this is
// the second half of the MITM defense: even a transport that serves a
// completely different file than the one CI published gets caught here
// before the caller ever executes or installs anything.
//
// onProgress may be nil (no progress reporting).
//
// On any failure the partial temp file is removed and the error is
// returned; callers must not apply an update whose download didn't
// succeed cleanly.
func downloadArtifact(ctx context.Context, client *http.Client, url, wantSHA256Hex string, onProgress ProgressFunc) (path string, err error) {
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
	dst := io.MultiWriter(f, h)
	if onProgress != nil {
		pw := &progressWriter{total: resp.ContentLength, onProgress: onProgress}
		onProgress(0, resp.ContentLength) // report the total as soon as it's known, before any bytes arrive
		dst = io.MultiWriter(dst, pw)
	}
	if _, err = io.Copy(dst, resp.Body); err != nil {
		return "", fmt.Errorf("download %s: %w", url, err)
	}
	if err = f.Sync(); err != nil {
		return "", err
	}
	if onProgress != nil && resp.ContentLength > 0 {
		onProgress(resp.ContentLength, resp.ContentLength) // always end at exactly 100%
	}

	gotSHA256Hex := hex.EncodeToString(h.Sum(nil))
	if gotSHA256Hex != wantSHA256Hex {
		err = fmt.Errorf("downloaded artifact hash mismatch (got %s, manifest says %s) — refusing to apply, this download may have been tampered with", gotSHA256Hex, wantSHA256Hex)
		return "", err
	}

	return tmpPath, nil
}

// progressWriter is a throttled io.Writer adapter that reports cumulative
// bytes written via ProgressFunc — written bytes, not "processed" bytes, so
// it only needs a running counter, no buffering.
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
