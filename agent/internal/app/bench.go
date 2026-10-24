package app

import (
	"fmt"
	"log"
	"runtime"
	"strings"

	"usbridge_agent/internal/account"
	"usbridge_agent/internal/benchvideo"
	"usbridge_agent/internal/displaypower"
	"usbridge_agent/internal/forkrelease"
	"usbridge_agent/internal/monitors"
	"usbridge_agent/internal/streamhost"
)

// BenchPlayer is the streamer benchmark's host-side video player (see
// internal/benchvideo), created on first use.
func (a *App) BenchPlayer() *benchvideo.Player {
	a.benchOnce.Do(func() {
		a.benchPlayer = benchvideo.New(a.cfg.StateDir)
	})
	return a.benchPlayer
}

// BenchStreamBackends reports the active stream backend and which ones a
// benchmark can switch to right now.
func (a *App) BenchStreamBackends() (active string, available []string) {
	available = []string{"sunshine"}
	if a.rustshineStaged() {
		available = append(available, "rustshine")
	}
	if streamhost.PunktfunkAvailable(a.exeDir) {
		available = append(available, "punktfunk")
	}
	if forkrelease.VibepolloStaged(a.cfg.StateDir) {
		available = append(available, "vibepollo")
	}
	return a.currentStreamKind(), available
}

// BenchHostGPU names this host's GPU, for the client's Net Graph HUD (which
// shows both ends of the stream: this plus the client's own decode GPU, see
// usbridge-client's net_graph.go netGraphGPUNameFn). Reuses
// account.CollectDeviceInfo's existing platform detection (cached after its
// first call, shared with the device-code login telemetry) instead of
// duplicating it.
func (a *App) BenchHostGPU() string {
	return account.CollectDeviceInfo().GPU
}

// BenchVideoOutput is the virtual display the active streamer streams
// instead of a monitor, by name prefix ("" for none) -- where the
// benchmark's test video has to be moved for it to be in the picture.
func (a *App) BenchVideoOutput() string {
	if v, ok := a.stream.(interface{ VirtualOutputPrefix() string }); ok {
		return v.VirtualOutputPrefix()
	}
	return ""
}

// BenchVirtualID is the benchmark's "virtual display" pick in place of a monitor
// (Windows): every streamer is pinned to benchVirtualSpec, so the agent keeps the one
// virtual monitor up across the backend switches. Without it each streamer used its
// own saved pick, and a switch to one with a physical monitor took the virtual monitor
// down and put the other monitors back mid-benchmark -- the next two streamers started
// into that and failed.
const BenchVirtualID = "virtual"

const defaultBenchVirtualSpec = "virtual:1920x1080@60"

// BenchVirtualSpec is the "virtual:WxH@FPS" the virtual-display benchmark runs (or would
// run) at: the one pinned, else the virtual display picked for streaming now, else the default.
func (a *App) BenchVirtualSpec() string {
	a.streamMu.Lock()
	pinned := a.benchMonitor == BenchVirtualID
	spec := a.benchVirtualSpec
	a.streamMu.Unlock()
	if pinned && spec != "" {
		return spec
	}
	if cur := a.SunshineOutputName(); strings.HasPrefix(cur, "virtual:") {
		return cur
	}
	return defaultBenchVirtualSpec
}

// isVirtualMonitor: id is the GDI name of the virtual monitor on the desktop now. The
// client lists it like any monitor while it is up, so picking it must mean the same as
// picking BenchVirtualID -- pinned as a physical monitor, the streamers whose own pick is
// a physical one took the virtual monitor down at each backend switch.
func isVirtualMonitor(id string) bool {
	list, err := displaypower.ListInSession()
	if err != nil {
		return false
	}
	for _, m := range list {
		if m.Virtual && m.Active && m.GDIName == id {
			return true
		}
	}
	return false
}

// BenchMonitors lists the host's monitors for the benchmark's monitor pick,
// named after what the streamers themselves report where they do (Sunshine
// logs each monitor's model name; Windows alone mostly says "Generic PnP
// Monitor").
func (a *App) BenchMonitors() ([]monitors.Monitor, error) {
	list, err := monitors.List()
	if err != nil {
		return nil, err
	}
	names := map[string]string{}
	for _, d := range streamhost.NewSunshine(a.exeDir, a.cfg.StateDir, a.logPath).ListCaptureDevices() {
		if d.GDIName != "" && d.DisplayName != "" && d.DisplayName != d.GDIName {
			names[d.GDIName] = d.DisplayName
		}
	}
	for i := range list {
		if n := names[list[i].ID]; n != "" {
			list[i].Name = n
		}
	}
	if runtime.GOOS == "windows" {
		// Sized by the mode it will run in (what was picked at connect), not a fixed one.
		w, h := 1920, 1080
		if m, err := displaypower.ParseMode(strings.TrimPrefix(a.BenchVirtualSpec(), "virtual:")); err == nil {
			w, h = m.Width, m.Height
		}
		list = append(list, monitors.Monitor{ID: BenchVirtualID, Name: "Virtual display", Width: w, Height: h})
	}
	return list, nil
}

