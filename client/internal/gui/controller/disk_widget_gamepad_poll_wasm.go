//go:build js && wasm

package controller

import "time"

// browserGamepadPollInterval is how often the "HID & Input Hub" dashboard
// re-scans navigator.getGamepads() on the web build. Needed because
// startPeriodicRefresh's own ticker deliberately excludes gamepad scanning
// on every platform (its doc comment: "Gamepad ... happen on explicit
// Refresh() calls only") -- fine for native OS enumeration, but the browser
// Gamepad API only starts reporting a pad non-null after the user presses
// one of its buttons (see gamepad_enum_wasm.go's doc comment), which can
// happen well after this widget was constructed and long before the
// operator thinks to hit a manual Refresh button. Polling here is what
// actually catches that transition and gets the pad to appear on its own,
// the same "just works" experience EnumerateGamepads already gives every
// other platform without polling at all.
const browserGamepadPollInterval = 1 * time.Second

// startBrowserGamepadPolling runs loadGamepadDevices on a short ticker for
// the lifetime of the widget (stopping on dw.refreshStop, same shutdown
// signal startPeriodicRefresh's own ticker goroutine already uses). Called
// once from NewDiskWidget; a no-op stub of the same name/signature exists
// for every other platform (disk_widget_gamepad_poll_other.go) so the call
// site itself needs no build tag.
func (dw *DiskWidget) startBrowserGamepadPolling() {
	go func() {
		ticker := time.NewTicker(browserGamepadPollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-dw.refreshStop:
				return
			case <-ticker.C:
				if dw.isClosing.Load() {
					continue
				}
				dw.loadGamepadDevices()
			}
		}
	}()
}
