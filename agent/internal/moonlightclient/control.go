package moonlightclient

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// Sunshine's ENet control channel packet types, gen7-encrypted variant --
// see moonlight-common-c/src/ControlStream.c's packetTypesGen7Enc array
// (used whenever encryptedControlStream is true, i.e.
// APP_VERSION_AT_LEAST(7,1,431), which is unconditionally true for every
// Sunshine version this package targets).
const (
	ctlPtypeStartA           = 0x0302 // "Request IDR frame" numerically, but ALSO Sunshine's Start A signal -- see startControlStream's packetTypes[IDX_START_A] which aliases IDX_REQUEST_IDR_FRAME (both are index 0 in the gen7enc table).
	ctlPtypeStartB           = 0x0307
	ctlPtypeInvalidateFrames = 0x0301
	ctlPtypePeriodicPing     = 0x0200
	ctlChannelGeneric        = 0x00

	// aesGCMTagLength matches ControlStream.c's AES_GCM_TAG_LENGTH.
	aesGCMTagLength = 16
)

var (
	startAPayload = []byte{0, 0} // requestIdrFrameGen7Enc / startAGen5 shape: 2 zero bytes
	startBPayload = []byte{0}    // startBGen5 shape: 1 zero byte
)

// controlChannel owns the ENet peer for Sunshine's control connection plus
// the AES-GCM state needed to frame every NVCTL message the way
// ControlStream.c's sendMessageEnet/encryptControlMessage do, and the
// goroutine that sends the periodic keepalive ping.
//
// Encryption scheme: Sunshine always encrypts the control channel once
// paired with a gen7+ client (encryptedControlStream =
// APP_VERSION_AT_LEAST(7,1,431), unconditional). What's negotiable is only
// the *IV construction* -- ControlStream.c's encryptControlMessage branches
// on "EncryptionFeaturesEnabled & SS_ENC_CONTROL_V2": if we asked for V2 (a
// 12-byte IV with 'C'/'C' markers), Sunshine uses that; otherwise it falls
// back to the "legacy" scheme (a 16-byte IV whose only non-zero byte is the
// low byte of the message sequence number). buildSDP (sdp.go) sends
// "x-ss-general.encryptionEnabled:0", explicitly not requesting V2, so this
// implementation only needs the legacy IV scheme -- one fewer moving part
// for a control channel that has nothing sensitive to protect anyway (no
// remote input is ever sent).
//
// Key: StreamConfig.remoteInputAesKey in moonlight-common-c terms, i.e. the
// exact same 16 "rikey" bytes sent to /launch (nvhttp.go's launch()).
type controlChannel struct {
	peer *enetPeer
	aead cipher.AEAD
	seq  uint32 // currentEnetSequenceNumber equivalent -- also used as the legacy IV's low byte

	stopOnce sync.Once
	stopCh   chan struct{}
	wg       sync.WaitGroup

	idrRequested atomic.Bool
}

func newControlChannel(peer *enetPeer, rikey []byte) (*controlChannel, error) {
	block, err := aes.NewCipher(rikey)
	if err != nil {
		return nil, fmt.Errorf("control channel AES key: %w", err)
	}
	// The legacy (non-V2) IV ControlStream.c uses is 16 bytes (see
	// encryptAndFrame below), not AES-GCM's usual 12-byte nonce -- Go's
	// crypto/cipher requires an explicit NewGCMWithNonceSize for that.
	// The tag stays the standard 16 bytes (aesGCMTagLength), which is
	// already cipher.NewGCM's default.
	aead, err := cipher.NewGCMWithNonceSize(block, 16)
	if err != nil {
		return nil, err
	}
	return &controlChannel{peer: peer, aead: aead, stopCh: make(chan struct{})}, nil
}

