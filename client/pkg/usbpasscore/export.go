// Package usbpasscore re-exports the platform-independent half of
// client/internal/usbpass -- the USB/IP wire-protocol exporter (Server/
// DeviceBackend/ExportedDevice) and the synthetic Xbox 360 controller
// backend (X360Backend) -- as a public, cross-module-importable path.
//
// Why this exists: usbridge-client and usbridge_agent are separate Go
// modules with separate go.mod files, and Go's internal/ visibility rule
// means agent/ code can never import usbridge-client/internal/usbpass
// directly, no matter how the require/replace directives are wired up. The
// agent needs exactly these types to host a loopback USB/IP export fed by
// browser-captured gamepad state (see agent/internal/browserusb) -- so
// rather than physically relocating server.go/x360_backend.go (and updating
// every one of the ~30 files in client/internal/usbpass that reference
// them), this package is a thin alias layer: the real implementation stays
// exactly where it is, still exercised by the native libusb/HID passthrough
// path, and this package is the one new public seam that lets the agent
// reuse the identical Server/DeviceBackend/X360Backend code instead of a
// second copy.
package usbpasscore

import "usbridge-client/internal/usbpass"

type (
	// Server is an in-process USB/IP v1.1.1 export listener.
	Server = usbpass.Server
	// DeviceBackend answers URBs for an imported device.
	DeviceBackend = usbpass.DeviceBackend
	// ExportedDevice is one device advertised on the USB/IP wire.
	ExportedDevice = usbpass.ExportedDevice
	// X360Backend serves a synthetic Xbox 360 controller's USB traffic.
	X360Backend = usbpass.X360Backend
	// X360State is one snapshot of a gamepad's state in XInput terms.
	X360State = usbpass.X360State
	// X360Command is something the importer's driver wrote to the OUT endpoint.
	X360Command = usbpass.X360Command
)

// StartExport binds addr (e.g. "127.0.0.1:<port>") and serves devices.
func StartExport(addr string, devices []*ExportedDevice) (*Server, error) {
	return usbpass.StartExport(addr, devices)
}

// NewX360Backend returns a controller that starts neutral. serial is the USB
// serial string; onCommand receives rumble/LED commands from the importer
// and may be nil.
func NewX360Backend(serial string, onCommand func(X360Command)) *X360Backend {
	return usbpass.NewX360Backend(serial, onCommand)
}
