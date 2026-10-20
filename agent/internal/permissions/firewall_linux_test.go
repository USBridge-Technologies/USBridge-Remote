//go:build linux

package permissions

import (
	"reflect"
	"testing"
)

// TestUFWMissingParsesUserRules covers the live Arch/ufw case: rules added
// by hand (single ports, a UDP range, a LAN-only rule) must count as open,
// and only genuinely uncovered ports come back as missing.
func TestUFWMissingParsesUserRules(t *testing.T) {
	content := `
### tuple ### allow tcp 8080 0.0.0.0/0 any 0.0.0.0/0 in comment=7573627269646765
-A ufw-user-input -p tcp --dport 8080 -j ACCEPT
### tuple ### allow tcp 47984 0.0.0.0/0 any 192.168.1.0/24 in
### tuple ### deny tcp 8443 0.0.0.0/0 any 0.0.0.0/0 in
### tuple ### allow udp 47998:48000 0.0.0.0/0 any 0.0.0.0/0 in
### tuple ### allow any 48010 0.0.0.0/0 any 0.0.0.0/0 in
`
	rules := parseUFWAllowRules(content)
	if got := ufwMissing(rules, "tcp", []int{8080, 8443, 47984, 48010}); !reflect.DeepEqual(got, []int{8443}) {
		t.Fatalf("tcp missing = %v, want [8443]", got)
	}
	if got := ufwMissing(rules, "udp", []int{47998, 47999, 48000, 48002}); !reflect.DeepEqual(got, []int{48002}) {
		t.Fatalf("udp missing = %v, want [48002]", got)
	}
}

func TestBuildFirewallScriptCollapsesRanges(t *testing.T) {
	tcp, udp := FirewallPorts(8080, 8443, 8090, 47990)
	got := buildFirewallScript(firewallUFW, tcp, udp)
	want := "ufw allow 8080/tcp comment usbridge && ufw allow 8090/tcp comment usbridge && " +
		"ufw allow 8443:8444/tcp comment usbridge && ufw allow 47984/tcp comment usbridge && " +
		"ufw allow 47989/tcp comment usbridge && ufw allow 48010/tcp comment usbridge && " +
		"ufw allow 47998:48002/udp comment usbridge"
	if got != want {
		t.Fatalf("ufw script =\n%s\nwant\n%s", got, want)
	}
	got = buildFirewallScript(firewallFirewalld, nil, []int{47998, 47999})
	if want := "firewall-cmd --permanent --add-port=47998-47999/udp && firewall-cmd --reload"; got != want {
		t.Fatalf("firewalld script = %q, want %q", got, want)
	}
	if got := buildFirewallScript(firewallUFW, nil, nil); got != "" {
		t.Fatalf("empty script = %q, want \"\"", got)
	}
}
