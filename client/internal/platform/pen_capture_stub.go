//go:build !darwin || ios

package platform

import "fmt"

// PenTabletInfo describes a Wacom-protocol pen tablet currently connected to
// the system. Pen tablet capture is macOS-only for now (see
// pen_capture_darwin.go) -- other platforms already have full raw USB
// passthrough (client/internal/usbpass) available for this use case.
type PenTabletInfo struct {
	ID   string
	Name string
	VID  uint16
	PID  uint16
}

// ListPenTablets is not supported on this platform.
func ListPenTablets() []PenTabletInfo { return nil }

// PenCaptureState mirrors pen_capture_darwin.go's decoded sample shape.
type PenCaptureState struct {
	X, Y         uint32
	Pressure     uint16
	TiltX, TiltY int8
	Rotation     int16
	InRange      bool
	TipSwitch    bool
	Button1      bool
	Button2      bool
	Eraser       bool
}

const (
	PenMaxX        = 15200
	PenMaxY        = 9500
	PenMaxPressure = 4095
)

// PenCapture is a no-op placeholder on this platform.
type PenCapture struct{}

// StartPenCapture is not supported on non-darwin platforms.
func StartPenCapture(_ string, _ func(PenCaptureState)) (*PenCapture, error) {
	return nil, fmt.Errorf("pen tablet capture is not supported on this platform")
}

// Stop is a no-op.
func (c *PenCapture) Stop() {}
