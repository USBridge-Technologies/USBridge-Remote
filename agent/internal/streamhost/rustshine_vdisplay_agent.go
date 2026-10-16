package streamhost

import (
	"errors"
	"log"
	"path/filepath"
	"runtime"

	"usbridge_agent/internal/displaypower"
)

// RustShine's virtual monitor on Windows is the agent's, as it is for Sunshine and
// Vibepollo (sunshine_vdisplay.go): the agent attaches the MttVDD monitor right before
// the streamer starts, makes it primary unless turned off, takes it off again once the
// streamer stops, and switches the other monitors off and on around sessions
// (app.virtualOthersWatchdog). The streamer manages no monitor -- its own
// `virtual_display` key stays empty -- and only captures the one the agent names in
// `monitor_device`, by GDI device name, so it stays on it while the others are switched
// off or on underneath (that renumbers the outputs `monitor_index` counts).

// rustshineAgentVirtualKey keeps the selected virtual monitor's "WxH@HZ" in
// sunshine.conf under a key the streamer doesn't know.
const rustshineAgentVirtualKey = "usbridge_virtual_display"

// agentVirtualDisplays: the agent attaches RustShine's virtual monitor itself (Windows;
// elsewhere the streamer still does: vkms on Linux, CGVirtualDisplay on macOS).
func agentVirtualDisplays() bool { return runtime.GOOS == "windows" }

// virtualSpec is the selected virtual monitor's "WxH@HZ", or "".
func (b *rustshineBackend) virtualSpec() string {
	if agentVirtualDisplays() {
		return b.ConfigKey(rustshineAgentVirtualKey)
	}
	return b.ConfigKey("virtual_display")
}

// setVirtualSpec selects a virtual monitor ("WxH@HZ") or none ("").
func (b *rustshineBackend) setVirtualSpec(spec string) error {
	if agentVirtualDisplays() {
		_ = b.SetConfigKey("virtual_display", "")
		return b.SetConfigKey(rustshineAgentVirtualKey, spec)
	}
	return b.SetConfigKey("virtual_display", spec)
}

// virtualStatePath holds displaypower's VirtualState of the attached monitor: what to
// undo, even after a crash.
func (b *rustshineBackend) virtualStatePath() string {
	p := b.ConfigPath()
	if p == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(p), "usbridge_virtual_display_state.json")
}

// applyAgentVirtualDisplayLocked runs right before the streamer launches (b.mu held):
// it takes off whatever monitor a previous run left attached, attaches the selected
// one and names it to the streamer. Failing to attach is not fatal: the streamer then
// captures monitor_index, as for a physical monitor.
func (b *rustshineBackend) applyAgentVirtualDisplayLocked() {
	if !agentVirtualDisplays() {
		return
	}
	// A virtual_display the streamer would attach itself, saved before the agent took
	// this over, moves to the agent's key.
	if legacy := b.ConfigKey("virtual_display"); legacy != "" {
		if err := b.setVirtualSpec(legacy); err != nil {
			log.Printf("[rustshine] moving virtual_display %s to the agent: %v", legacy, err)
		}
	}
	state := b.virtualStatePath()
	if state == "" {
		return
	}
	spec := b.virtualSpec()
	name := func(gdi string) {
		if err := b.SetConfigKey("monitor_device", gdi); err != nil {
			log.Printf("[rustshine] could not name the monitor to capture: %v", err)
		}
	}
	if spec == "" {
		if displaypower.LoadState(state) != nil {
			if res, err := runDisplayHelper("apply", state); err != nil || res.Error != "" {
				log.Printf("[rustshine] taking the virtual display off: %v %s", err, res.Error)
			}
		}
		name("")
		return
	}
	if !virtualDriverInstalled() {
		log.Printf("[rustshine] virtual display %s selected but the MttVDD driver is not installed -- streaming monitor_index", spec)
		name("")
		return
	}
	res, err := runDisplayHelper("apply", state, spec, boolArg(b.VirtualDisplayPrimary()), "0")
	if err != nil || res.State == nil || res.State.GDIName == "" {
		log.Printf("[rustshine] virtual display %s not attached (%v %s) -- streaming monitor_index", spec, err, res.Error)
		name("")
		return
	}
	if res.Error != "" {
		log.Printf("[rustshine] virtual display %s: %s", spec, res.Error)
	}
	st := res.State
	log.Printf("[rustshine] virtual display attached: %s %q in %s (primary=%v)", st.GDIName, st.FriendlyName, st.Mode, b.VirtualDisplayPrimary())
	name(st.GDIName)
	// Its index too, for a streamer from before monitor_device (it reads
	// monitor_index only): right at start, that is where it is.
	if idx := rustshineMonitorIndexOf(b, st.GDIName); idx != "" {
		if err := b.SetConfigKey("monitor_index", idx); err != nil {
			log.Printf("[rustshine] could not point monitor_index at the virtual display: %v", err)
		}
	}
}

// rustshineMonitorIndexOf is the streamer's monitor_index of the output with this GDI
// name, or ""; set on Windows (rustshine_devices_windows.go).
var rustshineMonitorIndexOf = func(b *rustshineBackend, gdi string) string { return "" }

// releaseAgentVirtualDisplayLocked runs after the streamer stopped (b.mu held): the
// virtual monitor comes off and the desktop goes back to how it was.
func (b *rustshineBackend) releaseAgentVirtualDisplayLocked() {
	if !agentVirtualDisplays() {
		return
	}
	state := b.virtualStatePath()
	if state == "" || displaypower.LoadState(state) == nil {
		return
	}
	if res, err := runDisplayHelper("apply", state); err != nil || res.Error != "" {
		log.Printf("[rustshine] taking the virtual display off: %v %s", err, res.Error)
		return
	}
	log.Printf("[rustshine] virtual display detached, desktop restored")
}

// setAgentVirtualDisplayPrimary applies "make it the primary display" to the attached
// monitor. live reports whether one was attached.
func (b *rustshineBackend) setAgentVirtualDisplayPrimary(primary bool) (live bool, err error) {
	state := b.virtualStatePath()
	if state == "" || displaypower.LoadState(state) == nil {
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
