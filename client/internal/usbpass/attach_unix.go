//go:build unix

package usbpass

import (
	"bytes"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

func setAttachProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func killAttachProcess(p *os.Process) {
	if p == nil {
		return
	}
	// Negative PID = process group (Setpgid at start).
	_ = syscall.Kill(-p.Pid, syscall.SIGKILL)
	_ = p.Kill()
}

// killOrphanClientBrokers reaps leftover --role client brokers from previous
// AppImage instances / remounts that escaped StopAttach (common cause of
// stacked VHCI "Invalid Device Descriptor" ports on the agent).
func killOrphanClientBrokers() {
	out, err := exec.Command("pgrep", "-f", `usbridge-usb-broker.*--role[[:space:]]+client`).Output()
	if err != nil {
		return
	}
	self := os.Getpid()
	for _, line := range bytes.Split(out, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		pid, err := strconv.Atoi(string(line))
		if err != nil || pid <= 1 || pid == self {
			continue
		}
		// Only kill brokers whose cmdline still looks like ours (belt+suspenders).
		cmdline, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/cmdline")
		if err != nil {
			continue
		}
		s := string(bytes.ReplaceAll(cmdline, []byte{0}, []byte{' '}))
		if !strings.Contains(s, "usbridge-usb-broker") || !strings.Contains(s, "--role") || !strings.Contains(s, "client") {
			continue
		}
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}
}
