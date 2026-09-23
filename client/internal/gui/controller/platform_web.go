//go:build js && wasm

package controller

// isWebBuild gates the handful of places scripts_tab_widget.go's shared
// (no build tag) code needs to build a different MCP card for the wasm
// build -- see newScriptsMCPCardWebBridge's doc comment (view package) for
// why the wasm build needs an entirely different MCP setup flow instead of
// just reusing MCPProxy's URL+Start/Stop card.
const isWebBuild = true
