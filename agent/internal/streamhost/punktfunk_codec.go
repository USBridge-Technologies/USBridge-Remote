package streamhost

import (
	"encoding/json"
	"net/http"
)

// punktfunkRuntimeStatus is the subset of punktfunk-host's RuntimeStatus
// schema (GET /api/v1/status) this backend actually reads. json.Decode
// ignores the many fields not listed here (games, sessions, audio,
// display, ...) -- confirmed shape from api/openapi.json's RuntimeStatus
// component.
type punktfunkRuntimeStatus struct {
	ActiveSessions int `json:"active_sessions"`
}

// SessionActive reports whether a Moonlight client is currently mid-stream
// on Punktfunk's GameStream plane, via its own live RuntimeStatus.active_sessions
// (the app's real state, not a log scrape) -- same precedent as
// rustshineBackend.SessionActive (rustshine_codec.go) and the fixed
// sunshineBackend.SessionActive (session_active.go).
func (b *punktfunkBackend) SessionActive() bool {
	b.mu.Lock()
	adminPort, token := b.adminPort, b.token
	b.mu.Unlock()
	if adminPort <= 0 {
		return false
	}
	resp, err := punktfunkRequest(http.MethodGet, adminPort, "/status", token, nil)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false
	}
	var status punktfunkRuntimeStatus
	if json.NewDecoder(resp.Body).Decode(&status) != nil {
		return false
	}
	return status.ActiveSessions > 0
}

// CurrentVideoCodec has no confirmed Punktfunk equivalent yet (RuntimeStatus
// was only read for active_sessions this pass -- see
// agent/docs/PUNKTFUNK_BACKEND_TODO.md). "" means unknown, same as any other
// backend's best-effort hint when it has nothing to report.
func (b *punktfunkBackend) CurrentVideoCodec() string { return "" }

// SupportedVideoCodecs reads the GameStream plane's own /serverinfo, as the
// other backends do: its ServerCodecModeSupport is probed against the GPU,
// and carries the USBridge PyroWave bit when the host can encode it.
// Punktfunk's GameStream ports are fixed (HTTP 47989), whatever port its
// management API was moved to.
func (b *punktfunkBackend) SupportedVideoCodecs(adminPort int) []string {
	return fetchSupportedVideoCodecs(punktfunkGameStreamHTTPPort + 1)
}

// punktfunkGameStreamHTTPPort is HTTP_PORT in punktfunk's gamestream/mod.rs.
const punktfunkGameStreamHTTPPort = 47989

// Color444Status: no confirmed Punktfunk concept (RustShine Pro-only color
// upgrade) -- always (false, false), same contract CodecProbe documents for
// Sunshine.
func (b *punktfunkBackend) Color444Status() (active bool, available bool) { return false, false }

// HdrStatus: no confirmed Punktfunk admin-API surface for this yet (distinct
// from VirtualDisplaySupported, which IS confirmed via pf-vdisplay) --
// always (false, false) until a real endpoint is found.
func (b *punktfunkBackend) HdrStatus() (active bool, available bool) { return false, false }

// PyroWaveColorStatus: this backend's PyroWave is 8-bit 4:2:0 only.
func (b *punktfunkBackend) PyroWaveColorStatus() (color444 bool, hdr bool) { return false, false }

// VirtualDisplaySupported is true: Punktfunk's pf-vdisplay crate creates a
// virtual output per session on every supported compositor (KWin, Mutter,
// Hyprland, wlroots, gamescope) and via the Windows IddCx driver -- confirmed
// directly from docs-site/content/docs/developers/architecture.md ("The host
// never scales a virtual display. Each session gets an output at the
// client's WxH@Hz").
func (b *punktfunkBackend) VirtualDisplaySupported() bool { return true }

// RawHIDSupported is true only for a punktfunk-host built with the USBridge
// patch, which hands LiSendRawHidEvent to the USB broker (see
// usbBrokerEnv). Stock Punktfunk has no handler for it. The host
// sets LI_FF_USBRIDGE_RAW_HID only when the broker is new enough to know
// hid_stream, and the client checks that flag as well. Whether a device is
// really built is the broker's call: a Wacom tablet needs a Pro license.
func (b *punktfunkBackend) RawHIDSupported() bool {
	return streamerHasUSBBridge(b.binaryPath(), "usbridge-bridge", nil)
}

// VirtualDisplayPrimary: making the virtual monitor primary is RustShine's
// (see rustshineBackend.SetVirtualDisplayPrimary); nothing to switch here.
func (b *punktfunkBackend) VirtualDisplayPrimary() bool { return false }

// SetVirtualDisplayPrimary: see VirtualDisplayPrimary.
func (b *punktfunkBackend) SetVirtualDisplayPrimary(bool) (bool, error) { return false, nil }

// SetVirtualDisplayExclusive: see VirtualDisplayPrimary.
func (b *punktfunkBackend) SetVirtualDisplayExclusive(bool) error { return nil }
