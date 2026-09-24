//go:build linux

package permissions

// Interactive grant tests: they run the real pkexec install (a password
// prompt on this machine's screen), so they only run when asked:
//
//	APPDIR=<dir with usr/bin/usbridge-streamer-launch> \
//	USBRIDGE_LIVE_GRANT_SUNSHINE_TREE=~/.config/usbridge-agent/sunshine-runtime \
//	  go test -run TestLiveGrant -v ./internal/permissions/
//
// USBRIDGE_LIVE_REINSTALL=1 force-replaces the installed launcher with the
// one under $APPDIR (for testing launcher changes without a protocol bump).

import (
	"os"
	"os/exec"
	"testing"

	"usbridge_agent/internal/streamerlaunch"
)

func TestLiveGrant_SunshineAndLauncher(t *testing.T) {
	src := os.Getenv("USBRIDGE_LIVE_GRANT_SUNSHINE_TREE")
	if src == "" {
		t.Skip("interactive (pkexec): set USBRIDGE_LIVE_GRANT_SUNSHINE_TREE")
	}
	s := New()
	if !s.RequestKMSCapture(src) {
		t.Fatalf("grant failed: %s", s.lastAccessErr)
	}
	if !s.StreamerLauncherInstalled() || !s.SunshineLaunchReady() || !s.KMSCaptureGranted(src) || !s.KMSCaptureGranted(streamerlaunch.InstallPath) {
		t.Fatal("grant reported success but state is incomplete")
	}
	for _, p := range []string{streamerlaunch.InstallDir, streamerlaunch.InstallPath, streamerlaunch.AllowedUIDsPath} {
		if err := streamerlaunch.CheckRootOwned(p); err != nil {
			t.Error(err)
		}
	}
	if err := streamerlaunch.CheckTreeRootOwned(streamerlaunch.SunshineDir); err != nil {
		t.Error(err)
	}
}

func TestLiveGrant_ReinstallLauncher(t *testing.T) {
	if os.Getenv("USBRIDGE_LIVE_REINSTALL") == "" {
		t.Skip("interactive (pkexec): set USBRIDGE_LIVE_REINSTALL=1")
	}
	staged, sum, err := copyAndHash(bundledStreamerLauncher())
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(staged)
	out, err := exec.Command("pkexec", "/bin/sh", "-c", buildStreamerLauncherInstallScript(staged, sum, findCapTool("setcap"), os.Getuid())).CombinedOutput()
	if err != nil {
		t.Fatalf("pkexec: %v %s", err, out)
	}
	if !New().StreamerLauncherInstalled() {
		t.Fatal("not installed after reinstall")
	}
}
