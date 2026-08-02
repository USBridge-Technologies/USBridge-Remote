//go:build android || ios

package controller

import (
	"math"
	"sync"
	"sync/atomic"
	"usbridge-client/internal/gui/graphics"

	"fyne.io/fyne/v2"
	"github.com/sirupsen/logrus"
)

var (
	activeMobileGestureTargetMu sync.RWMutex
	activeMobileGestureTarget   *VideoWidget

	// imeExpandBits stores math.Float32bits(imeHeightDp) atomically.
	// Non-zero means the system IME is open and the video should expand
	// to cover everything above the IME (tabs, custom keyboard panel, etc.).
	imeExpandBits atomic.Int32
)

func setImeExpandHeightDp(h float32) {
	imeExpandBits.Store(int32(math.Float32bits(h)))
}

func getImeExpandHeightDp() float32 {
	return math.Float32frombits(uint32(imeExpandBits.Load()))
}

func activeGestureVideoWidget() *VideoWidget {
	activeMobileGestureTargetMu.RLock()
	defer activeMobileGestureTargetMu.RUnlock()
	return activeMobileGestureTarget
}

func (vw *VideoWidget) platformRegisterGestureTarget() {
	activeMobileGestureTargetMu.Lock()
	activeMobileGestureTarget = vw
	activeMobileGestureTargetMu.Unlock()
}

func (vw *VideoWidget) platformHandleVirtualKeyboard() {
	if vw.virtualKeyboard == nil {
		if vw.parentWindow == nil {
			logrus.Warn("⚠️ Parent window is not set")
			return
		}
		// IME runes go through onRuneTyped → LiSendUtf8TextEvent, sent verbatim as
		// Unicode text -- the server (gamestream-server) now maps this into the
		// target OS's own input on the server side, so this works for any
		// language/layout the Android IME produces without a client-side rune→VK
		// guess table.
		vw.virtualKeyboard = graphics.NewVirtualKeyboard(vw.parentWindow, vw.handleVirtualKeyPress, func(r rune) {
			mi := vw.moonlightInput()
			if mi == nil || !mi.IsInputActive() {
				return
			}
			mi.SendMoonlightUtf8Text(string(r))
		})

		// When the Android IME opens/closes, expand Vulkan upward under the tabs.
		vw.virtualKeyboard.SetOnIMEChanged(func(imeHeightDp float32) {
			fyne.Do(func() {
				kl := vw.virtualKeyboard.GetKeyboardLayout()
				newH := kl.MinSize().Height
				canvasW := vw.parentWindow.Canvas().Size().Width
				kl.Resize(fyne.NewSize(canvasW, newH))
				vw.contentContainer.Objects = []fyne.CanvasObject{kl}
				vw.contentContainer.Resize(kl.Size())
				vw.container.Refresh()
				// Set expand flag AFTER layout so videoCanvasFrame reads the updated contentContainer size.
				vw.onIMEHeightChanged(imeHeightDp)
			})
		})
	}

	if vw.virtualKeyboard.IsVisible() {
		vw.virtualKeyboard.Hide()
		vw.contentContainer.Hide()
		vw.container.Refresh()
		// Vulkan rect will be restored on the next render tick via updateMetalVideoFrame.
		vw.forceCanvasRefresh.Store(true)
		logrus.Info("⌨️ Virtual keyboard hidden (Android mode)")
	} else {
		// Register as the recipient of native Android IME events
		vw.virtualKeyboard.RegisterAsIMETarget()

		keyboardLayout := vw.virtualKeyboard.GetKeyboardLayout()
		vw.virtualKeyboard.SetVisibleState(true)

		canvasSize := vw.parentWindow.Canvas().Size()
		keyboardLayout.Resize(fyne.NewSize(canvasSize.Width, keyboardLayout.MinSize().Height))
		keyboardLayout.Move(fyne.NewPos(0, 0))

		vw.contentContainer.Objects = []fyne.CanvasObject{keyboardLayout}
		vw.contentContainer.Resize(keyboardLayout.Size())
		vw.contentContainer.Show()
		vw.container.Refresh()
		// Shrink Vulkan SurfaceView above the keyboard on the next render tick.
		vw.forceCanvasRefresh.Store(true)
		logrus.Info("⌨️ Virtual keyboard shown with Android IME")
	}
}

func (vw *VideoWidget) platformShowVirtualKeyboardIfMobile() {
	if vw.virtualKeyboard == nil || !vw.virtualKeyboard.IsVisible() {
		vw.platformHandleVirtualKeyboard()
	}
}
