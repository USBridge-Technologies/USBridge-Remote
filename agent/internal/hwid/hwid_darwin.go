//go:build darwin

package hwid

import (
	"fmt"
	"os/exec"
	"strings"
)

// rawMachineID on macOS reads IOPlatformUUID out of the IOPlatformExpertDevice
// entry in the I/O Registry via `ioreg` (the standard, no-extra-permissions
// way to read it -- no cgo/IOKit binding needed, and ioreg ships with every
// macOS install). Unlike the Windows/Linux identifiers above, IOPlatformUUID
// is tied to the physical logic board, not the OS install: it survives a
// full macOS reinstall (a real difference from the other two platforms,
// worth knowing when reasoning about "what does reinstalling get you" --
// on macOS, nothing, by design; Apple documents this as their recommended
// hardware identifier).
func rawMachineID() (id string, source string, err error) {
	out, err := exec.Command("ioreg", "-rd1", "-c", "IOPlatformExpertDevice").Output()
	if err != nil {
		return "", "", fmt.Errorf("run ioreg: %w", err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, `"IOPlatformUUID"`) {
			continue
		}
		// Line shape: "IOPlatformUUID" = "XXXXXXXX-XXXX-XXXX-XXXX-XXXXXXXXXXXX"
		idx := strings.Index(line, "=")
		if idx < 0 {
			continue
		}
		val := strings.TrimSpace(line[idx+1:])
		val = strings.Trim(val, `"`)
		if val != "" {
			return val, "macos-platform-uuid", nil
		}
	}
	return "", "", fmt.Errorf("IOPlatformUUID not found in ioreg output")
}
