package entitlement

import (
	"strings"
	"time"
)

// Status is what the GUI (and, over adminapi, a thin-client GUI attached to
// a separate headless engine process) needs to render the four-way
// license switcher (see internal/ui's showLicenseDialog) and, once
// entitled, the Sunshine/RustShine switch.
type Status struct {
	// Linked is true once a verified, unexpired, hardware-bound desktop
	// token (any tier -- free/pro/enterprise) is cached locally. Only
	// false in the brief window before app.go's bootstrapFreeTier first
	// runs, or if this machine's hardware id can't be determined at all --
	// every hardware id gets an unconditional free tier with no purchase,
	// so in practice this is true almost all the time. The name predates
	// the Patreon->Stripe/hardware-bound switch (see this package's doc
	// comment) but the JSON field name is kept stable -- nothing
	// downstream needs to change just because what "linked" means
	// underneath did.
	Linked bool `json:"linked"`
	// Tier is "free", "pro", or "enterprise" -- see entitlement.Claims.Tier.
	// "pro" ($8/mo) gates both RustShine's 4:4:4 color upgrade end-to-end
	// (see rust-shine's video-encode/vaapi.rs and
	// gamestream_proto::server_state::GameStreamConfig::color444_supported)
	// and USB passthrough (rust-shine's bin/usb-broker require_licensed()).
	// "enterprise" ($25/mo) is a strict superset of "pro" -- billable today
	// but has no gated feature of its own yet beyond what "pro" already
	// unlocks; reserved for per-session logging + team/workspace access
	// once that work starts. See usbridge-entitlement-backend's
	// desktopLicense.ts tier doc comment for the full billing-side
	// reasoning.
	Tier      string    `json:"tier,omitempty"`
	ExpiresAt time.Time `json:"expires_at,omitempty"`

	// Vibepollo (Nonary's Sunshine fork, from Streamers-Forks): offered wherever the release
	// has a build for this platform; picking it downloads it.
	VibepolloAvailable        bool   `json:"vibepollo_available"`
	VibepolloStaged           bool   `json:"vibepollo_staged"`
	VibepolloVersion          string `json:"vibepollo_version,omitempty"`
	VibepolloUpdateInProgress bool   `json:"vibepollo_update_in_progress"`

	// ActiveBackend is "sunshine", "rustshine", "punktfunk" or "vibepollo" -- which one
	// is actually running right now, independent of Linked (a linked
	// supporter can still choose Sunshine).
	ActiveBackend string `json:"active_backend"`
	// PunktfunkAvailable is true when a punktfunk-host binary was found on
	// this machine (see streamhost.PunktfunkAvailable) or Streamers-Forks
	// publishes one for this platform (picking it downloads it); the GUI
	// offers the Punktfunk streamer only then.
	PunktfunkAvailable bool `json:"punktfunk_available,omitempty"`
	// PunktfunkStaged: the agent downloaded punktfunk-host itself
	// (forkrelease), so its card offers "check for updates" (GitHub).
	PunktfunkStaged bool `json:"punktfunk_staged,omitempty"`
	// PunktfunkVersion is the staged punktfunk-host's release tag.
	PunktfunkVersion string `json:"punktfunk_version,omitempty"`
	// PunktfunkUpdateInProgress mirrors RustShineUpdateInProgress for
	// CheckPunktfunkUpdateNow.
	PunktfunkUpdateInProgress bool `json:"punktfunk_update_in_progress,omitempty"`
	// SunshineUpdatable: Sunshine comes from Streamers-Forks here (Windows,
	// Linux x86_64, Apple Silicon) rather than with the agent, so its card offers "check for updates".
	SunshineUpdatable bool `json:"sunshine_updatable,omitempty"`
	// SunshineVersion is the downloaded Sunshine's release tag, "" while
	// a bundled one (older agent install) or none is in use.
	SunshineVersion string `json:"sunshine_version,omitempty"`
	// SunshineUpdateInProgress: a Sunshine download or update is running.
	SunshineUpdateInProgress bool `json:"sunshine_update_in_progress,omitempty"`
	// RustShineStaged is true once the binary has actually been
	// downloaded and verified onto disk -- switching to RustShine before
	// this is true requires a download first.
	RustShineStaged bool `json:"rustshine_staged"`
	// RustShineVersion is whatever StagedVersion(stateDir) currently
	// reports -- "" if never staged, or staged by a build of this agent
	// that predated version tracking. Shown next to the RustShine row so a
	// customer can see what's actually installed without opening the
	// license dialog; the GUI's "Check for updates" button
	// (CheckRustShineUpdateNow) is what refreshes this to a newer value.
	RustShineVersion string `json:"rustshine_version,omitempty"`
	// RustShineUpdateInProgress mirrors DownloadInProgress but specifically
	// for a check triggered by the "Check for updates" button (as opposed
	// to the very first "Download RustShine" click, or the silent
	// background watchdog) -- kept separate so the GUI can show "Checking
	// for updates…" instead of the first-download copy without the two
	// call sites racing each other's spinner text.
	RustShineUpdateInProgress bool `json:"rustshine_update_in_progress"`
	// RustShineAvailableVersion is a newer USBridge-streamer tag than the
	// one currently staged, when auto-update is off and the streamer
	// update watchdog has already seen it. Empty while auto-update is
	// applying (or when nothing newer exists). The Status-card version
	// prefix reads this.
	RustShineAvailableVersion string `json:"rustshine_available_version,omitempty"`
	// RustShineUpdateOffer is true when AvailableVersion is set and the
	// user has not declined that tag yet -- the GUI shows the Yes/No toast.
	RustShineUpdateOffer bool `json:"rustshine_update_offer,omitempty"`
	// WebRTCEnabled mirrors cfg.RustShineWebRTCDisabled (inverted) -- the
	// GUI's RustShine web-client checkbox reflects and toggles this.
	// Meaningful only when ActiveBackend == "rustshine"; Sunshine has no
	// WebRTC endpoint of its own.
	WebRTCEnabled bool `json:"webrtc_enabled"`

	// LinkInProgress/DownloadInProgress + Progress (0..1, -1 if
	// indeterminate/unknown total) describe an in-flight operation the GUI
	// should show a spinner/progress bar for.
	LinkInProgress     bool `json:"link_in_progress"`
	DownloadInProgress bool `json:"download_in_progress"`
	// DownloadName is the streamer DownloadInProgress is for ("Sunshine",
	// "Punktfunk", "USBridge Streamer"), for the GUI's progress copy.
	DownloadName string  `json:"download_name,omitempty"`
	Progress     float64 `json:"progress"`

	// LastError is a short, user-presentable message for the most recent
	// failed operation (checkout failed, download failed, ...) — cleared
	// on the next successful step. Empty string means "nothing to report."
	LastError string `json:"last_error,omitempty"`
}

// Protocol is the active tariff the client shows on a connection plaque:
// "opensource" (Sunshine, and Punktfunk -- the client treats both alike),
// or RustShine "free" / "pro" / "enterprise".
func (s Status) Protocol() string {
	if s.ActiveBackend != "rustshine" {
		return "opensource"
	}
	switch strings.ToLower(s.Tier) {
	case "pro":
		return "pro"
	case "enterprise":
		return "enterprise"
	default:
		return "free"
	}
}
