//go:build linux || windows || darwin

package usbpass

import (
	"bytes"
	"encoding/hex"
	"errors"
	"net"
	"testing"
	"time"
)

func TestAeadStreamRoundTrip(t *testing.T) {
	key := deriveSessionKey([]byte("test-master-key"))
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	done := make(chan []byte, 1)
	errCh := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			errCh <- err
			return
		}
		s, err := acceptAeadStream(conn, key)
		if err != nil {
			errCh <- err
			return
		}
		got, err := s.recvFrame()
		if err != nil {
			errCh <- err
			return
		}
		done <- got
	}()

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	s, err := connectAeadStream(conn, key)
	if err != nil {
		t.Fatal(err)
	}
	frame := encodeAttachFrame(attachPayload{
		BusID:         "1-92",
		VID:           0x0781,
		PID:           0x55A9,
		Speed:         3,
		DeviceDesc:    syntheticDeviceDesc(0x0781, 0x55A9),
		ConfigDesc:    syntheticMSCConfig(),
		ExportHost:    "127.0.0.1",
		ExportService: "3240",
	})
	if err := s.sendFrame(frame); err != nil {
		t.Fatal(err)
	}

	select {
	case err := <-errCh:
		t.Fatal(err)
	case got := <-done:
		msgType, payload, err := decodeUsbpFrame(got)
		if err != nil {
			t.Fatal(err)
		}
		if msgType != msgAttach {
			t.Fatalf("msgType = %d, want %d", msgType, msgAttach)
		}
		if len(payload) == 0 {
			t.Fatal("empty attach payload")
		}
	}
}

func TestFrameCodecRoundTrip(t *testing.T) {
	hello := encodeHelloFrame(usbAesRoleClient, usbAesProtoVersion)
	msgType, payload, err := decodeUsbpFrame(hello)
	if err != nil {
		t.Fatal(err)
	}
	if msgType != msgHello {
		t.Fatalf("msgType = %d, want %d", msgType, msgHello)
	}
	if len(payload) != 2 || payload[0] != usbAesRoleClient || payload[1] != usbAesProtoVersion {
		t.Fatalf("hello payload = %v", payload)
	}

	// HelloAck as the agent would encode it: ok=1, detail="agent".
	ack := encodeUsbpFrame(msgHelloAck, append([]byte{1}, appendUsbpString(nil, "agent")...))
	msgType, payload, err = decodeUsbpFrame(ack)
	if err != nil {
		t.Fatal(err)
	}
	if msgType != msgHelloAck {
		t.Fatalf("msgType = %d, want %d", msgType, msgHelloAck)
	}
	ok, detail, err := decodeHelloAck(payload)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || detail != "agent" {
		t.Fatalf("ok=%v detail=%q", ok, detail)
	}

	detach := encodeDetachFrame(7)
	msgType, payload, err = decodeUsbpFrame(detach)
	if err != nil {
		t.Fatal(err)
	}
	if msgType != msgDetach {
		t.Fatalf("msgType = %d, want %d", msgType, msgDetach)
	}
	if len(payload) != 4 {
		t.Fatalf("detach payload len = %d", len(payload))
	}
}

// pipePair is a connected dialer/acceptor stream pair.
func pipePair(t *testing.T, key [32]byte) (dialer, acceptor *aeadStream) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	type res struct {
		s   *aeadStream
		err error
	}
	ch := make(chan res, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			ch <- res{nil, err}
			return
		}
		s, err := acceptAeadStream(c, key)
		ch <- res{s, err}
	}()
	c, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	d, err := connectAeadStream(c, key)
	if err != nil {
		t.Fatal(err)
	}
	r := <-ch
	if r.err != nil {
		t.Fatal(r.err)
	}
	return d, r.s
}

func TestAeadBothDirectionsWork(t *testing.T) {
	key := deriveSessionKey([]byte("k"))
	d, a := pipePair(t, key)
	if err := d.sendFrame([]byte("to agent")); err != nil {
		t.Fatal(err)
	}
	if got, err := a.recvFrame(); err != nil || string(got) != "to agent" {
		t.Fatalf("agent got %q, %v", got, err)
	}
	if err := a.sendFrame([]byte("to client")); err != nil {
		t.Fatal(err)
	}
	if got, err := d.recvFrame(); err != nil || string(got) != "to client" {
		t.Fatalf("client got %q, %v", got, err)
	}
}