// encryptAndFrame builds one NVCTL_ENCRYPTED_PACKET_HEADER + AES-GCM tag +
// ciphertext(NVCTL_ENET_PACKET_HEADER_V2 + payload) buffer exactly as
// ControlStream.c's sendMessageEnet does for the encryptedControlStream=true
// path. All multi-byte header fields are little-endian (moonlight-common-c
// LE16/LE32-swaps them right before encryption -- see encryptControlMessage
// -- unlike the ENet transport header itself, which is big-endian; these
// are two independent framing layers).
func (c *controlChannel) encryptAndFrame(ptype uint16, payload []byte) ([]byte, error) {
	seq := atomic.AddUint32(&c.seq, 1) - 1

	// Legacy (non-V2) IV: 16 bytes, only byte 0 set, to the truncated
	// sequence number -- ControlStream.c: "iv[0] = (unsigned char)encPacket->seq;"
	iv := make([]byte, 16)
	iv[0] = byte(seq)

	plain := make([]byte, 4+len(payload))
	binary.LittleEndian.PutUint16(plain[0:], ptype)
	binary.LittleEndian.PutUint16(plain[2:], uint16(len(payload)))
	copy(plain[4:], payload)

	ciphertext := c.aead.Seal(nil, iv, plain, nil)
	// Go's cipher.AEAD.Seal appends the tag after the ciphertext; Sunshine's
	// wire format wants the tag BEFORE the ciphertext (NVCTL_ENCRYPTED_
	// PACKET_HEADER is immediately followed by the 16-byte tag, then the
	// ciphertext -- see PltEncryptMessage's call in encryptControlMessage,
	// which writes the tag into "encPacket + 1" and ciphertext after that).
	if len(ciphertext) < aesGCMTagLength {
		return nil, fmt.Errorf("control channel: GCM output shorter than tag")
	}
	tag := ciphertext[len(ciphertext)-aesGCMTagLength:]
	body := ciphertext[:len(ciphertext)-aesGCMTagLength]

	out := make([]byte, 2+2+4+aesGCMTagLength+len(body))
	binary.LittleEndian.PutUint16(out[0:], 0x0001) // encryptedHeaderType
	binary.LittleEndian.PutUint16(out[2:], uint16(4+aesGCMTagLength+len(plain)))
	binary.LittleEndian.PutUint32(out[4:], seq)
	copy(out[8:], tag)
	copy(out[8+aesGCMTagLength:], body)
	return out, nil
}

// sendMessage encrypts+frames ptype/payload and sends it reliably on the
// generic control channel, mirroring ControlStream.c's
// sendMessageAndDiscardReply for the ENet (AppVersionQuad[0]>=5) path --
// which, for ENet, is really just sendMessageEnet with no reply wait at all
// (the "discard reply" naming is a holdover from the pre-ENet TCP path).
func (c *controlChannel) sendMessage(ptype uint16, payload []byte) error {
	framed, err := c.encryptAndFrame(ptype, payload)
	if err != nil {
		return err
	}
	return c.peer.sendReliable(ctlChannelGeneric, framed)
}

// start sends START_A then START_B -- the two messages that actually tell
// Sunshine's gamestream-server to start producing frames (see
// ControlStream.c's startControlStream) -- then launches the periodic
// keepalive ping loop. Without the 100ms periodic ping (usePeriodicPing =
// APP_VERSION_AT_LEAST(7,1,415), true for every Sunshine version), Sunshine
// treats the client as gone after its own idle timeout and tears the
// session down.
func (c *controlChannel) start() error {
	if err := c.sendMessage(ctlPtypeStartA, startAPayload); err != nil {
		return fmt.Errorf("send Start A: %w", err)
	}
	if err := c.sendMessage(ctlPtypeStartB, startBPayload); err != nil {
		return fmt.Errorf("send Start B: %w", err)
	}

	c.wg.Add(1)
	go c.keepaliveLoop()
	return nil
}

// keepaliveLoop reproduces ControlStream.c's lossStatsThreadFunc in
// usePeriodicPing mode: a 0x0200 "periodic ping" message, 8-byte payload,
// sent reliably every PERIODIC_PING_INTERVAL_MS (100ms). We skip the
// Sunshine-specific per-frame FEC status messages that real thread also
// sends (SS_FRAME_FEC_PTYPE) -- those report OUR decode/loss statistics
// back to Sunshine for its adaptive FEC tuning, which doesn't apply here
// since nothing in this package decodes the video; omitting them costs
// nothing but slightly less optimal FEC tuning on Sunshine's side.
func (c *controlChannel) keepaliveLoop() {
	defer c.wg.Done()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	payload := make([]byte, 8) // length=4 (u16 LE) + timestamp=0 (u32 LE) + 2 zero pad bytes
	binary.LittleEndian.PutUint16(payload[0:], 4)
	binary.LittleEndian.PutUint32(payload[2:], 0)

	for {
		select {
		case <-c.stopCh:
			return
		case <-ticker.C:
			if err := c.sendMessage(ctlPtypePeriodicPing, payload); err != nil {
				// A send failure here almost always means the UDP socket
				// (and therefore the whole session) is already dead --
				// nothing productive to do but stop trying.
				return
			}
			if c.idrRequested.CompareAndSwap(true, false) {
				_ = c.sendMessage(ctlPtypeStartA, startAPayload) // ptype 0x0302 doubles as "Request IDR frame" -- see ctlPtypeStartA's doc comment
			}
		}
	}
}

// requestIDRFrame asks Sunshine to send a fresh keyframe on the next
// keepalive tick. Exposed for a caller (e.g. webrtcbridge, on a new WebRTC
// viewer joining) that needs a keyframe without waiting for Sunshine's own
// periodic IDR interval.
func (c *controlChannel) requestIDRFrame() {
	c.idrRequested.Store(true)
}

// stop halts the keepalive loop and closes the underlying ENet peer.
func (c *controlChannel) stop() {
	c.stopOnce.Do(func() {
		close(c.stopCh)
	})
	c.wg.Wait()
	_ = c.peer.close()
}
