package controller

import (
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
	if !vw.videoReconcilePending.CompareAndSwap(false, true) {
		logrus.Debugf("⏳ video reconcile already queued, coalescing reason=%s", reason)
		return
	}

	vw.enqueueVideoOp("video-reconcile:"+reason, func() {
		defer vw.videoReconcilePending.Store(false)
		vw.reconcileVideoState(reason)
		if vw.videoReconcileNeeded() {
			vw.scheduleVideoReconcile("coalesced")
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

func (vw *VideoWidget) reconcileVideoState(reason string) {
	if !vw.beginVideoOperation() {
		logrus.Debugf("⏳ reconcile skipped, video operation already running reason=%s", reason)
		return
	}
	defer vw.finishVideoOperation()

	vw.videoOpMu.Lock()
	desiredStreaming := vw.desiredStreaming
	restartPending := vw.videoRestartPending
	streaming := vw.isStreaming
	vw.videoOpMu.Unlock()

	logrus.Infof("🎬 video reconcile: reason=%s desired=%v streaming=%v restart=%v", reason, desiredStreaming, streaming, restartPending)

	if !desiredStreaming {
		if vw.usbClient != nil {
			vw.stopVideoInternal()
		} else if vw.isStreaming {
			// Клиент уже ушёл (потеря соединения), очищаем только локальное состояние
			vw.isStreaming = false
			vw.isGStreamerConnected = false
			vw.isMouseConnected = false
			vw.clearVideo()
			fyne.Do(func() { vw.updateButtons() })
			vw.updateStatus()
		}
		return
	}

	if streaming && !restartPending {
		return
	}

	cfg, err := vw.resolvePreferredVideoConfig()
	if err != nil {
		logrus.Warnf("⚠️ cannot resolve preferred video config during reconcile (%s): %v", reason, err)
		if vw.statusLabel != nil {
			fyne.Do(func() {
				vw.statusLabel.SetText("❌ " + err.Error())
			})
		}
		return
	}

	vw.videoOpMu.Lock()
	vw.videoRestartPending = false
	vw.videoOpMu.Unlock()

	vw.startVideoWithParamsInternal(cfg.ToVideoStartRequest())
}
