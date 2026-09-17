//go:build linux

package autostart

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
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

// TestXdgQuoteExecArgLeavesPlainArgsBare makes sure the overwhelmingly
// common case (a path/flag with no special characters, e.g. "--tray" or a
// typical install path) is left unquoted -- a real DE's Exec= parser is far
// more battle-tested against bare args than quoted ones, so needlessly
// quoting everything would be a regression risk of its own.
func TestXdgQuoteExecArgLeavesPlainArgsBare(t *testing.T) {
	for _, in := range []string{"--tray", "/opt/usbridge-agent/usbridge-agent", "/home/user/.local/bin/usbridge-agent"} {
		if got := xdgQuoteExecArg(in); got != in {
			t.Errorf("xdgQuoteExecArg(%q) = %q, want it unchanged", in, got)
		}
	}
}

// TestXdgQuoteExecArgQuotesAndEscapesSpecialChars covers a path containing a
// space (the one realistic case, e.g. "/home/first last/...") plus the
// characters the Desktop Entry Specification says must be backslash-escaped
// inside a quoted Exec= argument.
func TestXdgQuoteExecArgQuotesAndEscapesSpecialChars(t *testing.T) {
	in := `/home/first last/bin/usbridge-agent "quoted" $VAR \tail`
	got := xdgQuoteExecArg(in)
	if !strings.HasPrefix(got, `"`) || !strings.HasSuffix(got, `"`) {
		t.Fatalf("xdgQuoteExecArg(%q) = %q, want it wrapped in double quotes", in, got)
	}
	for _, want := range []string{`\"quoted\"`, `\$VAR`, `\\tail`} {
		if !strings.Contains(got, want) {
			t.Errorf("xdgQuoteExecArg(%q) = %q, want it to contain the escaped form %q", in, got, want)
		}
	}
}

// TestTrayDesktopEntryContentIsWellFormed pins the shape of the generated
// XDG autostart entry: launches the given exe with exactly --tray, is
// hidden from application menus/launchers (NoDisplay=true -- it's an
// autostart-only helper, not something a user should be able to launch a
// second copy of), and is an Application-type entry so every desktop
// environment's autostart implementation actually honors it.
func TestTrayDesktopEntryContentIsWellFormed(t *testing.T) {
	got := trayDesktopEntryContent("/opt/usbridge-agent/usbridge-agent")

	for _, want := range []string{
		"[Desktop Entry]",
		"Type=Application",
		"NoDisplay=true",
		"Exec=/opt/usbridge-agent/usbridge-agent --tray",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("trayDesktopEntryContent missing %q in:\n%s", want, got)
		}
	}
}

// TestTrayDesktopEntryContentQuotesSpacyPath is the regression case for a
// path containing a space (a real possibility -- e.g. an AppImage placed
// under a user's Desktop/Downloads folder) actually surviving Exec=
// parsing, rather than being split into two bogus arguments.
func TestTrayDesktopEntryContentQuotesSpacyPath(t *testing.T) {
	got := trayDesktopEntryContent("/home/first last/usbridge-agent")
	if !strings.Contains(got, `Exec="/home/first last/usbridge-agent" --tray`) {
		t.Fatalf("trayDesktopEntryContent did not quote the spacy exe path:\n%s", got)
	}
}

// TestTrayAutostartDirHonorsXDGConfigHome mirrors the same override every
// other XDG-compliant autostart consumer respects -- a test-only sanity
// check that this doesn't silently ignore it and hardcode ~/.config.
func TestTrayAutostartDirHonorsXDGConfigHome(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/custom/xdg-config")

	dir, err := trayAutostartDir()
	if err != nil {
		t.Fatalf("trayAutostartDir: %v", err)
	}
	want := filepath.Join("/custom/xdg-config", "autostart")
	if dir != want {
		t.Fatalf("trayAutostartDir() = %q, want %q", dir, want)
	}
}

// TestTrayAutostartDirFallsBackToDotConfig covers the common case (no
// XDG_CONFIG_HOME set) against the Base Directory Specification's own
// documented default, not just "whatever the code currently does".
func TestTrayAutostartDirFallsBackToDotConfig(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")

	dir, err := trayAutostartDir()
	if err != nil {
		t.Fatalf("trayAutostartDir: %v", err)
	}
	if !strings.HasSuffix(dir, filepath.Join(".config", "autostart")) {
		t.Fatalf("trayAutostartDir() = %q, want it to end in .config/autostart", dir)
	}
}

// TestInstallAndRemoveTrayAutostartRoundTrip exercises the actual
// file-write/remove path (not just content generation) against a temp
// HOME, so a real filesystem-permission or path-joining mistake in
// installTrayAutostart/removeTrayAutostart would fail this test even
// though it can't run the privileged systemd half of Enable()/Disable() in
// CI.
func TestInstallAndRemoveTrayAutostartRoundTrip(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	dir, err := trayAutostartDir()
	if err != nil {
		t.Fatalf("trayAutostartDir: %v", err)
	}
	entryPath := filepath.Join(dir, trayAutostartFile)

	if err := installTrayAutostart("/opt/usbridge-agent/usbridge-agent"); err != nil {
		t.Fatalf("installTrayAutostart: %v", err)
	}
	if _, err := os.Stat(entryPath); err != nil {
		t.Fatalf("expected %s to exist after installTrayAutostart: %v", entryPath, err)
	}

	if err := removeTrayAutostart(); err != nil {
		t.Fatalf("removeTrayAutostart: %v", err)
	}
	if _, err := os.Stat(entryPath); err == nil {
		t.Fatalf("expected %s to be gone after removeTrayAutostart", entryPath)
	}

	// Removing an already-removed entry must stay a no-op, not an error --
	// Disable() is expected to be callable even when autostart was never
	// enabled in the first place.
	if err := removeTrayAutostart(); err != nil {
		t.Fatalf("removeTrayAutostart on an already-removed entry: %v", err)
	}
}
