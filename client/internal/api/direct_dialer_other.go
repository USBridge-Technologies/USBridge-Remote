//go:build !darwin

package api

import "net"

func buildDirectDialer(destHost string) *net.Dialer {
	srcIP := findLANSourceIP(destHost)
	if srcIP == nil {
		return &net.Dialer{}
	}
	return &net.Dialer{
		LocalAddr: &net.TCPAddr{IP: srcIP, Port: 0},
	}
}
