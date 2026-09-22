//go:build js && wasm

package platform

// WebHID gamepad capture: the primary capture method for the browser-sourced
// USB/IP path in Chrome/Edge (Firefox and Safari have no WebHID support at
// all, so gamepad_capture_wasm.go's Gamepad API poller stays as the fallback
// there and for any device the user never grants HID access to).
//
// Why this exists instead of just fixing up the Gamepad API path: the W3C
// Gamepad API only reports a "standard" (XInput-shaped) button/axis layout
// for pads Chrome's own internal database recognizes -- for anything else
// (confirmed live with a Razer Raiju Tournament Edition) it hands back the
// device's raw, unmapped button/axis order instead, which is what produced
// the "mapping is wrong" report this file fixes. WebHID sidesteps Chrome's
// database entirely: it exposes the real USB HID report descriptor
// (collections/reportId/usage/logicalMinimum/logicalMaximum, byte for byte
// what the device itself declares) plus the real vendorId/productId, which
// is exactly what gamepad_sdlmap.go's SDL_GameControllerDB lookup needs --
// the same authoritative per-model mapping table this project's Windows and
// Linux native capture already uses (gamepad_capture_windows.go), just fed
// from a descriptor this code parses itself instead of from WinMM/evdev.
//
// requestDevice() itself is user-gesture-gated and reliably fails when
// called from inside a Go js.FuncOf callback (see client/web/index.html's
// installFullscreenOnFirstTap/installClipboardPasteBridge doc comments for
// other browser APIs that showed the exact same failure live) -- so the
// permission prompt is triggered by a plain DOM button in index.html, with
// zero Go involved in that one call; the resulting HIDDevice is handed to
// RegisterHIDDevice (an ordinary, non-gesture-gated call) once it's open.
// getDevices() (recovering devices already granted in an earlier visit)
// needs no gesture at all and is called at startup the same way.

import (
	"fmt"
	"strconv"
	"sync"
	"syscall/js"
)

// hidGamepadUsages are the two Generic Desktop usages a physical gamepad's
// top-level collection declares (0x05 Gamepad, 0x04 Joystick) -- used both
// as the requestDevice() filter (in the JS half, index.html) and here to
// recognize which of a granted device's collections actually describes one.
const (
	hidUsagePageGenericDesktop = 0x01
	hidUsageGamepad            = 0x05
	hidUsageJoystick           = 0x04
	hidUsagePageButton         = 0x09
	hidUsageHatswitch          = 0x39
	hidUsageX                  = 0x30
	hidUsageY                  = 0x31
	hidUsageZ                  = 0x32
	hidUsageRx                 = 0x33
	hidUsageRy                 = 0x34
	hidUsageRz                 = 0x35
)

// genericHIDMapping is used when the device's VID:PID has no
// SDL_GameControllerDB entry: the same fixed usage-order assumption
// gamepad_capture_darwin.go's kButtonMap/GenericDesktop switch already
// makes (Button page 1-11 -> A,B,X,Y,LB,RB,Back,Start,LS,RS,Guide; X/Y left
// stick, Z left trigger, Rx/Ry right stick, Rz right trigger), expressed as
// an sdlMapping so it can share sdlMapping.capture's flip/trigger/hat logic
// instead of duplicating it.
var genericHIDMapping = &sdlMapping{src: map[string]sdlSource{
	"a":             {kind: sdlButton, index: 0},
	"b":             {kind: sdlButton, index: 1},
	"x":             {kind: sdlButton, index: 2},
	"y":             {kind: sdlButton, index: 3},
	"leftshoulder":  {kind: sdlButton, index: 4},
	"rightshoulder": {kind: sdlButton, index: 5},
	"back":          {kind: sdlButton, index: 6},
	"start":         {kind: sdlButton, index: 7},
	"leftstick":     {kind: sdlButton, index: 8},
	"rightstick":    {kind: sdlButton, index: 9},
	"guide":         {kind: sdlButton, index: 10},
	"leftx":         {kind: sdlAxis, index: 0},
	"lefty":         {kind: sdlAxis, index: 1},
	"lefttrigger":   {kind: sdlAxis, index: 2},
	"rightx":        {kind: sdlAxis, index: 3},
	"righty":        {kind: sdlAxis, index: 4},
	"righttrigger":  {kind: sdlAxis, index: 5},
}}

// hidField is one decoded input-report field's location within a report,
// resolved once from device.collections[].inputReports[].items[] and reused
// for every subsequent oninputreport event.
type hidField struct {
	usagePage  int
	usage      int // for a ranged (array) item, usageMinimum + the field's position
	bitOffset  int
	bitSize    int
	logicalMin int
	logicalMax int
}