// v1 sealed frame N of both directions under one key and nonce. Each direction
// has its own key now, so identical plaintexts at the same counter differ.
func TestAeadDirectionsNeverShareKeyOrNonce(t *testing.T) {
	key := deriveSessionKey([]byte("k"))
	si, sr := bytes.Repeat([]byte{1}, 16), bytes.Repeat([]byte{2}, 16)
	i2r := deriveDirectionKey(key, si, sr, usbAesDirI2R)
	r2i := deriveDirectionKey(key, si, sr, usbAesDirR2I)
	if i2r == r2i {
		t.Fatal("the two directions share a key")
	}
	if deriveDirectionKey(key, si, bytes.Repeat([]byte{3}, 16), usbAesDirI2R) == i2r {
		t.Fatal("two connections share a key")
	}
	if deriveTunnelDirectionKey(key, usbAesDirI2R) == deriveTunnelDirectionKey(key, usbAesDirR2I) {
		t.Fatal("tunnel directions share a key")
	}
	iv := frameIV(1)
	g1, _ := newGCMCipher(i2r)
	g2, _ := newGCMCipher(r2i)
	aad := []byte{1, 0, 0, 0, 0, 0, 0, 0}
	if bytes.Equal(g1.Seal(nil, iv[:], []byte("identical"), aad), g2.Seal(nil, iv[:], []byte("identical"), aad)) {
		t.Fatal("same plaintext at the same counter sealed identically in both directions")
	}
}

// A frame cannot be reflected back at its sender, nor replayed, nor dropped.
func TestAeadRejectsReflectionReplayAndReorder(t *testing.T) {
	key := deriveSessionKey([]byte("k"))
	// reflection: the dialer's own sent bytes fed to the dialer's receive side
	d, _ := pipePair(t, key)
	iv := frameIV(1)
	var aad [8]byte
	aad[0] = 1
	ct := d.send.Seal(nil, iv[:], []byte("reflect me"), aad[:])
	if _, err := d.recv.Open(nil, iv[:], ct, aad[:]); err == nil {
		t.Fatal("a frame opened under the receive key of the side that sealed it")
	}

	// a dropped frame: the peer's next frame is #2 but #1 is expected
	d2, a2 := pipePair(t, key)
	if err := d2.sendFrame([]byte("one")); err != nil { // consumed by nobody
		t.Fatal(err)
	}
	_ = a2
	d2.sendCounter = 0 // the next frame goes out as #1 again: a repeated counter
	if err := d2.sendFrame([]byte("replayed counter")); err != nil {
		t.Fatal(err)
	}
	// the receiver reads frame "one" first, then the next frame carries counter 1
	// again but 2 is expected: it must not open
	if got, err := a2.recvFrame(); err != nil || string(got) != "one" {
		t.Fatalf("first frame: %q %v", got, err)
	}
	if _, err := a2.recvFrame(); err == nil {
		t.Fatal("a replayed (repeated counter) frame was accepted")
	}
}

