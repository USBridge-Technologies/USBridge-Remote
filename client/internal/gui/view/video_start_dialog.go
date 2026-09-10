package view

import (
	"fmt"
	"image/color"
	"sort"
	"strconv"
	"strings"

	"usbridge-client/internal/gui/design"
	"usbridge-client/internal/gui/i18n"
	"usbridge-client/internal/models"
	"usbridge-client/internal/service"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/sirupsen/logrus"
)

type ResolutionPreset struct {
	Label string
	Mode  models.VideoCaptureMode
}

type VideoStartDialog struct {
	dialog      *widget.PopUp
	parent      fyne.Window
	dialogShown bool // tracks whether overlayShow was called (guards against double-hide)

	streamModes      []models.VideoTransportMode
	captureModes     []models.VideoCaptureMode
	resolutionLabels map[string]models.VideoCaptureMode
	resolutionHints  map[string]string

	currentModeID     string
	modeButtons       map[string]*videoCodecButton
	modeButtonsRow    *fyne.Container
	modeDescription   *canvas.Text
	resolutionSelect  *HeaderDropdown
	resolutionMeta    *canvas.Text
	fpsSelect         *HeaderDropdown
	fpsMeta           *canvas.Text
	bitrateSlider     *widget.Slider
	bitrateValueLabel *widget.Label
	bitrateBlock      *fyne.Container
	bitrateHintsRow   *fyne.Container
	modeDetailsSlot   *fyne.Container
	jpegHint          *widget.Label
	deviceLabel       *widget.Label
	vsyncCheck        *widget.Check
	aiVisionCheck     *widget.Check
	aiVisionHint      *widget.Label
	// color444Check/color444Hint: the RustShine Pro 4:4:4 color upgrade,
	// placed right below the resolution picker and above the AI Vision
	// checkbox. Only shown when the currently selected codec is H.265 AND
	// the agent's own video-info response says color444Available (hardware
	// probe AND license tier, see models.VideoStatus.Color444Available's
	// doc comment) -- see refreshModeUI.
	color444Check     *widget.Check
	color444Hint      *widget.Label
	color444Available bool

	startBtn  *videoDialogPillButton
	cancelBtn *videoDialogPillButton
	extraBtn  *videoDialogPillButton

	onApply func(request *models.VideoStartRequest)

	// liveCodecProvider, when set, returns the codec actually negotiated by
	// the running Moonlight session (ok=false if no session is active). It
	// is authoritative and takes priority over the agent's pre-connection
	// guess in info.Encoding — see Configure().
	liveCodecProvider func() (codec string, ok bool)
}

// SetLiveCodecProvider wires a callback that reports the codec truly
// negotiated by the active Moonlight session (e.g.
// service.VideoClient.NegotiatedVideoCodecName). Configure() prefers this
// over the agent's best-effort guess whenever it reports a value, since it
// reflects what the server actually accepted, not what was requested.
func (vsd *VideoStartDialog) SetLiveCodecProvider(provider func() (string, bool)) {
	vsd.liveCodecProvider = provider
}

type videoCodecButtonsLayout struct {
	gap float32
}

type bitrateBlockLayout struct {
	headerHeight float32
	sliderTop    float32
	sliderHeight float32
	hintsTop     float32
}

type videoCodecButton struct {
	widget.BaseWidget

	text    string
	active  bool
	hovered bool
	onTap   func()

	bg    *canvas.Rectangle
	label *canvas.Text
}

func (l *videoCodecButtonsLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) == 0 {
		return
	}

	totalGap := l.gap * float32(len(objects)-1)
	itemWidth := (size.Width - totalGap) / float32(len(objects))
	if itemWidth < 0 {
		itemWidth = 0
	}

	x := float32(0)
	for _, object := range objects {
		object.Move(fyne.NewPos(x, 0))
		object.Resize(fyne.NewSize(itemWidth, size.Height))
		x += itemWidth + l.gap
	}
}

func (l *videoCodecButtonsLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	if len(objects) == 0 {
		return fyne.NewSize(0, 0)
	}

	width := float32(0)
	height := float32(0)
	for i, object := range objects {
		min := object.MinSize()
		width += min.Width
		if i > 0 {
			width += l.gap
		}
		if min.Height > height {
			height = min.Height
		}
	}

	return fyne.NewSize(width, height)
}

func (l *bitrateBlockLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) < 3 {
		return
	}

	header := objects[0]
	slider := objects[1]
	hints := objects[2]

	header.Move(fyne.NewPos(0, 0))
	header.Resize(fyne.NewSize(size.Width, l.headerHeight))

	slider.Move(fyne.NewPos(0, l.sliderTop))
	slider.Resize(fyne.NewSize(size.Width, l.sliderHeight))

	hintsHeight := size.Height - l.hintsTop
	if hintsHeight < 0 {
		hintsHeight = 0
	}
	hints.Move(fyne.NewPos(0, l.hintsTop))
	hints.Resize(fyne.NewSize(size.Width, hintsHeight))
}

func (l *bitrateBlockLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	if len(objects) < 3 {
		return fyne.NewSize(0, 0)
	}

	headerMin := objects[0].MinSize()
	sliderMin := objects[1].MinSize()
	hintsMin := objects[2].MinSize()

	width := headerMin.Width
	if sliderMin.Width > width {
		width = sliderMin.Width
	}
	if hintsMin.Width > width {
		width = hintsMin.Width
	}

	height := l.hintsTop + hintsMin.Height
	minHeight := l.sliderTop + sliderMin.Height
	if minHeight > height {
		height = minHeight
	}

	return fyne.NewSize(width, height)
}

func newVideoCodecButton(text string, onTap func()) *videoCodecButton {
	button := &videoCodecButton{text: text, onTap: onTap}
	button.ExtendBaseWidget(button)
	return button
}

func (b *videoCodecButton) SetActive(active bool) {
	if b.active == active {
		return
	}
	b.active = active
	b.refreshVisuals()
}

func (b *videoCodecButton) Tapped(*fyne.PointEvent) {
	if b.onTap != nil {
		b.onTap()
	}
}

func (b *videoCodecButton) TappedSecondary(*fyne.PointEvent) {}

func (b *videoCodecButton) MouseIn(*desktop.MouseEvent) {
	b.hovered = true
	b.refreshVisuals()
}

