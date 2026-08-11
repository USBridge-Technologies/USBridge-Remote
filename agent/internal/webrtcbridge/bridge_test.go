package webrtcbridge

import (
	"testing"
	"time"

	"github.com/pion/webrtc/v4"
)

// TestOfferAnswerDataChannelRoundTrip stands in for stage 1 of the plan's
// staged rollout: it drives the Bridge exactly the way a browser client
// will (create a PeerConnection, open a DataChannel, do real SDP
// offer/answer + ICE, no shortcuts/mocks), then sends a message over the
// DataChannel and waits for the echoed reply, measuring RTT. This proves the
// signaling + DataChannel path genuinely works end-to-end before any
// video/input wiring is layered on top.
func TestOfferAnswerDataChannelRoundTrip(t *testing.T) {
	bridge := New()
	defer bridge.Close()

	// "client" side: a second, independent PeerConnection playing the role
	// the browser's RTCPeerConnection will play later.
	client, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatalf("client NewPeerConnection: %v", err)
	}
	defer client.Close()

	dc, err := client.CreateDataChannel("input", nil)
	if err != nil {
		t.Fatalf("CreateDataChannel: %v", err)
	}

	opened := make(chan struct{})
	dc.OnOpen(func() { close(opened) })

	replies := make(chan []byte, 1)
	dc.OnMessage(func(msg webrtc.DataChannelMessage) {
		replies <- msg.Data
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

	answerSDP, err := bridge.Offer("test-session", client.LocalDescription().SDP)
	if err != nil {
		t.Fatalf("bridge.Offer: %v", err)
	}

	if err := client.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeAnswer,
		SDP:  answerSDP,
	}); err != nil {
		t.Fatalf("client SetRemoteDescription: %v", err)
	}

	select {
	case <-opened:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for DataChannel to open")
	}

	start := time.Now()
	if err := dc.Send([]byte("ping")); err != nil {
		t.Fatalf("dc.Send: %v", err)
	}

	select {
	case reply := <-replies:
		rtt := time.Since(start)
		if string(reply) != "pong:ping" {
			t.Fatalf("unexpected reply: %q", reply)
		}
		t.Logf("round trip: %s", rtt)
		if rtt > 2*time.Second {
			t.Fatalf("round trip too slow for a same-process LAN test: %s", rtt)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for echo reply")
	}

	if got := bridge.SessionCount(); got != 1 {
		t.Fatalf("SessionCount = %d, want 1", got)
	}
}
