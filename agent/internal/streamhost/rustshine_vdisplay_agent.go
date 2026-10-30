package streamhost

import (
	"log"
	"path/filepath"
	"runtime"

	"usbridge_agent/internal/displaypower"
)

// RustShine's virtual monitor on Windows is the agent's, as for every stream host: the
// agent brings the MttVDD monitor up for a session and takes it down after
// (app.virtualOthersWatchdog). The streamer manages no monitor -- its own
// `virtual_display` key stays empty -- and is only told, in `monitor_device`, the GDI
// device name the monitor has once up (displaypower.PlannedVirtual), which it captures
// by name whenever it is there: that survives the monitor coming and going and the
// others being switched off or on (which renumbers the outputs `monitor_index` counts).

// rustshineAgentVirtualKey keeps the selected virtual monitor's "WxH@HZ" in
// sunshine.conf under a key the streamer doesn't know.
const rustshineAgentVirtualKey = "usbridge_virtual_display"

// agentVirtualDisplays: the agent attaches RustShine's virtual monitor itself (Windows:
// MttVDD, Linux: vkms, macOS: a CGVirtualDisplay the agent process holds).
func agentVirtualDisplays() bool { return displaypower.AgentVirtual }

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
// it names the virtual monitor to the streamer when that is the selected output, and
// takes off a monitor an agent from before session-long virtual monitors left
// attached.
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
	b.releaseAgentVirtualDisplayLocked()
	spec := b.virtualSpec()
	name := func(gdi string) {
		if err := b.SetConfigKey("monitor_device", gdi); err != nil {
			log.Printf("[rustshine] could not name the monitor to capture: %v", err)
		}
	}
	if spec == "" {
		name("")
		return
	}
	if runtime.GOOS == "linux" {
		// The vkms connector, named before it is up: the streamer captures it
		// by card and connector once the agent brings it up for a session.
		connector, card, err := displaypower.PlannedVirtual()
		if err != nil {
			log.Printf("[rustshine] virtual display %s: %v -- streaming the configured monitor", spec, err)
			return
		}
		log.Printf("[rustshine] virtual display %s is %s %s (up for sessions only)", spec, card, connector)
		if err := b.SetConfigKey("adapter_name", card); err != nil {
			log.Printf("[rustshine] could not point adapter_name at the virtual display: %v", err)
		}
		if err := b.SetConfigKey("kms_connector", connector); err != nil {
			log.Printf("[rustshine] could not point kms_connector at the virtual display: %v", err)
		}
		return
	}
	if runtime.GOOS == "darwin" {
		// The CGVirtualDisplay's EDID identity: its display ID is new each time it
		// comes up, so the streamer finds it by this whenever a session opens.
		gdi, friendly, _ := displaypower.PlannedVirtual()
		log.Printf("[rustshine] virtual display %s is %q %s (up for sessions only)", spec, friendly, gdi)
		name(gdi)
		return
	}
	if !virtualDriverInstalled() {
		log.Printf("[rustshine] virtual display %s selected but the MttVDD driver is not installed -- streaming monitor_index", spec)
		name("")
		return
	}
	res, err := runDisplayHelper("vname", "-")
	if err != nil || res.State == nil || res.State.GDIName == "" {
		log.Printf("[rustshine] virtual display %s: no name for it (%v %s) -- streaming monitor_index", spec, err, res.Error)
		name("")
		return
	}
	log.Printf("[rustshine] virtual display %s is %s %q (up for sessions only)", spec, res.State.GDIName, res.State.FriendlyName)
	name(res.State.GDIName)
}

// rustshineMonitorIndexOf is the streamer's monitor_index of the output with this GDI
// name, or ""; set on Windows (rustshine_devices_windows.go).
var rustshineMonitorIndexOf = func(b *rustshineBackend, gdi string) string { return "" }

// releaseAgentVirtualDisplayLocked takes off a monitor an agent from before
// session-long virtual monitors left attached (its state file next to sunshine.conf).
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

// setAgentVirtualDisplayPrimary: the choice is saved by the caller; the agent applies
// it to the monitor up for a session (app.SetVirtualDisplayPrimary).
func (b *rustshineBackend) setAgentVirtualDisplayPrimary(primary bool) (live bool, err error) {
	return false, nil
}
