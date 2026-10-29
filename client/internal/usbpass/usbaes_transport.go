//go:build linux || windows || darwin || (js && wasm)

package usbpass

// AES-256-GCM framing for the USB passthrough control plane, byte-for-byte
// compatible with rust-shine's crates/usb-passthrough/src/transport.rs
// (AeadStream). Linux and Windows export raw USB via libusb
// (backend_gousb.go); macOS exports HID devices via the non-exclusive
// IOHIDManager tap in hidbridge_darwin.go instead (see its doc comment for
// why libusb can't claim a HID interface there) — this control-plane
// exchange with the agent is identical either way, so all three platforms
// link this file. Only a platform with neither (hardware KVM, etc.) gets
// the stub in usbaes_attach_stub.go.
//
// wasm also links this: it's typed against net.Conn (an interface, not a
// concrete OS socket), and wasm's browser-sourced devices (usbaes_attach_wasm.go)
// speak this exact same AES-GCM framing to the agent, just over a
// platform.DialWebSocket-backed net.Conn instead of a raw net.Dialer.Dial
// one -- see wsconn_wasm.go's doc comment for why the transport has to
// differ there.

import (
	"bufio"
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

// usbAesInfo must match the Rust side's transport::INFO exactly.
const usbAesInfo = "usbridge-usb-passthrough-v1"

// usbTunnelInfo must match the Rust side's transport::TUNNEL_INFO exactly.
const usbTunnelInfo = "usbridge-usb-tunnel-v1"

const (
	usbAesMinFrame = 12 + 16 // nonce + GCM tag, empty plaintext
	usbAesMaxFrame = 16 * 1024 * 1024
)

// ErrAgentLicenseRequired is returned by recvFrame when the broker answers
// the very first frame with a plain-text HTTP response instead of the
// AES-framed protocol. Live-verified: rust-shine's entitlement gate for HID/
// pen passthrough on the closed usbridge-usb-broker binary rejects an
// unlicensed agent this way rather than an AES-encrypted HelloAck{ok:false}
// -- without this check, recvFrame reads "HTTP"'s 4 ASCII bytes as a
// length-prefix (0x48545450 = 1,213,486,160), fails usbAesMaxFrame's bounds
// check, and surfaces as an opaque "aes frame size out of range" error.
var ErrAgentLicenseRequired = errors.New("usb passthrough: this feature requires a license on the agent")

func deriveSessionKey(masterKey []byte) [32]byte {
	h := sha256.New()
	h.Write([]byte(usbAesInfo))
	h.Write(masterKey)
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

// deriveTunnelKey mirrors rust-shine's transport::derive_tunnel_key exactly:
// an ephemeral, single-attach key for the USB/IP data-plane tunnel, distinct
// per (session key, busID, nonce) so a leaked key only ever covers one
// attach of one device, never the whole pairing.
func deriveTunnelKey(sessionKey [32]byte, busID string, nonce []byte) [32]byte {
	h := sha256.New()
	h.Write([]byte(usbTunnelInfo))
	h.Write(sessionKey[:])
	h.Write([]byte(busID))
	h.Write(nonce)
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

// v2 of the control-plane framing (rust-shine transport.rs, same constants).
//
// v1 used one static key per pairing and an IV made of a counter plus a
// direction byte that both ends set to 'U' when sending, so frame N of the
// client and frame N of the agent shared a key and a nonce (AES-GCM nonce
// reuse), every connection started over at counter 1 under the same key, and a
// recorded session could be replayed. v2 gives every direction of every
// connection its own key, derived from the pairing key and a fresh random salt
// from each side:
//
//	dialer -> acceptor: "UAE2" || salt_i (16 bytes)
//	acceptor -> dialer: salt_r (16 bytes)
//	key(label) = SHA256("usbridge-usb-aead-v2" || key || salt_i || salt_r || label)
//	             label "i2r" for what the dialer sends, "r2i" for what the acceptor sends
//
// Frames are unchanged on the wire (length || IV || ciphertext+tag); the IV is
// the little-endian counter, and the receiver uses the counter it expects, so a
// replayed, dropped or reordered frame fails to open.
const (
	usbAesHandshakeMagic   = "UAE2"
	usbAesSaltLen          = 16
	usbAesAeadInfo         = "usbridge-usb-aead-v2"
	usbAesDirInfo          = "usbridge-usb-aead-dir-v2"
	usbAesDirI2R           = "i2r"
	usbAesDirR2I           = "r2i"
	usbAesHandshakeTimeout = 10 * time.Second
)

// ErrAgentTooOld is returned when the agent closes the connection during the
// v2 handshake: an agent that predates v2 reads the magic as a frame length and
// hangs up.
var ErrAgentTooOld = errors.New("usb passthrough: the agent does not speak the v2 handshake; update the agent / KVM firmware")

func deriveDirectionKey(key [32]byte, saltI, saltR []byte, label string) [32]byte {
	h := sha256.New()
	h.Write([]byte(usbAesAeadInfo))
	h.Write(key[:])
	h.Write(saltI)
	h.Write(saltR)
	h.Write([]byte(label))
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

// deriveTunnelDirectionKey splits a single-attach tunnel key by direction.
func deriveTunnelDirectionKey(key [32]byte, label string) [32]byte {
	h := sha256.New()
	h.Write([]byte(usbAesDirInfo))
	h.Write(key[:])
	h.Write([]byte(label))
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

// aeadStream mirrors Rust's AeadStream: one key and one monotonic counter per
// direction, the counter folded into both the GCM nonce and the AAD.
type aeadStream struct {
	send        cipher.AEAD
	recv        cipher.AEAD
	sendCounter uint64
	recvCounter uint64
	conn        net.Conn
}

func tuneConn(conn net.Conn) {
	if tc, ok := conn.(*net.TCPConn); ok {
		_ = tc.SetNoDelay(true)
		// A dead agent (crashed, network drop, machine asleep) otherwise
		// leaves recvFrame's blocking read parked forever in
		// holdAttachSession, so the client never notices and never retries.
		// Mirrors rust-shine's own TCP keepalive tuning on the agent side.
		_ = tc.SetKeepAliveConfig(net.KeepAliveConfig{
			Enable:   true,
			Idle:     10 * time.Second,
			Interval: 5 * time.Second,
			Count:    6,
		})
	}
}

func newAeadStreamFromKeys(conn net.Conn, sendKey, recvKey [32]byte, sendCounter, recvCounter uint64) (*aeadStream, error) {
	tuneConn(conn)
	send, err := newGCMCipher(sendKey)
	if err != nil {
		return nil, err
	}
	recv, err := newGCMCipher(recvKey)
	if err != nil {
		return nil, err
	}
	return &aeadStream{send: send, recv: recv, sendCounter: sendCounter, recvCounter: recvCounter, conn: conn}, nil
}

// connectAeadStream is the dialing side of the control plane (rust-shine's
// AeadStream::connect): the handshake above, then a stream keyed per direction.
func connectAeadStream(conn net.Conn, key [32]byte) (*aeadStream, error) {
	saltI := make([]byte, usbAesSaltLen)
	if _, err := rand.Read(saltI); err != nil {
		return nil, err
	}
	_ = conn.SetDeadline(time.Now().Add(usbAesHandshakeTimeout))
	if _, err := conn.Write(append([]byte(usbAesHandshakeMagic), saltI...)); err != nil {
		return nil, err
	}
	saltR := make([]byte, usbAesSaltLen)
	if _, err := io.ReadFull(conn, saltR); err != nil {
		// A v1 broker closes with our handshake still unread, which the
		// kernel answers with a reset rather than a clean close.
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || isConnReset(err) {
			return nil, ErrAgentTooOld
		}
		return nil, err
	}
	_ = conn.SetDeadline(time.Time{})
	return newAeadStreamFromKeys(conn,
		deriveDirectionKey(key, saltI, saltR, usbAesDirI2R),
		deriveDirectionKey(key, saltI, saltR, usbAesDirR2I), 0, 0)
}

// acceptAeadStream is the accepting side (rust-shine's AeadStream::accept).
// Only tests and in-process peers use it: the real acceptor is the broker.
func acceptAeadStream(conn net.Conn, key [32]byte) (*aeadStream, error) {
	_ = conn.SetDeadline(time.Now().Add(usbAesHandshakeTimeout))
	head := make([]byte, len(usbAesHandshakeMagic)+usbAesSaltLen)
	if _, err := io.ReadFull(conn, head); err != nil {
		return nil, err
	}
	if string(head[:4]) != usbAesHandshakeMagic {
		return nil, errors.New("peer does not speak the v2 handshake")
	}
	saltI := head[4:]
	saltR := make([]byte, usbAesSaltLen)
	if _, err := rand.Read(saltR); err != nil {
		return nil, err
	}
	if _, err := conn.Write(saltR); err != nil {
		return nil, err
	}
	_ = conn.SetDeadline(time.Time{})
	return newAeadStreamFromKeys(conn,
		deriveDirectionKey(key, saltI, saltR, usbAesDirR2I),
		deriveDirectionKey(key, saltI, saltR, usbAesDirI2R), 0, 0)
}

// newTunnelAeadStream is a stream over a single-attach tunnel key, the two
// directions keyed apart. The tunnel listener (this side) is the acceptor: it
// sends "r2i" and receives "i2r"; the relay that dials it does the opposite.
func newTunnelAeadStream(conn net.Conn, tunnelKey [32]byte, sendLabel, recvLabel string, sendCounter, recvCounter uint64) (*aeadStream, error) {
	return newAeadStreamFromKeys(conn,
		deriveTunnelDirectionKey(tunnelKey, sendLabel),
		deriveTunnelDirectionKey(tunnelKey, recvLabel), sendCounter, recvCounter)
}

func newGCMCipher(key [32]byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// frameIV is the little-endian counter, zero padded.
func frameIV(counter uint64) [12]byte {
	var iv [12]byte
	binary.LittleEndian.PutUint64(iv[0:8], counter)
	return iv
}

func (s *aeadStream) sendFrame(plaintext []byte) error {
	s.sendCounter++
	iv := frameIV(s.sendCounter)
	var aad [8]byte
	binary.LittleEndian.PutUint64(aad[:], s.sendCounter)
	ct := s.send.Seal(nil, iv[:], plaintext, aad[:])

	packet := make([]byte, 0, len(iv)+len(ct))
	packet = append(packet, iv[:]...)
	packet = append(packet, ct...)

	var lenb [4]byte
	binary.BigEndian.PutUint32(lenb[:], uint32(len(packet)))
	if _, err := s.conn.Write(lenb[:]); err != nil {
		return err
	}
	_, err := s.conn.Write(packet)
	return err
}

func (s *aeadStream) recvFrame() ([]byte, error) {
	var lenb [4]byte
	if _, err := io.ReadFull(s.conn, lenb[:]); err != nil {
		return nil, err
	}
	if string(lenb[:]) == "HTTP" {
		return nil, s.readLicenseRejection(lenb[:])
	}
	n := binary.BigEndian.Uint32(lenb[:])
	if n < usbAesMinFrame || n > usbAesMaxFrame {
		return nil, fmt.Errorf("aes frame size out of range (%d bytes)", n)
	}
	packet := make([]byte, n)
	if _, err := io.ReadFull(s.conn, packet); err != nil {
		return nil, err
	}
	s.recvCounter++
	var aad [8]byte
	binary.LittleEndian.PutUint64(aad[:], s.recvCounter)
	// The nonce is the counter expected here, not the bytes on the wire.
	iv := frameIV(s.recvCounter)
	pt, err := s.recv.Open(nil, iv[:], packet[12:], aad[:])
	if err != nil {
		return nil, fmt.Errorf("aes decrypt: %w", err)
	}
	return pt, nil
}

// readLicenseRejection finishes reading the plain-text HTTP response
// recvFrame detected (already-consumed bytes passed as prefix), logs the
// broker's actual status/body for diagnostics, and returns
// ErrAgentLicenseRequired regardless of parse success -- an HTTP response on
// this port is only ever the entitlement gate, so even a malformed one still
// means "no license".
func (s *aeadStream) readLicenseRejection(prefix []byte) error {
	resp, err := http.ReadResponse(bufio.NewReader(io.MultiReader(bytes.NewReader(prefix), s.conn)), nil)
	if err != nil {
		logrus.Warnf("usbpass: agent rejected hello with an unparsable HTTP response: %v", err)
		return ErrAgentLicenseRequired
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	logrus.Warnf("usbpass: agent rejected hello: HTTP %s: %s", resp.Status, strings.TrimSpace(string(body)))
	return ErrAgentLicenseRequired
}
