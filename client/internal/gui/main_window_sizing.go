package gui

import (
	"usbridge-client/internal/gui/view"

	"fyne.io/fyne/v2"
)

const (
	minConfiguredWindowWidth  = 800
	minConfiguredWindowHeight = 600
	defaultWindowWidth        = 1024
	defaultWindowHeight       = 768
)

func normalizeConfiguredWindowPixels(width, height int) (int, int) {
	if width < minConfiguredWindowWidth {
		width = defaultWindowWidth
	}
	if height < minConfiguredWindowHeight {
		height = defaultWindowHeight
	}

	return width, height
}

// windowSizeToLogical converts configured sizes into Fyne logical units.
func windowSizeToLogical(width, height int, contentMin fyne.Size) fyne.Size {
	width, height = normalizeConfiguredWindowPixels(width, height)

	size := fyne.NewSize(float32(width), float32(height))
	if contentMin.Width > size.Width {
		size.Width = contentMin.Width
	}
	if contentMin.Height > size.Height {
		size.Height = contentMin.Height
	}

	return size
}

func clampWindowSizeToAvailableArea(size fyne.Size, maxSize fyne.Size) fyne.Size {
	if maxSize.Width > 0 && size.Width > maxSize.Width {
		size.Width = maxSize.Width
	}
	if maxSize.Height > 0 && size.Height > maxSize.Height {
		size.Height = maxSize.Height
	}

	return size
}

func expandWindowSizeToPreferredArea(size fyne.Size, preferred fyne.Size) fyne.Size {
	if preferred.Width > size.Width {
		size.Width = preferred.Width
	}
	if preferred.Height > size.Height {
		size.Height = preferred.Height
	}

	return size
}

func (mw *MainWindow) applyInitialWindowSize() {
	if mw.window == nil || mw.app == nil {
		return
	}

	if view.ForceMobileDesign {
		mw.applyPhonePreviewWindowSize()
		return
	}
	mw.window.SetFixedSize(false)

	var contentMin fyne.Size
	if content := mw.window.Content(); content != nil {
		contentMin = content.MinSize()
	}

	width, height := mw.config.WindowWidth, mw.config.WindowHeight
	if lw, lh, ok := mw.savedLogicalWindowSize(); ok {
		width, height = lw, lh
	}

	mw.window.Resize(windowSizeToLogical(width, height, contentMin))
	// CenterOnScreen uses Fyne/GLFW's "current" monitor, which on first
	// Show is the primary. Skip it when we have a last-session frame so
	// the window can reopen on the same display the user left it on.
	if !mw.canRestoreWindowPlacement() {
		mw.window.CenterOnScreen()
	}
}

func (mw *MainWindow) applyPhonePreviewWindowSize() {
	view.ApplyPreviewUserScale()
	view.ReloadFyneCanvasScale()
	p := view.CurrentPhonePreview()
	size := fyne.NewSize(p.Width, p.Height)
	mw.lastGoodWindowSize = size
	mw.window.SetFixedSize(false)
	mw.window.Resize(size)
	mw.window.SetFixedSize(true)
	if !mw.canRestoreWindowPlacement() {
		mw.window.CenterOnScreen()
	}
}

func (mw *MainWindow) ensureWindowFitsContent() {
	if mw.window == nil || view.ForceMobileDesign {
		return
	}

	content := mw.window.Content()
	if content == nil {
		return
	}

	minSize := content.MinSize()
	currentSize := mw.window.Canvas().Size()
	if currentSize.Width <= 0 || currentSize.Height <= 0 {
		return
	}

	targetSize := currentSize
	needsResize := false

	if targetSize.Width < minSize.Width {
		targetSize.Width = minSize.Width
		needsResize = true
	}
	if targetSize.Height < minSize.Height {
		targetSize.Height = minSize.Height
		needsResize = true
	}

	if needsResize {
		mw.window.Resize(targetSize)
	}
}
