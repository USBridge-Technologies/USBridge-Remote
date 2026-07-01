//go:build windows

package controller

import (
	"usbridge-client/internal/gui/graphics"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver"
	"golang.org/x/sys/windows"
)

var (
	modUser32    = windows.NewLazySystemDLL("user32.dll")
	procSetWinPos = modUser32.NewProc("SetWindowPos")
)

// platformSetupKeyboardWindow makes the virtual keyboard window TOPMOST on
// Windows so it appears above the Vulkan overlay (which is also WS_EX_TOPMOST).
func platformSetupKeyboardWindow(vk *graphics.VirtualKeyboard) {
	vk.SetOnWindowShown(func(w fyne.Window) {
		nw, ok := w.(driver.NativeWindow)
		if !ok {
			return
		}
		nw.RunNative(func(ctx any) {
			var hwnd uintptr
			switch c := ctx.(type) {
			case driver.WindowsWindowContext:
				hwnd = c.HWND
			case *driver.WindowsWindowContext:
				hwnd = c.HWND
			}
			if hwnd == 0 {
				return
			}
			const (
				hwndTopmost  = ^uintptr(0) // HWND_TOPMOST = (HWND)-1
				swpNoMove    = 0x0002
				swpNoSize    = 0x0001
				swpNoActivate = 0x0010
			)
			procSetWinPos.Call(hwnd, hwndTopmost, 0, 0, 0, 0, swpNoMove|swpNoSize|swpNoActivate)
		})
	})
}
