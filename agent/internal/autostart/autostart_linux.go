//go:build linux

package autostart

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"strings"
)

// unitName/unitPath: a system-wide (not --user) systemd unit. This is
// deliberate: KMS screen capture already requires one-time root elevation
// via pkexec (see internal/permissions.RequestKMSCapture) to grant
// CAP_SYS_ADMIN to sunshine_capexec, so the user has already agreed to a
// polkit prompt for this app — reusing that same pkexec path to install a
// system unit costs nothing extra, and unlike a `systemd --user` unit (which
// only starts once systemd's user instance for that UID is running —
// normally at login, or earlier only if `loginctl enable-linger` was set) a
// system unit starts at boot unconditionally, before any graphical session
// exists. That matters for KMS capture specifically: it reads frames
// straight from DRM/KMS, no compositor or portal required, so it's the one
// capture mode that actually can come up before a display manager does.
const (
	unitName = "usbridge-agent.service"
	unitPath = "/etc/systemd/system/" + unitName
)

// systemdQuote quotes a single ExecStart= argument per systemd's unit-file
// quoting rules (C-style double-quoted string) so a path containing spaces
// still parses as one argument.
func systemdQuote(s string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `"`, `\"`, `$`, `\$`)
	return `"` + replacer.Replace(s) + `"`
}

func IsEnabled() bool {
	out, err := exec.Command("systemctl", "is-enabled", unitName).CombinedOutput()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "enabled"
}

func Enable() error {
	exe, args, err := LaunchTarget()
	if err != nil {
		return err
	}
	u, err := user.Current()
	if err != nil {
		return fmt.Errorf("resolve current user: %w", err)
	}

	execParts := make([]string, 0, len(args)+1)
	execParts = append(execParts, systemdQuote(exe))
	for _, a := range args {
		execParts = append(execParts, systemdQuote(a))
	}

	// XDG_RUNTIME_DIR/DBUS_SESSION_BUS_ADDRESS are normally set by pam_systemd
	// on interactive login. A system unit (see the comment above on why this
	// is a system rather than --user unit) gets neither, so without setting
	// them explicitly here, every child process (this agent's own `pactl`
	// calls in internal/audio, and Sunshine itself, which inherits this
	// process's environment — see sunshine.Process.Start) can never reach the
	// user's PipeWire/PulseAudio session: not just "before login", but
	// permanently, since a unit's Environment= is fixed at service start and
	// never picks up a session that comes up later. loginctl enable-linger
	// is what actually gets PipeWire/WirePlumber running at boot without a
	// physical login in the first place; the two together make audio_sink
	// enumeration and Sunshine's own audio capture work headlessly.
	unit := fmt.Sprintf(`[Unit]
Description=USBridge Agent
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=%s
Environment=HOME=%s
Environment=APPIMAGE_EXTRACT_AND_RUN=1
Environment=XDG_RUNTIME_DIR=/run/user/%s
Environment=DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/%s/bus
ExecStart=%s
Restart=on-failure
RestartSec=2

[Install]
WantedBy=multi-user.target
`, u.Username, u.HomeDir, u.Uid, u.Uid, strings.Join(execParts, " "))

	tmp, err := os.CreateTemp("", "usbridge-agent-*.service")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(unit); err != nil {
		tmp.Close()
		return err
	}
	tmp.Close()

	// tmp.Name() and unitPath are both Go/constant-generated paths with no
	// spaces or shell metacharacters, so no extra shell-quoting is needed
	// here — only the ExecStart= line inside the unit itself (systemdQuote,
	// above) handles a path that might contain them.
	// enable-linger starts the user's own systemd instance (and with it
	// PipeWire/WirePlumber, which the audio_sink lookups and Sunshine's audio
	// capture both depend on) at boot, without requiring an interactive
	// login — otherwise XDG_RUNTIME_DIR above points at a session that never
	// comes up on a headless boot.
	script := fmt.Sprintf(
		"install -m 0644 %s %s && systemctl daemon-reload && systemctl enable --now %s && loginctl enable-linger %s",
		tmp.Name(), unitPath, unitName, systemdQuote(u.Username),
	)
	cmd := exec.Command("pkexec", "/bin/sh", "-c", script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("install systemd service: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func Disable() error {
	script := fmt.Sprintf(
		"systemctl disable --now %s >/dev/null 2>&1; rm -f %s && systemctl daemon-reload",
		unitName, unitPath,
	)
	cmd := exec.Command("pkexec", "/bin/sh", "-c", script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("remove systemd service: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}
