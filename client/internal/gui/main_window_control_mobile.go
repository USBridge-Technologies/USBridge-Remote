package gui

import (
	"image/color"

	"usbridge-client/internal/gui/assets"
	"usbridge-client/internal/gui/design"
	"usbridge-client/internal/gui/view"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
)

// useMobileControl is the Control/Devices/Snapshots/Scripts chrome fork:
// real phones and the desktop phone preview. Desktop header stays labeled.
func useMobileControl() bool {
	return view.IsMobile()
}

func mobileControlTabGaps() (gap, minGap float32) {
	if useMobileControl() {
		return 10, 6
	}
	return 16, 8
}

func scriptsTabLabel() string {
	return "AI & Scripts"
}

func (mw *MainWindow) createMobileConnectedFooter(tabs fyne.CanvasObject) fyne.CanvasObject {
	const kbSize float32 = 32
	kb := newHeaderStatusBadgeButton(assets.KeyboardIcon, func() {
		if mw.videoWidget != nil {
			mw.videoWidget.HandleVirtualKeyboard()
		}
		mw.syncMobileKeyboardToggleLook()
	})
	kb.SetIconSize(fyne.NewSize(16, 16))
	kb.SetBadgeText("")
	kb.SetHoverStyle(design.ColorAlphaWhite07, kbSize/2)
	kb.SetSelectedStyle(design.ColorAlphaWhite12, assets.KeyboardIconFooterActive)
	mw.mobileKeyboardToggle = kb
	mw.mobileKeyboardBtn = container.NewGridWrap(fyne.NewSize(kbSize, kbSize), kb)

	// Keyboard overlays the right edge and is not in the tabs' layout, so
	// the four tabs stay centered in the full bar whether the key is shown.
	kbLayer := container.NewBorder(nil, nil, nil, mw.mobileKeyboardBtn, nil)
	row := container.NewStack(tabs, kbLayer)
	heightLock := canvas.NewRectangle(color.Transparent)
	heightLock.SetMinSize(fyne.NewSize(0, 52))
	inner := view.NewInsetExact(container.NewMax(heightLock, row), 8, 8, 6, 10)

	accent := canvas.NewRectangle(design.ColorHeaderAccentLine)
	accent.SetMinSize(fyne.NewSize(1, 1))
	bg := canvas.NewRectangle(design.ColorGray950)
	bg.CornerRadius = 0
	return container.NewStack(bg, view.NewTopLine(inner, accent))
}

func (mw *MainWindow) syncMobileKeyboardButton(controlActive bool) {
	if mw.mobileKeyboardToggle == nil {
		return
	}
	if controlActive {
		mw.mobileKeyboardToggle.Show()
	} else {
		mw.mobileKeyboardToggle.Hide()
		if mw.videoWidget != nil && mw.videoWidget.IsVirtualKeyboardVisible() {
			mw.videoWidget.HandleVirtualKeyboard()
		}
	}
	mw.syncMobileKeyboardToggleLook()
}

func (mw *MainWindow) syncMobileKeyboardToggleLook() {
	if mw.mobileKeyboardToggle == nil {
		return
	}
	on := mw.videoWidget != nil && mw.videoWidget.IsVirtualKeyboardVisible()
	mw.mobileKeyboardToggle.SetSelected(on)
}
