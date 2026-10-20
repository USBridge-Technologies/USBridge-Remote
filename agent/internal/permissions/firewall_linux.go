//go:build linux

package permissions

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// firewallKind identifies which host firewall front-end is active. Only ufw
// and firewalld are handled: they're what desktop distros enable by default
// (Ubuntu/Mint/Manjaro ship ufw, Fedora/openSUSE/RHEL ship firewalld). A
// hand-written nftables/iptables ruleset is left alone -- there's no safe
// generic way to add rules to someone else's custom chains.
type firewallKind int

const (
	firewallNone firewallKind = iota
	firewallUFW
	firewallFirewalld
)

const (
	ufwConfPath      = "/etc/ufw/ufw.conf"
	ufwDefaultsPath  = "/etc/default/ufw"
	ufwUserRulesPath = "/etc/ufw/user.rules"
)

// ufwEnabled reports ENABLED=yes in ufw.conf -- what `ufw enable` writes and
// ufw-init reads at boot. Read straight from the file because `ufw status`
// itself refuses to run without root.
func ufwEnabled() bool {
	if _, err := exec.LookPath("ufw"); err != nil {
		if _, err := os.Stat("/usr/sbin/ufw"); err != nil {
			return false
		}
	}
	return confValue(ufwConfPath, "ENABLED") == "yes"
}

// confValue returns key's value from a shell-style KEY=value file, unquoted
// and lower-cased, or "" if absent/unreadable.
func confValue(path, key string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		k, v, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(k) != key {
			continue
		}
		return strings.ToLower(strings.Trim(strings.TrimSpace(v), `"'`))
	}
	return ""
}

func firewalldRunning() bool {
	if _, err := exec.LookPath("firewall-cmd"); err != nil {
		return false
	}
	out, err := exec.Command("firewall-cmd", "--state").CombinedOutput()
	return err == nil && strings.TrimSpace(string(out)) == "running"
}

func detectFirewall() firewallKind {
	switch {
	case ufwEnabled():
		return firewallUFW
	case firewalldRunning():
		return firewallFirewalld
	}
	return firewallNone
}

// ufwAllowRule is one inbound allow rule parsed from user.rules' "### tuple
// ###" comments, e.g. "allow tcp 8080 0.0.0.0/0 any 0.0.0.0/0 in".
type ufwAllowRule struct {
	proto string // "tcp", "udp" or "any"
	dport string // "any", "8080", "47998:48000" or "80,443"
}

// parseUFWAllowRules extracts the inbound allow rules from user.rules
// content. Rules restricted to a source subnet still count -- the point is
// only to tell whether the user has already opened these ports at all.
func parseUFWAllowRules(content string) []ufwAllowRule {
	var rules []ufwAllowRule
	for _, line := range strings.Split(content, "\n") {
		rest, ok := strings.CutPrefix(strings.TrimSpace(line), "### tuple ###")
		if !ok {
			continue
		}
		f := strings.Fields(rest)
		// action proto dport dst sport src [direction] ...
		if len(f) < 6 || f[0] != "allow" {
			continue
		}
		if len(f) >= 7 && f[6] != "in" && !strings.HasPrefix(f[6], "comment=") {
			continue
		}
		rules = append(rules, ufwAllowRule{proto: f[1], dport: f[2]})
	}
	return rules
}

// portSpecCovers reports whether a ufw ("a:b", "a,b") or firewalld ("a-b")
// port spec includes port.
func portSpecCovers(spec string, port int) bool {
	if spec == "any" {
		return true
	}
	for _, part := range strings.Split(spec, ",") {
		lo, hi, isRange := strings.Cut(part, ":")
		if !isRange {
			lo, hi, isRange = strings.Cut(part, "-")
		}
		if !isRange {
			hi = lo
		}
		l, err1 := strconv.Atoi(lo)
		h, err2 := strconv.Atoi(hi)
		if err1 == nil && err2 == nil && port >= l && port <= h {
			return true
		}
	}
	return false
}

func ufwMissing(rules []ufwAllowRule, proto string, ports []int) []int {
	var missing []int
	for _, p := range ports {
		covered := false
		for _, r := range rules {
			if (r.proto == proto || r.proto == "any") && portSpecCovers(r.dport, p) {
				covered = true
				break
			}
		}
		if !covered {
			missing = append(missing, p)
		}
	}
	return missing
}

// firewalldOpenPorts lists the default zone's open ports ("8080/tcp
// 47998-48002/udp ..."). Querying needs no root on stock polkit policy.
func firewalldOpenPorts() (map[string][]string, error) {
	out, err := exec.Command("firewall-cmd", "--list-ports").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("firewall-cmd --list-ports: %v (%s)", err, strings.TrimSpace(string(out)))
	}
	open := map[string][]string{}
	for _, entry := range strings.Fields(string(out)) {
		spec, proto, ok := strings.Cut(entry, "/")
		if ok {
			open[proto] = append(open[proto], spec)
		}
	}
	return open, nil
}

func firewalldMissing(open map[string][]string, proto string, ports []int) []int {
	var missing []int
	for _, p := range ports {
		covered := false
		for _, spec := range open[proto] {
			if portSpecCovers(spec, p) {
				covered = true
				break
			}
		}
		if !covered {
			missing = append(missing, p)
		}
	}
	return missing
}

