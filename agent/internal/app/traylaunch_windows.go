//go:build windows

package app

import (
	"log"
	"os"
	"sync"

	"usbridge_agent/internal/sessionlaunch"
)

// trayHelper tracks the tray-only helper process this service has launched
// into the active console session (see LaunchTrayHelperInActiveSession) --
// guards against launching a second copy into the same session on a
// spurious duplicate session-change notification, and clears itself once
// the tracked process actually exits so a later call (a fresh login, or a
// retry) can relaunch it.
var trayHelper struct {
	mu     sync.Mutex
	handle *sessionlaunch.Handle
	sessID uint32
}

// LaunchTrayHelperInActiveSession (re-)launches this same binary with
// --tray --attach adminSocketPath into whichever session is currently
// attached to the physical console, using the same session-broker
// mechanism gamestream-server's own capture process already relies on (see
// internal/sessionlaunch's package doc) -- necessary because a LocalSystem
// service is permanently confined to Session 0 and has no other way to
// show a tray icon in a real user's desktop at all. This is also why
// --attach exists (see StartOptions.Attach's doc comment): a plain
// autostart Registry/Task Scheduler entry would instead launch an
// unprivileged process whose own config-path discovery resolves to the
// interactive user's own profile, not LocalSystem's -- a completely
// different directory, so it could never find this service's admin socket
// on its own. Handing it the exact path this service already knows
// sidesteps that mismatch entirely.
//
// Safe to call repeatedly -- from service startup and from every
// WTS_SESSION_LOGON/WTS_CONSOLE_CONNECT notification, mirroring how
// NotifySessionChange reacts to the same events for the stream host: a
// no-op if a helper this function already launched is still alive in the
// currently active session.
func LaunchTrayHelperInActiveSession(adminSocketPath string) {
	sessID, ok := sessionlaunch.ActiveConsoleSessionID()
	if !ok {
		return
	}

	trayHelper.mu.Lock()
	if trayHelper.handle != nil && trayHelper.sessID == sessID {
		trayHelper.mu.Unlock()
		return
	}
	trayHelper.mu.Unlock()

	exe, err := os.Executable()
	if err != nil {
		log.Printf("[app] tray helper: could not resolve own executable path: %v", err)
		return
	}

	h, err := sessionlaunch.LaunchInActiveSession(exe, []string{"--tray", "--attach", adminSocketPath}, "", nil, nil, nil)
	if err != nil {
		if err == sessionlaunch.ErrNoActiveSession {
			return
		}
		log.Printf("[app] tray helper: launch into active session failed: %v", err)
		return
	}
	log.Printf("[app] tray helper launched into session %d (pid=%d)", sessID, h.Pid())

	trayHelper.mu.Lock()
	trayHelper.handle = h
	trayHelper.sessID = sessID
	trayHelper.mu.Unlock()

	// Clear the tracked handle once it actually exits (session ended, user
	// closed it via the tray Quit item, or it crashed) so the next
	// SessionChange event or retry relaunches it instead of treating a dead
	// PID as still-live.
	go func() {
		_, _ = h.Wait()
		trayHelper.mu.Lock()
		if trayHelper.handle == h {
			trayHelper.handle = nil
		}
		trayHelper.mu.Unlock()
	}()
}
