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
	"fmt"
	"image/color"
	"sort"

	"usbridge-client/internal/gui/assets"
	"usbridge-client/internal/gui/design"
	"usbridge-client/internal/gui/view"
	"usbridge-client/internal/models"

	"github.com/sirupsen/logrus"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
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

// newFixedWidthFPSText sizes a box wide enough for the worst case fps text
// ("888 FPS", 3 digits) so the group's own gap/dot/resolution text after it
// don't shift left/right every time the fps digit count changes (1 -> 2 ->
// 3 digits as the stream ramps up). sample supplies the color/size to
// measure against; wrapped is what actually goes in the box -- now that
// the fps text is clickable (see statusBarTextButton below), that's the
// button wrapping it, not the bare *canvas.Text itself.
func newFixedWidthFPSText(sample *canvas.Text, wrapped fyne.CanvasObject) fyne.CanvasObject {
	measure := canvas.NewText("888 FPS", sample.Color)
	measure.TextSize = sample.TextSize
	return container.NewGridWrap(measure.MinSize(), wrapped)
}

// statusBarTextButton is a plain canvas.Text with tap and the same hover
// chip as this strip's icon buttons (design.ColorStatusBarIconChip, no
// background at rest) -- the video group's fps/resolution labels, tappable
// to open a quick value picker (showVideoFPSMenu/showVideoResolutionMenu)
// styled like every other header dropdown (view.ShowStyledMenuTeal).
type statusBarTextButton struct {
	widget.BaseWidget
	label    *canvas.Text
	onTapped func()
	hovered  bool
	bg       *canvas.Rectangle
}

func newStatusBarTextButton(label *canvas.Text, onTapped func()) *statusBarTextButton {
	b := &statusBarTextButton{label: label, onTapped: onTapped}
	b.ExtendBaseWidget(b)
	return b
}

func (b *statusBarTextButton) Tapped(*fyne.PointEvent) {
	if b.onTapped != nil {
		b.onTapped()
	}
}

func (b *statusBarTextButton) TappedSecondary(*fyne.PointEvent) {}

func (b *statusBarTextButton) MouseIn(*desktop.MouseEvent) {
	b.hovered = true
	b.Refresh()
}

func (b *statusBarTextButton) MouseMoved(*desktop.MouseEvent) {}

func (b *statusBarTextButton) MouseOut() {
	b.hovered = false
	b.Refresh()
}

func (b *statusBarTextButton) CreateRenderer() fyne.WidgetRenderer {
	b.bg = canvas.NewRectangle(color.Transparent)
	b.bg.CornerRadius = statusBarIconHoverRadius
	r := &statusBarTextButtonRenderer{btn: b, objects: []fyne.CanvasObject{b.bg, b.label}}
	r.Refresh()
	return r
}

type statusBarTextButtonRenderer struct {
	btn     *statusBarTextButton
	objects []fyne.CanvasObject
}

func (r *statusBarTextButtonRenderer) Layout(size fyne.Size) {
	r.btn.bg.Resize(size)
	r.btn.bg.Move(fyne.NewPos(0, 0))
	labelMin := r.btn.label.MinSize()
	r.btn.label.Resize(labelMin)
	r.btn.label.Move(fyne.NewPos((size.Width-labelMin.Width)/2, (size.Height-labelMin.Height)/2))
}

func (r *statusBarTextButtonRenderer) MinSize() fyne.Size {
	return r.btn.label.MinSize()
}

func (r *statusBarTextButtonRenderer) Refresh() {
	r.btn.bg.FillColor = color.Transparent
	if r.btn.hovered {
		r.btn.bg.FillColor = design.ColorStatusBarIconChip
	}
	r.btn.bg.Refresh()
	canvas.Refresh(r.btn.label)
}

func (r *statusBarTextButtonRenderer) Objects() []fyne.CanvasObject {
	return r.objects
}

