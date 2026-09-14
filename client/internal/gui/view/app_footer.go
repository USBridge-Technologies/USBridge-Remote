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

// Compact mobile pad under version/chips (Control and other tabs). Android
// reports bottom inset=0 so this sits near the screen edge.
const appFooterMobileBottomPad = float32(2)

// Connections keeps a taller footer on mobile so the version clears the
// gesture/nav buttons without enabling Fyne's system bottom safe-inset
// (which inflated every screen's chrome).
const connectionsMobileBottomPad = float32(18)

func appFooterVPads() (top, bottom float32) {
	top = 4
	bottom = 6
	if IsMobile() {
		bottom = appFooterMobileBottomPad
	}
	return
}

func appFooterLineHeight() float32 {
	if IsMobile() {
		return 1
	}
	return 0.5
}

// AppFooterOuterHeight is NewAppFooter's full strip height (row + pads +
// hairline). Native video overlays subtract this from canvasH − containerH
// when AbsolutePosition has not settled yet.
func AppFooterOuterHeight() float32 {
	top, bottom := appFooterVPads()
	return AppFooterRowHeight + top + bottom + appFooterLineHeight()
}

// NewAppFooter is the one bottom strip used on every main screen: optional
// left chips (busy spinner, script status), optional right action
// (Disconnect All), and the build version. A ColorHeaderAccentLine
// hairline sits on top -- the same stroke the app header wears underneath.
// rightBtn, spinner, and extraLeft may be nil.
func NewAppFooter(version string, rightBtn, spinner fyne.CanvasObject, extraLeft ...fyne.CanvasObject) fyne.CanvasObject {
	top, bottom := appFooterVPads()
	return newAppFooter(version, true, top, bottom, rightBtn, spinner, extraLeft...)
}

// NewConnectionsAppFooter is NewAppFooter with a taller mobile bottom pad so
// the Connections version sits above the nav/gesture area without turning
// on the system bottom safe-inset for the whole app.
func NewConnectionsAppFooter(version string, rightBtn, spinner fyne.CanvasObject, extraLeft ...fyne.CanvasObject) fyne.CanvasObject {
	top, bottom := appFooterVPads()
	if IsMobile() {
		bottom = connectionsMobileBottomPad
	}
	return newAppFooter(version, true, top, bottom, rightBtn, spinner, extraLeft...)
}

// NewAppFooterNoLine is NewAppFooter without the top hairline -- used when
// this strip sits directly under another bar that already has its own line
// (mobile Control tab footer + version).
func NewAppFooterNoLine(version string, rightBtn, spinner fyne.CanvasObject, extraLeft ...fyne.CanvasObject) fyne.CanvasObject {
	top, bottom := appFooterVPads()
	return newAppFooter(version, false, top, bottom, rightBtn, spinner, extraLeft...)
}

func newAppFooter(version string, withLine bool, top, bottom float32, rightBtn, spinner fyne.CanvasObject, extraLeft ...fyne.CanvasObject) fyne.CanvasObject {
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
	return newAppFooterStrip(NewInsetExact(container.NewMax(heightLock, row), 18, 18, top, bottom), withLine)
}

func newAppFooterStrip(inner fyne.CanvasObject, withLine bool) fyne.CanvasObject {
	// Square fill to the window's own bottom edge -- without this the
	// footer is just a hairline + inset content, and a maximized Win11
	// window shows leftover rounded "ears" of whatever sits behind it.
	bg := canvas.NewRectangle(design.ColorGray950)
	bg.CornerRadius = 0
	if !withLine {
		return container.NewStack(bg, inner)
	}
	accentLine := canvas.NewRectangle(design.ColorHeaderAccentLine)
	accentLine.SetMinSize(fyne.NewSize(1, appFooterLineHeight()))
	return container.NewStack(bg, NewTopLine(inner, accentLine))
}
