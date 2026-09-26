//go:build windows

package usbpass

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// whatHoldsPort identifies whatever process is bound to port, formatted as
// "<image name> (pid <n>)" -- shells out to netstat then tasklist, the same
// two commands a human would run by hand to answer "who has this port"
// (confirmed live: this exact sequence found WsToastNotification.exe
// squatting on the broker's URB port). Returns "" if the port isn't bound,
// or on any parse/exec failure -- this is a best-effort diagnostic, never
// worth failing the caller over.
func whatHoldsPort(port int) string {
	out, err := exec.Command("netstat", "-ano").Output()
	if err != nil {
		return ""
	}
	needle := fmt.Sprintf(":%d ", port)
	var pid string
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.Contains(line, "LISTENING") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 || !strings.Contains(fields[1], needle[:len(needle)-1]) {
			continue
		}
		// Confirm the local-address field's port (not a coincidental
		// substring match elsewhere in the line) actually ends the address.
		if !strings.HasSuffix(fields[1], ":"+strconv.Itoa(port)) {
			continue
		}
		pid = fields[len(fields)-1]
		break
	}
	if pid == "" {
		return ""
	}
	tl, err := exec.Command("tasklist", "/FI", "PID eq "+pid, "/NH", "/FO", "CSV").Output()
	if err != nil {
		return "pid " + pid
	}
	line := strings.TrimSpace(strings.SplitN(string(tl), "\n", 2)[0])
	fields := strings.Split(line, "\",\"")
	if len(fields) == 0 {
		return "pid " + pid
	}
	name := strings.Trim(fields[0], "\"")
	if name == "" {
		return "pid " + pid
	}
	return fmt.Sprintf("%s (pid %s)", name, pid)
}
