//go:build !linux

package sunshine

import "os/exec"

// configureProcess is a no-op outside Linux — Pdeathsig has no equivalent on
// Windows/macOS; those platforms rely solely on the agent's own graceful
// shutdown (App.RestartSunshine/Stop) to terminate Sunshine.
func configureProcess(cmd *exec.Cmd) {}
