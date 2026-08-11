//go:build js && wasm

package api

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
func NewDirectUSBClient(host string, port int, timeout int) *USBClient {
	return NewUSBClient(host, port, timeout)
}
