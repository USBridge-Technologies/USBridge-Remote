//go:build !(js && wasm)

package platform

// PageServedOverUSB is the browser's (usb_page_wasm.go): never on the apps.
func PageServedOverUSB() bool { return false }
