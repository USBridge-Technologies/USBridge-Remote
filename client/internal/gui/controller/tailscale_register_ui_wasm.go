//go:build js && wasm

package controller

// tailscaleRegisterUISupported reports whether the "register bridge in
// Tailscale" checkbox should be offered in connection dialogs. It should
// not be in the browser build: registering the bridge into a tailnet only
// pays off if the client can then actually reconnect over that tailnet, and
// wasm has no separate Tailscale transport to reconnect over (see
// gui.dialTailscaleTarget's wasm variant) — a Tailscale address there is
// just dialed like any other reachable hostname. Offering the checkbox
// would only cost the user an unexplained back-and-forth LAN→"Tailscale"
// reconnect for no benefit.
func tailscaleRegisterUISupported() bool { return false }
