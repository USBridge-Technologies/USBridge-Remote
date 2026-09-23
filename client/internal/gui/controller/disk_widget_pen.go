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

// pendingPenCapture is a placeholder dw.activePenCaptures holds for a
// tablet whose attach is still in flight on a background goroutine (see
// syncPenCaptures) -- its only job is to make that tablet id look "already
// handled" to a second, concurrent syncPenCaptures call (the periodic
// poll's own combineDrives can run at any time, including while a toggle's
// own attach is still connecting), so the id never gets attached twice.
// Stop is a no-op: there is nothing to tear down yet, the real handle
// replaces this the moment the attach finishes (see the goroutine below).
type pendingPenCapture struct{}

func (pendingPenCapture) Stop() {}

// syncPenCaptures compares the set of currently mounted pen tablet drives
// (a row's toggle, same as syncGamepadCaptures/keyboard/mouse) against the
// set of active captures and starts/stops captures accordingly. Must be
// called from the Fyne UI goroutine (dw.allDrives/dw.activePenCaptures
// access has no lock of its own) -- the actual attach, which blocks on real
// network I/O (AttachBrowserPen's HTTP POST plus two WebSocket dials),
// always happens on its own background goroutine instead (see below),
// never here: confirmed live that running it synchronously from a
// Tapped() handler corrupts Fyne's threading model the moment that I/O
// parks and later resumes off Fyne's own event loop.
func (dw *DiskWidget) syncPenCaptures() {
	if dw.activePenCaptures == nil {
		dw.activePenCaptures = make(map[string]penCaptureHandle)
	}

	byID := make(map[string]platform.PenTabletInfo)
	for _, t := range platform.ListPenTablets() {
		byID[t.ID] = t
	}

	wanted := make(map[string]bool)
	penRowsSeen := 0
	for _, d := range dw.allDrives {
		if d.IsPenTablet {
			penRowsSeen++
			logrus.Infof("🖊️ [PEN] sync: row id=%q IsMounted=%v", d.PenTabletID, d.IsMounted)
		}
		if d.IsPenTablet && d.IsMounted && d.PenTabletID != "" {
			wanted[d.PenTabletID] = true
		}
	}
	logrus.Infof("🖊️ [PEN] sync: %d pen row(s) in allDrives, %d wanted, %d active", penRowsSeen, len(wanted), len(dw.activePenCaptures))

	for id, cap := range dw.activePenCaptures {
		if !wanted[id] {
			logrus.Infof("🖊️ [PEN] stopping capture for %s", id)
			cap.Stop()
			delete(dw.activePenCaptures, id)
		}
	}

	for id := range wanted {
		if _, ok := dw.activePenCaptures[id]; ok {
			continue // already active, or already connecting (pendingPenCapture)
		}
		t, ok := byID[id]
		if !ok {
			continue // mounted but no longer connected -- next poll will unmount it
		}
		logrus.Infof("🖊️ [PEN] starting capture for %s (%s, vid=%04x pid=%04x)", t.ID, t.Name, t.VID, t.PID)
		dw.activePenCaptures[t.ID] = pendingPenCapture{}
		go dw.attachPenCapture(t)
	}
}

// attachPenCapture runs one tablet's attach (startPenCaptureRecovered,
// which blocks on real network I/O) off the Fyne thread and writes the
// result back through updateUIAsync/fyne.Do, the only place that touches
// dw.activePenCaptures again. If the tablet was toggled off (or its
// pendingPenCapture placeholder is gone for any other reason) by the time
// the attach finishes, the freshly-opened capture is stopped immediately
// instead of being adopted -- otherwise it would leak, tracked by nothing.
func (dw *DiskWidget) attachPenCapture(t platform.PenTabletInfo) {
	cap, err := dw.startPenCaptureRecovered(t)
	dw.updateUIAsync(func() {
		cur, stillPending := dw.activePenCaptures[t.ID]
		if !stillPending {
			if err == nil && cap != nil {
				cap.Stop()
			}
			return
		}
		if _, isPlaceholder := cur.(pendingPenCapture); !isPlaceholder {
			return
		}
		if err != nil {
			logrus.Warnf("🖊️ [PEN] capture failed for %s: %v", t.ID, err)
			delete(dw.activePenCaptures, t.ID)
			return
		}
		dw.activePenCaptures[t.ID] = cap
	})
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
//
// Looks the row up by drive.PenTabletID at click time rather than trusting
// the idx this closure was built with: combineDrives rebuilds dw.allDrives
// (and therefore every row's index) on its own periodic cadence
// (startPenTabletPolling's 1-second tick, same as gamepad's own poller),
// independently of refreshDashboard's own rebuild of the widgets holding
// this closure -- a combine landing in between left idx pointing at
// whatever drive happened to end up there in the freshly rebuilt slice,
// silently flipping the wrong row's IsMounted while the tablet's own
// (correct-index-but-still-false) entry made syncPenCaptures immediately
// undo the toggle. Confirmed live as the toggle appearing to work (the
// attach starts) and then the capture stopping again within about a
// second, every time.
func (dw *DiskWidget) newPenTabletToggle(drive DriveItem, cardHover func(bool)) *view.DeviceToggle {
	tabletID := drive.PenTabletID
	t := view.NewDeviceToggle(drive.IsMounted, func(on bool) {
		found := false
		for i := range dw.allDrives {
			if dw.allDrives[i].IsPenTablet && dw.allDrives[i].PenTabletID == tabletID {
				dw.allDrives[i].IsMounted = on
				found = true
				break
			}
		}
		logrus.Infof("🖊️ [PEN] toggle id=%q on=%v found=%v (len(allDrives)=%d)", tabletID, on, found, len(dw.allDrives))
		// syncPenCaptures itself is cheap (no I/O) -- it only ever queues a
		// pendingPenCapture placeholder and spawns attachPenCapture, which
		// does the actual blocking network I/O (AttachBrowserPen's HTTP
		// POST plus two WebSocket dials) off the Fyne thread. Confirmed
		// live that running that I/O synchronously from here -- Tapped()
		// itself runs on Fyne's own dispatch thread -- corrupts Fyne's
		// threading model the moment it parks and later resumes off Fyne's
		// own event loop, so syncPenCaptures must never do it directly.
		dw.syncPenCaptures()
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