// hidReportLayout is one reportId's fields, resolved from the descriptor.
type hidReportLayout struct {
	reportID int
	fields   []hidField
}

var (
	hidDevicesMu sync.Mutex
	hidDevices   = map[string]js.Value{} // id -> opened HIDDevice
	hidNextID    int
)

// RegisterHIDDevice adopts an already-open HIDDevice (see this file's doc
// comment for why the open() call itself lives in plain JS) and returns the
// stable id EnumerateGamepads/StartHIDGamepadCapture will use for it. A
// device already registered (same vendor/product/name -- see hidDeviceKey)
// keeps its existing id instead of adding a duplicate row: the HID button
// can be clicked more than once, and RefreshGrantedHIDDevices may already
// have recovered the same device from an earlier visit before the button is
// ever touched.
func RegisterHIDDevice(device js.Value) string {
	hidDevicesMu.Lock()
	defer hidDevicesMu.Unlock()
	key := hidDeviceKey(device)
	for id, d := range hidDevices {
		if hidDeviceKey(d) == key {
			hidDevices[id] = device
			return id
		}
	}
	id := "hid:" + strconv.Itoa(hidNextID)
	hidNextID++
	hidDevices[id] = device
	return id
}

// RefreshGrantedHIDDevices repopulates the registry from
// navigator.hid.getDevices() -- devices granted in an earlier visit, which
// need no user gesture to re-enumerate (only requestDevice() does). Safe to
// call from a Go callback since, unlike requestDevice(), getDevices() is not
// gesture-gated. Devices already open()ing/open from a prior call are left
// alone; only newly-seen ones get a fresh id.
func RefreshGrantedHIDDevices(onReady func()) {
	hid := js.Global().Get("navigator").Get("hid")
	if hid.IsUndefined() {
		return
	}
	var then js.Func
	then = js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		defer then.Release()
		devices := args[0]
		hidDevicesMu.Lock()
		known := make(map[string]bool, len(hidDevices))
		for _, d := range hidDevices {
			known[hidDeviceKey(d)] = true
		}
		hidDevicesMu.Unlock()
		n := devices.Get("length").Int()
		remaining := n
		if remaining == 0 && onReady != nil {
			onReady()
			return nil
		}
		for i := 0; i < n; i++ {
			dev := devices.Index(i)
			if known[hidDeviceKey(dev)] {
				remaining--
				if remaining == 0 && onReady != nil {
					onReady()
				}
				continue
			}
			openAndRegister(dev, func() {
				remaining--
				if remaining == 0 && onReady != nil {
					onReady()
				}
			})
		}
		return nil
	})
	hid.Call("getDevices").Call("then", then)
}

// hidDeviceKey identifies a device across getDevices() calls (WebHID gives
// no stable string id of its own): vendorId+productId is not unique for two
// identical pads, but collides no worse than treating every getDevices()
// refresh as "all new" would, and is enough for the common one-pad case.
func hidDeviceKey(d js.Value) string {
	return fmt.Sprintf("%d:%d:%s", d.Get("vendorId").Int(), d.Get("productId").Int(), d.Get("productName").String())
}

func openAndRegister(dev js.Value, done func()) {
	if dev.Get("opened").Bool() {
		RegisterHIDDevice(dev)
		if done != nil {
			done()
		}
		return
	}
	var then, catch js.Func
	then = js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		defer then.Release()
		defer catch.Release()
		RegisterHIDDevice(dev)
		if done != nil {
			done()
		}
		return nil
	})
	catch = js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		defer then.Release()
		defer catch.Release()
		if done != nil {
			done()
		}
		return nil
	})
	dev.Call("open").Call("then", then).Call("catch", catch)
}

// EnumerateHIDGamepads lists every currently-registered HID gamepad in the
// same shape gamepad_enum_wasm.go's EnumerateGamepads uses for the Gamepad
// API, so disk_widget_data.go's loadGamepadDevices can merge both without
// caring which source a row came from.
func EnumerateHIDGamepads() []GamepadDevice {
	hidDevicesMu.Lock()
	defer hidDevicesMu.Unlock()
	out := make([]GamepadDevice, 0, len(hidDevices))
	for id, dev := range hidDevices {
		name := dev.Get("productName").String()
		if name == "" {
			name = "HID Gamepad"
		}
		out = append(out, GamepadDevice{
			ID:        id,
			Name:      name,
			VendorID:  fmt.Sprintf("0x%04X", dev.Get("vendorId").Int()),
			ProductID: fmt.Sprintf("0x%04X", dev.Get("productId").Int()),
		})
	}
	return out
}

