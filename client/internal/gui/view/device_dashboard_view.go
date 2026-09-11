// device_dashboard_view.go -- the Devices tab's card-grid layout: a narrow
// left column (HID & Input Hub, Video Pipe & EDID, Audio Pipeline) stacked
// above one another, and a wide right column (Virtual Mass Storage & ISO
// Media) beside them, both styled after the Connections grid's own cards
// (see connection_grid_card.go). Populated from controller/disk_widget_dashboard.go.
package view

import (
	"fmt"
	"image/color"
	"strings"
	"sync"
	"time"

	"usbridge-client/internal/gui/assets"
	"usbridge-client/internal/gui/design"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)

// DeviceDashboardColumnsLayout arranges exactly two children side by side --
// a narrow column and a wide one, the wide column Ratio times as wide as
// the narrow one, with Gap pixels between them. The Devices tab's own
// narrow-left/wide-right split (HID/Video/Audio cards vs. the single big
// Storage card).
type DeviceDashboardColumnsLayout struct {
	Gap   float32
	Ratio float32
}

func (l *DeviceDashboardColumnsLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) < 2 {
		return
	}
	narrow, wide := objects[0], objects[1]
	ratio := l.Ratio
	if ratio <= 0 {
		ratio = 1
	}
	avail := maxFloat32(0, size.Width-l.Gap)
	narrowWidth := avail / (ratio + 1)
	wideWidth := avail - narrowWidth

	// Each column gets its own natural (MinSize) height, not the shared
	// size.Height -- forcing both to size.Height (the taller column's own
	// height, since that's what MinSize below reports) used to stretch the
	// shorter column's card to fill all the leftover space instead of
	// sitting at whatever height its own content actually needs.
	narrow.Move(fyne.NewPos(0, 0))
	narrow.Resize(fyne.NewSize(narrowWidth, narrow.MinSize().Height))
	wide.Move(fyne.NewPos(narrowWidth+l.Gap, 0))
	wide.Resize(fyne.NewSize(wideWidth, wide.MinSize().Height))
}

func (l *DeviceDashboardColumnsLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	if len(objects) < 2 {
		return fyne.NewSize(0, 0)
	}
	narrowMin := objects[0].MinSize()
	wideMin := objects[1].MinSize()
	return fyne.NewSize(narrowMin.Width+l.Gap+wideMin.Width, maxFloat32(narrowMin.Height, wideMin.Height))
}

func visibleDashboardObjects(objects []fyne.CanvasObject) []fyne.CanvasObject {
	visible := make([]fyne.CanvasObject, 0, len(objects))
	for _, obj := range objects {
		if usableCanvasObject(obj) && obj.Visible() {
			visible = append(visible, obj)
		}
	}
	return visible
}

// DeviceDashboardPairLayout places its visible children in one equal-width
// row with Gap between them -- Network and Backups under Storage, each
// taking half the wide column when both are shown, or the full row if the
// other card is hidden.
type DeviceDashboardPairLayout struct {
	Gap float32
}

func (l *DeviceDashboardPairLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	visible := visibleDashboardObjects(objects)
	if len(visible) == 0 {
		return
	}
	if len(visible) == 1 {
		visible[0].Move(fyne.NewPos(0, 0))
		visible[0].Resize(size)
		return
	}
	n := float32(len(visible))
	slot := (size.Width - l.Gap*(n-1)) / n
	if slot < 0 {
		slot = 0
	}
	x := float32(0)
	for _, obj := range visible {
		obj.Move(fyne.NewPos(x, 0))
		obj.Resize(fyne.NewSize(slot, size.Height))
		x += slot + l.Gap
	}
}

func (l *DeviceDashboardPairLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	visible := visibleDashboardObjects(objects)
	if len(visible) == 0 {
		return fyne.NewSize(0, 0)
	}
	if len(visible) == 1 {
		return visible[0].MinSize()
	}
	var width, height float32
	for i, obj := range visible {
		min := obj.MinSize()
		width += min.Width
		if i > 0 {
			width += l.Gap
		}
		if min.Height > height {
			height = min.Height
		}
	}
	return fyne.NewSize(width, height)
}

// deviceDashboardHIDSplitLayout places keyboard and mouse side by side in
// one HID row, with a 1px vertical rule between them. objects are
// [left, divider, right]; Gap is the breathing room on each side of the
// rule.
type deviceDashboardHIDSplitLayout struct {
	Gap float32
}

func (l *deviceDashboardHIDSplitLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) < 3 {
		if len(objects) == 1 && objects[0] != nil {
			objects[0].Move(fyne.NewPos(0, 0))
			objects[0].Resize(size)
		}
		return
	}
	left, sep, right := objects[0], objects[1], objects[2]
	sepW := float32(1)
	slot := (size.Width - l.Gap*2 - sepW) / 2
	if slot < 0 {
		slot = 0
	}
	left.Move(fyne.NewPos(0, 0))
	left.Resize(fyne.NewSize(slot, size.Height))
	sepH := sep.MinSize().Height
	if sepH > size.Height {
		sepH = size.Height
	}
	sep.Move(fyne.NewPos(slot+l.Gap, (size.Height-sepH)/2))
	sep.Resize(fyne.NewSize(sepW, sepH))
	right.Move(fyne.NewPos(slot+l.Gap*2+sepW, 0))
	right.Resize(fyne.NewSize(slot, size.Height))
}

func (l *deviceDashboardHIDSplitLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	if len(objects) < 3 {
		if len(objects) == 1 && objects[0] != nil {
			return objects[0].MinSize()
		}
		return fyne.NewSize(0, 0)
	}
	left := objects[0].MinSize()
	sep := objects[1].MinSize()
	right := objects[2].MinSize()
	h := left.Height
	if right.Height > h {
		h = right.Height
	}
	if sep.Height > h {
		h = sep.Height
	}
	return fyne.NewSize(left.Width+l.Gap+1+l.Gap+right.Width, h)
}

// deviceDashboardTightVBoxLayout stacks exactly two children vertically
// with a fixed Gap instead of theme.Padding() -- a plain container.NewVBox
// put a card's description noticeably too far below its title/header row.
type deviceDashboardTightVBoxLayout struct {
	Gap float32
}

func (l *deviceDashboardTightVBoxLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) < 2 {
		return
	}
	top, bottom := objects[0], objects[1]
	topMin := top.MinSize()
	top.Move(fyne.NewPos(0, 0))
	top.Resize(fyne.NewSize(size.Width, topMin.Height))
	bottom.Move(fyne.NewPos(0, topMin.Height+l.Gap))
	bottom.Resize(fyne.NewSize(size.Width, bottom.MinSize().Height))
}

func (l *deviceDashboardTightVBoxLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	if len(objects) < 2 {
		return fyne.NewSize(0, 0)
	}
	topMin := objects[0].MinSize()
	bottomMin := objects[1].MinSize()
	return fyne.NewSize(maxFloat32(topMin.Width, bottomMin.Width), topMin.Height+l.Gap+bottomMin.Height)
}

// deviceDashboardCardSep is the same muted divider color used under a
// Connections card's own top row (see connection_grid_card.go's own divider
// rectangle) -- used both for a card's full-width header separator and
// (inset, see NewDeviceDashboardRowSeparator) between its own rows.
var deviceDashboardCardSep = color.NRGBA{R: 0x29, G: 0x2d, B: 0x27, A: 0xff}

// deviceDashboardDescColor is the small caption line under a dashboard
// card's title (e.g. Storage's "Emulated OTG USB Mass Storage Drive &
// CD-ROM Devices").
var deviceDashboardDescColor = color.NRGBA{R: 0xc5, G: 0xc8, B: 0xb5, A: 0xff}

// NewDeviceDashboardHoverCell returns a stable onHover callback -- safe to
// hand to buttons/rows built before the thing that ultimately reacts to it
// exists -- plus a bind function that wires it to a real implementation
// later. NewDeviceDashboardCard's own caller needs this: it builds a
// card's row-level buttons (Delete/Upload/Connect/toggle/mode-picker)
// before the card that will contain them, unlike connection_grid_card.go's
// own cards, which build everything in one function and can wire hover
// directly. Every interactive control inside the card should have its own
// OnHover set to the returned onHover -- otherwise hovering it flickers
// the card's border off and back on instead of it staying teal
// continuously the whole time the cursor is anywhere over the card (see
// connection_grid_card.go's own identical per-button wiring).
func NewDeviceDashboardHoverCell() (onHover func(bool), bind func(func(bool))) {
	var real func(bool)
	onHover = func(hovered bool) {
		if real != nil {
			real(hovered)
		}
	}
	bind = func(fn func(bool)) { real = fn }
	return onHover, bind
}

// NewDeviceDashboardCard builds one dashboard card in the Connections
// grid's own visual language: a dark rounded panel (design.ColorGray900,
// design.RadiusLG, a muted border that brightens to teal on hover -- see
// connection_grid_card.go's own cards) with an icon+title header (an
// optional element on the header's right edge, e.g. a header button) and
// an optional one-line description below it, a full-width hairline
// divider, then content -- the header/content each carry their own
// padding independently so the divider between them can span edge to
// edge. bindHover, from NewDeviceDashboardHoverCell, wires this card's own
// hover-border logic to the onHover cell every interactive control inside
// content was already built with.
func NewDeviceDashboardCard(icon fyne.Resource, title string, description string, headerRight fyne.CanvasObject, content fyne.CanvasObject, bindHover func(func(bool))) fyne.CanvasObject {
	card, _ := NewDeviceDashboardCardWithTitle(icon, title, description, headerRight, content, bindHover)
	return card
}

