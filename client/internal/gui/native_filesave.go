package gui

import (
	"path/filepath"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver"
)

// saveFilter is the one file type a native save panel offers, next to
// "All files": Name is shown to the operator, Ext ("zip", "png") is the
// pattern and the extension appended when the operator types none.
type saveFilter struct {
	Name string
	Ext  string
}

var (
	zipSaveFilter = saveFilter{Name: "ZIP archive", Ext: "zip"}
	pngSaveFilter = saveFilter{Name: "PNG image", Ext: "png"}
)

func withZipExt(path string) string {
	if strings.EqualFold(filepath.Ext(path), ".zip") {
		return path
	}
	return path + ".zip"
}

// nativeParentWindow is the platform handle of w (X11 window id, HWND,
// NSWindow) the native save panel is parented to, so it opens modal over
// the client instead of somewhere behind it. 0 when there is none (Wayland,
// mobile, a non-GLFW driver). Called on the Fyne thread, before the
// goroutine that blocks on the panel.
func nativeParentWindow(w fyne.Window) uintptr {
	nw, ok := w.(driver.NativeWindow)
	if !ok || w == nil {
		return 0
	}
	var h uintptr
	nw.RunNative(func(ctx any) {
		switch c := ctx.(type) {
		case driver.X11WindowContext:
			h = c.WindowHandle
		case *driver.X11WindowContext:
			h = c.WindowHandle
		case driver.WindowsWindowContext:
			h = c.HWND
		case *driver.WindowsWindowContext:
			h = c.HWND
		case driver.MacWindowContext:
			h = c.NSWindow
		case *driver.MacWindowContext:
			h = c.NSWindow
		}
	})
	return h
}
