//go:build js && wasm

package platform

// WebHID pen tablet capture: the wasm counterpart of pen_capture_darwin.go,
// for a Wacom tablet granted via the same "Connect USB" button
// gamepad_hid_wasm.go's requestDevice() call uses (see index.html) --
// distinguished from a gamepad grant by vendor id (WacomVendorID) rather
// than by HID usage page: confirmed live that a real Intuos S's top-level
// collection is not on the Digitizer page (0x0D) at all -- Wacom's actual
// descriptors are vendor-specific enough that wacom_model.go itself never
// tries to parse them generically either, it works from a captured/database
// model keyed by vendor:product id (see that file's own doc comment). Vendor
// id is also the only thing usbpasscore.NewWacomExportedDevice needs to find
// the right model agent-side, so filtering on it here keeps both sides
// using the same one signal.
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

	"github.com/sirupsen/logrus"
)

// PenTabletInfo describes a Wacom-protocol pen tablet currently connected to
// the system -- same shape as every other platform's (pen_capture_stub.go),
// which disk_widget_pen.go's syncPenCaptures reads generically.
type PenTabletInfo struct {
	ID   string
	Name string
	VID  uint16
	PID  uint16
}

// ListPenTablets lists every currently-registered HID device whose vendor
// id is Wacom's.
func ListPenTablets() []PenTabletInfo {
	hidDevicesMu.Lock()
	defer hidDevicesMu.Unlock()
	out := make([]PenTabletInfo, 0, len(hidDevices))
	for id, dev := range hidDevices {
		if dev.Get("vendorId").Int() != WacomVendorID {
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
	c.listener = js.FuncOf(func(this js.Value, args []js.Value) (result interface{}) {
		// A panic here (a JS exception surfacing as one, or a bad index) is
		// a per-report failure, not a reason to take the whole wasm program
		// down with it -- an uncaught panic in any goroutine, including one
		// a browser event drives, kills the entire Go runtime (confirmed
		// live: "Go program has already exited" on every subsequent
		// callback after one such panic). Recovering here just drops this
		// one report and keeps capturing.
		defer func() {
			if r := recover(); r != nil {
				logrus.Warnf("usbpass(wasm): pen oninputreport panic recovered: %v", r)
			}
		}()
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
