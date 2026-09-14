//go:build darwin

package usbpass

import (
	"fmt"

	"usbridge-client/internal/models"
)

// listHIDDarwin enumerates every HID device (see ListAllHIDDevices in
// hidbridge_darwin.go for the built-in/no-VID/non-USB exclusions) as a
// passthrough candidate -- generalized from the original Wacom-only
// listing, which reused platform.ListPenTablets (a hardcoded Wacom-vendor-ID
// match; that function still exists and is still used by the separate
// semantic CGEvent pen-capture path in disk_widget_pen.go, just no longer by
// this list). Any device hidbridge_darwin.go's TryClaimGousb can later open
// and read a HID report descriptor from -- gamepad, drawing tablet, external
// keyboard/mouse, etc. -- shows up here, matching how the Windows/Linux
// raw-USB device list already has no class restriction.
//
// InstanceID embeds d.ID (the IOKit registry entry ID) as a trailing path
// segment -- live-verified necessary: a single Logitech Unifying receiver
// enumerates as four separate HID devices at the same VID:PID (one
// interface each for its keyboard/mouse/consumer-control/vendor HID++
// functions), which a bare "HID\VID_xxxx&PID_yyyy" InstanceID can't tell
// apart and TryClaimGousb's VID:PID-only claim would then always grab
// whichever one IOHIDManager happens to enumerate first. hidUsageLabel adds
// the same disambiguation to Description so the device list is not four
// identical-looking "USB Receiver" rows either. BusID is synthesized from
// the (now-unique-per-interface) InstanceID via StableUSBIPBusID since
// macOS has no Linux-style busid.
func listHIDDarwin() ([]models.USBPassthroughDevice, error) {
	var out []models.USBPassthroughDevice
	for _, d := range ListAllHIDDevices() {
		instanceID := fmt.Sprintf("HID\\VID_%04X&PID_%04X\\%d", d.VID, d.PID, d.ID)
		desc := d.Name
		if label := hidUsageLabel(d.UsagePage, d.Usage); label != "" {
			desc = fmt.Sprintf("%s (%s)", d.Name, label)
		}
		out = append(out, models.USBPassthroughDevice{
			BusID:       StableUSBIPBusID(instanceID),
			InstanceID:  instanceID,
			VID:         fmt.Sprintf("%04x", d.VID),
			PID:         fmt.Sprintf("%04x", d.PID),
			Description: desc,
		})
	}
	return out, nil
}

// hidUsageLabel gives a human name to the usage pages this project expects
// to actually see on a passthrough candidate. Returns "" for anything else
// rather than guessing -- Description then just falls back to the device's
// own product string.
func hidUsageLabel(usagePage, usage uint16) string {
	switch usagePage {
	case 0x01: // Generic Desktop
		switch usage {
		case 0x02:
			return "Mouse"
		case 0x04:
			return "Joystick"
		case 0x05:
			return "Gamepad"
		case 0x06:
			return "Keyboard"
		case 0x08:
			return "Multi-axis Controller"
		}
	case 0x0C:
		return "Consumer Control"
	case 0x0D:
		return "Digitizer"
	}
	return ""
}
