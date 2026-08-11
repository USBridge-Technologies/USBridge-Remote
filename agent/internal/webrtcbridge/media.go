package webrtcbridge

import (
	"log"
	"net"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

// VideoSource is the subset of agent/internal/moonlightclient.Session the
// bridge needs — a live, actively-streaming loopback Moonlight session
// already sending H.264 video RTP and Opus audio RTP to 127.0.0.1. Defined
// here (rather than importing moonlightclient directly) so webrtcbridge
// stays testable/usable without dragging in the whole Sunshine-driving
// stack — see Bridge.StartSession's doc comment.
type VideoSource interface {
	VideoRTPAddr() string
	AudioRTPAddr() string
	RequestIDRFrame()
	Stop() error
}

// addMediaTracks creates local video/audio tracks, attaches them to pc, and
// starts the underlying Moonlight loopback session + RTP pumps that feed
// them. Returns the started VideoSource so the caller (Offer) can tear it
// down when the session ends. Must be called before CreateAnswer so the
// tracks' m-lines make it into the SDP answer.
func (b *Bridge) addMediaTracks(sessionID string, pc *webrtc.PeerConnection) (VideoSource, error) {
	videoTrack, err := webrtc.NewTrackLocalStaticRTP(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264},
		"video", "usbridge-"+sessionID,
	)
	if err != nil {
		return nil, err
	}
	audioTrack, err := webrtc.NewTrackLocalStaticRTP(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus},
		"audio", "usbridge-"+sessionID,
	)
	if err != nil {
		return nil, err
	}

	videoSender, err := pc.AddTrack(videoTrack)
	if err != nil {
		return nil, err
	}
	audioSender, err := pc.AddTrack(audioTrack)
	if err != nil {
		return nil, err
	}
	// pion requires RTCP (sender/receiver reports, PLI, etc.) coming back
	// from the browser to actually be read off each RTPSender, or its
	// internal buffers back up — we don't act on any of it yet (stage
	// 2/3 doesn't wire PLI-triggered IDR requests back to Sunshine), just
	// drain it.
	go drainRTCP(sessionID, "video", videoSender)
	go drainRTCP(sessionID, "audio", audioSender)

	src, err := b.StartSession(sessionID)
	if err != nil {
		return nil, err
	}

	go pumpRTP(sessionID, "video", src.VideoRTPAddr(), videoTrack)
	go pumpRTP(sessionID, "audio", src.AudioRTPAddr(), audioTrack)

	return src, nil
}

func drainRTCP(sessionID, kind string, sender *webrtc.RTPSender) {
	buf := make([]byte, 1500)
	for {
		if _, _, err := sender.Read(buf); err != nil {
			log.Printf("[webrtcbridge] session=%s %s: RTCP reader stopped: %v", sessionID, kind, err)
			return
		}
	}
}

// pumpRTP listens on udpAddr (a 127.0.0.1:port the agent's own
// moonlightclient.Session established with Sunshine — see VideoSource) and
// forwards every raw RTP packet straight into track via WriteRTP. Sunshine
// already produces standards-shaped RTP (H.264 per RFC 6184, Opus per RFC
// 7587), so this is a passthrough, not a transcode: no re-encoding, no
// depacketization/repacketization, just moving bytes from a UDP socket to
// a WebRTC track.
func pumpRTP(sessionID, kind, udpAddr string, track *webrtc.TrackLocalStaticRTP) {
	addr, err := net.ResolveUDPAddr("udp", udpAddr)
	if err != nil {
		log.Printf("[webrtcbridge] session=%s %s: resolve %s: %v", sessionID, kind, udpAddr, err)
		return
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		log.Printf("[webrtcbridge] session=%s %s: listen %s: %v", sessionID, kind, udpAddr, err)
		return
	}
	defer conn.Close()

	buf := make([]byte, 1500)
	var packetCount int
	for {
		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			return // socket closed by Bridge on session teardown
		}
		pkt := &rtp.Packet{}
		if err := pkt.Unmarshal(buf[:n]); err != nil {
			continue
		}
		if err := track.WriteRTP(pkt); err != nil {
			// A "no readers"/closed-track error here just means every
			// downstream sender (RTCPeerConnection) went away — normal at
			// session teardown, nothing to log loudly about.
			return
		}
		packetCount++
		if packetCount == 1 {
			log.Printf("[webrtcbridge] session=%s %s: first RTP packet forwarded (%d bytes)", sessionID, kind, n)
		}
	}
}