func (b *videoCodecButton) MouseMoved(*desktop.MouseEvent) {}

func (b *videoCodecButton) MouseOut() {
	b.hovered = false
	b.refreshVisuals()
}

func (b *videoCodecButton) Cursor() desktop.Cursor {
	return desktop.PointerCursor
}

func (b *videoCodecButton) MinSize() fyne.Size {
	return fyne.NewSize(90, 36)
}

func (b *videoCodecButton) CreateRenderer() fyne.WidgetRenderer {
	b.bg = canvas.NewRectangle(design.ColorSurfaceLight)
	b.bg.CornerRadius = design.RadiusMD
	b.bg.StrokeWidth = 1

	b.label = canvas.NewText(b.text, design.ColorTextLight)
	b.label.TextSize = 13
	b.label.TextStyle.Bold = true
	b.label.Alignment = fyne.TextAlignCenter

	b.refreshVisuals()
	return widget.NewSimpleRenderer(container.NewMax(b.bg, container.NewCenter(b.label)))
}

func (b *videoCodecButton) refreshVisuals() {
	if b.bg == nil || b.label == nil {
		return
	}

	if b.active {
		// Selected codec reads like the dialog's own teal Apply button
		// (design.ColorConnectionBadgeText fill, dark text) -- ties codec
		// selection to the same accent color used elsewhere in this dialog.
		b.bg.FillColor = design.ColorConnectionBadgeText
		b.bg.StrokeColor = color.Transparent
		b.label.Color = design.ColorGray950
	} else {
		// Unselected look matches the Add Connection dialog's Scan QR/Paste
		// Link pills: transparent fill, muted border, hover fills dark gray
		// and the border lights up teal.
		b.bg.FillColor = color.Transparent
		b.bg.StrokeColor = design.ColorTailscaleChipBorder
		b.label.Color = design.ColorTextLight
		if b.hovered {
			b.bg.FillColor = color.NRGBA{R: 0x26, G: 0x2a, B: 0x2e, A: 0xff}
			b.bg.StrokeColor = design.ColorConnectionBadgeText
		}
	}

	b.bg.Refresh()
	b.label.Refresh()
}

// newVideoDialogPicker builds a small teal pill dropdown matching the
// per-connection AUTO/TS/LAN protocol picker's own look (an UltraCompact
// HeaderDropdown -- see connection_grid_card.go's protocolDropdown), reused
// here for the resolution and FPS pickers so this dialog's controls read as
// one family with the rest of the app instead of a bespoke widget.
func newVideoDialogPicker(onSelected func(string)) *HeaderDropdown {
	d := NewHeaderDropdown(nil, "", onSelected)
	d.UltraCompact = true
	d.CornerRadius = 6
	d.BorderColor = design.ColorTailscaleChipBorder
	d.TextColor = design.ColorConnectionBadgeText
	d.IconColor = color.NRGBA{R: 0xc5, G: 0xc8, B: 0xb5, A: 0xff}
	d.TextSize = 10
	d.HoverBorderColor = design.ColorConnectionBadgeText
	d.HoverFillColor = design.ColorGray900
	return d
}

// videoDialogPillButton is the small compact bordered pill button used for
// this dialog's Cancel/Apply/extra footer actions -- matching the look of
// the Add Connection dialog's own footer buttons (Cancel, Save), whose
// widget lives in the controller package and isn't exported for reuse here.
type videoDialogPillButton struct {
	widget.BaseWidget

	text     string
	OnTapped func()
	hovered  bool
	disabled bool

	fillColor         color.Color
	hoverFillColor    color.Color
	disabledFillColor color.Color
	borderColor       color.Color
	hoverBorderColor  color.Color
	textColor         color.Color
	hoverTextColor    color.Color
	disabledTextColor color.Color

	bg     *canvas.Rectangle
	border *canvas.Rectangle
	label  *canvas.Text
}

const (
	videoDialogPillTextSize = float32(9.5)
	videoDialogPillHeight   = float32(32)
	videoDialogPillPadX     = float32(15)
)

// newVideoDialogCancelButton matches the Add Connection dialog's Cancel
// button: no fill/border, muted gray text that lightens on hover.
func newVideoDialogCancelButton(text string, onTap func()) *videoDialogPillButton {
	b := &videoDialogPillButton{
		text:           text,
		OnTapped:       onTap,
		fillColor:      color.Transparent,
		borderColor:    color.Transparent,
		textColor:      color.NRGBA{R: 0x8f, G: 0x93, B: 0x81, A: 0xff},
		hoverTextColor: design.ColorTextLight,
	}
	b.ExtendBaseWidget(b)
	return b
}

// newVideoDialogApplyButton matches the Add Connection dialog's Save
// button: a solid teal pill with dark text, lightening on hover.
func newVideoDialogApplyButton(text string, onTap func()) *videoDialogPillButton {
	b := &videoDialogPillButton{
		text:           text,
		OnTapped:       onTap,
		fillColor:      design.ColorConnectionBadgeText,
		borderColor:    color.Transparent,
		textColor:      design.ColorGray950,
		hoverFillColor: color.NRGBA{R: 0x61, G: 0xf0, B: 0xd3, A: 0xff},
		hoverTextColor: design.ColorGray950,
		// Mirrors connectionDialogTealDisabled: a darker shade of this same
		// teal instead of switching to a neutral gray while disabled.
		disabledFillColor: color.NRGBA{R: 0x31, G: 0xa6, B: 0x94, A: 0xff},
		disabledTextColor: design.ColorGray950,
	}
	b.ExtendBaseWidget(b)
	return b
}

// newVideoDialogExtraButton is the optional extra footer action (see
// SetExtraAction) -- a neutral bordered pill, same border/hover treatment as
// the unselected codec buttons.
func newVideoDialogExtraButton() *videoDialogPillButton {
	b := &videoDialogPillButton{
		fillColor:        color.Transparent,
		borderColor:      design.ColorTailscaleChipBorder,
		textColor:        design.ColorTextLight,
		hoverFillColor:   color.NRGBA{R: 0x26, G: 0x2a, B: 0x2e, A: 0xff},
		hoverBorderColor: design.ColorConnectionBadgeText,
		hoverTextColor:   design.ColorTextLight,
	}
	b.ExtendBaseWidget(b)
	return b
}

