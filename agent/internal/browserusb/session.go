// Package browserusb hosts a loopback USB/IP export, inside the agent's own
// process, for devices whose actual input comes from a browser tab (Gamepad
// API / WebHID) rather than from real local hardware on the agent machine.
//
// This mirrors, on the agent side, what client/internal/usbpass/session.go
// already does on the client side for a real physical device: build an
// ExportedDevice backed by a DeviceBackend, and hand it to
// usbpasscore.StartExport. The one thing that's different -- and the reason
// this package exists instead of just reusing session.go's Attach() flow
// unmodified -- is *where* the exporter binds: a browser tab can never
// accept an inbound TCP connection (no listen()/accept() in any web
// platform API), so for a browser-sourced device the exporter has to live
// on the agent's own machine instead of the client's.
//
// That in turn means the agent, not the client, has to be the AEAD tunnel
// endpoint: rust-shine's bin/usb-broker/src/main.rs (resolve_dial_target)
// only skips the data-plane tunnel entirely when the attach's ExportHost is
// a loopback string *and* the broker was started with no --tsnet-bridge --
// but a production agent normally *does* have one configured (for real
// Tailscale-remote passthrough), and once it is, "127.0.0.1" gets routed
// through spawn_agent_relay_via_bridge, which expects to reach a *remote*
// tsnet peer's exporter, not one living in this very process -- confirmed
// live: the browser's attach got as far as one decoded GET_DESCRIPTOR before
// the tunnel connection reset. Reporting an ExportHost the broker does not
// recognize as loopback (is_loopback_host only matches "127.0.0.1"/"::1"/
// "localhost" exactly) instead takes the plain spawn_agent_relay branch,
// which dials ExportHost:ExportService directly -- still AEAD-wrapped by
// that relay's own pump(), so a TunnelListener has to be on the receiving
// end here, keyed the same way a native Attach() would key one for itself
// (see usbpasscore.RegisterTunnelKey/DeriveSessionKey/DeriveTunnelKey,
// tunnel.go's startTunneledExport).
package browserusb

import (
	"sync"

	"usbridge-client/pkg/usbpasscore"
)

// GamepadSession owns one browser-sourced synthetic Xbox 360 controller's
// loopback USB/IP export.
type GamepadSession struct {
	busID   string
	export  *tunneledExport
	backend *usbpasscore.X360Backend

	mu     sync.Mutex
	closed bool
}

// NewGamepadSession starts a USB/IP export presenting a synthetic Xbox 360
// controller (busID identifies the device the same way a real passthrough
// attach would) behind an AEAD tunnel listener, and returns the session.
// secret is the agent's HMAC master key, needed to derive the same tunnel
// key rust-shine's broker will derive from the nonce this returns. Call
// SetState as browser Gamepad API/WebHID samples arrive, and Close when the
// browser disconnects or the WebSocket relay drops.
func NewGamepadSession(busID, serial string, secret []byte) (*GamepadSession, error) {
	backend := usbpasscore.NewX360Backend(serial, nil)
	dev := backend.ExportedDevice(busID)

	export, err := startTunneledExport(busID, dev, secret)
	if err != nil {
		return nil, err
	}

	return &GamepadSession{busID: busID, export: export, backend: backend}, nil
}

// ExportHost/ExportPort/Nonce are what the browser's own Attach payload
// should carry as ExportHost/ExportService/TunnelNonce.
func (s *GamepadSession) ExportHost() string { return s.export.exportHost }
func (s *GamepadSession) ExportPort() string { return s.export.exportPort }
func (s *GamepadSession) Nonce() []byte      { return s.export.nonce }

// BusID is the USB/IP bus id this session was created for.
func (s *GamepadSession) BusID() string { return s.busID }

// SetState publishes a new gamepad sample, converted from whatever the
// browser's Gamepad API/WebHID reported (see the wasm-side
// gamepad_capture_wasm.go/gamepad_hid_wasm.go) into X360Backend's
// XInput-shaped state.
func (s *GamepadSession) SetState(state usbpasscore.X360State) {
	s.backend.SetState(state)
}

// Close stops the tunnel listener and the export, releasing both ports.
func (s *GamepadSession) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	s.mu.Unlock()
	s.export.Close()
}