// NewDeviceDashboardCardWithTitle is NewDeviceDashboardCard plus a setter
// for the header label. Devices' storage card uses it to show
// "Virtual Mass Storage & ISO Media" on hardware KVM and "USB Emulation"
// on a software agent.
func NewDeviceDashboardCardWithTitle(icon fyne.Resource, title string, description string, headerRight fyne.CanvasObject, content fyne.CanvasObject, bindHover func(func(bool))) (fyne.CanvasObject, func(string)) {
	iconImg := canvas.NewImageFromResource(icon)
	iconImg.FillMode = canvas.ImageFillContain
	iconImg.SetMinSize(fyne.NewSize(16, 16))

	titleText := canvas.NewText(title, design.ColorTextLight)
	titleText.TextSize = 11
	titleText.TextStyle.Bold = true

	titleRow := container.New(&DeviceRowControlsLayout{Gap: 8}, iconImg, titleText)

	var headerRow fyne.CanvasObject = titleRow
	if headerRight != nil {
		headerRow = container.NewBorder(nil, nil, titleRow, headerRight)
	}

	headerBlock := headerRow
	if strings.TrimSpace(description) != "" {
		descText := canvas.NewText(description, deviceDashboardDescColor)
		descText.TextSize = 8
		// Tight custom gap, not a plain VBox (whose default theme.Padding()
		// left the description reading too far from the title above it).
		headerBlock = container.New(&deviceDashboardTightVBoxLayout{Gap: 1}, headerRow, descText)
	}

	sep := canvas.NewRectangle(deviceDashboardCardSep)
	sep.SetMinSize(fyne.NewSize(0, 1))

	body := container.NewVBox(
		NewInset(headerBlock, 14, 14, 7, 5),
		sep,
		// Top padding trimmed (was 8) -- the row list read like it was
		// sitting noticeably lower than the divider above it.
		NewInset(content, 14, 14, 3, 12),
	)

	cardBg := canvas.NewRectangle(design.ColorGray900)
	cardBg.CornerRadius = design.RadiusLG
	cardBg.StrokeColor = design.ColorTailscaleChipBorder
	cardBg.StrokeWidth = 1

	// Same debounced hover-border technique as connection_grid_card.go's
	// own cards: a 50ms grace period before actually applying "unhovered"
	// so a quick transition from the overlay (or one interactive control)
	// straight into another (both routed through this same onHover cell)
	// never visibly flickers the border off.
	var hoverTimer *time.Timer
	setCardHovered := func(hovered bool) {
		if hovered {
			if hoverTimer != nil {
				hoverTimer.Stop()
				hoverTimer = nil
			}
			cardBg.StrokeColor = design.ColorConnectionBadgeText
			cardBg.Refresh()
		} else {
			if hoverTimer != nil {
				hoverTimer.Stop()
			}
			hoverTimer = time.AfterFunc(50*time.Millisecond, func() {
				cardBg.StrokeColor = design.ColorTailscaleChipBorder
				cardBg.Refresh()
			})
		}
	}
	bindHover(setCardHovered)

	// Covers whatever part of the card no interactive control already
	// claims (e.g. the header/divider area) -- placed behind the actual
	// content so a button on top still gets first claim on the cursor.
	overlay := newConnectionCardOverlay(nil, setCardHovered)

	return container.NewStack(overlay, cardBg, body), func(next string) {
		if titleText.Text == next {
			return
		}
		titleText.Text = next
		titleText.Refresh()
	}
}

// NewDeviceDashboardRowSeparator is a thin divider between two rows inside
// a card's own list -- inset a little from the card's own left/right edge
// (unlike the card's own header separator, which spans it fully), so it
// reads as a list separator rather than another full-width rule.
func NewDeviceDashboardRowSeparator() fyne.CanvasObject {
	sep := canvas.NewRectangle(deviceDashboardCardSep)
	sep.SetMinSize(fyne.NewSize(0, 1))
	return NewInsetExact(sep, 6, 6, 0, 0)
}

// NewDeviceDashboardCardGap is blank (no divider line) breathing room
// between two stacked cards in the same column -- a plain VBox's own
// default theme.Padding() gap read as the cards sitting flush against
// each other.
func NewDeviceDashboardCardGap() fyne.CanvasObject {
	spacer := canvas.NewRectangle(color.Transparent)
	spacer.SetMinSize(fyne.NewSize(0, 10))
	return spacer
}

// NewDeviceDashboardRow is one compact device line inside a dashboard card:
// its type icon (see controller/disk_widget_dashboard.go's driveIconResource
// -- a folder/disc/SD-card/keyboard/... glyph matching the drive's own
// kind, already brightened when active, same as the old list's own per-row
// icon), the device's display name (brighter when active), and an optional
// connect/disconnect button trailing on the right. connectBtn is nil for
// device kinds this dashboard doesn't drive mount/unmount for (Video,
// Audio -- see disk_widget_mount.go's own IsVideo/IsAudio exclusions).
func NewDeviceDashboardRow(icon fyne.Resource, name string, active bool, connectBtn fyne.CanvasObject) fyne.CanvasObject {
	return newDeviceDashboardRowWithActiveColor(icon, name, active, connectBtn, design.ColorAccent)
}

// NewDeviceDashboardTealRow is NewDeviceDashboardRow with HID's turquoise
// (#41e0c3, design.ColorConnectionBadgeText) as the mounted name color --
// extras (a mode picker, then the on/off toggle) trail on the right.
func NewDeviceDashboardTealRow(icon fyne.Resource, name string, active bool, extras ...fyne.CanvasObject) fyne.CanvasObject {
	left := newDeviceDashboardRowLeftColored(icon, name, rowNameColor(active, design.ColorConnectionBadgeText))
	var parts []fyne.CanvasObject
	for _, extra := range extras {
		if extra != nil {
			parts = append(parts, extra)
		}
	}
	var right fyne.CanvasObject
	if len(parts) > 0 {
		right = container.New(&DeviceRowControlsLayout{Gap: 8}, parts...)
	}
	row := container.NewBorder(nil, nil, left, right)
	return NewInsetExact(row, 0, 0, 2, 2)
}

// NewDeviceDashboardHIDCell is one half of the keyboard+mouse pair row --
// icon, name, then trailing extras (mouse: settings gear then toggle;
// keyboard: just the toggle) -- without the outer inset (the pair wrapper
// adds it once around both halves).
func NewDeviceDashboardHIDCell(icon fyne.Resource, name string, active bool, extras ...fyne.CanvasObject) fyne.CanvasObject {
	left := newDeviceDashboardRowLeftColored(icon, name, rowNameColor(active, design.ColorConnectionBadgeText))
	var parts []fyne.CanvasObject
	for _, extra := range extras {
		if extra != nil {
			parts = append(parts, extra)
		}
	}
	var right fyne.CanvasObject
	if len(parts) > 0 {
		right = container.New(&DeviceRowControlsLayout{Gap: 6}, parts...)
	}
	return container.NewBorder(nil, nil, left, right)
}

// NewDeviceDashboardHIDPairRow puts keyboard and mouse on one line with a
// short vertical rule between them. Either half may be nil.
func NewDeviceDashboardHIDPairRow(left, right fyne.CanvasObject) fyne.CanvasObject {
	if left == nil && right == nil {
		return canvas.NewRectangle(color.Transparent)
	}
	if left == nil {
		return NewInsetExact(right, 0, 0, 2, 2)
	}
	if right == nil {
		return NewInsetExact(left, 0, 0, 2, 2)
	}
	sep := canvas.NewRectangle(deviceDashboardCardSep)
	sep.SetMinSize(fyne.NewSize(1, 16))
	return NewInsetExact(container.New(&deviceDashboardHIDSplitLayout{Gap: 8}, left, sep, right), 0, 0, 2, 2)
}

// NewDeviceDashboardLimeRow is NewDeviceDashboardRow with this dashboard's
// own lime (#c4e77a) as the mounted name color -- Network uses it so a
// mounted bridge matches the card title icon, same lime Storage already
// uses (see newDeviceDashboardRowLeftSized). chip, when set, is the same
// under-name plaque Storage/Backups use for size -- Network puts "RNDIS"
// there instead of leaving it in the title. extras trail on the right
// (RNDIS settings gear, then the on/off toggle).
func NewDeviceDashboardLimeRow(icon fyne.Resource, name string, active bool, chip string, extras ...fyne.CanvasObject) fyne.CanvasObject {
	var left fyne.CanvasObject
	if strings.TrimSpace(chip) != "" {
		left = newDeviceDashboardRowLeftSized(icon, name, active, chip)
	} else {
		left = newDeviceDashboardRowLeftColored(icon, name, rowNameColor(active, DeviceDashboardAccentLime))
	}
	var parts []fyne.CanvasObject
	for _, extra := range extras {
		if extra != nil {
			parts = append(parts, extra)
		}
	}
	var right fyne.CanvasObject
	if len(parts) > 0 {
		right = container.New(&DeviceRowControlsLayout{Gap: 8}, parts...)
	}
	row := container.NewBorder(nil, nil, left, right)
	return NewInsetExact(row, 0, 0, 2, 2)
}

// NewDeviceDashboardVideoRow is a Video Pipe row: wrapping name, an optional
// latency chip under it (turquoise "Ultra Low Latency" on USB 3, gray
// "Medium" on USB 2), and trailing extras (settings gear, then the round
// exclusive radio). Active name uses the same turquoise as Audio/HID when
// the capture radio is on; off stays ColorTextLight.
func NewDeviceDashboardVideoRow(icon fyne.Resource, name string, active bool, chipText string, tealChip bool, extras ...fyne.CanvasObject) fyne.CanvasObject {
	nameColor := rowNameColor(active, design.ColorConnectionBadgeText)
	var chip fyne.CanvasObject
	if strings.TrimSpace(chipText) != "" {
		if tealChip {
			chip = newConnectionPlatformChipColored(chipText, 7, design.ColorConnectionBadgeText, design.ColorConnectionBadgeText)
		} else {
			chip = newConnectionPlatformChipSized(chipText, 7)
		}
	}
	return newDeviceDashboardFlexibleRow(icon, name, nameColor, chip, extras...)
}

// NewDeviceDashboardAudioRow is an Audio Pipeline row: wrapping name (capture
// device titles routinely overflow the narrow left column), then trailing
// extras -- USB Audio Codec gets the UAC1/UAC2 picker, every audio row
// gets the exclusive round radio. Active name uses HID/Video/Audio
// turquoise, matching the card title icon.
func NewDeviceDashboardAudioRow(icon fyne.Resource, name string, active bool, extras ...fyne.CanvasObject) fyne.CanvasObject {
	return newDeviceDashboardFlexibleRow(icon, name, rowNameColor(active, design.ColorConnectionBadgeText), nil, extras...)
}