func (b *videoDialogPillButton) SetText(text string) {
	b.text = text
	if b.label != nil {
		b.label.Text = text
		b.label.Refresh()
	}
	b.Refresh()
}

func (b *videoDialogPillButton) Enable() {
	if !b.disabled {
		return
	}
	b.disabled = false
	b.refreshVisuals()
}

func (b *videoDialogPillButton) Disable() {
	if b.disabled {
		return
	}
	b.disabled = true
	b.refreshVisuals()
}

func (b *videoDialogPillButton) Tapped(*fyne.PointEvent) {
	if b.disabled || b.OnTapped == nil {
		return
	}
	b.OnTapped()
}

func (b *videoDialogPillButton) TappedSecondary(*fyne.PointEvent) {}

func (b *videoDialogPillButton) MouseIn(*desktop.MouseEvent) {
	b.hovered = true
	b.refreshVisuals()
}

func (b *videoDialogPillButton) MouseMoved(*desktop.MouseEvent) {}

func (b *videoDialogPillButton) MouseOut() {
	b.hovered = false
	b.refreshVisuals()
}

func (b *videoDialogPillButton) Cursor() desktop.Cursor {
	return desktop.PointerCursor
}

func (b *videoDialogPillButton) MinSize() fyne.Size {
	measure := canvas.NewText(b.text, color.Black)
	measure.TextSize = videoDialogPillTextSize
	measure.TextStyle.Bold = true
	width := measure.MinSize().Width + videoDialogPillPadX*2
	return fyne.NewSize(width, videoDialogPillHeight)
}

func (b *videoDialogPillButton) CreateRenderer() fyne.WidgetRenderer {
	b.bg = canvas.NewRectangle(color.Transparent)
	b.bg.CornerRadius = design.RadiusMD

	b.border = canvas.NewRectangle(color.Transparent)
	b.border.CornerRadius = design.RadiusMD
	b.border.StrokeWidth = 1

	b.label = canvas.NewText(b.text, b.textColor)
	b.label.TextSize = videoDialogPillTextSize
	b.label.TextStyle.Bold = true
	b.label.Alignment = fyne.TextAlignCenter

	b.refreshVisuals()
	return widget.NewSimpleRenderer(container.NewStack(b.bg, container.NewCenter(b.label), b.border))
}

func (b *videoDialogPillButton) refreshVisuals() {
	if b.bg == nil || b.border == nil || b.label == nil {
		return
	}

	fill := b.fillColor
	border := b.borderColor
	text := b.textColor

	switch {
	case b.disabled:
		if b.disabledFillColor != nil {
			fill = b.disabledFillColor
		}
		if b.disabledTextColor != nil {
			text = b.disabledTextColor
		}
	case b.hovered:
		if b.hoverFillColor != nil {
			fill = b.hoverFillColor
		}
		if b.hoverBorderColor != nil {
			border = b.hoverBorderColor
		}
		if b.hoverTextColor != nil {
			text = b.hoverTextColor
		}
	}

	b.bg.FillColor = fill
	b.border.StrokeColor = border
	if border == nil || border == color.Transparent {
		b.border.StrokeWidth = 0
	} else {
		b.border.StrokeWidth = 1
	}
	b.label.Color = text

	b.bg.Refresh()
	b.border.Refresh()
	b.label.Refresh()
}

// videoDialogCornerButtonLayout pins its one child (the header's close X) a
// fixed offset from the panel's own top-right corner, decoupled from the
// title's own margin -- matching the Add Connection dialog's cornerBtn.
type videoDialogCornerButtonLayout struct {
	Top   float32
	Right float32
}

func (l *videoDialogCornerButtonLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) == 0 {
		return
	}
	obj := objects[0]
	min := obj.MinSize()
	obj.Resize(min)
	obj.Move(fyne.NewPos(size.Width-l.Right-min.Width, l.Top))
}

func (l *videoDialogCornerButtonLayout) MinSize([]fyne.CanvasObject) fyne.Size {
	return fyne.NewSize(0, 0)
}

// newVideoDialogTopAccentBar is the thin teal-to-lime fade hairline the Add
// Connection dialog carries along its top edge -- faded to transparent at
// both ends so it doesn't butt into the panel's rounded corners.
func newVideoDialogTopAccentBar() fyne.CanvasObject {
	teal := design.ColorConnectionBadgeText
	lime := design.ColorConnectionAddFill
	tealTransparent := color.NRGBA{R: 0x41, G: 0xe0, B: 0xc3, A: 0}
	limeTransparent := color.NRGBA{R: 0xc4, G: 0xe7, B: 0x7a, A: 0}
	accentLeftFade := canvas.NewHorizontalGradient(tealTransparent, teal)
	accentLeftFade.SetMinSize(fyne.NewSize(70, 2))
	accentRightFade := canvas.NewHorizontalGradient(lime, limeTransparent)
	accentRightFade.SetMinSize(fyne.NewSize(70, 2))
	accentMid := canvas.NewHorizontalGradient(teal, lime)
	return container.NewBorder(nil, nil, accentLeftFade, accentRightFade, accentMid)
}

func NewVideoStartDialog(parent fyne.Window) *VideoStartDialog {
	vsd := &VideoStartDialog{
		parent:           parent,
		resolutionLabels: make(map[string]models.VideoCaptureMode),
		modeButtons:      make(map[string]*videoCodecButton),
	}
	vsd.createInterface()
	return vsd
}

