package controller

import "github.com/sirupsen/logrus"

func (vw *VideoWidget) startVideoOpsLoop() {
	if vw.videoOps != nil {
		return
	}

	vw.videoOps = make(chan videoOperation, 32)
	go func(ops <-chan videoOperation) {
		for op := range ops {
			if op.fn == nil {
				if op.done != nil {
					close(op.done)
				}
				continue
			}

			logrus.Debugf("🎬 video op start: %s", op.name)
			op.fn()
			logrus.Debugf("✅ video op done: %s", op.name)

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
