//go:build linux

package vdisplay

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
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
// A running compositor keeps /dev/dri/cardN of vkms open, so `modprobe -r`
// fails with "Module vkms is in use". The virtual monitor is then removed by
// forcing its connector off (and back with "detect") through `tee` on the
// connector's own sysfs status file -- the rule allows tee only for that one
// path pattern, plus a synthetic "change" uevent on the card: the kernel does
// not announce a forced status change by itself, so without it KWin keeps
// showing the output as connected.
//
// Scoped on purpose: modprobe [-r] vkms, and tee on
// /sys/class/drm/cardN-Virtual-M/status -- no other module, file or program.
const polkitRuleContent = `polkit.addRule(function(action, subject) {
    if (action.id != "org.freedesktop.policykit.exec") return;
    if (!subject.isInGroup("` + GroupName + `")) return;
    var prog = action.lookup("program");
    var cmd = action.lookup("command_line") || "";
    if (/^\/(usr\/)?s?bin\/modprobe$/.test(prog) && /^\S+ (-r )?vkms$/.test(cmd)) return polkit.Result.YES;
    if (/^\/(usr\/)?s?bin\/tee$/.test(prog) && /^\S+ \/sys\/class\/drm\/card[0-9]+(-Virtual-[0-9]+\/status|\/uevent)$/.test(cmd)) return polkit.Result.YES;
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

func vkmsConnectorStatusFiles() []string {
	files, _ := filepath.Glob("/sys/class/drm/card*-Virtual-*/status")
	return files
}

func pkexecTee(path, value string) error {
	cmd := exec.Command("pkexec", "tee", path)
	cmd.Stdin = strings.NewReader(value + "\n")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("tee %s: %v (%s)", path, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// writeConnectorStatus sets the connector's forced status and then emits a
// "change" uevent on its card so the compositor re-probes and adds/drops the
// output right away.
func writeConnectorStatus(path, value string) error {
	if err := pkexecTee(path, value); err != nil {
		return err
	}
	card := filepath.Join(filepath.Dir(filepath.Dir(path)), strings.SplitN(filepath.Base(filepath.Dir(path)), "-", 2)[0], "uevent")
	return pkexecTee(card, "change")
}

// Unload removes the virtual monitor. It tries to unload vkms; when the
// compositor still holds the card ("Module vkms is in use", the normal case on
// a desktop) it forces every vkms connector off instead, so the monitor
// disappears from the desktop and from capture lists while the module stays.
// Only runs with the passwordless grant in place, so a delete never pops a
// polkit dialog; without it this is a no-op.
func Unload() error {
	if _, err := os.Stat("/sys/module/vkms"); err != nil {
		return nil
	}
	if !AccessGranted() {
		return nil
	}
	out, err := exec.Command("pkexec", "modprobe", "-r", "vkms").CombinedOutput()
	if err == nil {
		return nil
	}
	log.Printf("[vdisplay] modprobe -r vkms: %v (%s); forcing connectors off instead", err, strings.TrimSpace(string(out)))
	var firstErr error
	for _, f := range vkmsConnectorStatusFiles() {
		if e := writeConnectorStatus(f, "off"); e != nil && firstErr == nil {
			firstErr = e
		}
	}
	return firstErr
}

// Revive undoes Unload's forced-off state (connector status back to "detect")
// so the streamer finds a connected vkms connector again. No-op when the
// connector is not forced off or the grant is missing.
func Revive() error {
	if !AccessGranted() {
		return nil
	}
	var firstErr error
	for _, f := range vkmsConnectorStatusFiles() {
		b, err := os.ReadFile(f)
		if err != nil || strings.TrimSpace(string(b)) == "connected" {
			continue
		}
		if e := writeConnectorStatus(f, "detect"); e != nil && firstErr == nil {
			firstErr = e
		}
	}
	return firstErr
}