// newDeviceDashboardFlexibleRow is icon | wrapping name[+chip] | extras --
// the name sits in the Border center so it receives leftover width and
// wraps instead of pushing the card wider (canvas.Text as a Border left
// child reports its full unwrapped MinSize).
func newDeviceDashboardFlexibleRow(icon fyne.Resource, name string, nameColor color.Color, chip fyne.CanvasObject, extras ...fyne.CanvasObject) fyne.CanvasObject {
	nameText := newDeviceDashboardWrapText(name, nameColor)
	var center fyne.CanvasObject = nameText
	if chip != nil {
		chipRow := container.New(&DeviceRowControlsLayout{Gap: 0}, chip)
		center = container.New(&tightStatsVBoxLayout{Gap: 2}, nameText, chipRow)
	}
	var left fyne.CanvasObject
	if icon != nil {
		iconImg := canvas.NewImageFromResource(icon)
		iconImg.FillMode = canvas.ImageFillContain
		iconImg.SetMinSize(fyne.NewSize(14, 14))
		left = NewInsetExact(container.NewCenter(iconImg), 0, 8, 0, 0)
	}
	var parts []fyne.CanvasObject
	for _, extra := range extras {
		if extra != nil {
			parts = append(parts, extra)
		}
	}
	var right fyne.CanvasObject
	if len(parts) > 0 {
		right = container.New(&DeviceRowControlsLayout{Gap: 8}, parts...)
	}
	// Border stretches the name to the row's height (set by the radio /
	// settings gear); wrap text would otherwise sit at y=0 and read as
	// shifted up on a short one-line title. Center the name block in
	// that leftover height, same as the icon and radio already do.
	center = container.New(&deviceDashboardVCenterLayout{}, center)
	return NewInsetExact(container.NewBorder(nil, nil, left, right, center), 0, 0, 2, 2)
}

// deviceDashboardVCenterLayout gives its one child the full width it was
// assigned and the child's own MinSize height, then vertically centers
// that block -- used so a short Audio/Video name lines up with the row's
// icon and round radio.
type deviceDashboardVCenterLayout struct{}

func (*deviceDashboardVCenterLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	if len(objects) == 0 {
		return fyne.NewSize(1, 0)
	}
	s := objects[0].MinSize()
	if s.Width < 1 {
		s.Width = 1
	}
	return s
}

func (*deviceDashboardVCenterLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) == 0 {
		return
	}
	o := objects[0]
	h := o.MinSize().Height
	if h > size.Height {
		h = size.Height
	}
	y := (size.Height - h) / 2
	if y < 0 {
		y = 0
	}
	o.Move(fyne.NewPos(0, y))
	o.Resize(fyne.NewSize(size.Width, h))
}

const dashboardWrapMaxLines = 3

// deviceDashboardWrapText is an 11px name that wraps to the width it is
// given (up to dashboardWrapMaxLines, then ellipsizes) -- widget.Label's
// own wrapping still reports the full unwrapped width as MinSize, which
// is what overflowed the Audio card.
type deviceDashboardWrapText struct {
	widget.BaseWidget
	text     string
	color    color.Color
	textSize float32
}

func newDeviceDashboardWrapText(text string, col color.Color) *deviceDashboardWrapText {
	t := &deviceDashboardWrapText{text: text, color: col, textSize: 11}
	t.ExtendBaseWidget(t)
	return t
}

func (t *deviceDashboardWrapText) MinSize() fyne.Size {
	h := fyne.MeasureText("Ag", t.textSize, fyne.TextStyle{}).Height
	if h < 1 {
		h = 14
	}
	n := len(wrapDashboardName(t.text, t.textSize, t.Size().Width))
	if n < 1 {
		n = 1
	}
	return fyne.NewSize(1, float32(n)*h)
}

func (t *deviceDashboardWrapText) Resize(size fyne.Size) {
	prev := t.Size()
	t.BaseWidget.Resize(size)
	if prev.Width != size.Width {
		t.Refresh()
	}
}

func (t *deviceDashboardWrapText) CreateRenderer() fyne.WidgetRenderer {
	r := &deviceDashboardWrapTextRenderer{t: t}
	for i := range r.lines {
		ln := canvas.NewText("", t.color)
		ln.TextSize = t.textSize
		r.lines[i] = ln
	}
	return r
}

type deviceDashboardWrapTextRenderer struct {
	t     *deviceDashboardWrapText
	lines [dashboardWrapMaxLines]*canvas.Text
}

func (r *deviceDashboardWrapTextRenderer) Layout(size fyne.Size) {
	r.apply(size.Width)
}

func (r *deviceDashboardWrapTextRenderer) apply(width float32) {
	parts := wrapDashboardName(r.t.text, r.t.textSize, width)
	h := fyne.MeasureText("Ag", r.t.textSize, fyne.TextStyle{}).Height
	y := float32(0)
	for i, ln := range r.lines {
		if i < len(parts) {
			ln.Text = parts[i]
			ln.Color = r.t.color
			ln.TextSize = r.t.textSize
			ln.Show()
			ln.Move(fyne.NewPos(0, y))
			ln.Resize(ln.MinSize())
			ln.Refresh()
			y += h
		} else {
			ln.Hide()
		}
	}
}

func (r *deviceDashboardWrapTextRenderer) MinSize() fyne.Size {
	return r.t.MinSize()
}

func (r *deviceDashboardWrapTextRenderer) Refresh() {
	r.apply(r.t.Size().Width)
	canvas.Refresh(r.t)
}

func (r *deviceDashboardWrapTextRenderer) BackgroundColor() color.Color {
	return color.Transparent
}

func (r *deviceDashboardWrapTextRenderer) Objects() []fyne.CanvasObject {
	objs := make([]fyne.CanvasObject, 0, dashboardWrapMaxLines)
	for _, ln := range r.lines {
		objs = append(objs, ln)
	}
	return objs
}

func (r *deviceDashboardWrapTextRenderer) Destroy() {}

func wrapDashboardName(text string, textSize, width float32) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return []string{""}
	}
	if width <= 8 {
		return []string{text}
	}
	style := fyne.TextStyle{}
	fit := func(s string) bool {
		return fyne.MeasureText(s, textSize, style).Width <= width
	}
	runes := []rune(text)
	var lines []string
	start := 0
	for start < len(runes) {
		best := start + 1
		lastSpace := -1
		for end := start + 1; end <= len(runes); end++ {
			if !fit(string(runes[start:end])) {
				break
			}
			best = end
			if runes[end-1] == ' ' {
				lastSpace = end
			}
		}
		cut := best
		if cut < len(runes) && lastSpace > start {
			cut = lastSpace
		}
		line := strings.TrimSpace(string(runes[start:cut]))
		if line == "" {
			line = string(runes[start:cut])
		}
		lines = append(lines, line)
		start = cut
		for start < len(runes) && runes[start] == ' ' {
			start++
		}
		if len(lines) >= dashboardWrapMaxLines {
			break
		}
	}
	if start < len(runes) && len(lines) > 0 {
		last := lines[len(lines)-1]
		lastRunes := []rune(last)
		for len(lastRunes) > 1 && !fit(string(lastRunes)+"…") {
			lastRunes = lastRunes[:len(lastRunes)-1]
		}
		lines[len(lines)-1] = string(lastRunes) + "…"
	}
	if len(lines) == 0 {
		return []string{text}
	}
	return lines
}

// NewDeviceDashboardCaptureSelector is Video/Audio's exclusive round radio
// -- the same CaptureSelector the old list used, recolored to this
// dashboard's turquoise and wired to the card's hover cell.
func NewDeviceDashboardCaptureSelector(selected, disabled bool, onTap func(), onHover func(bool)) *CaptureSelector {
	s := NewCaptureSelector(onTap)
	s.ActiveColor = design.ColorConnectionBadgeText
	s.OnHover = onHover
	s.SetSelected(selected)
	s.SetDisabled(disabled)
	return s
}

func newDeviceDashboardRowWithActiveColor(icon fyne.Resource, name string, active bool, connectBtn fyne.CanvasObject, activeColor color.Color) fyne.CanvasObject {
	left := newDeviceDashboardRowLeftColored(icon, name, rowNameColor(active, activeColor))
	row := container.NewBorder(nil, nil, left, connectBtn)
	return NewInsetExact(row, 0, 0, 2, 2)
}

func rowNameColor(active bool, activeColor color.Color) color.Color {
	if active {
		return activeColor
	}
	return design.ColorTextLight
}

// newDeviceDashboardRowLeftColored is the icon+name composition with an
// explicit name color -- HID/Audio/Video use turquoise
// (design.ColorConnectionBadgeText); Storage/Network use
// DeviceDashboardAccentLime instead.
func newDeviceDashboardRowLeftColored(icon fyne.Resource, name string, nameColor color.Color) fyne.CanvasObject {
	nameText := canvas.NewText(name, nameColor)
	nameText.TextSize = 11

	if icon == nil {
		return nameText
	}

	iconImg := canvas.NewImageFromResource(icon)
	iconImg.FillMode = canvas.ImageFillContain
	iconImg.SetMinSize(fyne.NewSize(14, 14))

	return container.New(&DeviceRowControlsLayout{Gap: 8}, iconImg, nameText)
}

// newDeviceDashboardRowLeftSized is a Storage row's own icon+name -- with an
// optional size badge, in which case the icon sits once, vertically
// centered, to the left of a two-line name/size column (not repeated above
// each line the way newDeviceDashboardRowLeft's own single-line icon+name
// row would read if just stacked with a badge underneath). sizeText empty
// means no badge (e.g. the drive's own size isn't known) -- falls back to
// the plain single-line row.
func newDeviceDashboardRowLeftSized(icon fyne.Resource, name string, active bool, sizeText string) fyne.CanvasObject {
	return newDeviceDashboardRowLeftChips(icon, name, active, sizeText)
}