// BenchMonitor is the monitor the benchmark is pinned to, "" for none.
func (a *App) BenchMonitor() string {
	a.streamMu.Lock()
	defer a.streamMu.Unlock()
	return a.benchMonitor
}

// SetBenchMonitor pins both streamers to one monitor for the benchmark
// (id from BenchMonitors), or with "" puts each streamer's own pick back.
// The active streamer restarts when its monitor changes; the other one
// picks the change up on its next start (SetStreamBackend). Nothing is
// persisted to the agent config: this is only for the benchmark's run.
//
// deferRestart leaves a running streamer alone and has the next
// SetStreamBackend restart it instead (see benchRestartPending): the
// benchmark switches backends right after pinning and right after
// releasing, so restarting here too meant a second full streamer start
// (~25 s for Sunshine) on each side of the benchmark, spent on a streamer
// that was about to be restarted or replaced anyway.
func (a *App) SetBenchMonitor(id string, deferRestart bool) error {
	if id != "" && id != BenchVirtualID && isVirtualMonitor(id) {
		id = BenchVirtualID
	}
	if id != "" && id != BenchVirtualID {
		list, err := monitors.List()
		if err != nil {
			return err
		}
		if _, ok := monitors.Find(list, id); !ok {
			return fmt.Errorf("no monitor %q", id)
		}
	}

	a.streamMu.Lock()
	defer a.streamMu.Unlock()
	if id == a.benchMonitor {
		return nil
	}
	a.benchMonitor = id
	if id == BenchVirtualID {
		// Keep the mode the virtual monitor is in now, so it isn't rebuilt at the pin.
		a.benchVirtualSpec = defaultBenchVirtualSpec
		if cur := a.SunshineOutputName(); strings.HasPrefix(cur, "virtual:") {
			a.benchVirtualSpec = cur
		}
	}
	if id == "" {
		return a.restoreBenchOutputs(deferRestart)
	}
	log.Printf("[bench] pinning the streamers to monitor %s", id)
	if a.stream == nil {
		return nil
	}
	changed, ok := a.applyBenchMonitor(a.stream, a.streamKind)
	if !ok {
		// Resolved on the next start instead (see SetStreamBackend).
		log.Printf("[bench] %s doesn't report monitor %s yet", a.streamKind, id)
		return nil
	}
	if changed && a.stream.Running() {
		if deferRestart {
			a.benchRestartPending = true
			return nil
		}
		return a.RestartSunshine()
	}
	return nil
}

// applyBenchMonitor points b's capture at the pinned benchmark monitor,
// remembering b's own pick first. ok is false when b doesn't (yet) report
// that monitor. Caller holds streamMu.
func (a *App) applyBenchMonitor(b streamhost.Backend, kind string) (changed, ok bool) {
	if a.benchMonitor == "" || b == nil {
		return false, true
	}
	var output string
	if a.benchMonitor == BenchVirtualID {
		output, ok = a.benchVirtualSpec, true
	}
	for _, d := range b.ListCaptureDevices() {
		if ok {
			break
		}
		if d.GDIName == a.benchMonitor {
			output, ok = d.OutputName, true
			break
		}
	}
	if !ok {
		return false, false
	}
	current := b.OutputName()
	if a.benchOrigOutput == nil {
		a.benchOrigOutput = map[string]string{}
	}
	if _, saved := a.benchOrigOutput[kind]; !saved {
		a.benchOrigOutput[kind] = current
	}
	if current == output {
		return false, true
	}
	if err := b.SetOutputName(output); err != nil {
		log.Printf("[bench] pinning %s to monitor %s: %v", kind, a.benchMonitor, err)
		return false, false
	}
	log.Printf("[bench] %s now captures monitor %s (output %q)", kind, a.benchMonitor, output)
	return true, true
}

// restoreBenchOutputs puts back every streamer's own monitor pick that the
// benchmark changed. Caller holds streamMu. With deferRestart the active
// streamer's restart is left to the next SetStreamBackend (see
// SetBenchMonitor).
func (a *App) restoreBenchOutputs(deferRestart bool) error {
	restartActive := false
	for kind, orig := range a.benchOrigOutput {
		b := a.stream
		if kind != a.streamKind || b == nil {
			switch kind {
			case "rustshine":
				b = streamhost.NewRustshine(a.exeDir, a.cfg.StateDir, a.logPath)
			case "punktfunk":
				b = streamhost.NewPunktfunk(a.exeDir, a.cfg.StateDir, a.logPath)
			default:
				b = streamhost.NewSunshine(a.exeDir, a.cfg.StateDir, a.logPath)
			}
		}
		if b.OutputName() == orig {
			continue
		}
		if err := b.SetOutputName(orig); err != nil {
			log.Printf("[bench] restoring %s's monitor %q: %v", kind, orig, err)
			continue
		}
		log.Printf("[bench] %s's monitor restored to %q", kind, orig)
		if b == a.stream && b.Running() {
			restartActive = true
		}
	}
	a.benchOrigOutput = nil
	if restartActive {
		if deferRestart {
			a.benchRestartPending = true
			return nil
		}
		return a.RestartSunshine()
	}
	return nil
}
