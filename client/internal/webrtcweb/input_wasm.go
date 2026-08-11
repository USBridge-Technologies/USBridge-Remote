//go:build js && wasm

package webrtcweb

import "encoding/binary"

// Byte-for-byte the same NV_INPUT_HEADER-prefixed wire packets real
// Moonlight clients send over the ENet control channel (moonlight-common-c's
// InputStream.c), reimplemented here in pure Go since the wasm build has no
// cgo to call LiSendKeyboardEvent/LiSendMouseMoveEvent/etc. through.
// rustshine's crates/enet-input/src/input_decode.rs::decode_input_packet
// parses this exact byte layout (magics/field offsets/endianness copied
// from there, not re-derived) -- this file's job is producing bytes that
// function is already known to accept correctly, not defining a new
// protocol. Only the packet types service.WebRTCVideoClient's
// MoonlightInputSender implementation actually needs are implemented
// (keyboard, mouse move/button/scroll, UTF-8 text) -- gamepad/touch/pen
// aren't wired up to any UI yet on this platform.
//
// Frame shape: size:BE32 (payload length AFTER this 4-byte field, i.e.
// magic + body) then magic:LE32, then type-specific body. Unlike the body
// fields below (which mix big-endian and little-endian per field, matching
// each packet type's own historical convention -- see decode_input_packet's
// own doc comments), the outer size/magic split is fixed.
const (
	magicKeyDown      = 0x00000003
	magicKeyUp        = 0x00000004
	magicMouseMoveAbs = 0x00000005
	magicMouseMoveRel = 0x00000007
	magicMouseBtnDown = 0x00000008
	magicMouseBtnUp   = 0x00000009
	magicScroll       = 0x0000000A
	magicHScroll      = 0x55000001
	magicUtf8Text     = 0x00000017
)

func buildInputPacket(magic uint32, body []byte) []byte {
	buf := make([]byte, 8+len(body))
	binary.BigEndian.PutUint32(buf[0:4], uint32(4+len(body)))
	binary.LittleEndian.PutUint32(buf[4:8], magic)
	copy(buf[8:], body)
	return buf
}

// EncodeMouseMoveRelative matches NV_REL_MOUSE_MOVE_PACKET's body:
// deltaX/deltaY, both big-endian i16.
func EncodeMouseMoveRelative(dx, dy int16) []byte {
	body := make([]byte, 4)
	binary.BigEndian.PutUint16(body[0:2], uint16(dx))
	binary.BigEndian.PutUint16(body[2:4], uint16(dy))
	return buildInputPacket(magicMouseMoveRel, body)
}

// EncodeMouseMoveAbsolute matches NV_ABS_MOUSE_MOVE_PACKET's body: x, y,
// unused, width, height -- all big-endian i16/u16 (the "unused" field is a
// real field on the wire, per decode_input_packet's own body[6:8]/body[8:10]
// offsets for width/height, not a gap we can skip).
func EncodeMouseMoveAbsolute(x, y, width, height int16) []byte {
	body := make([]byte, 10)
	binary.BigEndian.PutUint16(body[0:2], uint16(x))
	binary.BigEndian.PutUint16(body[2:4], uint16(y))
	// body[4:6] left zero (unused field)
	binary.BigEndian.PutUint16(body[6:8], uint16(width))
	binary.BigEndian.PutUint16(body[8:10], uint16(height))
	return buildInputPacket(magicMouseMoveAbs, body)
}

// EncodeMouseButton matches the 1-byte button-code body every
// button-down/up packet carries (decode_input_packet reads body[0] only).
func EncodeMouseButton(button uint8, pressed bool) []byte {
	magic := uint32(magicMouseBtnUp)
	if pressed {
		magic = magicMouseBtnDown
	}
	return buildInputPacket(magic, []byte{button})
}

// EncodeScroll matches SCROLL_MAGIC_GEN5's 2-byte big-endian amount body.
func EncodeScroll(amount int16) []byte {
	body := make([]byte, 2)
	binary.BigEndian.PutUint16(body, uint16(amount))
	return buildInputPacket(magicScroll, body)
}

// EncodeHScroll matches SS_HSCROLL_MAGIC's 2-byte big-endian amount body.
func EncodeHScroll(amount int16) []byte {
	body := make([]byte, 2)
	binary.BigEndian.PutUint16(body, uint16(amount))
	return buildInputPacket(magicHScroll, body)
}

// EncodeKey matches NV_KEYBOARD_PACKET's body: flags:u8 (always 0, unused
// by decode_input_packet), keyCode:LE16 (only the low byte is read on the
// decode side, matching input.cpp's own `& 0x00FF` masking -- see
// decode_input_packet's doc comment on InputEvent::Key), modifiers:u8,
// zero2:LE16 padding.
func EncodeKey(vkCode uint8, modifiers uint8, pressed bool) []byte {
	magic := uint32(magicKeyUp)
	if pressed {
		magic = magicKeyDown
	}
	body := make([]byte, 6)
	// body[0] flags left zero
	binary.LittleEndian.PutUint16(body[1:3], uint16(vkCode))
	body[3] = modifiers
	// body[4:6] left zero
	return buildInputPacket(magic, body)
}

// EncodeUtf8Text matches NV_UNICODE_PACKET's body: raw UTF-8 bytes, no
// length prefix or NUL padding of their own (the outer size:BE32 field
// already carries the exact length) -- decode_input_packet takes
// body.len() as the string length directly.
func EncodeUtf8Text(text string) []byte {
	return buildInputPacket(magicUtf8Text, []byte(text))
}
