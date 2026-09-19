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
	AppName            string `yaml:"app_name"`
	ListenHost         string `yaml:"listen_host"`
	HTTPPort           int    `yaml:"http_port"`
	UsbPassthroughPort int    `yaml:"usb_passthrough_port"`
	TailscaleEnabled   bool   `yaml:"tailscale_enabled"`
	NBDMountCommand    string `yaml:"nbd_mount_command"`
	StateDir           string `yaml:"state_dir"`
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
	// PreferredBackend is the user's own explicit choice once entitled:
	// "" (never linked / not entitled -- always Sunshine), "sunshine", or
	// "rustshine". Only meaningful together with a currently-valid
	// EntitlementToken; see App.applyPreferredBackend.
	PreferredBackend string `yaml:"preferred_backend,omitempty"`
	// RustShineWebRTCDisabled turns off gamestream-server's native WebRTC
	// signaling endpoint (--webrtc-disable) -- the surface USBridge's
	// browser/WASM web client connects through. Defaults to false (enabled,
	// matching gamestream-server's own default) so existing installs keep
	// the web client working without needing to opt in.
	RustShineWebRTCDisabled bool `yaml:"rustshine_webrtc_disabled,omitempty"`

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

// RemoteWindowLockEnabled is true only when the user turned the General
// Settings checkbox on. Omitted YAML (nil) is off.
func (c Config) RemoteWindowLockEnabled() bool {
	return c.RemoteWindowLock != nil && *c.RemoteWindowLock
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