// buildHIDLayout walks device.collections (WebHID's already-parsed report
// descriptor) and resolves the bit offset of every field of every
// inputReport, the one piece the descriptor's own JS objects don't give
// directly -- report items are declared in order and packed back-to-back,
// LSB first, so the offset has to be accumulated by walking them in
// declaration order per the USB HID spec, same as any other HID report
// consumer (evdev, IOKit, hidapi) does internally.
func buildHIDLayout(device js.Value) []hidReportLayout {
	var layouts []hidReportLayout
	collections := device.Get("collections")
	var walk func(js.Value)
	walk = func(col js.Value) {
		reports := col.Get("inputReports")
		if !reports.IsUndefined() {
			n := reports.Length()
			for i := 0; i < n; i++ {
				report := reports.Index(i)
				reportID := report.Get("reportId").Int()
				items := report.Get("items")
				bitOffset := 0
				var fields []hidField
				m := items.Length()
				for j := 0; j < m; j++ {
					item := items.Index(j)
					reportSize := item.Get("reportSize").Int()
					reportCount := item.Get("reportCount").Int()
					logicalMin := item.Get("logicalMinimum").Int()
					logicalMax := item.Get("logicalMaximum").Int()
					usages := item.Get("usages")
					isRange := item.Get("isRange").Bool()
					usageMin := 0
					if isRange {
						usageMin = item.Get("usageMinimum").Int()
					}
					for k := 0; k < reportCount; k++ {
						usage := 0
						page := 0
						if isRange {
							usage = usageMin + k
							// isRange items encode page+usage combined into
							// usageMinimum/usageMaximum on some engines and
							// split on others; the button page is what every
							// gamepad range item in practice uses, so a bare
							// low 16 bits is the usage id either way.
							page = hidUsagePageButton
						} else if k < usages.Length() {
							full := usages.Index(k).Int()
							page = full >> 16
							usage = full & 0xFFFF
							if page == 0 {
								// Some engines report a plain usage id here
								// (no page packed in); fall back to the
								// item's own usagePage field.
								page = item.Get("usagePage").Int()
								usage = full
							}
						}
						fields = append(fields, hidField{
							usagePage:  page,
							usage:      usage,
							bitOffset:  bitOffset,
							bitSize:    reportSize,
							logicalMin: logicalMin,
							logicalMax: logicalMax,
						})
						bitOffset += reportSize
					}
				}
				layouts = append(layouts, hidReportLayout{reportID: reportID, fields: fields})
			}
		}
		children := col.Get("children")
		if !children.IsUndefined() {
			cn := children.Length()
			for i := 0; i < cn; i++ {
				walk(children.Index(i))
			}
		}
	}
	if !collections.IsUndefined() {
		n := collections.Length()
		for i := 0; i < n; i++ {
			top := collections.Index(i)
			page := top.Get("usagePage").Int()
			usage := top.Get("usage").Int()
			if page != hidUsagePageGenericDesktop || (usage != hidUsageGamepad && usage != hidUsageJoystick) {
				continue
			}
			walk(top)
		}
	}
	return layouts
}

// readField extracts one field's raw unsigned integer from a report's raw
// bytes, LSB-first bit packing per the USB HID spec (the same convention
// every field in a HID report uses, buttons included).
func readField(data []byte, f hidField) int {
	var v uint32
	for i := 0; i < f.bitSize; i++ {
		bit := f.bitOffset + i
		byteIdx := bit / 8
		if byteIdx >= len(data) {
			break
		}
		bitIdx := uint(bit % 8)
		if data[byteIdx]&(1<<bitIdx) != 0 {
			v |= 1 << uint(i)
		}
	}
	return int(v)
}

// HIDGamepadCapture is an active WebHID oninputreport capture.
type HIDGamepadCapture struct {
	device   js.Value
	listener js.Func
}

