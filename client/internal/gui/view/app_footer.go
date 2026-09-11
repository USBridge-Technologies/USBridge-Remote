package view

import (
	"image/color"
	"strings"

	"usbridge-client/internal/gui/design"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
)

// AppFooterRowHeight is the locked content height of the shared window
// footer (Connections, Control, Devices, Snapshots, Scripts). Spinner and
// script chips are 14px; without the lock the strip jumps when a hint
// appears or disappears.
const AppFooterRowHeight = float32(14)

// AppFooterOuterHeight is NewAppFooter's full strip: the 14px row, 4/6
// insets, and the 0.5px hairline. Native video overlays subtract this
// from canvasH − containerH when AbsolutePosition has not settled yet.
const AppFooterOuterHeight = AppFooterRowHeight + 4 + 6 + 0.5

// NewAppFooter is the one bottom strip used on every main screen: optional
// left chips (busy spinner, script status), optional right action
// (Disconnect All), and the build version. A ColorHeaderAccentLine
// hairline sits on top -- the same stroke the app header wears underneath.
// rightBtn, spinner, and extraLeft may be nil.
func NewAppFooter(version string, rightBtn, spinner fyne.CanvasObject, extraLeft ...fyne.CanvasObject) fyne.CanvasObject {
	leftParts := make([]fyne.CanvasObject, 0, 1+len(extraLeft))
	if usableCanvasObject(spinner) {
		leftParts = append(leftParts, spinner)
	}
	for _, extra := range extraLeft {
		if usableCanvasObject(extra) {
			leftParts = append(leftParts, extra)
		}
	}
	var left fyne.CanvasObject
	if len(leftParts) > 0 {
		left = container.New(&DeviceRowControlsLayout{Gap: 10}, leftParts...)
	}
	var rightParts []fyne.CanvasObject
	if rightBtn != nil {
		rightParts = append(rightParts, rightBtn)
	}
	if v := strings.TrimSpace(version); v != "" {
		label := canvas.NewText("v"+v, design.ColorTextMuted)
		label.TextSize = 9
		rightParts = append(rightParts, label)
	}
	var right fyne.CanvasObject
	if len(rightParts) > 0 {
		right = container.New(&DeviceRowControlsLayout{Gap: 12}, rightParts...)
	}
	var row fyne.CanvasObject
	if left == nil && right == nil {
		row = canvas.NewRectangle(color.Transparent)
	} else {
		row = container.NewBorder(nil, nil, left, right)
	}
	heightLock := canvas.NewRectangle(color.Transparent)
	heightLock.SetMinSize(fyne.NewSize(0, AppFooterRowHeight))
	return newAppFooterStrip(NewInsetExact(container.NewMax(heightLock, row), 18, 18, 4, 6))
}

func newAppFooterStrip(inner fyne.CanvasObject) fyne.CanvasObject {
	accentLine := canvas.NewRectangle(design.ColorHeaderAccentLine)
	accentLine.SetMinSize(fyne.NewSize(1, 0.5))
	return NewTopLine(inner, accentLine)
}
