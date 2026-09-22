package controller

import (
	"usbridge-client/internal/platform"

	"github.com/sirupsen/logrus"
)

// penCaptureHandle is whatever startPenCapture returned for one connected
// tablet -- a native OS-level capture (*platform.PenCapture) forwarding
// semantic Moonlight pen events on macOS, or a browser-sourced USB/IP
// synthetic tablet export on the web build (see
// disk_widget_pen_start_wasm.go). syncPenCaptures only ever calls Stop() on
// it, so the two can share dw.activePenCaptures without a build tag of
// their own -- same pattern as gamepadCaptureHandle.
type penCaptureHandle interface {
	Stop()
}

// syncPenCaptures compares currently connected Wacom-family pen tablets
// against the set of active captures and starts/stops captures accordingly.
// Unlike syncGamepadCaptures, there is no "mount" step here — a pen tablet
// forwards the instant it's plugged in (and, on the semantic-Moonlight
// platforms, once a Moonlight session is active), the same way built-in
// keyboard/mouse capture already works.
func (dw *DiskWidget) syncPenCaptures() {
	if dw.activePenCaptures == nil {
		dw.activePenCaptures = make(map[string]penCaptureHandle)
	}

	tablets := platform.ListPenTablets()
	wanted := make(map[string]bool, len(tablets))
	for _, t := range tablets {
		wanted[t.ID] = true
	}

	for id, cap := range dw.activePenCaptures {
		if !wanted[id] {
			logrus.Infof("🖊️ [PEN] stopping capture for %s", id)
			cap.Stop()
			delete(dw.activePenCaptures, id)
		}
	}

	for _, t := range tablets {
		if _, ok := dw.activePenCaptures[t.ID]; ok {
			continue
		}
		logrus.Infof("🖊️ [PEN] starting capture for %s (%s, vid=%04x pid=%04x)", t.ID, t.Name, t.VID, t.PID)
		cap, err := dw.startPenCapture(t)
		if err != nil {
			logrus.Warnf("🖊️ [PEN] capture failed for %s: %v", t.ID, err)
			continue
		}
		dw.activePenCaptures[t.ID] = cap
	}
}

// stopAllPenCaptures stops every active pen capture; called on disconnect.
func (dw *DiskWidget) stopAllPenCaptures() {
	for id, cap := range dw.activePenCaptures {
		logrus.Infof("🖊️ [PEN] stopping capture (disconnect) for %s", id)
		cap.Stop()
		delete(dw.activePenCaptures, id)
	}
}
