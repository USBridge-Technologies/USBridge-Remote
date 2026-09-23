package browserusb

import (
	"sync"

	"usbridge-client/pkg/usbpasscore"
)

// PenSession owns one browser-sourced synthetic Wacom tablet's loopback
// USB/IP export -- the WebHID counterpart of GamepadSession, see session.go
// for the shared reasoning (AEAD tunnel requirement, ExportHost choice).
// Unlike a gamepad, a Wacom tablet is not synthetic end-to-end: its report
// descriptor and feature reports come from the real model (captured or
// database, see client/internal/usbpass/wacom_model.go), identified only by
// the vid/pid/productName the browser's WebHID grant reports -- only the
// live input reports (finger/pen contact) actually come from the browser.
type PenSession struct {
	busID   string
	export  *tunneledExport
	backend *usbpasscore.WacomSession

	mu     sync.Mutex
	closed bool
}

// NewPenSession resolves vid:pid to a known Wacom model (see
// usbpasscore.NewWacomExportedDevice) and starts its USB/IP export behind
// an AEAD tunnel listener. Returns an error for a tablet with no known
// model -- there is no generic fallback the way there is for a gamepad
// (there's no "synthetic Wacom" descriptor to fall back to; a wrong one
// would just make the host's real Wacom driver refuse the device).
func NewPenSession(busID string, vid, pid uint16, productName string, secret []byte) (*PenSession, error) {
	dev, backend, err := usbpasscore.NewWacomExportedDevice(busID, vid, pid, productName)
	if err != nil {
		return nil, err
	}

	export, err := startTunneledExport(busID, dev, secret)
	if err != nil {
		return nil, err
	}

	return &PenSession{busID: busID, export: export, backend: backend}, nil
}

// ExportHost/ExportPort/Nonce are what the browser's own Attach payload
// should carry as ExportHost/ExportService/TunnelNonce.
func (s *PenSession) ExportHost() string { return s.export.exportHost }
func (s *PenSession) ExportPort() string { return s.export.exportPort }
func (s *PenSession) Nonce() []byte      { return s.export.nonce }

// BusID is the USB/IP bus id this session was created for.
func (s *PenSession) BusID() string { return s.busID }

// PushReport forwards one raw HID input report from the browser's
// device.oninputreport straight through to the real model's matching
// interface -- see usbpasscore.WacomSession.PushReport.
func (s *PenSession) PushReport(rep []byte) {
	s.backend.PushReport(rep)
}

// Close stops the tunnel listener and the export, releasing both ports.
func (s *PenSession) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	s.mu.Unlock()
	s.export.Close()
}
