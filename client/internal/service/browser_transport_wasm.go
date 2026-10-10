//go:build js && wasm

package service

import (
	"syscall/js"

	"usbridge-client/internal/webrtcweb"
)

// transportPrefKey stores the browser transport the user picked in the
// header (see gui's transport toggle): "webtransport" (WebData) or
// "webrtc". Absent: WebData where the page can run it.
const transportPrefKey = "usbridge.transport"

// BrowserWebDataSupported: this page can stream over WebTransport +
// WebCodecs (Chromium in a secure context).
func BrowserWebDataSupported() bool { return webrtcweb.WebTransportSupported() }

// BrowserUsesWebData reports the transport the next connection uses: the
// user's pick, else WebData where supported. Never WebData where the page
// can't run it.
func BrowserUsesWebData() bool {
	if !BrowserWebDataSupported() {
		return false
	}
	return readTransportPref() != "webrtc"
}

// SetBrowserUsesWebData stores the user's pick for later connections.
func SetBrowserUsesWebData(webData bool) {
	v := "webrtc"
	if webData {
		v = "webtransport"
	}
	defer func() { _ = recover() }() // storage blocked: the pick lasts this page only
	if ls := js.Global().Get("localStorage"); ls.Truthy() {
		ls.Call("setItem", transportPrefKey, v)
	}
	transportPrefMemory = v
}

var transportPrefMemory string

func readTransportPref() string {
	if transportPrefMemory != "" {
		return transportPrefMemory
	}
	defer func() { _ = recover() }()
	ls := js.Global().Get("localStorage")
	if !ls.Truthy() {
		return ""
	}
	if v := ls.Call("getItem", transportPrefKey); v.Type() == js.TypeString {
		return v.String()
	}
	return ""
}