func (vsd *VideoStartDialog) createInterface() {
	vsd.modeDescription = canvas.NewText("", design.ColorTextMuted)
	vsd.modeDescription.TextSize = 12
	vsd.modeDescription.Alignment = fyne.TextAlignCenter
	vsd.modeButtonsRow = container.New(&videoCodecButtonsLayout{gap: 10})

	vsd.resolutionSelect = newVideoDialogPicker(func(string) {
		vsd.refreshAvailableModes()
		vsd.refreshFPSOptions()
	})
	vsd.resolutionMeta = canvas.NewText("", design.ColorTextMuted)
	vsd.resolutionMeta.TextSize = 11
	vsd.resolutionMeta.Alignment = fyne.TextAlignCenter

	vsd.fpsSelect = newVideoDialogPicker(nil)
	vsd.fpsMeta = canvas.NewText(i18n.Current.FramesPerSecond, design.ColorTextMuted)
	vsd.fpsMeta.TextSize = 11
	vsd.fpsMeta.Alignment = fyne.TextAlignCenter

	vsd.bitrateSlider = widget.NewSlider(1000, 150000)
	vsd.bitrateSlider.Step = 1000
	vsd.bitrateSlider.Value = 20000
	vsd.bitrateValueLabel = widget.NewLabel("")
	vsd.bitrateSlider.OnChanged = func(value float64) {
		vsd.bitrateValueLabel.SetText(fmt.Sprintf("%.1f %s", value/1000, i18n.Current.UnitMbps))
	}

	lowQualityLabel := canvas.NewText("Low Quality", design.ColorTextMuted)
	lowQualityLabel.TextSize = 11
	highBandwidthLabel := canvas.NewText("High Bandwidth", design.ColorTextMuted)
	highBandwidthLabel.TextSize = 11
	bitrateHintSpacer := canvas.NewRectangle(color.Transparent)
	vsd.bitrateHintsRow = NewInset(
		container.NewBorder(nil, nil, lowQualityLabel, highBandwidthLabel, bitrateHintSpacer),
		12, 12, 0, 0,
	)

	vsd.jpegHint = widget.NewLabel(i18n.Current.VideoJPEGRTPHint)
	vsd.jpegHint.Wrapping = fyne.TextWrapWord
	vsd.jpegHint.Alignment = fyne.TextAlignCenter
	vsd.deviceLabel = widget.NewLabel("")
	vsd.deviceLabel.Wrapping = fyne.TextWrapWord
	vsd.vsyncCheck = widget.NewCheck(i18n.Current.EnableVSync, nil)
	vsd.vsyncCheck.SetChecked(true)

	// AI Vision: off by default, takes effect immediately (not gated behind
	// Start/Apply) since it's a pure local-rendering overlay -- see
	// service.SetAIVisionEnabled's doc comment.
	vsd.aiVisionCheck = widget.NewCheck(i18n.Current.AIVision, func(checked bool) {
		service.SetAIVisionEnabled(checked)
	})
	vsd.aiVisionCheck.SetChecked(service.AIVisionEnabled())
	vsd.aiVisionHint = widget.NewLabel(i18n.Current.AIVisionHint)
	vsd.aiVisionHint.Wrapping = fyne.TextWrapWord
	vsd.aiVisionHint.TextStyle = fyne.TextStyle{Italic: true}

	// RustShine Pro 4:4:4 color: off by default, takes effect on the next
	// Start (unlike AI Vision, this is a real renegotiation with the
	// server, not a pure local overlay) -- see models.VideoStartRequest.Color444's
	// doc comment. Visibility/enabled state is entirely driven by
	// refreshModeUI (codec == H.265 and the agent currently offers it).
	vsd.color444Check = widget.NewCheck(i18n.Current.Color444, nil)
	vsd.color444Hint = widget.NewLabel("")
	vsd.color444Hint.Wrapping = fyne.TextWrapWord
	vsd.color444Hint.TextStyle = fyne.TextStyle{Italic: true}
	vsd.color444Check.Hide()
	vsd.color444Hint.Hide()

	vsd.startBtn = newVideoDialogApplyButton(i18n.Current.StartVideo, vsd.handleStart)
	vsd.cancelBtn = newVideoDialogCancelButton(i18n.Current.Cancel, vsd.handleCancel)
	vsd.extraBtn = newVideoDialogExtraButton()
	vsd.extraBtn.Hide()

	bitrateHeader := container.NewBorder(nil, nil,
		widget.NewLabel(i18n.Current.Bitrate),
		vsd.bitrateValueLabel,
		nil,
	)
	vsd.bitrateBlock = container.New(&bitrateBlockLayout{
		headerHeight: 28,
		sliderTop:    20,
		sliderHeight: 44,
		hintsTop:     54,
	}, bitrateHeader, vsd.bitrateSlider, vsd.bitrateHintsRow)
	modeDetailsMinHeight := maxFloat32(vsd.bitrateBlock.MinSize().Height, vsd.jpegHint.MinSize().Height)
	modeDetailsReserve := canvas.NewRectangle(color.Transparent)
	modeDetailsReserve.SetMinSize(fyne.NewSize(0, modeDetailsMinHeight))
	vsd.modeDetailsSlot = container.NewStack(modeDetailsReserve, vsd.bitrateBlock)

	// Header/footer chrome (top accent bar, corner-pinned close X, hairline
	// separators, left-aligned title, right-grouped footer buttons) mirrors
	// the Add Connection dialog's own panel -- see showAdaptiveConnectionDialog
	// in controller/connection_manager_dialogs.go, whose pieces live in the
	// controller package and aren't reusable here directly.
	title := NewBrandText(i18n.Current.VideoParameters, 13, design.ColorTextLight, true)
	closeBtn := newIconChromeButton(iconChromeButtonSpec{
		NormalFill: color.Transparent,
		HoverFill:  design.ColorSurfaceLight,
		NormalIcon: theme.CancelIcon(),
		HoverIcon:  theme.CancelIcon(),
		IconSize:   fyne.NewSize(14, 14),
		ButtonSize: fyne.NewSize(28, 28),
		OnTapped:   vsd.handleCancel,
	})

	headerSepLine := color.NRGBA{R: 0x30, G: 0x34, B: 0x2e, A: 0xff}
	headerSep := canvas.NewRectangle(headerSepLine)
	headerSep.SetMinSize(fyne.NewSize(0, 1))
	footerSep := canvas.NewRectangle(headerSepLine)
	footerSep.SetMinSize(fyne.NewSize(0, 1))

	headerBlock := container.NewVBox(newVideoDialogTopAccentBar(), NewInset(title, 21, 44, 9, 4), headerSep)

	bodyContent := container.NewVBox(
		widget.NewLabel("Codec"),
		vsd.modeButtonsRow,
		container.NewCenter(vsd.modeDescription),
		container.NewVBox(
			widget.NewLabelWithStyle(i18n.Current.Resolution, fyne.TextAlignLeading, fyne.TextStyle{}),
			vsd.resolutionSelect,
			container.NewCenter(vsd.resolutionMeta),
		),
		container.NewVBox(
			widget.NewLabel(i18n.Current.FrameRate),
			vsd.fpsSelect,
			container.NewCenter(vsd.fpsMeta),
		),
		vsd.modeDetailsSlot,
		vsd.vsyncCheck,
		vsd.color444Check,
		vsd.color444Hint,
		vsd.aiVisionCheck,
		vsd.aiVisionHint,
	)

	// Cancel sits opposite Apply/extra, same as the Add Connection footer --
	// DeviceRowControlsLayout skips extraBtn entirely while it's hidden, so
	// the group collapses to just Apply when no extra action is set.
	footerButtons := container.NewBorder(nil, nil, vsd.cancelBtn, container.New(&DeviceRowControlsLayout{Gap: 12}, vsd.extraBtn, vsd.startBtn))
	footerBlock := container.NewVBox(footerSep, NewInset(footerButtons, 12, 18, 14, 0))

	form := container.NewBorder(headerBlock, footerBlock, nil, nil, NewInset(bodyContent, 18, 18, 12, 0))

	bg := canvas.NewRectangle(design.ColorGray900)
	bg.CornerRadius = design.RadiusMD
	border := canvas.NewRectangle(color.Transparent)
	border.CornerRadius = design.RadiusMD
	border.StrokeColor = design.ColorBorder
	border.StrokeWidth = 1
	cornerBtn := container.New(&videoDialogCornerButtonLayout{Top: 12, Right: 12}, closeBtn)
	panel := container.NewStack(
		bg,
		form,
		cornerBtn,
		border,
	)
	vsd.dialog = NewOverlayPopup(vsd.parent, OverlayPopupSpec{
		Panel:    panel,
		DimColor: color.NRGBA{R: 0x00, G: 0x00, B: 0x00, A: 0x72},
		PanelSize: func(canvasSize fyne.Size, panel fyne.CanvasObject) fyne.Size {
			margin := clampFloat32(minFloat32(canvasSize.Width, canvasSize.Height)*0.04, 20, 28)
			maxWidth := canvasSize.Width - margin*2
			maxHeight := canvasSize.Height - margin*2
			if maxWidth <= 0 {
				maxWidth = canvasSize.Width
			}
			if maxHeight <= 0 {
				maxHeight = canvasSize.Height
			}

			panelMin := panel.MinSize()
			panelWidth := minFloat32(maxFloat32(panelMin.Width, 460), maxWidth)
			panelHeight := minFloat32(maxFloat32(panelMin.Height, 520), maxHeight)
			return fyne.NewSize(panelWidth, panelHeight)
		},
	})
	vsd.bitrateSlider.OnChanged(vsd.bitrateSlider.Value)
}
func (vsd *VideoStartDialog) Configure(info *models.VideoInfoData, defaultWidth, defaultHeight, defaultFPS int, defaultBitrate string) {
	vsd.streamModes = nil
	vsd.captureModes = nil
	vsd.resolutionLabels = make(map[string]models.VideoCaptureMode)
	vsd.resolutionHints = make(map[string]string)
	vsd.color444Available = info != nil && info.Color444Available
	if !vsd.color444Available {
		vsd.color444Check.SetChecked(false)
	}

	// Only Moonlight-compatible encodings are supported; filter out legacy JPEG/RAW modes
	// that older server versions may still advertise.
	moonlightEncodings := map[string]bool{"h264": true, "h265": true, "av1": true}
	if info != nil {
		for _, m := range info.SupportedModes {
			if moonlightEncodings[m.Encoding] {
				vsd.streamModes = append(vsd.streamModes, m)
			}
		}
	}
	if len(vsd.streamModes) == 0 {
		vsd.streamModes = []models.VideoTransportMode{
			{
				ID:          models.VideoModeH264,
				Name:        i18n.Current.VideoModeH264Name,
				Description: i18n.Current.VideoModeH264Description,
				Transport:   "rtp",
				Encoding:    "h264",
			},
			{
				ID:          models.VideoModeH265,
				Name:        i18n.Current.VideoModeH265Name,
				Description: i18n.Current.VideoModeH265Description,
				Transport:   "rtp",
				Encoding:    "h265",
			},
			{
				ID:          models.VideoModeAV1,
				Name:        i18n.Current.VideoModeAV1Name,
				Description: i18n.Current.VideoModeAV1Description,
				Transport:   "rtp",
				Encoding:    "av1",
			},
		}
	}

	if info != nil && len(info.CaptureModes) > 0 {
		vsd.captureModes = append(vsd.captureModes, info.CaptureModes...)
	}

	// The capture card can offer the same resolution in multiple pixel
	// formats (e.g. both MJPEG and YUYV) — every entry is kept and shown as
	// its own option (formatResolutionBaseLabel appends "(FORMAT)" so they
	// don't collide in the dropdown) rather than collapsing to just the
	// server's current/default format, so the user can actually pick and
	// apply a different format than whatever's currently active.
	actualFormat := "YUYV"
	if info != nil {
		if info.SourceFormat != "" {
			actualFormat = normalizePixelFormat(info.SourceFormat)
		} else if info.DefaultPixelFormat != "" {
			actualFormat = normalizePixelFormat(info.DefaultPixelFormat)
		}
	}

	if len(vsd.captureModes) == 0 {
		vsd.captureModes = []models.VideoCaptureMode{
			{Width: defaultWidth, Height: defaultHeight, FPS: []int{defaultFPS}, PixelFormat: actualFormat},
		}
	}

	sort.Slice(vsd.captureModes, func(i, j int) bool {
		li := vsd.captureModes[i].Width * vsd.captureModes[i].Height
		lj := vsd.captureModes[j].Width * vsd.captureModes[j].Height
		if li != lj {
			return li < lj
		}
		return vsd.captureModes[i].Width < vsd.captureModes[j].Width
	})

	resolutionOptions := make([]string, 0, len(vsd.captureModes))
	defaultResolutionLabel := ""
	hasMultipleFormats := false
	formatsSeen := map[string]bool{}
	for _, captureMode := range vsd.captureModes {
		formatsSeen[captureMode.PixelFormat] = true
	}
	hasMultipleFormats = len(formatsSeen) > 1

	for _, captureMode := range vsd.captureModes {
		label := formatResolutionBaseLabel(captureMode)
		vsd.resolutionLabels[label] = captureMode
		vsd.resolutionHints[label] = resolutionDescriptor(captureMode.Width, captureMode.Height)
		resolutionOptions = append(resolutionOptions, label)
		if captureMode.Width == defaultWidth && captureMode.Height == defaultHeight {
			// Prefer the entry that matches the currently active capture format on the server.
			// This ensures the selected item in the list reflects reality, not a random duplicate.
			activeFormat := ""
			if info != nil {
				activeFormat = normalizePixelFormat(info.SourceFormat)
				if activeFormat == "" {
					activeFormat = normalizePixelFormat(info.DefaultPixelFormat)
				}
			}
			modeFormat := normalizePixelFormat(captureMode.PixelFormat)
			if defaultResolutionLabel == "" || (activeFormat != "" && modeFormat == activeFormat) {
				defaultResolutionLabel = label
			}
		}
	}
	vsd.resolutionSelect.SetOptions(resolutionOptions)
	vsd.resolutionSelect.SetDetails(vsd.resolutionHints)

	if defaultResolutionLabel == "" && len(resolutionOptions) > 0 {
		defaultResolutionLabel = resolutionOptions[0]
	}
	if defaultResolutionLabel != "" {
		vsd.resolutionSelect.SetSelected(defaultResolutionLabel)
	}
	vsd.updateResolutionMeta(hasMultipleFormats)

	// Resolve which codec button should be preselected, in priority order:
	//  1. The codec actually negotiated by a currently-running session — the
	//     only fully authoritative source (from moonlight-common-c's
	//     dr_setup, via liveCodecProvider).
	//  2. The agent's best-effort guess from info.Encoding (NOT info.Mode,
	//     which is a transport label like "moonlight", not a codec — using
	//     it here previously meant this always fell through to the first
	//     entry in the mode list, i.e. always H264).
	//  3. H264 as the last-resort default.
	selectedMode, source := "", "default"
	if vsd.liveCodecProvider != nil {
		if codec, ok := vsd.liveCodecProvider(); ok && codec != "" {
			selectedMode, source = codec, "live-negotiated"
		}
	}
	if selectedMode == "" && info != nil && info.Encoding != "" {
		selectedMode, source = info.Encoding, "agent-hint"
	}
	if selectedMode == "" {
		selectedMode = models.VideoModeH264
	}
	logrus.Infof("🎬 [VideoStartDialog] preselecting codec=%s (source=%s)", selectedMode, source)
	vsd.refreshAvailableModes()
	vsd.setSelectedModeID(selectedMode)

	if bitrate, ok := parseBitrate(defaultBitrate); ok {
		vsd.bitrateSlider.SetValue(float64(bitrate))
	} else {
		vsd.bitrateSlider.SetValue(20000)
	}

	vsd.refreshFPSOptions()
	vsd.setDefaultFPS(defaultFPS)
	vsd.refreshModeUI()
}

