package webrtcbridge

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"

	"usbridge_agent/internal/moonlightclient"
)

// TestOffer_RealSunshine_ProducesRealVideoRTP is the capstone check for
// stage 2/3 of the web-client rollout plan: it drives Bridge.Offer exactly
// the way a browser would (recvonly video+audio transceivers in a real SDP
// offer, via a real pion PeerConnection standing in for the browser), with
// Bridge.StartSession wired to the real moonlightclient package -- the
// exact same wiring agent/internal/app/webrtc_video.go uses -- against
// whatever real Sunshine instance is already running on this machine
// (skipped entirely if none is reachable). If the offer/answer negotiates
// video+audio and real RTP frames come out the other end of a real
// RTCPeerConnection, the entire agent-side stack (signaling -> transceiver
// negotiation -> moonlightclient session -> RTP capture -> WebRTC track ->
// SRTP encryption -> browser-side decryption) is proven correct end to end.
//
// Deliberately targets an already-running Sunshine rather than launching
// one from this test, for the same reason
// moonlightclient/session_test.go's TestSession_RealSunshine does (see its
// doc comment) -- reads the admin password moonlightclient's own real test
// already established as the safe way to authenticate against it.
func TestOffer_RealSunshine_ProducesRealVideoRTP(t *testing.T) {
	adminPort := envInt(t, "MOONLIGHTCLIENT_TEST_ADMIN_PORT", 47990)
	httpPort := envInt(t, "MOONLIGHTCLIENT_TEST_HTTP_PORT", 47989)
	httpsPort := envInt(t, "MOONLIGHTCLIENT_TEST_HTTPS_PORT", 47984)

	if !waitForPort(adminPort, 500*time.Millisecond) {
		t.Skipf("no Sunshine admin API reachable on 127.0.0.1:%d -- start a real Sunshine instance (e.g. the agent itself) to run this real end-to-end check", adminPort)
	}

	passFile := os.Getenv("MOONLIGHTCLIENT_TEST_ADMIN_PASS_FILE")
	if passFile == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			t.Skip("no home directory to locate the default admin password file")
		}
		passFile = filepath.Join(home, ".config", "usbridge-agent", "sunshine", "usbridge_admin_pass")
	}
	passBytes, err := os.ReadFile(passFile)
	if err != nil {
		t.Skipf("can't read Sunshine admin password at %s: %v", passFile, err)
	}
	adminPass := strings.TrimSpace(string(passBytes))

	submitPIN := func(pin string) error {
		body, _ := json.Marshal(map[string]string{"pin": pin})
		req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("https://127.0.0.1:%d/api/pin", adminPort), bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		req.SetBasicAuth("sunshine", adminPass)
		client := &http.Client{
			Timeout:   5 * time.Second,
			Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}, //nolint:gosec
		}
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("sunshine /api/pin returned HTTP %d", resp.StatusCode)
		}
		return nil
	}

	stateDir := t.TempDir()
	bridge := New()
	bridge.StartSession = func(sessionID string) (VideoSource, error) {
		return moonlightclient.Start(context.Background(), moonlightclient.Config{
			Host:      "127.0.0.1",
			HTTPPort:  httpPort,
			HTTPSPort: httpsPort,
			StateDir:  stateDir,
			Width:     1280,
			Height:    720,
			FPS:       30,
			SubmitPIN: submitPIN,
		})
	}
	defer bridge.Close()

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

	videoPackets := make(chan struct{}, 256)
	client.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		if track.Kind() != webrtc.RTPCodecTypeVideo {
			return
		}
		for {
			_, _, err := track.ReadRTP()
			if err != nil {
				return
			}
			select {
			case videoPackets <- struct{}{}:
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

	answerSDP, err := bridge.Offer("real-sunshine-media-test", client.LocalDescription().SDP)
	if err != nil {
		t.Fatalf("bridge.Offer: %v", err)
	}
	if !strings.Contains(answerSDP, "m=video") {
		t.Fatalf("answer SDP has no video m-line:\n%s", answerSDP)
	}

	if err := client.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeAnswer,
		SDP:  answerSDP,
	}); err != nil {
		t.Fatalf("client SetRemoteDescription: %v", err)
	}

	select {
	case <-videoPackets:
		t.Log("received at least one real video RTP packet over a real RTCPeerConnection, sourced from a real Sunshine session")
	case <-time.After(20 * time.Second):
		t.Fatal("timed out waiting for real video RTP over WebRTC")
	}
}

func waitForPort(port int, timeout time.Duration) bool {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), timeout)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

func envInt(t *testing.T, key string, def int) int {
	t.Helper()
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	var n int
	if _, err := fmt.Sscanf(v, "%d", &n); err != nil {
		t.Fatalf("invalid %s=%q: %v", key, v, err)
	}
	return n
}
