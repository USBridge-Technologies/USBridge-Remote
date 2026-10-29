package config

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	AppName    string `yaml:"app_name"`
	ListenHost string `yaml:"listen_host"`
	HTTPPort   int    `yaml:"http_port"`
	// TLSPort is the second HTTPS listener (see internal/tlshost,
	// internal/devicecert) serving the SAME handler as HTTPPort's plain-HTTP
	// one -- exists so the browser-based web client (client/web, loaded from
	// https://web.usbridge.io) can reach this agent at all: a page served
	// over https can't fetch()/WebSocket to a plain-http origin (mixed
	// content) or an untrusted-cert https origin (no click-through for a
	// background request).
	TLSPort int `yaml:"tls_port"`
	// TLSEnabled is the General Settings "Enable HTTPS" checkbox (see
	// ui.Window's HTTP Listen Address dialog). Nil (omitted in YAML) means
	// on -- the product default, matching StreamerAutoUpdate's identical
	// nil-means-default-true pattern -- so existing configs keep the HTTPS
	// listener without needing a config migration. A pointer is required so
	// an explicit false (the user turned it off) round-trips instead of
	// collapsing back to the default.
	TLSEnabled         *bool `yaml:"tls_enabled,omitempty"`
	UsbPassthroughPort int   `yaml:"usb_passthrough_port"`
	TailscaleEnabled   bool  `yaml:"tailscale_enabled"`
	// GuestOwnTailscale: guests reach this computer over the owner's own
	// Tailscale (the guest's Tailscale must see this node, e.g. a node the
	// owner shared by hand) instead of the USBridge-run guest network.
	GuestOwnTailscale bool   `yaml:"guest_own_tailscale,omitempty"`
	NBDMountCommand   string `yaml:"nbd_mount_command"`
	StateDir          string `yaml:"state_dir"`
	// Moonlight/Sunshine protocol
	MasterKey    string `yaml:"master_key"`
	SunshinePort int    `yaml:"sunshine_port"`
	// SunshineCaptureMode selects Sunshine's Linux capture backend: "" (auto,
	// portal-based, no root), "portal" (explicit XDG desktop portal, no root),
	// or "kms" (direct KMS capture, requires CAP_SYS_ADMIN on the sunshine binary).
	SunshineCaptureMode string `yaml:"sunshine_capture_mode"`
	// SunshineOutputName pins Sunshine's KMS/portal capture to a specific
	// monitor: Sunshine's own connected-output index (stringified), or ""
	// to let Sunshine auto-pick (its default, first-found output).
	SunshineOutputName string `yaml:"sunshine_output_name"`
	// Clipboard sync (agent <-> client shared clipboard)
	ClipboardSyncEnabled bool  `yaml:"clipboard_sync_enabled"`
	ClipboardMaxBytes    int64 `yaml:"clipboard_max_bytes"` // cap per image/file payload
	// LockGPUClocksEnabled: Windows+NVIDIA only. When true, every rustshine
	// backend Start() also launches an elevated gamestream-server
	// --gpu-clock-lock-daemon helper (see internal/permissions/
	// service_windows.go) that holds an NVML max-clock lock for the life of
	// the streaming session -- prevents the GPU idling into a low power
	// state between frames and stalling NVENC 30-60ms on the next one.
	// Requires a UAC consent prompt on every session start (Windows has no
	// one-time-grant equivalent to Linux's CAP_SYS_ADMIN setcap).
	LockGPUClocksEnabled bool `yaml:"lock_gpu_clocks_enabled"`
	// NvencTwoPass and NvidiaMaxPerformance are NVIDIA encoder preferences
	// written into both streamers' configs, under the keys Sunshine and
	// RustShine share (see app.applyNvencPrefs).
	//
	// NvencTwoPass: NVENC's quarter-resolution first pass
	// (nvenc_twopass). Better picture at low bitrates, ~20% more GPU 3D
	// load and ~0.5 ms more encode time. Nil means off -- this agent's
	// default, opposite of the streamers' own on-by-default: the extra
	// GPU load/latency isn't a trade most streaming setups want made for
	// them silently.
	NvencTwoPass *bool `yaml:"nvenc_two_pass,omitempty"`
	// NvidiaMaxPerformance: the driver profile "Prefer maximum
	// performance" for the streamer (nvenc_latency_over_power). Keeps the
	// GPU at full clocks while streaming; without it NVENC slows down
	// whenever the driver lowers the clocks. No admin rights needed,
	// unlike LockGPUClocksEnabled.
	//
	// Superseded by NvidiaPowerMode; read only when that is unset.
	NvidiaMaxPerformance *bool `yaml:"nvidia_max_performance,omitempty"`
	// NvidiaPowerMode: the NVIDIA driver profile's power management mode
	// for the streamer, one of NvidiaPowerModes ("" = the default, "max").
	NvidiaPowerMode string `yaml:"nvidia_power_mode,omitempty"`

	// Hardware-bound RustShine entitlement (see agent/internal/entitlement,
	// agent/internal/hwid). Same trust level as MasterKey above: plain
	// YAML, no separate encryption -- consistent with the rest of this
	// struct, and the entitlement token itself is Ed25519-signed AND
	// bound to this machine's own hwid.Get() value, independently
	// re-verified both locally (entitlement.VerifyForHardware) and against
	// the backend, so a locally-forged value here can't be used to fake
	// entitlement, and a copied value from another machine's config.yaml
	// fails the hardware check even if it copies validly.
	//
	// Unlike the old Patreon-linked scheme this replaced, there is no
	// separate refresh-token secret to persist: RefreshLicense/StartTrial
	// re-derive everything from this machine's own hwid.Get() on every
	// call, nothing durable to store beyond the token itself.
	EntitlementToken string `yaml:"entitlement_token,omitempty"`
	// PreferredBackend is the stream host to run: "rustshine" (the USBridge
	// streamer), "sunshine", "punktfunk" or "vibepollo". "" means the default,
	// the USBridge streamer (see WantsRustShine).
	PreferredBackend string `yaml:"preferred_backend,omitempty"`
	// RustShineWebRTCDisabled turns off gamestream-server's native WebRTC
	// signaling endpoint (--webrtc-disable) -- the surface USBridge's
	// browser/WASM web client connects through. Defaults to false (enabled,
	// matching gamestream-server's own default) so existing installs keep
	// the web client working without needing to opt in.
	RustShineWebRTCDisabled bool `yaml:"rustshine_webrtc_disabled,omitempty"`
	// VirtualDisplayExclusive: while a session streams a virtual monitor, the other
	// monitors are switched off, and back on once it is over -- the agent's own, for
	// every stream host (app.virtualOthersWatchdog).
	VirtualDisplayExclusive bool `yaml:"virtual_display_exclusive,omitempty"`
	// DisableAWDLDuringStreaming (macOS only): bring the awdl0 interface
	// down for the duration of an active streaming session and back up
	// once it ends, since AWDL (AirDrop/Handoff/Sidecar's mesh protocol)
	// shares the Wi-Fi radio and periodically forces channel-hopping scans
	// that hurt a concurrent stream's latency/stability -- see
	// internal/netutil/awdl_darwin.go. Defaults to false/opt-in: enabling
	// it installs a sudoers.d NOPASSWD rule (one admin-password prompt) so
	// it can run without a prompt on every toggle.
	DisableAWDLDuringStreaming bool `yaml:"disable_awdl_during_streaming,omitempty"`
	// WebRTCSignalRelayEnabled gates the agent's outbound WebSocket to
	// usbridge-entitlement's WebRTC signaling relay (see
	// internal/app/webrtc_signal_relay.go, usbridge-entitlement-backend's
	// webrtcSignalRelay.ts) -- lets a browser client with no LAN/Tailscale
	// route to this agent still deliver its "POST /webrtc/offer" signaling
	// request. Nil (omitted in YAML) means on -- same nil-means-default-true
	// convention as TLSEnabled/StreamerAutoUpdate -- so a pro/enterprise box
	// dials out by default; already gated server-side to that tier (free
	// tier gets a clean 403, never reaches this flag's meaning at all), this
	// exists purely so a user/support can opt a pro/enterprise box OUT of
	// the outbound relay connection entirely (e.g. a locked-down network
	// policy), independent of tier.
	WebRTCSignalRelayEnabled *bool `yaml:"webrtc_signal_relay_enabled,omitempty"`

	// StreamerAutoUpdate is the General Settings "USBridge protocol auto-update"
	// checkbox for USBridge-streamer. Nil (omitted in YAML) means on --
	// the product default -- so existing config files keep silent
	// background updates. A pointer is required so an explicit false
	// round-trips instead of collapsing to that default. Checks still
	// piggyback on streamerUpdateWatchdog (once an hour -- see
	// streamerUpdateCheckInterval).
	StreamerAutoUpdate *bool `yaml:"streamer_auto_update,omitempty"`
	// StreamerUpdateSnoozed is the USBridge-streamer release tag the user
	// declined ("No" on the update toast). The header still shows that an
	// update is available; the toast is not shown again for this tag.
	StreamerUpdateSnoozed string `yaml:"streamer_update_snoozed,omitempty"`

	// RemoteWindowLock is the General Settings "Block remote control of this
	// window" checkbox. Nil (omitted) and false both mean off -- opt-in, so
	// existing installs keep the old "remote session can click the agent"
	// behavior. When on, the GUI process drops SendInput-injected mouse
	// and keyboard aimed at its own windows (see internal/remotelock);
	// real local hardware input is not touched.
	RemoteWindowLock *bool `yaml:"remote_window_lock,omitempty"`

	// USBBrokerConsent is the one-time, explicit "enable USB passthrough"
	// consent from the USB status row's button (see ui.Window's
	// usbBrokerRow). Nil (omitted) and false both mean the closed
	// usb-broker binary must never be downloaded or started, even for a
	// free-tier device (mouse/keyboard/gamepad) and even once entitled --
	// opt-in, same nil-means-false shape as RemoteWindowLock above, because
	// by default this agent must run only the open-source Go code. See
	// App.EnableUSBBroker (the only place this ever flips true) and
	// App.ensureUSBBroker (the only place it gates staging/starting the
	// binary).
	USBBrokerConsent *bool `yaml:"usb_broker_consent,omitempty"`

	// Account login (see agent/internal/account) -- a SEPARATE identity
	// from EntitlementToken above: this is "which USBridge account (Google
	// login) is the human running this agent signed into", used only to
	// list/rebind THAT account's desktop licenses (App.RebindLicenseToThisDevice).
	// It never gates RustShine on its own -- EntitlementToken (hardware-bound)
	// remains the only thing that does, same trust level as MasterKey/
	// EntitlementToken: plain YAML, no separate encryption. AccountToken is
	// a long-lived (30-day) Bearer token; AccountLoginToken() re-logs-in
	// once it's rejected rather than trying to refresh it silently.
	AccountEmail string `yaml:"account_email,omitempty"`
	AccountToken string `yaml:"account_token,omitempty"`
}