func (vsd *VideoStartDialog) Show(onApply func(request *models.VideoStartRequest)) {
	vsd.onApply = onApply
	vsd.startBtn.Enable()
	vsd.cancelBtn.Enable()
	vsd.aiVisionCheck.SetChecked(service.AIVisionEnabled())
	if vsd.dialog != nil && vsd.parent != nil {
		vsd.dialog.Move(fyne.NewPos(0, 0))
		vsd.dialog.Resize(vsd.parent.Canvas().Size())
		vsd.dialog.Refresh()
	}
	if !vsd.dialogShown {
		vsd.dialogShown = true
		overlayShow()
	}
	vsd.dialog.Show()
}

func (vsd *VideoStartDialog) SetDeviceLabel(text string) {
	vsd.deviceLabel.SetText(text)
	vsd.deviceLabel.Show()
	vsd.deviceLabel.Hide()
}

func (vsd *VideoStartDialog) SetPrimaryAction(label string) {
	if label == "" {
		label = i18n.Current.StartVideo
	}
	vsd.startBtn.SetText(label)
}

func (vsd *VideoStartDialog) SetExtraAction(label string, onTap func()) {
	if label == "" || onTap == nil {
		vsd.extraBtn.Hide()
		vsd.extraBtn.OnTapped = nil
		return
	}
	vsd.extraBtn.SetText(label)
	vsd.extraBtn.OnTapped = onTap
	vsd.extraBtn.Show()
}

