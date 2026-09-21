//go:build !(js && wasm)

package controller

// startBrowserGamepadPolling is a no-op on every platform except the web
// build (see disk_widget_gamepad_poll_wasm.go's doc comment for why wasm
// specifically needs it) -- native platforms' OS-level gamepad enumeration
// (gamepad_darwin.go/gamepad_linux.go/gamepad_windows.go) doesn't have the
// browser Gamepad API's "only visible after a button press" quirk, so
// startPeriodicRefresh's existing explicit-Refresh-only behavior for
// gamepad scanning is already correct there.
func (dw *DiskWidget) startBrowserGamepadPolling() {}
