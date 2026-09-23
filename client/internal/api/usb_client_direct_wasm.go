//go:build js && wasm

package api

import "syscall/js"

// NewDirectUSBClient on wasm is just NewUSBClient: the physical-interface
// pinning the native implementation does (see usb_client_direct_default.go)
// exists to route around VPN/Tailscale interception on a real OS network
// stack, which a browser tab has no access to at all -- there is no
// meaning to "bind to a LAN source IP" from inside a sandboxed fetch()
// call, and Tailscale itself isn't reachable from the web client either
// way (see the implementation plan: browser access to a tailnet goes
// through the OS's own Tailscale client, not this app).
//
// More importantly, setting Transport.DialContext at all -- even to a
// dialer that would work correctly natively -- changes which code path Go's
// net/http takes under GOOS=js: net/http/roundtrip_js.go only uses the
// Fetch API when Transport.Dial/DialContext/DialTLS/DialTLSContext are all
// nil; if any is set, it falls back to a real net.Dial, which wasm has no
// working implementation of and which always fails with a misleadingly
// specific-looking "dial tcp ...: connect: Connection refused" -- even
// though the exact same request succeeds instantly via a plain fetch() to
// the same address. Confirmed live: this was the actual root cause of the
// web client showing "Connection refused" against a server that answered
// curl/fetch() from the same device without any trouble.
//
// tlsPort selects the agent's HTTPS listener (see USBridge-Remote/agent's
// internal/tlshost, internal/devicecert) when this page itself was loaded
// over https: browsers block a fetch()/WebSocket from an https:// page to
// a plain-http origin outright (mixed content), and there's no way for a
// background request to click through an untrusted-cert warning the way
// top-level navigation can -- so self-signed https:// wouldn't help either
// unless host happens to be the agent's real, browser-trusted
// <label>.device.usbridge.io hostname (only the wildcard cert covers
// that). When this page is plain http (e.g. served directly by an agent on
// the LAN, or a local dev build), host:port behaves exactly as before.
func NewDirectUSBClient(host string, port, tlsPort int, timeout int) *USBClient {
	if BrowserIsHTTPS() {
		return NewUSBClientWithScheme("https", host, tlsPort, timeout, nil)
	}
	return NewUSBClient(host, port, timeout)
}

// BrowserIsHTTPS reports whether this wasm module's own page was loaded
// over https -- window.location.protocol, the only reliable way to detect
// this from inside the module itself. Exported (not just used by
// NewDirectUSBClient above) so other packages with their own direct
// http://<agent>/... calls -- e.g. service.MoonlightService's pairing PIN
// submission -- can make the same http-vs-https decision without each
// reimplementing this check; see usb_client_direct_default.go's
// counterpart (always false -- no browser sandbox on desktop-native
// builds) for why this symbol exists unconditionally on every platform.
func BrowserIsHTTPS() bool {
	loc := js.Global().Get("location")
	if loc.IsUndefined() || loc.IsNull() {
		return false
	}
	return loc.Get("protocol").String() == "https:"
}
