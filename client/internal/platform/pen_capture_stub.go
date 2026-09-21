//go:build !darwin || ios

package platform

import "fmt"

// PenTabletInfo describes a Wacom-protocol pen tablet currently connected to
// the system. Native OS-level pen tablet capture (opening the device and
// tapping its own input reports) is macOS-only for now (see
// pen_capture_darwin.go) -- other platforms already have full raw USB
// passthrough (client/internal/usbpass) available for this use case, and
// (via WebHID) the browser/wasm client can also decode the same reports
// this package's pen_protocol.go parses, regardless of host OS.
type PenTabletInfo struct {
	ID   string
	Name string
	VID  uint16
	PID  uint16
}

// ListPenTablets is not supported on this platform.
func ListPenTablets() []PenTabletInfo { return nil }

// PenCapture is a no-op placeholder on this platform.
type PenCapture struct{}

// StartPenCapture is not supported on non-darwin platforms.
func StartPenCapture(_ string, _ func(PenCaptureState)) (*PenCapture, error) {
	return nil, fmt.Errorf("pen tablet capture is not supported on this platform")
}

// Stop is a no-op.
func (c *PenCapture) Stop() {}
