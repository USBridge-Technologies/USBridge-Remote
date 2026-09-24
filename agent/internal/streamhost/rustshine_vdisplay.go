package streamhost

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"time"
)

// virtualDisplayTeardownTimeout bounds the one-shot helper below. It only
// detaches a monitor (well under a second in practice), so hitting this
// means it hung -- Stop() must not hang with it.
const virtualDisplayTeardownTimeout = 10 * time.Second

// virtualDisplayTeardownNeeded reports whether Stop() has to run the
// streamer's `--teardown-virtual-display` helper after killing it. Windows
// only: Stop() hard-kills the streamer there (Job Object / TerminateProcess),
// which skips its own shutdown teardown. With SudoVDA that was harmless (the
// driver's watchdog drops a monitor nobody pings), but MttVDD -- the
// driver the agent installs -- has no watchdog, so without this every stop
// would leave an extra monitor on the user's desktop. Elsewhere the
// streamer either gets a real signal (Linux/macOS) or its virtual display
// dies with the process (macOS CGVirtualDisplay).
func virtualDisplayTeardownNeeded(goos string, launchedWithVirtualDisplay bool, bin string) bool {
	return goos == "windows" && launchedWithVirtualDisplay && bin != ""
}

// runVirtualDisplayTeardown runs `bin --teardown-virtual-display` to
// completion. A package var so tests can observe Stop()'s decision without
// launching anything.
var runVirtualDisplayTeardown = defaultVirtualDisplayTeardown

func defaultVirtualDisplayTeardown(bin string) error {
	args := []string{"--teardown-virtual-display"}
	dir := filepath.Dir(bin)
	var proc rustshineProcess
	if useSessionBroker() {
		// Same reason the streamer itself goes through the broker: display
		// changes only apply to the calling session's desktop, and the
		// service runs in session 0.
		p, err := sessionBrokerLaunch(bin, args, dir, nil, nil)
		if err != nil {
			return err
		}
		proc = p
	} else {
		cmd := exec.Command(bin, args...)
		configureRustshineProcess(cmd)
		cmd.Dir = dir
		if err := cmd.Start(); err != nil {
			return err
		}
		proc = execCmdProcess{cmd}
	}
	done := make(chan error, 1)
	go func() { done <- proc.Wait() }()
	select {
	case err := <-done:
		return err
	case <-time.After(virtualDisplayTeardownTimeout):
		_ = proc.Kill()
		return fmt.Errorf("--teardown-virtual-display did not finish within %s", virtualDisplayTeardownTimeout)
	}
}
