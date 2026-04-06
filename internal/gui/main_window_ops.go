package gui

import "github.com/sirupsen/logrus"

func (mw *MainWindow) runLifecycleLoop() {
	for op := range mw.lifecycleOps {
		if op == nil {
			continue
		}
		op()
	}
}

func (mw *MainWindow) enqueueLifecycleOp(name string, op func()) {
	if op == nil {
		return
	}

	wrapped := func() {
		mw.lifecycleMu.Lock()
		defer mw.lifecycleMu.Unlock()
		logrus.Debugf("🔁 lifecycle op start: %s", name)
		defer logrus.Debugf("✅ lifecycle op done: %s", name)
		op()
	}

	select {
	case mw.lifecycleOps <- wrapped:
	default:
		go func() {
			mw.lifecycleOps <- wrapped
		}()
	}
}