// newDeviceDashboardRowLeftChips is newDeviceDashboardRowLeftSized with
// one or more under-name plaques (snapshot count + flash size on Backups).
func newDeviceDashboardRowLeftChips(icon fyne.Resource, name string, active bool, chips ...string) fyne.CanvasObject {
	nameColor := rowNameColor(active, DeviceDashboardAccentLime)
	var chipObjs []fyne.CanvasObject
	for _, text := range chips {
		if strings.TrimSpace(text) == "" {
			continue
		}
		chipObjs = append(chipObjs, newConnectionPlatformChipSized(text, 7))
	}
	if len(chipObjs) == 0 {
		return newDeviceDashboardRowLeftColored(icon, name, nameColor)
	}
	if len(chipObjs) == 1 {
		return newDeviceDashboardRowLeftWithChip(icon, name, nameColor, chipObjs[0])
	}
	return newDeviceDashboardRowLeftWithChip(icon, name, nameColor, container.New(&DeviceRowControlsLayout{Gap: 4}, chipObjs...))
}

// newDeviceDashboardRowLeftWithChip is icon + name with an already-built
// chip under the name -- Video's latency plaque (turquoise or gray) and
// Storage/Network's size/RNDIS chips all go through here.
func newDeviceDashboardRowLeftWithChip(icon fyne.Resource, name string, nameColor color.Color, chip fyne.CanvasObject) fyne.CanvasObject {
	nameText := canvas.NewText(name, nameColor)
	nameText.TextSize = 11
	chipRow := container.New(&DeviceRowControlsLayout{Gap: 0}, chip)
	textColumn := container.New(&tightStatsVBoxLayout{Gap: 2}, nameText, chipRow)
	if icon == nil {
		return textColumn
	}
	iconImg := canvas.NewImageFromResource(icon)
	iconImg.FillMode = canvas.ImageFillContain
	iconImg.SetMinSize(fyne.NewSize(14, 14))
	return container.New(&DeviceRowControlsLayout{Gap: 8}, iconImg, textColumn)
}

// NewDeviceDashboardEmptyState is the muted placeholder line a dashboard
// card shows in place of its rows when it currently has no devices.
func NewDeviceDashboardEmptyState(text string) fyne.CanvasObject {
	label := canvas.NewText(text, videoDialogHintColor)
	label.TextSize = 10
	return NewInset(label, 0, 0, 6, 6)
}

// deviceToggleWidth/Height is the pill track's own footprint; deviceToggleKnob
// is the round knob sliding inside it, deviceToggleInset its resting gap
// from the track's own edge. Kept a little under a typical 30x16 iOS-style
// switch so it sits quieter next to an 11px row name.
const (
	deviceToggleWidth  = float32(26)
	deviceToggleHeight = float32(14)
	deviceToggleKnob   = float32(10)
	deviceToggleInset  = float32(2)
)

// DeviceToggle is a small on/off pill switch for a HID/Network row's own
// mount/unmount action -- Storage rows use a proper button instead (see
// NewDeviceDashboardMountButton/NewDeviceDashboardDisconnectButton), but
// HID/Network keep this switch. Fyne has no built-in switch widget
// (widget.Check is a checkbox).
type DeviceToggle struct {
	widget.BaseWidget

	Active    bool
	OnChanged func(bool)
	// OnHover, when set, is called with the pointer's hover state -- wire
	// it to a card's own onHover cell (NewDeviceDashboardHoverCell) so
	// hovering this toggle keeps that card's border teal instead of
	// flickering it off and back on.
	OnHover func(bool)
	// ActiveFill is the on-state track color. Nil keeps HID's teal
	// (design.ColorConnectionBadgeText); Network sets DeviceDashboardAccentLime.
	ActiveFill color.Color

	disabled bool
	hovered  bool

	track *canvas.Rectangle
	knob  *canvas.Circle
}

// NewDeviceToggle builds a switch reflecting active, calling onChanged with
// the new state whenever tapped (unless later disabled via SetEnabled).
func NewDeviceToggle(active bool, onChanged func(bool)) *DeviceToggle {
	t := &DeviceToggle{Active: active, OnChanged: onChanged}
	t.ExtendBaseWidget(t)
	return t
}

func (t *DeviceToggle) SetActive(active bool) {
	if t.Active == active {
		return
	}
	t.Active = active
	t.Refresh()
}

func (t *DeviceToggle) SetEnabled(enabled bool) {
	if t.disabled == !enabled {
		return
	}
	t.disabled = !enabled
	t.Refresh()
}

func (t *DeviceToggle) Tapped(*fyne.PointEvent) {
	if t.disabled {
		return
	}
	// Don't flip Active here -- the dashboard rebuilds this switch from
	// IsMounted, and a speculative flip (then a refresh while the gadget
	// is still connecting) is what made keyboard/mouse twitch on then off.
	// OnChanged starts the mount/unmount; the switch stays put until the
	// operation actually finishes.
	if t.OnChanged != nil {
		t.OnChanged(!t.Active)
	}
}

func (t *DeviceToggle) TappedSecondary(*fyne.PointEvent) {}

func (t *DeviceToggle) MouseIn(*desktop.MouseEvent) {
	if !t.disabled {
		t.hovered = true
		t.Refresh()
	}
	if t.OnHover != nil {
		t.OnHover(true)
	}
}

func (t *DeviceToggle) MouseMoved(*desktop.MouseEvent) {}

func (t *DeviceToggle) MouseOut() {
	t.hovered = false
	t.Refresh()
	if t.OnHover != nil {
		t.OnHover(false)
	}
}

func (t *DeviceToggle) Cursor() desktop.Cursor {
	if t.disabled {
		return desktop.DefaultCursor
	}
	return desktop.PointerCursor
}

func (t *DeviceToggle) MinSize() fyne.Size {
	return fyne.NewSize(deviceToggleWidth, deviceToggleHeight)
}

func (t *DeviceToggle) CreateRenderer() fyne.WidgetRenderer {
	t.track = canvas.NewRectangle(color.Transparent)
	t.track.CornerRadius = deviceToggleHeight / 2
	t.knob = canvas.NewCircle(color.White)
	r := &deviceToggleRenderer{t: t}
	r.applyColors()
	return r
}

type deviceToggleRenderer struct {
	t *DeviceToggle
}

func (r *deviceToggleRenderer) Layout(fyne.Size) {
	t := r.t
	t.track.Move(fyne.NewPos(0, 0))
	t.track.Resize(fyne.NewSize(deviceToggleWidth, deviceToggleHeight))

	knobY := (deviceToggleHeight - deviceToggleKnob) / 2
	knobX := deviceToggleInset
	if t.Active {
		knobX = deviceToggleWidth - deviceToggleKnob - deviceToggleInset
	}
	t.knob.Move(fyne.NewPos(knobX, knobY))
	t.knob.Resize(fyne.NewSize(deviceToggleKnob, deviceToggleKnob))
}

func (r *deviceToggleRenderer) applyColors() {
	t := r.t
	switch {
	case t.disabled:
		t.track.FillColor = color.NRGBA{R: 0x22, G: 0x26, B: 0x2a, A: 0xff}
		t.knob.FillColor = color.NRGBA{R: 0x55, G: 0x58, B: 0x52, A: 0xff}
	case t.Active:
		fill := t.ActiveFill
		if fill == nil {
			fill = design.ColorConnectionBadgeText
		}
		t.track.FillColor = fill
		t.knob.FillColor = design.ColorGray950
	default:
		fill := color.Color(color.NRGBA{R: 0x22, G: 0x26, B: 0x2a, A: 0xff})
		if t.hovered {
			fill = color.NRGBA{R: 0x2c, G: 0x30, B: 0x34, A: 0xff}
		}
		t.track.FillColor = fill
		t.knob.FillColor = videoDialogHintColor
	}
}

func (r *deviceToggleRenderer) MinSize() fyne.Size {
	return r.t.MinSize()
}

func (r *deviceToggleRenderer) Refresh() {
	r.applyColors()
	r.Layout(r.t.Size())
	canvas.Refresh(r.t)
}

func (r *deviceToggleRenderer) BackgroundColor() color.Color {
	return color.Transparent
}

func (r *deviceToggleRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.t.track, r.t.knob}
}

func (r *deviceToggleRenderer) Destroy() {}