func (vsd *VideoStartDialog) Hide() {
	if vsd.dialogShown {
		vsd.dialogShown = false
		overlayHide()
	}
	vsd.dialog.Hide()
	vsd.startBtn.Enable()
	vsd.cancelBtn.Enable()
	vsd.startBtn.SetText(i18n.Current.StartVideo)
}

func (vsd *VideoStartDialog) refreshFPSOptions() {
	mode, ok := vsd.resolutionLabels[vsd.resolutionSelect.Selected]
	if !ok {
		return
	}

	// Cap at 120: a server-reported capture mode can list far higher values
	// (e.g. a virtual/display capture reporting 240) than the encode
	// pipeline can actually sustain at this resolution/bitrate — picking
	// one that high overloads the hardware encoder (multi-hundred-ms
	// keyframe stalls, IDR-request storms) and can drive the session into
	// a disconnect/reconnect loop that never recovers, since the request
	// stays the same on every automatic retry.
	const maxSelectableFPS = 120
	options := make([]string, 0, len(mode.FPS))
	for _, fps := range mode.FPS {
		if fps > maxSelectableFPS {
			continue
		}
		options = append(options, strconv.Itoa(fps))
	}
	if len(options) == 0 {
		options = []string{"30"}
	}
	vsd.fpsSelect.SetOptions(options)
	if vsd.fpsSelect.Selected == "" {
		vsd.fpsSelect.SetSelected(options[0])
	}
}

func (vsd *VideoStartDialog) setDefaultFPS(defaultFPS int) {
	if defaultFPS <= 0 {
		return
	}
	for _, option := range vsd.fpsSelect.Options {
		if option == strconv.Itoa(defaultFPS) {
			vsd.fpsSelect.SetSelected(option)
			return
		}
	}
}

