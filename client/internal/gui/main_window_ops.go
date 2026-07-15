package gui

import "github.com/sirupsen/logrus"

func (mw *MainWindow) runLifecycleLoop() {
	defer func() {
		if r := recover(); r != nil {
			logrus.Errorf("🔥 PANIC in lifecycle loop: %v", r)
		}
	}()

	for op := range mw.lifecycleOps {
		if op == nil {
			continue
		}
		func() {
			defer func() {
				if r := recover(); r != nil {
					logrus.Errorf("🔥 PANIC in lifecycle operation: %v", r)
				}
			}()
			op()
		}()
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

// RefreshNetworkState обновляет состояние сети во всех сервисах
func (mw *MainWindow) RefreshNetworkState() {
	mw.enqueueLifecycleOp("refresh-network", func() {
		if mw.tailscaleService != nil {
			mw.tailscaleService.RefreshNetwork()
		}

		if mw.isConnected && mw.isStreaming && mw.videoWidget != nil {
			logrus.Info("🎬 Network changed while streaming, checking video state...")
			// Можно добавить легкую проверку или просто дождаться следующего кадра.
			// Если соединение реально упало, handleTransportError это заметит.
		}
	})
}
