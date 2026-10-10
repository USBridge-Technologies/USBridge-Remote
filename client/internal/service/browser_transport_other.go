//go:build !(js && wasm)

package service

// BrowserWebDataSupported, BrowserUsesWebData, SetBrowserUsesWebData: the
// browser build's transport pick (see browser_transport_wasm.go); native
// clients stream over Moonlight.
func BrowserWebDataSupported() bool { return false }
func BrowserUsesWebData() bool      { return false }
func SetBrowserUsesWebData(bool)    {}
