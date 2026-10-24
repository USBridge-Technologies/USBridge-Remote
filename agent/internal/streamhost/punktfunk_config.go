package streamhost

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"usbridge_agent/internal/displaypower"
)

// punktfunkConfigDir is where this agent pins punktfunk-host's config
// directory (via PUNKTFUNK_CONFIG_DIR, set in Start -- punktfunk_backend.go)
// instead of its generic per-OS default ($HOME/.config/punktfunk,
// %ProgramData%\punktfunk). Same rationale as sunshineDataDir: an
// independently installed standalone Punktfunk on the same machine must
// never collide with this agent's own managed instance.
func (b *punktfunkBackend) punktfunkConfigDir() string {
	if b.stateDir == "" {
		return ""
	}
	return filepath.Join(b.stateDir, "punktfunk")
}

// ConfigPath returns where punktfunk-host keeps host-settings.json (the
// settings store SetConfigKey/ConfigKey read/write) -- see
// punktfunk-host's own settings_cli doc comment in main.rs ("settings set
// <ID> <VALUE> writes the console's settings store without a running host").
func (b *punktfunkBackend) ConfigPath() string {
	dir := b.punktfunkConfigDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "host-settings.json")
}

// LogPath: punktfunk-host's own internal log file path is not confirmed yet
// (only this backend's stdout/stderr capture, b.logPath, is wired -- see
// Start). "" until confirmed, same as CapExecPath off Linux elsewhere in
// this package -- callers already treat an empty path as "not applicable".
func (b *punktfunkBackend) LogPath() string { return "" }

// SetConfigKey writes one punktfunk-host setting via its own `settings set`
// CLI (confirmed command, see main.rs settings_cli) -- the only way to write
// host-settings.json documented as safe without a running host, since
// hand-editing it would race the host's own debounced writer. Per that same
// CLI's own doc comment, most settings only take effect after punktfunk-host
// restarts.
func (b *punktfunkBackend) SetConfigKey(key, value string) error {
	bin := b.binaryPath()
	if bin == "" {
		return nil
	}
	cmd := exec.Command(bin, "settings", "set", key, value)
	cmd.Env = append(os.Environ(), punktfunkConfigDirEnv+"="+b.punktfunkConfigDir())
	if out, err := cmd.CombinedOutput(); err != nil {
		log.Printf("[punktfunk] settings set %s failed: %v: %s", key, err, out)
		return err
	}
	return nil
}

// ConfigKey reads one value back from host-settings.json. "" if unset or
// the file doesn't exist yet (fresh install, or punktfunk-host never
// started).
func (b *punktfunkBackend) ConfigKey(key string) string {
	path := b.ConfigPath()
	if path == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var store map[string]json.RawMessage
	if json.Unmarshal(data, &store) != nil {
		return ""
	}
	raw, ok := store[key]
	if !ok {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	// Not a JSON string (bool/number) -- return the raw JSON text rather
	// than silently losing the value.
	return string(raw)
}

// punktfunkCaptureMonitorEnv pins punktfunk-host's capture at a physical
// monitor (its connector name, as `punktfunk-host list-monitors` prints it)
// instead of a per-session virtual display -- confirmed from punktfunk's
// configuration.md ("Linux: stream this physical monitor instead of a
// virtual display, overriding the console"). The host snapshots it at
// startup, so a change needs a restart, like the other backends' output_name.
const punktfunkCaptureMonitorEnv = "PUNKTFUNK_CAPTURE_MONITOR"

// outputNamePath is where the pinned monitor is kept between starts.
// Punktfunk has no setting for it in host-settings.json (only the env
// variable above and its console's own display policy), so it lives in a
// file of this backend's own next to that store.
func (b *punktfunkBackend) outputNamePath() string {
	dir := b.punktfunkConfigDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "usbridge-capture-monitor")
}

// punktfunkVirtualPrefix marks an output name that asks for a virtual
// display rather than a monitor -- the "virtual:WxH@FPS" spec the agent's
// virtual-display API hands every backend (see api.Server's
// virtualDisplayCreate). Punktfunk needs none of the spec: left unpinned it
// creates a display of its own for each session, at whatever mode that
// client asks for.
const punktfunkVirtualPrefix = "virtual:"

