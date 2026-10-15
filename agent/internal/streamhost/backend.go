// Package streamhost abstracts the Moonlight/GameStream host process the
// agent manages (capture, encode, and serve a Moonlight stream) behind a
// single Backend interface, so the rest of the agent (app, capture, admin
// API, GUI) never depends on which concrete game-streaming host is running.
// The only implementation in this repository is Sunshine (see
// sunshine_backend.go); nothing here assumes it's the only one that will
// ever exist.
package streamhost

import "time"

// Identity reports which concrete backend is running, for display purposes
// only (logs, GUI status, /api/status) — nothing in the agent branches
// behavior on this, it exists so a human looking at the running agent can
// tell which streaming host it's actually using.
type Identity interface {
	// DisplayName is a short human-readable label, e.g. "Sunshine (Open
	// Source)" or "RustShine (Proprietary)".
	DisplayName() string
}

// ProcessWatcher is implemented by backends that manage their own child OS
// process (both bundled ones, rustshine and Sunshine, do) and can notify a
// caller the instant that process exits, rather than that only ever being
// reflected passively in Running(). Deliberately not folded into Lifecycle
// itself: callers that care check for it via a type assertion (see
// app.Run's own use), so a hypothetical future backend with no real child
// process to watch (or a lifecycle managed entirely externally) doesn't
// need a no-op implementation just to satisfy Backend.
type ProcessWatcher interface {
	// SetOnExit registers fn to run once, every time the backend's own
	// child process exits for any reason (clean Stop(), crash, or an
	// abort() from within the process itself) -- see each backend's own
	// SetOnExit doc comment for the exact recovery-latency problem this
	// solves.
	SetOnExit(fn func())
}

// UpdatePauser is implemented by backends whose own executable can be
// hot-replaced on disk while the agent keeps running (currently just
// rustshine on Windows -- see rustshineBackend's own doc comment). Windows
// locks a running .exe's image section against rename/replace, so the
// update flow stops the main process first specifically to release that
// lock -- but that's not the only thing that can open a fresh handle on the
// same binary: a --list-capture-devices helper subprocess (spawned by
// ListCaptureDevices, itself triggered by an ordinary /api/video/devices
// poll a client's GUI keeps making regardless of what the update flow is
// doing) can win a race against the rename by momentarily re-locking the
// file the instant the main process lets go of it. SetUpdateInProgress(true)
// tells the backend to skip spawning any such helper for as long as an
// update's stop-download-rename sequence is in flight; ordinary polling
// callers just see ListCaptureDevices return nothing new until it clears.
type UpdatePauser interface {
	SetUpdateInProgress(inProgress bool)
}

// Lifecycle starts, stops, and health-checks the game-streaming host process.
type Lifecycle interface {
	Start(adminPort int) error
	Stop() error
	Running() bool
	// WaitReady polls adminPort until the host's HTTPS/NvHTTP listener
	// accepts a connection, or the deadline passes.
	WaitReady(adminPort int, deadline time.Duration) bool
	// SetCapExecPath sets the path to a capability-granted launcher binary
	// used to start the host with elevated capture privileges (Linux KMS
	// only). A no-op for backends with no equivalent concept.
	SetCapExecPath(path string)
	// BinaryPath and CapExecPath return the fully resolved (staged-if-needed)
	// paths to the bundled host binary / its capability launcher, or "" if
	// not bundled on this OS.
	BinaryPath() string
	CapExecPath() string
	// Pid returns the running host process's PID, or 0 if not running --
	// used by internal/permissions' Windows GPU-clock-lock helper (see
	// service_windows.go), which needs a PID to watch so it knows when to
	// release the NVML lock.
	Pid() int
}