func Default() Config {
	remoteLockOff := false
	return Config{
		AppName:            "USBridge Agent",
		ListenHost:         "0.0.0.0",
		HTTPPort:           8080,
		TLSPort:            8443,
		UsbPassthroughPort: 8090,
		TailscaleEnabled:   true,
		NBDMountCommand:    "",
		StateDir:           defaultStateDir(),
		SunshinePort:       47990,

		ClipboardSyncEnabled: true,
		ClipboardMaxBytes:    200 * 1024 * 1024,
		RemoteWindowLock:     &remoteLockOff,
	}
}

func (c Config) EffectiveListenHost() string {
	host := strings.TrimSpace(c.ListenHost)
	if host == "" {
		return "127.0.0.1"
	}
	return host
}

// StreamerAutoUpdateEnabled is true unless the user turned the General
// Settings checkbox off. Omitted YAML (nil) is on, matching the product
// default.
func (c Config) StreamerAutoUpdateEnabled() bool {
	return c.StreamerAutoUpdate == nil || *c.StreamerAutoUpdate
}

// NvencTwoPassOK is false unless explicitly turned on; NvidiaMaxPerformanceOK
// is true unless turned off.
// WantsRustShine reports whether the USBridge streamer is the stream host to
// run: picked explicitly, or nothing picked yet (it is the default).
func (c Config) WantsRustShine() bool {
	return c.PreferredBackend == "" || c.PreferredBackend == "rustshine"
}

