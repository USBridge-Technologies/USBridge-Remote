//go:build js && wasm

package controller

import "syscall/js"

// openBridgeDownload opens bridge.cjs (see client/web/mcp-bridge/README.md)
// in a new tab so the browser's own download handling takes over --
// window.open resolves a relative URL against the current page's own
// origin natively, no origin-string plumbing needed here. Dropping to raw
// syscall/js rather than fyne's cross-platform OpenURL for the same reason
// web/index.html's #hidConnectBtn doc comment gives for its own browser
// API calls: this needs to definitely behave like a real user-initiated
// browser navigation, not go through a Go-side abstraction whose wasm
// backing isn't this file's to depend on.
func openBridgeDownload() {
	js.Global().Call("open", "/mcp-bridge/bin/bridge.cjs", "_blank")
}
