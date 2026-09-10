package view

import (
	"fmt"
	"image/color"
	"math"
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

	currentModeID    string
	modeButtons      map[string]*videoCodecButton
	modeButtonsRow   *fyne.Container
	modeDescription  *canvas.Text
	resolutionSelect *HeaderDropdown
	resolutionMeta   *canvas.Text
	fpsSelect        *HeaderDropdown
	fpsMeta          *canvas.Text
	bitrateSlider    *videoDialogBitrateSlider
	bitrateBlock     *fyne.Container
	modeDetailsSlot  *fyne.Container
	jpegHint         *widget.Label
	deviceLabel      *widget.Label
	vsyncCheck       *videoDialogCheckbox
	aiVisionCheck    *videoDialogCheckbox
	aiVisionHint     *videoDialogWrapText
	// color444Check/color444Hint: the RustShine Pro 4:4:4 color upgrade.
	// Unlike AI Vision this row is always shown, on any codec -- it just
	// reads as an inactive/grayed item (dim title, disabled checkbox, a
	// badge explaining why) when it doesn't currently apply, rather than
	// disappearing outright. It's enabled only when the selected codec is
	// H.265 AND the agent's own video-info response says color444Available
	// (hardware probe AND license tier, see
	// models.VideoStatus.Color444Available's doc comment) -- see
	// refreshModeUI, which drives all of this via setColor444State.
	color444Check      *videoDialogCheckbox
	color444Hint       *videoDialogWrapText
	color444TitleText  *canvas.Text
	color444BadgeLabel *canvas.Text
	color444Available  bool

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
	// Slightly smaller than before (was 90x36) -- these now sit inside
	// their own bordered card (see the codec card wrapper in
	// createInterface) instead of directly in the body, which left them
	// looking oversized for the tight padding around that card.
	return fyne.NewSize(84, 30)
}

