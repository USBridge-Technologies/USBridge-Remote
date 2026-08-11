package webrtcbridge

import (
	"net"
	"testing"
	"time"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

// fakeVideoSource is a minimal VideoSource that just hands out two
// pre-bound loopback UDP addresses — standing in for a real
// moonlightclient.Session so this test can drive Bridge's video/audio
// wiring without needing a real Sunshine instance running.
type fakeVideoSource struct {
	videoAddr, audioAddr string
	stopped              chan struct{}
}

func newFakeVideoSource(t *testing.T) *fakeVideoSource {
	t.Helper()
	return &fakeVideoSource{
		videoAddr: allocUDPAddr(t),
		audioAddr: allocUDPAddr(t),
		stopped:   make(chan struct{}, 1),
	}
}

// allocUDPAddr binds a UDP socket just to claim an ephemeral port, then
// releases it immediately -- mirrors moonlightclient's own
// establishRTPAddr/ping.go hand-off pattern (see that package's doc
// comments), which is exactly the real-world sequence pumpRTP has to cope
// with (bind after the port was already "promised" to something else).
func allocUDPAddr(t *testing.T) string {
	t.Helper()
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("alloc UDP addr: %v", err)
	}
	addr := conn.LocalAddr().String()
	conn.Close()
	return addr
}

func (f *fakeVideoSource) VideoRTPAddr() string { return f.videoAddr }
func (f *fakeVideoSource) AudioRTPAddr() string { return f.audioAddr }
func (f *fakeVideoSource) RequestIDRFrame()     {}
func (f *fakeVideoSource) Stop() error {
	select {
	case f.stopped <- struct{}{}:
	default:
	}
	return nil
}

// sendFakeRTP fires a handful of synthetic video RTP packets at addr,
// standing in for Sunshine's real video RTP stream. Each packet's payload
// is shaped like a real (single-packet, SOF|EOF) Moonlight/GameStream
// video frame -- 16-byte NV_VIDEO_PACKET header + 8-byte frame header
// (marker byte 0x01) + a minimal Annex-B NAL -- matching exactly what
// moonlightVideoDepacketizer (video_depacketizer.go) expects to strip, so
// this test exercises the real depacketize-then-WriteSample path instead
// of bypassing it.
func sendFakeRTP(t *testing.T, addr string, payloadType uint8, count int) {
	t.Helper()
	raddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		t.Fatalf("resolve %s: %v", addr, err)
	}
	conn, err := net.DialUDP("udp", nil, raddr)
	if err != nil {
		t.Fatalf("dial %s: %v", addr, err)
	}
	defer conn.Close()

	for i := 0; i < count; i++ {
		nvVideoPacketHeader := make([]byte, 16)
		nvVideoPacketHeader[8] = flagSOF | flagEOF // single-packet frame

		frameHeader := make([]byte, 8)
		frameHeader[0] = 0x01 // selects the 8-byte frame-header format

		annexB := []byte{0x00, 0x00, 0x00, 0x01, 0x65, byte(i)} // start code + fake IDR-slice NAL

		payload := append(append(nvVideoPacketHeader, frameHeader...), annexB...)

		pkt := &rtp.Packet{
			Header: rtp.Header{
				Version:        2,
				PayloadType:    payloadType,
				SequenceNumber: uint16(i),
				Timestamp:      uint32(i * 3000),
				SSRC:           0xC0FFEE,
			},
			Payload: payload,
		}
		b, err := pkt.Marshal()
		if err != nil {
			t.Fatalf("marshal rtp: %v", err)
		}
		if _, err := conn.Write(b); err != nil {
			t.Fatalf("write rtp: %v", err)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// TestOffer_WithMedia_AttachesVideoAudioTracks drives the Bridge exactly
// like a browser requesting video+audio (recvonly transceivers in the
// offer, matching what client/internal/webrtcweb adds before creating its
// offer), with a StartSession backed by fakeVideoSource, and verifies the
// synthetic RTP fed into the fake source's UDP addresses actually arrives
// as real RTP packets on the client's OnTrack callback -- proving the
// offer/answer -> transceiver -> AddTrack -> pumpRTP -> browser path is
// wired correctly end to end (media content itself is faked; the WebRTC
// plumbing is real).
func TestOffer_WithMedia_AttachesVideoAudioTracks(t *testing.T) {
	bridge := New()
	defer bridge.Close()

	var source *fakeVideoSource
	bridge.StartSession = func(sessionID string) (VideoSource, error) {
		source = newFakeVideoSource(t)
		return source, nil
	}

	client, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatalf("NewPeerConnection: %v", err)
	}
	defer client.Close()

	if _, err := client.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo, webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly}); err != nil {
		t.Fatalf("AddTransceiverFromKind video: %v", err)
	}
	if _, err := client.AddTransceiverFromKind(webrtc.RTPCodecTypeAudio, webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly}); err != nil {
		t.Fatalf("AddTransceiverFromKind audio: %v", err)
	}

	videoPktCh := make(chan *rtp.Packet, 8)
	client.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		if track.Kind() != webrtc.RTPCodecTypeVideo {
			return
		}
		for {
			pkt, _, err := track.ReadRTP()
			if err != nil {
				return
			}
			select {
			case videoPktCh <- pkt:
			default:
			}
		}
	})

	offer, err := client.CreateOffer(nil)
	if err != nil {
		t.Fatalf("CreateOffer: %v", err)
	}
	gatherComplete := webrtc.GatheringCompletePromise(client)
	if err := client.SetLocalDescription(offer); err != nil {
		t.Fatalf("SetLocalDescription: %v", err)
	}
	<-gatherComplete

	answerSDP, err := bridge.Offer("media-test-session", client.LocalDescription().SDP)
	if err != nil {
		t.Fatalf("bridge.Offer: %v", err)
	}
	if source == nil {
		t.Fatal("StartSession was never called")
	}

	if err := client.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeAnswer,
		SDP:  answerSDP,
	}); err != nil {
		t.Fatalf("client SetRemoteDescription: %v", err)
	}

	// Give ICE/DTLS/SRTP a moment to finish before firing test RTP at the
	// (by-then-listening) pumpRTP goroutine.
	time.Sleep(300 * time.Millisecond)
	sendFakeRTP(t, source.VideoRTPAddr(), 96, 10)

	select {
	case pkt := <-videoPktCh:
		if pkt.PayloadType == 0 {
			t.Fatal("received RTP packet with zero payload type")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for forwarded video RTP packet on the browser side")
	}

	client.Close()
	select {
	case <-source.stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for VideoSource.Stop() to be called on teardown")
	}
}
