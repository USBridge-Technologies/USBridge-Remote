//go:build linux || windows || darwin

package usbpass

import (
	"encoding/hex"
	"io"
	"net"
	"testing"
	"time"
)

// Pinned against an independently computed SHA256("usbridge-usb-tunnel-v1"
// || zero32 || "1-92" || {0xAA,0xBB}) vector — catches accidental drift from
// rust-shine's byte-for-byte-compatible transport::derive_tunnel_key.
func TestDeriveTunnelKeyCrossLangVector(t *testing.T) {
	var sessionKey [32]byte
	got := deriveTunnelKey(sessionKey, "1-92", []byte{0xAA, 0xBB})
	want := "e3dc93cb3009e5d6b64803b9574e2a6ed45c9c81162c16c0c8bc1de57044002f"
	if gotHex := hex.EncodeToString(got[:]); gotHex != want {
		t.Fatalf("deriveTunnelKey mismatch:\n got  %s\n want %s", gotHex, want)
	}
}

// spawnFakeExporter stands in for the real loopback USB/IP exporter
// (server.go): echoes whatever it reads, so a test can confirm bytes
// survived the tunnel round-trip.
func spawnFakeExporter(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		io.Copy(conn, conn) //nolint:errcheck
	}()
	t.Cleanup(func() { ln.Close() })
	return ln.Addr().String()
}

func TestTunnelListenerAuthenticatesAndRelays(t *testing.T) {
	exportAddr := spawnFakeExporter(t)

	key := deriveTunnelKey(deriveSessionKey([]byte("test-secret")), "1-92", []byte("nonce-a"))
	registerTunnelKey("1-92", key)

	tl, err := StartTunnelListener("127.0.0.1:0", exportAddr)
	if err != nil {
		t.Fatal(err)
	}
	defer tl.Stop()

	conn, err := net.Dial("tcp", tl.ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	agentSide, err := newAeadStream(conn, key)
	if err != nil {
		t.Fatal(err)
	}
	if err := agentSide.sendFrame([]byte("submit-urb-bytes")); err != nil {
		t.Fatal(err)
	}
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	got, err := agentSide.recvFrame()
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "submit-urb-bytes" {
		t.Fatalf("got %q, want %q", got, "submit-urb-bytes")
	}
}

func TestTunnelListenerDropsUnauthenticated(t *testing.T) {
	exportAddr := spawnFakeExporter(t)
	tl, err := StartTunnelListener("127.0.0.1:0", exportAddr)
	if err != nil {
		t.Fatal(err)
	}
	defer tl.Stop()

	conn, err := net.Dial("tcp", tl.ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	// A well-formed frame under a key nobody registered — must not
	// authenticate, and the connection must be closed rather than
	// bridged to the real exporter.
	stream, err := newAeadStream(conn, [32]byte{1, 2, 3})
	if err != nil {
		t.Fatal(err)
	}
	if err := stream.sendFrame([]byte("not authorized")); err != nil {
		t.Fatal(err)
	}
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 1)
	if n, err := conn.Read(buf); err != io.EOF && n != 0 {
		t.Fatalf("expected connection to be dropped, got n=%d err=%v", n, err)
	}
}
