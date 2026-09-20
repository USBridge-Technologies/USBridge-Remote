//go:build linux

package usbpass

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/user"
	"strings"
)

const (
	// AttachGroupName owns the polkit exemption below. Membership is the
	// one-time, persistent grant the GUI's "USB" permission chip installs.
	AttachGroupName = "usbridge-usbip"
	polkitRulePath  = "/etc/polkit-1/rules.d/49-usbridge-usbip.rules"
)

// polkitRuleContent lets members of AttachGroupName run exactly
// `usbip attach|detach|port` as root through pkexec without a password
// prompt. The rust-shine usb-broker shells out to `pkexec usbip ...` on every
// attach/detach (bin usb-broker, crates/usb-passthrough/src/vhci_linux.rs),
// and vhci-hcd's sysfs attach/detach files are root-only, so without this
// every device connect/disconnect pops a polkit password dialog.
//
// Scoped on purpose: only a usbip binary in a root-owned system location
// (never a user-writable path) and only the three subcommands the broker
// uses -- not `bind`/`unbind`, and not any other program.
const polkitRuleContent = `polkit.addRule(function(action, subject) {
    if (action.id != "org.freedesktop.policykit.exec") return;
    if (!subject.isInGroup("` + AttachGroupName + `")) return;
    var prog = action.lookup("program");
    if (!/^\/(usr\/)?s?bin\/usbip$/.test(prog) &&
        !/^\/usr\/lib\/linux-tools[^\/]*\/[^\/]+\/usbip$/.test(prog)) return;
    var cmd = action.lookup("command_line") || "";
    if (/^\S+ (attach|detach|port)( |$)/.test(cmd)) return polkit.Result.YES;
});
`

func shellQuote(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, `$`, `\$`)
	return `"` + r.Replace(s) + `"`
}

// buildAttachGrantScript builds the /bin/sh -c script run under pkexec:
// create the group, add the user, install the polkit rule. Pure so its shape
// is testable without root.
func buildAttachGrantScript(tmpRule, currentUser string) string {
	return fmt.Sprintf(
		"(getent group %[1]s >/dev/null || groupadd %[1]s) && usermod -aG %[1]s %[2]s && "+
			"install -d -m 0755 /etc/polkit-1/rules.d && install -m 0644 %[3]s %[4]s",
		AttachGroupName, currentUser, tmpRule, polkitRulePath,
	)
}

// AttachAccessGranted reports whether the polkit rule is installed in its
// current form and the running user is in AttachGroupName (checked against
// the user database, which is what polkit itself consults -- so it holds
// immediately, without a re-login).
func AttachAccessGranted() bool {
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
	g, err := user.LookupGroup(AttachGroupName)
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

// GrantAttachAccess installs the passwordless-usbip polkit rule (one pkexec
// prompt, ever) and adds the current user to AttachGroupName.
func (s *Service) GrantAttachAccess() error {
	if AttachAccessGranted() {
		return nil
	}
	if _, err := exec.LookPath("pkexec"); err != nil {
		return fmt.Errorf("pkexec is not installed; install it (e.g. \"apt install pkexec\") and try again")
	}
	tmp, err := os.CreateTemp("", "usbridge-usbip-*.rules")
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
	out, err := exec.Command("pkexec", "/bin/sh", "-c", buildAttachGrantScript(tmp.Name(), currentUser)).CombinedOutput()
	log.Printf("[usbpass] attach grant exit=%v output=%q", err, string(out))
	if err != nil {
		if strings.Contains(err.Error(), "exit status 126") {
			return fmt.Errorf("authentication was cancelled or dismissed")
		}
		return fmt.Errorf("usb access grant: %v (%s)", err, strings.TrimSpace(string(out)))
	}
	if !AttachAccessGranted() {
		return fmt.Errorf("polkit rule installed but group membership is not visible yet; log out and back in")
	}
	return nil
}
