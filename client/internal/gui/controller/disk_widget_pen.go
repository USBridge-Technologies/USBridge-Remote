package controller

import (
	"fmt"

	"usbridge-client/internal/gui/view"
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

// syncPenCaptures compares the set of currently mounted pen tablet drives
// (a row's toggle, same as syncGamepadCaptures/keyboard/mouse) against the
// set of active captures and starts/stops captures accordingly.
func (dw *DiskWidget) syncPenCaptures() {
	if dw.activePenCaptures == nil {
		dw.activePenCaptures = make(map[string]penCaptureHandle)
	}

	byID := make(map[string]platform.PenTabletInfo)
	for _, t := range platform.ListPenTablets() {
		byID[t.ID] = t
	}

	wanted := make(map[string]bool)
	for _, d := range dw.allDrives {
		if d.IsPenTablet && d.IsMounted && d.PenTabletID != "" {
			wanted[d.PenTabletID] = true
		}
	}

	for id, cap := range dw.activePenCaptures {
		if !wanted[id] {
			logrus.Infof("🖊️ [PEN] stopping capture for %s", id)
			cap.Stop()
			delete(dw.activePenCaptures, id)
		}
	}

	for id := range wanted {
		if _, ok := dw.activePenCaptures[id]; ok {
			continue
		}
		t, ok := byID[id]
		if !ok {
			continue // mounted but no longer connected -- next poll will unmount it
		}
		logrus.Infof("🖊️ [PEN] starting capture for %s (%s, vid=%04x pid=%04x)", t.ID, t.Name, t.VID, t.PID)
		cap, err := dw.startPenCaptureRecovered(t)
		if err != nil {
			logrus.Warnf("🖊️ [PEN] capture failed for %s: %v", t.ID, err)
			continue
		}
		dw.activePenCaptures[t.ID] = cap
	}
}

// newPenTabletToggle is IsPenTablet's own on/off switch -- unlike
// newDriveToggle (used by every other HID row, keyboard/mouse/gamepad/the
// real-hardware Wacom passthrough row), it does not go through
// toggleDriveMount/handleMount's agent RPC dispatch at all: those rows'
// IsMounted comes back from the agent's own device-status report
// (mountedDevices, see disk_widget_data.go's combineDrives), which has no
// equivalent for a tablet this client captures locally itself (macOS's
// IOKit tap, or a WebHID grant) -- there is nothing for the agent to
// report, since the agent never chose to start anything. Flipping
// IsMounted locally and re-running syncPenCaptures directly is the whole
// mount step for this source.
func (dw *DiskWidget) newPenTabletToggle(idx int, drive DriveItem, cardHover func(bool)) *view.DeviceToggle {
	t := view.NewDeviceToggle(drive.IsMounted, func(on bool) {
		if idx < 0 || idx >= len(dw.allDrives) {
			return
		}
		dw.allDrives[idx].IsMounted = on
		// syncPenCaptures's own attach (AttachBrowserPen -> an HTTP POST
		// plus two WebSocket dials) blocks on real network I/O. Confirmed
		// live: calling it synchronously from here -- Tapped() itself runs
		// on Fyne's own dispatch thread -- corrupts Fyne's threading model
		// the moment that I/O parks and later resumes the goroutine from a
		// JS Promise callback instead of Fyne's own loop ("*** Error in
		// Fyne call thread, this should have been called in fyne.Do ***",
		// followed by a "call to released function" panic and the capture
		// immediately stopping again). A plain background goroutine avoids
		// ever starting this chain on the Fyne thread at all, same as
		// every agent-mount path already does via handleMount's own
		// `go func() {...}()` wrapping (see disk_widget_mount.go).
		go dw.syncPenCaptures()
	})
	t.OnHover = cardHover
	t.SetEnabled(!dw.controlsLocked())
	return t
}

// startPenCaptureRecovered wraps startPenCapture (native OS capture, or a
// browser-sourced USB/IP attach on the web build -- see
// disk_widget_pen_start_wasm.go) so a panic anywhere in that one attempt --
// a malformed device shape, a JS interop edge case, anything -- surfaces as
// an ordinary error for this one tablet instead of taking the whole
// process down: on wasm specifically, an uncaught panic in any goroutine
// kills the entire Go runtime (confirmed live as "Go program has already
// exited" on every subsequent browser callback after one such panic), not
// just this attach attempt.
func (dw *DiskWidget) startPenCaptureRecovered(t platform.PenTabletInfo) (cap penCaptureHandle, err error) {
	defer func() {
		if r := recover(); r != nil {
			cap, err = nil, fmt.Errorf("panic: %v", r)
		}
	}()
	return dw.startPenCapture(t)
}

// stopAllPenCaptures stops every active pen capture; called on disconnect.
func (dw *DiskWidget) stopAllPenCaptures() {
	for id, cap := range dw.activePenCaptures {
		logrus.Infof("🖊️ [PEN] stopping capture (disconnect) for %s", id)
		cap.Stop()
		delete(dw.activePenCaptures, id)
	}
}
