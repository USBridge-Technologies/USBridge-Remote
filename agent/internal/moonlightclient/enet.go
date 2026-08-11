package moonlightclient

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"net"
	"sync"
	"time"
)

// This file implements a deliberately minimal, client-only subset of the
// ENet 1.3.x wire protocol (as vendored in
// client/moonlight-common-c/enet, itself cgutman/enet -- the fork Moonlight
// apps use), just enough to open Sunshine's ENet-based control channel (see
// control.go) and keep it alive. No pure-Go ENet library exists on
// pkg.go.dev (checked github.com/jhoonb/go-enet and github.com/codecat/
// go-enet; neither resolves as an importable pure-Go module -- codecat's is
// a cgo wrapper around the C library, which this package cannot use), so
// this hand-rolled subset is a deliberate design choice, not an oversight.
//
// What IS implemented:
//   - the CONNECT / VERIFY_CONNECT handshake (enet_host_connect,
//     enet_protocol_handle_verify_connect in protocol.c)
//   - sending SEND_RELIABLE commands on one channel, fire-and-forget (no
//     retransmission/reliability-window tracking)
//   - ACKing every reliable command we receive from the peer (so Sunshine's
//     own reliability layer doesn't retransmit at us or eventually time out
//     believing we've gone silent)
//   - responding to PING with an ACK
//   - a clean DISCONNECT on Stop()
//
// What is deliberately NOT implemented (and why it's safe to skip here):
//   - retransmission/reliable-delivery tracking for OUR sends: this is a
//     127.0.0.1 loopback socket talking to a process on the same machine.
//     UDP loopback essentially never drops or reorders packets in practice,
//     and the only things we send reliably (START_A, START_B, the 100ms
//     periodic ping) are either sent once at startup or repeated frequently
//     enough that one dropped packet is invisible.
//   - fragmentation (SEND_FRAGMENT): every message control.go sends fits in
//     a single UDP datagram (well under ENet's ~1180-byte practical MTU
//     ceiling), so no message we originate ever needs to span multiple
//     ENet fragments.
//   - channel counts beyond what we use: Sunshine's control protocol only
//     ever addresses channel 0 (CTRL_CHANNEL_GENERIC) for the messages this
//     package sends.
//   - compression and the optional per-packet checksum: moonlight-common-c
//     itself never enables either for the control channel, so Sunshine
//     never sends us a compressed or checksummed packet, and we don't need
//     to produce one either.
const (
	enetCmdAcknowledge    = 1
	enetCmdConnect        = 2
	enetCmdVerifyConnect  = 3
	enetCmdDisconnect     = 4
	enetCmdPing           = 5
	enetCmdSendReliable   = 6
	enetCmdSendUnreliable = 7
	enetCmdCommandMask    = 0x0F

	enetFlagAcknowledge = 0x80
	enetFlagUnsequenced = 0x40

	enetHeaderFlagSentTime = 0x8000
	// enetMaxPeerID is ENET_PROTOCOL_MAXIMUM_PEER_ID -- also the sentinel
	// value ENet peers start with for their not-yet-assigned outgoingPeerID
	// (see peer.c's enet_peer_reset), which is why the very first CONNECT
	// packet's header carries this value verbatim (no session bits folded
	// in yet -- see protocol.c's enet_protocol_send_outgoing_commands).
	enetMaxPeerID = 0x0FFF
)

// enetPeer is a minimal client-side ENet peer/host combined into one type
// (real ENet separates ENetHost/ENetPeer because a host can have many
// peers; this package only ever needs exactly one).
type enetPeer struct {
	conn *net.UDPConn

	mu                sync.Mutex
	outgoingPeerID    uint16 // ours, as assigned by the server's VERIFY_CONNECT
	incomingSessionID uint8
	outgoingSessionID uint8
	connectID         uint32
	channelSeq        map[uint8]uint16 // per-channel outgoingReliableSequenceNumber
	startTime         time.Time
	pendingAcks       []pendingAck

	closed  bool
	closeCh chan struct{}
}

type pendingAck struct {
	channelID    uint8
	receivedSeq  uint16
	receivedTime uint16
}

