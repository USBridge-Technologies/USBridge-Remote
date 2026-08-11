//go:build js && wasm

package service

import "fmt"

// startMoonlightProxy stub for wasm/browser builds: the real Moonlight video
// pipeline (and its tsnet-tunneled RTSP/UDP proxying) is desktop/mobile-only.
// The browser-native video path is wired separately via WebRTC (see
// internal/webrtcweb) and does not go through this proxy at all.
func startMoonlightProxy(ts *TailscaleService, serverHost string, rtspPort int) (stop func(), localRTSPPort int, err error) {
	return nil, 0, fmt.Errorf("moonlight tsnet proxy not supported in browser")
}
