//go:build rustshine

package streamhost

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
)

// statusResponse mirrors gamestream-server's confirmed GET /api/status JSON
// shape: {"active_video_codec": "h264"|"h265"}.
type statusResponse struct {
	ActiveVideoCodec string `json:"active_video_codec"`
}

// CurrentVideoCodec queries gamestream-server's own /api/status admin route
// directly — unlike Sunshine, there's no documented log line to scrape as a
// fallback, but the admin API is always reachable once the process is up
// (confirmed route, HTTP Basic Auth), so no fallback is needed. Defaults to
// "h264" if the server isn't reachable yet.
func (b *rustshineBackend) CurrentVideoCodec() string {
	b.mu.Lock()
	adminPort := b.adminPort
	b.mu.Unlock()
	if adminPort <= 0 {
		adminPort = 47990
	}
	url := fmt.Sprintf("https://%s:%d/api/status", adminHost(), adminPort)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "h264"
	}
	req.SetBasicAuth(b.AdminUser(), b.AdminPass())
	c := &http.Client{
		Timeout: 2 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
		},
	}
	resp, err := c.Do(req)
	if err != nil {
		return "h264"
	}
	defer resp.Body.Close()
	var status statusResponse
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		log.Printf("[rustshine] /api/status decode failed: %v", err)
		return "h264"
	}
	if status.ActiveVideoCodec == "" {
		return "h264"
	}
	return status.ActiveVideoCodec
}

// SupportedVideoCodecs reuses the exact same /serverinfo NvHTTP probe as
// Sunshine's (fetchSupportedVideoCodecs, sunshine_codec.go) — confirmed
// gamestream-server implements the identical ServerCodecModeSupport bitmask
// field at the same base-port-minus-1 NvHTTP endpoint.
func (b *rustshineBackend) SupportedVideoCodecs(adminPort int) []string {
	b.supportedCodecsCache.mu.Lock()
	if !b.supportedCodecsCache.fetchedAt.IsZero() && time.Since(b.supportedCodecsCache.fetchedAt) < supportedCodecsCacheTTL {
		cached := b.supportedCodecsCache.codecs
		b.supportedCodecsCache.mu.Unlock()
		return cached
	}
	b.supportedCodecsCache.mu.Unlock()

	codecs := fetchSupportedVideoCodecs(adminPort)

	b.supportedCodecsCache.mu.Lock()
	b.supportedCodecsCache.codecs = codecs
	b.supportedCodecsCache.fetchedAt = time.Now()
	b.supportedCodecsCache.mu.Unlock()
	return codecs
}
