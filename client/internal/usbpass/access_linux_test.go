//go:build linux

package usbpass

import (
	"strings"
	"testing"
)

func TestUSBRuleIsNotWorldWritable(t *testing.T) {
	if strings.Contains(usbRuleContent, `MODE="0666"`) {
		t.Fatalf("usbRuleContent still grants world-writable MODE=0666: %q", usbRuleContent)
	}
	if !strings.Contains(usbRuleContent, `MODE="0660"`) {
		t.Fatalf("usbRuleContent does not grant MODE=0660: %q", usbRuleContent)
	}
	if !strings.Contains(usbRuleContent, `GROUP="`+usbGroupName+`"`) {
		t.Fatalf("usbRuleContent does not scope to %s: %q", usbGroupName, usbRuleContent)
	}
	if !strings.Contains(usbRuleContent, `TAG+="uaccess"`) {
		t.Fatalf("usbRuleContent dropped TAG+=uaccess: %q", usbRuleContent)
	}
}

func TestBuildUSBGrantScript(t *testing.T) {
	script := buildUSBGrantScript("/tmp/rule", `"amir"`, []string{"2-3"})

	if strings.Contains(script, "chmod 0666") {
		t.Fatalf("script still chmods 0666: %q", script)
	}
	if !strings.Contains(script, "chmod 0660") {
		t.Fatalf("script missing chmod 0660: %q", script)
	}
	if !strings.Contains(script, "groupadd "+usbGroupName) {
		t.Fatalf("script missing groupadd: %q", script)
	}
	if !strings.Contains(script, `usermod -aG `+usbGroupName+` "amir"`) {
		t.Fatalf("script missing usermod: %q", script)
	}
	if !strings.Contains(script, `setfacl -m u:"amir":rw-`) {
		t.Fatalf("script missing setfacl: %q", script)
	}
	if !strings.Contains(script, `/sys/bus/usb/devices/2-3:*`) {
		t.Fatalf("script missing unbind for 2-3: %q", script)
	}
	if !strings.Contains(script, "exit $STATUS") {
		t.Fatalf("script does not preserve exit status: %q", script)
	}
}

func TestBuildUSBGrantScriptRejectsUnsafeBusID(t *testing.T) {
	script := buildUSBGrantScript("/tmp/rule", `"amir"`, []string{`2-3; rm -rf /`, `1-1.2`})
	if strings.Contains(script, "rm -rf") {
		t.Fatalf("unsafe busid leaked into script: %q", script)
	}
	if !strings.Contains(script, `/sys/bus/usb/devices/1-1.2:*`) {
		t.Fatalf("hub-port busid 1-1.2 should be allowed: %q", script)
	}
}

func TestShellQuoteEscapesMetacharacters(t *testing.T) {
	cases := map[string]string{
		"amir": `"amir"`,
		`a"b`:  `"a\"b"`,
		`a$b`:  `"a\$b"`,
		`a\b`:  `"a\\b"`,
	}
	for in, want := range cases {
		if got := shellQuote(in); got != want {
			t.Errorf("shellQuote(%q) = %q, want %q", in, got, want)
		}
	}
}
