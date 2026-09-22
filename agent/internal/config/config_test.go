package config

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDefault(t *testing.T) {
	cfg := Default()

	if cfg.AppName != "USBridge Agent" || cfg.ListenHost != "0.0.0.0" {
		t.Fatalf("Default() identity/listen fields = %q/%q", cfg.AppName, cfg.ListenHost)
	}
	if cfg.HTTPPort != 8080 || cfg.UsbPassthroughPort != 8090 || cfg.SunshinePort != 47990 {
		t.Fatalf("Default() ports = HTTP %d, USB %d, Sunshine %d", cfg.HTTPPort, cfg.UsbPassthroughPort, cfg.SunshinePort)
	}
	if !cfg.TailscaleEnabled || !cfg.ClipboardSyncEnabled {
		t.Fatal("Default() should enable Tailscale and clipboard sync")
	}
	if cfg.ClipboardMaxBytes != 200*1024*1024 {
		t.Fatalf("Default() ClipboardMaxBytes = %d", cfg.ClipboardMaxBytes)
	}
	if cfg.StateDir == "" || !filepath.IsAbs(cfg.StateDir) {
		t.Fatalf("Default() StateDir = %q, want an absolute path", cfg.StateDir)
	}
}

func TestEffectiveListenHost(t *testing.T) {
	tests := []struct {
		name string
		host string
		want string
	}{
		{name: "empty", want: "127.0.0.1"},
		{name: "whitespace", host: " \t\n", want: "127.0.0.1"},
		{name: "trimmed", host: " 192.0.2.10 ", want: "192.0.2.10"},
		{name: "wildcard", host: "0.0.0.0", want: "0.0.0.0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := (Config{ListenHost: tt.host}).EffectiveListenHost(); got != tt.want {
				t.Fatalf("EffectiveListenHost() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLoadMissingReturnsDefaults(t *testing.T) {
	got, err := Load(filepath.Join(t.TempDir(), "missing", "config.yaml"))
	if err != nil {
		t.Fatalf("Load(missing): %v", err)
	}
	if want := Default(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Load(missing) = %#v, want %#v", got, want)
	}
}

func TestLoadResolvesRelativeStateDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	data := []byte("app_name: Test Agent\nhttp_port: 9000\nstate_dir: data/state\nclipboard_sync_enabled: false\n")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.AppName != "Test Agent" || got.HTTPPort != 9000 || got.ClipboardSyncEnabled {
		t.Fatalf("Load did not apply YAML values: %#v", got)
	}
	wantStateDir := filepath.Join(filepath.Dir(path), "data", "state")
	if got.StateDir != wantStateDir {
		t.Fatalf("StateDir = %q, want %q", got.StateDir, wantStateDir)
	}
	if got.UsbPassthroughPort != Default().UsbPassthroughPort {
		t.Fatalf("unspecified default was lost: UsbPassthroughPort = %d", got.UsbPassthroughPort)
	}
}

func TestResolvePaths(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config", "config.yaml")
	defaultDir := Default().StateDir
	absoluteDir := filepath.Join(t.TempDir(), "state")
	tests := []struct {
		name  string
		path  string
		state string
		want  string
	}{
		{name: "no config path", state: "relative", want: "relative"},
		{name: "empty state", path: configPath, want: defaultDir},
		{name: "legacy default", path: configPath, state: "./var", want: defaultDir},
		{name: "relative", path: configPath, state: "data/../state", want: filepath.Join(filepath.Dir(configPath), "state")},
		{name: "absolute", path: configPath, state: absoluteDir, want: absoluteDir},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolvePaths(Config{StateDir: tt.state}, tt.path)
			if got.StateDir != tt.want {
				t.Fatalf("resolvePaths StateDir = %q, want %q", got.StateDir, tt.want)
			}
		})
	}
}

func TestSaveLoadRoundTripAndEnsureState(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "config.yaml")
	want := Default()
	want.AppName = "Round Trip"
	want.StateDir = filepath.Join(dir, "state", "nested")
	want.EntitlementToken = "signed-token"

	if err := Save(path, want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip = %#v, want %#v", got, want)
	}
	if err := got.EnsureState(); err != nil {
		t.Fatalf("EnsureState: %v", err)
	}
	if info, err := os.Stat(got.StateDir); err != nil || !info.IsDir() {
		t.Fatalf("state directory was not created: info=%v err=%v", info, err)
	}
}

func TestLoadRejectsMalformedYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("http_port: [not-an-int\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("Load(malformed YAML) returned nil error")
	}
}

func TestGenerateSecureToken(t *testing.T) {
	first, err := GenerateSecureToken()
	if err != nil {
		t.Fatalf("GenerateSecureToken: %v", err)
	}
	second, err := GenerateSecureToken()
	if err != nil {
		t.Fatalf("GenerateSecureToken (second): %v", err)
	}
	if len(first) != 32 {
		t.Fatalf("token length = %d, want 32", len(first))
	}
	decoded, err := base64.RawURLEncoding.DecodeString(first)
	if err != nil || len(decoded) != 24 {
		t.Fatalf("token is not 24-byte raw URL base64: length=%d err=%v", len(decoded), err)
	}
	if first == second {
		t.Fatal("two generated secure tokens unexpectedly matched")
	}
}
