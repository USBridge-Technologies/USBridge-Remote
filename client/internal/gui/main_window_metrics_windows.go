//go:build windows

package gui

import (
	"syscall"
	"unsafe"

	"fyne.io/fyne/v2"
)

const (
	defaultWindowsDPI        = 96
	startupWorkAreaPaddingPx = 48
	spiGetWorkArea           = 0x0030
	preferredWorkAreaWidth   = 0.75
	preferredWorkAreaHeight  = 0.82
)

type winRect struct {
	Left   int32
	Top    int32
	Right  int32
	Bottom int32
}

func startupWindowScale(app fyne.App) float32 {
	user32 := syscall.NewLazyDLL("user32.dll")
	getDpiForSystem := user32.NewProc("GetDpiForSystem")
	if getDpiForSystem.Find() == nil {
		dpi, _, _ := getDpiForSystem.Call()
		if dpi > 0 {
			return float32(dpi) / defaultWindowsDPI
		}
	}

	return app.Settings().Scale()
}

func startupWindowMaxSize(scale float32) fyne.Size {
	if scale <= 0 {
		scale = 1
	}

	user32 := syscall.NewLazyDLL("user32.dll")
	systemParametersInfoW := user32.NewProc("SystemParametersInfoW")
	if systemParametersInfoW.Find() != nil {
		return fyne.NewSize(0, 0)
	}

	var rect winRect
	ret, _, _ := systemParametersInfoW.Call(
		uintptr(spiGetWorkArea),
		0,
		uintptr(unsafe.Pointer(&rect)),
		0,
	)
	if ret == 0 {
		return fyne.NewSize(0, 0)
	}

	widthPx := float32(rect.Right - rect.Left - startupWorkAreaPaddingPx)
	heightPx := float32(rect.Bottom - rect.Top - startupWorkAreaPaddingPx)
	if widthPx <= 0 || heightPx <= 0 {
		return fyne.NewSize(0, 0)
	}

	return fyne.NewSize(widthPx/scale, heightPx/scale)
}

func startupWindowPreferredSize(scale float32) fyne.Size {
	maxSize := startupWindowMaxSize(scale)
	if maxSize.Width <= 0 || maxSize.Height <= 0 {
		return fyne.NewSize(0, 0)
	}

	return fyne.NewSize(
		maxSize.Width*preferredWorkAreaWidth,
		maxSize.Height*preferredWorkAreaHeight,
	)
}
