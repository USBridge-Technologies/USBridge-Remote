//go:build ios

package graphics

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Foundation -framework UIKit

extern void initKeyboardObserver(void);
extern void setStickyIMEEnabled(int enabled);
extern void reassertStickyIME(void);
*/
import "C"

import (
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"github.com/sirupsen/logrus"
)

var (
	lastIMEH        float32 // last nav-bar/IME margin in Fyne dp units
	pendingIMEPx    int     // raw px value pending Fyne canvas initialization
	pendingScreenPx int

	imeTextHandlerMu sync.Mutex
	imeTextHandler   func(deleteCount int, text string)

	imeUserDismissedMu      sync.Mutex
	imeUserDismissedHandler func()
)

// GetLastIMEH returns the last cached IME margin
func GetLastIMEH() float32 {
	return lastIMEH
}

// SetStickySystemIME keeps a hidden UITextField as first responder so the
// soft keyboard stays up while Control video touches run (Android-parity).
func SetStickySystemIME(enabled bool) {
	flag := 0
	if enabled {
		flag = 1
	}
	C.setStickyIMEEnabled(C.int(flag))
	logrus.Infof("⌨️ [IME] SetStickySystemIME(%v)", enabled)
}

// SetIMETextHandler registers the sticky soft-IME text sink (VideoWidget).
func SetIMETextHandler(fn func(deleteCount int, text string)) {
	imeTextHandlerMu.Lock()
	imeTextHandler = fn
	imeTextHandlerMu.Unlock()
}

// SetIMEUserDismissedHandler is used on Android (Back). iOS dismiss is the
// special-keys button only while sticky owns first responder.
func SetIMEUserDismissedHandler(fn func()) {
	imeUserDismissedMu.Lock()
	imeUserDismissedHandler = fn
	imeUserDismissedMu.Unlock()
}

// ReassertStickySystemIME re-claims first responder if something stole it.
func ReassertStickySystemIME() {
	C.reassertStickyIME()
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

//export deliverIMETextFromObjC
func deliverIMETextFromObjC(deleteCount C.int, textStr *C.char) {
	del := int(deleteCount)
	text := ""
	if textStr != nil {
		text = C.GoString(textStr)
	}
	logrus.Infof("⌨️ [IME-TEXT-ObjC] del=%d add=%q", del, text)
	imeTextHandlerMu.Lock()
	fn := imeTextHandler
	imeTextHandlerMu.Unlock()
	if fn == nil {
		return
	}
	fyne.Do(func() {
		fn(del, text)
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
	// Use raw points: Fyne canvas often already shrinks for the IME, so
	// scaling by canvasH/screenPx under-reports and Metal overdraws the keys.
	calculatedIMEH := float32(imePx)
	lastIMEH = calculatedIMEH
	logrus.Infof("⌨️ [IME-ObjC] lastIMEH=%.0f canvasH=%.0f", lastIMEH, canvasH)

	if vk != nil {
		// Fyne already shrinks the canvas on iOS for the keyboard — do not
		// add an extra spacer (that would push content off the top).
		vk.setIMEOffset(0)
		if vk.onIMEChanged != nil {
			vk.onIMEChanged(calculatedIMEH)
		}
	}
}
