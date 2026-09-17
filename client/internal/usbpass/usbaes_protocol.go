//go:build linux || windows || darwin

package usbpass

// Tagged frame codec matching rust-shine's crates/usb-passthrough/src/{protocol,codec}.rs.
// Only the subset the client role speaks is implemented: Hello, HelloAck,
// Attach, Detach, Reset. UrbSubmit/UrbComplete/Unlink never cross this
// connection — Go owns the USB/IP export on :3240 directly.

import (
	"encoding/binary"
	"fmt"
)

const usbpMagic = "USBP"
const usbpVersion byte = 1

const (
	msgHello    byte = 1
	msgHelloAck byte = 2
	msgAttach   byte = 3
	msgDetach   byte = 4
	msgReset    byte = 8
)

const usbAesRoleClient byte = 1

type attachPayload struct {
	BusID         string
	VID           uint16
	PID           uint16
	Speed         byte
	DeviceDesc    []byte
	ConfigDesc    []byte
	ExportHost    string
	ExportService string
}

func appendUint16(out []byte, v uint16) []byte {
	var b [2]byte
	binary.BigEndian.PutUint16(b[:], v)
	return append(out, b[:]...)
}

func appendUint32(out []byte, v uint32) []byte {
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], v)
	return append(out, b[:]...)
}

func appendUsbpBytes(out, data []byte) []byte {
	out = appendUint32(out, uint32(len(data)))
	return append(out, data...)
}

func appendUsbpString(out []byte, s string) []byte {
	return appendUsbpBytes(out, []byte(s))
}

func encodeUsbpFrame(msgType byte, payload []byte) []byte {
	out := make([]byte, 0, 12+len(payload))
	out = append(out, usbpMagic...)
	out = append(out, usbpVersion, msgType, 0, 0)
	out = appendUint32(out, uint32(len(payload)))
	out = append(out, payload...)
	return out
}

func encodeHelloFrame(role, version byte) []byte {
	return encodeUsbpFrame(msgHello, []byte{role, version})
}

func encodeAttachFrame(a attachPayload) []byte {
	var p []byte
	p = appendUsbpString(p, a.BusID)
	p = appendUint16(p, a.VID)
	p = appendUint16(p, a.PID)
	p = append(p, a.Speed)
	p = appendUsbpBytes(p, a.DeviceDesc)
	p = appendUsbpBytes(p, a.ConfigDesc)
	p = appendUsbpString(p, a.ExportHost)
	p = appendUsbpString(p, a.ExportService)
	return encodeUsbpFrame(msgAttach, p)
}

func encodeDetachFrame(devid uint32) []byte {
	return encodeUsbpFrame(msgDetach, appendUint32(nil, devid))
}

// decodeUsbpFrame splits the 12-byte header from the payload without
// interpreting it — callers decode further based on msgType.
func decodeUsbpFrame(buf []byte) (msgType byte, payload []byte, err error) {
	if len(buf) < 12 {
		return 0, nil, fmt.Errorf("truncated USB passthrough frame")
	}
	if string(buf[0:4]) != usbpMagic {
		return 0, nil, fmt.Errorf("bad magic")
	}
	if buf[4] != usbpVersion {
		return 0, nil, fmt.Errorf("unsupported version %d", buf[4])
	}
	msgType = buf[5]
	plen := binary.BigEndian.Uint32(buf[8:12])
	if uint32(len(buf)) < 12+plen {
		return 0, nil, fmt.Errorf("truncated payload")
	}
	return msgType, buf[12 : 12+plen], nil
}

func readUsbpString(buf []byte, i int) (string, int, error) {
	if len(buf) < i+4 {
		return "", i, fmt.Errorf("truncated string length")
	}
	n := int(binary.BigEndian.Uint32(buf[i : i+4]))
	i += 4
	if len(buf) < i+n {
		return "", i, fmt.Errorf("truncated string")
	}
	return string(buf[i : i+n]), i + n, nil
}

func decodeHelloAck(p []byte) (ok bool, detail string, err error) {
	if len(p) < 1 {
		return false, "", fmt.Errorf("short hello-ack payload")
	}
	ok = p[0] != 0
	detail, _, err = readUsbpString(p, 1)
	return ok, detail, err
}
