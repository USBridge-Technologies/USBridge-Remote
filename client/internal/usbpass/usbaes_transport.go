//go:build linux || windows

package usbpass

// AES-256-GCM framing for the USB passthrough control plane, byte-for-byte
// compatible with rust-shine's crates/usb-passthrough/src/transport.rs
// (AeadStream). Only Linux and Windows ship real USB passthrough today;
// other platforms (hardware KVM, macOS — HID only) get the stub in
// usbaes_attach_stub.go instead of linking this file.

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"net"
)

// usbAesInfo must match the Rust side's transport::INFO exactly.
const usbAesInfo = "usbridge-usb-passthrough-v1"

const (
	usbAesMinFrame = 12 + 16       // nonce + GCM tag, empty plaintext
	usbAesMaxFrame = 16 * 1024 * 1024
)

func deriveSessionKey(masterKey []byte) [32]byte {
	h := sha256.New()
	h.Write([]byte(usbAesInfo))
	h.Write(masterKey)
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

// aeadStream mirrors Rust's AeadStream: independent monotonic counters for
// each direction, folded into both the GCM nonce and the AAD so a frame
// replayed or reordered on the wire fails to decrypt.
type aeadStream struct {
	gcm         cipher.AEAD
	sendCounter uint64
	recvCounter uint64
	conn        net.Conn
}

func newAeadStream(conn net.Conn, key [32]byte) (*aeadStream, error) {
	if tc, ok := conn.(*net.TCPConn); ok {
		_ = tc.SetNoDelay(true)
	}
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &aeadStream{gcm: gcm, conn: conn}, nil
}

func frameIV(counter uint64, sending bool) [12]byte {
	var iv [12]byte
	binary.LittleEndian.PutUint64(iv[0:8], counter)
	if sending {
		iv[11] = 'U'
	} else {
		iv[11] = 'u'
	}
	return iv
}

func (s *aeadStream) sendFrame(plaintext []byte) error {
	s.sendCounter++
	iv := frameIV(s.sendCounter, true)
	var aad [8]byte
	binary.LittleEndian.PutUint64(aad[:], s.sendCounter)
	ct := s.gcm.Seal(nil, iv[:], plaintext, aad[:])

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
	nonce := packet[:12]
	pt, err := s.gcm.Open(nil, nonce, packet[12:], aad[:])
	if err != nil {
		return nil, fmt.Errorf("aes decrypt: %w", err)
	}
	return pt, nil
}
