//go:build darwin

package autostart

import (
	"os"
	"strings"
	"testing"
)

// TestLaunchAgentContentIsWellFormed pins the shape shared by both the
// primary (--headless engine) and tray (--tray helper) LaunchAgents: a
// valid plist wrapper, the right Label, every argument present in
// ProgramArguments (exe first, then each arg in order), and RunAtLoad set
// so it actually starts at login without needing a separate "kickstart".
func TestLaunchAgentContentIsWellFormed(t *testing.T) {
	got := launchAgentContent("io.usbridge.agent.tray", "/Applications/USBridge Agent.app/Contents/MacOS/usbridge-agent", []string{"--tray"})

	for _, want := range []string{
		`<?xml version="1.0" encoding="UTF-8"?>`,
		"<key>Label</key>",
		"<string>io.usbridge.agent.tray</string>",
		"<key>ProgramArguments</key>",
		"<string>/Applications/USBridge Agent.app/Contents/MacOS/usbridge-agent</string>",
		"<string>--tray</string>",
		"<key>RunAtLoad</key>",
		"<true/>",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("launchAgentContent missing %q in:\n%s", want, got)
		}
	}
}

// TestLaunchAgentContentEscapesXML is the regression case for a path
// containing XML-significant characters (a space is common and already
// harmless in plist strings, but '&'/'<'/'>' are not) actually surviving
// plist parsing instead of producing malformed XML launchd silently
// refuses to load.
func TestLaunchAgentContentEscapesXML(t *testing.T) {
	got := launchAgentContent("io.usbridge.agent", "/tmp/a&b<c>d", nil)
	if strings.Contains(got, "/tmp/a&b<c>d") {
		t.Fatalf("launchAgentContent did not escape XML-significant characters:\n%s", got)
	}
	if !strings.Contains(got, "/tmp/a&amp;b&lt;c&gt;d") {
		t.Fatalf("launchAgentContent did not produce the expected escaped path:\n%s", got)
	}
}

// TestPlistPathsAreDistinct guards the whole point of having a second
// LaunchAgent at all: if these ever collapsed to the same path, Enable()
// would silently overwrite the primary (--headless engine) LaunchAgent
// with the tray helper's, breaking autostart entirely.
func TestPlistPathsAreDistinct(t *testing.T) {
	main, err := plistPath()
	if err != nil {
		t.Fatalf("plistPath: %v", err)
	}
	tray, err := trayPlistPath()
	if err != nil {
		t.Fatalf("trayPlistPath: %v", err)
	}
	if main == tray {
		t.Fatalf("plistPath() and trayPlistPath() must differ, both returned %q", main)
	}
}

// TestWriteAndRemoveLaunchAgentRoundTrip exercises the actual file-write/
// remove path against a temp HOME (launchctl load/unload themselves are
// best-effort and ignored on error already, see writeLaunchAgent/
// removeLaunchAgent -- this only pins the filesystem side, which is what a
// path-joining or permission mistake would actually break).
func TestWriteAndRemoveLaunchAgentRoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	// A distinct, obviously-test-only label/path -- launchctl load/unload
	// (called inside writeLaunchAgent/removeLaunchAgent) are real syscalls
	// against the real launchd, so this deliberately never touches the
	// production io.usbridge.agent(.tray) labels a real install might
	// already have loaded on this machine.
	path, err := launchAgentPath("io.usbridge.agent.tray.roundtriptest")
	if err != nil {
		t.Fatalf("launchAgentPath: %v", err)
	}

	if err := writeLaunchAgent(path, launchAgentContent("io.usbridge.agent.tray.roundtriptest", "/nonexistent/usbridge-agent-test-binary", []string{"--tray"})); err != nil {
		t.Fatalf("writeLaunchAgent: %v", err)
	}
	if !trayPlistExists(t, path) {
		t.Fatalf("expected %s to exist after writeLaunchAgent", path)
	}

	if err := removeLaunchAgent(path); err != nil {
		t.Fatalf("removeLaunchAgent: %v", err)
	}
	if trayPlistExists(t, path) {
		t.Fatalf("expected %s to be gone after removeLaunchAgent", path)
	}

	// Removing an already-removed plist must stay a no-op -- Disable() is
	// expected to be callable even when autostart was never enabled.
	if err := removeLaunchAgent(path); err != nil {
		t.Fatalf("removeLaunchAgent on an already-removed plist: %v", err)
	}
}

func trayPlistExists(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Stat(path)
	return err == nil
}
