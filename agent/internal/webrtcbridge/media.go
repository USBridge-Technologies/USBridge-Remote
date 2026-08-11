package webrtcbridge

import (
	"log"
	"net"
	"time"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
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
	// Video uses TrackLocalStaticSample, not TrackLocalStaticRTP: Sunshine's
	// video RTP packets are NOT standard RFC 6184 H.264-over-RTP (they
	// carry a proprietary 16-byte NV_VIDEO_PACKET header + a per-frame
	// header on top of the usual 12-byte RTP header — see
	// video_depacketizer.go's doc comment for how this was actually
	// diagnosed: forwarding the raw bytes let the RTCPeerConnection reach
	// "connected" and the MediaStreamTrack report readyState="live", but
	// the browser's H.264 decoder never produced a single frame, silently).
	// WriteSample hands pion a clean per-frame Annex-B buffer and lets its
	// own H.264 payloader re-packetize it correctly for the codec/profile
	// actually negotiated in the SDP answer.
	videoTrack, err := webrtc.NewTrackLocalStaticSample(
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

	go pumpVideoSamples(sessionID, src.VideoRTPAddr(), videoTrack)
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

// pumpVideoSamples listens on udpAddr for Sunshine's raw video RTP packets,
// runs each one through moonlightVideoDepacketizer to strip the
// Moonlight/GameStream-specific per-packet framing, and hands each
// reassembled frame to track.WriteSample as a clean Annex-B buffer — see
// addMediaTracks' doc comment for why this can't be a plain RTP-to-RTP
// passthrough the way audio's pumpRTP is.
func pumpVideoSamples(sessionID, udpAddr string, track *webrtc.TrackLocalStaticSample) {
	addr, err := net.ResolveUDPAddr("udp", udpAddr)
	if err != nil {
		log.Printf("[webrtcbridge] session=%s video: resolve %s: %v", sessionID, udpAddr, err)
		return
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		log.Printf("[webrtcbridge] session=%s video: listen %s: %v", sessionID, udpAddr, err)
		return
	}
	defer conn.Close()

	var frameCount int
	depacketizer := newMoonlightVideoDepacketizer(func(annexB []byte) {
		if err := track.WriteSample(media.Sample{Data: annexB, Duration: time.Second / 60}); err != nil {
			return // no readers / track closed at session teardown
		}
		frameCount++
		if frameCount == 1 {
			log.Printf("[webrtcbridge] session=%s video: first Annex-B frame forwarded (%d bytes)", sessionID, len(annexB))
		}
	})

	buf := make([]byte, 65535) // a full video frame can span a jumbo-sized UDP datagram on loopback
	for {
		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			return // socket closed by Bridge on session teardown
		}
		pkt := &rtp.Packet{}
		if err := pkt.Unmarshal(buf[:n]); err != nil {
			continue
		}
		depacketizer.pushPacket(pkt.Payload)
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
