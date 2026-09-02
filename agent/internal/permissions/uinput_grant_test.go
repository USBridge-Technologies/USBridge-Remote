//go:build linux

package permissions

import (
	"strings"
	"testing"
)

// TestUinputRuleIsNotWorldWritable is the regression test for the
// local-privilege-escalation report: the persistent udev rule for
// /dev/uinput must never grant MODE="0666" (any local user, any process),
// only a scoped GROUP + tighter MODE, with uaccess kept alongside for the
// live-session case. See uinputRuleContent's doc comment.
func TestUinputRuleIsNotWorldWritable(t *testing.T) {
	if got := uinputRuleContent; strings.Contains(got, `MODE="0666"`) {
		t.Fatalf("uinputRuleContent still grants world-writable MODE=0666: %q", got)
	}
	if !strings.Contains(uinputRuleContent, `MODE="0660"`) {
		t.Fatalf("uinputRuleContent does not grant the expected MODE=0660: %q", uinputRuleContent)
	}
	if !strings.Contains(uinputRuleContent, `GROUP="`+UinputGroupName+`"`) {
		t.Fatalf("uinputRuleContent does not scope access to UinputGroupName (%q): %q", UinputGroupName, uinputRuleContent)
	}
	if !strings.Contains(uinputRuleContent, `TAG+="uaccess"`) {
		t.Fatalf("uinputRuleContent dropped TAG+=\"uaccess\" (needed for the immediate-effect, active-session case): %q", uinputRuleContent)
	}
}

// TestBuildUinputGrantScriptNeverChmods666 covers the other half: even the
// live, this-session grant (chgrp/chmod on the already-existing device node,
// for immediate effect before the next module reload) must use 0660, not
// the old world-writable 0666 — and it must create/join UinputGroupName
// rather than relying on chmod alone.
func TestBuildUinputGrantScriptNeverChmods666(t *testing.T) {
	script := buildUinputGrantScript("/tmp/rule", "/tmp/modules", `"amir"`)

	if strings.Contains(script, "chmod 0666") {
		t.Fatalf("buildUinputGrantScript still chmods /dev/uinput to 0666: %q", script)
	}
	if !strings.Contains(script, "chmod 0660") {
		t.Fatalf("buildUinputGrantScript does not chmod /dev/uinput to 0660: %q", script)
	}
	if !strings.Contains(script, "chgrp "+UinputGroupName) {
		t.Fatalf("buildUinputGrantScript does not chgrp the device to UinputGroupName: %q", script)
	}
	if !strings.Contains(script, "groupadd "+UinputGroupName) {
		t.Fatalf("buildUinputGrantScript does not create UinputGroupName when missing: %q", script)
	}
	if !strings.Contains(script, `usermod -aG `+UinputGroupName+` "amir"`) {
		t.Fatalf("buildUinputGrantScript does not add the current user to UinputGroupName: %q", script)
	}
	if !strings.Contains(script, `setfacl -m u:"amir":rw- /dev/uinput`) {
		t.Fatalf("buildUinputGrantScript does not grant an immediate per-user ACL: %q", script)
	}
	if !strings.Contains(script, "exit $STATUS") {
		t.Fatalf("buildUinputGrantScript does not preserve the real exit status past the best-effort setfacl step: %q", script)
	}
}

// TestShellQuoteUsernameEscapesMetacharacters guards the one thing that
// would turn an edge-case username into a shell-injection bug in the
// pkexec-run script above.
func TestShellQuoteUsernameEscapesMetacharacters(t *testing.T) {
	cases := map[string]string{
		"amir": `"amir"`,
		`a"b`:  `"a\"b"`,
		`a$b`:  `"a\$b"`,
		`a\b`:  `"a\\b"`,
	}
	for in, want := range cases {
		if got := shellQuoteUsername(in); got != want {
			t.Errorf("shellQuoteUsername(%q) = %q, want %q", in, got, want)
		}
	}
}