func (vsd *VideoStartDialog) refreshModeUI() {
	modeID := vsd.selectedModeID()
	description := localizedVideoModeDescription(modeID)
	if description == "" {
		for _, mode := range vsd.streamModes {
			if mode.ID == modeID {
				description = mode.Description
				break
			}
		}
	}
	vsd.modeDescription.Text = description
	vsd.modeDescription.Refresh()

	switch modeID {
	case models.VideoModeJPEGRTP:
		vsd.jpegHint.SetText(i18n.Current.VideoJPEGRTPHint)
		vsd.modeDetailsSlot.Objects = []fyne.CanvasObject{vsd.modeDetailsSlot.Objects[0], NewInset(vsd.jpegHint, 0, 0, 10, 0)}
	case models.VideoModeRawYUYV:
		vsd.jpegHint.SetText(i18n.Current.VideoRawYUYVHint)
		vsd.modeDetailsSlot.Objects = []fyne.CanvasObject{vsd.modeDetailsSlot.Objects[0], NewInset(vsd.jpegHint, 0, 0, 10, 0)}
	default:
		vsd.modeDetailsSlot.Objects = []fyne.CanvasObject{vsd.modeDetailsSlot.Objects[0], vsd.bitrateBlock}
	}
	vsd.modeDetailsSlot.Refresh()

	// RustShine Pro 4:4:4 color: only meaningful for H.265 (this project's
	// hardware encode path has no H.264/AV1 4:4:4 profile, see
	// service.moonlightVideoFormat's doc comment) -- hidden entirely for
	// every other codec rather than shown-disabled, since it's not a
	// choice that could ever apply there.
	if modeID == models.VideoModeH265 {
		vsd.color444Check.Show()
		vsd.color444Check.Enable()
		if vsd.color444Available {
			vsd.color444Hint.SetText(i18n.Current.Color444Hint)
		} else {
			vsd.color444Check.SetChecked(false)
			vsd.color444Check.Disable()
			vsd.color444Hint.SetText(i18n.Current.Color444UnavailableHint)
		}
		vsd.color444Hint.Show()
	} else {
		vsd.color444Check.SetChecked(false)
		vsd.color444Check.Hide()
		vsd.color444Hint.Hide()
	}
}

func localizedVideoModeDescription(modeID string) string {
	switch modeID {
	case models.VideoModeH264:
		return i18n.Current.VideoModeH264Description
	case models.VideoModeJPEGRTP:
		return i18n.Current.VideoModeJPEGDescription
	case models.VideoModeRawYUYV:
		return i18n.Current.VideoModeRawYUYVDescription
	default:
		return ""
	}
}

func (vsd *VideoStartDialog) selectedModeID() string {
	if vsd.currentModeID != "" {
		return vsd.currentModeID
	}
	return models.VideoModeH264
}

func (vsd *VideoStartDialog) setSelectedModeID(modeID string) {
	if modeID == "" {
		modeID = models.VideoModeH264
	}
	if len(vsd.modeButtons) > 0 {
		if _, ok := vsd.modeButtons[modeID]; !ok {
			requested := modeID
			for _, mode := range vsd.streamModes {
				if _, exists := vsd.modeButtons[mode.ID]; exists {
					modeID = mode.ID
					break
				}
			}
			logrus.Warnf("🎬 [VideoStartDialog] codec %q not offered by this device — falling back to %q", requested, modeID)
		}
	}
	vsd.currentModeID = modeID
	for id, button := range vsd.modeButtons {
		button.SetActive(id == modeID)
	}
	vsd.refreshModeUI()
}

func (vsd *VideoStartDialog) rebuildModeButtons() {
	vsd.modeButtons = make(map[string]*videoCodecButton)
	buttons := make([]fyne.CanvasObject, 0, len(vsd.streamModes))
	for _, mode := range vsd.streamModes {
		modeID := mode.ID
		button := newVideoCodecButton(videoCodecButtonLabel(modeID), func() {
			vsd.setSelectedModeID(modeID)
		})
		vsd.modeButtons[modeID] = button
		buttons = append(buttons, button)
	}
	vsd.modeButtonsRow.Objects = buttons
	vsd.modeButtonsRow.Refresh()
}

func videoCodecButtonLabel(modeID string) string {
	switch modeID {
	case models.VideoModeRawYUYV:
		return "RAW"
	case models.VideoModeJPEGRTP:
		return "JPEG"
	case models.VideoModeH265:
		return "H.265"
	case models.VideoModeAV1:
		return "AV1"
	default:
		return "H.264"
	}
}

func (vsd *VideoStartDialog) updateResolutionMeta(hasMultipleFormats bool) {
	if vsd.resolutionMeta == nil {
		return
	}

	commonFormat := ""
	commonFPS := ""
	if len(vsd.captureModes) > 0 {
		first := vsd.captureModes[0]
		commonFormat = first.PixelFormat
		commonFPS = formatFPSRange(first.FPS)
		for _, mode := range vsd.captureModes[1:] {
			if mode.PixelFormat != commonFormat {
				commonFormat = ""
			}
			if formatFPSRange(mode.FPS) != commonFPS {
				commonFPS = ""
			}
		}
	}

	parts := make([]string, 0, 2)
	if !hasMultipleFormats && commonFormat != "" {
		parts = append(parts, commonFormat)
	}
	if commonFPS != "" {
		parts = append(parts, commonFPS)
	}

	vsd.resolutionMeta.Text = strings.Join(parts, " · ")
	vsd.resolutionMeta.Refresh()
}

func formatResolutionBaseLabel(mode models.VideoCaptureMode) string {
	if mode.PixelFormat != "" {
		return fmt.Sprintf("%d x %d (%s)", mode.Width, mode.Height, mode.PixelFormat)
	}
	return fmt.Sprintf("%d x %d", mode.Width, mode.Height)
}

