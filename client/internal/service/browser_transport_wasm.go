//go:build js && wasm

package service

import (
	"syscall/js"

	"usbridge-client/internal/webrtcweb"
)

// transportPrefKey stores the browser transport the user picked in the
// header (see gui's transport toggle): "webtransport" or
// "webrtc". Absent: WebTransport where the page can run it.
const transportPrefKey = "usbridge.transport"

// BrowserWebTransportSupported: this page can stream over WebTransport +
// WebCodecs (Chromium in a secure context).
func BrowserWebTransportSupported() bool { return webrtcweb.WebTransportSupported() }

// BrowserUsesWebTransport reports the transport the next connection uses: the
// user's pick, else WebTransport where supported. Never WebTransport where the page
// can't run it.
func BrowserUsesWebTransport() bool {
	if !BrowserWebTransportSupported() {
		return false
	}
	return readTransportPref() != "webrtc"
}

// SetBrowserUsesWebTransport stores the user's pick for later connections.
func SetBrowserUsesWebTransport(webTransport bool) {
	v := "webrtc"
	if webTransport {
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

// BrowserTransportPicked reports whether the user chose a transport in the
// header. Without a pick WebTransport is only the first try: a connect that
// fails falls back to WebRTC.
func BrowserTransportPicked() bool { return readTransportPref() != "" }
