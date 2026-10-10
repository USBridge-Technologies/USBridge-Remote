//go:build !(js && wasm)

package service

// BrowserWebTransportSupported, BrowserUsesWebTransport, SetBrowserUsesWebTransport: the
// browser build's transport pick (see browser_transport_wasm.go); native
// clients stream over Moonlight.
func BrowserWebTransportSupported() bool { return false }
func BrowserUsesWebTransport() bool      { return false }
func SetBrowserUsesWebTransport(bool)    {}
