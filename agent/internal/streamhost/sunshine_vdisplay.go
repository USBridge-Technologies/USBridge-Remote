package streamhost

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"usbridge_agent/internal/displaypower"
)

// The virtual monitor for Sunshine and Vibepollo on Windows. Neither has one the agent
// can use (Sunshine has none; Vibepollo's is SudoVDA, and the agent keeps it off -- see
// Start), so the agent attaches the shared MttVDD monitor itself, the way RustShine does
// in-process: in the mode of the "virtual:WxH@HZ" output, made the primary display
// unless turned off, the only display if asked. It goes on right before the stream host
// starts and comes off when it stops, and the stream host's output_name is pointed at
// it. The display calls themselves are displaypower's, run in the user's session
// (runDisplayHelper).

// sunshineVirtualDisplay is the agent's own choice for the next virtual monitor, kept
// next to sunshine.conf (not in it: neither host knows these keys).
type sunshineVirtualDisplay struct {
	// Spec is "WxH@HZ" while a virtual display is the selected output, else "".
	Spec string `json:"spec,omitempty"`
	// Primary: make it the primary display. nil means yes, like RustShine's
	// virtual_display_primary.
	Primary *bool `json:"primary,omitempty"`
	// Exclusive: switch the other monitors off while it is attached.
	Exclusive bool `json:"exclusive,omitempty"`
}

func (v sunshineVirtualDisplay) primary() bool { return v.Primary == nil || *v.Primary }

func (b *sunshineBackend) virtualDisplayPath(name string) string {
	dir := b.sunshineDataDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, name)
}

// virtualDisplayConfigPath holds sunshineVirtualDisplay.
func (b *sunshineBackend) virtualDisplayConfigPath() string {
	return b.virtualDisplayPath("usbridge_virtual_display.json")
}

// virtualDisplayStatePath holds displaypower's VirtualState of the attached monitor:
// what to undo, even after a crash.
func (b *sunshineBackend) virtualDisplayStatePath() string {
	return b.virtualDisplayPath("usbridge_virtual_display_state.json")
}

func (b *sunshineBackend) loadVirtualDisplay() sunshineVirtualDisplay {
	var v sunshineVirtualDisplay
	if p := b.virtualDisplayConfigPath(); p != "" {
		if data, err := os.ReadFile(p); err == nil {
			_ = json.Unmarshal(data, &v)
		}
	}
	return v
}

func (b *sunshineBackend) saveVirtualDisplay(v sunshineVirtualDisplay) error {
	p := b.virtualDisplayConfigPath()
	if p == "" {
		return fmt.Errorf("no state dir")
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o644)
}

// VirtualDisplaySupported: Windows with the MttVDD driver installed.
func (b *sunshineBackend) VirtualDisplaySupported() bool {
	return runtime.GOOS == "windows" && virtualDriverInstalled()
}

// VirtualDisplayPrimary reports the saved "make the virtual monitor the primary display"
// choice (on unless turned off).
func (b *sunshineBackend) VirtualDisplayPrimary() bool { return b.loadVirtualDisplay().primary() }

// SetVirtualDisplayExclusive saves whether the next virtual monitor is the only display.
func (b *sunshineBackend) SetVirtualDisplayExclusive(exclusive bool) error {
	v := b.loadVirtualDisplay()
	v.Exclusive = exclusive
	return b.saveVirtualDisplay(v)
}

// SetVirtualDisplayPrimary saves the choice for the next virtual monitor and applies it
// to the attached one right away. live reports whether one was attached.
func (b *sunshineBackend) SetVirtualDisplayPrimary(primary bool) (live bool, err error) {
	v := b.loadVirtualDisplay()
	v.Primary = &primary
	if err := b.saveVirtualDisplay(v); err != nil {
		return false, err
	}
	state := b.virtualDisplayStatePath()
	if runtime.GOOS != "windows" || displaypower.LoadState(state) == nil {
		return false, nil
	}
	res, err := runDisplayHelper("primary", state, boolArg(primary))
	if err != nil {
		return true, err
	}
	if res.Error != "" {
		return true, errors.New(res.Error)
	}
	return true, nil
}

func boolArg(v bool) string {
	if v {
		return "1"
	}
	return "0"
}

