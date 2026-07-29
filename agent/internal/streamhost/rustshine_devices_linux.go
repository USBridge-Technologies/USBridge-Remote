//go:build rustshine && linux

package streamhost

import (
	"bufio"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// kmsConnectorLineRe matches the KMS-section rows of
// `gamestream-server --list-capture-devices` (desktop/kms feature builds
// only): a /dev/dri/cardN device path followed by a connector name, e.g.
// "/dev/dri/card0  DP-2". The exact column separator/formatting wasn't
// pinned down further than "plain text table, columns device+name+formats
// for v4l2, plus a KMS section listing /dev/dri/cardN + connector names" —
// this regex is a best-effort first cut and should be checked against real
// `--list-capture-devices` output before relying on it.
var kmsConnectorLineRe = regexp.MustCompile(`(/dev/dri/card\d+)\s+(\S+)`)

// ListCaptureDevices shells out to gamestream-server --list-capture-devices
// and parses the KMS section for connector names, so capture correlates
// against the agent's own display.Connectors() enumeration the same way
// sunshineBackend's Linux ListCaptureDevices does — except gamestream-server
// selects a connector directly by name (kms_connector config key, see
// rustshine_config.go's SetOutputName) rather than by a numeric index, so
// both Key and OutputName are set to the same connector name here.
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
		m := kmsConnectorLineRe.FindStringSubmatch(sc.Text())
		if m == nil {
			continue
		}
		name := strings.TrimSpace(m[2])
		devices = append(devices, CaptureDevice{Key: name, OutputName: name})
	}
	return devices
}