// ConfigStore reads/writes the subset of the backend's persistent config
// file the agent controls.
type ConfigStore interface {
	ConfigPath() string
	LogPath() string
	// SetConfigKey/ConfigKey are the generic escape hatch: a backend with
	// config keys no other backend has (e.g. an adapter/connector selector)
	// can be driven through these without this interface needing to grow
	// backend-specific typed methods.
	SetConfigKey(key, value string) error
	ConfigKey(key string) string
	SetExternalIP(ip string) error
	ExternalIP() string
	SetBindAddress(ip string) error
	BindAddress() string
	SetCaptureMode(mode string) error
	CaptureMode() string
	SetAudioSink(sink string) error
	AudioSink() string
	SetOutputName(name string) error
	OutputName() string
}

// CodecProbe reports which video codec is/can be used. CurrentVideoCodec is
// a best-effort "what did the last session actually use" hint;
// SupportedVideoCodecs is a live hardware-capability probe independent of
// any session history.
type CodecProbe interface {
	CurrentVideoCodec() string
	SupportedVideoCodecs(adminPort int) []string
	// SessionActive reports whether a Moonlight client is currently
	// mid-stream, as opposed to merely paired or the streamer process
	// merely running idle -- see session_active.go.
	SessionActive() bool
	// Color444Status reports the RustShine Pro color upgrade's state:
	// active is whether the current/most recent session actually negotiated
	// 4:4:4 chroma; available is whether this host could offer it right now
	// (hardware probe AND license tier -- see gamestream-server's
	// GameStreamConfig::color444_supported/AppState::color444_licensed
	// docs). Always (false, false) on a backend with no such concept
	// (Sunshine).
	Color444Status() (active bool, available bool)
	// HdrStatus mirrors Color444Status's shape, for the RustShine HDR color
	// upgrade (HEVC Main10, BT.2020 + PQ) instead of 4:4:4 chroma -- see
	// gamestream-server's GameStreamConfig::hdr_supported/AppState::hdr_licensed
	// docs, and rust-shine's docs/COLOR_MODES.md for why this is an
	// independent axis (today: macOS-only, unlike 4:4:4 which is Linux-only).
	// Unlike Color444Status, available here is hardware probe only -- HDR is
	// free-tier, no license check.
	HdrStatus() (active bool, available bool)
	// PyroWaveColorStatus reports whether this host can offer the color
	// upgrades with PyroWave (rust-shine on Linux and macOS: any GPU that
	// encodes PyroWave does 4:4:4 and HDR -- Vulkan slots on Linux,
	// ScreenCaptureKit's 4:4:4 / BT.2100 PQ captures on macOS; the Windows
	// encoder is 8-bit 4:2:0 and reports neither. 4:4:4 still needs the Pro
	// license, HDR is free). Independent of Color444Status/HdrStatus, which
	// are the HEVC encoder's. (false, false) on backends without PyroWave.
	PyroWaveColorStatus() (color444 bool, hdr bool)
	// VirtualDisplayPrimary / SetVirtualDisplayPrimary: whether the virtual
	// monitor is made the primary display (taskbar and new windows go there).
	// Saved for the next virtual monitor and applied to the live one; live
	// reports whether one existed. Backends without it report true / do
	// nothing.
	VirtualDisplayPrimary() bool
	SetVirtualDisplayPrimary(primary bool) (live bool, err error)
	// SetVirtualDisplayExclusive saves whether the next virtual monitor
	// switches every other monitor off while it exists (back on when it goes
	// away). Backends without it do nothing.
	SetVirtualDisplayExclusive(exclusive bool) error
	// VirtualDisplaySupported reports whether this backend supports native
	// virtual displays (creation and streaming) without external physical monitors.
	VirtualDisplaySupported() bool
	// RawHIDSupported reports whether this backend's streamer process
	// understands LiSendRawHidEvent -- the USBridge-proprietary moonlight-
	// common-c extension that carries a Wacom tablet's live input reports
	// over the stream's control channel instead of USB/IP (see
	// client/docs/TABLETS.md, "Over the video stream instead of USB/IP").
	// rust-shine's own streamer rebuilds the tablet from that stream
	// (usb-passthrough/src/virtual_rawhid.rs), and a punktfunk-host or
	// Sunshine with the USBridge patch hands it to the USB broker to
	// rebuild (see usbBrokerEnv). Stock Sunshine and Punktfunk have no
	// handler, so the client must not switch a tablet into stream mode
	// against them: it would silently stop working (no USB/IP fallback
	// kicks in once the client has committed to stream mode).
	// The client learns this over MasterSyncResponse.RawHIDSupported
	// (agent/internal/api/sync.go), refreshed on every sync, and gates
	// DiskWidget.splitRawHID on it.
	RawHIDSupported() bool
}

