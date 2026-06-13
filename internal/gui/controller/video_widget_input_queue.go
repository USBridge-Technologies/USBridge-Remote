package controller

import (
	"time"

	"usbridge-client/internal/service"

	"fyne.io/fyne/v2"
	"github.com/sirupsen/logrus"
)

func (vw *VideoWidget) enqueueMouseMove(dx, dy int) {
	if dx == 0 && dy == 0 {
		return
	}
	vw.moveQueueMu.Lock()
	vw.pendingMoveX += dx
	vw.pendingMoveY += dy
	vw.moveQueueMu.Unlock()
}

func (vw *VideoWidget) startMouseMoveWorker() {
	vw.moveQueueMu.Lock()
	if vw.moveWorkerStarted {
		vw.moveQueueMu.Unlock()
		return
	}
	vw.moveWorkerStarted = true
	vw.moveQueueMu.Unlock()

	// Periodic stats — every 30 s.
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			relML := vw.statRelMoonlight.Swap(0)
			absML := vw.statAbsMoonlight.Swap(0)
			logrus.Infof("🖱️ [Mouse] 30s | rel: moonlight=%d | abs: moonlight=%d | mode=%s",
				relML, absML, vw.GetMouseInputMode())
		}
	}()

	go func() {
		ticker := time.NewTicker(vw.mouseMoveFlushInterval())
		defer ticker.Stop()

		for range ticker.C {
			mi := vw.moonlightInput()
			if mi == nil || !mi.IsInputActive() {
				// Discard pending moves when not connected.
				vw.moveQueueMu.Lock()
				vw.pendingMoveX = 0
				vw.pendingMoveY = 0
				vw.moveQueueMu.Unlock()
				continue
			}
			for {
				dx, dy := vw.takeMouseMoveChunk()
				if dx == 0 && dy == 0 {
					break
				}
				vw.statRelMoonlight.Add(1)
				mi.SendMoonlightMouseMove(int16(dx), int16(dy))
			}
		}
	}()
}

func (vw *VideoWidget) mouseMoveFlushInterval() time.Duration {
	if fyne.CurrentDevice().IsMobile() {
		return 8 * time.Millisecond
	}
	return 6 * time.Millisecond
}

func (vw *VideoWidget) takeMouseMoveChunk() (int, int) {
	vw.moveQueueMu.Lock()
	defer vw.moveQueueMu.Unlock()

	dx := takeAxisChunk(&vw.pendingMoveX)
	dy := takeAxisChunk(&vw.pendingMoveY)
	return dx, dy
}

func (vw *VideoWidget) prependMouseMoveChunk(dx, dy int) {
	if dx == 0 && dy == 0 {
		return
	}
	vw.moveQueueMu.Lock()
	vw.pendingMoveX += dx
	vw.pendingMoveY += dy
	vw.moveQueueMu.Unlock()
}

func takeAxisChunk(pending *int) int {
	if *pending > 127 {
		*pending -= 127
		return 127
	}
	if *pending < -127 {
		*pending += 127
		return -127
	}
	value := *pending
	*pending = 0
	return value
}

func (vw *VideoWidget) enqueueMouseClick(button int) {
	mi := vw.moonlightInput()
	if mi == nil || !mi.IsInputActive() {
		return
	}
	moonlightBtn := button
	if button == 2 {
		moonlightBtn = service.LiMouseButtonRight
	}
	mi.SendMoonlightMouseButton(service.LiMouseButtonPress, moonlightBtn)
	mi.SendMoonlightMouseButton(service.LiMouseButtonRelease, moonlightBtn)
}

func (vw *VideoWidget) enqueueMouseScroll(scroll int) {
	mi := vw.moonlightInput()
	if mi == nil || !mi.IsInputActive() {
		return
	}
	clicks := int8(clamp(scroll, -127, 127))
	mi.SendMoonlightScroll(clicks)
}

// enqueueMouseAction — reset all buttons and state.
func (vw *VideoWidget) enqueueMouseAction(dx, dy, buttons, wheel int) {
	if dx == 0 && dy == 0 && buttons == 0 && wheel == 0 {
		vw.ReleaseAllAbsoluteButtons(vw.lastAbsX, vw.lastAbsY)
	}
}

// Touch functions: route through absolute Moonlight mouse path.

func (vw *VideoWidget) enqueueTouch(x, y int, down bool) {
	if down {
		vw.PressAbsoluteButton(1, x, y)
	} else {
		vw.ReleaseAbsoluteButton(1, x, y)
	}
}

func (vw *VideoWidget) enqueueTouchPositionOnly(x, y int, _ bool) {
	vw.SendAbsolutePosition(x, y, true)
}

func (vw *VideoWidget) enqueueTouchTap(x, y int) {
	vw.ClickAbsoluteButton(1, x, y)
}

func (vw *VideoWidget) enqueueSecondaryTouchTap(x, y int) {
	vw.ClickAbsoluteButton(2, x, y)
}
