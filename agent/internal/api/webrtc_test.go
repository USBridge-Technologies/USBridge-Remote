package api

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"
)

// TestWebRTCOffer_CORSPreflight verifies the withCORS wrapper (server.go)
// answers an OPTIONS preflight the way a browser's fetch() to
// /api/webrtc/offer needs, without ever reaching the HMAC-checked handler —
// preflight requests never carry the custom auth headers, so this must be
// handled before SecurityMiddleware sees the request at all.
func TestWebRTCOffer_CORSPreflight(t *testing.T) {
	srv := NewServerWithAuth(&stubApp{}, []byte("cors-test-secret"), 0)
	ts := httptest.NewServer(srv.Routes())
	defer ts.Close()

	req, err := http.NewRequest(http.MethodOptions, ts.URL+"/api/webrtc/offer", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", "http://example.test")
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Headers", "content-type, x-auth-signature, x-auth-timestamp")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("preflight status = %d, want 204", resp.StatusCode)
	}
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "http://example.test" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want reflected origin", got)
	}
	if resp.Header.Get("Access-Control-Allow-Headers") == "" {
		t.Fatal("Access-Control-Allow-Headers missing")
	}
}

// TestWebRTCOffer_RealHandshake drives /api/webrtc/offer with a real
// pion PeerConnection playing the browser's role — same technique as
// webrtcbridge's own test, but through the full HTTP+HMAC+CORS stack this
// package owns, not just the Bridge directly.
func TestWebRTCOffer_RealHandshake(t *testing.T) {
	secret := []byte("webrtc-offer-test-secret")
	stub := &stubApp{}
	srv := NewServerWithAuth(stub, secret, 0)
	ts := httptest.NewServer(srv.Routes())
	defer ts.Close()

	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatalf("NewPeerConnection: %v", err)
	}
	defer pc.Close()

	dc, err := pc.CreateDataChannel("input", nil)
	if err != nil {
		t.Fatalf("CreateDataChannel: %v", err)
	}
	opened := make(chan struct{})
	dc.OnOpen(func() { close(opened) })

	offer, err := pc.CreateOffer(nil)
	if err != nil {
		t.Fatalf("CreateOffer: %v", err)
	}
	gatherComplete := webrtc.GatheringCompletePromise(pc)
	if err := pc.SetLocalDescription(offer); err != nil {
		t.Fatalf("SetLocalDescription: %v", err)
	}
	<-gatherComplete

	reqBody, _ := json.Marshal(map[string]string{
		"session_id": "api-test-session",
		"sdp":        pc.LocalDescription().SDP,
	})
	path := "/api/webrtc/offer"
	ts2 := strconv.FormatInt(time.Now().Unix(), 10)
	derived := sha256.Sum256(secret)
	mac := hmac.New(sha256.New, derived[:])
	mac.Write([]byte("POST" + path + ts2 + string(reqBody)))
	sig := hex.EncodeToString(mac.Sum(nil))

	req, _ := http.NewRequest(http.MethodPost, ts.URL+path, bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Auth-Timestamp", ts2)
	req.Header.Set("X-Auth-Signature", sig)
	req.Header.Set("Origin", "http://example.test")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "http://example.test" {
		t.Fatalf("Access-Control-Allow-Origin on real response = %q", got)
	}

	var parsed struct {
		Success bool `json:"success"`
		Data    struct {
			SDP string `json:"sdp"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("unmarshal: %v, body=%s", err, body)
	}
	if !parsed.Success || parsed.Data.SDP == "" {
		t.Fatalf("bad response: %s", body)
	}

	if err := pc.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeAnswer,
		SDP:  parsed.Data.SDP,
	}); err != nil {
		t.Fatalf("SetRemoteDescription: %v", err)
	}

	select {
	case <-opened:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for DataChannel to open")
	}
}

// TestWebRTCOffer_RejectsUnsigned confirms the endpoint is still behind the
// normal HMAC gate for everything except the OPTIONS preflight above.
func TestWebRTCOffer_RejectsUnsigned(t *testing.T) {
	srv := NewServerWithAuth(&stubApp{}, []byte("secret"), 0)
	ts := httptest.NewServer(srv.Routes())
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/api/webrtc/offer", "application/json", bytes.NewReader([]byte(`{}`)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}