// A recorded dialer stream cannot be replayed at a new connection: the acceptor's
// salt is fresh, so the keys differ.
func TestAeadRecordedSessionCannotBeReplayed(t *testing.T) {
	key := deriveSessionKey([]byte("k"))
	saltI := bytes.Repeat([]byte{5}, 16)
	// 1. a genuine session, built by hand so the bytes are known
	frameFor := func(saltR []byte) []byte {
		k := deriveDirectionKey(key, saltI, saltR, usbAesDirI2R)
		g, _ := newGCMCipher(k)
		iv := frameIV(1)
		ct := g.Seal(nil, iv[:], []byte("attach"), []byte{1, 0, 0, 0, 0, 0, 0, 0})
		pkt := append(iv[:], ct...)
		out := []byte{0, 0, 0, byte(len(pkt))}
		return append(out, pkt...)
	}
	attempt := func(replayFrom []byte) (saltR []byte, ok bool) {
		ln, _ := net.Listen("tcp", "127.0.0.1:0")
		defer ln.Close()
		done := make(chan bool, 1)
		go func() {
			c, _ := ln.Accept()
			defer c.Close()
			s, err := acceptAeadStream(c, key)
			if err != nil {
				done <- false
				return
			}
			c.SetReadDeadline(time.Now().Add(2 * time.Second))
			_, err = s.recvFrame()
			done <- err == nil
		}()
		c, _ := net.Dial("tcp", ln.Addr().String())
		defer c.Close()
		c.Write(append([]byte(usbAesHandshakeMagic), saltI...))
		saltR = make([]byte, 16)
		if _, err := c.Read(saltR); err != nil {
			t.Fatal(err)
		}
		if replayFrom == nil {
			c.Write(frameFor(saltR))
		} else {
			c.Write(replayFrom)
		}
		return saltR, <-done
	}
	saltR1, ok := attempt(nil)
	if !ok {
		t.Fatal("the genuine session was refused")
	}
	recorded := frameFor(saltR1)
	saltR2, ok := attempt(recorded)
	if bytes.Equal(saltR1, saltR2) {
		t.Fatal("the acceptor reused its salt")
	}
	if ok {
		t.Fatal("a replayed recording was accepted by a new connection")
	}
}

// An agent that predates v2 hangs up on the unknown "length"; the client says so.
func TestConnectToLegacyAgentReportsTooOld(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		c.Close() // what a v1 broker does with a 0x55414532-byte frame length
	}()
	c, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := connectAeadStream(c, deriveSessionKey([]byte("k"))); !errors.Is(err, ErrAgentTooOld) {
		t.Fatalf("err = %v, want ErrAgentTooOld", err)
	}
}

// The acceptor refuses a v1 peer rather than speaking the weak protocol.
func TestAcceptRefusesLegacyPeer(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	defer ln.Close()
	res := make(chan error, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			res <- err
			return
		}
		_, err = acceptAeadStream(c, deriveSessionKey([]byte("k")))
		res <- err
	}()
	c, _ := net.Dial("tcp", ln.Addr().String())
	defer c.Close()
	c.Write(append([]byte{0, 0, 0, 40}, make([]byte, 40)...))
	if err := <-res; err == nil {
		t.Fatal("a v1 peer was accepted")
	}
}

// Known answers computed by rust-shine (transport.rs interop_vectors): if either
// side's derivation drifts, the two stop being able to talk.
func TestInteropVectorsWithRust(t *testing.T) {
	key := deriveSessionKey([]byte("interop-master-key"))
	if got := hex.EncodeToString(key[:]); got != "4f2a1319a6b714597e385365cd24b395b8bc2d51285d6efde40883cffbd4e722" {
		t.Fatalf("session key %s", got)
	}
	si := make([]byte, 16)
	sr := make([]byte, 16)
	for i := range si {
		si[i] = byte(i)
		sr[i] = byte(16 + i)
	}
	i2r := deriveDirectionKey(key, si, sr, usbAesDirI2R)
	r2i := deriveDirectionKey(key, si, sr, usbAesDirR2I)
	if got := hex.EncodeToString(i2r[:]); got != "aa3a4ebd843cf9f59e98f7f7369288e875b5915ae17918175bddc3bf3b825292" {
		t.Fatalf("i2r %s", got)
	}
	if got := hex.EncodeToString(r2i[:]); got != "19149736ce71cf80fdbbf5cb5a36547b189270ca72e7e0ae5b17533db9ac52d9" {
		t.Fatalf("r2i %s", got)
	}
	tk := deriveTunnelKey(key, "1-1", []byte("nonce"))
	td := deriveTunnelDirectionKey(tk, usbAesDirI2R)
	if got := hex.EncodeToString(td[:]); got != "dda71e6944cd4206a4ac976ddfa646049f704762d1ec229d9aeacf82c01201e2" {
		t.Fatalf("tunnel i2r %s", got)
	}
	g, _ := newGCMCipher(i2r)
	iv := frameIV(1)
	ct := g.Seal(nil, iv[:], []byte("vector"), []byte{1, 0, 0, 0, 0, 0, 0, 0})
	if got := hex.EncodeToString(ct); got != "5a3612569eaa761183e9da8334390bc47f51f33408eb" {
		t.Fatalf("sealed frame %s", got)
	}
}
