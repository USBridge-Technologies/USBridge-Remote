package controller

import (
	"time"

	"fyne.io/fyne/v2"
	"github.com/sirupsen/logrus"
)

func (vw *VideoWidget) startVideoOpsLoop() {
	if vw.videoOps != nil {
		return
	}

	vw.videoOps = make(chan videoOperation, 32)
	go func(ops <-chan videoOperation) {
		defer func() {
			if r := recover(); r != nil {
				logrus.Errorf("🔥 PANIC in video ops loop: %v", r)
			}
		}()

		for op := range ops {
			if op.fn == nil {
				if op.done != nil {
					close(op.done)
				}
				continue
			}

			func() {
				defer func() {
					if r := recover(); r != nil {
						logrus.Errorf("🔥 PANIC in video op %s: %v", op.name, r)
					}
				}()
				logrus.Debugf("🎬 video op start: %s", op.name)
				op.fn()
				logrus.Debugf("✅ video op done: %s", op.name)
			}()

			if op.done != nil {
				close(op.done)
			}
		}
	}(vw.videoOps)
}

func (vw *VideoWidget) enqueueVideoOp(name string, fn func()) {
	if fn == nil {
		return
	}
	vw.startVideoOpsLoop()
	vw.videoOps <- videoOperation{name: name, fn: fn}
}

func (vw *VideoWidget) runVideoOpSync(name string, fn func()) {
	if fn == nil {
		return
	}
	vw.startVideoOpsLoop()
	done := make(chan struct{})
	vw.videoOps <- videoOperation{name: name, fn: fn, done: done}
	<-done
}

func (vw *VideoWidget) scheduleVideoReconcile(reason string) {
	if vw.isClosing.Load() {
		return
	}
	if !vw.videoReconcilePending.CompareAndSwap(false, true) {
		logrus.Debugf("⏳ video reconcile already queued, coalescing reason=%s", reason)
		return
	}

	vw.enqueueVideoOp("video-reconcile:"+reason, func() {
		defer vw.videoReconcilePending.Store(false) // safety net: always resets on panic
		immediateRetry := vw.reconcileVideoState(reason)
		// Clear pending before the check so that scheduleVideoReconcile("coalesced")
		// can succeed its CAS. Without this the defer runs too late and the coalesced
		// reconcile is silently dropped, leaving desired≠streaming forever.
		vw.videoReconcilePending.Store(false)
		if vw.isClosing.Load() {
			return
		}
		if immediateRetry && vw.videoReconcileNeeded() {
			vw.scheduleVideoReconcile("coalesced")
		}
	})
}

const videoStartRetryDelay = 2 * time.Second

func (vw *VideoWidget) stopDelayedVideoRetry() {
	vw.videoStartRetryMu.Lock()
	defer vw.videoStartRetryMu.Unlock()
	if vw.videoStartRetry != nil {
		vw.videoStartRetry.Stop()
		vw.videoStartRetry = nil
	}
}

func (vw *VideoWidget) scheduleDelayedVideoRetry(reason string) {
	if vw.isClosing.Load() || vw.userStoppedVideo.Load() {
		return
	}
	vw.videoStartRetryMu.Lock()
	defer vw.videoStartRetryMu.Unlock()
	if vw.videoStartRetry != nil {
		vw.videoStartRetry.Stop()
	}
	vw.videoStartRetry = time.AfterFunc(videoStartRetryDelay, func() {
		if vw.isClosing.Load() || vw.userStoppedVideo.Load() {
			return
		}
		if vw.videoReconcileNeeded() {
			vw.scheduleVideoReconcile(reason)
		}
	})
}

func (vw *VideoWidget) videoReconcileNeeded() bool {
	vw.videoOpMu.Lock()
	defer vw.videoOpMu.Unlock()

	if vw.videoOpRunning {
		return false
	}
	if vw.videoRestartPending {
		return true
	}
	return vw.desiredStreaming != vw.isStreaming
}

func (vw *VideoWidget) reconcileVideoState(reason string) bool {
	if vw.isClosing.Load() {
		return false
	}
	if !vw.beginVideoOperation() {
		logrus.Debugf("⏳ reconcile skipped, video operation already running reason=%s", reason)
		return false
	}
	defer vw.finishVideoOperation()

	vw.videoOpMu.Lock()
	desiredStreaming := vw.desiredStreaming
	restartPending := vw.videoRestartPending
	streaming := vw.isStreaming
	vw.videoOpMu.Unlock()

	logrus.Infof("🎬 video reconcile: reason=%s desired=%v streaming=%v restart=%v", reason, desiredStreaming, streaming, restartPending)

	if !desiredStreaming {
		// Only call stopVideoInternal when we actually think we are streaming.
		// A stale reconcile triggered by a disconnect callback can arrive AFTER a
		// new session has started (e.g. StopVideoSync timed out → Disconnect fired
		// onStateChanged → new usbClient set → reconcile runs). Without this guard
		// the reconcile would call StopVideo() / Disconnect() on the new client.
		if streaming {
			if vw.usbClient != nil {
				vw.stopVideoInternal()
			} else {
				// Client is already gone (connection lost), clean up only the local state
				vw.isStreaming = false
				vw.isVideoConnected = false
				vw.isMouseConnected = false
				vw.clearVideo()
				if !vw.isClosing.Load() {
					fyne.Do(func() { vw.updateButtons() })
					vw.updateStatus()
				}
			}
		}
		// Clear restartPending: if we don't want streaming there's nothing to restart.
		// Without this, videoReconcileNeeded() sees restartPending=true and re-schedules
		// reconcile indefinitely, hammering StopVideo() and UpdateStatusBar() on each tick.
		if restartPending {
			vw.videoOpMu.Lock()
			vw.videoRestartPending = false
			vw.videoOpMu.Unlock()
		}
		return vw.videoReconcileNeeded()
	}

	if streaming && !restartPending {
		return false
	}

	if streaming && restartPending {
		logrus.Info("🎬 Restart pending: stopping current stream before restart")
		if vw.usbClient != nil {
			vw.stopVideoInternal()
		} else {
			vw.isStreaming = false
			vw.isVideoConnected = false
			vw.isMouseConnected = false
			vw.clearVideo()
		}
	}

	cfg, err := vw.resolvePreferredVideoConfig()
	if err != nil {
		logrus.Warnf("⚠️ cannot resolve preferred video config during reconcile (%s): %v", reason, err)
		if vw.statusLabel != nil && !vw.isClosing.Load() {
			fyne.Do(func() {
				vw.statusLabel.SetText("❌ " + err.Error())
			})
		}
		// Missing capture devices used to coalesced-retry immediately
		// (HTTP + fyne.Do several times a second) and keep the Fyne queue
		// busy through Quit. Retry on a timer instead.
		vw.scheduleDelayedVideoRetry("devices-retry")
		return false
	}

	vw.videoOpMu.Lock()
	vw.videoRestartPending = false
	vw.videoOpMu.Unlock()

	// Fires the same callback applyVideoDeviceConfig uses for an explicit
	// resolution change (see its own doc comment) -- reconcile is also how
	// video starts on app launch/auto-connect, which never goes through
	// applyVideoDeviceConfig at all, so without this the header's
	// resolution label had no way to learn the real starting resolution
	// and stuck at its stale default until the user changed it once.
	if vw.onResolutionChanged != nil && cfg.VideoWidth > 0 && cfg.VideoHeight > 0 {
		vw.onResolutionChanged(cfg.VideoWidth, cfg.VideoHeight)
	}

	vw.startVideoWithParamsInternal(cfg.ToVideoStartRequest())
	return false
}