func resolutionDescriptor(width, height int) string {
	switch {
	case width == 1920 && height == 1080:
		return "Full HD (16:9)"
	case width == 1600 && height == 1200:
		return "UXGA (4:3)"
	case width == 1360 && height == 768:
		return "HD+ (16:9)"
	case width == 1280 && height == 1024:
		return "SXGA (5:4)"
	case width == 1280 && height == 960:
		return "SXGA- (4:3)"
	case width == 1280 && height == 720:
		return "HD (16:9)"
	case width == 1024 && height == 768:
		return "XGA (4:3)"
	case width == 800 && height == 600:
		return "SVGA (4:3)"
	case width == 720 && height == 576:
		return "PAL (5:4)"
	case width == 720 && height == 480:
		return "NTSC (3:2)"
	case width == 640 && height == 480:
		return "VGA (4:3)"
	default:
		ratio := aspectRatioLabel(width, height)
		if ratio == "" {
			return ""
		}
		return ratio
	}
}

func aspectRatioLabel(width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}

	gcd := greatestCommonDivisor(width, height)
	if gcd <= 0 {
		return ""
	}

	return fmt.Sprintf("(%d:%d)", width/gcd, height/gcd)
}

func greatestCommonDivisor(a, b int) int {
	for b != 0 {
		a, b = b, a%b
	}
	if a < 0 {
		return -a
	}
	return a
}

func (vsd *VideoStartDialog) handleStart() {
	vsd.startBtn.Disable()
	vsd.cancelBtn.Disable()
	vsd.startBtn.SetText("⏳ " + i18n.Current.Starting)

	selectedMode, ok := vsd.resolutionLabels[vsd.resolutionSelect.Selected]
	if !ok {
		selectedMode = models.VideoCaptureMode{Width: 800, Height: 600, FPS: []int{30}}
	}

	fps, err := strconv.Atoi(vsd.fpsSelect.Selected)
	if err != nil || fps <= 0 {
		fps = 30
	}

	request := &models.VideoStartRequest{
		VideoWidth:         selectedMode.Width,
		VideoHeight:        selectedMode.Height,
		VideoFPS:           fps,
		VideoQuality:       80,
		VideoBitrate:       fmt.Sprintf("%.0fK", vsd.bitrateSlider.Value),
		VideoMode:          vsd.selectedModeID(),
		CapturePixelFormat: selectedMode.PixelFormat,
		EnableVSync:        vsd.vsyncCheck.Checked,
		Color444:           vsd.selectedModeID() == models.VideoModeH265 && vsd.color444Check.Checked,
	}

	logrus.Infof("🎥 Starting video: mode=%s %dx%d @ %d fps, bitrate %s",
		request.VideoMode, request.VideoWidth, request.VideoHeight, request.VideoFPS, request.VideoBitrate)

	vsd.Hide()
	if vsd.onApply != nil {
		go vsd.onApply(request)
	}
}

func (vsd *VideoStartDialog) handleCancel() {
	logrus.Info("❌ Video start cancelled")
	vsd.Hide()
}

func formatFPSRange(values []int) string {
	if len(values) == 0 {
		return "fps?"
	}
	if len(values) == 1 {
		return fmt.Sprintf("%d fps", values[0])
	}
	return fmt.Sprintf("%d-%d fps", values[0], values[len(values)-1])
}

func (vsd *VideoStartDialog) refreshAvailableModes() {
	selectedCaptureMode, ok := vsd.resolutionLabels[vsd.resolutionSelect.Selected]
	selectedFormat := ""
	if ok {
		selectedFormat = normalizePixelFormat(selectedCaptureMode.PixelFormat)
	}

	allowed := allowedModesForPixelFormat(selectedFormat)
	previous := vsd.selectedModeID()
	vsd.modeButtons = make(map[string]*videoCodecButton)
	buttons := make([]fyne.CanvasObject, 0, len(vsd.streamModes))
	selectedAllowed := false
	for _, mode := range vsd.streamModes {
		if len(allowed) > 0 && !allowed[mode.ID] {
			continue
		}
		modeID := mode.ID
		button := newVideoCodecButton(videoCodecButtonLabel(modeID), func() {
			vsd.setSelectedModeID(modeID)
		})
		vsd.modeButtons[modeID] = button
		buttons = append(buttons, button)
		if modeID == previous {
			selectedAllowed = true
		}
	}
	vsd.modeButtonsRow.Objects = buttons
	vsd.modeButtonsRow.Refresh()

	if selectedAllowed {
		vsd.setSelectedModeID(previous)
		return
	}

	for _, mode := range vsd.streamModes {
		if len(allowed) == 0 || allowed[mode.ID] {
			vsd.setSelectedModeID(mode.ID)
			return
		}
	}

	vsd.setSelectedModeID(models.VideoModeH264)
}

func allowedModesForPixelFormat(format string) map[string]bool {
	switch normalizePixelFormat(format) {
	case "MJPG", "MJPEG", "JPEG":
		return map[string]bool{
			models.VideoModeH264:    true,
			models.VideoModeH265:    true,
			models.VideoModeAV1:     true,
			models.VideoModeJPEGRTP: true,
		}
	case "YUYV", "YUYV422", "YUY2":
		return map[string]bool{
			models.VideoModeH264:    true,
			models.VideoModeH265:    true,
			models.VideoModeAV1:     true,
			models.VideoModeJPEGRTP: true,
			models.VideoModeRawYUYV: true,
		}
	default:
		return map[string]bool{
			models.VideoModeH264: true,
			models.VideoModeH265: true,
			models.VideoModeAV1:  true,
		}
	}
}

func normalizePixelFormat(format string) string {
	f := strings.TrimSpace(strings.ToUpper(format))
	// The server reports source_format as "mjpeg" while capture_modes use "MJPG".
	// Normalize both to the same canonical form for comparison.
	switch f {
	case "MJPEG":
		return "MJPG"
	case "YUYV422":
		return "YUYV"
	case "UYVY422":
		return "UYVY"
	}
	return f
}

func parseBitrate(value string) (int, bool) {
	if value == "" {
		return 0, false
	}
	last := value[len(value)-1]
	switch last {
	case 'M':
		v, err := strconv.ParseFloat(value[:len(value)-1], 64)
		if err != nil {
			return 0, false
		}
		return int(v * 1000), true
	case 'K':
		v, err := strconv.ParseFloat(value[:len(value)-1], 64)
		if err != nil {
			return 0, false
		}
		return int(v), true
	default:
		v, err := strconv.Atoi(value)
		if err != nil {
			return 0, false
		}
		return v, true
	}
}
