// Package webrtcbridge lets a browser-based client (the WASM web client)
// connect to the agent over plain low-latency WebRTC instead of the
// Moonlight/GameStream protocol the desktop client uses.
//
// Stage 1 of the rollout (see the implementation plan) only wires up
// signaling and a DataChannel: a client posts an SDP offer, the Bridge
// answers with pion/webrtc and opens an "input" DataChannel that currently
// just echoes whatever it receives back (ping-pong), so end-to-end
// connectivity and latency can be verified before any real video/audio/input
// wiring is added on top in later stages.
package webrtcbridge

import (
	"fmt"
	"log"
	"sync"

	"github.com/pion/webrtc/v4"
)

// Bridge manages WebRTC peer connections for browser clients. One Bridge
// instance is owned by the agent's App and reused across sessions; each
// Offer() call creates a fresh PeerConnection (a browser tab reconnecting
// should not reuse a stale one).
type Bridge struct {
	mu       sync.Mutex
	sessions map[string]*webrtc.PeerConnection
	// sources tracks the live moonlightclient.Session (as a VideoSource)
	// backing each session's video/audio tracks, so it gets torn down
	// alongside the PeerConnection instead of leaking a Sunshine stream.
	sources map[string]VideoSource

	// OnInputMessage, if set, is called with the raw bytes received on the
	// "input" DataChannel of any session. Stage 1 leaves this nil, which
	// makes the DataChannel handler fall back to an echo/ping-pong responder
	// instead — see handleInputChannel.
	OnInputMessage func(sessionID string, data []byte)

	// StartSession, if set, is called once per Offer() (after the browser's
	// video/audio transceivers are known from its SDP offer, before the
	// answer is generated) to start the actual Moonlight loopback session
	// (agent/internal/moonlightclient.Start, wired in by agent/internal/app)
	// that will feed the video/audio tracks. Left nil in tests that only
	// care about signaling/DataChannel behavior (see bridge_test.go) — no
	// video/audio tracks are added to the answer in that case.
	StartSession func(sessionID string) (VideoSource, error)
}

// New creates a Bridge with no active sessions.
func New() *Bridge {
	return &Bridge{
		sessions: make(map[string]*webrtc.PeerConnection),
		sources:  make(map[string]VideoSource),
	}
}

// Offer processes an SDP offer from a browser client and returns the SDP
// answer. sessionID identifies the connection for logging/lookup (e.g. a
// client-generated UUID); a previous session under the same ID, if any, is
// closed first so a reconnect doesn't leak the old PeerConnection.
func (b *Bridge) Offer(sessionID, offerSDP string) (answerSDP string, err error) {
	b.mu.Lock()
	if old, ok := b.sessions[sessionID]; ok {
		_ = old.Close()
		delete(b.sessions, sessionID)
	}
	b.mu.Unlock()

	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		return "", fmt.Errorf("webrtcbridge: create peer connection: %w", err)
	}

	pc.OnDataChannel(func(dc *webrtc.DataChannel) {
		b.handleInputChannel(sessionID, dc)
	})

	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		log.Printf("[webrtcbridge] session=%s state=%s", sessionID, state)
		if state == webrtc.PeerConnectionStateClosed ||
			state == webrtc.PeerConnectionStateFailed ||
			state == webrtc.PeerConnectionStateDisconnected {
			b.mu.Lock()
			if cur, ok := b.sessions[sessionID]; ok && cur == pc {
				delete(b.sessions, sessionID)
			}
			if src, ok := b.sources[sessionID]; ok {
				delete(b.sources, sessionID)
				go func() {
					if err := src.Stop(); err != nil {
						log.Printf("[webrtcbridge] session=%s video source stop: %v", sessionID, err)
					}
				}()
			}
			b.mu.Unlock()
		}
	})

	if err := pc.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  offerSDP,
	}); err != nil {
		_ = pc.Close()
		return "", fmt.Errorf("webrtcbridge: set remote description: %w", err)
	}

	// Media tracks must be attached to the transceivers the browser's offer
	// already created (SetRemoteDescription above) -- an answer can't
	// introduce new m-lines the offer didn't have, per JSEP -- so this has
	// to happen after SetRemoteDescription and before CreateAnswer. Only
	// attempted when the caller wired a session starter (see StartSession's
	// doc comment); a failure here is logged but doesn't abort the whole
	// connection -- the browser still gets working signaling/input even if
	// Sunshine/moonlightclient couldn't be started.
	if b.StartSession != nil {
		if src, err := b.addMediaTracks(sessionID, pc); err != nil {
			log.Printf("[webrtcbridge] session=%s addMediaTracks failed, continuing without video/audio: %v", sessionID, err)
		} else {
			b.mu.Lock()
			b.sources[sessionID] = src
			b.mu.Unlock()
		}
	}

	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		_ = pc.Close()
		return "", fmt.Errorf("webrtcbridge: create answer: %w", err)
	}

	// Wait for ICE gathering to complete so the answer we hand back already
	// contains all local candidates (no trickle-ICE signaling channel yet in
	// stage 1) — simplest thing that works for LAN/Tailscale-IP connections.
	gatherComplete := webrtc.GatheringCompletePromise(pc)
	if err := pc.SetLocalDescription(answer); err != nil {
		_ = pc.Close()
		return "", fmt.Errorf("webrtcbridge: set local description: %w", err)
	}
	<-gatherComplete

	b.mu.Lock()
	b.sessions[sessionID] = pc
	b.mu.Unlock()

	return pc.LocalDescription().SDP, nil
}

// handleInputChannel wires up the "input" DataChannel. Stage 1 has no real
// input backend yet (see OnInputMessage), so it echoes messages back
// prefixed with "pong:" — enough for a client to measure round-trip latency
// and prove the channel is alive end-to-end.
func (b *Bridge) handleInputChannel(sessionID string, dc *webrtc.DataChannel) {
	log.Printf("[webrtcbridge] session=%s datachannel %q open (label)", sessionID, dc.Label())

	dc.OnOpen(func() {
		log.Printf("[webrtcbridge] session=%s datachannel %q ready", sessionID, dc.Label())
	})

	dc.OnMessage(func(msg webrtc.DataChannelMessage) {
		if b.OnInputMessage != nil {
			b.OnInputMessage(sessionID, msg.Data)
			return
		}
		reply := append([]byte("pong:"), msg.Data...)
		if err := dc.Send(reply); err != nil {
			log.Printf("[webrtcbridge] session=%s echo send failed: %v", sessionID, err)
		}
	})
}

// Close tears down every active session. Called on agent shutdown.
func (b *Bridge) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	for id, pc := range b.sessions {
		_ = pc.Close()
		delete(b.sessions, id)
	}
	for id, src := range b.sources {
		_ = src.Stop()
		delete(b.sources, id)
	}
}

// SessionCount returns the number of currently tracked (not necessarily
// still-connected) sessions — used by tests and status reporting.
func (b *Bridge) SessionCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.sessions)
}
