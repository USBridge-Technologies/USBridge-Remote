package service

// The Android interface list (MainActivity.getInterfacesAsString via JNI),
// parsed for tailscale's netmon -- Go's net.Interfaces() gets nothing from
// netlink on Android 11+. Untagged so it can be tested on the host.

import (
	"fmt"
	"net"
	"net/netip"
	"strings"

	"tailscale.com/net/netmon"
)

func parseAndroidInterfaces(raw string) []netmon.Interface {
	var ifaces []netmon.Interface
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Split(line, "|")
		if len(parts) != 2 {
			continue
		}

		var (
			name         string
			index, mtu   int
			up           bool
			broadcast    bool
			loopback     bool
			pointToPoint bool
			multicast    bool
		)
		if _, err := fmt.Sscanf(parts[0], "%s %d %d %t %t %t %t %t", &name, &index, &mtu, &up, &broadcast, &loopback, &pointToPoint, &multicast); err != nil {
			continue
		}

		iface := netmon.Interface{
			Interface: &net.Interface{
				Name:  name,
				Index: index,
				MTU:   mtu,
			},
			AltAddrs: []net.Addr{},
		}
		if up {
			iface.Flags |= net.FlagUp
		}
		if broadcast {
			iface.Flags |= net.FlagBroadcast
		}
		if loopback {
			iface.Flags |= net.FlagLoopback
		}
		if pointToPoint {
			iface.Flags |= net.FlagPointToPoint
		}
		if multicast {
			iface.Flags |= net.FlagMulticast
		}

		for _, addrText := range strings.Fields(parts[1]) {
			addrText = strings.TrimSpace(addrText)
			if addrText == "" {
				continue
			}
			addrText = strings.ReplaceAll(addrText, "%"+name, "")
			// The interface's own address with its prefix length, e.g.
			// 192.168.1.103/24 -- NOT net.ParseCIDR's second result, which
			// is the network (192.168.1.0/24): tailscale then advertised
			// 192.168.1.0:<port> as this device's LAN endpoint, a peer
			// behind another NAT on this LAN could never reach it, and the
			// connection stayed on a DERP relay (same subnet still worked,
			// from the pong's source address).
			if pfx, err := netip.ParsePrefix(addrText); err == nil {
				iface.AltAddrs = append(iface.AltAddrs, prefixToIPNet(pfx))
				continue
			}
			if addr, err := netip.ParseAddr(addrText); err == nil {
				bits := 32
				if addr.Is6() {
					bits = 128
				}
				pfx := netip.PrefixFrom(addr, bits)
				iface.AltAddrs = append(iface.AltAddrs, prefixToIPNet(pfx))
			}
		}
		ifaces = append(ifaces, iface)
	}
	return ifaces
}

func prefixToIPNet(pfx netip.Prefix) *net.IPNet {
	addr := pfx.Addr()
	if addr.Is4() {
		ip := addr.As4()
		return &net.IPNet{IP: net.IP(ip[:]), Mask: net.CIDRMask(pfx.Bits(), 32)}
	}
	ip := addr.As16()
	return &net.IPNet{IP: net.IP(ip[:]), Mask: net.CIDRMask(pfx.Bits(), 128)}
}