// FirewallBlocking reports whether an active ufw/firewalld would drop inbound
// connections on any of tcp/udp -- the symptom being a client that pairs
// fine over Tailscale but shows no stream on the LAN. False when no managed
// firewall is active, or ufw's default inbound policy is already ACCEPT.
func (s *Service) FirewallBlocking(tcp, udp []int) bool {
	switch detectFirewall() {
	case firewallUFW:
		if confValue(ufwDefaultsPath, "DEFAULT_INPUT_POLICY") == "accept" {
			return false
		}
		data, err := os.ReadFile(ufwUserRulesPath)
		if err != nil {
			return true
		}
		rules := parseUFWAllowRules(string(data))
		return len(ufwMissing(rules, "tcp", tcp)) > 0 || len(ufwMissing(rules, "udp", udp)) > 0
	case firewallFirewalld:
		open, err := firewalldOpenPorts()
		if err != nil {
			log.Printf("[permissions] %v", err)
			return true
		}
		return len(firewalldMissing(open, "tcp", tcp)) > 0 || len(firewalldMissing(open, "udp", udp)) > 0
	}
	return false
}

// buildFirewallScript builds the root script RequestFirewall runs under
// pkexec for kind. ufw rules are tagged "usbridge" so they're easy to find
// and remove (`ufw status numbered`); firewalld rules go into the default
// zone both permanently and at runtime (--reload applies the permanent set).
func buildFirewallScript(kind firewallKind, tcp, udp []int) string {
	var cmds []string
	add := func(proto string, ports []int) {
		for _, r := range portRanges(ports) {
			switch kind {
			case firewallUFW:
				spec := strconv.Itoa(r[0])
				if r[1] != r[0] {
					spec += ":" + strconv.Itoa(r[1])
				}
				cmds = append(cmds, "ufw allow "+spec+"/"+proto+" comment usbridge")
			case firewallFirewalld:
				spec := strconv.Itoa(r[0])
				if r[1] != r[0] {
					spec += "-" + strconv.Itoa(r[1])
				}
				cmds = append(cmds, "firewall-cmd --permanent --add-port="+spec+"/"+proto)
			}
		}
	}
	add("tcp", tcp)
	add("udp", udp)
	if len(cmds) == 0 {
		return ""
	}
	if kind == firewallFirewalld {
		cmds = append(cmds, "firewall-cmd --reload")
	}
	return strings.Join(cmds, " && ")
}

// missingFirewallPorts narrows tcp/udp to the ports kind doesn't already
// allow, so the script only adds what's actually missing.
func missingFirewallPorts(kind firewallKind, tcp, udp []int) ([]int, []int) {
	switch kind {
	case firewallUFW:
		data, _ := os.ReadFile(ufwUserRulesPath)
		rules := parseUFWAllowRules(string(data))
		return ufwMissing(rules, "tcp", tcp), ufwMissing(rules, "udp", udp)
	case firewallFirewalld:
		open, err := firewalldOpenPorts()
		if err != nil {
			return tcp, udp
		}
		return firewalldMissing(open, "tcp", tcp), firewalldMissing(open, "udp", udp)
	}
	return nil, nil
}

// FirewallPreview returns the exact pkexec command RequestFirewall would run
// right now, for the UI's "?" button -- "" if nothing would run.
func (s *Service) FirewallPreview(tcp, udp []int) string {
	kind := detectFirewall()
	mt, mu := missingFirewallPorts(kind, tcp, udp)
	script := buildFirewallScript(kind, mt, mu)
	if script == "" {
		return ""
	}
	return "pkexec /bin/sh -c \"" + script + "\""
}

// RequestFirewall opens the missing tcp/udp ports in the active ufw or
// firewalld via pkexec -- same prompt/error handling as RequestClipboardTool.
func (s *Service) RequestFirewall(tcp, udp []int) bool {
	s.lastAccessErr = ""
	kind := detectFirewall()
	if kind == firewallNone || !s.FirewallBlocking(tcp, udp) {
		return true
	}
	if _, err := exec.LookPath("pkexec"); err != nil {
		s.lastAccessErr = "pkexec is not installed. Open the ports manually instead:\n  " +
			buildFirewallScript(kind, tcp, udp)
		return false
	}
	mt, mu := missingFirewallPorts(kind, tcp, udp)
	script := buildFirewallScript(kind, mt, mu)
	out, err := exec.Command("pkexec", "/bin/sh", "-c", script).CombinedOutput()
	log.Printf("[permissions] firewall pkexec exit=%v output=%q", err, string(out))
	if err != nil {
		switch {
		case strings.Contains(string(out), "No authentication agent found"):
			s.lastAccessErr = "no polkit authentication agent is running for this session " +
				"(pkexec needs one to prompt for the password). Log into a full desktop " +
				"session and make sure its polkit agent is running, then retry."
		case strings.Contains(err.Error(), "exit status 126"):
			s.lastAccessErr = "authentication was cancelled or dismissed. Click Open Ports again and approve the prompt."
		default:
			s.lastAccessErr = fmt.Sprintf("opening firewall ports failed: %v (%s)", err, strings.TrimSpace(string(out)))
		}
		return false
	}
	if s.FirewallBlocking(tcp, udp) {
		s.lastAccessErr = "the firewall rules were added but some ports still look closed; check `ufw status` / `firewall-cmd --list-ports`."
		return false
	}
	return true
}
