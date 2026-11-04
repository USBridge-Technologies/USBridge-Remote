//go:build linux

package usbpass

import (
	"regexp"
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

// The command lines rust-shine's vhci_linux.rs runs under pkexec, against the
// rule's own pattern.
func TestPolkitRuleMatchesBrokerCommands(t *testing.T) {
	const open, shut = "if (/", "/.test(cmd))"
	i := strings.LastIndex(polkitRuleContent, open)
	j := strings.Index(polkitRuleContent, shut)
	if i < 0 || j < i {
		t.Fatal("command pattern not found in the rule")
	}
	re := regexp.MustCompile(polkitRuleContent[i+len(open) : j])
	for cmd, want := range map[string]bool{
		"/usr/bin/usbip attach -r 127.0.0.1 -b 1-2":                  true,
		"/usr/bin/usbip --tcp-port 39541 attach -r 127.0.0.1 -b 7-1": true,
		"/usr/bin/usbip detach -p 0":                                 true,
		"/usr/bin/usbip port":                                        true,
		"/usr/bin/usbip bind -b 1-2":                                 false,
		"/usr/bin/usbip --tcp-port 1 bind -b 1-2":                    false,
		"/usr/bin/usbip --tcp-port x attach":                         false,
	} {
		if got := re.MatchString(cmd); got != want {
			t.Errorf("%q: allowed=%v, want %v", cmd, got, want)
		}
	}
}

// The rule file is installed verbatim by the Linux packages too, so the
// group name in it can't follow AttachGroupName on its own.
func TestPolkitRuleNamesAttachGroup(t *testing.T) {
	if !strings.Contains(polkitRuleContent, `subject.isInGroup("`+AttachGroupName+`")`) {
		t.Fatalf("usbip-polkit.rules does not check group %q", AttachGroupName)
	}
}
