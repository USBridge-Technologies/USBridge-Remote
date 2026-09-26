//go:build js && wasm

package platform

// Browser Gamepad API capture, feeding the browser-sourced USB/IP path
// (usbpass.AttachBrowserGamepad) rather than the semantic Moonlight
// controller-event path other platforms' gamepad_capture_*.go files feed --
// see the plan this shipped under for why: the goal is a synthetic Xbox 360
// controller the agent's own xusb22.sys binds to, sharing usbpass/
// x360_backend.go's code with the native USB-passthrough gamepad path,
// not a separate semantic-only mechanism.
//
// No WebHID/permission dialog is needed here (unlike pen_capture_wasm.go):
// the Gamepad API requires only that the user press a button on the pad at
// least once, per spec -- no page-level permission prompt at all.

import (
	"encoding/binary"
	"syscall/js"
	"time"
)

// browserGamepadFrameLen must match agent/internal/api/usb_passthrough_browser.go's
// browserGamepadFrameLen exactly -- this is that endpoint's one, otherwise
// undocumented wire contract.
const browserGamepadFrameLen = 12

// EncodeBrowserGamepadFrame builds one wire frame for the browser-gamepad
// relay WebSocket: buttons:u16, leftTrigger/rightTrigger:u8, four
// axes:i16 -- all little-endian, 12 bytes total.
func EncodeBrowserGamepadFrame(buttons uint16, leftTrigger, rightTrigger uint8, leftX, leftY, rightX, rightY int16) []byte {
	b := make([]byte, browserGamepadFrameLen)
	binary.LittleEndian.PutUint16(b[0:2], buttons)
	b[2] = leftTrigger
	b[3] = rightTrigger
	binary.LittleEndian.PutUint16(b[4:6], uint16(leftX))
	binary.LittleEndian.PutUint16(b[6:8], uint16(leftY))
	binary.LittleEndian.PutUint16(b[8:10], uint16(rightX))
	binary.LittleEndian.PutUint16(b[10:12], uint16(rightY))
	return b
}

// BrowserGamepadCapture is an active browser Gamepad API poller.
type BrowserGamepadCapture struct {
	stop chan struct{}
}

// gamepadPollInterval matches a real controller's own report cadence closely
// enough (a physical XInput pad polls at ~125-250Hz; the browser's own
// Gamepad API snapshot only actually changes once per rendered frame
// anyway, so polling faster than ~60Hz would just re-read the same
// unchanged snapshot).
const gamepadPollInterval = 16 * time.Millisecond

// StartBrowserGamepadCapture polls navigator.getGamepads() and calls onFrame with
// an EncodeBrowserGamepadFrame-shaped frame each time the first connected
// pad's state changes. Only the first connected pad is forwarded -- the
// agent's loopback export presents exactly one synthetic controller per
// browser attach (see agent/internal/browserusb.GamepadSession), same
// one-controller-per-attach shape usbpass/x360_backend.go already has
// natively.
func StartBrowserGamepadCapture(onFrame func([]byte)) *BrowserGamepadCapture {
	c := &BrowserGamepadCapture{stop: make(chan struct{})}
	go func() {
		ticker := time.NewTicker(gamepadPollInterval)
		defer ticker.Stop()
		var last []byte
		for {
			select {
			case <-c.stop:
				return
			case <-ticker.C:
				frame, ok := pollGamepad()
				if !ok {
					continue
				}
				if last != nil && string(frame) == string(last) {
					continue
				}
				last = frame
				onFrame(frame)
			}
		}
	}()
	return c
}

// Stop halts the polling goroutine.
func (c *BrowserGamepadCapture) Stop() { close(c.stop) }

func pollGamepad() ([]byte, bool) {
	pads := js.Global().Get("navigator").Call("getGamepads")
	length := pads.Get("length").Int()
	for i := 0; i < length; i++ {
		pad := pads.Index(i)
		if pad.IsNull() || pad.IsUndefined() || !pad.Get("connected").Bool() {
			continue
		}
		return decodeGamepad(pad), true
	}
	return nil, false
}

// decodeGamepad converts one W3C "standard gamepad" snapshot into an
// EncodeBrowserGamepadFrame-shaped frame. The standard mapping's button
// order (https://www.w3.org/TR/gamepad/#remapping) is already the same
// order XInput uses -- 0 A, 1 B, 2 X, 3 Y, 4 LB, 5 RB, 6 LT, 7 RT, 8 Back,
// 9 Start, 10 L3, 11 R3, 12-15 DPad Up/Down/Left/Right, 16 Guide -- which is
// exactly why none of this project's native XInput/evdev decoders
// (gamepad_sdlmap.go et al.) need a per-model remap table for a pad
// reporting this mapping either.

func decodeGamepad(pad js.Value) []byte {
	buttons := pad.Get("buttons")
	axes := pad.Get("axes")

	btnPressed := func(i int) bool {
		if i >= buttons.Length() {
			return false
		}
		return buttons.Index(i).Get("pressed").Bool()
	}
	btnValue := func(i int) float64 {
		if i >= buttons.Length() {
			return 0
		}
		return buttons.Index(i).Get("value").Float()
	}
	axis := func(i int) float64 {
		if i >= axes.Length() {
			return 0
		}
		return axes.Index(i).Float()
	}

	var flags uint16
	if btnPressed(0) {
		flags |= MoonlightButtonA
	}
	if btnPressed(1) {
		flags |= MoonlightButtonB
	}
	if btnPressed(2) {
		flags |= MoonlightButtonX
	}
	if btnPressed(3) {
		flags |= MoonlightButtonY
	}
	if btnPressed(4) {
		flags |= MoonlightButtonLeftShoulder
	}
	if btnPressed(5) {
		flags |= MoonlightButtonRightShoulder
	}
	if btnPressed(8) {
		flags |= MoonlightButtonBack
	}
	if btnPressed(9) {
		flags |= MoonlightButtonStart
	}
	if btnPressed(10) {
		flags |= MoonlightButtonLeftStick
	}
	if btnPressed(11) {
		flags |= MoonlightButtonRightStick
	}
	if btnPressed(12) {
		flags |= MoonlightButtonDPadUp
	}
	if btnPressed(13) {
		flags |= MoonlightButtonDPadDown
	}
	if btnPressed(14) {
		flags |= MoonlightButtonDPadLeft
	}
	if btnPressed(15) {
		flags |= MoonlightButtonDPadRight
	}

	leftTrigger := uint8(btnValue(6) * 255)
	rightTrigger := uint8(btnValue(7) * 255)

	toAxis := func(v float64) int16 {
		if v > 1 {
			v = 1
		} else if v < -1 {
			v = -1
		}
		return int16(v * 32767)
	}
	// Gamepad API's Y axes report "down = positive"; XInput/Moonlight's own
	// convention is "up = positive" (documented on x360_backend.go's
	// X360State) -- inverted here to match.
	leftX := toAxis(axis(0))
	leftY := -toAxis(axis(1))
	rightX := toAxis(axis(2))
	rightY := -toAxis(axis(3))

	return EncodeBrowserGamepadFrame(flags, leftTrigger, rightTrigger, leftX, leftY, rightX, rightY)
}
