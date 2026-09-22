//go:build js && wasm

package platform

// Browser Gamepad API enumeration -- what actually populates the "HID &
// Input Hub" dashboard's gamepad row(s) on the web client. Before this
// file existed, GOOS=js fell through to gamepad_stub.go's generic
// EnumerateGamepads (its build tag matched wasm too, since wasm satisfies
// none of darwin/linux/windows), which unconditionally returns nil -- so no
// gamepad ever showed up, with no error and no indication why, matching
// exactly what was reported.
//
// Unlike WebHID (pen_capture_wasm.go, once that lands) or macOS's IOKit
// gamepad path (gamepad_darwin.go), the W3C Gamepad API needs no page-level
// permission prompt at all: the spec requires only that the user press a
// button or move an axis on the pad at least once before
// navigator.getGamepads() starts returning it non-null. There is
// deliberately no "connect gamepad" button here for that reason -- one
// would have nothing to do that pressing a real button doesn't already do.

import (
	"regexp"
	"strconv"
	"syscall/js"
)

// GamepadDevice describes a system gamepad -- same shape as every other
// platform's GamepadDevice (gamepad_darwin.go/gamepad_linux.go/
// gamepad_windows.go/gamepad_stub.go), which disk_widget_data.go's
// loadGamepadDevices reads generically without a build-tag switch of its
// own.
type GamepadDevice struct {
	ID        string
	Name      string
	VendorID  string
	ProductID string
}

// browserGamepadVIDPID extracts "Vendor: XXXX Product: YYYY" from a
// Gamepad.id string -- the de facto format Chrome/Edge on Windows and
// Linux report for the vast majority of pads (sourced from the underlying
// HID/XInput device), e.g. "Xbox 360 Controller (XInput STANDARD GAMEPAD
// Vendor: 045e Product: 028e)". Not guaranteed by spec and not always
// present (notably typical on macOS, where Gamepad.id often omits it
// entirely) -- VendorID/ProductID are left empty when it doesn't match,
// same as this project's other platforms already do for a pad whose ids
// couldn't be read (see xinputPads' own "when known" comment).
var browserGamepadVIDPID = regexp.MustCompile(`(?i)vendor:\s*([0-9a-f]{4}).*?product:\s*([0-9a-f]{4})`)

// EnumerateGamepads returns every gamepad the browser currently exposes,
// preferring WebHID-granted devices (see gamepad_hid_wasm.go) over the
// Gamepad API: once the user has granted HID access to a pad, that grant is
// both more accurate (real vendor/product id, real per-model SDL mapping
// instead of Chrome's own -- sometimes incomplete -- gamepad database) and
// available without needing a button press first, so it fully replaces the
// Gamepad API entry rather than sitting next to it as a second row for the
// same physical pad. Gamepad API rows only appear when no HID grant exists
// yet at all -- the "still visible before the user does anything extra"
// fallback this project had before WebHID capture existed.
func EnumerateGamepads() []GamepadDevice {
	if hidPads := EnumerateHIDGamepads(); len(hidPads) > 0 {
		return hidPads
	}
	pads := js.Global().Get("navigator").Call("getGamepads")
	length := pads.Get("length").Int()
	out := make([]GamepadDevice, 0, length)
	for i := 0; i < length; i++ {
		pad := pads.Index(i)
		if pad.IsNull() || pad.IsUndefined() || !pad.Get("connected").Bool() {
			continue
		}
		id := pad.Get("id").String()
		dev := GamepadDevice{
			ID:   "browser:" + strconv.Itoa(i),
			Name: id,
		}
		if m := browserGamepadVIDPID.FindStringSubmatch(id); m != nil {
			dev.VendorID = "0x" + m[1]
			dev.ProductID = "0x" + m[2]
		}
		out = append(out, dev)
	}
	return out
}
