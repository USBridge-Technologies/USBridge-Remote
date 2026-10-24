package streamhost

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// The host's app list -- what a Moonlight client shows after connecting
// (Desktop, Steam Big Picture, a game) and what /launch starts. One list
// for every stream host this agent runs, kept as Sunshine's own apps.json
// in the agent's state directory (HostAppsPath):
//
//   - Sunshine, Apollo and Vibepollo read it as is ("file_apps=", see
//     sunshineConfigArgs), and an edit made in their own web UI lands in the
//     same file. They read it on start, so a change restarts them.
//   - RustShine reads the same file (its sunshine.conf's file_apps key) on
//     every /applist and /launch.
//   - punktfunk-host has its own apps.json format; writePunktfunkApps
//     converts the list into its config directory, which it also re-reads
//     per request.

// HostApp is one entry of Sunshine's apps.json. Fields the agent's dialog
// doesn't edit (a fork's own per-app settings such as Apollo's
// "virtual-display" or Vibepollo's "gamepad") are kept as they were in
// Extra and written back unchanged.
type HostApp struct {
	Name       string
	Cmd        string
	WorkingDir string
	Output     string
	Detached   []string
	PrepCmd    []HostAppPrepCmd
	ImagePath  string
	// Elevated runs the app with the logged-in user's administrator token
	// (Windows).
	Elevated             bool
	AutoDetach           bool
	WaitAll              bool
	ExitTimeout          int // seconds
	ExcludeGlobalPrepCmd bool
	Extra                map[string]json.RawMessage
}

// HostAppPrepCmd is a prep-cmd step: Do before the app starts, Undo after
// it ends.
type HostAppPrepCmd struct {
	Do       string `json:"do"`
	Undo     string `json:"undo"`
	Elevated bool   `json:"elevated,omitempty"`
}

// hostAppKeys are the keys HostApp models; everything else goes to Extra.
var hostAppKeys = []string{"name", "cmd", "working-dir", "output", "detached", "prep-cmd", "image-path",
	"elevated", "auto-detach", "wait-all", "exit-timeout", "exclude-global-prep-cmd"}

// NewHostApp is an entry with Sunshine's defaults for the fields its web
// UI presets.
func NewHostApp(name string) HostApp {
	return HostApp{Name: name, AutoDetach: true, WaitAll: true, ExitTimeout: 5}
}

// jsonFlag reads a boolean Sunshine wrote either as a JSON boolean or, in
// older versions, as a "true"/"false" string.
func jsonFlag(raw json.RawMessage, fallback bool) bool {
	var b bool
	if json.Unmarshal(raw, &b) == nil {
		return b
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return strings.EqualFold(strings.TrimSpace(s), "true")
	}
	return fallback
}

func (a *HostApp) UnmarshalJSON(data []byte) error {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		return err
	}
	*a = NewHostApp("")
	str := func(key string) string {
		var s string
		_ = json.Unmarshal(m[key], &s)
		return s
	}
	a.Name = str("name")
	a.Cmd = str("cmd")
	a.WorkingDir = str("working-dir")
	a.Output = str("output")
	a.ImagePath = str("image-path")
	_ = json.Unmarshal(m["detached"], &a.Detached)
	if raw, ok := m["prep-cmd"]; ok {
		var steps []map[string]json.RawMessage
		_ = json.Unmarshal(raw, &steps)
		for _, s := range steps {
			var step HostAppPrepCmd
			_ = json.Unmarshal(s["do"], &step.Do)
			_ = json.Unmarshal(s["undo"], &step.Undo)
			step.Elevated = jsonFlag(s["elevated"], false)
			a.PrepCmd = append(a.PrepCmd, step)
		}
	}
	a.Elevated = jsonFlag(m["elevated"], false)
	a.AutoDetach = jsonFlag(m["auto-detach"], true)
	a.WaitAll = jsonFlag(m["wait-all"], true)
	a.ExcludeGlobalPrepCmd = jsonFlag(m["exclude-global-prep-cmd"], false)
	if raw, ok := m["exit-timeout"]; ok {
		var n int
		if json.Unmarshal(raw, &n) != nil {
			var s string
			if json.Unmarshal(raw, &s) == nil {
				fmt.Sscanf(strings.TrimSpace(s), "%d", &n)
			}
		}
		a.ExitTimeout = n
	}
	for _, k := range hostAppKeys {
		delete(m, k)
	}
	if len(m) > 0 {
		a.Extra = m
	}
	return nil
}

