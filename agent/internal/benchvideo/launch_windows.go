//go:build windows

package benchvideo

import (
	"errors"
	"fmt"
	"os/exec"

	"usbridge_agent/internal/sessionlaunch"
)

type cmdProcess struct{ cmd *exec.Cmd }

func (c cmdProcess) Kill() error { return c.cmd.Process.Kill() }
func (c cmdProcess) Wait() error { return c.cmd.Wait() }
func (c cmdProcess) Pid() int    { return c.cmd.Process.Pid }

type sessionProcess struct{ h *sessionlaunch.Handle }

func (s sessionProcess) Kill() error { return s.h.Kill() }
func (s sessionProcess) Pid() int    { return s.h.Pid() }
func (s sessionProcess) Wait() error {
	code, err := s.h.Wait()
	s.h.Close()
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("exit code %d", code)
	}
	return nil
}

// launch puts the player on the interactive desktop. The agent normally
// runs as a Session 0 service, where a window is invisible to both DXGI
// capture paths, so it goes through sessionlaunch first -- under the logged-in
// user's own token: the player is found in places users write to (winget's
// per-user packages, C:fmpeg), and a SYSTEM launch would have run whatever
// was put there as SYSTEM. An agent started by hand in the user's session
// lacks SeTcbPrivilege for that and just starts the player directly.
func launch(path string, args []string) (process, error) {
	h, err := sessionlaunch.LaunchAsActiveSessionUser(path, args)
	if err == nil {
		return sessionProcess{h}, nil
	}
	if errors.Is(err, sessionlaunch.ErrNoActiveSession) {
		return nil, err
	}
	cmd := exec.Command(path, args...)
	if cerr := cmd.Start(); cerr != nil {
		return nil, fmt.Errorf("%v (session launch: %v)", cerr, err)
	}
	return cmdProcess{cmd}, nil
}
