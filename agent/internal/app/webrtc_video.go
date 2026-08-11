package app

import (
	"context"
	"fmt"
	"path/filepath"

	"usbridge_agent/internal/moonlightclient"
	"usbridge_agent/internal/webrtcbridge"
)

// startWebRTCVideoSession is wired as webrtcbridge.Bridge.StartSession (see
// New() in app.go): every time a browser client successfully offers a
// video/audio transceiver, this drives the agent's own bundled Sunshine
// into an actively-streaming loopback session via moonlightclient, so the
// Bridge has real H.264/Opus RTP to forward. Uses the exact same
// NvHTTP-base-port arithmetic as restartStreamProxy/initTailscale
// (SunshinePort is the *admin* port; NvHTTP base = admin - 1) and the same
// admin-API PIN-submission path SubmitMoonlightPIN uses for real remote
// Moonlight clients, so no protocol/config duplication exists between the
// two client paths.
func (a *App) startWebRTCVideoSession(sessionID string) (webrtcbridge.VideoSource, error) {
	if a.stream == nil {
		return nil, fmt.Errorf("stream host not running")
	}
	basePort := a.cfg.SunshinePort - 1

	cfg := moonlightclient.Config{
		Host:        "127.0.0.1",
		HTTPSPort:   basePort - 5,
		HTTPPort:    basePort,
		StateDir:    filepath.Join(a.cfg.StateDir, "moonlightclient"),
		Width:       1920,
		Height:      1080,
		FPS:         60,
		BitrateKbps: 20000,
		SubmitPIN: func(pin string) error {
			return a.stream.SubmitPIN(a.cfg.SunshinePort, pin)
		},
	}

	session, err := moonlightclient.Start(context.Background(), cfg)
	if err != nil {
		return nil, fmt.Errorf("start moonlightclient session for webrtc session %s: %w", sessionID, err)
	}
	return session, nil
}