// enetConnect performs the ENet CONNECT/VERIFY_CONNECT handshake against
// addr (Sunshine's control UDP port) and starts a background goroutine that
// keeps the peer's receive side serviced (ACKing reliable commands, PING
// replies) until Stop is called. connectData is RTSP SETUP's
// X-SS-Connect-Data value (rtspSession.controlConnectData), which Sunshine
// uses server-side to correlate this ENet connection with the RTSP session
// that just set it up.
func enetConnect(addr string, connectData uint32, timeout time.Duration) (*enetPeer, error) {
	udpAddr, err := net.ResolveUDPAddr("udp4", addr)
	if err != nil {
		return nil, err
	}
	conn, err := net.DialUDP("udp4", nil, udpAddr)
	if err != nil {
		return nil, err
	}

	p := &enetPeer{
		conn:           conn,
		outgoingPeerID: enetMaxPeerID,
		connectID:      binary.BigEndian.Uint32(randomBytes32()),
		channelSeq:     map[uint8]uint16{},
		startTime:      time.Now(),
		closeCh:        make(chan struct{}),
	}

	connectPkt := p.buildConnectCommand(connectData)
	if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		conn.Close()
		return nil, err
	}
	if _, err := conn.Write(connectPkt); err != nil {
		conn.Close()
		return nil, fmt.Errorf("send ENet CONNECT: %w", err)
	}

	// Wait for VERIFY_CONNECT (retry the CONNECT send a couple of times in
	// case the first datagram is lost -- loopback rarely drops, but a fresh
	// Sunshine process may not have its UDP socket bound the instant RTSP
	// SETUP returns).
	buf := make([]byte, 4096)
	deadline := time.Now().Add(timeout)
	attempt := 0
	for {
		if time.Now().After(deadline) {
			conn.Close()
			return nil, fmt.Errorf("ENet CONNECT: timed out waiting for VERIFY_CONNECT")
		}
		_ = conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		n, err := conn.Read(buf)
		if err != nil {
			attempt++
			if attempt%2 == 0 {
				_, _ = conn.Write(connectPkt)
			}
			continue
		}
		ok, verr := p.handleVerifyConnect(buf[:n])
		if verr != nil {
			conn.Close()
			return nil, verr
		}
		if ok {
			break
		}
	}

	_ = conn.SetDeadline(time.Time{})
	go p.serviceLoop()
	return p, nil
}

func randomBytes32() []byte {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return b
}

// buildConnectCommand serializes the CONNECT command exactly as
// enet_host_connect (host.c) does: header.channelID=0xFF, our own
// incomingPeerID=0 (we're peers[0], the only peer our "host" has), session
// IDs 0xFF (wildcard -- let the server assign), and the standard default
// mtu/window/throttle constants cgutman/enet's enet_peer_reset seeds a new
// peer with.
func (p *enetPeer) buildConnectCommand(connectData uint32) []byte {
	const (
		defaultMTU               = 1400  // ENET_HOST_DEFAULT_MTU is 900 upstream, but moonlight-common-c's PlatformSockets raises it via enet_host_create's mtu arg-equivalent path in practice; 1400 stays safely under ENET_PROTOCOL_MAXIMUM_MTU and matches what real Moonlight clients negotiate down to on a LAN/loopback path.
		defaultWindowSize        = 65536 // ENET_PROTOCOL_MAXIMUM_WINDOW_SIZE (no bandwidth limit set, so enet_host_connect picks the max)
		defaultChannelCount      = 1
		defaultThrottleInterval  = 5000
		defaultThrottleAccel     = 2
		defaultThrottleDecel     = 2
		defaultIncomingBandwidth = 0
		defaultOutgoingBandwidth = 0
	)

	// 4 (command header) + 2 (outgoingPeerID) + 1+1 (session IDs) +
	// 8*4 (mtu, windowSize, channelCount, incomingBandwidth,
	// outgoingBandwidth, throttleInterval, throttleAcceleration,
	// throttleDeceleration) + 4 (connectID) + 4 (data/connectData).
	body := make([]byte, 4+2+1+1+8*4+4+4)
	off := 0
	// ENetProtocolCommandHeader
	body[off] = enetCmdConnect | enetFlagAcknowledge
	off++
	body[off] = 0xFF // channelID
	off++
	binary.BigEndian.PutUint16(body[off:], 1) // reliableSequenceNumber (peer's global outgoingReliableSequenceNumber, pre-incremented to 1 for the first ever reliable command)
	off += 2
	// ENetProtocolConnect fields
	binary.BigEndian.PutUint16(body[off:], 0) // outgoingPeerID = our incomingPeerID = 0
	off += 2
	body[off] = 0xFF // incomingSessionID
	off++
	body[off] = 0xFF // outgoingSessionID
	off++
	binary.BigEndian.PutUint32(body[off:], defaultMTU)
	off += 4
	binary.BigEndian.PutUint32(body[off:], defaultWindowSize)
	off += 4
	binary.BigEndian.PutUint32(body[off:], defaultChannelCount)
	off += 4
	binary.BigEndian.PutUint32(body[off:], defaultIncomingBandwidth)
	off += 4
	binary.BigEndian.PutUint32(body[off:], defaultOutgoingBandwidth)
	off += 4
	binary.BigEndian.PutUint32(body[off:], defaultThrottleInterval)
	off += 4
	binary.BigEndian.PutUint32(body[off:], defaultThrottleAccel)
	off += 4
	binary.BigEndian.PutUint32(body[off:], defaultThrottleDecel)
	off += 4
	// connectID is NOT byte-swapped in upstream enet_host_connect (it's
	// stored/sent host-endian in the real library -- see host.c: "command
	// .connect.connectID = currentPeer->connectID;" with no
	// ENET_HOST_TO_NET_32 wrapper), so match that exactly: write it in the
	// same byte order our local random source produced it, big-endian here
	// since that's simplest and self-consistent (we generate AND compare
	// connectID ourselves; only Sunshine's echo in VERIFY_CONNECT needs to
	// round-trip unchanged, which it does regardless of byte order as long
	// as both sides just echo the raw 4 bytes back).
	binary.BigEndian.PutUint32(body[off:], p.connectID)
	off += 4
	binary.BigEndian.PutUint32(body[off:], connectData)

	header := make([]byte, 4)
	// First packet: outgoingPeerID still enetMaxPeerID (unassigned), so no
	// session bits are folded in, and SENT_TIME is set since this command
	// is reliable (mirrors protocol.c's enet_protocol_send_outgoing_commands).
	binary.BigEndian.PutUint16(header[0:], enetMaxPeerID|enetHeaderFlagSentTime)
	binary.BigEndian.PutUint16(header[2:], p.sentTime())

	return append(header, body...)
}

