package moonlightclient

import (
	"encoding/binary"
	"fmt"
	"net"
	"time"
)

// Sunshine (like GFE before it) does not send video/audio RTP to whatever
// address the RTSP SETUP request came from -- RTSP runs over its own TCP
// connection, entirely separate from the RTP UDP sockets. Instead, exactly
// like a classic UDP hole-punch, the *client* must send a "ping" packet to
// Sunshine's fixed video/audio ports first; Sunshine learns the reply
// address from that packet's source address and starts sending RTP there.
// See moonlight-common-c/src/VideoStream.c's VideoPingThreadProc (and
// AudioStream.c's equivalent): it sends SS_PING (16-byte payload from RTSP
// SETUP's X-SS-Ping-Payload header, plus a big-endian sequence number) --
// or, if Sunshine didn't send that header, a legacy 4-byte ASCII "PING" --
// every 500ms, starting before the first frame is read, for the life of
// the connection.
//
// This package's simplification: since it only ever talks to Sunshine over
// 127.0.0.1 (no NAT, no firewall connection-state expiry -- see the
// package doc comment), a short burst of pings from the exact local
// port a caller is about to net.ListenUDP on is enough to establish the
// mapping once; continuous re-pinging isn't needed to keep it alive the
// way it would be for a real network path. See establishRTPAddr's doc
// comment for exactly how that hand-off race is made safe.
const legacyPingPayload = "PING"

// buildPingPacket returns the exact bytes VideoPingThreadProc/
// AudioPingThreadProc would send for sequence number seq. payload is the
// 16-byte X-SS-Ping-Payload captured from RTSP SETUP (rtspSession.
// video/audioPingPayload), or nil to use the legacy fallback.
func buildPingPacket(payload []byte, seq uint32) []byte {
	if len(payload) != 16 {
		return []byte(legacyPingPayload)
	}
	pkt := make([]byte, 20)
	copy(pkt[0:16], payload)
	binary.BigEndian.PutUint32(pkt[16:20], seq)
	return pkt
}

// establishRTPAddr sends a short burst of ping packets to Sunshine's fixed
// remotePort from a freshly bound local UDP socket, then closes that
// socket and returns the local port number it used.
//
// Why open-ping-close instead of holding the socket open: this package's
// public API (Session.VideoRTPAddr/AudioRTPAddr) hands back a plain
// "127.0.0.1:port" string for a *separate* caller (webrtcbridge) to
// net.ListenUDP on directly, rather than handing back a live *net.UDPConn
// -- matching the documented contract in session.go. Holding our own
// socket open on that port would make the caller's later ListenUDP on the
// same port fail with EADDRINUSE. Since this is loopback-only (no NAT
// state to expire), Sunshine remembers the learned (127.0.0.1, localPort)
// destination indefinitely once the burst below lands, so releasing the
// port immediately after and letting the caller bind moments later works
// reliably in practice -- the same "brief gap between one socket's close
// and the next bind" tolerance agent/internal/tailscale/stream_proxy.go's
// startUDPRelay doc comment describes for the same underlying Sunshine
// pipeline behavior.
func establishRTPAddr(host string, remotePort int, pingPayload []byte) (localPort int, err error) {
	remoteAddr, err := net.ResolveUDPAddr("udp4", fmt.Sprintf("%s:%d", host, remotePort))
	if err != nil {
		return 0, err
	}
	conn, err := net.DialUDP("udp4", nil, remoteAddr)
	if err != nil {
		return 0, fmt.Errorf("open ping socket to %s: %w", remoteAddr, err)
	}
	defer conn.Close()

	localAddr, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok {
		return 0, fmt.Errorf("unexpected local address type %T", conn.LocalAddr())
	}

	// A handful of pings spaced closely together, matching the reference's
	// 500ms steady-state interval but front-loaded: nothing is reading a
	// reply here (there isn't one -- this is one-way hole-punching), so
	// there's no round-trip to wait on between sends, and sending several
	// up front maximizes the chance Sunshine's socket was already ready to
	// receive at least one of them even if the very first lands before
	// Sunshine finished setting up in response to the ENet Start A/B
	// messages control.go just sent.
	for i := uint32(1); i <= 5; i++ {
		pkt := buildPingPacket(pingPayload, i)
		if _, werr := conn.Write(pkt); werr != nil {
			return localAddr.Port, fmt.Errorf("send ping %d: %w", i, werr)
		}
		time.Sleep(20 * time.Millisecond)
	}

	return localAddr.Port, nil
}
