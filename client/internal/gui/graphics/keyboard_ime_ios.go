//go:build ios

package graphics

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Foundation -framework UIKit

extern void initKeyboardObserver(void);
*/
import "C"

import (
	"time"

	"fyne.io/fyne/v2"
	"github.com/sirupsen/logrus"
)

var (
	lastIMEH        float32 // last nav-bar/IME margin in Fyne dp units
	pendingIMEPx    int     // raw px value pending Fyne canvas initialization
	pendingScreenPx int
)

// GetLastIMEH returns the last cached IME margin
func GetLastIMEH() float32 {
	return lastIMEH
}

func init() {
	C.initKeyboardObserver()
}

// RegisterAsIMETarget registers this VirtualKeyboard as a receiver of native IME events.
func (vk *VirtualKeyboard) RegisterAsIMETarget() {
	activeIMEKeyboardMu.Lock()
	activeIMEKeyboardTarget = vk
	activeIMEKeyboardMu.Unlock()

	fyne.Do(func() {
		vk.setIMEOffset(lastIMEH)
	})
}

//export deliverIMEHeightFromObjC
func deliverIMEHeightFromObjC(imeHeightPx C.int, screenHeightPx C.int) {
	imePx := int(imeHeightPx)
	screenPx := int(screenHeightPx)
	logrus.Infof("⌨️ [IME-ObjC] imeHeightPx=%d screenHeightPx=%d", imePx, screenPx)
	fyne.Do(func() {
		applyIMEHeight(imePx, screenPx)
	})
}

// applyIMEHeight converts raw pixel values to Fyne dp and updates the keyboard spacer.
// Must be called on the Fyne main thread (inside fyne.Do).
func applyIMEHeight(imePx, screenPx int) {
	if screenPx <= 0 {
		return
	}

	vk := activeIMEKeyboard()
	canvasH := float32(0)
	if vk != nil && vk.parentWindow != nil {
		canvasH = vk.parentWindow.Canvas().Size().Height
	} else if fyne.CurrentApp() != nil && len(fyne.CurrentApp().Driver().AllWindows()) > 0 {
		canvasH = fyne.CurrentApp().Driver().AllWindows()[0].Canvas().Size().Height
	}

	if canvasH <= 0 {
		// Fyne canvas not ready yet — store raw values and retry once the window exists.
		pendingIMEPx = imePx
		pendingScreenPx = screenPx
		go func() {
			time.Sleep(300 * time.Millisecond)
			fyne.Do(func() {
				if pendingIMEPx == 0 {
					return
				}
				p, s := pendingIMEPx, pendingScreenPx
				pendingIMEPx, pendingScreenPx = 0, 0
				applyIMEHeight(p, s)
			})
		}()
		return
	}

	pendingIMEPx, pendingScreenPx = 0, 0
	calculatedIMEH := float32(imePx) / float32(screenPx) * canvasH
	lastIMEH = calculatedIMEH
	logrus.Infof("⌨️ [IME-ObjC] lastIMEH=%.0f canvasH=%.0f", lastIMEH, canvasH)

	if vk != nil {
		// Fyne already shrinks the canvas on iOS to accommodate the keyboard.
		// If we set the spacer height to >0, Fyne pushes the contentContainer
		// up by that much extra padding, causing it to fly off the top.
		vk.setIMEOffset(0)

		// But we MUST notify the video widget that the keyboard is open with its real height
		// so that the Metal overlay expands upward to cover the tabs.
		if vk.onIMEChanged != nil {
			vk.onIMEChanged(calculatedIMEH)
		}
	}
}
