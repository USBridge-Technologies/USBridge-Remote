//go:build linux

package vdisplay

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildGrantScript(t *testing.T) {
	s := buildGrantScript("/tmp/r.rules", `"bob"`)
	for _, want := range []string{"groupadd " + GroupName, `usermod -aG ` + GroupName + ` "bob"`, polkitRulePath} {
		if !strings.Contains(s, want) {
			t.Errorf("script missing %q: %s", want, s)
		}
	}
}

func TestPolkitRuleScopedToVkms(t *testing.T) {
	if !strings.Contains(polkitRuleContent, `(-r )?vkms$`) {
		t.Error("rule should allow only modprobe [-r] vkms")
	}
	if strings.Contains(polkitRuleContent, "sh|bash") {
		t.Error("rule must not allow shells")
	}
}

func TestPolkitRuleAllowsOnlyVkmsConnectorTee(t *testing.T) {
	if !strings.Contains(polkitRuleContent, `Virtual-[0-9]+\/status|\/uevent)$`) {
		t.Error("tee must be limited to the vkms connector status file")
	}
}

func TestUeventPathFromStatusPath(t *testing.T) {
	// mirrors writeConnectorStatus's derivation
	p := "/sys/class/drm/card0-Virtual-1/status"
	card := filepath.Join(filepath.Dir(filepath.Dir(p)), strings.SplitN(filepath.Base(filepath.Dir(p)), "-", 2)[0], "uevent")
	if card != "/sys/class/drm/card0/uevent" {
		t.Fatalf("got %s", card)
	}
}