// SetOutputName picks what Punktfunk streams, effective on the next Start:
// a physical monitor by connector name (see ListCaptureDevices), mirrored as
// it is; a "virtual:" spec for Punktfunk's own per-session virtual display;
// or "" for Punktfunk's default, a virtual display of its own per session.
//
// A monitor has to be one Punktfunk itself lists. The other backends' names
// reach this through the same API -- RustShine's "/dev/dri/card1|HDMI-A-1",
// Sunshine's bare index -- and Punktfunk fails every session outright on a
// PUNKTFUNK_CAPTURE_MONITOR that matches nothing. The "card|connector" form
// is accepted for its connector.
func (b *punktfunkBackend) SetOutputName(name string) error {
	path := b.outputNamePath()
	if path == "" {
		return nil
	}
	name = strings.TrimSpace(name)
	if name == "" {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	if !strings.HasPrefix(name, punktfunkVirtualPrefix) {
		if _, connector, ok := strings.Cut(name, "|"); ok {
			name = connector
		}
		resolved, ok := punktfunkMonitorName(name)
		if !ok {
			return fmt.Errorf("punktfunk has no monitor %q", name)
		}
		name = resolved
		// Nothing listed means the compositor can't be asked right now
		// (agent started before the desktop); the name is taken on trust.
		if devices := b.ListCaptureDevices(); len(devices) > 0 {
			known := false
			for _, d := range devices {
				if d.OutputName == name {
					known = true
					break
				}
			}
			if !known {
				return fmt.Errorf("punktfunk has no monitor %q", name)
			}
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(name+"\n"), 0o644)
}

// punktfunkMonitorName is the name Punktfunk captures the monitor by. On Windows
// that is its GDI name (`\.\DISPLAYn`); the API hands every backend the other
// streamers' names for a monitor -- its friendly name (Sunshine, Vibepollo), its
// device path, its index -- and Punktfunk fails every session on one it doesn't
// know ("no monitor named ..."). ok is false for a name that matches no monitor of
// the desktop now. Anywhere else, or when the desktop can't be listed, the name is
// taken as it is.
// punktfunkListMonitors lists the desktop's monitors; a package var so tests can stand in.
var punktfunkListMonitors = displaypower.ListInSession

func punktfunkMonitorName(name string) (resolved string, ok bool) {
	if runtime.GOOS != "windows" || strings.HasPrefix(name, `\\.\`) {
		return name, true
	}
	all, err := punktfunkListMonitors()
	if err != nil {
		return name, true
	}
	var active []displaypower.Monitor
	for _, m := range all {
		if m.Active && !m.Virtual && m.GDIName != "" {
			active = append(active, m)
		}
	}
	if idx, err := strconv.Atoi(name); err == nil && idx >= 0 && idx < len(active) {
		return active[idx].GDIName, true
	}
	for _, m := range active {
		if strings.EqualFold(m.Name, strings.TrimSuffix(name, " (off)")) || strings.EqualFold(m.ID, name) {
			return m.GDIName, true
		}
	}
	return name, false
}

// punktfunkManagedOutputPrefix starts the name of every display Punktfunk
// creates on KWin ("Virtual-punktfunk", "Virtual-punktfunk-<id>") --
// MANAGED_PREFIX in its pf-vdisplay/kwin.rs.
const punktfunkManagedOutputPrefix = "Virtual-punktfunk"

// VirtualOutputPrefix is the name prefix of the display a session is
// streamed from when a virtual display is picked, "" when a monitor is
// mirrored. The benchmark plays its test video there (app.BenchVideoOutput).
func (b *punktfunkBackend) VirtualOutputPrefix() string {
	if strings.HasPrefix(b.OutputName(), punktfunkVirtualPrefix) {
		return punktfunkManagedOutputPrefix
	}
	return ""
}

// OutputName is the pinned monitor's connector name, "" for none.
func (b *punktfunkBackend) OutputName() string {
	path := b.outputNamePath()
	if path == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// SetExternalIP, SetBindAddress, SetCaptureMode, and SetAudioSink are not
// yet wired to real pf-host-config setting IDs -- those weren't researched
// (see agent/docs/PUNKTFUNK_BACKEND_TODO.md). No-ops rather than guessed IDs
// that could silently write the wrong setting: GameStream streaming still
// works with Punktfunk's own auto-detection (audio sink) in the meantime.
func (b *punktfunkBackend) SetExternalIP(ip string) error    { return nil }
func (b *punktfunkBackend) ExternalIP() string               { return "" }
func (b *punktfunkBackend) SetBindAddress(ip string) error   { return nil }
func (b *punktfunkBackend) BindAddress() string              { return "" }
func (b *punktfunkBackend) SetCaptureMode(mode string) error { return nil }
func (b *punktfunkBackend) CaptureMode() string              { return "" }
func (b *punktfunkBackend) SetAudioSink(sink string) error   { return nil }
func (b *punktfunkBackend) AudioSink() string                { return "" }
