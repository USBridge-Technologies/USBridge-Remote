package netutil

import (
	"net"
	"testing"
)

func TestInterfacePriority(t *testing.T) {
	cases := []struct {
		name  string
		flags net.Flags
		want  int
	}{
		{"docker0", 0, 2},
		{"enp2s0", 0, 0},
		{"wlp3s0", 0, 0},
		{"eth0", 0, 0},
		{"wlan0", 0, 0},
		{"en0", 0, 0},
		{"Ethernet", 0, 0},
		{"Ethernet 2", 0, 0},
		{"Wi-Fi", 0, 0},
		{"tailscale0", net.FlagPointToPoint, 2},
		{"vEthernet (WSL)", 0, 2},
		{"utun4", net.FlagPointToPoint, 2},
		{"veth1234", 0, 2},
		{"br-abc123", 0, 2},
		{"virbr0", 0, 2},
		{"unknownadapter0", 0, 1},
	}
	for _, c := range cases {
		if got := interfacePriority(c.name, c.flags); got != c.want {
			t.Errorf("interfacePriority(%q, %v) = %d, want %d", c.name, c.flags, got, c.want)
		}
	}
}
