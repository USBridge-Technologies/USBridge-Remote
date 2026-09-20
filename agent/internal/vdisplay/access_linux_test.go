//go:build linux

package vdisplay

import (
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
