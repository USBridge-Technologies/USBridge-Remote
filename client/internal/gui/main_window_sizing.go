package gui

import "fyne.io/fyne/v2"

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

	var contentMin fyne.Size
	if content := mw.window.Content(); content != nil {
		contentMin = content.MinSize()
	}

	mw.window.Resize(windowSizeToLogical(
		mw.config.WindowWidth,
		mw.config.WindowHeight,
		contentMin,
	))
	mw.window.CenterOnScreen()
}

func (mw *MainWindow) ensureWindowFitsContent() {
	if mw.window == nil {
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