// DeviceDashboardStorageIconSVG is the Storage card's own SSD glyph,
// recolored to #c4e77a (must match DeviceDashboardAccentLime below --
// SVG resources can't reference a Go color value).
var DeviceDashboardStorageIconSVG = fyne.NewStaticResource("device_dashboard_ssd.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" fill="#c4e77a" viewBox="0 0 24 24"><path fill-rule="evenodd" clip-rule="evenodd" d="M5.56216 2.87174C6.14861 2.41455 6.82355 2.25 7.5 2.25H16.5C17.1765 2.25 17.8514 2.41455 18.4378 2.87174C19.0172 3.32344 19.4352 4.0024 19.7154 4.89243L19.7225 4.91513L22.2604 15.2159C22.5738 15.8383 22.75 16.5463 22.75 17.2941C22.75 19.7141 20.887 21.75 18.5 21.75H5.5C3.11298 21.75 1.25 19.7141 1.25 17.2941C1.25 16.5463 1.42621 15.8383 1.73961 15.2159L4.27747 4.91513L4.28461 4.89243C4.56481 4.0024 4.98276 3.32344 5.56216 2.87174ZM3.77626 13.2197C4.30106 12.9752 4.88373 12.8382 5.5 12.8382H18.5C19.1163 12.8382 19.6989 12.9752 20.2237 13.2197L18.2777 5.32094C18.0589 4.63669 17.7822 4.26258 17.5156 4.05473C17.2532 3.85015 16.9281 3.75 16.5 3.75H7.5C7.07188 3.75 6.74682 3.85015 6.48441 4.05473C6.21779 4.26258 5.94107 4.63669 5.72234 5.32094L3.77626 13.2197ZM5.5 14.3382C4.49271 14.3382 3.59139 14.9242 3.10912 15.8329C2.88147 16.2618 2.75 16.7597 2.75 17.2941C2.75 18.9676 4.02103 20.25 5.5 20.25H18.5C19.979 20.25 21.25 18.9676 21.25 17.2941C21.25 16.7597 21.1185 16.2618 20.8909 15.8329C20.4086 14.9242 19.5073 14.3382 18.5 14.3382H5.5ZM10.5 16.25C10.9142 16.25 11.25 16.5858 11.25 17V18C11.25 18.4142 10.9142 18.75 10.5 18.75C10.0858 18.75 9.75 18.4142 9.75 18V17C9.75 16.5858 10.0858 16.25 10.5 16.25ZM13 16.25C13.4142 16.25 13.75 16.5858 13.75 17V18C13.75 18.4142 13.4142 18.75 13 18.75C12.5858 18.75 12.25 18.4142 12.25 18V17C12.25 16.5858 12.5858 16.25 13 16.25ZM15.5 16.25C15.9142 16.25 16.25 16.5858 16.25 17V18C16.25 18.4142 15.9142 18.75 15.5 18.75C15.0858 18.75 14.75 18.4142 14.75 18V17C14.75 16.5858 15.0858 16.25 15.5 16.25ZM18 16.25C18.4142 16.25 18.75 16.5858 18.75 17V18C18.75 18.4142 18.4142 18.75 18 18.75C17.5858 18.75 17.25 18.4142 17.25 18V17C17.25 16.5858 17.5858 16.25 18 16.25Z"/></svg>`))

// DeviceDashboardAccentLime is the lime accent used for the Storage card's
// SSD icon and as its "Mount New ISO" header button's own fill -- must
// match the hex inlined into DeviceDashboardStorageIconSVG above.
var DeviceDashboardAccentLime = color.NRGBA{R: 0xc4, G: 0xe7, B: 0x7a, A: 0xff}

// deviceDashboardAccentLimeHover is DeviceDashboardAccentLime lightened,
// for a header button's own hover fill.
var deviceDashboardAccentLimeHover = color.NRGBA{R: 0xd9, G: 0xf2, B: 0xa3, A: 0xff}

// DeviceDashboardFolderIconActive/DiscIconActive/SDCardIconActive recolor
// the shared assets.FolderIconActive/DiscIconActive/SDCardIconActive
// (#93C572, design.ColorAccent's green) to this card's own lime accent
// (#c4e77a) instead -- driveIconResource (disk_widget_dashboard.go) uses
// these for a mounted Storage row's icon, matching the lime its name text
// already uses (see newDeviceDashboardRowLeftSized). The shared assets
// themselves are left alone since the old list view (disk_widget_row.go's
// configureDriveRow) still uses them with the original green.
var DeviceDashboardFolderIconActive = fyne.NewStaticResource("device_dashboard_folder_active.svg", []byte(strings.ReplaceAll(string(assets.FolderIconActive.Content()), "#93C572", "#c4e77a")))
var DeviceDashboardDiscIconActive = fyne.NewStaticResource("device_dashboard_disc_active.svg", []byte(strings.ReplaceAll(string(assets.DiscIconActive.Content()), "#93C572", "#c4e77a")))
var DeviceDashboardSDCardIconActive = fyne.NewStaticResource("device_dashboard_sdcard_active.svg", []byte(strings.ReplaceAll(string(assets.SDCardIconActive.Content()), "#93C572", "#c4e77a")))

// DeviceDashboardHIDIconSVG/VideoIconSVG/AudioIconSVG are those three
// cards' own title glyphs, recolored to turquoise (#41e0c3,
// design.ColorConnectionBadgeText) -- HID/Video/Audio share that accent,
// distinct from Storage/Network/Backups' lime.
var DeviceDashboardHIDIconSVG = fyne.NewStaticResource("device_dashboard_hid.svg", []byte(strings.ReplaceAll(string(assets.KeyboardIcon.Content()), "#C9C9C9", "#41e0c3")))
var DeviceDashboardVideoIconSVG = fyne.NewStaticResource("device_dashboard_video.svg", []byte(strings.ReplaceAll(string(assets.MonitorTabIcon.Content()), "#F5F5F5", "#41e0c3")))
var DeviceDashboardAudioIconSVG = fyne.NewStaticResource("device_dashboard_audio.svg", []byte(strings.ReplaceAll(string(assets.AudioIcon.Content()), "#C9C9C9", "#41e0c3")))

// DeviceDashboardKeyboardIconActive/MouseIconActive/GamepadIconActive/
// AudioIconActive/CameraIconActive are a mounted HID/Audio/Video row's
// own icons -- the shared *Active green (#93C572) recolored to turquoise,
// matching the row's name text.
var DeviceDashboardKeyboardIconActive = fyne.NewStaticResource("device_dashboard_keyboard_active.svg", []byte(strings.ReplaceAll(string(assets.KeyboardIconActive.Content()), "#93C572", "#41e0c3")))
var DeviceDashboardMouseIconActive = fyne.NewStaticResource("device_dashboard_mouse_active.svg", []byte(strings.ReplaceAll(string(assets.MouseIconActive.Content()), "#93C572", "#41e0c3")))
var DeviceDashboardGamepadIconActive = fyne.NewStaticResource("device_dashboard_gamepad_active.svg", []byte(strings.ReplaceAll(string(assets.GamepadIconActive.Content()), "#93C572", "#41e0c3")))
var DeviceDashboardAudioIconActive = fyne.NewStaticResource("device_dashboard_audio_active.svg", []byte(strings.ReplaceAll(string(assets.AudioIconActive.Content()), "#93C572", "#41e0c3")))
var DeviceDashboardCameraIconActive = fyne.NewStaticResource("device_dashboard_camera_active.svg", []byte(strings.ReplaceAll(string(assets.CameraIconActive.Content()), "#93C572", "#41e0c3")))

// DeviceDashboardNetworkIconSVG/DeviceDashboardBackupsIconSVG are those
// two cards' own title glyphs, recolored to #c4e77a to match Storage's
// SSD icon (DeviceDashboardStorageIconSVG). Network uses language-svgrepo-
// com.svg -- a filled globe that already renders correctly as the header
// language glyph (the dedicated network-backup globe filled solid at this
// size). Backups uses SnapshotsTabIcon (the floppy the Snapshots tab uses).
var DeviceDashboardNetworkIconSVG = fyne.NewStaticResource("device_dashboard_network.svg", []byte(strings.ReplaceAll(string(assets.LanguageIcon.Content()), "#F5F5F5", "#c4e77a")))
var DeviceDashboardBackupsIconSVG = fyne.NewStaticResource("device_dashboard_backups.svg", []byte(strings.ReplaceAll(string(assets.SnapshotsTabIcon.Content()), "#F5F5F5", "#c4e77a")))

// DeviceDashboardNetworkIconActive is a mounted Network row's own icon --
// assets.NetworkIconActive's #93C572 recolored to this dashboard's lime,
// matching the row's name text (see NewDeviceDashboardLimeRow).
var DeviceDashboardNetworkIconActive = fyne.NewStaticResource("device_dashboard_network_active.svg", []byte(strings.ReplaceAll(string(assets.NetworkIconActive.Content()), "#93C572", "#c4e77a")))

// deviceDashboardHeaderButtonBusyFill is DeviceDashboardAccentLime
// darkened -- a header button's own fill (e.g. "Mount New ISO") and a
// Storage/Backups Connect button's fill while its action is in flight.
var deviceDashboardHeaderButtonBusyFill = color.NRGBA{R: 0x75, G: 0x8a, B: 0x49, A: 0xff}

// DeviceDashboardHeaderButtonTextColor is the dark olive-green text/icon
// color read against a header button's own bright lime fill -- the same
// #4c6803 the Connections table's own Connect button uses for its label
// (connection_list_table.go). Exported so a caller building its own icon
// glyph (e.g. NewDeviceDashboardPlusGlyph) for a header button can match
// it.
var DeviceDashboardHeaderButtonTextColor = color.NRGBA{R: 0x4c, G: 0x68, B: 0x03, A: 0xff}

// NewDeviceDashboardPlusGlyph draws a "+" from two plain rectangles rather
// than an SVG resource -- an SVG circle+plus icon here rendered either
// fully invisible or blending into the button's own fill across two
// separate attempts (see this repo's own history); a shape this simple
// doesn't need SVG parsing at all, which sidesteps whatever the actual
// cause was. A fixed, thin 1.2px bar -- size/5 (2px at size 10) still
// read as bold.
func NewDeviceDashboardPlusGlyph(size float32, col color.Color) fyne.CanvasObject {
	thickness := float32(1.2)
	h := canvas.NewRectangle(col)
	h.SetMinSize(fyne.NewSize(size, thickness))
	v := canvas.NewRectangle(col)
	v.SetMinSize(fyne.NewSize(thickness, size))
	return container.NewStack(container.NewCenter(h), container.NewCenter(v))
}

// DeviceDashboardHeaderButton is a small, icon+label tappable pill for a
// dashboard card's own header (e.g. Storage's "Mount New ISO") -- filled
// with accent (dark text/icon on top, matching the app's own bright-pill
// button convention), lightening slightly on hover. Shorter and
// independently colorable per card, unlike view.DeviceActionButton (used
// elsewhere for the footer's compact mount/unmount buttons), which has no
// exported way to customize either without changing those other call
// sites too.
type DeviceDashboardHeaderButton struct {
	widget.BaseWidget

	text     string
	icon     fyne.CanvasObject
	accent   color.Color
	onTap    func()
	hovered  bool
	busy     bool
	disabled bool

	// OnHover, when set, is called with the pointer's hover state -- see
	// DeviceToggle.OnHover's own doc comment.
	OnHover func(bool)

	bg *canvas.Rectangle
}

// NewDeviceDashboardHeaderButton builds a header button filled with
// accent, showing icon (may be nil, e.g. from NewDeviceDashboardPlusGlyph)
// before text, calling onTap when tapped.
func NewDeviceDashboardHeaderButton(text string, icon fyne.CanvasObject, accent color.Color, onTap func()) *DeviceDashboardHeaderButton {
	b := &DeviceDashboardHeaderButton{text: text, icon: icon, accent: accent, onTap: onTap}
	b.ExtendBaseWidget(b)
	return b
}

func (b *DeviceDashboardHeaderButton) Tapped(*fyne.PointEvent) {
	if b.disabled || b.busy {
		return
	}
	if b.onTap != nil {
		b.onTap()
	}
}

// SetEnabled greys the header pill and swallows taps -- used while a
// mount/unmount is in flight (see DiskWidget.controlsLocked).
func (b *DeviceDashboardHeaderButton) SetEnabled(enabled bool) {
	disabled := !enabled
	if b.disabled == disabled {
		return
	}
	b.disabled = disabled
	b.refreshVisuals()
}

// SetBusy darkens the button while its own action is in flight (e.g. the
// OS file picker is open for "Mount New ISO") -- purely visual, doesn't
// disable tapping; the caller is expected to already guard re-entrancy
// itself (handleAddImage's own imagePickerInFlight).
func (b *DeviceDashboardHeaderButton) SetBusy(busy bool) {
	if b.busy == busy {
		return
	}
	b.busy = busy
	b.refreshVisuals()
}

func (b *DeviceDashboardHeaderButton) TappedSecondary(*fyne.PointEvent) {}

func (b *DeviceDashboardHeaderButton) Cursor() desktop.Cursor {
	if b.disabled {
		return desktop.DefaultCursor
	}
	return desktop.PointerCursor
}

func (b *DeviceDashboardHeaderButton) MouseIn(*desktop.MouseEvent) {
	if b.disabled {
		return
	}
	b.hovered = true
	b.refreshVisuals()
	if b.OnHover != nil {
		b.OnHover(true)
	}
}

func (b *DeviceDashboardHeaderButton) MouseMoved(*desktop.MouseEvent) {}

func (b *DeviceDashboardHeaderButton) MouseOut() {
	b.hovered = false
	b.refreshVisuals()
	if b.OnHover != nil {
		b.OnHover(false)
	}
}

func (b *DeviceDashboardHeaderButton) refreshVisuals() {
	if b.bg == nil {
		return
	}
	fill := b.accent
	switch {
	case b.disabled:
		fill = deviceDashboardHeaderButtonBusyFill
	case b.busy:
		fill = deviceDashboardHeaderButtonBusyFill
	case b.hovered:
		fill = deviceDashboardAccentLimeHover
	}
	b.bg.FillColor = fill
	b.bg.Refresh()
}

func (b *DeviceDashboardHeaderButton) CreateRenderer() fyne.WidgetRenderer {
	b.bg = canvas.NewRectangle(b.accent)
	b.bg.CornerRadius = 6

	label := canvas.NewText(b.text, DeviceDashboardHeaderButtonTextColor)
	label.TextSize = 10

	var row fyne.CanvasObject = label
	if b.icon != nil {
		row = container.New(&DeviceRowControlsLayout{Gap: 6}, b.icon, label)
	}

	// 4px top/bottom padding -- shorter than view.DeviceActionButton's own
	// (unexported) padding, per this button's own "shorter" requirement.
	return widget.NewSimpleRenderer(container.NewStack(b.bg, NewInsetExact(row, 10, 10, 4, 4)))
}

// NewDeviceDashboardModePicker is a Storage row's own USB Stick/CD-ROM
// mode picker -- the same small teal pill dropdown style used elsewhere in
// this app (see e.g. newVideoDialogPicker in video_start_dialog.go).
func NewDeviceDashboardModePicker(options []string, selected string, onSelected func(string)) *HeaderDropdown {
	// Built empty, styled, *then* populated -- NewHeaderDropdown's own
	// updateMinWidth() runs once immediately at construction, using
	// whatever TextSize/UltraCompact it was constructed with (its own
	// defaults, TextSize 14, not UltraCompact). Passing options straight
	// into NewHeaderDropdown computed MinWidth from that default 14px
	// font, and setting TextSize afterward (a plain field assignment, not
	// a setter) never recomputed it -- MinWidth stayed stuck too wide
	// until something else later called SetSelected/SetOptions and
	// recomputed it against the real 9px size. SetOptions below runs
	// after every style field is already in place, so it gets it right
	// the first time -- the same order newVideoDialogPicker (video_start_dialog.go)
	// already uses.
	d := NewHeaderDropdown(nil, "", onSelected)
	d.UltraCompact = true
	d.CornerRadius = 6
	d.BorderColor = design.ColorTailscaleChipBorder
	d.TextColor = design.ColorConnectionBadgeText
	d.IconColor = deviceDashboardDescColor
	d.TextSize = 9
	d.HoverBorderColor = design.ColorConnectionBadgeText
	d.HoverFillColor = design.ColorGray900
	d.SetOptions(options)
	d.SetSelected(selected)
	return d
}

// deviceDashboardDeleteIconSVG is the exact trash glyph the Connections
// table's own delete button uses (connection_list_table.go) -- reused
// directly so this button matches it exactly, not Fyne's own
// theme.DeleteIcon(), which reads differently.
var deviceDashboardDeleteIconSVG = fyne.NewStaticResource("device_dashboard_delete.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="#c5c8b5"><path d="M6 19c0 1.1.9 2 2 2h8c1.1 0 2-.9 2-2V7H6v12zM19 4h-3.5l-1-1h-5l-1 1H5v2h14V4z"/></svg>`))

