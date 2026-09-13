//go:build ios

package controller

import "usbridge-client/internal/gui/graphics"

func platformSetupKeyboardWindow(_ *graphics.VirtualKeyboard) {}

func (vw *VideoWidget) platformSetSystemIMESticky(on bool) {
	vw.systemIMESticky.Store(false)
	_ = on
}
