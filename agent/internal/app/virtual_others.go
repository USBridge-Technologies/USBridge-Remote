package app

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"usbridge_agent/internal/config"
	"usbridge_agent/internal/displaypower"
	"usbridge_agent/internal/streamhost"
)

// The virtual monitor on a Windows host is the agent's, for every stream host, and
// it exists for the length of a session only: it comes up when a session on it
// starts (made the primary display unless turned off, and the only one with "only
// the virtual monitor"), and once the session is over the other monitors come back,
// the primary role goes back and the virtual monitor goes away -- left attached, it
// stayed as an extra, primary screen new windows opened on. The stream hosts are
// only told its name (displaypower.PlannedVirtual) and attach nothing. What to undo
// is kept in a state file, so it survives the agent or the stream host dying.
//
// A session starts when the client says so before it starts its stream
// (ClientSessionStarting, so the monitor is there for the stream host's first
// frame), or, from an older client, when the stream host reports one. It ends when
// the client says so (ClientSessionEnded) or the stream host has reported none for
// virtualSessionGrace.

// virtualSessionGrace is how long a session may be gone, without the client saying
// it left (a dropped network, a crash), before the virtual monitor goes: a stream
// host restart (codec or audio device switch) has the client back within 1-3s.
const virtualSessionGrace = 3 * time.Second

// virtualSessionStartup is how long after the monitor came up the stream host may
// still report no session without that ending it: the client announces the session
// before it starts its stream, and a stream host takes seconds to report one
// (Punktfunk: ~6s -- the monitor went down under the starting stream).
const virtualSessionStartup = 30 * time.Second

// virtualOthersRecheck is how often a session checks the other monitors are still
// off (each check is a helper process in the user's session).
const virtualOthersRecheck = 10 * time.Second

// virtualStrayRecheck is how often, with no session, the agent checks the virtual
// monitor isn't attached (each check is a helper process in the user's session).
const virtualStrayRecheck = 30 * time.Second

// virtualMonitorOwner is a stream host that brings up its own virtual monitor
// (Punktfunk on Windows): the agent then leaves the displays alone and only hands it
// the "only the virtual monitor" choice (SetVirtualDisplayExclusive).
type virtualMonitorOwner interface {
	OwnsVirtualMonitor() bool
}

// streamOwnsVirtualMonitor reports whether the active stream host attaches its own
// virtual monitor.
func (a *App) streamOwnsVirtualMonitor() bool {
	a.streamMu.Lock()
	stream := a.stream
	a.streamMu.Unlock()
	o, ok := stream.(virtualMonitorOwner)
	return ok && o.OwnsVirtualMonitor()
}

// legacyVirtualExclusive is a stream host that kept "only the virtual monitor" itself
// before.
type legacyVirtualExclusive interface {
	TakeLegacyVirtualExclusive() bool
}

func (a *App) virtualSessionStatePath() string {
	return filepath.Join(a.cfg.StateDir, "virtual_session_state.json")
}

// virtualSpec is the streamed virtual monitor's "WxH@HZ", or "" for a physical one.
func (a *App) virtualSpec() string {
	spec, ok := strings.CutPrefix(a.SunshineOutputName(), "virtual:")
	if !ok {
		return ""
	}
	return spec
}

// ClientSessionEnded is the client saying its stream is over (it disconnects or
// quits): the virtual monitor goes and the other monitors come back at once.
func (a *App) ClientSessionEnded() {
	select {
	case a.clientLeft <- struct{}{}:
	default:
	}
}

// ClientSessionStarting is the client saying it is about to start its stream: the
// virtual monitor, when it is the one streamed, is brought up before this returns
// (or timeout passes), so the stream host captures it from the first frame.
func (a *App) ClientSessionStarting(timeout time.Duration) {
	done := make(chan struct{})
	select {
	case a.clientStarting <- done:
	case <-time.After(timeout):
		return
	}
	select {
	case <-done:
	case <-time.After(timeout):
	}
}