// NewDeviceDashboardDeleteButton matches the Connections table's own
// delete button (connection_list_table.go) exactly -- same icon, fill,
// border, and size -- so a Storage row's delete action reads as the same
// family instead of a one-off. onHover may be nil; see DeviceToggle.OnHover's
// own doc comment for what it's for.
func NewDeviceDashboardDeleteButton(onTap func(), onHover func(bool)) *iconChromeButton {
	return newIconChromeButton(iconChromeButtonSpec{
		NormalFill:   color.Transparent,
		HoverFill:    design.ColorSurfaceLight,
		DisabledFill: connectionActionBlockedFill,
		Stroke:       design.ColorTailscaleChipBorder,
		StrokeWidth:  1,
		CornerRadius: 6,
		NormalIcon:   deviceDashboardDeleteIconSVG,
		IconSize:     fyne.NewSize(11, 11),
		ButtonSize:   fyne.NewSize(23, 23),
		OnTapped:     onTap,
		OnHover:      onHover,
	})
}

// deviceDashboardUploadIconSVG recolors assets.UploadIcon to #c5c8b5, the
// same muted color deviceDashboardDeleteIconSVG's own fill uses --
// assets.UploadIcon's own baked-in #F5F5F5 read as a different, brighter
// color than the delete icon right next to it. Same recoloring technique
// connection_list_table.go's own connectIconColored already uses (grab
// the existing resource's content, replace its hex).
var deviceDashboardUploadIconSVG = fyne.NewStaticResource("device_dashboard_upload.svg", []byte(strings.ReplaceAll(string(assets.UploadIcon.Content()), "#F5F5F5", "#c5c8b5")))

// NewDeviceDashboardUploadButton is NewDeviceDashboardDeleteButton's own
// chrome (same fill/border/hover/size) with an upload glyph instead --
// Storage's own Upload action, styled as the same button family. onHover
// may be nil; see DeviceToggle.OnHover's own doc comment.
func NewDeviceDashboardUploadButton(onTap func(), onHover func(bool)) *iconChromeButton {
	return newIconChromeButton(iconChromeButtonSpec{
		NormalFill:   color.Transparent,
		HoverFill:    design.ColorSurfaceLight,
		DisabledFill: connectionActionBlockedFill,
		Stroke:       design.ColorTailscaleChipBorder,
		StrokeWidth:  1,
		CornerRadius: 6,
		NormalIcon:   deviceDashboardUploadIconSVG,
		HoverIcon:    deviceDashboardUploadIconSVG,
		DisabledIcon: assets.UploadIconMuted,
		IconSize:     fyne.NewSize(11, 11),
		ButtonSize:   fyne.NewSize(23, 23),
		OnTapped:     onTap,
		OnHover:      onHover,
	})
}

// deviceDashboardUploadProgressWidth is how wide Storage's own upload
// progress bar reads at -- shorter than ShowConnectingToast's own
// (dialog_helper.go's connectingProgressBar, which stretches to fill
// whatever width it's given); a fixed GridWrap here keeps it compact
// inside a row instead.
const deviceDashboardUploadProgressWidth = float32(50)

// NewDeviceDashboardUploadProgress is Storage's own upload-in-progress
// indicator -- reuses ShowConnectingToast's own teal-to-lime gradient
// progress bar (dialog_helper.go's connectingProgressBar) at a fixed,
// shorter width, plus a percentage label, shown in place of the mode
// picker/Delete/Upload controls while a file is actively uploading (see
// disk_widget_dashboard.go's buildStorageRowExtras).
func NewDeviceDashboardUploadProgress(progressPercent float64) fyne.CanvasObject {
	bar := newConnectingProgressBar()
	bar.SetProgress(float32(progressPercent / 100))
	barBox := container.NewGridWrap(fyne.NewSize(deviceDashboardUploadProgressWidth, connectingProgressBarHeight), bar)

	label := canvas.NewText(fmt.Sprintf("%.0f%%", progressPercent), deviceDashboardDescColor)
	label.TextSize = 9

	content := container.New(&DeviceRowControlsLayout{Gap: 6}, barBox, label)

	// Fixed-height sizer keeps this row's own MinSize height equal to a
	// normal button row's (23px, matching iconChromeButton's own
	// ButtonSize height) -- without it, switching a row between its
	// button state and this progress state changed the Storage card's
	// total content height just enough to toggle the dashboard's own
	// scrollbar on/off, which shrank the whole columns' available width
	// and made every card/row visibly narrower while a file uploaded.
	sizer := canvas.NewRectangle(color.Transparent)
	sizer.SetMinSize(fyne.NewSize(0, 23))
	return container.NewStack(sizer, content)
}