func (b *videoCodecButton) CreateRenderer() fyne.WidgetRenderer {
	b.bg = canvas.NewRectangle(design.ColorSurfaceLight)
	b.bg.CornerRadius = design.RadiusMD
	b.bg.StrokeWidth = 1

	// Matches the Apply/Cancel footer buttons' own text size
	// (videoDialogPillTextSize) -- was 13, which read oversized next to
	// everything else in this dialog.
	b.label = canvas.NewText(b.text, design.ColorTextLight)
	b.label.TextSize = videoDialogPillTextSize
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

// videoDialogHintColor/videoDialogHintTextSize style the small caption line
// shown under a field (codec description, resolution/FPS meta) -- muted gray,
// smaller than the field's own label.
var videoDialogHintColor = color.NRGBA{R: 0x8f, G: 0x93, B: 0x81, A: 0xff}

const videoDialogHintTextSize = float32(8)

// newVideoDialogFieldLabel is this dialog's field caption ("Codec",
// "Resolution", "Frame Rate") -- matches the Bitrate card's own caption
// style (uppercase, bold, muted olive) so every field label in this dialog
// reads as one family.
func newVideoDialogFieldLabel(text string) *canvas.Text {
	label := canvas.NewText(strings.ToUpper(text), color.NRGBA{R: 0xc5, G: 0xc8, B: 0xb5, A: 0xff})
	label.TextSize = 10
	label.TextStyle.Bold = true
	return label
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

// videoDialogVSpace is a fixed-height, invisible spacer for bodyContent's
// VBox -- a plain zero-width rectangle with a forced MinSize, not
// layout.NewSpacer() (which VBoxLayout instead stretches to fill any extra
// room), so it reserves exactly height pixels of breathing room and no
// more.
func videoDialogVSpace(height float32) fyne.CanvasObject {
	spacer := canvas.NewRectangle(color.Transparent)
	spacer.SetMinSize(fyne.NewSize(0, height))
	return spacer
}

// videoDialogBitrateSlider is a small custom slider matching this dialog's
// own look (a thin gray track, a glowing teal thumb) -- Fyne's themed
// widget.Slider has no way to recolor the track/thumb independently of the
// app theme, so this draws them directly instead.
type videoDialogBitrateSlider struct {
	widget.BaseWidget

	Min, Max, Step float64
	Value          float64
	OnChanged      func(float64)

	track *canvas.Rectangle
	glow  *canvas.Circle
	thumb *canvas.Circle
}

const (
	bitrateSliderThumbRadius = float32(6)
	bitrateSliderGlowRadius  = float32(9)
	bitrateSliderTrackHeight = float32(3)
	bitrateSliderHeight      = float32(20)
)

func newVideoDialogBitrateSlider(min, max, step float64) *videoDialogBitrateSlider {
	s := &videoDialogBitrateSlider{Min: min, Max: max, Step: step, Value: min}
	s.ExtendBaseWidget(s)
	return s
}

// SetValue clamps value to [Min, Max] and, if it actually changes Value,
// redraws the thumb and fires OnChanged -- mirrors widget.Slider.SetValue's
// own contract closely enough that Configure()'s existing call site needs no
// changes.
func (s *videoDialogBitrateSlider) SetValue(value float64) {
	if value < s.Min {
		value = s.Min
	}
	if value > s.Max {
		value = s.Max
	}
	if value == s.Value {
		return
	}
	s.Value = value
	s.Refresh()
	if s.OnChanged != nil {
		s.OnChanged(s.Value)
	}
}

func (s *videoDialogBitrateSlider) valueFraction() float32 {
	if s.Max <= s.Min {
		return 0
	}
	f := (s.Value - s.Min) / (s.Max - s.Min)
	if f < 0 {
		f = 0
	}
	if f > 1 {
		f = 1
	}
	return float32(f)
}

func (s *videoDialogBitrateSlider) setValueFromX(x float32) {
	usable := s.Size().Width - bitrateSliderThumbRadius*2
	if usable <= 0 {
		return
	}
	rel := (x - bitrateSliderThumbRadius) / usable
	if rel < 0 {
		rel = 0
	}
	if rel > 1 {
		rel = 1
	}
	value := s.Min + float64(rel)*(s.Max-s.Min)
	if s.Step > 0 {
		value = math.Round(value/s.Step) * s.Step
	}
	s.SetValue(value)
}

func (s *videoDialogBitrateSlider) Tapped(e *fyne.PointEvent) {
	s.setValueFromX(e.Position.X)
}

func (s *videoDialogBitrateSlider) TappedSecondary(*fyne.PointEvent) {}

func (s *videoDialogBitrateSlider) Dragged(e *fyne.DragEvent) {
	s.setValueFromX(e.Position.X)
}

func (s *videoDialogBitrateSlider) DragEnd() {}

func (s *videoDialogBitrateSlider) Cursor() desktop.Cursor {
	return desktop.PointerCursor
}

func (s *videoDialogBitrateSlider) MinSize() fyne.Size {
	return fyne.NewSize(120, bitrateSliderHeight)
}

func (s *videoDialogBitrateSlider) CreateRenderer() fyne.WidgetRenderer {
	s.track = canvas.NewRectangle(color.NRGBA{R: 0x31, G: 0x35, B: 0x39, A: 0xff})
	s.track.CornerRadius = bitrateSliderTrackHeight / 2

	s.glow = canvas.NewCircle(color.NRGBA{R: 0x41, G: 0xe0, B: 0xc3, A: 0x33})
	s.thumb = canvas.NewCircle(design.ColorConnectionBadgeText)

	return &videoDialogBitrateSliderRenderer{slider: s}
}

type videoDialogBitrateSliderRenderer struct {
	slider *videoDialogBitrateSlider
}

func (r *videoDialogBitrateSliderRenderer) Layout(size fyne.Size) {
	s := r.slider
	trackY := (size.Height - bitrateSliderTrackHeight) / 2
	// The track spans the full control width (not inset by the thumb
	// radius) so its left/right edges line up with the label row above and
	// the Low/High hint row below -- only the thumb's own travel range is
	// inset, so it doesn't get visually clipped at either end.
	s.track.Move(fyne.NewPos(0, trackY))
	s.track.Resize(fyne.NewSize(size.Width, bitrateSliderTrackHeight))

	thumbRange := size.Width - bitrateSliderThumbRadius*2
	if thumbRange < 0 {
		thumbRange = 0
	}
	cx := bitrateSliderThumbRadius + s.valueFraction()*thumbRange
	cy := size.Height / 2

	s.glow.Move(fyne.NewPos(cx-bitrateSliderGlowRadius, cy-bitrateSliderGlowRadius))
	s.glow.Resize(fyne.NewSize(bitrateSliderGlowRadius*2, bitrateSliderGlowRadius*2))

	s.thumb.Move(fyne.NewPos(cx-bitrateSliderThumbRadius, cy-bitrateSliderThumbRadius))
	s.thumb.Resize(fyne.NewSize(bitrateSliderThumbRadius*2, bitrateSliderThumbRadius*2))
}

func (r *videoDialogBitrateSliderRenderer) MinSize() fyne.Size {
	return r.slider.MinSize()
}

func (r *videoDialogBitrateSliderRenderer) Refresh() {
	r.Layout(r.slider.Size())
	canvas.Refresh(r.slider)
}

func (r *videoDialogBitrateSliderRenderer) BackgroundColor() color.Color {
	return color.Transparent
}

func (r *videoDialogBitrateSliderRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.slider.track, r.slider.glow, r.slider.thumb}
}

func (r *videoDialogBitrateSliderRenderer) Destroy() {}

// videoDialogBorderColor is the muted olive border shared by this dialog's
// bordered cards (the bitrate card, the boxed toggle rows) and small badges.
var videoDialogBorderColor = color.NRGBA{R: 0x33, G: 0x37, B: 0x2f, A: 0xff}

// videoDialogCancelIconSVG is the same muted-gray X glyph the Add
// Connection dialog's own header close button uses (see
// connectionDialogCancelIconRes in controller/connection_manager_dialogs.go
// -- that copy is unexported and controller-package-private, so this
// dialog needs its own identical resource rather than theme.CancelIcon(),
// whose default stroke-style X reads differently).
var videoDialogCancelIconSVG = fyne.NewStaticResource("video_dialog_cancel.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="#8f9381"><path d="M19 6.41 17.59 5 12 10.59 6.41 5 5 6.41 10.59 12 5 17.59 6.41 19 12 13.41 17.59 19 19 17.59 13.41 12z"/></svg>`))

// videoDialogCheckmarkSVG is a small dark checkmark glyph, drawn onto
// videoDialogCheckbox's teal fill when checked.
var videoDialogCheckmarkSVG = fyne.NewStaticResource("video_dialog_checkmark.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="#0b0f12" stroke-width="4" stroke-linecap="round" stroke-linejoin="round"><path d="M4 12L10 18L20 6"/></svg>`))

// videoDialogCrossmarkSVG is a small muted-gray cross, drawn instead of the
// checkmark when a checkbox is disabled and unchecked -- e.g. the 4:4:4 row
// on a non-H.265 codec, where an empty box read as ambiguous ("is this off,
// or just not rendered yet?") rather than clearly unavailable.
var videoDialogCrossmarkSVG = fyne.NewStaticResource("video_dialog_crossmark.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="#8f9381" stroke-width="4" stroke-linecap="round" stroke-linejoin="round"><path d="M5 5L19 19M19 5L5 19"/></svg>`))

const (
	videoDialogCheckboxSize   = float32(16)
	videoDialogCheckboxRadius = float32(4)
	videoDialogCheckboxMark   = float32(9)
)

// videoDialogCheckbox is a small custom checkbox matching this dialog's own
// look (rounded teal square, a checkmark sized to fit) -- Fyne's themed
// widget.Check has no way to recolor, resize, or round its box
// independently of the app theme. Mirrors widget.Check's own API surface
// (Checked field, OnChanged, SetChecked, Enable/Disable) closely enough
// that this dialog's Configure/handleStart call sites needed no changes.
type videoDialogCheckbox struct {
	widget.BaseWidget

	Checked   bool
	OnChanged func(bool)

	// OnTapWhileDisabled, when set, fires instead of the normal toggle when
	// the checkbox is tapped while disabled -- the 4:4:4 row uses this to
	// let a tap switch the codec to H.265 and turn itself on in one action,
	// rather than silently doing nothing.
	OnTapWhileDisabled func()

	disabled bool
	hovered  bool

	bg    *canvas.Rectangle
	check *canvas.Image
	cross *canvas.Image
}

func newVideoDialogCheckbox(checked bool, onChanged func(bool)) *videoDialogCheckbox {
	c := &videoDialogCheckbox{Checked: checked, OnChanged: onChanged}
	c.ExtendBaseWidget(c)
	return c
}

func (c *videoDialogCheckbox) SetChecked(checked bool) {
	if c.Checked == checked {
		return
	}
	c.Checked = checked
	c.Refresh()
}

func (c *videoDialogCheckbox) Enable() {
	if !c.disabled {
		return
	}
	c.disabled = false
	c.Refresh()
}

func (c *videoDialogCheckbox) Disable() {
	if c.disabled {
		return
	}
	c.disabled = true
	c.Refresh()
}

func (c *videoDialogCheckbox) Disabled() bool {
	return c.disabled
}

func (c *videoDialogCheckbox) Tapped(*fyne.PointEvent) {
	if c.disabled {
		if c.OnTapWhileDisabled != nil {
			c.OnTapWhileDisabled()
		}
		return
	}
	c.Checked = !c.Checked
	c.Refresh()
	if c.OnChanged != nil {
		c.OnChanged(c.Checked)
	}
}

func (c *videoDialogCheckbox) TappedSecondary(*fyne.PointEvent) {}

func (c *videoDialogCheckbox) MouseIn(*desktop.MouseEvent) {
	c.hovered = true
	c.Refresh()
}

func (c *videoDialogCheckbox) MouseMoved(*desktop.MouseEvent) {}

func (c *videoDialogCheckbox) MouseOut() {
	c.hovered = false
	c.Refresh()
}

func (c *videoDialogCheckbox) Cursor() desktop.Cursor {
	return desktop.PointerCursor
}

func (c *videoDialogCheckbox) MinSize() fyne.Size {
	return fyne.NewSize(videoDialogCheckboxSize, videoDialogCheckboxSize)
}

func (c *videoDialogCheckbox) CreateRenderer() fyne.WidgetRenderer {
	c.bg = canvas.NewRectangle(color.Transparent)
	c.bg.CornerRadius = videoDialogCheckboxRadius
	c.bg.StrokeWidth = 1

	c.check = canvas.NewImageFromResource(videoDialogCheckmarkSVG)
	c.check.FillMode = canvas.ImageFillContain

	c.cross = canvas.NewImageFromResource(videoDialogCrossmarkSVG)
	c.cross.FillMode = canvas.ImageFillContain

	r := &videoDialogCheckboxRenderer{cb: c}
	r.applyColors()
	return r
}

type videoDialogCheckboxRenderer struct {
	cb *videoDialogCheckbox
}

func (r *videoDialogCheckboxRenderer) Layout(size fyne.Size) {
	cb := r.cb
	boxPos := fyne.NewPos((size.Width-videoDialogCheckboxSize)/2, (size.Height-videoDialogCheckboxSize)/2)
	cb.bg.Move(boxPos)
	cb.bg.Resize(fyne.NewSize(videoDialogCheckboxSize, videoDialogCheckboxSize))

	markPos := fyne.NewPos(boxPos.X+(videoDialogCheckboxSize-videoDialogCheckboxMark)/2, boxPos.Y+(videoDialogCheckboxSize-videoDialogCheckboxMark)/2)
	cb.check.Move(markPos)
	cb.check.Resize(fyne.NewSize(videoDialogCheckboxMark, videoDialogCheckboxMark))
	cb.cross.Move(markPos)
	cb.cross.Resize(fyne.NewSize(videoDialogCheckboxMark, videoDialogCheckboxMark))
}

func (r *videoDialogCheckboxRenderer) applyColors() {
	cb := r.cb
	switch {
	case cb.disabled:
		cb.bg.FillColor = color.NRGBA{R: 0x22, G: 0x26, B: 0x2a, A: 0xff}
		cb.bg.StrokeColor = videoDialogBorderColor
		cb.check.Translucency = 0.6
		cb.check.Hidden = !cb.Checked
		cb.cross.Hidden = cb.Checked
	case cb.Checked:
		cb.bg.FillColor = design.ColorConnectionBadgeText
		cb.bg.StrokeColor = color.Transparent
		cb.check.Translucency = 0
		cb.check.Hidden = false
		cb.cross.Hidden = true
	default:
		fill := color.Color(color.Transparent)
		if cb.hovered {
			fill = color.NRGBA{R: 0x22, G: 0x26, B: 0x2a, A: 0xff}
		}
		cb.bg.FillColor = fill
		cb.bg.StrokeColor = videoDialogBorderColor
		cb.check.Hidden = true
		cb.cross.Hidden = true
	}
}

func (r *videoDialogCheckboxRenderer) MinSize() fyne.Size {
	return r.cb.MinSize()
}

func (r *videoDialogCheckboxRenderer) Refresh() {
	r.applyColors()
	r.Layout(r.cb.Size())
	r.cb.bg.Refresh()
	r.cb.check.Refresh()
	r.cb.cross.Refresh()
}

func (r *videoDialogCheckboxRenderer) BackgroundColor() color.Color {
	return color.Transparent
}

func (r *videoDialogCheckboxRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.cb.bg, r.cb.check, r.cb.cross}
}

func (r *videoDialogCheckboxRenderer) Destroy() {}

// videoDialogCardBG is the same slightly-lightened card background as the
// bitrate card, reused for the boxed AI Vision/4:4:4 toggle rows so every
// bordered card in this dialog reads as one family.
var videoDialogCardBG = color.NRGBA{R: 0x1e, G: 0x22, B: 0x25, A: 0xff}

// videoDialogPanelWidth/videoDialogBodyInsetLR/videoDialogBoxedInsetLR mirror
// the actual layout constants used below (the panel's width floor in
// PanelSize, bodyContent's own NewInset, and newVideoDialogBoxedToggleRow's
// own NewInset) -- kept as named constants here so videoDialogToggleDescWidth
// can derive the real usable width instead of a second, easily-drifting copy
// of the same numbers.
const (
	videoDialogPanelWidth   = float32(408)
	videoDialogBodyInsetLR  = float32(18)
	videoDialogBoxedInsetLR = float32(10)
)

// videoDialogBodyInsetQuirk is NewInset's own extra theme.Padding() (4px by
// default) added on top of every non-zero side it's given -- see NewInset's
// doc comment. bodyContent (below) is wrapped with NewInset, not
// NewInsetExact, so its real left/right inset is videoDialogBodyInsetLR
// plus this, not videoDialogBodyInsetLR alone.
const videoDialogBodyInsetQuirk = float32(4)

// videoDialogToggleDescWidth is the width a toggle row's description text
// actually ends up with once fully laid out -- plain rows (VSync, 4:4:4) vs.
// AI Vision's own boxed row, which loses an extra 12px of exact padding on
// each side (see newVideoDialogBoxedToggleRow's own NewInsetExact). Each
// videoDialogWrapText description needs this width up front, at
// construction/SetSpans time, since it wraps eagerly rather than lazily on
// some future Resize.
func videoDialogToggleDescWidth(boxed bool) float32 {
	width := videoDialogPanelWidth - (videoDialogBodyInsetLR+videoDialogBodyInsetQuirk)*2 - videoDialogToggleIndent
	if boxed {
		width -= videoDialogBoxedInsetLR * 2
	} else {
		// Plain rows (VSync, 4:4:4) are shifted right by
		// videoDialogToggleAlignLeft so their checkboxes line up with AI
		// Vision's own boxed row -- see its use in createInterface.
		width -= videoDialogToggleAlignLeft
	}
	return width
}

// videoDialogWrapSpan is one differently-styled run of text within a
// videoDialogWrapText -- e.g. a teal monospace "ui.parse()" inline in an
// otherwise plain, muted sentence.
type videoDialogWrapSpan struct {
	Text      string
	Color     color.Color
	Monospace bool
}

// videoDialogWrapText renders one or more styled spans as manually
// word-wrapped canvas.Text lines, instead of widget.Label/widget.RichText.
//
// It replaces three separate failed attempts at getting a toggle row's
// description positioned correctly under its title (HBox, then
// container.NewBorder, then container.NewVBox+NewInset, then a hand-rolled
// widget.BaseWidget wrapping widget.Label/RichText) -- every one of those
// relied on Label/RichText recomputing their own wrapped line bounds at the
// right moment relative to *two* separate "skip if size unchanged" guards
// (widget.BaseWidget.Resize and RichText.Resize both no-op when the new
// size equals the current one), so a description that needed a fresh wrap
// after a presize trick or a runtime SetText could easily end up displayed
// with stale bounds from an earlier (often placeholder-width-only) pass.
//
// This widget sidesteps that whole mechanism: since this dialog's panel
// width is fixed, wrapping is computed eagerly, directly from a known
// width, in SetSpans itself (not lazily on some future Resize/Refresh) --
// there is no cache to go stale. CreateRenderer's Layout just repositions
// the same already-wrapped lines, which is idempotent no matter how many
// times or when it runs.
type videoDialogWrapText struct {
	widget.BaseWidget

	textSize float32
	italic   bool
	width    float32

	lineHeight float32
	lines      [][]*canvas.Text
}

func newVideoDialogWrapText(width, textSize float32, italic bool, spans ...videoDialogWrapSpan) *videoDialogWrapText {
	t := &videoDialogWrapText{textSize: textSize, italic: italic, width: width}
	t.ExtendBaseWidget(t)
	t.SetSpans(spans...)
	return t
}

// SetSpans replaces the wrapped content and immediately recomputes and
// repositions every line -- used by the 4:4:4 row, whose description text
// changes at runtime (see refreshModeUI).
func (t *videoDialogWrapText) SetSpans(spans ...videoDialogWrapSpan) {
	style := fyne.TextStyle{Italic: t.italic}
	t.lineHeight = fyne.MeasureText("M", t.textSize, style).Height
	spaceWidth := fyne.MeasureText(" ", t.textSize, style).Width

	type word struct {
		txt   *canvas.Text
		width float32
	}
	var words []word
	for _, span := range spans {
		wordStyle := style
		wordStyle.Monospace = span.Monospace
		for _, w := range strings.Fields(span.Text) {
			txt := canvas.NewText(w, span.Color)
			txt.TextSize = t.textSize
			txt.TextStyle = wordStyle
			words = append(words, word{txt: txt, width: fyne.MeasureText(w, t.textSize, wordStyle).Width})
		}
	}

	var lines [][]*canvas.Text
	var cur []*canvas.Text
	curWidth := float32(0)
	for _, w := range words {
		add := w.width
		if len(cur) > 0 {
			add += spaceWidth
		}
		if len(cur) > 0 && curWidth+add > t.width {
			lines = append(lines, cur)
			cur = nil
			curWidth = 0
			add = w.width
		}
		cur = append(cur, w.txt)
		curWidth += add
	}
	if len(cur) > 0 {
		lines = append(lines, cur)
	}
	t.lines = lines
	t.layoutLines()
	t.Refresh()
}

// layoutLines positions every already-wrapped word at its final (x, y) --
// called eagerly from SetSpans (so the widget is correctly laid out even
// before Fyne ever calls its renderer) and again, redundantly but
// harmlessly, from the renderer's own Layout.
func (t *videoDialogWrapText) layoutLines() {
	style := fyne.TextStyle{Italic: t.italic}
	spaceWidth := fyne.MeasureText(" ", t.textSize, style).Width
	y := float32(0)
	for _, line := range t.lines {
		x := float32(0)
		for _, word := range line {
			size := word.MinSize()
			word.Move(fyne.NewPos(x, y))
			word.Resize(size)
			x += size.Width + spaceWidth
		}
		y += t.lineHeight
	}
}

func (t *videoDialogWrapText) MinSize() fyne.Size {
	if len(t.lines) == 0 {
		return fyne.NewSize(t.width, 0)
	}
	return fyne.NewSize(t.width, float32(len(t.lines))*t.lineHeight)
}

func (t *videoDialogWrapText) CreateRenderer() fyne.WidgetRenderer {
	return &videoDialogWrapTextRenderer{t: t}
}

type videoDialogWrapTextRenderer struct {
	t *videoDialogWrapText
}

func (r *videoDialogWrapTextRenderer) Layout(fyne.Size) {
	r.t.layoutLines()
}

func (r *videoDialogWrapTextRenderer) MinSize() fyne.Size {
	return r.t.MinSize()
}

func (r *videoDialogWrapTextRenderer) Refresh() {
	canvas.Refresh(r.t)
}

func (r *videoDialogWrapTextRenderer) BackgroundColor() color.Color {
	return color.Transparent
}

func (r *videoDialogWrapTextRenderer) Objects() []fyne.CanvasObject {
	objs := make([]fyne.CanvasObject, 0, len(r.t.lines)*4)
	for _, line := range r.t.lines {
		for _, w := range line {
			objs = append(objs, w)
		}
	}
	return objs
}

func (r *videoDialogWrapTextRenderer) Destroy() {}

// newVideoDialogDescription is a wrapped, muted description line under a
// toggle's title -- the VSync/4:4:4 rows' own plain (non-italic) style.
func newVideoDialogDescription(text string, width float32) *videoDialogWrapText {
	return newVideoDialogWrapText(width, videoDialogHintTextSize, false, videoDialogWrapSpan{Text: text, Color: videoDialogHintColor})
}

// newVideoDialogHighlightDescription is a wrapped, muted, italic
// description that highlights every occurrence of code inline as teal
// monospace -- the AI Vision row's own style, so it can reference a literal
// API call (e.g. "ui.parse()") inline without breaking the sentence into
// separate, hard-to-translate string fields.
func newVideoDialogHighlightDescription(text, code string, width float32) *videoDialogWrapText {
	spans := make([]videoDialogWrapSpan, 0, 3)
	rest := text
	for {
		idx := strings.Index(rest, code)
		if idx < 0 {
			if rest != "" {
				spans = append(spans, videoDialogWrapSpan{Text: rest, Color: videoDialogHintColor})
			}
			break
		}
		if idx > 0 {
			spans = append(spans, videoDialogWrapSpan{Text: rest[:idx], Color: videoDialogHintColor})
		}
		spans = append(spans, videoDialogWrapSpan{Text: code, Color: design.ColorConnectionBadgeText, Monospace: true})
		rest = rest[idx+len(code):]
	}
	return newVideoDialogWrapText(width, videoDialogHintTextSize, true, spans...)
}

// newVideoDialogMutableBadge is the small uppercase pill shown next to a
// toggle's title (e.g. "RECOMMENDED", "EXPERIMENTAL", "PRO") -- it also
// returns the label so a caller that needs to change the badge's wording or
// color later (the 4:4:4 row, see setColor444State) can. Everyone else uses
// newVideoDialogBadge, which just discards that second value.
//
// Uses NewInsetExact, not NewInset -- NewInset's own extra theme.Padding()
// on top of what's asked (see its doc comment) was silently turning the "1"
// top/bottom padding asked for here into an effective 5, which is why this
// badge kept looking taller than intended.
func newVideoDialogMutableBadge(text string, textColor color.Color) (fyne.CanvasObject, *canvas.Text) {
	bg := canvas.NewRectangle(color.NRGBA{R: 0x22, G: 0x26, B: 0x2a, A: 0xff})
	bg.CornerRadius = 4
	border := canvas.NewRectangle(color.Transparent)
	border.CornerRadius = 4
	border.StrokeColor = videoDialogBorderColor
	border.StrokeWidth = 1
	label := canvas.NewText(strings.ToUpper(text), textColor)
	label.TextSize = 7
	label.TextStyle = fyne.TextStyle{Bold: true, Monospace: true}
	return container.NewStack(bg, border, NewInsetExact(label, 6, 6, 1, 1)), label
}

func newVideoDialogBadge(text string, textColor color.Color) fyne.CanvasObject {
	badge, _ := newVideoDialogMutableBadge(text, textColor)
	return badge
}

// newVideoDialogRowTitle is a toggle row's bold title text -- a plain
// constructor (rather than building it inline in newVideoDialogToggleRow)
// so a caller that needs to gray the title out later (the 4:4:4 row) can
// keep its own reference.
func newVideoDialogRowTitle(text string) *canvas.Text {
	title := canvas.NewText(text, design.ColorTextLight)
	title.TextSize = 10
	title.TextStyle.Bold = true
	return title
}

// videoDialogRobotSVG is a small robot glyph shown before the AI Vision
// row's title -- recolored to match its own "Experimental" badge
// (design.ColorConnectionAddFill, #c4e77a), the same way
// videoDialogCheckmarkSVG/videoDialogCrossmarkSVG inline their own fixed
// stroke colors rather than pulling from the app theme.
var videoDialogRobotSVG = fyne.NewStaticResource("video_dialog_robot.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" fill="#c4e77a" viewBox="0 0 24 24"><path d="M9,15a1,1,0,1,0,1,1A1,1,0,0,0,9,15ZM2,14a1,1,0,0,0-1,1v2a1,1,0,0,0,2,0V15A1,1,0,0,0,2,14Zm20,0a1,1,0,0,0-1,1v2a1,1,0,0,0,2,0V15A1,1,0,0,0,22,14ZM17,7H13V5.72A2,2,0,0,0,14,4a2,2,0,0,0-4,0,2,2,0,0,0,1,1.72V7H7a3,3,0,0,0-3,3v9a3,3,0,0,0,3,3H17a3,3,0,0,0,3-3V10A3,3,0,0,0,17,7ZM13.72,9l-.5,2H10.78l-.5-2ZM18,19a1,1,0,0,1-1,1H7a1,1,0,0,1-1-1V10A1,1,0,0,1,7,9H8.22L9,12.24A1,1,0,0,0,10,13h4a1,1,0,0,0,1-.76L15.78,9H17a1,1,0,0,1,1,1Zm-3-4a1,1,0,1,0,1,1A1,1,0,0,0,15,15Z"/></svg>`))

// videoDialogStarSVG is a small star glyph shown before the 4:4:4 row's
// title, colored purple (#aa42e0) to read as a distinct "Pro" indicator
// from AI Vision's lime robot.
var videoDialogStarSVG = fyne.NewStaticResource("video_dialog_star.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="#aa42e0" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M11.2691 4.41115C11.5006 3.89177 11.6164 3.63208 11.7776 3.55211C11.9176 3.48263 12.082 3.48263 12.222 3.55211C12.3832 3.63208 12.499 3.89177 12.7305 4.41115L14.5745 8.54808C14.643 8.70162 14.6772 8.77839 14.7302 8.83718C14.777 8.8892 14.8343 8.93081 14.8982 8.95929C14.9705 8.99149 15.0541 9.00031 15.2213 9.01795L19.7256 9.49336C20.2911 9.55304 20.5738 9.58288 20.6997 9.71147C20.809 9.82316 20.8598 9.97956 20.837 10.1342C20.8108 10.3122 20.5996 10.5025 20.1772 10.8832L16.8125 13.9154C16.6877 14.0279 16.6252 14.0842 16.5857 14.1527C16.5507 14.2134 16.5288 14.2807 16.5215 14.3503C16.5132 14.429 16.5306 14.5112 16.5655 14.6757L17.5053 19.1064C17.6233 19.6627 17.6823 19.9408 17.5989 20.1002C17.5264 20.2388 17.3934 20.3354 17.2393 20.3615C17.0619 20.3915 16.8156 20.2495 16.323 19.9654L12.3995 17.7024C12.2539 17.6184 12.1811 17.5765 12.1037 17.56C12.0352 17.5455 11.9644 17.5455 11.8959 17.56C11.8185 17.5765 11.7457 17.6184 11.6001 17.7024L7.67662 19.9654C7.18404 20.2495 6.93775 20.3915 6.76034 20.3615C6.60623 20.3354 6.47319 20.2388 6.40075 20.1002C6.31736 19.9408 6.37635 19.6627 6.49434 19.1064L7.4341 14.6757C7.46898 14.5112 7.48642 14.429 7.47814 14.3503C7.47081 14.2807 7.44894 14.2134 7.41394 14.1527C7.37439 14.0842 7.31195 14.0279 7.18708 13.9154L3.82246 10.8832C3.40005 10.5025 3.18884 10.3122 3.16258 10.1342C3.13978 9.97956 3.19059 9.82316 3.29993 9.71147C3.42581 9.58288 3.70856 9.55304 4.27406 9.49336L8.77835 9.01795C8.94553 9.00031 9.02911 8.99149 9.10139 8.95929C9.16534 8.93081 9.2226 8.8892 9.26946 8.83718C9.32241 8.77839 9.35663 8.70162 9.42508 8.54808L11.2691 4.41115Z"/></svg>`))

// newVideoDialogInlineIcon is a small, fixed-size icon glyph meant to sit
// immediately before a toggle row's title text.
func newVideoDialogInlineIcon(icon fyne.Resource) *canvas.Image {
	img := canvas.NewImageFromResource(icon)
	img.FillMode = canvas.ImageFillContain
	img.SetMinSize(fyne.NewSize(11, 11))
	return img
}

// newVideoDialogIconTitle is a toggle row's title with a small icon glyph
// immediately before the text, both on the row's first line -- the AI
// Vision row's own style. Unlike the wrapped descriptions above, this is a
// fixed-size, non-wrapping pair (icon + one line of text), so a plain HBox
// is fine here -- none of videoDialogWrapText's reasons for existing apply
// to content that never needs to reflow.
func newVideoDialogIconTitle(icon fyne.Resource, text string) fyne.CanvasObject {
	return container.NewHBox(newVideoDialogInlineIcon(icon), newVideoDialogRowTitle(text))
}

// newVideoDialogIconTitleText is newVideoDialogIconTitle's variant for a
// title the caller already built and needs to keep mutating later -- the
// 4:4:4 row's own title, grayed in and out by setColor444State.
func newVideoDialogIconTitleText(icon fyne.Resource, title *canvas.Text) fyne.CanvasObject {
	return container.NewHBox(newVideoDialogInlineIcon(icon), title)
}

// videoDialogToggleIndent is the description line's left indent -- how far
// the checkbox's own width plus the title's own gap reaches. Exposed as a
// constant since videoDialogToggleDescWidth (used to eagerly wrap
// descriptions to their final width, see videoDialogWrapText) needs to
// subtract exactly what videoDialogToggleLayout will actually use.
const videoDialogToggleIndent = videoDialogCheckboxSize + videoDialogToggleGap

const (
	videoDialogToggleGap     = float32(8) // checkbox -> title, and title -> badge
	videoDialogToggleDescGap = float32(2) // title row -> description
)

// videoDialogToggleAlignLeft shifts every toggle row right by this amount,
// so a plain row's checkbox lines up with the boxed AI Vision row's own
// checkbox -- its card's own left padding (videoDialogBoxedInsetLR) would
// otherwise push just that one row's checkbox further right than VSync's
// and 4:4:4's.
const videoDialogToggleAlignLeft = videoDialogBoxedInsetLR

// videoDialogToggleLayout is a plain fyne.Layout (not a widget) for one
// checkbox+title+badge+description row -- not a composition of generic
// Fyne containers. Generic containers (HBox/VBox/Border/NewInset) kept
// introducing their own padding/gap here that this dialog didn't ask for
// (theme.Padding() between HBox children, NewInset's own extra Border
// padding on top of its spacer rectangles -- see NewInset's doc comment)
// and repeatedly threw the description out of alignment with its own
// title. A plain fyne.Layout, used via container.New, gives full, exact
// control over every position -- and, unlike an earlier attempt at this
// same fix that wrapped a widget.BaseWidget around a hand-rolled renderer,
// doesn't add its own extra layer of widget.BaseWidget/cache.Renderer
// machinery for Fyne to schedule around; container.New's caller-visible
// *fyne.Container already does the minimum necessary itself.
//
// Objects must always be exactly [check, title, badge, description].
type videoDialogToggleLayout struct{}

func (l *videoDialogToggleLayout) titleRowHeight(objs []fyne.CanvasObject) float32 {
	check, title, badge := objs[0], objs[1], objs[2]
	return maxFloat32(check.MinSize().Height, maxFloat32(title.MinSize().Height, badge.MinSize().Height))
}

func (l *videoDialogToggleLayout) indentX(objs []fyne.CanvasObject) float32 {
	return objs[0].MinSize().Width + videoDialogToggleGap
}

func (l *videoDialogToggleLayout) MinSize(objs []fyne.CanvasObject) fyne.Size {
	title, badge, desc := objs[1], objs[2], objs[3]
	indent := l.indentX(objs)
	titleRowWidth := indent + title.MinSize().Width + videoDialogToggleGap + badge.MinSize().Width
	descSize := desc.MinSize()
	width := maxFloat32(titleRowWidth, indent+descSize.Width)
	height := l.titleRowHeight(objs) + videoDialogToggleDescGap + descSize.Height
	return fyne.NewSize(width, height)
}

func (l *videoDialogToggleLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	check, title, badge, desc := objs[0], objs[1], objs[2], objs[3]
	rowHeight := l.titleRowHeight(objs)

	checkSize := check.MinSize()
	check.Move(fyne.NewPos(0, (rowHeight-checkSize.Height)/2))
	check.Resize(checkSize)

	indent := l.indentX(objs)
	titleSize := title.MinSize()
	title.Move(fyne.NewPos(indent, (rowHeight-titleSize.Height)/2))
	title.Resize(titleSize)

	badgeSize := badge.MinSize()
	badgeX := indent + titleSize.Width + videoDialogToggleGap
	badge.Move(fyne.NewPos(badgeX, (rowHeight-badgeSize.Height)/2))
	badge.Resize(badgeSize)

	descY := rowHeight + videoDialogToggleDescGap
	descWidth := maxFloat32(0, size.Width-indent)
	descHeight := maxFloat32(0, size.Height-descY)
	desc.Move(fyne.NewPos(indent, descY))
	desc.Resize(fyne.NewSize(descWidth, descHeight))
}

// newVideoDialogToggleRow lays out one checkbox row in the reference
// design: the checkbox and a bold title + badge share the first line, and
// the description sits on its own line below, indented to the title's own
// left edge.
func newVideoDialogToggleRow(check *videoDialogCheckbox, titleText fyne.CanvasObject, badge fyne.CanvasObject, description fyne.CanvasObject) *fyne.Container {
	return container.New(&videoDialogToggleLayout{}, check, titleText, badge, description)
}

// newVideoDialogBoxedToggleRow wraps newVideoDialogToggleRow's content in
// its own bordered card (same bg/border as the bitrate card) -- AI Vision's
// own "special" treatment. Every other toggle in this dialog (VSync, 4:4:4)
// stays a plain inline row instead -- boxing every row made the whole
// section too tall to fit comfortably.
func newVideoDialogBoxedToggleRow(check *videoDialogCheckbox, titleText fyne.CanvasObject, badge fyne.CanvasObject, description fyne.CanvasObject) *fyne.Container {
	cardBG := canvas.NewRectangle(design.ColorGray950)
	cardBG.CornerRadius = design.RadiusMD
	cardBorder := canvas.NewRectangle(color.Transparent)
	cardBorder.CornerRadius = design.RadiusMD
	cardBorder.StrokeColor = videoDialogBorderColor
	cardBorder.StrokeWidth = 1
	row := newVideoDialogToggleRow(check, titleText, badge, description)
	// NewInsetExact, not NewInset -- this padding needs to match
	// videoDialogToggleDescWidth's own subtraction exactly (see that
	// function's doc comment); NewInset's extra, undocumented
	// theme.Padding() on top of what's asked would silently widen the gap
	// between this formula and the row's real available width.
	return container.NewStack(cardBG, cardBorder, NewInsetExact(row, videoDialogBoxedInsetLR, videoDialogBoxedInsetLR, 8, 8))
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
	vsd.modeDescription = canvas.NewText("", videoDialogHintColor)
	vsd.modeDescription.TextSize = videoDialogHintTextSize
	vsd.modeDescription.Alignment = fyne.TextAlignCenter
	vsd.modeButtonsRow = container.New(&videoCodecButtonsLayout{gap: 10})

	vsd.resolutionSelect = newVideoDialogPicker(func(string) {
		vsd.refreshAvailableModes()
		vsd.refreshFPSOptions()
	})
	vsd.resolutionMeta = canvas.NewText("", videoDialogHintColor)
	vsd.resolutionMeta.TextSize = videoDialogHintTextSize
	vsd.resolutionMeta.Alignment = fyne.TextAlignCenter

	vsd.fpsSelect = newVideoDialogPicker(nil)
	vsd.fpsMeta = canvas.NewText(i18n.Current.FramesPerSecond, videoDialogHintColor)
	vsd.fpsMeta.TextSize = videoDialogHintTextSize
	vsd.fpsMeta.Alignment = fyne.TextAlignCenter

	vsd.bitrateSlider = newVideoDialogBitrateSlider(1000, 150000, 1000)
	vsd.bitrateSlider.Value = 20000

	vsd.jpegHint = widget.NewLabel(i18n.Current.VideoJPEGRTPHint)
	vsd.jpegHint.Wrapping = fyne.TextWrapWord
	vsd.jpegHint.Alignment = fyne.TextAlignCenter
	vsd.deviceLabel = widget.NewLabel("")
	vsd.deviceLabel.Wrapping = fyne.TextWrapWord
	vsd.vsyncCheck = newVideoDialogCheckbox(true, nil)
	vsyncRow := newVideoDialogToggleRow(
		vsd.vsyncCheck,
		newVideoDialogRowTitle(i18n.Current.EnableVSync),
		newVideoDialogBadge(i18n.Current.EnableVSyncBadge, design.ColorConnectionBadgeText),
		newVideoDialogDescription(i18n.Current.EnableVSyncHint, videoDialogToggleDescWidth(false)),
	)

	// AI Vision: off by default, takes effect immediately (not gated behind
	// Start/Apply) since it's a pure local-rendering overlay -- see
	// service.SetAIVisionEnabled's doc comment.
	vsd.aiVisionCheck = newVideoDialogCheckbox(service.AIVisionEnabled(), func(checked bool) {
		service.SetAIVisionEnabled(checked)
	})
	vsd.aiVisionHint = newVideoDialogHighlightDescription(i18n.Current.AIVisionHint, "ui.parse()", videoDialogToggleDescWidth(true))
	aiVisionRow := newVideoDialogBoxedToggleRow(
		vsd.aiVisionCheck,
		newVideoDialogIconTitle(videoDialogRobotSVG, i18n.Current.AIVision),
		newVideoDialogBadge(i18n.Current.AIVisionBadge, design.ColorConnectionAddFill),
		vsd.aiVisionHint,
	)

	// RustShine Pro 4:4:4 color: off by default, takes effect on the next
	// Start (unlike AI Vision, this is a real renegotiation with the
	// server, not a pure local overlay) -- see models.VideoStartRequest.Color444's
	// doc comment. Always shown (any codec) -- refreshModeUI grays it out via
	// setColor444State instead of hiding it outright when it doesn't apply.
	// Its text changes at runtime (see refreshModeUI), unlike VSync/AI
	// Vision's static copy -- built with no spans yet here, since
	// videoDialogWrapText.SetSpans is what actually fills it in, called by
	// refreshModeUI before this dialog is ever shown.
	vsd.color444Check = newVideoDialogCheckbox(false, nil)
	vsd.color444Check.OnTapWhileDisabled = func() {
		// Tapping the checkbox while it's grayed out because H.265 isn't
		// selected is treated as "turn this on": switch the codec for the
		// user instead of making them hunt for the codec buttons above.
		// Only when RustShine Pro/hardware actually supports it, though --
		// if it's unavailable outright, switching codecs wouldn't help.
		if !vsd.color444Available || vsd.selectedModeID() == models.VideoModeH265 {
			return
		}
		vsd.setSelectedModeID(models.VideoModeH265)
		vsd.color444Check.SetChecked(true)
	}
	vsd.color444Hint = newVideoDialogWrapText(videoDialogToggleDescWidth(false), videoDialogHintTextSize, true)
	vsd.color444TitleText = newVideoDialogRowTitle(i18n.Current.Color444)
	color444Badge, color444BadgeLabel := newVideoDialogMutableBadge(i18n.Current.Color444Badge, videoDialogHintColor)
	vsd.color444BadgeLabel = color444BadgeLabel
	color444Row := newVideoDialogToggleRow(
		vsd.color444Check,
		newVideoDialogIconTitleText(videoDialogStarSVG, vsd.color444TitleText),
		color444Badge,
		vsd.color444Hint,
	)

	// vsyncRow/color444Row are plain rows with no left padding of their own,
	// unlike aiVisionRow's own card (see newVideoDialogBoxedToggleRow) --
	// without this, its own left inset would push just its checkbox further
	// right than these two, breaking the visual column of checkboxes down
	// the whole section.
	vsyncRow = NewInsetExact(vsyncRow, videoDialogToggleAlignLeft, 0, 0, 0)
	color444Row = NewInsetExact(color444Row, videoDialogToggleAlignLeft, 0, 0, 0)

	vsd.startBtn = newVideoDialogApplyButton(i18n.Current.StartVideo, vsd.handleStart)
	vsd.cancelBtn = newVideoDialogCancelButton(i18n.Current.Cancel, vsd.handleCancel)
	vsd.extraBtn = newVideoDialogExtraButton()
	vsd.extraBtn.Hide()

	// Bitrate card: bg #1e2225 / border #33372f, a "TARGET BITRATE" caption
	// with the live value in its own small pill (bg #0b0f12, same border),
	// the teal-thumbed slider, and Low/High bound hints -- see the
	// reference screenshot this restyle matches.
	bitrateValueNumber := canvas.NewText("", color.NRGBA{R: 0xeb, G: 0xff, B: 0xbc, A: 0xff})
	bitrateValueNumber.TextSize = 10
	bitrateValueNumber.TextStyle = fyne.TextStyle{Bold: true, Monospace: true}
	bitrateValueUnit := canvas.NewText(i18n.Current.UnitMbps, color.NRGBA{R: 0xeb, G: 0xff, B: 0xbc, A: 0xff})
	bitrateValueUnit.TextSize = 8
	bitrateValueUnit.TextStyle = fyne.TextStyle{Monospace: true}
	vsd.bitrateSlider.OnChanged = func(value float64) {
		bitrateValueNumber.Text = fmt.Sprintf("%.1f", value/1000)
		bitrateValueNumber.Refresh()
	}

	valuePillBG := canvas.NewRectangle(design.ColorGray950)
	valuePillBG.CornerRadius = 6
	valuePillBorder := canvas.NewRectangle(color.Transparent)
	valuePillBorder.CornerRadius = 6
	valuePillBorder.StrokeColor = videoDialogBorderColor
	valuePillBorder.StrokeWidth = 1
	valuePill := container.NewStack(valuePillBG, valuePillBorder,
		NewInset(container.NewHBox(bitrateValueNumber, bitrateValueUnit), 10, 10, 2, 2),
	)

	bitrateLabel := canvas.NewText(strings.ToUpper(i18n.Current.Bitrate), color.NRGBA{R: 0xc5, G: 0xc8, B: 0xb5, A: 0xff})
	bitrateLabel.TextSize = 10
	bitrateLabel.TextStyle.Bold = true
	bitrateHeaderRow := container.NewBorder(nil, nil, bitrateLabel, valuePill, nil)

	lowHint := canvas.NewText(fmt.Sprintf("Low Latency (%.1f %s)", vsd.bitrateSlider.Min/1000, i18n.Current.UnitMbps), videoDialogHintColor)
	lowHint.TextSize = videoDialogHintTextSize
	highHint := canvas.NewText(fmt.Sprintf("High Fidelity (%.1f %s)", vsd.bitrateSlider.Max/1000, i18n.Current.UnitMbps), videoDialogHintColor)
	highHint.TextSize = videoDialogHintTextSize
	bitrateHintsRow := container.NewBorder(nil, nil, lowHint, highHint, nil)

	bitrateCardBG := canvas.NewRectangle(videoDialogCardBG)
	bitrateCardBG.CornerRadius = design.RadiusMD
	bitrateCardBorder := canvas.NewRectangle(color.Transparent)
	bitrateCardBorder.CornerRadius = design.RadiusMD
	bitrateCardBorder.StrokeColor = videoDialogBorderColor
	bitrateCardBorder.StrokeWidth = 1
	bitrateCardContent := NewInset(container.NewVBox(bitrateHeaderRow, vsd.bitrateSlider, bitrateHintsRow), 14, 14, 10, 10)
	vsd.bitrateBlock = container.NewStack(bitrateCardBG, bitrateCardContent, bitrateCardBorder)

	vsd.bitrateSlider.OnChanged(vsd.bitrateSlider.Value)

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
		NormalIcon: videoDialogCancelIconSVG,
		HoverIcon:  videoDialogCancelIconSVG,
		IconSize:   fyne.NewSize(18, 18),
		ButtonSize: fyne.NewSize(28, 28),
		OnTapped:   vsd.handleCancel,
	})

	headerSepLine := color.NRGBA{R: 0x30, G: 0x34, B: 0x2e, A: 0xff}
	headerSep := canvas.NewRectangle(headerSepLine)
	headerSep.SetMinSize(fyne.NewSize(0, 1))
	footerSep := canvas.NewRectangle(headerSepLine)
	footerSep.SetMinSize(fyne.NewSize(0, 1))

	headerBlock := container.NewVBox(newVideoDialogTopAccentBar(), NewInset(title, 21, 44, 9, 4), headerSep)

	resolutionFPSRow := container.NewGridWithColumns(2,
		container.NewVBox(
			newVideoDialogFieldLabel(i18n.Current.Resolution),
			vsd.resolutionSelect,
			container.NewCenter(vsd.resolutionMeta),
		),
		container.NewVBox(
			newVideoDialogFieldLabel(i18n.Current.FrameRate),
			vsd.fpsSelect,
			container.NewCenter(vsd.fpsMeta),
		),
	)

	// Codec buttons get their own small bordered card (same border color as
	// everywhere else in this dialog) instead of sitting bare in the body --
	// see videoCodecButton.MinSize's own comment for why the buttons
	// themselves shrank slightly to fit it.
	codecCardBG := canvas.NewRectangle(design.ColorGray950)
	codecCardBG.CornerRadius = design.RadiusMD
	codecCardBorder := canvas.NewRectangle(color.Transparent)
	codecCardBorder.CornerRadius = design.RadiusMD
	codecCardBorder.StrokeColor = videoDialogBorderColor
	codecCardBorder.StrokeWidth = 1
	codecCard := container.NewStack(codecCardBG, codecCardBorder, NewInsetExact(vsd.modeButtonsRow, 2, 2, 2, 2))

	bodyContent := container.NewVBox(
		newVideoDialogFieldLabel("Codec"),
		codecCard,
		container.NewCenter(vsd.modeDescription),
		resolutionFPSRow,
		vsd.modeDetailsSlot,
		videoDialogVSpace(8), // breathing room before VSync
		vsyncRow,
		aiVisionRow,
		color444Row,
		videoDialogVSpace(8), // breathing room after 4:4:4 Color
	)

	// Cancel sits opposite Apply/extra, same as the Add Connection footer --
	// DeviceRowControlsLayout skips extraBtn entirely while it's hidden, so
	// the group collapses to just Apply when no extra action is set.
	footerButtons := container.NewBorder(nil, nil, container.NewCenter(vsd.cancelBtn), container.New(&DeviceRowControlsLayout{Gap: 12}, vsd.extraBtn, vsd.startBtn))
	// NewInsetExact, not NewInset -- was 12/18/14/0 through NewInset (whose
	// own +4px-per-side quirk made the top inset an effective 18), trimmed
	// down to shrink the whole footer band by roughly 20px total together
	// with the panel's own bottom margin below.
	footerBlock := container.NewVBox(footerSep, NewInsetExact(footerButtons, 12, 18, 6, 0))

	form := container.NewBorder(headerBlock, footerBlock, nil, nil, NewInset(bodyContent, videoDialogBodyInsetLR, videoDialogBodyInsetLR, 12, 0))

	bg := canvas.NewRectangle(design.ColorGray900)
	bg.CornerRadius = design.RadiusMD
	border := canvas.NewRectangle(color.Transparent)
	border.CornerRadius = design.RadiusMD
	border.StrokeColor = design.ColorBorder
	border.StrokeWidth = 1
	cornerBtn := container.New(&videoDialogCornerButtonLayout{Top: 12, Right: 12}, closeBtn)
	// A small bottom margin here (matching showAdaptiveConnectionDialog's
	// own panel) keeps the footer buttons from sitting flush against the
	// panel's bottom rounded corner -- trimmed down (was 16, +4 quirk via
	// NewInset) together with footerBlock's own inset above, to cut the
	// whole footer band down by roughly 20px total.
	panel := container.NewStack(
		bg,
		NewInsetExact(form, 0, 0, 0, 8),
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
			panelWidth := minFloat32(maxFloat32(panelMin.Width, videoDialogPanelWidth), maxWidth)
			panelHeight := minFloat32(maxFloat32(panelMin.Height, 520), maxHeight)
			return fyne.NewSize(panelWidth, panelHeight)
		},
	})
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
	// resolutionShortLabels abbreviates the closed picker's display text
	// (e.g. "1920x1080" instead of the full "1920 x 1080 (YUYV)" option/key)
	// so the pill stays narrow enough for the Resolution/FPS row to sit side
	// by side -- the popup's own rows still show the full option text.
	resolutionShortLabels := make(map[string]string, len(vsd.captureModes))
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
		resolutionShortLabels[label] = fmt.Sprintf("%dx%d", captureMode.Width, captureMode.Height)
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
	vsd.resolutionSelect.SetShortLabels(resolutionShortLabels)

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
	// service.moonlightVideoFormat's doc comment). The row itself always
	// stays visible on every codec -- rather than disappearing when it
	// doesn't apply, it grays out and its badge explains why (requires
	// H.265, or requires RustShine Pro), which reads clearer than the
	// choice silently vanishing.
	switch {
	case modeID != models.VideoModeH265:
		vsd.color444Check.SetChecked(false)
		vsd.color444Check.Disable()
		vsd.color444Hint.SetSpans(videoDialogWrapSpan{Text: i18n.Current.Color444RequiresH265Hint, Color: videoDialogHintColor})
		vsd.setColor444State(false, i18n.Current.Color444CodecBadge)
	case vsd.color444Available:
		vsd.color444Check.Enable()
		vsd.color444Hint.SetSpans(videoDialogWrapSpan{Text: i18n.Current.Color444Hint, Color: videoDialogHintColor})
		vsd.setColor444State(true, i18n.Current.Color444Badge)
	default:
		vsd.color444Check.SetChecked(false)
		vsd.color444Check.Disable()
		vsd.color444Hint.SetSpans(videoDialogWrapSpan{Text: i18n.Current.Color444UnavailableHint, Color: videoDialogHintColor})
		vsd.setColor444State(false, i18n.Current.Color444Badge)
	}
}

// setColor444State grays or restores the 4:4:4 row's title and swaps its
// badge's wording -- see refreshModeUI's callers.
func (vsd *VideoStartDialog) setColor444State(enabled bool, badgeText string) {
	if enabled {
		vsd.color444TitleText.Color = design.ColorTextLight
		vsd.color444BadgeLabel.Color = design.ColorConnectionBadgeText
	} else {
		vsd.color444TitleText.Color = videoDialogHintColor
		vsd.color444BadgeLabel.Color = videoDialogHintColor
	}
	vsd.color444TitleText.Refresh()
	vsd.color444BadgeLabel.Text = strings.ToUpper(badgeText)
	vsd.color444BadgeLabel.Refresh()
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