// Client is a Moonlight client paired with the streaming host.
type Client struct {
	Name     string `json:"name"`
	UniqueID string `json:"uuid"`
}

// PairingAPI is the admin-API pairing surface: list/submit-pin/unpair over
// HTTPS with basic auth.
type PairingAPI interface {
	AdminUser() string
	AdminPass() string
	ListClients(adminPort int) ([]Client, error)
	SubmitPIN(adminPort int, pin string) error
	UnpairClient(adminPort int, uniqueID string) error
}

// CaptureDevice is one capturable output as the backend itself identifies
// it, for correlating against the agent's own independent display
// enumeration (display.Connectors on Linux, kbinani/screenshot index on
// Windows).
type CaptureDevice struct {
	// Key is the connector name (e.g. "DP-2") this entry correlates to, when
	// the backend reports correlation by name (Linux). "" when the backend
	// identifies devices directly (Windows) rather than by matching a name.
	Key string
	// OutputName is the value to persist via SetOutputName to pin capture to
	// this device.
	OutputName    string
	DisplayName   string
	Primary       bool
	Width, Height int
	// GDIName is the monitor's Windows GDI device name (`\\.\DISPLAY1`),
	// "" elsewhere. Both Windows backends report it, so it is what ties one
	// physical monitor to each backend's own OutputName (Sunshine's
	// device_id GUID, rustshine's DXGI index).
	GDIName string
	// Adapter and VendorID are the GPU driving the monitor ("NVIDIA GeForce
	// RTX 3090", 0x10DE), where the backend reports them (rustshine on
	// Windows); empty/0 elsewhere.
	Adapter  string
	VendorID uint32
}

// CaptureDeviceLister is the platform-specific device/monitor-correlation
// seam. Nothing in this interface assumes how a backend gets this data (log
// scrape, CLI flag, config keys) — only that it can report it.
type CaptureDeviceLister interface {
	ListCaptureDevices() []CaptureDevice
}

// NetworkPorts reports the fixed TCP/UDP ports (relative to an NvHTTP-style
// base port) the host listens on, for tsnet port forwarding. Kept as a
// method (not a shared constant) since nothing guarantees this offset
// scheme is protocol-universal rather than specific to one backend's own
// port arithmetic.
type NetworkPorts interface {
	Ports(basePort int) (tcp []int, udp []int)
}

// Backend is the full abstraction agent/internal/app, agent/internal/capture,
// and agent/internal/api use instead of depending on any one concrete
// game-streaming host implementation.
type Backend interface {
	Identity
	Lifecycle
	ConfigStore
	CodecProbe
	PairingAPI
	CaptureDeviceLister
	NetworkPorts
}

// BackendKind identifies which concrete backend b is, as the short string
// shared_auth.go's RemoveTrustedClientEverywhere/resolveFingerprint use to
// know which native trust-file format and unpair-identifier semantics apply
// (see UnpairClient's per-backend doc comments: a uniqueid for Sunshine/
// rust-shine, a certificate fingerprint for punktfunk). "" for any other
// Backend implementation (e.g. a test fake) -- callers must treat that as
// "don't know how to sync this one" rather than guessing.
func BackendKind(b Backend) string {
	switch b.(type) {
	case *sunshineBackend:
		return "sunshine"
	case *rustshineBackend:
		return "rustshine"
	case *punktfunkBackend:
		return "punktfunk"
	default:
		return ""
	}
}
