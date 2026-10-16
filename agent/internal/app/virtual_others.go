package app

import (
	"context"
	"log"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"usbridge_agent/internal/config"
	"usbridge_agent/internal/displaypower"
	"usbridge_agent/internal/streamhost"
)

// "Only the virtual monitor" on a Windows host is the agent's own, for every stream
// host: whether the virtual monitor came from the stream host (RustShine attaches
// it itself) or from the agent (Sunshine, Vibepollo), the agent switches the other
// monitors off while a session streams it and back on once the session is over, so
// a client that quits leaves the host's screens as they were. What to put back is
// kept in a state file, so it survives the agent or the stream host dying in between
// (RustShine did this in-process before, and an agent restart that killed it left
// the screens off).

// virtualOthersGrace is how long a session may be gone before the monitors come
// back: a stream reconnect (a codec or audio device switch restarts the stream
// host) shouldn't flash them on and off.
const virtualOthersGrace = 10 * time.Second

// virtualOthersRecheck is how often a session checks the others are still off (each
// check is a helper process in the user's session).
const virtualOthersRecheck = 10 * time.Second

// legacyVirtualExclusive is a stream host that kept this choice itself before.
type legacyVirtualExclusive interface {
	TakeLegacyVirtualExclusive() bool
}

func (a *App) virtualOthersStatePath() string {
	return filepath.Join(a.cfg.StateDir, "virtual_others_state.json")
}

// virtualOthersWanted: "only the virtual monitor" is on and the virtual monitor is
// the streamed output.
func (a *App) virtualOthersWanted() bool {
	return a.cfg.VirtualDisplayExclusive && strings.HasPrefix(a.SunshineOutputName(), "virtual:")
}

// virtualOthersWatchdog keeps the other monitors off while they should be and puts
// them back when not. Windows only; elsewhere it returns at once.
func (a *App) virtualOthersWatchdog(ctx context.Context) {
	if runtime.GOOS != "windows" {
		return
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

	state := a.virtualOthersStatePath()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	var idleSince, lastOff time.Time
	lastErr := ""
	run := func(arg string) {
		res, err := streamhost.RunDisplayHelper("others", state, arg)
		msg := res.Error
		if err != nil {
			msg = err.Error()
		}
		if msg != "" && msg != lastErr {
			log.Printf("[app] other monitors (%s): %s", map[string]string{"0": "off", "1": "back on"}[arg], msg)
		}
		lastErr = msg
	}
	restore := func() {
		if displaypower.LoadState(state) != nil {
			run("1")
			log.Printf("[app] the other monitors are back on")
		}
	}
	for {
		select {
		case <-ctx.Done():
			restore()
			return
		case <-ticker.C:
		}
		if !a.virtualOthersWanted() {
			// Turned off, or a physical monitor is streamed now: no reason to wait.
			idleSince = time.Time{}
			restore()
			continue
		}
		a.streamMu.Lock()
		stream := a.stream
		a.streamMu.Unlock()
		if stream != nil && stream.SessionActive() {
			idleSince = time.Time{}
			// Again every virtualOthersRecheck: a stream host restart re-attaches the
			// virtual monitor and Windows brings the others back with it. A no-op
			// while it is the only one, or before it is attached (then retried on
			// the next tick).
			if time.Since(lastOff) >= virtualOthersRecheck {
				run("0")
				if lastErr == "" && displaypower.LoadState(state) != nil {
					lastOff = time.Now()
				}
			}
			continue
		}
		lastOff = time.Time{}
		if idleSince.IsZero() {
			idleSince = time.Now()
		}
		if time.Since(idleSince) >= virtualOthersGrace {
			restore()
		}
	}
}
