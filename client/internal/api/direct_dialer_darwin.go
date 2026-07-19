//go:build darwin

package api

import (
	"net"
	"syscall"

	"github.com/sirupsen/logrus"
)

// IP_BOUND_IF is a macOS socket option that forces a socket to use a specific
// network interface regardless of the routing table or VPN Network Extensions.
// When Tailscale (or any NEPacketTunnelProvider) is in a transitional state
// (NeedsLogin, being restarted, etc.) it can intercept bundle-launched app
// traffic and return EHOSTUNREACH. Setting IP_BOUND_IF to the LAN interface
// index bypasses this interception for the socket.
const ipBoundIF = 25

// buildDirectDialer returns a net.Dialer whose sockets are pinned to the best
// LAN interface for reaching destHost. On macOS this uses IP_BOUND_IF
// (setsockopt IPPROTO_IP, 25) so traffic goes through the physical LAN
// interface even when a VPN Network Extension is active and in a bad state.
func buildDirectDialer(destHost string) *net.Dialer {
	// Loopback destinations never go through a VPN Network Extension or
	// physical interface in the first place, so IP_BOUND_IF has nothing to
	// route around here -- and forcing one on breaks it outright: binding a
	// loopback-destined socket to a physical LAN interface makes the kernel
	// reject the connect with EADDRNOTAVAIL ("can't assign requested
	// address"), since loopback traffic can't be routed out through a
	// physical NIC. This is exactly what made "protocol=direct" connections
	// to 127.0.0.1 fail unconditionally before this check existed.
	if isLoopbackHost(destHost) {
		return &net.Dialer{}
	}

	// Find the LAN interface (prefer the one whose subnet contains destHost).
	ifaceIdx, ifaceName := findLANInterface(destHost)
	if ifaceIdx < 0 {
		return &net.Dialer{}
	}

	capturedIdx := ifaceIdx
	capturedName := ifaceName
	logrus.Debugf("🔗 [Direct] Will bind sockets to %s (index %d) via IP_BOUND_IF", capturedName, capturedIdx)

	return &net.Dialer{
		Control: func(network, address string, c syscall.RawConn) error {
			return c.Control(func(fd uintptr) {
				if err := syscall.SetsockoptInt(int(fd), syscall.IPPROTO_IP, ipBoundIF, capturedIdx); err != nil {
					logrus.Debugf("⚠️ [Direct] IP_BOUND_IF setsockopt failed (falling back to default routing): %v", err)
				}
			})
		},
	}
}

// findLANInterface finds the index and name of the best network interface
// for directly reaching destHost, excluding Tailscale (100.64/10) and loopback.
func findLANInterface(destHost string) (int, string) {
	destIP := net.ParseIP(stripPort(destHost))
	tailscale := &net.IPNet{IP: net.ParseIP("100.64.0.0").To4(), Mask: net.CIDRMask(10, 32)}

	ifaces, err := net.Interfaces()
	if err != nil {
		return -1, ""
	}

	var fallbackIdx int = -1
	var fallbackName string

	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			ip := ipNet.IP.To4()
			if ip == nil || tailscale.Contains(ip) || ip[0] == 169 {
				continue
			}
			// Best: the interface whose subnet contains the destination.
			if destIP != nil && ipNet.Contains(destIP) {
				return iface.Index, iface.Name
			}
			if fallbackIdx < 0 {
				fallbackIdx = iface.Index
				fallbackName = iface.Name
			}
		}
	}
	return fallbackIdx, fallbackName
}

func stripPort(hostport string) string {
	h, _, err := net.SplitHostPort(hostport)
	if err != nil {
		return hostport
	}
	return h
}
