//go:build !windows

package app

import (
	"os"
	"os/signal"
	"syscall"
)

// A second launch signals the running GUI with SIGUSR1; it shows its window.
func watchRaiseRequests() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGUSR1)
	go func() {
		for range ch {
			raiseThisGUI()
		}
	}()
}

func raiseOtherGUI(pid int) {
	if pid > 0 {
		_ = syscall.Kill(pid, syscall.SIGUSR1)
	}
}
