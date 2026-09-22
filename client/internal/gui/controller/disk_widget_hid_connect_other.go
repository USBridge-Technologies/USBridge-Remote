//go:build !(js && wasm)

package controller

import "usbridge-client/internal/gui/view"

// newDashboardHIDConnectButton is nil on every native platform -- native
// gamepad/pen capture uses the OS's own HID access (IOKit, evdev, WinMM),
// no explicit per-device grant needed. See disk_widget_hid_connect_wasm.go.
func (dw *DiskWidget) newDashboardHIDConnectButton() *view.DeviceDashboardHeaderButton {
	return nil
}

// startHIDConnectOverlaySync is a no-op on every native platform -- there is
// no dashboardHIDConnectBtn to track (see newDashboardHIDConnectButton).
func (dw *DiskWidget) startHIDConnectOverlaySync() {}
