//go:build !(js && wasm)

package controller

// tailscaleRegisterUISupported reports whether the "register bridge in
// Tailscale" checkbox should be offered in connection dialogs — see
// tailscale_register_ui_wasm.go's counterpart for why it isn't in the
// browser build.
func tailscaleRegisterUISupported() bool { return true }
