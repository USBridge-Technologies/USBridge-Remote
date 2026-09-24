package ui

import (
	"net/url"
	"runtime"

	"usbridge_agent/internal/usbpass"
)

// usbipWin2ReleasesURL is where the Windows USB Passthrough "Download"
// button sends the user: usbip-win2 ships its own signed installer.
const usbipWin2ReleasesURL = "https://github.com/vadimgrn/usbip-win2/releases/latest"

// usbPermGranted is the USB Passthrough chip's tick: on Linux the one-time
// polkit attach grant, on Windows whether usbip-win2's drivers are
// installed (read directly, so it's right even before the broker consent
// that Status().VhciDriver depends on).
func usbPermGranted(goos string, usb usbpass.Status, usbipInstalled func() bool) bool {
	if goos == "windows" {
		return usbipInstalled()
	}
	return usb.AttachGranted
}

// showUSBDriverRow decides the separate "USB Passthrough Driver" install
// row. Never on Windows, where the USB Passthrough chip's own "Download"
// button covers it.
func showUSBDriverRow(goos string, usb usbpass.Status) bool {
	if goos == "windows" {
		return false
	}
	return usb.ConsentGiven && usb.Available && !usb.VhciDriver
}

// permRequestLabel is the not-granted button text for the driver-backed
// chips: "Download" on Windows, the default "Grant" ("") elsewhere.
func permRequestLabel(goos, download string) string {
	if goos == "windows" {
		return download
	}
	return ""
}

// refreshPermRequestLabels (re)applies permRequestLabel -- at build time and
// on every language change.
func (w *Window) refreshPermRequestLabels() {
	label := permRequestLabel(runtime.GOOS, loc().PermDownload)
	w.usbAccessCheck.SetRequestLabel(label)
	w.vdisplayAccessCheck.SetRequestLabel(label)
}

func (w *Window) openUSBIPDriverDownload() {
	if parsed, err := url.Parse(usbipWin2ReleasesURL); err == nil {
		_ = w.app.OpenURL(parsed)
	}
}
