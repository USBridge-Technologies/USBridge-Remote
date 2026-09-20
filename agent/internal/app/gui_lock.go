package app

import (
	"log"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"usbridge_agent/internal/config"
	"usbridge_agent/internal/ui"
)

// guiLock is held for the life of the process once this launch became the
// state dir's one GUI (window or tray helper). Never closed: the OS drops it
// when the process dies.
var guiLock *os.File

func guiLockPath(stateDir string) string { return filepath.Join(stateDir, "gui.lock") }

// guiStateDir resolves the same state dir Start will use, without side effects
// beyond what Start itself does.
func guiStateDir() string {
	cfg, err := config.Load(resolveConfigPath())
	if err != nil {
		return ""
	}
	if !config.DirIsUsable(cfg.StateDir) {
		return config.Default().StateDir
	}
	return cfg.StateDir
}

// acquireGUILockForStart makes a normal/tray launch the state dir's only GUI.
// If another GUI already holds gui.lock it asks that process to come forward
// and returns false (the caller exits). Retries briefly so a self-update
// relaunch, which starts before its predecessor has fully exited, still gets
// the lock. Any lock trouble fails open: better a second window than none.
func acquireGUILockForStart() bool {
	dir := guiStateDir()
	if dir == "" {
		return true
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return true
	}
	path := guiLockPath(dir)
	deadline := time.Now().Add(6 * time.Second)
	for {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
		if err != nil {
			return true
		}
		ok, lerr := tryLockExclusive(f)
		if lerr != nil {
			_ = f.Close()
			return true
		}
		if ok {
			_ = f.Truncate(0)
			_, _ = f.WriteAt([]byte(strconv.Itoa(os.Getpid())), 0)
			guiLock = f
			watchRaiseRequests()
			return true
		}
		pid := readLockPID(f)
		_ = f.Close()
		if time.Now().After(deadline) {
			log.Printf("[app] another GUI (pid=%d) is already running -- asking it to come forward and exiting", pid)
			raiseOtherGUI(pid)
			return false
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// raiseThisGUI is what a raise request from a second launch runs.
func raiseThisGUI() { ui.RaiseMainWindow() }
