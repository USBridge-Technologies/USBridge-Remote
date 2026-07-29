package streamhost

import (
	"bufio"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// ConfigPath returns the default sunshine.conf location Sunshine uses when
// launched without an explicit config argument (matches Sunshine's own
// platf::appdata() resolution: $HOME/.config/sunshine/sunshine.conf on Linux).
func (b *sunshineBackend) ConfigPath() string {
	if runtime.GOOS == "windows" {
		if b.windowsDir == "" {
			return ""
		}
		return filepath.Join(b.windowsDir, "config", "sunshine.conf")
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return ""
	}
	switch runtime.GOOS {
	case "linux":
		return filepath.Join(home, ".config", "sunshine", "sunshine.conf")
	case "darwin":
		return filepath.Join(home, ".config", "sunshine", "sunshine.conf")
	default:
		return ""
	}
}

// LogPath returns the default sunshine.log location Sunshine writes to
// alongside sunshine.conf (same appdata() dir, see ConfigPath) when launched
// without an explicit log path override. This also covers Windows: the
// portable build's config dir (windowsDir/config) holds both sunshine.conf
// and sunshine.log, so deriving from ConfigPath works there too.
func (b *sunshineBackend) LogPath() string {
	dir := filepath.Dir(b.ConfigPath())
	if dir == "" || dir == "." {
		return ""
	}
	return filepath.Join(dir, "sunshine.log")
}

// SetConfigKey upserts a single "key = value" line in sunshine.conf.
// An empty value removes the key (falling back to Sunshine's own default).
func (b *sunshineBackend) SetConfigKey(key, value string) error {
	return b.setConfigKey(key, value)
}

// setConfigKey upserts a single "key = value" line in sunshine.conf. An
// empty value removes the key (falling back to Sunshine's own default/auto
// behavior). The file is created if missing; other keys/values are
// preserved verbatim.
func (b *sunshineBackend) setConfigKey(key, value string) error {
	path := b.ConfigPath()
	if path == "" {
		return os.ErrNotExist
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	var lines []string
	if data, err := os.ReadFile(path); err == nil {
		scanner := bufio.NewScanner(strings.NewReader(string(data)))
		for scanner.Scan() {
			line := scanner.Text()
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, key) && strings.Contains(trimmed, "=") {
				parts := strings.SplitN(trimmed, "=", 2)
				if strings.TrimSpace(parts[0]) == key {
					continue // drop existing line, re-added below if needed
				}
			}
			lines = append(lines, line)
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	value = strings.TrimSpace(value)
	if value != "" {
		lines = append(lines, key+" = "+value)
	}

	content := strings.Join(lines, "\n")
	if content != "" {
		content += "\n"
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

// ConfigKey reads the current value of a "key = value" line from
// sunshine.conf, or "" if unset.
func (b *sunshineBackend) ConfigKey(key string) string {
	path := b.ConfigPath()
	if path == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, key) {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 && strings.TrimSpace(parts[0]) == key {
			return strings.TrimSpace(parts[1])
		}
	}
	return ""
}

// SetExternalIP sets the IP address Sunshine advertises to Moonlight clients
// via the external_ip key in sunshine.conf. Pass "" or "0.0.0.0" to remove
// the override (Sunshine auto-detects).
func (b *sunshineBackend) SetExternalIP(ip string) error {
	if ip == "" || ip == "0.0.0.0" {
		return b.setConfigKey("external_ip", "")
	}
	return b.setConfigKey("external_ip", ip)
}

// ExternalIP reads the current external_ip value from sunshine.conf, or ""
// if unset (auto-detect / all interfaces).
func (b *sunshineBackend) ExternalIP() string {
	return b.ConfigKey("external_ip")
}

// SetBindAddress sets (or removes) the bind_address key in sunshine.conf,
// restricting ALL Sunshine servers (web admin + streaming) to the given IP.
// Pass "" or "0.0.0.0" to remove the restriction (bind on all interfaces).
func (b *sunshineBackend) SetBindAddress(ip string) error {
	if ip == "" || ip == "0.0.0.0" {
		return b.setConfigKey("bind_address", "")
	}
	return b.setConfigKey("bind_address", ip)
}

// BindAddress reads the current bind_address from sunshine.conf, or "" if
// unset (Sunshine defaults to all interfaces).
func (b *sunshineBackend) BindAddress() string {
	return b.ConfigKey("bind_address")
}

// SetCaptureMode upserts the "capture" key in sunshine.conf. An empty mode
// removes the key (Sunshine auto-detects: portal on Wayland, x11 on X11).
func (b *sunshineBackend) SetCaptureMode(mode string) error {
	return b.setConfigKey("capture", mode)
}

// CaptureMode reads the current "capture" value from sunshine.conf, or ""
// if unset (auto-detect).
func (b *sunshineBackend) CaptureMode() string {
	return b.ConfigKey("capture")
}

// SetAudioSink upserts the "audio_sink" key in sunshine.conf — which system
// audio output device Sunshine captures from for GameStream. An empty sink
// removes the key (Sunshine falls back to the system default sink).
func (b *sunshineBackend) SetAudioSink(sink string) error {
	return b.setConfigKey("audio_sink", sink)
}

// AudioSink reads the current "audio_sink" value from sunshine.conf, or ""
// if unset (system default).
func (b *sunshineBackend) AudioSink() string {
	return b.ConfigKey("audio_sink")
}

// SetOutputName upserts the "output_name" key in sunshine.conf — which
// connected monitor Sunshine captures, addressed by Sunshine's own
// connected-output index (see display.Connectors). An empty name removes
// the key (Sunshine auto-picks the first output it finds).
func (b *sunshineBackend) SetOutputName(name string) error {
	return b.setConfigKey("output_name", name)
}

// OutputName reads the current "output_name" value from sunshine.conf, or
// "" if unset (auto-pick).
func (b *sunshineBackend) OutputName() string {
	return b.ConfigKey("output_name")
}