func (c Config) NvencTwoPassOK() bool { return c.NvencTwoPass != nil && *c.NvencTwoPass }

func (c Config) NvidiaMaxPerformanceOK() bool { return c.NvidiaPowerModeValue() == "max" }

// GPUInfo is one of the host's GPUs as the agent's encoder settings show
// it: its name, vendor ("nvidia", "amd", "intel", or ""), the monitors it
// drives (GDI names), and whether the running streamer captures one of
// them.
type GPUInfo struct {
	Name      string   `json:"name"`
	Vendor    string   `json:"vendor"`
	Monitors  []string `json:"monitors"`
	Streaming bool     `json:"streaming"`
}

// NvidiaPowerModes are the NVIDIA Control Panel's power management modes
// as the streamers' nvidia_power_mode takes them: "max" (prefer maximum
// performance), "consistent", "adaptive", "optimal" (optimal power), and
// "driver" (no override: the driver's global setting applies).
var NvidiaPowerModes = []string{"max", "consistent", "adaptive", "optimal", "driver"}

// NvidiaPowerModeValue is NvidiaPowerMode, defaulting to "max" -- or to
// "driver" when the older NvidiaMaxPerformance switch was turned off.
func (c Config) NvidiaPowerModeValue() string {
	for _, m := range NvidiaPowerModes {
		if c.NvidiaPowerMode == m {
			return m
		}
	}
	if c.NvidiaMaxPerformance != nil && !*c.NvidiaMaxPerformance {
		return "driver"
	}
	return "max"
}

