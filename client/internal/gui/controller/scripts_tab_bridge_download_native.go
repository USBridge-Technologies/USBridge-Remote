//go:build !(js && wasm)

package controller

// openBridgeDownload -- see scripts_tab_bridge_download_wasm.go's doc
// comment. Unreachable on this platform: the desktop MCP card
// (isWebBuild == false) never wires up an OnDownloadBridge callback.
func openBridgeDownload() {}
