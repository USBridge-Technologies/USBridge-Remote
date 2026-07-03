//go:build !linux && !windows

package sunshine

import "os/exec"

// configureProcess is a no-op on macOS — Pdeathsig has no equivalent there;
// it relies solely on the agent's own graceful shutdown (App.RestartSunshine/
// Stop) to terminate Sunshine.
func configureProcess(cmd *exec.Cmd) {}
