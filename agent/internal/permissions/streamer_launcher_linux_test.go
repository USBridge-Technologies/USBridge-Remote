//go:build linux

package permissions

import (
	"strings"
	"testing"

	"usbridge_agent/internal/streamerlaunch"
)

// The capability must only ever be applied to the root-owned copy, and
// only after that copy's hash was checked.
func TestStreamerLauncherInstallScript_HashBeforeSetcap(t *testing.T) {
	s := buildStreamerLauncherInstallScript("/tmp/src launcher", "abc123", "/usr/sbin/setcap", 1000)
	iHash := strings.Index(s, "sha256sum -c")
	iCap := strings.Index(s, "cap_sys_admin=ep")
	iMove := strings.Index(s, "mv -f")
	if !strings.HasPrefix(s, "set -e\n") || iHash < 0 || iCap < iHash || iMove < iCap {
		t.Fatalf("wrong order:\n%s", s)
	}
	if !strings.Contains(s, `install -o root -g root -m 0755 "/tmp/src launcher" "`+streamerlaunch.InstallDir+`/.usbridge-streamer-launch.tmp"`) {
		t.Fatalf("source not copied root-owned first:\n%s", s)
	}
	if strings.Contains(s, `setcap" cap_sys_admin=ep "/tmp`) {
		t.Fatal("setcap on the user-writable source")
	}
	if !strings.Contains(s, "grep -qx 1000") {
		t.Fatal("uid not added to allowlist")
	}
}

func TestSunshineTreeInstallScript_ChecksBeforeReplacing(t *testing.T) {
	s := buildSunshineTreeInstallScript("/home/u/.config/usbridge-agent/sunshine-runtime", "/tmp/m", "deadbeef")
	iChown := strings.Index(s, "chown -R -h root:root")
	iHash := strings.Index(s, "sha256sum -c --status --strict")
	iList := strings.Index(s, "cmp -s")
	iSwap := strings.Index(s, `mv "`+streamerlaunch.InstallDir+`/.sunshine.tmp" "`+streamerlaunch.SunshineDir+`"`)
	if iChown < 0 || iHash < iChown || iList < iHash || iSwap < iList {
		t.Fatalf("wrong order:\n%s", s)
	}
}
