package usbpass

// RequiresProLicense mirrors rust-shine's crates/usb-passthrough/src/
// license_class.rs `classify` function byte for byte (same
// freeInterfaceClasses triples, same generic-HID-gamepad Usage Page/Usage
// exception, same "all interfaces must be free, unknown defaults to Pro"
// rule) -- kept in sync by hand, since this repo has no build-time link to
// that closed crate.
//
// DISPLAY ONLY. This is never an enforcement decision: the real gate lives
// entirely in rust-shine's bin/usb-broker, which classifies from its own
// live OP_REQ_DEVLIST probe against the real exporter, not from anything
// this open-source client reports about itself. A modified client could
// make this function return whatever it wants -- the actual attach would
// still be accepted or refused by rust-shine exactly the same either way.
// This only drives the dashboard's "Pro" badge (see
// controller/disk_widget_dashboard.go), so a wrong answer here misleads the
// user about what to expect, never what they can actually get.
//
// hidUsagePage/hidUsage are the device's top-level HID usage (0 if unknown)
// -- see models.USBPassthroughDevice.HIDUsagePage's doc comment for why
// this is needed at all: a generic HID interface (03/00/00) alone can't
// tell a gamepad from, say, a Wacom tablet.
func RequiresProLicense(interfaces [][3]uint8, hidUsagePage, hidUsage uint16) bool {
	if len(interfaces) == 0 {
		return true // unknown -> Pro, same default as license_class.rs
	}
	isGamepadUsage := hidUsagePage == 0x01 && (hidUsage == 0x04 || hidUsage == 0x05)
	for _, iface := range interfaces {
		if isFreeInterfaceClass(iface) {
			continue
		}
		if iface == genericHIDClass && isGamepadUsage {
			continue
		}
		return true
	}
	return false
}

func isFreeInterfaceClass(iface [3]uint8) bool {
	for _, free := range freeInterfaceClasses {
		if iface == free {
			return true
		}
	}
	return false
}

var freeInterfaceClasses = [][3]uint8{
	{0x03, 0x01, 0x01}, // HID boot keyboard
	{0x03, 0x01, 0x02}, // HID boot mouse
	{0xFF, 0x5D, 0x01}, // XInput (Xbox 360) gamepad
	{0xFF, 0x47, 0xD0}, // GIP (Xbox One/Series) gamepad
}

// genericHIDClass covers everything from a DirectInput gamepad to a Wacom
// tablet -- only free when hidUsagePage/hidUsage (Generic Desktop/Joystick
// or GamePad) actually says "gamepad", checked by RequiresProLicense's
// caller.
var genericHIDClass = [3]uint8{0x03, 0x00, 0x00}
