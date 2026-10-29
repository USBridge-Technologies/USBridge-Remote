//go:build linux && !android

package netutil

import (
	"fmt"
	"strings"
	"testing"
)

func TestUDPBufferCommandWritesTheGrantsDropIn(t *testing.T) {
	cmd := UDPBufferCommand()
	t.Log(cmd)
	for _, want := range []string{
		"sudo sh -c",
		UDPBufferSysctlPath,
		fmt.Sprintf("net.core.rmem_max=%d", UDPBufferWanted),
		fmt.Sprintf("net.core.rmem_default=%d", UDPBufferWanted),
		"sysctl -q -p",
	} {
		if !strings.Contains(cmd, want) {
			t.Errorf("command lacks %q", want)
		}
	}
}
