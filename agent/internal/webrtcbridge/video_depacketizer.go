package webrtcbridge

import (
	"log"
)

// moonlightVideoDepacketizer strips Sunshine/GameStream's proprietary
// per-packet NV_VIDEO_PACKET framing off the raw RTP payloads
// moonlightclient.Session hands us, and reassembles each frame into a
// contiguous Annex-B H.264/H.265 byte stream -- the format
// webrtc.TrackLocalStaticSample.WriteSample expects, which internally
// re-packetizes it into standards-shaped RTP (RFC 6184 FU-A/single-NAL)
// for the browser.
//
// This exists because Moonlight/GameStream's RTP video packets are NOT
// standard RFC 6184 H.264-over-RTP despite superficially looking like it
// (same outer 12-byte RTP header) -- confirmed by reading
// moonlight-common-c/src/Video.h and VideoDepacketizer.c: every packet
// carries an extra 16-byte NV_VIDEO_PACKET header (streamPacketIndex,
// frameIndex, flags, extraFlags, multiFecFlags, multiFecBlocks, fecInfo)
// immediately after the RTP header, and the first packet of each frame
// additionally carries a short frame-type header before the actual Annex-B
// NAL data begins. A naive "forward the raw UDP payload straight into a
// WebRTC video track" (this package's original approach) produces bytes a
// browser's standard H.264 decoder can't parse at all -- it never errors,
// it just never decodes a single frame (confirmed live: RTCPeerConnection
// reports "connected" and the video MediaStreamTrack reports
// readyState="live", but the <video> element's own readyState/videoWidth
// never leave 0).
//
// Deliberately minimal compared to VideoDepacketizer.c's full
// implementation: no FEC handling (see sdp.go's fec.enable:0 -- nothing to
// recover from on a loopback UDP path that never actually drops/reorders
// packets in practice) and no frame-loss/corruption recovery bookkeeping
// (same reasoning). What's kept: the exact byte-layout skip logic
// (NV_VIDEO_PACKET header + conditional frame header) needed to produce a
// byte-correct Annex-B stream, which is the part that actually determines
// whether the browser's decoder can parse the result.
type moonlightVideoDepacketizer struct {
	frameBuf []byte
	inFrame  bool
	onFrame  func(annexB []byte)
}

func newMoonlightVideoDepacketizer(onFrame func(annexB []byte)) *moonlightVideoDepacketizer {
	return &moonlightVideoDepacketizer{onFrame: onFrame}
}

// nvVideoPacketHeaderSize is sizeof(NV_VIDEO_PACKET) in moonlight-common-c's
// Video.h: streamPacketIndex(4) + frameIndex(4) + flags(1) + extraFlags(1)
// + multiFecFlags(1) + multiFecBlocks(1) + fecInfo(4) = 16 bytes.
const nvVideoPacketHeaderSize = 16

// Flags from moonlight-common-c/src/Video.h.
const (
	flagContainsPicData = 0x1
	flagEOF             = 0x2
	flagSOF             = 0x4
)

// pushPacket feeds one RTP payload (the bytes after the 12-byte RTP header
// -- i.e. rtp.Packet.Payload) through the depacketizer. Calls onFrame
// whenever a complete frame has been reassembled.
func (d *moonlightVideoDepacketizer) pushPacket(payload []byte) {
	if len(payload) < nvVideoPacketHeaderSize {
		return // too short to even hold the NV_VIDEO_PACKET header -- drop
	}

	flags := payload[8]
	rest := payload[nvVideoPacketHeaderSize:]

	// isFirstPacket per VideoDepacketizer.c's isFirstPacket (with
	// fecBlockNumber always 0 since FEC is disabled): flags with the
	// picture-data bit masked off must be exactly SOF, or SOF|EOF for a
	// single-packet frame.
	maskedFlags := flags &^ flagContainsPicData
	firstPacket := maskedFlags == flagSOF || maskedFlags == (flagSOF|flagEOF)
	lastPacket := flags&flagEOF != 0

	if firstPacket {
		d.frameBuf = d.frameBuf[:0]
		d.inFrame = true

		// Instead of trusting VideoDepacketizer.c's marker-byte-selected
		// frameHeaderSize table (8 vs 24/41/44 bytes depending on exact
		// Sunshine APP_VERSION bracket): search for the real Annex-B start
		// code directly. Confirmed live against this agent's actual
		// Sunshine build (a fork, "itsme228/Sunshine" -- see
		// sunshine_backend.go) that the marker-byte table's assumptions
		// don't hold (logged "unexpected marker byte" on nearly every
		// packet, meaning the guessed header size was wrong far too often
		// to be a version-bracket edge case) -- searching for the actual
		// 00 00 00 01 / 00 00 01 prefix is what every Annex-B-aware
		// decoder does anyway when parsing frame headers of unknown/
		// variable length, and doesn't depend on correctly guessing which
		// Sunshine build's exact header layout applies.
		offset, found := findAnnexBStartCode(rest)
		if !found {
			d.inFrame = false
			return
		}
		rest = rest[offset:]
	}

	if !d.inFrame {
		return // mid-frame packet arrived without ever seeing SOF -- drop
	}

	d.frameBuf = append(d.frameBuf, rest...)

	if lastPacket {
		d.inFrame = false
		if len(d.frameBuf) > 0 && d.onFrame != nil {
			// Copy out: frameBuf is reused (truncated, not reallocated) on
			// the next SOF, and onFrame may hand this to a goroutine
			// (WriteSample) that outlives this call.
			frame := make([]byte, len(d.frameBuf))
			copy(frame, d.frameBuf)
			d.onFrame(frame)
		}
	}
}

// findAnnexBStartCode returns the offset of the first Annex-B NAL start
// code (00 00 01, or its more common 00 00 00 01 variant) in data, scanning
// only the first 64 bytes -- Sunshine's per-frame header (whatever its
// exact length, which varies by build/version, see the doc comment above)
// is always much shorter than that in practice, so this bounds the search
// instead of accidentally matching a start-code-shaped byte sequence deep
// inside actual NAL payload data.
func findAnnexBStartCode(data []byte) (offset int, found bool) {
	limit := len(data) - 3
	if scanLimit := 64; limit > scanLimit {
		limit = scanLimit
	}
	for i := 0; i <= limit; i++ {
		if data[i] == 0 && data[i+1] == 0 && data[i+2] == 1 {
			return i, true
		}
		if i+3 < len(data) && data[i] == 0 && data[i+1] == 0 && data[i+2] == 0 && data[i+3] == 1 {
			return i, true
		}
	}
	log.Printf("[webrtcbridge] no Annex-B start code found in first %d bytes of video frame data", limit+3)
	return 0, false
}
