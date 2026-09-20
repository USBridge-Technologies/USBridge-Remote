//go:build linux || windows || darwin

package usbpass

// TunnelListener is the network-facing side of the USB/IP data-plane
// tunnel: the real USB/IP exporter (server.go) is bound to loopback only
// (see StartSession), so this is the only thing actually reachable from the
// agent. It speaks nothing but "AEAD or nothing" — a connection either
// decrypts its first frame under one of the currently-registered per-attach
// tunnel keys (see registerTunnelKey, called from Attach) or it gets
// dropped silently, indistinguishable from a port nobody is listening on.
//
// Once a key matches, the rest of the connection is a plain byte pump:
// decrypt from the network, forward to the real loopback exporter; encrypt
// whatever the exporter answers, send back. The tunnel key derivation
// itself (deriveTunnelKey, usbaes_transport.go) is what ties a connection to
// one specific attach/device — this file never inspects the USB/IP bytes it
// carries.

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// tunnelKeyTTL bounds how long a registered key stays valid if the agent
// never actually opens the tunnel (attach failed, network never reached it,
// etc.) — keys are also single-use, removed the moment one successfully
// authenticates a connection.
const tunnelKeyTTL = 60 * time.Second

type tunnelKeyEntry struct {
	busID     string
	key       [32]byte
	expiresAt time.Time
}

var (
	tunnelKeysMu sync.Mutex
	tunnelKeys   []tunnelKeyEntry
)

// registerTunnelKey makes key valid for one incoming tunnel connection for
// busID, until tunnelKeyTTL passes or it's consumed, whichever comes first.
// Called from Attach() right before the Attach frame carrying the matching
// nonce is sent, so the key is already live by the time the agent could
// possibly reach this listener.
func registerTunnelKey(busID string, key [32]byte) {
	tunnelKeysMu.Lock()
	defer tunnelKeysMu.Unlock()
	now := time.Now()
	live := tunnelKeys[:0]
	for _, e := range tunnelKeys {
		if e.expiresAt.After(now) {
			live = append(live, e)
		}
	}
	tunnelKeys = append(live, tunnelKeyEntry{busID: busID, key: key, expiresAt: now.Add(tunnelKeyTTL)})
}

// takeMatchingTunnelKey tries every currently-live registered key against
// packet (the tunnel's first AEAD frame, counter/AAD fixed at 1 — see
// aeadStream.recvFrame), returning the plaintext and consuming (removing)
// the key on the first match. Cheap: at most a handful of entries, tried
// only once per new connection, never per data frame.
func takeMatchingTunnelKey(packet []byte) (busID string, key [32]byte, plaintext []byte, ok bool) {
	tunnelKeysMu.Lock()
	defer tunnelKeysMu.Unlock()
	now := time.Now()
	live := tunnelKeys[:0]
	for _, e := range tunnelKeys {
		if !e.expiresAt.After(now) {
			continue
		}
		if ok {
			live = append(live, e)
			continue
		}
		if pt, err := probeDecryptFirstFrame(e.key, packet); err == nil {
			busID, key, plaintext, ok = e.busID, e.key, pt, true
			continue // consumed: not carried into `live`
		}
		live = append(live, e)
	}
	tunnelKeys = live
	return
}

func probeDecryptFirstFrame(key [32]byte, packet []byte) ([]byte, error) {
	if len(packet) < 12 {
		return nil, fmt.Errorf("short frame")
	}
	gcm, err := newGCMCipher(key)
	if err != nil {
		return nil, err
	}
	var aad [8]byte
	binary.LittleEndian.PutUint64(aad[:], 1) // first frame's fixed recvCounter
	return gcm.Open(nil, packet[:12], packet[12:], aad[:])
}

// TunnelListener is the public/network-facing endpoint for the USB/IP
// data-plane tunnel.
type TunnelListener struct {
	ln net.Listener
}

// StartTunnelListener listens on addr (e.g. "0.0.0.0:3240") and relays
// authenticated connections to exportAddr (the real, loopback-only USB/IP
// exporter — see StartExport).
func StartTunnelListener(addr, exportAddr string) (*TunnelListener, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	t := &TunnelListener{ln: ln}
	go t.acceptLoop(exportAddr)
	logrus.Infof("usbpass: USB/IP tunnel listen %s -> %s", addr, exportAddr)
	return t, nil
}

func (t *TunnelListener) Stop() {
	_ = t.ln.Close()
}

func (t *TunnelListener) acceptLoop(exportAddr string) {
	for {
		conn, err := t.ln.Accept()
		if err != nil {
			return
		}
		go handleTunnelConn(conn, exportAddr)
	}
}

func handleTunnelConn(conn net.Conn, exportAddr string) {
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	var lenb [4]byte
	if _, err := io.ReadFull(conn, lenb[:]); err != nil {
		return
	}
	n := binary.BigEndian.Uint32(lenb[:])
	if n < usbAesMinFrame || n > usbAesMaxFrame {
		return
	}
	packet := make([]byte, n)
	if _, err := io.ReadFull(conn, packet); err != nil {
		return
	}

	busID, key, plaintext, ok := takeMatchingTunnelKey(packet)
	if !ok {
		logrus.Debugf("usbpass: tunnel: unauthenticated connection from %s dropped", conn.RemoteAddr())
		return
	}
	_ = conn.SetReadDeadline(time.Time{})

	local, err := net.Dial("tcp", exportAddr)
	if err != nil {
		logrus.Warnf("usbpass: tunnel: dial local exporter %s: %v", exportAddr, err)
		return
	}
	defer local.Close()
	if _, err := local.Write(plaintext); err != nil {
		return
	}

	// sendCounter starts fresh (nothing sent yet on this connection);
	// recvCounter starts at 1 since the frame just probed above was the
	// real frame #1 — the stream below picks up at #2.
	stream, err := newAeadStreamWithCounters(conn, key, 0, 1)
	if err != nil {
		logrus.Warnf("usbpass: tunnel: aead setup for bus=%s: %v", busID, err)
		return
	}
	logrus.Infof("usbpass: tunnel: authenticated %s bus=%s", conn.RemoteAddr(), busID)
	pumpTunnel(stream, local)
}

// pumpTunnel copies bytes both ways between the AEAD-wrapped network
// connection and the plaintext local exporter connection until either side
// closes.
func pumpTunnel(stream *aeadStream, local net.Conn) {
	done := make(chan struct{}, 2)
	go func() {
		defer func() { done <- struct{}{} }()
		buf := make([]byte, 64*1024)
		for {
			n, err := local.Read(buf)
			if n > 0 {
				if werr := stream.sendFrame(buf[:n]); werr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()
	go func() {
		defer func() { done <- struct{}{} }()
		for {
			pt, err := stream.recvFrame()
			if err != nil {
				return
			}
			if _, werr := local.Write(pt); werr != nil {
				return
			}
		}
	}()
	<-done
	_ = local.Close()
	_ = stream.conn.Close()
}
