//go:build darwin

package autostart

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const launchAgentLabel = "io.usbridge.agent"

func plistPath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(homeDir, "Library", "LaunchAgents", launchAgentLabel+".plist"), nil
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

func Enable() error {
	exe, args, err := LaunchTarget()
	if err != nil {
		return err
	}
	path, err := plistPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	var argsXML strings.Builder
	argsXML.WriteString("\t\t<string>" + escapeXML(exe) + "</string>\n")
	for _, a := range args {
		argsXML.WriteString("\t\t<string>" + escapeXML(a) + "</string>\n")
	}

	content := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
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
`, launchAgentLabel, argsXML.String())

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return err
	}

	// Activate immediately instead of waiting for the next login. Ignore the
	// error: launchctl exits non-zero if it's already loaded, which isn't a
	// real failure here.
	_ = exec.Command("launchctl", "load", "-w", path).Run()
	return nil
}

func Disable() error {
	path, err := plistPath()
	if err != nil {
		return err
	}
	_ = exec.Command("launchctl", "unload", "-w", path).Run()
	err = os.Remove(path)
	if err != nil && os.IsNotExist(err) {
		return nil
	}
	return err
}