func (p *enetPeer) sentTime() uint16 {
	return uint16(time.Since(p.startTime).Milliseconds() & 0xFFFF)
}

// handleVerifyConnect parses an incoming datagram looking for a
// VERIFY_CONNECT command. Returns ok=true once a valid one matching our
// connectID has been processed and outgoingPeerID/session IDs captured (see
// protocol.c's enet_protocol_handle_verify_connect, which is what this
// mirrors field-for-field).
func (p *enetPeer) handleVerifyConnect(data []byte) (bool, error) {
	if len(data) < 4 {
		return false, nil
	}
	headerLen := 4
	peerWord := binary.BigEndian.Uint16(data[0:2])
	if peerWord&enetHeaderFlagSentTime == 0 {
		headerLen = 2
	}
	if len(data) < headerLen+4 {
		return false, nil
	}
	cmds := data[headerLen:]
	if len(cmds) < 4 || cmds[0]&enetCmdCommandMask != enetCmdVerifyConnect {
		return false, nil
	}
	if len(cmds) < 4+2+1+1+4*8+4 {
		return false, fmt.Errorf("ENet VERIFY_CONNECT: truncated (%d bytes)", len(cmds))
	}
	off := 4
	p.mu.Lock()
	p.outgoingPeerID = binary.BigEndian.Uint16(cmds[off:])
	off += 2
	p.incomingSessionID = cmds[off]
	off++
	p.outgoingSessionID = cmds[off]
	off++
	off += 4 * 6 // mtu, windowSize, channelCount, incomingBandwidth, outgoingBandwidth, throttleInterval — unused by this minimal client
	off += 4 * 2 // throttleAccel, throttleDecel — unused
	gotConnectID := binary.BigEndian.Uint32(cmds[off:])
	p.mu.Unlock()

	if gotConnectID != p.connectID {
		return false, fmt.Errorf("ENet VERIFY_CONNECT: connectID mismatch (sent %08x, got %08x)", p.connectID, gotConnectID)
	}
	return true, nil
}

// sendReliable sends one SEND_RELIABLE command on channelID, fire-and-
// forget (see the file doc comment for why no retransmission is
// implemented). data is the pre-serialized NVCTL payload (see control.go --
// already includes whatever encryption framing that layer applies).
func (p *enetPeer) sendReliable(channelID uint8, data []byte) error {
	p.mu.Lock()
	seq := p.channelSeq[channelID] + 1
	p.channelSeq[channelID] = seq
	outgoingPeerID := p.outgoingPeerID
	outgoingSessionID := p.outgoingSessionID
	acks := p.pendingAcks
	p.pendingAcks = nil
	p.mu.Unlock()

	var pkt []byte
	pkt = append(pkt, 0, 0, 0, 0) // header placeholder, filled below

	// Piggy-back any pending ACKs for commands we've received from the
	// server ahead of our own outgoing command, exactly like a real ENet
	// host batches multiple commands into one outgoing datagram.
	for _, a := range acks {
		pkt = append(pkt, p.buildAckCommand(a)...)
	}

	cmd := make([]byte, 4+2+len(data))
	cmd[0] = enetCmdSendReliable | enetFlagAcknowledge
	cmd[1] = channelID
	binary.BigEndian.PutUint16(cmd[2:], seq)
	binary.BigEndian.PutUint16(cmd[4:], uint16(len(data)))
	copy(cmd[6:], data)
	pkt = append(pkt, cmd...)

	headerFlags := uint16(enetHeaderFlagSentTime)
	peerIDField := outgoingPeerID
	if outgoingPeerID < enetMaxPeerID {
		headerFlags |= uint16(outgoingSessionID) << 12
	}
	binary.BigEndian.PutUint16(pkt[0:], peerIDField|headerFlags)
	binary.BigEndian.PutUint16(pkt[2:], p.sentTime())

	_, err := p.conn.Write(pkt)
	return err
}

