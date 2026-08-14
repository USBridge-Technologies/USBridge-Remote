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

// showConnectingSpinner starts (or restarts) the Moonlight-style spinner
// centered over the video area. Called from beginVideoTrace, i.e. on
// every connect attempt and every automatic reconnect, not just the
// first -- so it reappears if a mid-stream reconnect needs to
// renegotiate. Picks the gear icon over the plain dot spinner when
// connected to a real USBridge device (rust-shine backend) rather than a
// generic/manually-added Sunshine host, using the same isUSBridgeAgentOS
// signal already gating scripts/backup/pcpanel elsewhere in this package.
func (vw *VideoWidget) showConnectingSpinner() {
	if vw == nil || vw.ui == nil || vw.ui.SpinnerIcon == nil || vw.ui.SpinnerOverlay == nil {
		return
	}

	frames := assets.VideoConnectingFrames
	if isUSBridgeAgentOS(vw.agentOS) {
		frames = assets.VideoConnectingGearFrames
	}
	if len(frames) == 0 {
		return
	}

	vw.spinnerMu.Lock()
	if vw.spinnerStop != nil {
		// Already running -- just make sure the frame set matches
		// (agentOS may have been unknown on an earlier call and resolved
		// to USBridge by now) and that it's visible.
		vw.spinnerMu.Unlock()
		fyne.Do(func() {
			vw.ui.SpinnerIcon.Resource = frames[0]
			vw.ui.SpinnerIcon.Refresh()
			vw.ui.SpinnerOverlay.Show()
		})
		return
	}
	stop := make(chan struct{})
	vw.spinnerStop = stop
	vw.spinnerMu.Unlock()

	fyne.Do(func() {
		vw.ui.SpinnerIcon.Resource = frames[0]
		vw.ui.SpinnerIcon.Refresh()
		vw.ui.SpinnerOverlay.Show()
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
	})
}
