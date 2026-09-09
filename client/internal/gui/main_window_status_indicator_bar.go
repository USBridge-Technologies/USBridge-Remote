package gui

// main_window_status_indicator_bar.go -- the Control header's bordered
// video/peripherals/storage strip: a single rounded, bordered container
// (design.ColorStatusBarBorder/Fill) holding, left to right, the video icon
// + fps + a dot + the capture resolution, a thin divider, the peripheral
// status icons (mw.statusPanel), another divider, and the SD storage chip
// (mw.sdStorageProgress). Replaces the old plain mw.sdStorageProgress +
// mw.statusPanel pairing that used to sit directly in createMainAddressBar's
// middleGroup with no shared background/border of its own.

import (
	"usbridge-client/internal/gui/design"
	"usbridge-client/internal/gui/view"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
)

const (
	// statusIndicatorBarPadY is 0, not a few px like most other chips in
	// this header -- this strip's tallest child (the 28px video-icon
	// GridWrap, headerCompactButtonSize) already matches the Control
	// header's own overall target height (28px, same as the connections
	// screen's header -- see createMainAddressBar/headerCompactButtonSize's
	// own doc comments for why that number is load-bearing); any added
	// vertical padding here would push this row taller than that again.
	statusIndicatorBarPadX     = float32(10)
	statusIndicatorBarPadY     = float32(0)
	statusIndicatorBarGap      = float32(8)
	statusIndicatorGroupGap    = float32(4)
	statusIndicatorDividerH    = float32(16)
	statusIndicatorDotSize     = float32(3)
	statusIndicatorFPSTextSize = float32(11)
)

// newStatusBarDivider is the thin vertical rule between this strip's
// video/peripherals/storage groups.
func newStatusBarDivider() fyne.CanvasObject {
	line := canvas.NewRectangle(design.ColorStatusBarDivider)
	return container.NewGridWrap(fyne.NewSize(1, statusIndicatorDividerH), line)
}

// newStatusBarDot is the small separator dot between the video group's fps
// and resolution text (e.g. "89 FPS · 1080p@60Hz").
func newStatusBarDot() fyne.CanvasObject {
	dot := canvas.NewRectangle(design.ColorStatusBarDivider)
	dot.CornerRadius = statusIndicatorDotSize / 2
	return container.NewGridWrap(fyne.NewSize(statusIndicatorDotSize, statusIndicatorDotSize), container.NewCenter(dot))
}

// buildStatusIndicatorBar assembles the bordered strip itself. mw.videoIcon,
// mw.statusPanel and mw.sdStorageProgress must already exist (built by
// createStatusBar) before this is called.
func (mw *MainWindow) buildStatusIndicatorBar() fyne.CanvasObject {
	mw.videoFPSText = canvas.NewText("", design.ColorStatusBarAccent)
	mw.videoFPSText.TextSize = statusIndicatorFPSTextSize
	mw.videoResolutionText = canvas.NewText("", design.ColorStatusBarResolutionText)
	mw.videoResolutionText.TextSize = statusIndicatorFPSTextSize

	mw.videoStatusGroup = container.New(&centeredInlineLayout{gap: statusIndicatorGroupGap, minGap: 2},
		container.NewGridWrap(headerCompactButtonSize, mw.videoIcon),
		mw.videoFPSText,
		newStatusBarDot(),
		mw.videoResolutionText,
	)
	mw.videoStatusGroup.Hide()

	content := container.New(&centeredInlineLayout{gap: statusIndicatorBarGap, minGap: 4},
		mw.videoStatusGroup,
		newStatusBarDivider(),
		mw.statusPanel,
		newStatusBarDivider(),
		mw.sdStorageProgress,
	)

	bg := canvas.NewRectangle(design.ColorStatusBarFill)
	bg.StrokeColor = design.ColorStatusBarBorder
	bg.StrokeWidth = 1
	bg.CornerRadius = design.RadiusMD

	return container.NewStack(bg, view.NewInset(content, statusIndicatorBarPadX, statusIndicatorBarPadX, statusIndicatorBarPadY, statusIndicatorBarPadY))
}
