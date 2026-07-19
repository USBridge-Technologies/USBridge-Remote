//go:build !darwin && !android

package api

import "net"

func buildDirectDialer(destHost string) *net.Dialer {
	// See the darwin variant's comment: forcing a LAN source IP on a
	// loopback-destined socket breaks it outright, since loopback traffic
	// was never going through a VPN interface to begin with.
	if isLoopbackHost(destHost) {
		return &net.Dialer{}
	}
	srcIP := findLANSourceIP(destHost)
	if srcIP == nil {
		return &net.Dialer{}
	}
	return &net.Dialer{
		LocalAddr: &net.TCPAddr{IP: srcIP, Port: 0},
	}
}
