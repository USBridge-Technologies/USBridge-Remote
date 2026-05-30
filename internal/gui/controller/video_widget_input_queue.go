package controller

import (
	"time"

	"usbridge-client/internal/api"
	"usbridge-client/internal/service"

	"fyne.io/fyne/v2"
	"github.com/sirupsen/logrus"
)

const videoInputQueueSize = 256

func (vw *VideoWidget) startInputWorker() {
	if vw.inputQueue != nil {
		return
	}
	vw.inputQueue = make(chan inputCommand, videoInputQueueSize)

	go func(queue <-chan inputCommand) {
		for cmd := range queue {
			client := vw.usbClient
			if client == nil || cmd.run == nil {
				continue
			}
			if err := cmd.run(client); err != nil {
				logrus.Debugf("video input command %s failed: %v", cmd.name, err)
			}
		}
	}(vw.inputQueue)

	vw.startMouseMoveWorker()
}

func (vw *VideoWidget) enqueueInputCommand(dropIfBusy bool, name string, run func(*api.USBClient) error) {
	if run == nil {
		return
	}
	if vw.inputQueue == nil {
		vw.startInputWorker()
	}

	cmd := inputCommand{
		dropIfBusy: dropIfBusy,
		name:       name,
		run:        run,
	}

	if dropIfBusy {
		select {
		case vw.inputQueue <- cmd:
		default:
		}
		return
	}

	select {
	case vw.inputQueue <- cmd:
	default:
		go func() {
			vw.inputQueue <- cmd
		}()
	}
}

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

	// Periodic stats — every 30 s, mirrors the server-side [MouseBridge] log.
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			relML := vw.statRelMoonlight.Swap(0)
			relWS := vw.statRelWS.Swap(0)
			absML := vw.statAbsMoonlight.Swap(0)
			absWS := vw.statAbsWS.Swap(0)
			logrus.Infof("🖱️ [Mouse] 30s | rel: moonlight=%d ws=%d | abs: moonlight=%d ws=%d | mode=%s",
				relML, relWS, absML, absWS, vw.GetMouseInputMode())
		}
	}()

	go func() {
		ticker := time.NewTicker(vw.mouseMoveFlushInterval())
		defer ticker.Stop()

		var lastRelPath string // "moonlight" | "ws" | ""

		for range ticker.C {
			if mi := vw.moonlightInput(); mi != nil && mi.IsInputActive() {
				if lastRelPath != "moonlight" {
					logrus.Infof("🖱️ [Mouse] relative path → Moonlight SendMoonlightMouseMove (was: %q)", lastRelPath)
					lastRelPath = "moonlight"
				}
				for {
					dx, dy := vw.takeMouseMoveChunk()
					if dx == 0 && dy == 0 {
						break
					}
					vw.statRelMoonlight.Add(1)
					mi.SendMoonlightMouseMove(int16(dx), int16(dy))
				}
				continue
			}

			if lastRelPath == "moonlight" {
				logrus.Info("🖱️ [Mouse] relative path → WebSocket (Moonlight no longer active)")
				lastRelPath = "ws"
			}

			client := vw.usbClient
			if client == nil {
				continue
			}

			if lastRelPath == "" {
				logrus.Info("🖱️ [Mouse] relative path → WebSocket (initial)")
				lastRelPath = "ws"
			}

			for {
				dx, dy := vw.takeMouseMoveChunk()
				if dx == 0 && dy == 0 {
					break
				}
				vw.statRelWS.Add(1)
				if err := client.SendMouseMove(dx, dy); err != nil {
					logrus.Debugf("video input command mouse-move failed: %v", err)
					vw.prependMouseMoveChunk(dx, dy)
					break
				}
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
	if mi := vw.moonlightInput(); mi != nil && mi.IsInputActive() {
		// Moonlight right button = 3; our right = 2.
		moonlightBtn := button
		if button == 2 {
			moonlightBtn = 3
		}
		mi.SendMoonlightMouseButton(service.LiMouseButtonPress, moonlightBtn)
		mi.SendMoonlightMouseButton(service.LiMouseButtonRelease, moonlightBtn)
		return
	}
	vw.enqueueInputCommand(false, "mouse-click", func(client *api.USBClient) error {
		return client.SendMouseClick(button)
	})
}

func (vw *VideoWidget) enqueueMouseScroll(scroll int) {
	if mi := vw.moonlightInput(); mi != nil && mi.IsInputActive() {
		clicks := int8(clamp(scroll, -127, 127))
		mi.SendMoonlightScroll(clicks)
		return
	}
	vw.enqueueInputCommand(true, "mouse-scroll", func(client *api.USBClient) error {
		return client.SendMouseScroll(scroll)
	})
}

func (vw *VideoWidget) enqueueMouseAction(dx, dy, buttons, wheel int) {
	vw.enqueueInputCommand(true, "mouse-action", func(client *api.USBClient) error {
		return client.SendMouseAction(dx, dy, buttons, wheel)
	})
}

func (vw *VideoWidget) enqueueTouch(x, y int, down bool) {
	vw.enqueueInputCommand(!down, "touch", func(client *api.USBClient) error {
		return client.SendTouch(x, y, down)
	})
}

func (vw *VideoWidget) enqueueTouchPositionOnly(x, y int, down bool) {
	vw.enqueueInputCommand(!down, "touch-position", func(client *api.USBClient) error {
		return client.SendTouchPositionOnly(x, y, down)
	})
}

func (vw *VideoWidget) enqueueTouchTap(x, y int) {
	vw.enqueueInputCommand(false, "touch-tap", func(client *api.USBClient) error {
		if err := client.SendTouch(x, y, true); err != nil {
			return err
		}
		return client.SendTouch(x, y, false)
	})
}

func (vw *VideoWidget) enqueueSecondaryTouchTap(x, y int) {
	vw.enqueueInputCommand(false, "touch-secondary-tap", func(client *api.USBClient) error {
		if err := client.SendTouchPositionOnly(x, y, false); err != nil {
			return err
		}
		return client.SendMouseClick(2)
	})
}
