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
	"image/color"

	"usbridge-client/internal/gui/assets"
	"usbridge-client/internal/gui/design"
	"usbridge-client/internal/gui/view"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
)

// statusBarIconBoxSize is every icon button's own clickable box inside this
// strip (video + peripherals) -- smaller than headerCompactButtonSize (28,
// shared with the connections header's own buttons) specifically so each
// icon isn't touching its own box's edge, which read as "stuck to the
// frame" once this strip got its own visible border. 22 + the strip's own
// 3px top/bottom padding (statusIndicatorBarPadY) lands back on the same
// 28px total row height as before.
var statusBarIconBoxSize = fyne.NewSize(22, 22)

const (
	statusIndicatorBarPadX     = float32(10)
	statusIndicatorBarPadY     = float32(3)
	statusIndicatorBarGap      = float32(8)
	statusIndicatorGroupGap    = float32(4)
	statusIndicatorDividerH    = float32(16)
	statusIndicatorDotSize     = float32(3)
	statusIndicatorFPSTextSize = float32(11)
	// statusBarIconHoverRadius is deliberately smaller than design.RadiusMD
	// (8, the app's usual chip/panel radius) -- at RadiusMD a 22px icon's
	// hover highlight reads as too rounded for its size.
	statusBarIconHoverRadius = float32(4)
	// statusBarPeripheralIconSize overrides theme.SizeNameInlineIcon (via
	// statusBarPeripheralTheme below) for every plain widget.Button icon in
	// mw.statusPanel, matching mw.videoIcon/mw.audioIcon's own explicit
	// SetIconSize(14, 14).
	statusBarPeripheralIconSize = float32(14)
)

// statusBarPeripheralTheme is scoped to mw.statusPanel only (via
// container.NewThemeOverride in buildStatusIndicatorBar), so it doesn't
// touch any other button's icon size or hover color in the app. Only
// mw.scriptIcon is still a plain widget.Button by the time this runs --
// audio/keyboard/mouse/rndis moved to headerStatusBadgeButton (their own
// SetIconSize/SetHoverStyle/SetHoverIcon), so this override is now mostly
// redundant for them (harmless -- headerStatusBadgeButton doesn't read
// theme.SizeNameInlineIcon or ColorNameHover at all):
//   - theme.SizeNameInlineIcon -> 14px, matching mw.scriptIcon's own size
//     (plain widget.Button has no per-instance equivalent setter).
//   - theme.SizeNameInputRadius -> statusBarIconHoverRadius, the corner
//     radius Fyne's own button renderer uses for its hover-highlight rect.
//   - theme.ColorNameButton -> transparent, theme.ColorNameHover ->
//     design.ColorStatusBarIconChip: LowImportance buttons (every icon
//     here) are already background-less at rest and only paint
//     ColorNameButton blended with ColorNameHover while actually hovered
//     (see fyne's widget/button.go buttonColorNames) -- so this makes that
//     hover highlight design.ColorStatusBarIconChip instead of the app's
//     default translucent-white hover, matching mw.videoIcon/mw.audioIcon's
//     own SetHoverStyle below. There is no persistent background at rest.
//
// This was suspected (wrongly) of inflating the header's height and briefly
// removed -- the real cause was view.NewInset silently adding Fyne's own
// theme.Padding() on top of every explicit inset (see view.NewInset's own
// doc comment). Restored once that was fixed.
type statusBarPeripheralTheme struct {
	fyne.Theme
}

func (t *statusBarPeripheralTheme) Size(name fyne.ThemeSizeName) float32 {
	switch name {
	case theme.SizeNameInlineIcon:
		return statusBarPeripheralIconSize
	case theme.SizeNameInputRadius:
		return statusBarIconHoverRadius
	}
	return t.Theme.Size(name)
}

func (t *statusBarPeripheralTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	switch name {
	case theme.ColorNameButton:
		return color.Transparent
	case theme.ColorNameHover:
		return design.ColorStatusBarIconChip
	}
	return t.Theme.Color(name, variant)
}

// newStatusBarDivider is the thin vertical rule between this strip's
// video/peripherals/storage groups.
func newStatusBarDivider() fyne.CanvasObject {
	line := canvas.NewRectangle(design.ColorStatusBarDivider)
	return container.NewGridWrap(fyne.NewSize(1, statusIndicatorDividerH), line)
}

// newStatusBarDot is the small separator dot between the video group's fps
// and resolution text (e.g. "89 FPS · 1280 x 720").
func newStatusBarDot() fyne.CanvasObject {
	dot := canvas.NewRectangle(design.ColorStatusBarDivider)
	dot.CornerRadius = statusIndicatorDotSize / 2
	return container.NewGridWrap(fyne.NewSize(statusIndicatorDotSize, statusIndicatorDotSize), container.NewCenter(dot))
}

// newFixedWidthFPSText wraps mw.videoFPSText in a box wide enough for the
// worst case ("888 FPS", 3 digits) so the group's own gap/dot/resolution
// text after it don't shift left/right every time the fps digit count
// changes (1 -> 2 -> 3 digits as the stream ramps up).
func newFixedWidthFPSText(text *canvas.Text) fyne.CanvasObject {
	sample := canvas.NewText("888 FPS", text.Color)
	sample.TextSize = text.TextSize
	sampleSize := sample.MinSize()
	return container.NewGridWrap(sampleSize, text)
}