// applyVirtualDisplayLocked runs right before the stream host launches (b.mu held): it
// takes off whatever monitor a previous run left attached, then attaches the selected
// one and points output_name at it. Failing to attach is not fatal: the host then
// streams whatever output_name already names, as RustShine falls back to its
// configured monitor.
func (b *sunshineBackend) applyVirtualDisplayLocked() {
	if runtime.GOOS != "windows" {
		return
	}
	state := b.virtualDisplayStatePath()
	if state == "" {
		return
	}
	v := b.loadVirtualDisplay()
	if v.Spec == "" {
		if displaypower.LoadState(state) == nil {
			return
		}
		if res, err := runDisplayHelper("apply", state); err != nil || res.Error != "" {
			log.Printf("[sunshine] taking the virtual display off: %v %s", err, res.Error)
		}
		return
	}
	if !virtualDriverInstalled() {
		log.Printf("[sunshine] virtual display %s selected but the MttVDD driver is not installed -- streaming the configured monitor", v.Spec)
		return
	}
	res, err := runDisplayHelper("apply", state, v.Spec, boolArg(v.primary()), boolArg(v.Exclusive))
	if err != nil || res.State == nil || res.State.GDIName == "" {
		log.Printf("[sunshine] virtual display %s not attached (%v %s) -- streaming the configured monitor", v.Spec, err, res.Error)
		return
	}
	if res.Error != "" {
		log.Printf("[sunshine] virtual display %s: %s", v.Spec, res.Error)
	}
	st := res.State
	log.Printf("[sunshine] virtual display attached: %s %q in %s (primary=%v exclusive=%v)", st.GDIName, st.FriendlyName, st.Mode, v.primary(), v.Exclusive)
	if err := b.setConfigKey("output_name", b.virtualOutputName(st)); err != nil {
		log.Printf("[sunshine] could not point output_name at the virtual display: %v", err)
	}
}

// virtualOutputName is what output_name has to say for the host to capture the virtual
// monitor. Vibepollo maps a friendly name to the display itself. Sunshine takes only its
// own device_id, which the agent can read back from Sunshine's log (it lists every
// monitor, attached or not, at startup), so any run after the first has it; until then
// "" -- Sunshine's own pick, the first output of the adapter showing the primary
// display, which is the virtual monitor when it is primary.
func (b *sunshineBackend) virtualOutputName(st *displaypower.VirtualState) string {
	if b.vibepollo {
		return st.FriendlyName
	}
	for _, d := range b.ListCaptureDevices() {
		if st.FriendlyName != "" && strings.EqualFold(d.DisplayName, st.FriendlyName) {
			return d.OutputName
		}
	}
	return ""
}

// releaseVirtualDisplayLocked runs after the stream host stopped (b.mu held): the
// virtual monitor comes off and the desktop goes back to how it was.
func (b *sunshineBackend) releaseVirtualDisplayLocked() {
	if runtime.GOOS != "windows" {
		return
	}
	state := b.virtualDisplayStatePath()
	if state == "" || displaypower.LoadState(state) == nil {
		return
	}
	if res, err := runDisplayHelper("apply", state); err != nil || res.Error != "" {
		log.Printf("[sunshine] taking the virtual display off: %v %s", err, res.Error)
		return
	}
	log.Printf("[sunshine] virtual display detached, desktop restored")
}

// virtualDriverInstalled reports whether the MttVDD driver is there; set on Windows
// (vdisplay.DriverInstalled).
var virtualDriverInstalled = func() bool { return false }

// sessionDisplayHelperRun runs the display helper in the active console session; set on
// Windows (the service lives in session 0, where display changes are refused).
var sessionDisplayHelperRun func(exe string, args []string) ([]byte, error)

// runDisplayHelper runs `<agent exe> vdisplay-helper args...` and returns its result. A
// package var so tests can stand in for it.
var runDisplayHelper = func(args ...string) (displaypower.HelperResult, error) {
	exe, err := os.Executable()
	if err != nil {
		return displaypower.HelperResult{}, err
	}
	full := append([]string{displaypower.HelperArg}, args...)
	var out []byte
	if useSunshineSessionBroker() && sessionDisplayHelperRun != nil {
		out, err = sessionDisplayHelperRun(exe, full)
	} else {
		cmd := exec.Command(exe, full...)
		configureProcess(cmd)
		out, err = cmd.CombinedOutput()
	}
	res, perr := displaypower.ParseHelperOutput(out)
	if perr != nil {
		if err != nil {
			return res, fmt.Errorf("%v: %v", err, perr)
		}
		return res, perr
	}
	return res, nil
}
