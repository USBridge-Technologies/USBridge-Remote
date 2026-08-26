//go:build linux

package autostart

import (
	"errors"
	"os/exec"
	"strings"
	"testing"
)

// TestFriendlyPkexecErrorDevTTY is the regression test for the bug reported
// live on Linux Mint (Cinnamon): pkexec falling back to text-mode auth with
// no controlling terminal available prints a raw, confusing
// "Error opening current controlling terminal for the process (`/dev/tty')"
// — friendlyPkexecError must turn that into the same actionable
// "no polkit authentication agent" message used elsewhere in this codebase
// (permissions.Service), not pass the raw polkit internals through.
func TestFriendlyPkexecErrorDevTTY(t *testing.T) {
	out := []byte("Error creating textual authentication agent: Error opening current controlling terminal for the process (`/dev/tty'): No such device or address\n")
	err := errors.New("exit status 127")

	got := friendlyPkexecError(err, out)
	if !strings.Contains(got, "no polkit authentication agent is running") {
		t.Fatalf("friendlyPkexecError = %q, want it to mention the missing polkit authentication agent", got)
	}
}

// TestFriendlyPkexecErrorNoAgent covers the other, already-known spelling of
// the same underlying problem (older/different polkit versions report this
// instead of the /dev/tty message).
func TestFriendlyPkexecErrorNoAgent(t *testing.T) {
	out := []byte("No authentication agent found for action org.freedesktop.policykit.exec\n")
	err := errors.New("exit status 127")

	got := friendlyPkexecError(err, out)
	if !strings.Contains(got, "no polkit authentication agent is running") {
		t.Fatalf("friendlyPkexecError = %q, want it to mention the missing polkit authentication agent", got)
	}
}

// TestFriendlyPkexecErrorDismissed covers a user actually being shown the
// prompt and cancelling/timing it out — must stay a distinct, correct
// message rather than being misclassified as "no agent running".
func TestFriendlyPkexecErrorDismissed(t *testing.T) {
	out := []byte("Error executing command as another user: Request dismissed\n")
	err := errors.New("exit status 126")

	got := friendlyPkexecError(err, out)
	if strings.Contains(got, "no polkit authentication agent is running") {
		t.Fatalf("friendlyPkexecError = %q, misclassified a dismissed prompt as a missing agent", got)
	}
	if !strings.Contains(got, "cancelled or dismissed") {
		t.Fatalf("friendlyPkexecError = %q, want it to mention the dismissed prompt", got)
	}
}

// TestFriendlyPkexecErrorFallsThroughUnknownFailures makes sure a failure
// this function doesn't specifically recognize still surfaces the real
// pkexec output instead of being swallowed — no regression for whatever
// currently-handled-generically error a working install might already hit.
func TestFriendlyPkexecErrorFallsThroughUnknownFailures(t *testing.T) {
	out := []byte("systemctl: command not found\n")
	err := errors.New("exit status 1")

	got := friendlyPkexecError(err, out)
	if !strings.Contains(got, "command not found") {
		t.Fatalf("friendlyPkexecError = %q, want the raw pkexec output preserved for an unrecognized failure", got)
	}
}

// TestEnsurePolkitAuthAgentNoopsWhenAgentAlreadyRunning is the core
// don't-break-anything guarantee: on any system where a polkit
// authentication agent is already registered (every desktop environment
// this already worked on — GNOME, KDE, XFCE, ...), ensurePolkitAuthAgent
// must do nothing at all, never spawning a second process. Simulated here
// with a real, known-running `sleep` process standing in for "an agent is
// already up", so the test doesn't depend on one actually being installed
// on the CI/dev machine.
func TestEnsurePolkitAuthAgentNoopsWhenAgentAlreadyRunning(t *testing.T) {
	if _, err := exec.LookPath("pgrep"); err != nil {
		t.Skip("pgrep not available in this environment")
	}
	if _, err := exec.LookPath("sleep"); err != nil {
		t.Skip("sleep not available in this environment")
	}

	sentinel := exec.Command("sleep", "usbridge-autostart-test-sentinel-937d1")
	if err := sentinel.Start(); err != nil {
		t.Fatalf("start sentinel process: %v", err)
	}
	defer sentinel.Process.Kill()
	defer sentinel.Wait()

	origPattern := pkexecAuthAgentProcessPattern
	origCandidates := pkexecAuthAgentCandidates
	pkexecAuthAgentProcessPattern = "usbridge-autostart-test-sentinel-937d1"
	pkexecAuthAgentCandidates = []string{"/nonexistent/should-never-be-reached-if-noop-holds"}
	defer func() {
		pkexecAuthAgentProcessPattern = origPattern
		pkexecAuthAgentCandidates = origCandidates
	}()

	// Must return immediately having found the sentinel via pgrep, without
	// ever reaching (let alone os.Stat-ing) the bogus candidate above.
	ensurePolkitAuthAgent()
}