// syncStorageChipVisibility shows/hides mw.sdStorageProgress and the
// divider right before it together -- callers that used to just call
// mw.sdStorageProgress.Show()/Hide() directly (main_window_layout.go) now
// go through this instead, so the divider never gets left dangling when
// there's no SD card to show (e.g. an agent connection with none).
func (mw *MainWindow) syncStorageChipVisibility(visible bool) {
	if mw.sdStorageProgress == nil {
		return
	}
	if visible {
		mw.sdStorageProgress.Show()
	} else {
		mw.sdStorageProgress.Hide()
	}
	if mw.statusBarStorageDivider != nil {
		if visible {
			mw.statusBarStorageDivider.Show()
		} else {
			mw.statusBarStorageDivider.Hide()
		}
	}
}

// syncStatusBarDividers shows the divider between the video group and the
// peripherals group only when both sides actually have something to
// separate -- called at the end of updateStatusBarUI, after every icon's
// own show/hide for this tick has already been applied.
func (mw *MainWindow) syncStatusBarDividers() {
	if mw.statusBarIndicatorsDivider != nil {
		buttonsVisible := mw.statusBarButtonsGroup != nil && hasVisibleContent(mw.statusBarButtonsGroup)
		indicatorsVisible := mw.statusBarIndicatorsGroup != nil && hasVisibleContent(mw.statusBarIndicatorsGroup)
		if buttonsVisible && indicatorsVisible {
			mw.statusBarIndicatorsDivider.Show()
		} else {
			mw.statusBarIndicatorsDivider.Hide()
		}
	}

	if mw.statusBarPeripheralsDivider == nil {
		return
	}
	videoVisible := mw.videoStatusGroup != nil && hasVisibleContent(mw.videoStatusGroup)
	peripheralsVisible := mw.statusPanel != nil && hasVisibleContent(mw.statusPanel)
	if videoVisible && peripheralsVisible {
		mw.statusBarPeripheralsDivider.Show()
	} else {
		mw.statusBarPeripheralsDivider.Hide()
	}
}

// buildStatusIndicatorBar assembles the bordered strip itself. mw.videoIcon,
// mw.statusPanel and mw.sdStorageProgress must already exist (built by
// createStatusBar) before this is called.
func (mw *MainWindow) buildStatusIndicatorBar() fyne.CanvasObject {
	mw.videoIcon.SetHoverStyle(design.ColorStatusBarIconChip, statusBarIconHoverRadius)
	mw.videoIcon.SetHoverIcon(assets.CameraIconStatusBarHover)
	mw.audioIcon.SetHoverStyle(design.ColorStatusBarIconChip, statusBarIconHoverRadius)
	mw.audioIcon.SetHoverIcon(assets.AudioIconStatusBarHover)
	mw.keyboardIcon.SetHoverStyle(design.ColorStatusBarIconChip, statusBarIconHoverRadius)
	mw.keyboardIcon.SetHoverIcon(assets.KeyboardIconStatusBarHover)
	mw.mouseIcon.SetHoverStyle(design.ColorStatusBarIconChip, statusBarIconHoverRadius)
	mw.mouseIcon.SetHoverIcon(assets.MouseIconStatusBarHover)
	mw.rndisIcon.SetHoverStyle(design.ColorStatusBarIconChip, statusBarIconHoverRadius)
	mw.rndisIcon.SetHoverIcon(assets.NetworkIconStatusBarHover)
	mw.fullscreenIcon.SetHoverStyle(design.ColorStatusBarIconChip, statusBarIconHoverRadius)
	mw.fullscreenIcon.SetHoverIcon(assets.FullscreenIconStatusBarHover)

	mw.videoFPSText = canvas.NewText("", design.ColorStatusBarAccent)
	mw.videoFPSText.TextSize = statusIndicatorFPSTextSize
	mw.videoResolutionText = canvas.NewText("", design.ColorStatusBarResolutionText)
	mw.videoResolutionText.TextSize = statusIndicatorFPSTextSize

	mw.videoStatusGroup = container.New(&centeredInlineLayout{gap: statusIndicatorGroupGap, minGap: 2},
		container.NewGridWrap(statusBarIconBoxSize, mw.videoIcon),
		newFixedWidthFPSText(mw.videoFPSText),
		newStatusBarDot(),
		mw.videoResolutionText,
		container.NewGridWrap(statusBarIconBoxSize, mw.fullscreenIcon),
	)
	mw.videoStatusGroup.Hide()

	peripheralsSized := container.NewThemeOverride(mw.statusPanel, &statusBarPeripheralTheme{Theme: design.NewBrandTheme()})

	// statusBarPeripheralsDivider sits between the video group and the
	// peripherals group -- hidden whenever either side is empty (see
	// syncStatusBarDividers) so it never appears with nothing to separate
	// on one side (e.g. video off, or every peripheral disconnected).
	mw.statusBarPeripheralsDivider = newStatusBarDivider()
	mw.statusBarPeripheralsDivider.Hide()

	// statusBarStorageDivider sits only between the peripherals group and
	// mw.sdStorageProgress -- hidden together with it (see
	// syncStorageChipVisibility) so an agent connection with no SD card at
	// all doesn't leave a dangling divider with nothing after it.
	mw.statusBarStorageDivider = newStatusBarDivider()
	mw.statusBarStorageDivider.Hide()

	content := container.New(&centeredInlineLayout{gap: statusIndicatorBarGap, minGap: 4},
		mw.videoStatusGroup,
		mw.statusBarPeripheralsDivider,
		peripheralsSized,
		mw.statusBarStorageDivider,
		mw.sdStorageProgress,
	)

	bg := canvas.NewRectangle(design.ColorStatusBarFill)
	bg.StrokeColor = design.ColorStatusBarBorder
	bg.StrokeWidth = 1
	bg.CornerRadius = design.RadiusMD

	return container.NewStack(bg, view.NewInsetExact(content, statusIndicatorBarPadX, statusIndicatorBarPadX, statusIndicatorBarPadY, statusIndicatorBarPadY))
}
