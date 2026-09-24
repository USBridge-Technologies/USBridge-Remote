package streamhost

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"runtime"
	"time"
)

// statusResponse mirrors gamestream-server's confirmed GET /api/status JSON
// shape: {"active_video_codec": "h264"|"h265", "active_pixel_format": "...",
// "active_chroma_444": bool, "color_444_available": bool, "active_hdr": bool,
// "hdr_available": bool} -- see gamestream_proto::http::admin::StatusInfo.
type statusResponse struct {
	ActiveVideoCodec  string `json:"active_video_codec"`
	ActiveChroma444   bool   `json:"active_chroma_444"`
	Color444Available bool   `json:"color_444_available"`
	ActiveHdr         bool   `json:"active_hdr"`
	HdrAvailable      bool   `json:"hdr_available"`
}

// rustshineAdminHTTPClient is shared across every CurrentVideoCodec call --
// deliberately package-level and constructed exactly once, NOT a fresh
// `&http.Client{...}` per call the way this used to be written. An
// `http.Transport` owns a persistent-connection pool that's meant to be
// reused for exactly this "hit the same host repeatedly" polling pattern;
// throwing the whole Client/Transport away after a single call (as the
// window's own status-refresh ticker does every 2 seconds -- see
// `ui.Window.ShowAndRun`'s `time.NewTicker(2 * time.Second)`) doesn't close
// the connection it just used. `resp.Body.Close()` alone only returns the
// connection to *that Transport's* idle pool for potential reuse -- with no
// other reference to the Transport left (it was a local variable), nothing
// ever calls `CloseIdleConnections`, and Go's GC has no finalizer that
// proactively closes idle persistent connections, only ones on the raw fd
// itself once (and if) the whole unreachable Transport is actually
// collected. Confirmed live: a real multi-hour streaming session leaked
// enough of these to exhaust gamestream-server's 1024 fd limit ("Too many
// open files"), degrading the stream to a handful of fps with no client
// reconnect able to fix it, since the leak was entirely in this polling
// path, independent of any streaming session's own state.
var rustshineAdminHTTPClient = &http.Client{
	Timeout: 2 * time.Second,
	Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
	},
}

// fetchStatus hits gamestream-server's own /api/status admin route directly
// — unlike Sunshine, there's no documented log line to scrape as a fallback,
// but the admin API is always reachable once the process is up (confirmed
// route, HTTP Basic Auth). Returns nil on any failure (not reachable yet,
// bad response) -- callers each have their own "what to report before a
// session has ever run" default, so this doesn't pick one itself.
func (b *rustshineBackend) fetchStatus() *statusResponse {
	b.mu.Lock()
	adminPort := b.adminPort
	b.mu.Unlock()
	if adminPort <= 0 {
		adminPort = 47990
	}
	url := fmt.Sprintf("https://%s:%d/api/status", adminHost(), adminPort)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil
	}
	req.SetBasicAuth(b.AdminUser(), b.AdminPass())
	resp, err := rustshineAdminHTTPClient.Do(req)
	if err != nil {
		log.Printf("🎯 [CODEC-TRACE] [rustshine] GET %s failed: %v", url, err)
		return nil
	}
	defer resp.Body.Close()
	var status statusResponse
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		log.Printf("[rustshine] /api/status decode failed: %v", err)
		return nil
	}
	log.Printf("🎯 [CODEC-TRACE] [rustshine] GET %s -> active_video_codec=%q chroma444=%v hdr=%v", url, status.ActiveVideoCodec, status.ActiveChroma444, status.ActiveHdr)
	return &status
}

// CurrentVideoCodec reports which codec the most recent (or current)
// session actually negotiated. Defaults to "h264" if the server isn't
// reachable yet.
func (b *rustshineBackend) CurrentVideoCodec() string {
	status := b.fetchStatus()
	if status == nil || status.ActiveVideoCodec == "" {
		return "h264"
	}
	return status.ActiveVideoCodec
}

// Color444Status reports the RustShine Pro color upgrade's state -- see
// CodecProbe's doc comment. Defaults to (false, false) if the server isn't
// reachable yet, matching CurrentVideoCodec's own "assume nothing special"
// default.
func (b *rustshineBackend) Color444Status() (active bool, available bool) {
	status := b.fetchStatus()
	if status == nil {
		return false, false
	}
	return status.ActiveChroma444, status.Color444Available
}

// HdrStatus reports the RustShine HDR color upgrade's state -- mirrors
// Color444Status exactly, see CodecProbe's doc comment.
func (b *rustshineBackend) HdrStatus() (active bool, available bool) {
	status := b.fetchStatus()
	if status == nil {
		return false, false
	}
	return status.ActiveHdr, status.HdrAvailable
}

// VirtualDisplaySupported reports whether this backend supports native
// virtual displays: Windows (MttVDD, or SudoVDA), macOS (CGVirtualDisplay), and Linux
// desktop builds (the in-tree vkms kernel module -- see rust-shine's
// virtual_display::linux doc comment). The desktop Linux AppImage/deb this
// agent ever stages is always built with the "desktop" feature (KMS
// capture, the only realistic desktop-screen-capture path), which is the
// same feature vkms support is gated behind -- so unconditionally true
// here mirrors Windows/macOS, not a runtime capability probe.
func (b *rustshineBackend) VirtualDisplaySupported() bool {
	return runtime.GOOS == "windows" || runtime.GOOS == "darwin" || runtime.GOOS == "linux"
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

	flags, ok := fetchServerCodecFlags(adminPort)
	codecs := codecsFromFlags(flags, ok)
	// A failed query (the streamer restarting -- an update, a config
	// change -- and not listening yet) yields the h264-only fallback. Caching
	// that pinned the client to H.264 for the whole TTL: confirmed live, an
	// update restart at 01:53:30 was queried 0.8s later, got "connection
	// refused", and H.265 vanished from the client's codec list for 30
	// minutes. Only a real answer is cached; the next call retries.
	if ok {
		b.supportedCodecsCache.mu.Lock()
		b.supportedCodecsCache.codecs = codecs
		b.supportedCodecsCache.fetchedAt = time.Now()
		b.supportedCodecsCache.mu.Unlock()
	}
	return codecs
}
