//go:build js && wasm

package controller

import (
	"syscall/js"
	"time"

	"usbridge-client/internal/gui/view"

	"fyne.io/fyne/v2"
)

// newDashboardHIDConnectButton builds the HID & Input Hub card's "Connect
// USB" header action -- see disk_widget.go's dashboardHIDConnectBtn doc
// comment for why Tapped is a no-op here (a real DOM button overlaid on top
// of this widget, see startHIDConnectOverlaySync below, is what actually
// triggers navigator.hid.requestDevice()).
func (dw *DiskWidget) newDashboardHIDConnectButton() *view.DeviceDashboardHeaderButton {
	btn := view.NewDeviceDashboardHeaderButton("Connect USB", nil, view.DeviceDashboardAccentTeal, func() {})
	btn.OnHover = dw.dashboardHIDHover
	return btn
}

// hidOverlaySyncInterval matches browserGamepadPollInterval's own cadence --
// frequent enough that the overlay button visibly tracks scrolling/resizing
// the Devices tab, cheap enough (one AbsolutePositionForObject call plus one
// js.Value.Call) to just poll rather than hook into Fyne's own resize
// events, which nothing else in this codebase's wasm build does either
// (see video_widget_dom_overlay_wasm.go's own polling-based syncVideoOverlay
// for the established precedent).
const hidOverlaySyncInterval = 250 * time.Millisecond

// startHIDConnectOverlaySync keeps window.usbridgeSetHIDButtonRect (see
// index.html) fed with dashboardHIDConnectBtn's current absolute on-screen
// rect, so the real DOM button index.html positions there tracks it live --
// through scrolling, window resizes, and the Devices tab not even being the
// one currently shown (handled by hiding the overlay entirely, see below).
func (dw *DiskWidget) startHIDConnectOverlaySync() {
	if dw.dashboardHIDConnectBtn == nil {
		return
	}
	go func() {
		ticker := time.NewTicker(hidOverlaySyncInterval)
		defer ticker.Stop()
		for {
			select {
			case <-dw.refreshStop:
				setHIDButtonVisible(false)
				return
			case <-ticker.C:
				if dw.isClosing.Load() {
					continue
				}
				syncHIDConnectOverlay(dw)
			}
		}
	}()
}

func syncHIDConnectOverlay(dw *DiskWidget) {
	btn := dw.dashboardHIDConnectBtn
	// Visible() reflects both the card's own Hide()/Show() state and every
	// ancestor's (a Fyne CanvasObject reports itself hidden if a parent
	// container is), so this alone also covers "Devices tab isn't the one
	// currently shown" so long as that hides the dashboard container --
	// which the app's own tab-switching already does.
	if btn == nil || !btn.Visible() {
		setHIDButtonVisible(false)
		return
	}
	abs := fyne.CurrentApp().Driver().AbsolutePositionForObject(btn)
	size := btn.Size()
	js.Global().Call("usbridgeSetHIDButtonRect",
		float64(abs.X), float64(abs.Y), float64(size.Width), float64(size.Height))
}

func setHIDButtonVisible(visible bool) {
	fn := js.Global().Get("usbridgeSetHIDButtonVisible")
	if fn.Truthy() {
		fn.Invoke(visible)
	}
}
