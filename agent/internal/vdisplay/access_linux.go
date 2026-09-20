//go:build linux

package vdisplay

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/user"
	"strings"
)

const (
	// GroupName owns the polkit exemption below. Membership is the one-time,
	// persistent grant the GUI's "Virtual Display" permission chip installs.
	GroupName      = "usbridge-display"
	polkitRulePath = "/etc/polkit-1/rules.d/49-usbridge-display.rules"
)

// polkitRuleContent lets members of GroupName run exactly `modprobe vkms` and
// `modprobe -r vkms` as root through pkexec without a password prompt. Both
// the agent and the rust-shine streamer load vkms (the in-tree virtual KMS
// driver) when a virtual monitor is picked and unload it when it is dropped,
// so without this every switch pops a polkit password dialog.
//
// Scoped on purpose: only modprobe in a root-owned system location and only
// the vkms module, load or unload -- no other module, no other program.
const polkitRuleContent = `polkit.addRule(function(action, subject) {
    if (action.id != "org.freedesktop.policykit.exec") return;
    if (!subject.isInGroup("` + GroupName + `")) return;
    var prog = action.lookup("program");
    if (!/^\/(usr\/)?s?bin\/modprobe$/.test(prog)) return;
    var cmd = action.lookup("command_line") || "";
    if (/^\S+ (-r )?vkms$/.test(cmd)) return polkit.Result.YES;
});
`

func shellQuote(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, `$`, `\$`)
	return `"` + r.Replace(s) + `"`
}

// buildGrantScript builds the /bin/sh -c script run under pkexec: create the
// group, add the user, install the polkit rule. Pure so its shape is testable
// without root.
func buildGrantScript(tmpRule, currentUser string) string {
	return fmt.Sprintf(
		"(getent group %[1]s >/dev/null || groupadd %[1]s) && usermod -aG %[1]s %[2]s && "+
			"install -d -m 0755 /etc/polkit-1/rules.d && install -m 0644 %[3]s %[4]s",
		GroupName, currentUser, tmpRule, polkitRulePath,
	)
}

// AccessGranted reports whether the polkit rule is installed in its current
// form and the running user is in GroupName (checked against the user
// database, which is what polkit itself consults -- so it holds immediately,
// without a re-login).
func AccessGranted() bool {
	data, err := os.ReadFile(polkitRulePath)
	if err != nil || string(data) != polkitRuleContent {
		return false
	}
	u, err := user.Current()
	if err != nil {
		return false
	}
	gids, err := u.GroupIds()
	if err != nil {
		return false
	}
	g, err := user.LookupGroup(GroupName)
	if err != nil {
		return false
	}
	for _, id := range gids {
		if id == g.Gid {
			return true
		}
	}
	return false
}

// GrantAccess installs the passwordless-vkms polkit rule (one pkexec prompt,
// ever) and adds the current user to GroupName.
func GrantAccess() error {
	if AccessGranted() {
		return nil
	}
	if _, err := exec.LookPath("pkexec"); err != nil {
		return fmt.Errorf("pkexec is not installed; install it (e.g. \"apt install pkexec\") and try again")
	}
	tmp, err := os.CreateTemp("", "usbridge-display-*.rules")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(polkitRuleContent); err != nil {
		tmp.Close()
		return err
	}
	tmp.Close()

	currentUser := `"$(whoami)"`
	if u, err := user.Current(); err == nil && u.Username != "" {
		currentUser = shellQuote(u.Username)
	}
	out, err := exec.Command("pkexec", "/bin/sh", "-c", buildGrantScript(tmp.Name(), currentUser)).CombinedOutput()
	log.Printf("[vdisplay] access grant exit=%v output=%q", err, string(out))
	if err != nil {
		if strings.Contains(err.Error(), "exit status 126") {
			return fmt.Errorf("authentication was cancelled or dismissed")
		}
		return fmt.Errorf("virtual display access grant: %v (%s)", err, strings.TrimSpace(string(out)))
	}
	if !AccessGranted() {
		return fmt.Errorf("polkit rule installed but group membership is not visible yet; log out and back in")
	}
	return nil
}
