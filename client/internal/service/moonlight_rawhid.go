package service

import "sync/atomic"

// MoonlightRawHIDSender sends a local HID device (a Wacom tablet) to a
// USBridge host as it is, over the stream's control channel
// (LiSendRawHidEvent); the host rebuilds it as a virtual USB device for its
// own driver. usbpass.RawHIDSession drives it. Kept apart from
// MoonlightInputSender: only a native Moonlight stream can do this.
type MoonlightRawHIDSender interface {
	// RawHIDEpoch is non-zero while a stream is up whose host takes raw HID
	// devices, and changes with every new connection: the host forgets the
	// devices when a connection ends, so a new value means "send them again".
	RawHIDEpoch() uint64
	// SendMoonlightRawHID queues one chunk (at most 64 bytes of data). It
	// returns false if the chunk was not queued: no stream, or its input queue
	// is full.
	SendMoonlightRawHID(kind, slot, endpoint uint8, total, offset uint16, data []byte, reliable bool) bool
}

// liRawHIDEpoch counts stream connections (see RawHIDEpoch).
var liRawHIDEpoch atomic.Uint64

func (m *MoonlightService) RawHIDEpoch() uint64 {
	if w := m.activeWrapper; w != nil {
		return w.RawHIDEpoch()
	}
	return 0
}

func (m *MoonlightService) SendMoonlightRawHID(kind, slot, endpoint uint8, total, offset uint16, data []byte, reliable bool) bool {
	if w := m.activeWrapper; w != nil {
		return w.SendMoonlightRawHID(kind, slot, endpoint, total, offset, data, reliable)
	}
	return false
}

// MoonlightStreamEpoch changes with every new stream connection (0 before
// the first): a host forgets what a client announced when a connection
// ends, so a new value means "announce it again".
func MoonlightStreamEpoch() uint64 {
	return liRawHIDEpoch.Load()
}