func (a HostApp) MarshalJSON() ([]byte, error) {
	m := make(map[string]any, len(a.Extra)+len(hostAppKeys))
	for k, v := range a.Extra {
		m[k] = v
	}
	m["name"] = a.Name
	set := func(key, value string) {
		if value != "" {
			m[key] = value
		}
	}
	set("cmd", a.Cmd)
	set("working-dir", a.WorkingDir)
	set("output", a.Output)
	set("image-path", a.ImagePath)
	if len(a.Detached) > 0 {
		m["detached"] = a.Detached
	}
	if len(a.PrepCmd) > 0 {
		m["prep-cmd"] = a.PrepCmd
	}
	m["elevated"] = a.Elevated
	m["auto-detach"] = a.AutoDetach
	m["wait-all"] = a.WaitAll
	m["exit-timeout"] = a.ExitTimeout
	m["exclude-global-prep-cmd"] = a.ExcludeGlobalPrepCmd
	return json.Marshal(m)
}

// hostAppsFile is apps.json as a whole: the apps and the "env" map (and any
// other top-level key a fork keeps there), kept as read.
type hostAppsFile struct {
	Apps  []HostApp
	Other map[string]json.RawMessage
}

func parseHostAppsFile(data []byte) (hostAppsFile, error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		return hostAppsFile{}, err
	}
	var f hostAppsFile
	if raw, ok := m["apps"]; ok {
		if err := json.Unmarshal(raw, &f.Apps); err != nil {
			return hostAppsFile{}, err
		}
		delete(m, "apps")
	}
	f.Other = m
	return f, nil
}

