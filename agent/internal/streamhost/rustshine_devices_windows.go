//go:build windows

package streamhost

import (
	"bufio"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// rustshineDeviceLineRe matches one row of
// `gamestream-server.exe --list-capture-devices` on Windows: confirmed
// columns, in order, are index (u32), device_name, adapter_name, vendor
// (hex u32), resolution ("{width}x{height}"). The exact whitespace/column
// separator wasn't pinned down beyond "plain text table" — this assumes
// whitespace-separated fields with the resolution as a trailing "WxH" token
// and should be checked against real output before relying on it.
var rustshineDeviceLineRe = regexp.MustCompile(`^\s*(\d+)\s+(.+?)\s+(\S+)\s+(0x[0-9a-fA-F]+)\s+(\d+)x(\d+)\s*$`)

// ListCaptureDevices shells out to gamestream-server.exe
// --list-capture-devices (implemented via capture_dxgi::enumerate_monitors)
// and parses each row. Key is left "" since Windows identifies monitors
// directly by index (via the "monitor_index" config key, see
// rustshine_config.go's SetOutputName) rather than by name correlation.
func (b *rustshineBackend) ListCaptureDevices() []CaptureDevice {
	binPath := b.BinaryPath()
	if binPath == "" {
		return nil
	}
	ctx, cancel := execTimeout(3 * time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binPath, "--list-capture-devices")
	configureRustshineProcess(cmd) // hide the console window this pops up
	out, err := cmd.Output()
	if err != nil {
		return nil
	}

	var devices []CaptureDevice
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	for sc.Scan() {
		m := rustshineDeviceLineRe.FindStringSubmatch(sc.Text())
		if m == nil {
			continue
		}
		width, _ := strconv.Atoi(m[5])
		height, _ := strconv.Atoi(m[6])
		devices = append(devices, CaptureDevice{
			OutputName:  m[1], // monitor_index expects the numeric index, stringified
			DisplayName: strings.TrimSpace(m[2]),
			Width:       width,
			Height:      height,
		})
	}
	return devices
}

// firstKmsCardPath: KMS/DRM is Linux-only — see rustshine_devices_linux.go.
func (b *rustshineBackend) firstKmsCardPath() string { return "" }
