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
// on the agent's own machine, bound to 127.0.0.1, with the local
// usbridge-usb-broker dialing it directly. rust-shine's
// bin/usb-broker/src/main.rs::resolve_dial_target confirms this dial is
// unencrypted/direct for a loopback ExportHost with no tsnet bridge
// configured (which is how agent/internal/usbpass/service.go starts the
// broker today) -- so no AEAD tunnel (usbtunnel.go) is needed here at all,
// only the plain Server from usbpasscore.
package browserusb

import (
	"fmt"
	"net"
	"sync"

	"usbridge-client/pkg/usbpasscore"
)

// GamepadSession owns one browser-sourced synthetic Xbox 360 controller's
// loopback USB/IP export.
type GamepadSession struct {
	busID   string
	server  *usbpasscore.Server
	backend *usbpasscore.X360Backend
	addr    string

	mu     sync.Mutex
	closed bool
}

// NewGamepadSession allocates a free loopback port, starts a USB/IP export
// on it presenting a synthetic Xbox 360 controller (busID identifies the
// device the same way a real passthrough attach would), and returns the
// session. Call SetState as browser Gamepad API samples arrive, and Close
// when the browser disconnects or the WebSocket relay drops.
func NewGamepadSession(busID, serial string) (*GamepadSession, error) {
	addr, err := freeLoopbackAddr()
	if err != nil {
		return nil, fmt.Errorf("browserusb: allocate loopback port: %w", err)
	}

	backend := usbpasscore.NewX360Backend(serial, nil)
	dev := backend.ExportedDevice(busID)

	server, err := usbpasscore.StartExport(addr, []*usbpasscore.ExportedDevice{dev})
	if err != nil {
		return nil, fmt.Errorf("browserusb: start export on %s: %w", addr, err)
	}

	return &GamepadSession{busID: busID, server: server, backend: backend, addr: addr}, nil
}

// Addr is the loopback host:port the local usbridge-usb-broker should dial
// (ExportHost="127.0.0.1", ExportService=the port half of this address).
func (s *GamepadSession) Addr() string { return s.addr }

// BusID is the USB/IP bus id this session was created for.
func (s *GamepadSession) BusID() string { return s.busID }

// SetState publishes a new gamepad sample, converted from whatever the
// browser's Gamepad API reported (see the wasm-side gamepad_capture_wasm.go)
// into X360Backend's XInput-shaped state.
func (s *GamepadSession) SetState(state usbpasscore.X360State) {
	s.backend.SetState(state)
}

// Close stops the export and releases the loopback port.
func (s *GamepadSession) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	s.mu.Unlock()
	s.server.Stop()
}

// freeLoopbackAddr asks the OS for an ephemeral loopback port by briefly
// binding to port 0 and reading back what it chose, then releasing it --
// StartExport does its own net.Listen right after, so there's an
// unavoidable (tiny) TOCTOU window between the two binds, same as any other
// "reserve a port" pattern.
func freeLoopbackAddr() (string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return addr, nil
}