// TLSEnabledOK is true unless the user turned the "Enable HTTPS" checkbox
// off (see the HTTP Listen Address dialog). Omitted YAML (nil) is on,
// matching StreamerAutoUpdateEnabled's identical pattern.
func (c Config) TLSEnabledOK() bool {
	return c.TLSEnabled == nil || *c.TLSEnabled
}

// WebRTCSignalRelayEnabledOK is true unless the user explicitly turned the
// outbound signaling-relay connection off. Omitted YAML (nil) is on, same
// pattern as TLSEnabledOK -- the actual pro/enterprise tier gate happens
// server-side (see webrtcSignalRelayWatchdog), this only controls whether a
// tier-eligible box dials out at all.
func (c Config) WebRTCSignalRelayEnabledOK() bool {
	return c.WebRTCSignalRelayEnabled == nil || *c.WebRTCSignalRelayEnabled
}

// RemoteWindowLockEnabled is true only when the user turned the General
// Settings checkbox on. Omitted YAML (nil) is off.
func (c Config) RemoteWindowLockEnabled() bool {
	return c.RemoteWindowLock != nil && *c.RemoteWindowLock
}

// USBBrokerConsentGiven is true only once the user has explicitly clicked
// the USB status row's enable button (App.EnableUSBBroker). Omitted YAML
// (nil) is false -- the closed usb-broker binary stays un-downloaded and
// un-started until this flips, regardless of license tier.
func (c Config) USBBrokerConsentGiven() bool {
	return c.USBBrokerConsent != nil && *c.USBBrokerConsent
}

func Load(path string) (Config, error) {
	cfg := Default()
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return finalize(cfg, "")
		}
		return cfg, err
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, err
	}
	return finalize(cfg, path)
}

func Save(path string, cfg Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func GenerateSecureToken() (string, error) {
	randomBytes := make([]byte, 24)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(randomBytes)
	if len(token) != 32 {
		return "", errors.New("unexpected token length")
	}
	return token, nil
}

func (c Config) EnsureState() error {
	return os.MkdirAll(c.StateDir, 0o755)
}

// DirIsUsable reports whether this process can create and write dir.
// os.MkdirAll alone is not enough: on Windows it can fail with
// ERROR_ALREADY_EXISTS ("Cannot create a file when that file already
// exists") against LocalSystem's profile (…\system32\config\systemprofile)
// when a later interactive user loads a config.yaml the service wrote next
// to the exe. A successful MkdirAll of an existing-but-unwritable directory
// is also possible, so we probe with a throwaway file.
func DirIsUsable(dir string) bool {
	if strings.TrimSpace(dir) == "" {
		return false
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false
	}
	probe := filepath.Join(dir, ".write-probe")
	f, err := os.Create(probe)
	if err != nil {
		return false
	}
	_ = f.Close()
	_ = os.Remove(probe)
	return true
}

func defaultStateDir() string {
	if base, err := os.UserConfigDir(); err == nil && strings.TrimSpace(base) != "" {
		return filepath.Join(base, "usbridge-agent")
	}
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		if runtime.GOOS == "darwin" {
			return filepath.Join(home, "Library", "Application Support", "usbridge-agent")
		}
		return filepath.Join(home, ".config", "usbridge-agent")
	}
	return filepath.Join(".", "var")
}

func resolvePaths(cfg Config, cfgPath string) Config {
	if strings.TrimSpace(cfgPath) == "" {
		return cfg
	}
	defaults := Default()
	configDir := filepath.Dir(cfgPath)

	if strings.TrimSpace(cfg.StateDir) == "" || cfg.StateDir == "./var" {
		cfg.StateDir = defaults.StateDir
	} else if !filepath.IsAbs(cfg.StateDir) {
		cfg.StateDir = filepath.Clean(filepath.Join(configDir, cfg.StateDir))
	}

	return cfg
}

func finalize(cfg Config, cfgPath string) (Config, error) {
	return resolvePaths(cfg, cfgPath), nil
}
