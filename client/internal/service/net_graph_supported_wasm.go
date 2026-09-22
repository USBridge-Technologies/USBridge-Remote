//go:build js && wasm

package service

// NetGraphSupported is true on the web build: the WebRTC video path exposes
// enough of getStats() (packetsReceived/framesDecoded/packetsLost/jitter/
// totalDecodeTime, RTT off the selected candidate-pair) to drive a real,
// if reduced, Net Graph HUD -- see net_graph_wasm.go for the actual
// wiring. There's no FEC or host-latency data on this path (WebRTC has its
// own NACK/RTX loss recovery, not moonlight's FEC scheme, and rust-shine's
// WebRTC source doesn't carry the Sunshine-protocol per-frame host-latency
// header field the classic GameStream path does) -- those rows read as 0
// rather than being hidden entirely, same convention net_graph.go's own
// HostLatencyValid doc comment already describes for a server that simply
// doesn't fill a field in.
func NetGraphSupported() bool {
	return true
}