func (f hostAppsFile) marshal() ([]byte, error) {
	m := make(map[string]any, len(f.Other)+2)
	for k, v := range f.Other {
		m[k] = v
	}
	if _, ok := m["env"]; !ok {
		m["env"] = map[string]string{}
	}
	apps := f.Apps
	if apps == nil {
		apps = []HostApp{}
	}
	m["apps"] = apps
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(m); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// HostAppsPath is the agent's apps.json, "" without a state directory.
func HostAppsPath(stateDir string) string {
	if stateDir == "" {
		return ""
	}
	return filepath.Join(stateDir, "apps.json")
}

// DefaultHostApps is the list before anything was saved: Sunshine's own
// first-run apps.json (Desktop, and Steam Big Picture where Sunshine adds
// it).
func DefaultHostApps() []HostApp {
	apps := []HostApp{NewHostApp("Desktop")}
	apps[0].ImagePath = "desktop.png"
	if runtime.GOOS == "windows" {
		steam := NewHostApp("Steam Big Picture")
		steam.Detached = []string{"steam://open/bigpicture"}
		steam.PrepCmd = []HostAppPrepCmd{{Undo: "steam://close/bigpicture"}}
		steam.ImagePath = "steam.png"
		apps = append(apps, steam)
	}
	return apps
}

// LoadHostApps reads the agent's app list; DefaultHostApps when it was
// never saved.
func LoadHostApps(stateDir string) ([]HostApp, error) {
	path := HostAppsPath(stateDir)
	if path == "" {
		return nil, fmt.Errorf("no state directory")
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return DefaultHostApps(), nil
	}
	if err != nil {
		return nil, err
	}
	f, err := parseHostAppsFile(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return f.Apps, nil
}

// SaveHostApps writes apps over the agent's list, keeping the file's env
// map and other top-level keys.
func SaveHostApps(stateDir string, apps []HostApp) error {
	path := HostAppsPath(stateDir)
	if path == "" {
		return fmt.Errorf("no state directory")
	}
	for i, a := range apps {
		if strings.TrimSpace(a.Name) == "" {
			return fmt.Errorf("app %d has no name", i+1)
		}
	}
	var f hostAppsFile
	if data, err := os.ReadFile(path); err == nil {
		if prev, err := parseHostAppsFile(data); err == nil {
			f.Other = prev.Other
		}
	}
	f.Apps = apps
	data, err := f.marshal()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// adoptHostAppsFile moves an apps.json a Sunshine fork kept in its own data
// directory (before the agent shared one list) to the shared path, once:
// the apps a user set up there stay.
func adoptHostAppsFile(stateDir, legacy string) {
	path := HostAppsPath(stateDir)
	if path == "" || legacy == "" {
		return
	}
	if _, err := os.Stat(path); err == nil {
		return
	}
	data, err := os.ReadFile(legacy)
	if err != nil {
		return
	}
	if _, err := parseHostAppsFile(data); err != nil {
		return
	}
	if err := os.WriteFile(path, data, 0o644); err == nil {
		log.Printf("[apps] adopted %s as the shared app list %s", legacy, path)
	}
}

// punktfunkApp is an entry of punktfunk-host's apps.json
// (gamestream/apps.rs): no working directory, detached commands or
// elevation of its own.
type punktfunkApp struct {
	ID    int             `json:"id"`
	Title string          `json:"title"`
	Cmd   string          `json:"cmd,omitempty"`
	Icon  string          `json:"icon,omitempty"`
	Prep  []punktfunkPrep `json:"prep,omitempty"`
}

type punktfunkPrep struct {
	Do   string `json:"do"`
	Undo string `json:"undo,omitempty"`
}

// punktfunkShellCommand makes a command Punktfunk can run through the shell. It runs
// a command with cmd.exe, which can't open a URL (steam://open/bigpicture: "'steam:'
// is not recognized as an internal or external command"); `start` hands it to the
// registered handler.
func punktfunkShellCommand(cmd string) string {
	cmd = strings.TrimSpace(cmd)
	if runtime.GOOS != "windows" {
		return cmd
	}
	if scheme, _, ok := strings.Cut(cmd, "://"); ok && len(scheme) > 1 && !strings.ContainsAny(scheme, " /\\\"&") {
		return fmt.Sprintf("start \"\" \"%s\"", cmd)
	}
	return cmd
}

// punktfunkApps converts the list: ids in list order, as RustShine numbers
// them; a detached-only entry (Steam Big Picture) runs its first detached
// command; a working directory becomes a cd in front of the command.
func punktfunkApps(apps []HostApp) []punktfunkApp {
	out := make([]punktfunkApp, 0, len(apps))
	for i, a := range apps {
		cmd := strings.TrimSpace(a.Cmd)
		if cmd == "" && len(a.Detached) > 0 {
			cmd = strings.TrimSpace(a.Detached[0])
		}
		cmd = punktfunkShellCommand(cmd)
		if cmd != "" && a.WorkingDir != "" {
			if runtime.GOOS == "windows" {
				cmd = fmt.Sprintf("cd /d \"%s\" && %s", a.WorkingDir, cmd)
			} else {
				cmd = fmt.Sprintf("cd '%s' && %s", strings.ReplaceAll(a.WorkingDir, "'", `'\''`), cmd)
			}
		}
		p := punktfunkApp{ID: i + 1, Title: a.Name, Cmd: cmd}
		switch {
		case cmd == "":
			p.Icon = "monitor"
		case strings.Contains(strings.ToLower(cmd), "steam"):
			p.Icon = "steam"
		}
		for _, s := range a.PrepCmd {
			if s.Do == "" && s.Undo == "" {
				continue
			}
			do := s.Do
			if do == "" {
				// punktfunk runs every step's do; an undo-only step's do is a no-op.
				do = "exit 0"
			}
			p.Prep = append(p.Prep, punktfunkPrep{Do: punktfunkShellCommand(do), Undo: punktfunkShellCommand(s.Undo)})
		}
		out = append(out, p)
	}
	return out
}

// writePunktfunkApps writes the agent's list (when one was ever saved or
// adopted) into punktfunk-host's config directory.
func writePunktfunkApps(stateDir, configDir string) error {
	path := HostAppsPath(stateDir)
	if path == "" || configDir == "" {
		return nil
	}
	if _, err := os.Stat(path); err != nil {
		return nil
	}
	apps, err := LoadHostApps(stateDir)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(punktfunkApps(apps), "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(configDir, "apps.json"), data, 0o644)
}

// HostAppsApplier is a backend that takes a changed app list without a
// restart, or says it needs one.
type HostAppsApplier interface {
	// ApplyHostApps hands the saved list to the stream host. restart: it
	// only reads the list on start.
	ApplyHostApps() (restart bool, err error)
}

// ApplyHostApps: Sunshine and its forks read apps.json on start only.
func (b *sunshineBackend) ApplyHostApps() (bool, error) { return true, nil }

// ApplyHostApps: RustShine re-reads the file per request once its
// sunshine.conf names it; naming it the first time needs a restart.
func (b *rustshineBackend) ApplyHostApps() (bool, error) {
	path := HostAppsPath(b.stateDir)
	if path == "" || b.ConfigKey("file_apps") == path {
		return false, nil
	}
	return true, b.SetConfigKey("file_apps", path)
}

// ApplyHostApps: punktfunk-host re-reads its apps.json per request.
func (b *punktfunkBackend) ApplyHostApps() (bool, error) {
	return false, writePunktfunkApps(b.stateDir, b.punktfunkConfigDir())
}
