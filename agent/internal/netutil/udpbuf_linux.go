//go:build linux

package netutil

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// The kernel caps every socket's SO_RCVBUF at net.core.rmem_max, 212992 bytes on a stock
// install. moonlight-common-c asks for ~3 MB on the video socket and silently gets 208 KB --
// less than ONE PyroWave frame (300 KB - 1.4 MB at 1080p-4K). Any scheduling hiccup on the
// receive thread then overflows the buffer and the kernel drops the rest of the frame
// (UdpRcvbufErrors in `nstat`): a lost frame and a host/jitter spike every few seconds,
// whichever streamer is on the other end. Measured live: 344 packets dropped in 10 s at
// 300 Mbps. Raising the cap needs root once; a sysctl.d drop-in keeps it across reboots.
//
// UDPBufferSysctlPath is shared with USBridge-Remote/client's identical grant
// (internal/netutil/udpbuf_linux.go), so granting it from either app covers both.
const UDPBufferSysctlPath = "/etc/sysctl.d/90-usbridge-udp.conf"

// UDPBufferWanted is the receive-buffer cap the grant sets: room for a burst of several
// 4K PyroWave frames.
const UDPBufferWanted = 32 << 20

const rmemMaxPath = "/proc/sys/net/core/rmem_max"

// UDPBufferMax is the kernel's current SO_RCVBUF cap in bytes, 0 if it cannot be read.
func UDPBufferMax() int {
	b, err := os.ReadFile(rmemMaxPath)
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	return n
}

// UDPBufferOK reports whether the cap already fits a video burst (or cannot be read, in
// which case there is nothing useful to offer).
func UDPBufferOK() bool {
	n := UDPBufferMax()
	return n == 0 || n >= UDPBufferWanted
}

// EnsureUDPBuffer raises net.core.rmem_max / rmem_default now and installs the sysctl.d
// drop-in, through one pkexec password prompt.
func EnsureUDPBuffer() error {
	if _, err := exec.LookPath("pkexec"); err != nil {
		return fmt.Errorf("pkexec is not installed; run as root instead:\n  sysctl -w net.core.rmem_max=%d net.core.rmem_default=%d", UDPBufferWanted, UDPBufferWanted)
	}
	conf := fmt.Sprintf("# USBridge: room for whole video frames in the UDP receive buffer\nnet.core.rmem_max=%d\nnet.core.rmem_default=%d\n", UDPBufferWanted, UDPBufferWanted)
	script := fmt.Sprintf("printf '%%s' '%s' > %s && chmod 644 %s && sysctl -q -w net.core.rmem_max=%d net.core.rmem_default=%d",
		conf, UDPBufferSysctlPath, UDPBufferSysctlPath, UDPBufferWanted, UDPBufferWanted)
	out, err := exec.Command("pkexec", "/bin/sh", "-c", script).CombinedOutput()
	if err != nil {
		s := strings.TrimSpace(string(out))
		switch {
		case strings.Contains(s, "No authentication agent found"):
			return fmt.Errorf("no polkit authentication agent is running for this session, so pkexec cannot ask for the password")
		case strings.Contains(err.Error(), "exit status 126"):
			return fmt.Errorf("authentication was cancelled")
		case s != "":
			return fmt.Errorf("pkexec failed: %v (%s)", err, s)
		}
		return fmt.Errorf("pkexec failed: %v", err)
	}
	if !UDPBufferOK() {
		return fmt.Errorf("the sysctl was written but net.core.rmem_max is still %d", UDPBufferMax())
	}
	return nil
}
