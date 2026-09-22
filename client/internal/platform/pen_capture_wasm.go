//go:build js && wasm

package platform

// WebHID pen tablet capture: the wasm counterpart of pen_capture_darwin.go,
// for a Wacom (or other digitizer-class HID) tablet granted via the same
// "Connect USB" button gamepad_hid_wasm.go's requestDevice() call uses (see
// index.html) -- distinguished from a gamepad grant by its top-level HID
// collection usage page (Digitizer, 0x0D) instead of Generic Desktop
// Gamepad/Joystick (see hidHasTopLevelUsage).
//
// Unlike the gamepad path, no client-side decoding happens here at all: a
// pen tablet's semantics (which bits are pressure, tilt, which report id is
// which interface) are exactly what usbpasscore.NewWacomExportedDevice's
// model resolution already knows on the agent side (see
// client/internal/usbpass/wacom_model.go) -- this file's only job is to
// hand the agent the exact bytes the tablet itself reported, the same "wire
// report" shape hidraw/IOHID already give that code on every other
// platform. WebHID strips the report id into a separate event.reportId
// field instead of leaving it as the first byte of event.data (unlike
// hidraw/IOHID) -- re-prepending it below is what makes the bytes handed to
// PushReport match that same wire shape.

import (
	"fmt"
	"syscall/js"
)

const hidUsagePageDigitizer = 0x0D

// PenTabletInfo describes a Wacom-protocol pen tablet currently connected to
// the system -- same shape as every other platform's (pen_capture_stub.go),
// which disk_widget_pen.go's syncPenCaptures reads generically.
type PenTabletInfo struct {
	ID   string
	Name string
	VID  uint16
	PID  uint16
}

// ListPenTablets lists every currently-registered HID device that declares
// a top-level Digitizer-page collection.
func ListPenTablets() []PenTabletInfo {
	hidDevicesMu.Lock()
	defer hidDevicesMu.Unlock()
	out := make([]PenTabletInfo, 0, len(hidDevices))
	for id, dev := range hidDevices {
		if !hidHasAnyTopLevelPage(dev, hidUsagePageDigitizer) {
			continue
		}
		name := dev.Get("productName").String()
		if name == "" {
			name = "HID Tablet"
		}
		out = append(out, PenTabletInfo{
			ID:   id,
			Name: name,
			VID:  uint16(dev.Get("vendorId").Int()),
			PID:  uint16(dev.Get("productId").Int()),
		})
	}
	return out
}

// hidHasAnyTopLevelPage reports whether device declares a top-level
// collection on the given usage page, regardless of usage -- a digitizer's
// exact top-level usage varies by device (Pen 0x02, Touch Screen 0x04,
// Touch Pad 0x05, ...) more than a gamepad's does, so unlike
// hidHasTopLevelUsage this only checks the page.
func hidHasAnyTopLevelPage(device js.Value, page int) bool {
	collections := device.Get("collections")
	if collections.IsUndefined() {
		return false
	}
	n := collections.Length()
	for i := 0; i < n; i++ {
		if collections.Index(i).Get("usagePage").Int() == page {
			return true
		}
	}
	return false
}

// BrowserPenCapture is an active WebHID oninputreport capture for a pen
// tablet, forwarding raw reports rather than a decoded state (see this
// file's doc comment).
type BrowserPenCapture struct {
	device   js.Value
	listener js.Func
}

// StartBrowserPenCapture forwards id's (see ListPenTablets) raw input
// reports to onReport, each prefixed with its report id byte to match the
// "wire report" shape usbpasscore.WacomSession.PushReport expects.
func StartBrowserPenCapture(id string, onReport func([]byte)) (*BrowserPenCapture, error) {
	hidDevicesMu.Lock()
	device, ok := hidDevices[id]
	hidDevicesMu.Unlock()
	if !ok {
		return nil, fmt.Errorf("unknown HID tablet id %q", id)
	}

	c := &BrowserPenCapture{device: device}
	c.listener = js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		event := args[0]
		reportID := byte(event.Get("reportId").Int())
		dataView := event.Get("data")
		length := dataView.Get("byteLength").Int()
		rep := make([]byte, length+1)
		rep[0] = reportID
		for i := 0; i < length; i++ {
			rep[i+1] = byte(dataView.Call("getUint8", i).Int())
		}
		onReport(rep)
		return nil
	})
	device.Set("oninputreport", c.listener)
	return c, nil
}

// Stop detaches the oninputreport listener. The HIDDevice itself is left
// open (and registered) so a later mount can resume capture without a fresh
// permission prompt.
func (c *BrowserPenCapture) Stop() {
	c.device.Set("oninputreport", js.Null())
	c.listener.Release()
}