func (r *statusBarTextButtonRenderer) Destroy() {}

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

	// fps/resolution are both tappable now -- each opens a quick picker
	// (styled like every other header dropdown, view.ShowStyledMenuTeal)
	// sourced from the same capture-mode data the video settings dialog
	// itself uses (see showVideoFPSMenu/showVideoResolutionMenu), applying
	// the change directly without opening that dialog.
	var fpsBtn, resBtn *statusBarTextButton
	fpsBtn = newStatusBarTextButton(mw.videoFPSText, func() {
		mw.showVideoFPSMenu(fpsBtn)
	})
	resBtn = newStatusBarTextButton(mw.videoResolutionText, func() {
		mw.showVideoResolutionMenu(resBtn)
	})

	mw.videoStatusGroup = container.New(&centeredInlineLayout{gap: statusIndicatorGroupGap, minGap: 2},
		container.NewGridWrap(statusBarIconBoxSize, mw.videoIcon),
		newFixedWidthFPSText(mw.videoFPSText, fpsBtn),
		newStatusBarDot(),
		resBtn,
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

// showVideoFPSMenu opens a teal dropdown (view.ShowStyledMenuTeal, the same
// style every other header menu uses) listing the fps values the current
// resolution actually supports, sourced from VideoWidget.AvailableCaptureModes
// -- the same capture-mode data the video settings dialog uses, without
// opening that dialog. Selecting one calls VideoWidget.ApplyVideoFPS
// directly, keeping every other current setting untouched.
func (mw *MainWindow) showVideoFPSMenu(anchor fyne.CanvasObject) {
	if mw.videoWidget == nil || anchor == nil {
		return
	}
	go func() {
		modes, cfg, err := mw.videoWidget.AvailableCaptureModes()
		if err != nil {
			logrus.Warnf("⚠️ cannot load fps options: %v", err)
			return
		}
		fpsOptions := captureModeFPS(modes, cfg.VideoWidth, cfg.VideoHeight)
		if len(fpsOptions) == 0 {
			return
		}
		fyne.Do(func() {
			items := make([]view.StyledMenuItem, 0, len(fpsOptions))
			for _, fps := range fpsOptions {
				fps := fps
				items = append(items, view.StyledMenuItem{
					Label:    fmt.Sprintf("%d FPS", fps),
					Selected: fps == cfg.VideoFPS,
					OnTap: func() {
						go func() {
							if err := mw.videoWidget.ApplyVideoFPS(fps); err != nil {
								logrus.Warnf("⚠️ failed to apply fps %d from header menu: %v", fps, err)
							}
						}()
					},
				})
			}
			view.ShowStyledMenuTeal(anchor, items)
		})
	}()
}

// showVideoResolutionMenu is showVideoFPSMenu's own counterpart for
// resolution -- every distinct width/height AvailableCaptureModes reports,
// applied via VideoWidget.ApplyVideoResolution.
func (mw *MainWindow) showVideoResolutionMenu(anchor fyne.CanvasObject) {
	if mw.videoWidget == nil || anchor == nil {
		return
	}
	go func() {
		modes, cfg, err := mw.videoWidget.AvailableCaptureModes()
		if err != nil {
			logrus.Warnf("⚠️ cannot load resolution options: %v", err)
			return
		}
		options := distinctCaptureResolutions(modes)
		if len(options) == 0 {
			return
		}
		fyne.Do(func() {
			items := make([]view.StyledMenuItem, 0, len(options))
			for _, opt := range options {
				width, height := opt.width, opt.height
				items = append(items, view.StyledMenuItem{
					Label:    fmt.Sprintf("%d x %d", width, height),
					Selected: width == cfg.VideoWidth && height == cfg.VideoHeight,
					OnTap: func() {
						go func() {
							if err := mw.videoWidget.ApplyVideoResolution(width, height); err != nil {
								logrus.Warnf("⚠️ failed to apply resolution %dx%d from header menu: %v", width, height, err)
							}
						}()
					},
				})
			}
			view.ShowStyledMenuTeal(anchor, items)
		})
	}()
}

// maxSelectableFPS mirrors video_start_dialog.go's own refreshFPSOptions:
// a server-reported capture mode can list far higher values (e.g. a
// virtual/display capture reporting 240) than the encode pipeline can
// actually sustain at this resolution/bitrate -- picking one that high
// overloads the hardware encoder (multi-hundred-ms keyframe stalls,
// IDR-request storms) and can drive the session into a disconnect/
// reconnect loop that never recovers, since the request stays the same on
// every automatic retry. Kept in sync with that dialog's own constant by
// hand since the two menus are built independently (this header dropdown
// deliberately doesn't go through that dialog at all -- see
// VideoWidget.AvailableCaptureModes' own doc comment).
const maxSelectableFPS = 120

// captureModeFPS returns the fps list for whichever mode in modes matches
// width/height (capped at maxSelectableFPS), falling back to the first
// mode's own list if none match (e.g. the current resolution isn't itself
// one of the reported modes).
func captureModeFPS(modes []models.VideoCaptureMode, width, height int) []int {
	var all []int
	for _, m := range modes {
		if m.Width == width && m.Height == height {
			all = m.FPS
			break
		}
	}
	if all == nil && len(modes) > 0 {
		all = modes[0].FPS
	}

	fps := make([]int, 0, len(all))
	for _, f := range all {
		if f <= maxSelectableFPS {
			fps = append(fps, f)
		}
	}
	return fps
}

type captureResolution struct {
	width, height int
}

// distinctCaptureResolutions dedupes modes down to one entry per distinct
// width/height (the same capture card can list a resolution once per pixel
// format -- see video_start_dialog.go's own Configure, which keeps those as
// separate rows for its format picker; this menu only offers a resolution
// choice, so one row per size is enough), sorted smallest to largest.
func distinctCaptureResolutions(modes []models.VideoCaptureMode) []captureResolution {
	seen := make(map[captureResolution]bool, len(modes))
	options := make([]captureResolution, 0, len(modes))
	for _, m := range modes {
		key := captureResolution{m.Width, m.Height}
		if seen[key] {
			continue
		}
		seen[key] = true
		options = append(options, key)
	}
	sort.Slice(options, func(i, j int) bool {
		areaI := options[i].width * options[i].height
		areaJ := options[j].width * options[j].height
		if areaI != areaJ {
			return areaI < areaJ
		}
		return options[i].width < options[j].width
	})
	return options
}
