// Package netutil picks a LAN-facing IPv4 address to advertise for local
// pairing (QR codes, quick-connect links).
package netutil

import (
	"net"
	"sort"
	"strings"
)

// virtualNamePatterns match interface names that are never a real wired/Wi-Fi
// uplink: container bridges/veths (docker, podman, cni, flannel, weave),
// hypervisor/VM switches (virbr, vmnet, vbox, vnic, hyper-v, vethernet — the
// Windows Hyper-V/WSL virtual switch name contains "ethernet" as a
// substring, so it must be excluded before the "ethernet" allow-pattern
// below is checked), and VPN/tunnel adapters (tailscale, wireguard/wg, tun,
// tap, zerotier/zt, ppp, utun — macOS VPN tunnels). Matched with
// strings.Contains so both short Linux interface names (docker0) and the
// longer human-readable Windows/macOS adapter names (vEthernet (WSL)) hit.
var virtualNamePatterns = []string{
	"docker", "veth", "virbr", "vmnet", "vbox", "vnic", "hyper-v",
	"tailscale", "wireguard", "wg", "tun", "tap", "zerotier", "zt",
	"podman", "cni", "flannel", "weave", "utun", "ppp",
	"bridge", "br-", "awdl", "llw", "gif", "stf", "npcap",
}

// wiredWifiPrefixes match real Ethernet/Wi-Fi adapter names across
// platforms: Linux predictable names (enp2s0, eno1, ens33, eth0, wlan0,
// wlp3s0, wlo1), macOS (en0, en1, ...), and Windows friendly names
// ("Ethernet", "Ethernet 2", "Wi-Fi", "Wi-Fi 3").
var wiredWifiPrefixes = []string{"eth", "en", "wlan", "wl", "wi-fi", "wifi", "ethernet"}

// interfacePriority scores an interface for how likely it is to be the
// machine's real LAN uplink. Lower is preferred: 0 = known wired/Wi-Fi
// adapter, 1 = unrecognized (kept as a fallback), 2 = known virtual/VPN/
// container interface, always tried last.
func interfacePriority(name string, flags net.Flags) int {
	lower := strings.ToLower(name)
	for _, p := range virtualNamePatterns {
		if strings.Contains(lower, p) {
			return 2
		}
	}
	// VPN/tunnel adapters are point-to-point almost universally (Tailscale,
	// WireGuard, utun, PPP); real Ethernet/Wi-Fi/bridge links are not. This
	// catches VPN adapters using names we don't otherwise recognize.
	if flags&net.FlagPointToPoint != 0 {
		return 2
	}
	for _, p := range wiredWifiPrefixes {
		if strings.HasPrefix(lower, p) {
			return 0
		}
	}
	return 1
}

// PreferredIPv4 returns the IPv4 address most likely to be reachable from
// another device on the same LAN — preferring real Ethernet/Wi-Fi adapters
// over container bridges (docker0), VPN tunnels (tailscale0, utun*), and
// other virtual interfaces. Returns "" if no usable address is found.
func PreferredIPv4() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}

	type candidate struct {
		name     string
		ip       string
		priority int
	}
	var candidates []candidate
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		priority := interfacePriority(iface.Name, iface.Flags)
		for _, addr := range addrs {
			ip, _, err := net.ParseCIDR(addr.String())
			if err != nil || ip == nil {
				continue
			}
			ip4 := ip.To4()
			if ip4 == nil || ip4.IsLoopback() || (ip4[0] == 169 && ip4[1] == 254) {
				continue
			}
			candidates = append(candidates, candidate{iface.Name, ip4.String(), priority})
		}
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].priority != candidates[j].priority {
			return candidates[i].priority < candidates[j].priority
		}
		if candidates[i].name != candidates[j].name {
			return candidates[i].name < candidates[j].name
		}
		return candidates[i].ip < candidates[j].ip
	})
	if len(candidates) == 0 {
		return ""
	}
	return candidates[0].ip
}
