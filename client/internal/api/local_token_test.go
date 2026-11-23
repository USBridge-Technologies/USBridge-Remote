package api

import (
	"net/http"
	"testing"
)

func TestParseLocalTokenAndSignRequest(t *testing.T) {
	raw := []byte("usbloc1.share123.0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	id, secret, isLocal := ParseLocalToken(raw)
	if !isLocal || id != "share123" || len(secret) != 64 {
		t.Fatalf("ParseLocalToken failed: isLocal=%v id=%q secretLen=%d", isLocal, id, len(secret))
	}

	c := &USBClient{apiSecret: raw}
	req, _ := http.NewRequest("GET", "http://example.local/api/status", nil)
	if err := c.signRequest(req, "GET", "/api/status", nil); err != nil {
		t.Fatalf("signRequest: %v", err)
	}
	if req.Header.Get("X-Access-Token-ID") != "share123" {
		t.Fatalf("expected X-Access-Token-ID=share123, got %q", req.Header.Get("X-Access-Token-ID"))
	}
	ts := req.Header.Get("X-Auth-Timestamp")
	wantSig := CalculateHMACV2("GET", "/api/status", ts, "", secret)
	if req.Header.Get("X-Auth-Signature") != wantSig {
		t.Fatalf("signature mismatch: got %q want %q", req.Header.Get("X-Auth-Signature"), wantSig)
	}

	// Regular master key should not set X-Access-Token-ID
	c2 := &USBClient{apiSecret: []byte("plain-master-key")}
	req2, _ := http.NewRequest("GET", "http://example.local/api/status", nil)
	if err := c2.signRequest(req2, "GET", "/api/status", nil); err != nil {
		t.Fatalf("signRequest master: %v", err)
	}
	if req2.Header.Get("X-Access-Token-ID") != "" {
		t.Fatalf("master key must not set X-Access-Token-ID")
	}
}
