//go:build rustshine && linux

package streamhost

import (
	"bufio"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// kmsCardLineRe matches the KMS-section rows of `gamestream-server
// --list-capture-devices` (desktop/kms feature builds only): a
// /dev/dri/cardN device path followed by a comma-separated connector list,
// confirmed against real output as:
//
//	/dev/dri/card1       connectors: HDMI-A-1, DP-2
//
// (an earlier version of this regex assumed one connector per line with no
// "connectors:" label — checked against actual --list-capture-devices
// output now, which this replaces.)
var kmsCardLineRe = regexp.MustCompile(`(/dev/dri/card\d+)\s+connectors:\s*(.+)`)

// ListCaptureDevices shells out to gamestream-server --list-capture-devices
// and parses the KMS section for connector names, so capture correlates
// against the agent's own display.Connectors() enumeration the same way
// sunshineBackend's Linux ListCaptureDevices does — except gamestream-server
// selects a connector by name AND requires the card path that connector
// lives on (both `kms_connector` and `adapter_name` config keys — see
// rustshine_config.go's SetOutputName), rather than a single numeric index.
// OutputName packs both into "cardPath|connector" so SetOutputName has
// everything it needs from the one string CaptureDevice carries.
func (b *rustshineBackend) ListCaptureDevices() []CaptureDevice {
	binPath := b.BinaryPath()
	if binPath == "" {
		return nil
	}
	ctx, cancel := execTimeout(3 * time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, binPath, "--list-capture-devices").Output()
	if err != nil {
		return nil
	}

	var devices []CaptureDevice
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	for sc.Scan() {
		m := kmsCardLineRe.FindStringSubmatch(sc.Text())
		if m == nil {
			continue
		}
		card := strings.TrimSpace(m[1])
		for _, name := range strings.Split(m[2], ",") {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			devices = append(devices, CaptureDevice{Key: name, OutputName: card + "|" + name})
		}
	}
	return devices
}

// firstKmsCardPath returns the first /dev/dri/cardN this build's
// gamestream-server reports as having any connected KMS output, or "" if
// none (no desktop-capable card, or --list-capture-devices failed). Used to
// auto-fill `adapter_name` when capture mode is switched to "kms" without
// the user having explicitly picked a specific output via SetOutputName —
// gamestream-server's own --capture-device default (/dev/video0, the V4L2
// SBC default) is never a valid KMS card path, so leaving adapter_name
// unset after switching to kms silently fails capture with an I/O error
// opening a nonexistent device.
func (b *rustshineBackend) firstKmsCardPath() string {
	devices := b.ListCaptureDevices()
	if len(devices) == 0 {
		return ""
	}
	card, _, ok := strings.Cut(devices[0].OutputName, "|")
	if !ok {
		return ""
	}
	return card
}
