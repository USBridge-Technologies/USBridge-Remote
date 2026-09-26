//go:build linux

package usbpass

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// whatHoldsPort identifies whatever process is bound to port, formatted as
// "<comm> (pid <n>)" -- parses `ss -ltnp`'s pid=<n>,comm="<name>" annotation,
// the Linux equivalent of the Windows netstat+tasklist diagnostic in
// portdiag_windows.go. Returns "" if the port isn't listening, `ss` isn't on
// PATH, or the process/user field isn't visible (unprivileged callers only
// see other users' PIDs with CAP_SYS_PTRACE/root) -- best-effort only.
func whatHoldsPort(port int) string {
	out, err := exec.Command("ss", "-ltnp").Output()
	if err != nil {
		return ""
	}
	suffix := ":" + strconv.Itoa(port)
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 || !strings.HasSuffix(fields[3], suffix) {
			continue
		}
		idx := strings.Index(line, "users:((")
		if idx < 0 {
			return ""
		}
		rest := line[idx+len("users:(("):]
		nameEnd := strings.Index(rest, "\"")
		if !strings.HasPrefix(rest, "\"") {
			return ""
		}
		rest = rest[1:]
		nameEnd = strings.Index(rest, "\"")
		if nameEnd < 0 {
			return ""
		}
		name := rest[:nameEnd]
		pidIdx := strings.Index(rest, "pid=")
		if pidIdx < 0 {
			return name
		}
		pidRest := rest[pidIdx+len("pid="):]
		commaIdx := strings.IndexAny(pidRest, ",)")
		if commaIdx < 0 {
			return name
		}
		return fmt.Sprintf("%s (pid %s)", name, pidRest[:commaIdx])
	}
	return ""
}
