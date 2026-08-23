package entitlement

import "time"

// Status is what the GUI (and, over adminapi, a thin-client GUI attached to
// a separate headless engine process) needs to render the "Support us on
// Patreon" affordance and, once linked, the Sunshine/RustShine switch.
// Deliberately provider-agnostic in shape (no "patreon" field names) even
// though Patreon is the only backend provider today — mirrors the backend's
// own provider-agnostic HTTP contract (see usbridge-entitlement-backend's
// README).
type Status struct {
	// Linked is true once a verified, unexpired entitlement token is
	// cached locally -- i.e. this install is currently allowed to run
	// RustShine, independent of which backend is actually active right
	// now (see ActiveBackend).
	Linked    bool      `json:"linked"`
	Tier      string    `json:"tier,omitempty"`
	ExpiresAt time.Time `json:"expires_at,omitempty"`

	// ActiveBackend is "sunshine" or "rustshine" -- which one is actually
	// running right now, independent of Linked (a linked supporter can
	// still choose Sunshine).
	ActiveBackend string `json:"active_backend"`
	// RustShineStaged is true once the binary has actually been
	// downloaded and verified onto disk -- switching to RustShine before
	// this is true requires a download first.
	RustShineStaged bool `json:"rustshine_staged"`
	// WebRTCEnabled mirrors cfg.RustShineWebRTCDisabled (inverted) -- the
	// GUI's RustShine web-client checkbox reflects and toggles this.
	// Meaningful only when ActiveBackend == "rustshine"; Sunshine has no
	// WebRTC endpoint of its own.
	WebRTCEnabled bool `json:"webrtc_enabled"`

	// LinkInProgress/DownloadInProgress + Progress (0..1, -1 if
	// indeterminate/unknown total) describe an in-flight operation the GUI
	// should show a spinner/progress bar for.
	LinkInProgress     bool    `json:"link_in_progress"`
	DownloadInProgress bool    `json:"download_in_progress"`
	Progress           float64 `json:"progress"`

	// LastError is a short, user-presentable message for the most recent
	// failed operation (login denied, below tier, download failed, ...) —
	// cleared on the next successful step. Empty string means "nothing to
	// report."
	LastError string `json:"last_error,omitempty"`
}
