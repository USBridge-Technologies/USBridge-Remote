//go:build linux

package usbpass

import (
	"strings"
	"testing"
)

func TestBuildAttachGrantScript(t *testing.T) {
	s := buildAttachGrantScript("/tmp/r.rules", `"bob"`)
	for _, want := range []string{"groupadd " + AttachGroupName, `usermod -aG ` + AttachGroupName + ` "bob"`, polkitRulePath} {
		if !strings.Contains(s, want) {
			t.Errorf("script missing %q: %s", want, s)
		}
	}
}

func TestPolkitRuleScopedToUsbip(t *testing.T) {
	for _, bad := range []string{"unbind", "bind", "YES }"} {
		if strings.Contains(polkitRuleContent, bad+")") {
			t.Errorf("rule must not allow %q", bad)
		}
	}
	if !strings.Contains(polkitRuleContent, "attach|detach|port") {
		t.Error("rule should allow attach|detach|port")
	}
}
