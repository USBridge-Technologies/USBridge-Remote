package controller

import (
	"time"

	"usbridge-client/internal/gui/assets"

	"fyne.io/fyne/v2"
)

// spinnerFrameInterval matches HeaderActionButton.startSpinner's own
// cadence (header_action_button.go) so the two spinners feel consistent
// wherever a user sees both in the same session.
const spinnerFrameInterval = 140 * time.Millisecond

// showConnectingSpinner starts (or restarts) the Moonlight-style dot
// spinner centered over the video area. Called from beginVideoTrace, i.e.
// on every connect attempt and every automatic reconnect, not just the
// first -- so it reappears if a mid-stream reconnect needs to renegotiate,
// and so it can pick up a color change if isUSBridgeAgentOS's answer
// changed since the last call (agentOS is often still unknown -- and so
// treated as USBridge, see that function's own doc comment -- on the very
// first call, only resolving to its real value once device info actually
// arrives).
//
// Colored lime for real USBridge KVM hardware, turquoise for a plain OS
// agent (Windows/Linux/macOS) or anything else -- used to also switch to a
// different (rotating gear) shape for the USBridge case, which is what
// actually made this feel buggy: the frame-cycling goroutine below closes
// over its own `frames` slice once at start, so a later call here that
// wanted to switch that shape/color could only ever patch the single
// visible frame (this func's own old comment called that out explicitly)
// before the next tick silently reverted it back to the stale set. Now
// that both variants share one shape, that mismatch is just a color that
// looks briefly wrong for one call instead of a shape doing so on every
// tick -- but it's still fixed properly below: spinnerIsKVM records which
// variant is actually running, and a call that wants the other one stops
// and restarts the goroutine with the new frames instead of patching over
// the old one.
func (vw *VideoWidget) showConnectingSpinner() {
	if vw == nil {
		return
	}
	vw.debugLogSpinner("showConnectingSpinner-called")
	if vw.ui == nil || vw.ui.SpinnerIcon == nil || vw.ui.SpinnerOverlay == nil {
		return
	}

	wantKVM := isUSBridgeAgentOS(vw.agentOS)
	frames := assets.VideoConnectingFramesAgent
	if wantKVM {
		frames = assets.VideoConnectingFramesKVM
	}
	if len(frames) == 0 {
		return
	}

	vw.spinnerMu.Lock()
	if vw.spinnerStop != nil && vw.spinnerIsKVM == wantKVM {
		// Already running the right variant -- just make sure it's visible.
		vw.spinnerMu.Unlock()
		fyne.Do(func() {
			vw.ui.SpinnerOverlay.Show()
			vw.debugLogSpinner("show(already-running)")
		})
		return
	}
	if oldStop := vw.spinnerStop; oldStop != nil {
		// Running the wrong variant (agentOS resolved since it started) --
		// stop it so its goroutine's own stale `frames` closure can't keep
		// overwriting the new one below on its next tick.
		close(oldStop)
	}
	stop := make(chan struct{})
	vw.spinnerStop = stop
	vw.spinnerIsKVM = wantKVM
	vw.spinnerMu.Unlock()

	fyne.Do(func() {
		vw.ui.SpinnerIcon.Resource = frames[0]
		vw.ui.SpinnerIcon.Refresh()
		vw.ui.SpinnerOverlay.Show()
		vw.debugLogSpinner("show")
	})

	go func() {
		ticker := time.NewTicker(spinnerFrameInterval)
		defer ticker.Stop()
		step := 0
		for {
			select {
			case <-ticker.C:
				step = (step + 1) % len(frames)
				frame := frames[step]
				fyne.Do(func() {
					vw.spinnerMu.Lock()
					active := vw.spinnerStop == stop
					vw.spinnerMu.Unlock()
					if !active || vw.ui == nil || vw.ui.SpinnerIcon == nil {
						return
					}
					vw.ui.SpinnerIcon.Resource = frame
					vw.ui.SpinnerIcon.Refresh()
					vw.debugLogSpinner("tick")
				})
			case <-stop:
				return
			}
		}
	}()
}

// hideConnectingSpinner stops the frame-cycling goroutine and hides the
// overlay -- called once the first real video frame arrives
// (noteVideoTraceFirstFrame) or the session ends before one ever did
// (video_widget_ctor.go's disconnected/error/stopped state handler,
// cleanupDeadConnectionState).
func (vw *VideoWidget) hideConnectingSpinner() {
	if vw == nil {
		return
	}
	vw.spinnerMu.Lock()
	stop := vw.spinnerStop
	vw.spinnerStop = nil
	vw.spinnerMu.Unlock()
	if stop != nil {
		close(stop)
	}
	if vw.ui == nil || vw.ui.SpinnerOverlay == nil {
		return
	}
	fyne.Do(func() {
		vw.ui.SpinnerOverlay.Hide()
		vw.debugLogSpinner("hide")
	})
}