// deviceDashboardConnectIconSVG is the plug glyph on Storage's own
// "Connected" indicator, colored to match its own label (#4c6803) --
// stroke declared on the <svg> root itself, not per-shape (the source
// icon, plug-connect-svgrepo-com.svg, had each <path>/<line> carry its own
// stroke attribute; moved to the root to match the one SVG structure
// confirmed to render correctly in this app -- see
// videoDialogRobotSVG/videoDialogCheckmarkSVG in video_start_dialog.go).
var deviceDashboardConnectIconSVG = fyne.NewStaticResource("device_dashboard_plug.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="#4c6803" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M6 7H18V12C18 15.3137 15.3137 18 12 18V18C8.68629 18 6 15.3137 6 12V7Z"/><line x1="15" y1="2" x2="15" y2="7"/><path d="M12 18V22"/><line x1="9" y1="2" x2="9" y2="7"/></svg>`))

// deviceDashboardConnectDisabledIconSVG is the same plug on the gray
// disabled fill -- #111111, matching the Connections Connect button's
// own dark glyph, so the square still reads as a button instead of a
// ghost on ColorGray900.
var deviceDashboardConnectDisabledIconSVG = fyne.NewStaticResource("device_dashboard_plug_disabled.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="#111111" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M6 7H18V12C18 15.3137 15.3137 18 12 18V18C8.68629 18 6 15.3137 6 12V7Z"/><line x1="15" y1="2" x2="15" y2="7"/><path d="M12 18V22"/><line x1="9" y1="2" x2="9" y2="7"/></svg>`))

// deviceDashboardDisabledFill is the gray square Storage/Backups Connect
// (and Disconnect) use while the dashboard is locked -- ColorSurfaceLight
// on ColorGray900, not ColorGray900 on itself (that made the lime fill
// vanish and left a washed-out icon).
var deviceDashboardDisabledFill = design.ColorSurfaceLight

// deviceDashboardConnectHoverFill matches the Connections table's own
// Connect button hover fill exactly (connectHover in
// connection_list_table.go).
var deviceDashboardConnectHoverFill = color.NRGBA{R: 0xd4, G: 0xf7, B: 0x8a, A: 0xff}

// deviceDashboardDisconnectIconSVG reuses assets.ConnectIconBoldBlack's own
// path data (connect-svgrepo-com.svg's chain-link glyph, #111111) recolored
// to #c5c8b5 -- both assets.ExitIcon and, before that,
// debug-disconnect-svgrepo-com.svg were tried here first.
var deviceDashboardDisconnectIconSVG = fyne.NewStaticResource("device_dashboard_disconnect.svg", []byte(strings.ReplaceAll(string(assets.ConnectIconBoldBlack.Content()), "#111111", "#c5c8b5")))

// deviceDashboardDisconnectHoverIconSVG is the same shape recolored to
// #e997a2 for the hover state.
var deviceDashboardDisconnectHoverIconSVG = fyne.NewStaticResource("device_dashboard_disconnect_hover.svg", []byte(strings.ReplaceAll(string(assets.ConnectIconBoldBlack.Content()), "#111111", "#e997a2")))

// deviceDashboardDisconnectHoverFill/Stroke are NewDeviceDashboardDisconnectButton's
// own hover colors -- a dark maroon fill/border reading as a danger hover,
// unlike Delete/Upload's neutral design.ColorSurfaceLight hover.
var deviceDashboardDisconnectHoverFill = color.NRGBA{R: 0x2e, G: 0x20, B: 0x24, A: 0xff}
var deviceDashboardDisconnectHoverStroke = color.NRGBA{R: 0x82, G: 0x34, B: 0x37, A: 0xff}

// NewDeviceDashboardDisconnectButton is Storage's own "unmount this drive"
// action, shown once a drive is actually mounted (see
// disk_widget_dashboard.go's refreshDashboard) in the same trailing slot
// NewDeviceDashboardMountButton takes before that. Normal state matches
// NewDeviceDashboardDeleteButton exactly (transparent fill, muted icon,
// design.ColorTailscaleChipBorder border); only on hover does it turn
// danger-red, since unmounting isn't itself destructive the way deleting
// is. No label -- the mounted state is instead read from the row's own
// name/icon color (see newDeviceDashboardRowLeftSized).
func NewDeviceDashboardDisconnectButton(onTap func(), onHover func(bool)) *iconChromeButton {
	return newIconChromeButton(iconChromeButtonSpec{
		NormalFill:   color.Transparent,
		HoverFill:    deviceDashboardDisconnectHoverFill,
		DisabledFill: deviceDashboardDisabledFill,
		Stroke:       design.ColorTailscaleChipBorder,
		HoverStroke:  deviceDashboardDisconnectHoverStroke,
		StrokeWidth:  1,
		CornerRadius: 6,
		NormalIcon:   deviceDashboardDisconnectIconSVG,
		HoverIcon:    deviceDashboardDisconnectHoverIconSVG,
		DisabledIcon: assets.ConnectIconBoldBlack,
		IconSize:     fyne.NewSize(11, 11),
		ButtonSize:   fyne.NewSize(23, 23),
		OnTapped:     onTap,
		OnHover:      onHover,
	})
}

// NewDeviceDashboardMountButton is Storage's own "not yet mounted" action --
// a plain square icon button, no label, shown in the same trailing slot
// NewDeviceDashboardDisconnectButton takes once the drive is actually
// mounted (see disk_widget_dashboard.go's refreshDashboard) -- same lime
// fill/hover/icon recipe as "Mount New ISO". Tapping it mounts the drive.
// busy darkens the lime fill to the same shade "Mount New ISO" uses while
// its file picker is open (deviceDashboardHeaderButtonBusyFill) and
// swallows further taps until the mount finishes.
func NewDeviceDashboardMountButton(onTap func(), onHover func(bool), busy bool) *iconChromeButton {
	btn := newIconChromeButton(iconChromeButtonSpec{
		NormalFill:   DeviceDashboardAccentLime,
		HoverFill:    deviceDashboardConnectHoverFill,
		LoadingFill:  deviceDashboardHeaderButtonBusyFill,
		DisabledFill: deviceDashboardDisabledFill,
		Stroke:       color.Transparent,
		CornerRadius: 6,
		NormalIcon:   deviceDashboardConnectIconSVG,
		DisabledIcon: deviceDashboardConnectDisabledIconSVG,
		IconSize:     fyne.NewSize(11, 11),
		ButtonSize:   fyne.NewSize(23, 23),
		OnTapped:     onTap,
		OnHover:      onHover,
	})
	if busy {
		btn.SetLoading(true)
	}
	return btn
}

// deviceDashboardSettingsIconSVG recolors assets.ConfigVerticalIcon to
// #c5c8b5 -- same muted fill Delete/Upload use, so the Video row's gear
// reads as the same button family.
var deviceDashboardSettingsIconSVG = fyne.NewStaticResource("device_dashboard_settings.svg", []byte(strings.ReplaceAll(string(assets.ConfigVerticalIcon.Content()), "#F5F5F5", "#c5c8b5")))

// NewDeviceDashboardSettingsButton is NewDeviceDashboardDeleteButton's own
// chrome with a gear glyph -- Video Pipe's "open this capture device's
// config window" action. onHover may be nil; see DeviceToggle.OnHover.
func NewDeviceDashboardSettingsButton(onTap func(), onHover func(bool)) *iconChromeButton {
	return newIconChromeButton(iconChromeButtonSpec{
		NormalFill:   color.Transparent,
		HoverFill:    design.ColorSurfaceLight,
		DisabledFill: connectionActionBlockedFill,
		Stroke:       design.ColorTailscaleChipBorder,
		StrokeWidth:  1,
		CornerRadius: 6,
		NormalIcon:   deviceDashboardSettingsIconSVG,
		IconSize:     fyne.NewSize(11, 11),
		ButtonSize:   fyne.NewSize(23, 23),
		OnTapped:     onTap,
		OnHover:      onHover,
	})
}

// DisableDashboardAction greys a dashboard chrome control (gear, delete,
// upload, mount, disconnect) and swallows taps. Dashboard builders return
// these as fyne.CanvasObject, so callers in other packages can't name the
// unexported button type themselves.
func DisableDashboardAction(obj fyne.CanvasObject, disabled bool) {
	if b, ok := obj.(*iconChromeButton); ok {
		b.SetDisabled(disabled)
	}
}

// NewDeviceDashboardStorageRow is NewDeviceDashboardRow's Storage-specific
// variant: the same icon+name left side, but its right side can also carry
// a USB Stick/CD-ROM mode picker and Delete/Upload icon buttons -- any of
// which may be nil to omit it -- alongside the same connect button every
// other mountable row has.
// uploadProgress, when non-nil (see NewDeviceDashboardUploadProgress),
// replaces every other right-side control while a file is actively
// uploading -- the caller is expected to pass nil for
// modePicker/deleteBtn/uploadBtn/connectBtn in that case. chips, when
// non-empty, show small plaques under the name (drive size, snapshot
// count, …) -- see newDeviceDashboardRowLeftChips -- empty for drives
// whose size isn't known.
func NewDeviceDashboardStorageRow(icon fyne.Resource, name string, active bool, modePicker, deleteBtn, uploadBtn, connectBtn, uploadProgress fyne.CanvasObject, chips ...string) fyne.CanvasObject {
	left := newDeviceDashboardRowLeftChips(icon, name, active, chips...)

	var rightParts []fyne.CanvasObject
	if uploadProgress != nil {
		rightParts = append(rightParts, uploadProgress)
	} else {
		if modePicker != nil {
			rightParts = append(rightParts, modePicker)
		}
		if deleteBtn != nil {
			rightParts = append(rightParts, deleteBtn)
		}
		if uploadBtn != nil {
			rightParts = append(rightParts, uploadBtn)
		}
		if connectBtn != nil {
			rightParts = append(rightParts, connectBtn)
		}
	}
	right := container.New(&DeviceRowControlsLayout{Gap: 8}, rightParts...)

	row := container.NewBorder(nil, nil, left, right)
	return NewInsetExact(row, 0, 0, 2, 2)
}

const deviceDashboardBusySpinnerSize = AppFooterRowHeight
const deviceDashboardBusySpinnerInterval = 140 * time.Millisecond

// DeviceDashboardBusySpinner is the Devices footer's lime (#c4e77a) dot
// spinner -- shown while a gadget mount/unmount is in flight. Hidden and
// stopped otherwise so it doesn't leave a hole in the footer. Optional
// hint text (see NewDeviceDashboardBusyHint) sits to the right of the
// dots in the same lime, e.g. "connecting device".
type DeviceDashboardBusySpinner struct {
	widget.BaseWidget

	hint      string
	hintLabel *canvas.Text
	box       *fyne.Container

	mu     sync.Mutex
	stop   chan struct{}
	active bool
	img    *canvas.Image
}

func NewDeviceDashboardBusySpinner() *DeviceDashboardBusySpinner {
	return NewDeviceDashboardBusyHint("")
}

// NewDeviceDashboardBusyHint is the footer spinner with a lime status
// label shown next to the dots while the spinner is active.
func NewDeviceDashboardBusyHint(hint string) *DeviceDashboardBusySpinner {
	s := &DeviceDashboardBusySpinner{hint: strings.TrimSpace(hint)}
	s.ExtendBaseWidget(s)
	if s.hint != "" {
		s.hintLabel = canvas.NewText(s.hint, design.ColorConnectionAddFill)
		s.hintLabel.TextSize = 9
	}
	s.Hide()
	return s
}

func (s *DeviceDashboardBusySpinner) Start() {
	s.mu.Lock()
	if s.active {
		s.mu.Unlock()
		return
	}
	s.active = true
	stop := make(chan struct{})
	s.stop = stop
	s.mu.Unlock()

	s.Show()
	s.Refresh()
	frames := assets.LoadingLimeFrames
	if s.img != nil && len(frames) > 0 {
		s.img.Resource = frames[0]
		s.img.Refresh()
	}

	go func() {
		ticker := time.NewTicker(deviceDashboardBusySpinnerInterval)
		defer ticker.Stop()
		frame := 0
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				if len(frames) == 0 {
					continue
				}
				frame = (frame + 1) % len(frames)
				res := frames[frame]
				fyne.Do(func() {
					s.mu.Lock()
					active := s.active
					s.mu.Unlock()
					if !active || s.img == nil {
						return
					}
					s.img.Resource = res
					s.img.Refresh()
				})
			}
		}
	}()
}

