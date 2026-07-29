//go:build rustshine

package streamhost

import (
	"os"
	"path/filepath"
	"runtime"
)

// ConfigPath returns where this launcher keeps gamestream-server's config
// file. Unlike Sunshine, gamestream-server takes its config path as an
// explicit CLI positional argument (see Start) rather than looking one up
// itself, so this location is entirely our own choice — reusing the same
// per-OS directory Sunshine uses (~/.config/sunshine on Linux/macOS, a
// "config" dir next to the binary on Windows) only for consistency with the
// rest of this agent's file layout, not because gamestream-server expects it.
func (b *rustshineBackend) ConfigPath() string {
	if runtime.GOOS == "windows" {
		dir := filepath.Join(b.exeDir, "rustshine")
		return filepath.Join(dir, "config", "sunshine.conf")
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".config", "rustshine", "sunshine.conf")
}

// LogPath: this launcher's own stdout/stderr capture of gamestream-server
// (set via NewRustshine's logPath, same as Sunshine's), not a file
// gamestream-server itself manages.
func (b *rustshineBackend) LogPath() string { return b.logPath }

func (b *rustshineBackend) SetConfigKey(key, value string) error {
	return upsertConfigKey(b.ConfigPath(), key, value)
}

func (b *rustshineBackend) ConfigKey(key string) string {
	return readConfigKey(b.ConfigPath(), key)
}

// SetExternalIP/ExternalIP: gamestream-server has no external_ip/bind
// equivalent in its config file — its --local-ip flag is described as
// "cosmetic" (the confirmed source: the server always binds 0.0.0.0
// regardless). Persisted here as a config key anyway (under a key
// gamestream-server itself ignores) purely so the agent's own UI can
// still show/edit an advertised-IP value consistently across backends;
// it has no actual effect on gamestream-server's bind behavior.
func (b *rustshineBackend) SetExternalIP(ip string) error {
	return b.SetConfigKey("external_ip", ip)
}

func (b *rustshineBackend) ExternalIP() string {
	return b.ConfigKey("external_ip")
}

// SetBindAddress/BindAddress: see SetExternalIP — gamestream-server always
// binds 0.0.0.0; there is no bind_address equivalent to actually restrict it.
func (b *rustshineBackend) SetBindAddress(ip string) error {
	return b.SetConfigKey("bind_address", ip)
}

func (b *rustshineBackend) BindAddress() string {
	return b.ConfigKey("bind_address")
}

// SetCaptureMode/CaptureMode map to the "capture" key, values "v4l2"
// (default) or "kms" — confirmed key name, but different value set than
// Sunshine's ("portal"/"x11"/"kms").
func (b *rustshineBackend) SetCaptureMode(mode string) error {
	return b.SetConfigKey("capture", mode)
}

func (b *rustshineBackend) CaptureMode() string {
	return b.ConfigKey("capture")
}

// SetAudioSink/AudioSink map to the confirmed "audio_sink" key (PulseAudio
// sink name) — same key name as Sunshine's.
func (b *rustshineBackend) SetAudioSink(sink string) error {
	return b.SetConfigKey("audio_sink", sink)
}

func (b *rustshineBackend) AudioSink() string {
	return b.ConfigKey("audio_sink")
}

// SetOutputName/OutputName: gamestream-server has no single "output_name"
// key like Sunshine — device selection is either "kms_connector" (a
// connector name string, e.g. "DP-2", when capture=kms on Linux) or
// "monitor_index" (a numeric index, Windows only). This maps through to
// whichever applies for the current OS; ListCaptureDevices' CaptureDevice
// entries already carry the right value in OutputName for either case, so
// callers don't need to know which key is really being written.
func (b *rustshineBackend) SetOutputName(name string) error {
	switch runtime.GOOS {
	case "windows":
		return b.SetConfigKey("monitor_index", name)
	default:
		return b.SetConfigKey("kms_connector", name)
	}
}

func (b *rustshineBackend) OutputName() string {
	switch runtime.GOOS {
	case "windows":
		return b.ConfigKey("monitor_index")
	default:
		return b.ConfigKey("kms_connector")
	}
}
