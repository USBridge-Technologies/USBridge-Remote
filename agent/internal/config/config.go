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
	AppName          string `yaml:"app_name"`
	ListenHost       string `yaml:"listen_host"`
	HTTPPort         int    `yaml:"http_port"`
	TailscaleEnabled bool   `yaml:"tailscale_enabled"`
	NBDMountCommand  string `yaml:"nbd_mount_command"`
	StateDir         string `yaml:"state_dir"`
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
}

func Default() Config {
	return Config{
		AppName:          "USBridge Agent",
		ListenHost:       "0.0.0.0",
		HTTPPort:         8080,
		TailscaleEnabled: true,
		NBDMountCommand:  "",
		StateDir:         defaultStateDir(),
		SunshinePort:     47990,

		ClipboardSyncEnabled: true,
		ClipboardMaxBytes:    200 * 1024 * 1024,
	}
}

func (c Config) EffectiveListenHost() string {
	host := strings.TrimSpace(c.ListenHost)
	if host == "" {
		return "127.0.0.1"
	}
	return host
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
