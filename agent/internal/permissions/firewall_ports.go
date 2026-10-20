package permissions

import "sort"

// rustshineWebRTCPort mirrors streamhost's rustshineWebRTCPort (the
// streamer's --webrtc-port, TCP signaling for the browser client).
const rustshineWebRTCPort = 8444

// FirewallPorts returns the inbound TCP/UDP ports a client needs to reach
// this agent and its streamer: the agent's HTTP/HTTPS API, the USB
// passthrough listener, RustShine's WebRTC signaling, and the GameStream
// plane relative to sunshinePort-1 (the NvHTTP base -- same offsets every
// streamhost backend's Ports uses). basePort+1 (the streamer's admin/web UI)
// is left out: clients never connect to it. UDP covers basePort+9..+13, the
// union of all backends' video/control/audio/mic ports.
func FirewallPorts(httpPort, tlsPort, usbPort, sunshinePort int) (tcp []int, udp []int) {
	base := sunshinePort - 1
	tcp = []int{httpPort, tlsPort, usbPort, rustshineWebRTCPort, base - 5, base, base + 21}
	udp = []int{base + 9, base + 10, base + 11, base + 12, base + 13}
	return tcp, udp
}

// portRanges collapses ports into sorted, de-duplicated [lo, hi] runs of
// consecutive ports, so the firewall commands open 47998-48002 as one rule
// instead of five.
func portRanges(ports []int) [][2]int {
	seen := make(map[int]bool, len(ports))
	var sorted []int
	for _, p := range ports {
		if p > 0 && p < 65536 && !seen[p] {
			seen[p] = true
			sorted = append(sorted, p)
		}
	}
	sort.Ints(sorted)
	var out [][2]int
	for _, p := range sorted {
		if n := len(out); n > 0 && out[n-1][1] == p-1 {
			out[n-1][1] = p
			continue
		}
		out = append(out, [2]int{p, p})
	}
	return out
}
