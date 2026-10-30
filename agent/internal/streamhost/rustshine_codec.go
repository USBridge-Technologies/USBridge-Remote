package streamhost

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// statusResponse mirrors gamestream-server's confirmed GET /api/status JSON
// shape: {"session_active": bool, "active_video_codec": "h264"|"h265",
// "active_pixel_format": "...", "active_chroma_444": bool,
// "color_444_available": bool, "active_hdr": bool, "hdr_available": bool}
// -- see gamestream_proto::http::admin::StatusInfo.
type statusResponse struct {
	SessionActive     bool   `json:"session_active"`
	ActiveVideoCodec  string `json:"active_video_codec"`
	ActiveChroma444   bool   `json:"active_chroma_444"`
	Color444Available bool   `json:"color_444_available"`
	ActiveHdr         bool   `json:"active_hdr"`
	HdrAvailable      bool   `json:"hdr_available"`
	// PyroWave's color upgrades (license halves only, see
	// PyroWaveColorStatus); absent on older rust-shine builds -> false.
	PyroWaveColor444Available bool `json:"pyrowave_color_444_available"`
	PyroWaveHdrAvailable      bool `json:"pyrowave_hdr_available"`
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

// VirtualDisplayPrimary reports the saved "make the virtual monitor the
// primary display" choice (sunshine.conf `virtual_display_primary`; on unless
// turned off).
func (b *rustshineBackend) VirtualDisplayPrimary() bool {
	v := strings.ToLower(strings.TrimSpace(b.ConfigKey("virtual_display_primary")))
	return v != "false" && v != "0" && v != "no" && v != "off"
}

// SetVirtualDisplayExclusive saves sunshine.conf `virtual_display_exclusive`.
// The agent keeps it false: "only the virtual monitor" is the agent's own,
// for every stream host (app.virtualOthersWatchdog).
func (b *rustshineBackend) SetVirtualDisplayExclusive(exclusive bool) error {
	return b.SetConfigKey("virtual_display_exclusive", strconv.FormatBool(exclusive))
}

// TakeLegacyVirtualExclusive reports a `virtual_display_exclusive = true`
// saved before the agent took "only the virtual monitor" over, and turns it
// off.
func (b *rustshineBackend) TakeLegacyVirtualExclusive() bool {
	v := strings.ToLower(strings.TrimSpace(b.ConfigKey("virtual_display_exclusive")))
	if v != "true" && v != "1" && v != "yes" && v != "on" {
		return false
	}
	return b.SetVirtualDisplayExclusive(false) == nil
}

// SetVirtualDisplayPrimary saves the choice for the next virtual monitor and
// applies it to the live one through the streamer's admin API
// (/admin/virtual-display/primary) -- no restart. live reports whether a
// virtual monitor existed to apply it to.
func (b *rustshineBackend) SetVirtualDisplayPrimary(primary bool) (live bool, err error) {
	if err := b.SetConfigKey("virtual_display_primary", strconv.FormatBool(primary)); err != nil {
		return false, err
	}
	if agentVirtualDisplays() {
		// The agent's monitor (rustshine_vdisplay_agent.go), not the streamer's.
		return b.setAgentVirtualDisplayPrimary(primary)
	}
	b.mu.Lock()
	adminPort := b.adminPort
	b.mu.Unlock()
	if adminPort <= 0 {
		adminPort = 47990
	}
	body, _ := json.Marshal(map[string]bool{"primary": primary})
	url := fmt.Sprintf("https://%s:%d/admin/virtual-display/primary", adminHost(), adminPort)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return false, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(b.AdminUser(), b.AdminPass())
	// Re-moding the desktop takes a moment; the shared client's 2 s cap is
	// for polling.
	client := &http.Client{Timeout: 15 * time.Second, Transport: rustshineAdminHTTPClient.Transport}
	resp, err := client.Do(req)
	if err != nil {
		// Saved for the next start even when the streamer isn't up.
		return false, nil
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		// A streamer from before this switch existed.
		return false, nil
	}
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return false, fmt.Errorf("streamer: %s: %s", resp.Status, strings.TrimSpace(string(msg)))
	}
	var out struct {
		VirtualDisplay bool `json:"virtual_display"`
		Primary        bool `json:"primary"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return false, err
	}
	log.Printf("[rustshine] virtual display primary=%v requested -> live=%v primary=%v", primary, out.VirtualDisplay, out.Primary)
	return out.VirtualDisplay, nil
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
	log.Printf("🎯 [CODEC-TRACE] [rustshine] GET %s -> session_active=%v active_video_codec=%q chroma444=%v hdr=%v", url, status.SessionActive, status.ActiveVideoCodec, status.ActiveChroma444, status.ActiveHdr)
	return &status
}

// SessionActive reports whether a Moonlight client currently has a
// launched/resumed session open -- read straight from gamestream-server's
// own AppState::active_session via /api/status, not grepped from this
// process's log. A prior version of this method scanned the stdout log for
// Sunshine-style "CLIENT CONNECTED"/"CLIENT DISCONNECTED" lines, which (a)
// this server doesn't actually emit -- confirmed via `strings` on a real
// build, those literals aren't in the binary at all -- and (b) even when
// matched against stale lines left over from a prior Sunshine run, only
// ever scanned a fixed tail window that a long session's own telemetry
// volume pushes the opening marker out of within under a minute (see
// session_active.go's sessionActiveTracker, which still backs
// sunshineBackend -- Sunshine has no equivalent admin API to read instead).
// Both bugs together meant awdlWatchdog (app/awdl.go) silently stopped
// reasserting awdl0 down during any real RustShine session. Defaults to
// false if the server isn't reachable yet, matching CurrentVideoCodec's own
// "assume nothing special" default.
func (b *rustshineBackend) SessionActive() bool {
	status := b.fetchStatus()
	return status != nil && status.SessionActive
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

// PyroWaveColorStatus: see Backend.PyroWaveColorStatus. (false, false) if the
// server isn't reachable yet or predates the fields.
func (b *rustshineBackend) PyroWaveColorStatus() (color444 bool, hdr bool) {
	status := b.fetchStatus()
	if status == nil {
		return false, false
	}
	return status.PyroWaveColor444Available, status.PyroWaveHdrAvailable
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
	if agentVirtualDisplays() {
		// The agent's monitor (rustshine_vdisplay_agent.go): MttVDD needs its
		// driver; vkms and CGVirtualDisplay are always there.
		return virtualDriverInstalled()
	}
	return runtime.GOOS == "darwin" || runtime.GOOS == "linux"
}

// RawHIDSupported is true: rust-shine's own streamer is the only backend
// that rebuilds a Wacom tablet from LiSendRawHidEvent chunks as a virtual
// USB device (usb-passthrough/src/virtual_rawhid.rs) -- see CodecProbe's
// doc comment.
func (b *rustshineBackend) RawHIDSupported() bool { return true }

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
