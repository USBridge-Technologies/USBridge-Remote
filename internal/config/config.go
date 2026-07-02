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

type TailscaleMode int

const (
	TailscaleModeUserspace TailscaleMode = iota
	TailscaleModeSystem
)

func (m TailscaleMode) MarshalYAML() (interface{}, error) {
	switch m {
	case TailscaleModeUserspace:
		return "userspace", nil
	case TailscaleModeSystem:
		return "system", nil
	default:
		return "system", nil
	}
}

func (m *TailscaleMode) UnmarshalYAML(value *yaml.Node) error {
	var s string
	if err := value.Decode(&s); err == nil {
		switch strings.ToLower(s) {
		case "userspace":
			*m = TailscaleModeUserspace
			return nil
		case "system":
			*m = TailscaleModeSystem
			return nil
		}
	}

	var i int
	if err := value.Decode(&i); err == nil {
		*m = TailscaleMode(i)
		return nil
	}

	*m = TailscaleModeSystem
	return nil
}

type Config struct {
	AppName          string        `yaml:"app_name"`
	ListenHost       string        `yaml:"listen_host"`
	HTTPPort         int           `yaml:"http_port"`
	TailscaleEnabled bool          `yaml:"tailscale_enabled"`
	TailscaleMode    TailscaleMode `yaml:"tailscale_mode"`
	NBDMountCommand  string        `yaml:"nbd_mount_command"`
	StateDir         string        `yaml:"state_dir"`
	// Moonlight/Sunshine protocol
	MasterKey    string `yaml:"master_key"`
	SunshinePort int    `yaml:"sunshine_port"`
	// SunshineCaptureMode selects Sunshine's Linux capture backend: "" (auto,
	// portal-based, no root), "portal" (explicit XDG desktop portal, no root),
	// or "kms" (direct KMS capture, requires CAP_SYS_ADMIN on the sunshine binary).
	SunshineCaptureMode string `yaml:"sunshine_capture_mode"`
}

func Default() Config {
	return Config{
		AppName:          "USBridge Agent",
		ListenHost:       "0.0.0.0",
		HTTPPort:         8080,
		TailscaleEnabled: true,
		TailscaleMode:    TailscaleModeSystem,
		NBDMountCommand:  "",
		StateDir:         defaultStateDir(),
		SunshinePort:     47990,
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
