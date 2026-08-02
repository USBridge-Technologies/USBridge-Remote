//go:build rustshine

package streamhost

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
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
//
// Switching to "kms" also auto-fills `adapter_name` with the first
// available /dev/dri/cardN if it isn't already set: gamestream-server's own
// --capture-device default is /dev/video0 (its V4L2/SBC default), which is
// never a valid KMS card path, so a bare `capture = kms` with no
// adapter_name would otherwise capture from a nonexistent device and fail
// outright. This only fills a gap — an adapter_name already set (e.g. via
// SetOutputName picking a specific card/connector) is left untouched.
func (b *rustshineBackend) SetCaptureMode(mode string) error {
	if err := b.SetConfigKey("capture", mode); err != nil {
		return err
	}
	if mode == "kms" && runtime.GOOS == "linux" && b.ConfigKey("adapter_name") == "" {
		if card := b.firstKmsCardPath(); card != "" {
			return b.SetConfigKey("adapter_name", card)
		}
	}
	return nil
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
// key like Sunshine. On Windows device selection is "monitor_index" (a
// numeric index) alone. On Linux/KMS it needs BOTH "adapter_name" (the
// /dev/dri/cardN a connector lives on — --capture-device; there is no
// default that works on a desktop, see SetCaptureMode) and "kms_connector"
// (the connector name), so ListCaptureDevices packs both into OutputName as
// "cardPath|connector" (see its doc comment) for this to split back apart.
func (b *rustshineBackend) SetOutputName(name string) error {
	if runtime.GOOS == "windows" {
		return b.SetConfigKey("monitor_index", name)
	}
	card, connector, ok := strings.Cut(name, "|")
	if !ok {
		// Not our own "cardPath|connector" format. A bare digit string is
		// never a real connector name (real ones look like "HDMI-A-1") —
		// it's Sunshine's numeric output_name scheme leaking through (e.g.
		// a client/GUI still holding a stale "drm:<index>" device path from
		// before it re-fetched the device list, or from a cross-backend
		// switch — see videoSetDevice's "raw:" prefix handling). Blindly
		// writing that digit string into kms_connector previously corrupted
		// capture permanently (no connector is ever named "0"), breaking
		// every future session regardless of fps/resolution until someone
		// edited the config file by hand. Resolve it against the real
		// device list instead, same index scheme rustshineBackend itself
		// reports via ListCaptureDevices; anything else that isn't our
		// compound format and isn't a resolvable index is ignored rather
		// than written verbatim.
		if idx, err := strconv.Atoi(name); err == nil {
			devices := b.ListCaptureDevices()
			if idx >= 0 && idx < len(devices) {
				name = devices[idx].OutputName
				card, connector, ok = strings.Cut(name, "|")
			}
		}
		if !ok {
			return nil
		}
	}
	if err := b.SetConfigKey("adapter_name", card); err != nil {
		return err
	}
	return b.SetConfigKey("kms_connector", connector)
}

func (b *rustshineBackend) OutputName() string {
	if runtime.GOOS == "windows" {
		return b.ConfigKey("monitor_index")
	}
	return b.ConfigKey("adapter_name") + "|" + b.ConfigKey("kms_connector")
}