// virtualOthersWatchdog brings the virtual monitor up and down with the sessions on
// it. Windows only; elsewhere it only answers the client's calls.
func (a *App) virtualOthersWatchdog(ctx context.Context) {
	if runtime.GOOS != "windows" {
		for {
			select {
			case <-ctx.Done():
				return
			case done := <-a.clientStarting:
				close(done)
			case <-a.clientLeft:
			}
		}
	}
	a.streamMu.Lock()
	stream := a.stream
	a.streamMu.Unlock()
	if lb, ok := stream.(legacyVirtualExclusive); ok && lb.TakeLegacyVirtualExclusive() {
		a.cfg.VirtualDisplayExclusive = true
		if err := config.Save(a.cfgPath, a.cfg); err != nil {
			log.Printf("[app] only the virtual monitor: %v", err)
		}
		log.Printf("[app] only the virtual monitor: moved from the stream host's settings to the agent's")
	}

	// The other-monitors state of the agent before session-long virtual monitors;
	// the virtual monitor's own (taken off by the stream host backend) restores the desktop.
	_ = os.Remove(filepath.Join(a.cfg.StateDir, "virtual_others_state.json"))
	state := a.virtualSessionStatePath()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	var idleSince, lastOff, upAt, lastStrayCheck time.Time
	watcher := false
	// up: the spec the virtual monitor was brought up in, "" while it is down.
	up := ""
	if displaypower.LoadState(state) != nil {
		up = "?" // left up by an earlier run: taken down below unless a session needs it
	}
	// left: the client said it left. The stream host may report its session for a
	// moment longer; that no longer counts until it has reported none.
	left := false
	lastErr := ""
	run := func(what string, args ...string) bool {
		res, err := streamhost.RunDisplayHelper(args...)
		msg := res.Error
		if err != nil {
			msg = err.Error()
		}
		if msg != "" && msg != lastErr {
			log.Printf("[app] virtual monitor (%s): %s", what, msg)
		}
		lastErr = msg
		return msg == ""
	}
	bringUp := func(spec string) {
		if up == spec {
			return
		}
		primary, exclusive := a.VirtualDisplayPrimary(), a.cfg.VirtualDisplayExclusive
		ok := run("up", "apply", state, spec, boolArg(primary), boolArg(exclusive))
		if st := displaypower.LoadState(state); st != nil {
			if !watcher {
				// Puts the displays back should this process die mid-session and
				// stay down; one per agent run, it waits for this process to exit.
				if err := streamhost.StartDisplayWatcher(state, "USBridgeAgent"); err != nil {
					log.Printf("[app] virtual monitor watcher: %v", err)
				} else {
					watcher = true
				}
			}
			up = spec
			lastOff, upAt = time.Now(), time.Now()
			log.Printf("[app] session on the virtual monitor: %s %s up (primary=%v, others off=%v, ok=%v)", st.GDIName, spec, primary, exclusive, ok)
		}
	}
	// The other monitors around a stream host's own virtual monitor (Punktfunk).
	othersState := filepath.Join(a.cfg.StateDir, "virtual_owner_others_state.json")
	var ownerOffSince, ownerIdleSince time.Time
	ownerOthers := func(spec string, clientLeft bool) {
		a.streamMu.Lock()
		stream := a.stream
		a.streamMu.Unlock()
		active := stream != nil && stream.SessionActive()
		off := displaypower.LoadState(othersState) != nil
		if spec != "" && a.cfg.VirtualDisplayExclusive && active && !clientLeft {
			ownerIdleSince = time.Time{}
			// Again every virtualOthersRecheck; a no-op while the virtual monitor is
			// the only one, or before the stream host has attached it.
			if time.Since(ownerOffSince) >= virtualOthersRecheck {
				run("others off", "others", othersState, "0")
				if displaypower.LoadState(othersState) != nil {
					if !off {
						log.Printf("[app] session on the stream host's virtual monitor -- the other monitors are off")
					}
					ownerOffSince = time.Now()
				}
			}
			return
		}
		ownerOffSince = time.Time{}
		if !off {
			return
		}
		if !clientLeft && spec != "" && a.cfg.VirtualDisplayExclusive {
			if ownerIdleSince.IsZero() {
				ownerIdleSince = time.Now()
			}
			if time.Since(ownerIdleSince) < virtualSessionGrace {
				return
			}
		}
		ownerIdleSince = time.Time{}
		run("others back on", "others", othersState, "1")
		log.Printf("[app] the other monitors are back on")
	}
	takeDown := func(why string) {
		if up == "" && displaypower.LoadState(state) == nil {
			return
		}
		run("down", "apply", state)
		up = ""
		log.Printf("[app] %s -- the virtual monitor is down, the other monitors are back", why)
	}
	for {
		var starting chan struct{}
		select {
		case <-ctx.Done():
			takeDown("agent stopping")
			ownerOthers("", true)
			return
		case <-a.clientLeft:
			left = true
			idleSince = time.Time{}
			takeDown("the client left")
			if a.streamOwnsVirtualMonitor() {
				ownerOthers(a.virtualSpec(), true)
			}
			continue
		case starting = <-a.clientStarting:
			left = false
		case <-ticker.C:
		}
		spec := a.virtualSpec()
		if a.streamOwnsVirtualMonitor() {
			// The stream host attaches its own virtual monitor (Punktfunk): the agent
			// doesn't attach or detach it, but switches the other monitors off around
			// it -- Punktfunk's MttVDD path only makes it primary, its "exclusive"
			// topology is for its own driver.
			if starting != nil {
				close(starting)
			}
			takeDown("the stream host brings up its own virtual monitor")
			if left {
				a.streamMu.Lock()
				stream := a.stream
				a.streamMu.Unlock()
				if stream == nil || !stream.SessionActive() {
					left = false // the session the client left is gone
				}
			}
			ownerOthers(spec, left)
			continue
		}
		if starting == nil && up == "" && time.Since(lastStrayCheck) >= virtualStrayRecheck {
			a.streamMu.Lock()
			stream := a.stream
			a.streamMu.Unlock()
			if stream == nil || !stream.SessionActive() {
				lastStrayCheck = time.Now()
				if res, err := streamhost.RunDisplayHelper("list", "-"); err == nil && displaypower.StrayVirtual(res.Monitors) {
					run("detach", "apply", state)
					log.Printf("[app] the virtual monitor was attached with no session on it -- detached, the monitors are as before")
				}
			}
		}
		if starting != nil {
			if spec != "" {
				idleSince = time.Time{}
				bringUp(spec)
			}
			close(starting)
			continue
		}
		if spec == "" {
			// A physical monitor is streamed now: no reason to wait.
			idleSince = time.Time{}
			takeDown("a physical monitor is streamed")
			continue
		}
		a.streamMu.Lock()
		stream := a.stream
		a.streamMu.Unlock()
		active := stream != nil && stream.SessionActive()
		if left {
			if active {
				continue
			}
			left = false
		}
		if active {
			idleSince = time.Time{}
			if up != spec {
				// An older client, which doesn't say it starts.
				bringUp(spec)
				continue
			}
			// Again every virtualOthersRecheck: Windows can bring the others back
			// (a stream host restart). A no-op while the virtual one is the only one.
			if a.cfg.VirtualDisplayExclusive && time.Since(lastOff) >= virtualOthersRecheck {
				run("others off", "others", state, "0")
				lastOff = time.Now()
			}
			continue
		}
		if up == "" {
			continue
		}
		if time.Since(upAt) < virtualSessionStartup {
			// The stream is still starting; only the client saying it left ends it.
			idleSince = time.Time{}
			continue
		}
		if idleSince.IsZero() {
			idleSince = time.Now()
		}
		if time.Since(idleSince) >= virtualSessionGrace {
			takeDown("the session is over")
		}
	}
}

func boolArg(v bool) string {
	if v {
		return "1"
	}
	return "0"
}