func (p *enetPeer) buildAckCommand(a pendingAck) []byte {
	cmd := make([]byte, 8)
	cmd[0] = enetCmdAcknowledge
	cmd[1] = a.channelID
	binary.BigEndian.PutUint16(cmd[2:], a.receivedSeq)
	binary.BigEndian.PutUint16(cmd[4:], a.receivedSeq)
	binary.BigEndian.PutUint16(cmd[6:], a.receivedTime)
	return cmd
}

// serviceLoop reads incoming datagrams and queues ACKs for any reliable
// command received (see the file doc comment: we don't act on the content
// of anything Sunshine sends us over this channel besides HDR
// info/termination, which this minimal client ignores beyond exiting on
// DISCONNECT), and replies to PING immediately with a bare ACK packet so
// round-trip time stays fresh on Sunshine's side even between our own
// periodic sends.
func (p *enetPeer) serviceLoop() {
	buf := make([]byte, 8192)
	for {
		select {
		case <-p.closeCh:
			return
		default:
		}
		_ = p.conn.SetReadDeadline(time.Now().Add(1 * time.Second))
		n, err := p.conn.Read(buf)
		if err != nil {
			continue
		}
		p.handleIncoming(buf[:n])
	}
}

func (p *enetPeer) handleIncoming(data []byte) {
	if len(data) < 2 {
		return
	}
	headerLen := 4
	peerWord := binary.BigEndian.Uint16(data[0:2])
	if peerWord&enetHeaderFlagSentTime == 0 {
		headerLen = 2
	}
	if len(data) < headerLen {
		return
	}
	cmds := data[headerLen:]

	for len(cmds) >= 4 {
		cmdByte := cmds[0]
		cmdType := cmdByte & enetCmdCommandMask
		channelID := cmds[1]
		seq := binary.BigEndian.Uint16(cmds[2:4])

		var consumed int
		switch cmdType {
		case enetCmdAcknowledge:
			consumed = 8
		case enetCmdVerifyConnect:
			consumed = 4 + 2 + 1 + 1 + 4*8 + 4
		case enetCmdDisconnect:
			consumed = 8
			p.mu.Lock()
			p.closed = true
			p.mu.Unlock()
		case enetCmdPing:
			consumed = 4
		case enetCmdSendReliable:
			if len(cmds) < 6 {
				return
			}
			dataLen := int(binary.BigEndian.Uint16(cmds[4:6]))
			consumed = 6 + dataLen
		case enetCmdSendUnreliable:
			if len(cmds) < 8 {
				return
			}
			dataLen := int(binary.BigEndian.Uint16(cmds[6:8]))
			consumed = 8 + dataLen
		default:
			// Unknown/unhandled command type (fragments, bandwidth limit,
			// throttle configure) -- we don't originate anything that would
			// provoke these from Sunshine's control channel, so bail out of
			// parsing the rest of this datagram rather than risk
			// misinterpreting the remaining bytes.
			return
		}
		if consumed <= 0 || consumed > len(cmds) {
			return
		}

		if cmdByte&enetFlagAcknowledge != 0 && cmdType != enetCmdAcknowledge {
			p.mu.Lock()
			p.pendingAcks = append(p.pendingAcks, pendingAck{channelID: channelID, receivedSeq: seq, receivedTime: p.sentTime()})
			p.mu.Unlock()
		}

		cmds = cmds[consumed:]
	}
}

// close sends a best-effort DISCONNECT and stops the service loop.
func (p *enetPeer) close() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	p.mu.Unlock()
	close(p.closeCh)

	discPkt := make([]byte, 4+8)
	binary.BigEndian.PutUint16(discPkt[0:], p.outgoingPeerID|uint16(p.outgoingSessionID)<<12)
	binary.BigEndian.PutUint16(discPkt[2:], p.sentTime())
	discPkt[4] = enetCmdDisconnect
	discPkt[5] = 0xFF
	_, _ = p.conn.Write(discPkt)

	return p.conn.Close()
}