// StartHIDGamepadCapture decodes id's (see RegisterHIDDevice) raw input
// reports into EncodeBrowserGamepadFrame-shaped frames, using
// gamepad_sdlmap.go's SDL_GameControllerDB mapping when the device's
// VID:PID is known, falling back to genericHIDMapping's fixed usage-order
// assumption otherwise. onFrame is called from the browser's own
// oninputreport event, not a polling loop -- HID reports only arrive on
// change already, unlike the Gamepad API's snapshot-based getGamepads().
func StartHIDGamepadCapture(id string, onFrame func([]byte)) (*HIDGamepadCapture, error) {
	hidDevicesMu.Lock()
	device, ok := hidDevices[id]
	hidDevicesMu.Unlock()
	if !ok {
		return nil, fmt.Errorf("unknown HID gamepad id %q", id)
	}

	vid := uint16(device.Get("vendorId").Int())
	pid := uint16(device.Get("productId").Int())
	mapping := sdlMappingFor(vid, pid)
	if mapping == nil {
		mapping = genericHIDMapping
	} else if isDS4Family(vid, pid) {
		mapping.withDS4Defaults()
	}

	layouts := buildHIDLayout(device)

	c := &HIDGamepadCapture{device: device}
	c.listener = js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		event := args[0]
		reportID := event.Get("reportId").Int()
		dataView := event.Get("data")
		length := dataView.Get("byteLength").Int()
		raw := make([]byte, length)
		for i := 0; i < length; i++ {
			raw[i] = byte(dataView.Call("getUint8", i).Int())
		}

		var layout *hidReportLayout
		for i := range layouts {
			if layouts[i].reportID == reportID {
				layout = &layouts[i]
				break
			}
		}
		if layout == nil && len(layouts) == 1 {
			layout = &layouts[0]
		}
		if layout == nil {
			return nil
		}

		var in joyInput
		in.pov = -1
		for _, f := range layout.fields {
			rawVal := readField(raw, f)
			switch {
			case f.usagePage == hidUsagePageButton && f.usage >= 1 && f.usage <= 32:
				if rawVal != 0 {
					in.buttons |= 1 << uint(f.usage-1)
				}
			case f.usagePage == hidUsagePageGenericDesktop && f.usage == hidUsageHatswitch:
				in.pov = hidHatToPov(rawVal, f.logicalMin, f.logicalMax)
			case f.usagePage == hidUsagePageGenericDesktop:
				if axisIdx, ok := hidAxisIndex(f.usage); ok {
					in.axes[axisIdx] = hidAxisUnit(rawVal, f.logicalMin, f.logicalMax)
				}
			}
		}

		state := mapping.capture(in)
		onFrame(EncodeBrowserGamepadFrame(state.Buttons, state.LeftTrigger, state.RightTrigger,
			state.LeftX, state.LeftY, state.RightX, state.RightY))
		return nil
	})
	device.Set("oninputreport", c.listener)
	return c, nil
}

// Stop detaches the oninputreport listener. The HIDDevice itself is left
// open (and registered) so a later mount can resume capture without a fresh
// permission prompt.
func (c *HIDGamepadCapture) Stop() {
	c.device.Set("oninputreport", js.Null())
	c.listener.Release()
}

// hidAxisIndex maps a Generic Desktop usage to gamepad_sdlmap.go's joyInput
// axis slot, in direct usage order (X,Y,Z,Rx,Ry,Rz) -- unlike
// gamepad_capture_windows.go's sdlAxisToJoy, no remap table is needed here:
// that table exists only to correct WinMM's own opaque axis numbering
// (see its doc comment), which this code never goes through -- these axes
// come straight from the descriptor's own usage, already unambiguous.
func hidAxisIndex(usage int) (int, bool) {
	switch usage {
	case hidUsageX:
		return 0, true
	case hidUsageY:
		return 1, true
	case hidUsageZ:
		return 2, true
	case hidUsageRx:
		return 3, true
	case hidUsageRy:
		return 4, true
	case hidUsageRz:
		return 5, true
	}
	return 0, false
}

// hidAxisUnit scales a raw axis value to -1..1 given the field's own
// logical range, the same normalization winmmUnit does for WinMM.
func hidAxisUnit(raw, lo, hi int) float64 {
	if hi <= lo {
		return 0
	}
	mid := float64(hi+lo) / 2
	half := float64(hi-lo) / 2
	return (float64(raw) - mid) / half
}

// hidHatToPov converts a HID hat switch's raw value (typically 0..7 for the
// eight directions, with some out-of-range value -- often logicalMax+1 --
// meaning centered) to joyInput.pov's convention (hundredths of a degree,
// -1 centered), matching gamepad_capture_windows.go's dwPOV.
func hidHatToPov(raw, lo, hi int) int {
	if raw < lo || raw > hi {
		return -1
	}
	steps := hi - lo + 1
	if steps <= 0 {
		return -1
	}
	return ((raw - lo) * 36000) / steps
}
