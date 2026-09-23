//go:build !(js && wasm)

package api

import (
	"crypto/tls"
	"net/http"
	"time"
)

// NewDirectUSBClient creates a USB client whose TCP sockets bypass VPN/Tailscale
// routing. On macOS it uses IP_BOUND_IF to pin the socket to the physical LAN
// interface; on other platforms it binds to the LAN source IP. Both approaches
// prevent EHOSTUNREACH that can occur when Tailscale's NEPacketTunnelProvider is
// in a transitional state (NeedsLogin, restart) and intercepts bundle-launched
// app traffic.
// tlsPort is accepted only so this matches the wasm build's signature
// (see usb_client_direct_wasm.go) -- unused here, the desktop-native
// client always talks plain http to the agent's local/LAN listener, no
// browser sandbox to trip mixed-content blocking on this path.
func NewDirectUSBClient(host string, port, tlsPort int, timeout int) *USBClient {
	t := time.Duration(timeout) * time.Second
	dialer := buildDirectDialer(host)
	dialer.Timeout = t
	transport := &http.Transport{
		DialContext:         dialer.DialContext,
		TLSHandshakeTimeout: 10 * time.Second,
		Proxy:               http.ProxyURL(nil), // Explicitly disable proxy — nil field uses ProxyFromEnvironment which calls WinHTTP COM on Windows and crashes
		TLSNextProto:        make(map[string]func(authority string, c *tls.Conn) http.RoundTripper),
	}
	client := &http.Client{Timeout: t, Transport: transport}
	return NewUSBClientWithHTTPClient(host, port, timeout, client)
}

// BrowserIsHTTPS is always false on desktop-native builds -- see the wasm
// build's counterpart (usb_client_direct_wasm.go) for why this symbol
// exists unconditionally on every platform.
func BrowserIsHTTPS() bool { return false }
