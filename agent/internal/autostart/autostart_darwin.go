//go:build darwin

package autostart

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const launchAgentLabel = "io.usbridge.agent"

// trayLaunchAgentLabel is a second, separate LaunchAgent that launches the
// same binary with --tray instead of --headless -- unlike Linux/systemd,
// launchd LaunchAgents already run as the logged-in user by definition (no
// LocalSystem-style profile mismatch is possible here), so this is purely
// an additive convenience: a visible tray icon for the headless engine the
// primary LaunchAgent above starts, with no elevation or state-dir
// alignment concerns at all.
const trayLaunchAgentLabel = "io.usbridge.agent.tray"

func plistPath() (string, error) {
	return launchAgentPath(launchAgentLabel)
}

func trayPlistPath() (string, error) {
	return launchAgentPath(trayLaunchAgentLabel)
}

func launchAgentPath(label string) (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(homeDir, "Library", "LaunchAgents", label+".plist"), nil
}

func IsEnabled() bool {
	path, err := plistPath()
	if err != nil {
		return false
	}
	_, err = os.Stat(path)
	return err == nil
}

func escapeXML(s string) string {
	replacer := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	return replacer.Replace(s)
}

// launchAgentContent renders a minimal RunAtLoad LaunchAgent plist that
// execs exe with args -- shared by the primary (--headless engine) and tray
// (--tray helper) LaunchAgents, which differ only in label and arguments.
func launchAgentContent(label, exe string, args []string) string {
	var argsXML strings.Builder
	argsXML.WriteString("\t\t<string>" + escapeXML(exe) + "</string>\n")
	for _, a := range args {
		argsXML.WriteString("\t\t<string>" + escapeXML(a) + "</string>\n")
	}

	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>%s</string>
	<key>ProgramArguments</key>
	<array>
%s	</array>
	<key>RunAtLoad</key>
	<true/>
</dict>
</plist>
`, label, argsXML.String())
}

func writeLaunchAgent(label, path, content string, activateNow bool) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return err
	}
	if activateNow {
		// Activate immediately instead of waiting for the next login. Ignore
		// the error: launchctl exits non-zero if it's already loaded, which
		// isn't a real failure here. "-w" also clears launchd's persistent
		// per-label Disabled override (see below) as a side effect.
		_ = exec.Command("launchctl", "load", "-w", path).Run()
	} else {
		// Skipping "load -w" to avoid an immediate start must not skip
		// clearing the Disabled override that "-w" would otherwise clear:
		// removeLaunchAgent's "unload -w" (below) persists Disabled=true
		// for this label in launchd's overrides database, independent of
		// the plist file. If a label was ever disabled that way and later
		// re-enabled through this branch, the override survives even a
		// real reboot+login -- launchd's automatic ~/Library/LaunchAgents
		// scan silently skips disabled labels regardless of RunAtLoad or
		// the plist being perfectly valid. "launchctl enable" clears the
		// override without loading or starting anything, so the next real
		// login's scan picks it up normally.
		_ = exec.Command("launchctl", "enable", fmt.Sprintf("gui/%d/%s", os.Getuid(), label)).Run()
	}
	return nil
}

func removeLaunchAgent(path string) error {
	_ = exec.Command("launchctl", "unload", "-w", path).Run()
	err := os.Remove(path)
	if err != nil && os.IsNotExist(err) {
		return nil
	}
	return err
}

func Enable() error {
	exe, args, err := LaunchTarget()
	if err != nil {
		return err
	}
	path, err := plistPath()
	if err != nil {
		return err
	}
	if err := writeLaunchAgent(launchAgentLabel, path, launchAgentContent(launchAgentLabel, exe, args), true); err != nil {
		return err
	}

	// Best-effort, non-fatal: the primary LaunchAgent above is the real
	// autostart guarantee (the engine survives without this). This just
	// gives it a visible tray icon at login -- see trayLaunchAgentLabel's
	// doc comment. No elevation or state-dir alignment needed: both
	// LaunchAgents already run as this same logged-in user.
	//
	// activateNow=false here, unlike the primary LaunchAgent above: Enable()
	// is only ever reachable from a running GUI (the tray menu or the
	// settings window), which already owns a visible Dock/tray icon. Loading
	// this plist immediately would launchd-start a second --tray process
	// right now, which would see the engine's admin socket already up and
	// attach its own duplicate thin-client GUI (see Start/runThinClientGUI)
	// -- a second Dock+tray icon alongside the one already on screen. macOS
	// picks up ~/Library/LaunchAgents plists on its own at the next real
	// login, so skipping the immediate activation only defers this helper's
	// first appearance to that login instead of losing it.
	if trayPath, err := trayPlistPath(); err == nil {
		if err := writeLaunchAgent(trayLaunchAgentLabel, trayPath, launchAgentContent(trayLaunchAgentLabel, exe, []string{"--tray"}), false); err != nil {
			log.Printf("[autostart] warning: could not install tray LaunchAgent: %v", err)
		}
	}
	return nil
}

func Disable() error {
	path, err := plistPath()
	if err != nil {
		return err
	}
	if trayPath, trayErr := trayPlistPath(); trayErr == nil {
		if err := removeLaunchAgent(trayPath); err != nil {
			log.Printf("[autostart] warning: could not remove tray LaunchAgent: %v", err)
		}
	}
	return removeLaunchAgent(path)
}

// RefreshX11SessionEnv is a Linux/SDDM-only concept (see its doc comment on
// the linux build) -- no-op everywhere else.
func RefreshX11SessionEnv() {}

// EnsureDisplayActive is a Linux/X11-only concept (see its doc comment on
// the linux build) -- no-op everywhere else.
func EnsureDisplayActive() {}
