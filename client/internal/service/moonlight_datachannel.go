package service

import (
	"fmt"
	"net"
)

// OpenDataChannel satisfies VideoClient on every MoonlightService build: a
// Moonlight stream has no WebRTC DataChannel, so callers such as
// api.ClipboardSync fall back to their direct WebSocket dial.
func (ms *MoonlightService) OpenDataChannel(label string) (net.Conn, error) {
	return nil, fmt.Errorf("DataChannel not supported on MoonlightService")
}