func (s *DeviceDashboardBusySpinner) Stop() {
	s.mu.Lock()
	if !s.active {
		s.mu.Unlock()
		return
	}
	s.active = false
	stop := s.stop
	s.stop = nil
	s.mu.Unlock()
	if stop != nil {
		close(stop)
	}
	s.Hide()
	s.Refresh()
}

func (s *DeviceDashboardBusySpinner) MinSize() fyne.Size {
	if s.box != nil {
		return s.box.MinSize()
	}
	base := fyne.NewSize(deviceDashboardBusySpinnerSize, deviceDashboardBusySpinnerSize)
	if s.hintLabel == nil {
		return base
	}
	textSize := s.hintLabel.MinSize()
	return fyne.NewSize(base.Width+6+textSize.Width, maxFloat32(base.Height, textSize.Height))
}

func (s *DeviceDashboardBusySpinner) CreateRenderer() fyne.WidgetRenderer {
	s.img = canvas.NewImageFromResource(nil)
	s.img.FillMode = canvas.ImageFillContain
	s.img.SetMinSize(fyne.NewSize(deviceDashboardBusySpinnerSize, deviceDashboardBusySpinnerSize))
	if len(assets.LoadingLimeFrames) > 0 {
		s.img.Resource = assets.LoadingLimeFrames[0]
	}
	if s.hintLabel == nil {
		return widget.NewSimpleRenderer(s.img)
	}
	s.box = container.New(&DeviceRowControlsLayout{Gap: 6}, s.img, s.hintLabel)
	return widget.NewSimpleRenderer(s.box)
}

// DeviceDashboardFooterTextButton is a miniature, no-chrome text action
// for the Devices footer (Disconnect All): no fill, no padding, same 10px
// muted type as the version tag, brightening on hover.
type DeviceDashboardFooterTextButton struct {
	widget.BaseWidget

	text     string
	onTap    func()
	hovered  bool
	disabled bool
	lbl      *canvas.Text
}

func NewDeviceDashboardFooterTextButton(text string, onTap func()) *DeviceDashboardFooterTextButton {
	b := &DeviceDashboardFooterTextButton{text: text, onTap: onTap}
	b.ExtendBaseWidget(b)
	return b
}

func (b *DeviceDashboardFooterTextButton) Tapped(*fyne.PointEvent) {
	if b.disabled {
		return
	}
	if b.onTap != nil {
		b.onTap()
	}
}

func (b *DeviceDashboardFooterTextButton) SetEnabled(enabled bool) {
	disabled := !enabled
	if b.disabled == disabled {
		return
	}
	b.disabled = disabled
	b.refreshVisuals()
}

func (b *DeviceDashboardFooterTextButton) TappedSecondary(*fyne.PointEvent) {}

func (b *DeviceDashboardFooterTextButton) Cursor() desktop.Cursor {
	if b.disabled {
		return desktop.DefaultCursor
	}
	return desktop.PointerCursor
}

func (b *DeviceDashboardFooterTextButton) MouseIn(*desktop.MouseEvent) {
	if b.disabled {
		return
	}
	b.hovered = true
	b.refreshVisuals()
}

func (b *DeviceDashboardFooterTextButton) MouseMoved(*desktop.MouseEvent) {}

func (b *DeviceDashboardFooterTextButton) MouseOut() {
	b.hovered = false
	b.refreshVisuals()
}

func (b *DeviceDashboardFooterTextButton) refreshVisuals() {
	if b.lbl == nil {
		return
	}
	if b.disabled {
		b.lbl.Color = design.ColorBorder
	} else if b.hovered {
		b.lbl.Color = design.ColorTextLight
	} else {
		b.lbl.Color = design.ColorTextMuted
	}
	b.lbl.Refresh()
}

func (b *DeviceDashboardFooterTextButton) CreateRenderer() fyne.WidgetRenderer {
	b.lbl = canvas.NewText(b.text, design.ColorTextMuted)
	b.lbl.TextSize = 9
	b.refreshVisuals()
	return widget.NewSimpleRenderer(b.lbl)
}

// DeviceDashboardSpaceMeter is the compact SD fill readout in the Backups
// card header: a short teal bar plus "12/32 GB" digits, sitting to the
// right of the title. Same turquoise/muted-text recipe as the Control
// header's StorageProgressBar, without that chip's own icon (the card
// title already carries the floppy).
type DeviceDashboardSpaceMeter struct {
	widget.BaseWidget

	usedFrac float64 // 0-1
	text     string
	OnHover  func(bool)

	track *canvas.Rectangle
	fill  *canvas.Rectangle
	label *canvas.Text
}

const (
	deviceDashboardSpaceMeterBarW = float32(36)
	deviceDashboardSpaceMeterBarH = float32(3)
	deviceDashboardSpaceMeterGap  = float32(6)
)

func NewDeviceDashboardSpaceMeter() *DeviceDashboardSpaceMeter {
	m := &DeviceDashboardSpaceMeter{}
	m.ExtendBaseWidget(m)
	m.Hide()
	return m
}

// Set updates the fill fraction and the used/total label. An empty label
// hides the meter so the Backups title keeps the header to itself.
func (m *DeviceDashboardSpaceMeter) Set(usedFrac float64, sizeText string) {
	if usedFrac < 0 {
		usedFrac = 0
	}
	if usedFrac > 1 {
		usedFrac = 1
	}
	m.usedFrac = usedFrac
	m.text = strings.TrimSpace(sizeText)
	if m.text == "" {
		m.Hide()
	} else {
		m.Show()
	}
	m.Refresh()
}

func (m *DeviceDashboardSpaceMeter) Clear() {
	m.Set(0, "")
}

func (m *DeviceDashboardSpaceMeter) MouseIn(*desktop.MouseEvent) {
	if m.OnHover != nil {
		m.OnHover(true)
	}
}

func (m *DeviceDashboardSpaceMeter) MouseMoved(*desktop.MouseEvent) {}

func (m *DeviceDashboardSpaceMeter) MouseOut() {
	if m.OnHover != nil {
		m.OnHover(false)
	}
}

func (m *DeviceDashboardSpaceMeter) CreateRenderer() fyne.WidgetRenderer {
	m.track = canvas.NewRectangle(color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0x1c})
	m.track.CornerRadius = 2
	m.fill = canvas.NewRectangle(design.ColorConnectionBadgeText)
	m.fill.CornerRadius = 2
	m.label = canvas.NewText(m.text, design.ColorStatusBarIndicatorText)
	m.label.TextSize = 8
	return &deviceDashboardSpaceMeterRenderer{m: m}
}

type deviceDashboardSpaceMeterRenderer struct {
	m *DeviceDashboardSpaceMeter
}

func (r *deviceDashboardSpaceMeterRenderer) Layout(size fyne.Size) {
	m := r.m
	if m.track == nil || m.fill == nil || m.label == nil {
		return
	}
	barY := (size.Height - deviceDashboardSpaceMeterBarH) / 2
	if barY < 0 {
		barY = 0
	}
	m.track.Move(fyne.NewPos(0, barY))
	m.track.Resize(fyne.NewSize(deviceDashboardSpaceMeterBarW, deviceDashboardSpaceMeterBarH))
	fillW := deviceDashboardSpaceMeterBarW * float32(m.usedFrac)
	if fillW < 1 && m.usedFrac > 0 {
		fillW = 1
	}
	m.fill.Move(fyne.NewPos(0, barY))
	m.fill.Resize(fyne.NewSize(fillW, deviceDashboardSpaceMeterBarH))
	labelSize := m.label.MinSize()
	m.label.Move(fyne.NewPos(deviceDashboardSpaceMeterBarW+deviceDashboardSpaceMeterGap, (size.Height-labelSize.Height)/2))
	m.label.Resize(labelSize)
}

func (r *deviceDashboardSpaceMeterRenderer) MinSize() fyne.Size {
	m := r.m
	if m.text == "" {
		return fyne.NewSize(0, 0)
	}
	measure := canvas.NewText(m.text, design.ColorStatusBarIndicatorText)
	measure.TextSize = 8
	tw := measure.MinSize()
	h := tw.Height
	if h < deviceDashboardSpaceMeterBarH {
		h = deviceDashboardSpaceMeterBarH
	}
	return fyne.NewSize(deviceDashboardSpaceMeterBarW+deviceDashboardSpaceMeterGap+tw.Width, h)
}

func (r *deviceDashboardSpaceMeterRenderer) Refresh() {
	m := r.m
	if m.label != nil {
		m.label.Text = m.text
		m.label.Color = design.ColorStatusBarIndicatorText
		m.label.Refresh()
	}
	if m.track != nil {
		m.track.FillColor = color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0x1c}
		m.track.Refresh()
	}
	if m.fill != nil {
		m.fill.FillColor = design.ColorConnectionBadgeText
		m.fill.Refresh()
	}
	r.Layout(m.Size())
	canvas.Refresh(m)
}

func (r *deviceDashboardSpaceMeterRenderer) Objects() []fyne.CanvasObject {
	m := r.m
	return []fyne.CanvasObject{m.track, m.fill, m.label}
}

func (r *deviceDashboardSpaceMeterRenderer) Destroy() {}
