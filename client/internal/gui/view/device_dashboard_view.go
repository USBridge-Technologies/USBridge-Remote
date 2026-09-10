// device_dashboard_view.go -- the Devices tab's card-grid layout: a narrow
// left column (HID & Input Hub, Video Pipe & EDID, Audio Pipeline) stacked
// above one another, and a wide right column (Virtual Mass Storage & ISO
// Media) beside them, both styled after the Connections grid's own cards
// (see connection_grid_card.go). Populated from controller/disk_widget_dashboard.go.
package view

import (
	"image/color"
	"strings"

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
// rectangle).
var deviceDashboardCardSep = color.NRGBA{R: 0x29, G: 0x2d, B: 0x27, A: 0xff}

// deviceDashboardDescColor is the small caption line under a dashboard
// card's title (e.g. Storage's "Emulated OTG USB Mass Storage Drive &
// CD-ROM Devices").
var deviceDashboardDescColor = color.NRGBA{R: 0xc5, G: 0xc8, B: 0xb5, A: 0xff}

// NewDeviceDashboardCard builds one dashboard card in the Connections
// grid's own visual language: a dark rounded panel (design.ColorGray900,
// design.RadiusLG, a muted border) with an icon+title header (an optional
// element on the header's right edge, e.g. a header button) and an
// optional one-line description below it, a hairline divider, then
// content.
func NewDeviceDashboardCard(icon fyne.Resource, title string, description string, headerRight fyne.CanvasObject, content fyne.CanvasObject) fyne.CanvasObject {
	iconImg := canvas.NewImageFromResource(icon)
	iconImg.FillMode = canvas.ImageFillContain
	iconImg.SetMinSize(fyne.NewSize(16, 16))

	titleText := canvas.NewText(title, design.ColorTextLight)
	titleText.TextSize = 13
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

	body := container.NewVBox(headerBlock, sep, content)

	cardBg := canvas.NewRectangle(design.ColorGray900)
	cardBg.CornerRadius = design.RadiusLG
	cardBg.StrokeColor = design.ColorTailscaleChipBorder
	cardBg.StrokeWidth = 1

	return container.NewStack(cardBg, NewInset(body, 14, 14, 12, 12))
}

// NewDeviceDashboardRow is one compact device line inside a dashboard card:
// a status dot, the device's display name, its small type badge (see
// driveBadge in controller/disk_widget_sections.go), and an optional
// mount/unmount toggle trailing on the right -- reuses the Connections
// grid's own chip style (newConnectionPlatformChip) so a device's badge
// reads as the same family as a connection's platform chip. toggle is nil
// for device kinds this dashboard doesn't drive mount/unmount for (Video,
// Audio -- see disk_widget_mount.go's own IsVideo/IsAudio exclusions).
func NewDeviceDashboardRow(name string, active bool, badgeText string, toggle fyne.CanvasObject) fyne.CanvasObject {
	dotColor := color.Color(videoDialogHintColor)
	if active {
		dotColor = design.ColorConnectionBadgeText
	}
	dot := canvas.NewCircle(dotColor)
	dotWrap := container.NewGridWrap(fyne.NewSize(8, 8), dot)

	nameText := canvas.NewText(name, design.ColorTextLight)
	nameText.TextSize = 11

	left := container.New(&DeviceRowControlsLayout{Gap: 8}, dotWrap, nameText)

	var right fyne.CanvasObject = newConnectionPlatformChip(badgeText)
	if toggle != nil {
		right = container.New(&DeviceRowControlsLayout{Gap: 10}, newConnectionPlatformChip(badgeText), toggle)
	}

	row := container.NewBorder(nil, nil, left, right)
	return NewInset(row, 0, 0, 5, 5)
}

// deviceToggleWidth/Height is the pill track's own footprint; deviceToggleKnob
// is the round knob sliding inside it, deviceToggleInset its resting gap
// from the track's own edge.
const (
	deviceToggleWidth  = float32(30)
	deviceToggleHeight = float32(16)
	deviceToggleKnob   = float32(12)
	deviceToggleInset  = float32(2)
)

// DeviceToggle is a small on/off pill switch for a dashboard row's own
// mount/unmount action -- Fyne has no built-in switch widget (widget.Check
// is a checkbox), and this dialog's rows read better as a switch (matching
// the reference mockup) than a checkbox would.
type DeviceToggle struct {
	widget.BaseWidget

	Active    bool
	OnChanged func(bool)

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
	t.Active = !t.Active
	t.Refresh()
	if t.OnChanged != nil {
		t.OnChanged(t.Active)
	}
}

func (t *DeviceToggle) TappedSecondary(*fyne.PointEvent) {}

func (t *DeviceToggle) MouseIn(*desktop.MouseEvent) {
	t.hovered = true
	t.Refresh()
}

func (t *DeviceToggle) MouseMoved(*desktop.MouseEvent) {}

func (t *DeviceToggle) MouseOut() {
	t.hovered = false
	t.Refresh()
}

func (t *DeviceToggle) Cursor() desktop.Cursor {
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
		t.track.FillColor = design.ColorConnectionBadgeText
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

// NewDeviceDashboardEmptyState is the muted placeholder line a dashboard
// card shows in place of its rows when it currently has no devices.
func NewDeviceDashboardEmptyState(text string) fyne.CanvasObject {
	label := canvas.NewText(text, videoDialogHintColor)
	label.TextSize = 10
	return NewInset(label, 0, 0, 6, 6)
}

// DeviceDashboardStorageIconSVG is the Storage card's own SSD glyph,
// recolored to #c4e77a (must match DeviceDashboardAccentLime below --
// SVG resources can't reference a Go color value).
var DeviceDashboardStorageIconSVG = fyne.NewStaticResource("device_dashboard_ssd.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none"><path fill-rule="evenodd" clip-rule="evenodd" d="M5.56216 2.87174C6.14861 2.41455 6.82355 2.25 7.5 2.25H16.5C17.1765 2.25 17.8514 2.41455 18.4378 2.87174C19.0172 3.32344 19.4352 4.0024 19.7154 4.89243L19.7225 4.91513L22.2604 15.2159C22.5738 15.8383 22.75 16.5463 22.75 17.2941C22.75 19.7141 20.887 21.75 18.5 21.75H5.5C3.11298 21.75 1.25 19.7141 1.25 17.2941C1.25 16.5463 1.42621 15.8383 1.73961 15.2159L4.27747 4.91513L4.28461 4.89243C4.56481 4.0024 4.98276 3.32344 5.56216 2.87174ZM3.77626 13.2197C4.30106 12.9752 4.88373 12.8382 5.5 12.8382H18.5C19.1163 12.8382 19.6989 12.9752 20.2237 13.2197L18.2777 5.32094C18.0589 4.63669 17.7822 4.26258 17.5156 4.05473C17.2532 3.85015 16.9281 3.75 16.5 3.75H7.5C7.07188 3.75 6.74682 3.85015 6.48441 4.05473C6.21779 4.26258 5.94107 4.63669 5.72234 5.32094L3.77626 13.2197ZM5.5 14.3382C4.49271 14.3382 3.59139 14.9242 3.10912 15.8329C2.88147 16.2618 2.75 16.7597 2.75 17.2941C2.75 18.9676 4.02103 20.25 5.5 20.25H18.5C19.979 20.25 21.25 18.9676 21.25 17.2941C21.25 16.7597 21.1185 16.2618 20.8909 15.8329C20.4086 14.9242 19.5073 14.3382 18.5 14.3382H5.5ZM10.5 16.25C10.9142 16.25 11.25 16.5858 11.25 17V18C11.25 18.4142 10.9142 18.75 10.5 18.75C10.0858 18.75 9.75 18.4142 9.75 18V17C9.75 16.5858 10.0858 16.25 10.5 16.25ZM13 16.25C13.4142 16.25 13.75 16.5858 13.75 17V18C13.75 18.4142 13.4142 18.75 13 18.75C12.5858 18.75 12.25 18.4142 12.25 18V17C12.25 16.5858 12.5858 16.25 13 16.25ZM15.5 16.25C15.9142 16.25 16.25 16.5858 16.25 17V18C16.25 18.4142 15.9142 18.75 15.5 18.75C15.0858 18.75 14.75 18.4142 14.75 18V17C14.75 16.5858 15.0858 16.25 15.5 16.25ZM18 16.25C18.4142 16.25 18.75 16.5858 18.75 17V18C18.75 18.4142 18.4142 18.75 18 18.75C17.5858 18.75 17.25 18.4142 17.25 18V17C17.25 16.5858 17.5858 16.25 18 16.25Z" fill="#c4e77a"/></svg>`))

// DeviceDashboardPlusCircleIconSVG is the small "+" glyph shown in header
// buttons like Storage's "Mount New ISO" -- recolored dark (#0b0f12) to
// read against the button's own bright accent fill, same caveat as
// DeviceDashboardStorageIconSVG above (a literal hex, not a Go color
// value).
// Fill declared on the <svg> root, not the <path> (which carries only
// fill-rule) -- matches videoDialogRobotSVG's own structure in
// video_start_dialog.go, the one pattern in this codebase confirmed to
// render correctly; fill="none" on the root with the path's own fill
// overriding it (this icon's first version) rendered as fully invisible.
var DeviceDashboardPlusCircleIconSVG = fyne.NewStaticResource("device_dashboard_plus_circle_dark.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" fill="#0b0f12" viewBox="0 0 20 20"><path fill-rule="evenodd" d="M10 3a7 7 0 100 14 7 7 0 000-14zm-9 7a9 9 0 1118 0 9 9 0 01-18 0zm14 .069a1 1 0 01-1 1h-2.931V14a1 1 0 11-2 0v-2.931H6a1 1 0 110-2h3.069V6a1 1 0 112 0v3.069H14a1 1 0 011 1z"/></svg>`))

// DeviceDashboardAccentLime is the lime accent used for the Storage card's
// SSD icon and as its "Mount New ISO" header button's own fill -- must
// match the hex inlined into DeviceDashboardStorageIconSVG above.
var DeviceDashboardAccentLime = color.NRGBA{R: 0xc4, G: 0xe7, B: 0x7a, A: 0xff}

// deviceDashboardAccentLimeHover is DeviceDashboardAccentLime lightened,
// for a header button's own hover fill.
var deviceDashboardAccentLimeHover = color.NRGBA{R: 0xd9, G: 0xf2, B: 0xa3, A: 0xff}

// deviceDashboardHeaderButtonText is the dark text/icon color read against
// a header button's own bright accent fill -- design.ColorGray950, the
// same "dark text on a bright pill" convention the app's Apply/active-codec
// buttons already use.
var deviceDashboardHeaderButtonText = design.ColorGray950

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

	text    string
	icon    fyne.Resource
	accent  color.Color
	onTap   func()
	hovered bool

	bg *canvas.Rectangle
}

// NewDeviceDashboardHeaderButton builds a header button filled with
// accent, showing icon (may be nil) before text, calling onTap when
// tapped.
func NewDeviceDashboardHeaderButton(text string, icon fyne.Resource, accent color.Color, onTap func()) *DeviceDashboardHeaderButton {
	b := &DeviceDashboardHeaderButton{text: text, icon: icon, accent: accent, onTap: onTap}
	b.ExtendBaseWidget(b)
	return b
}

func (b *DeviceDashboardHeaderButton) Tapped(*fyne.PointEvent) {
	if b.onTap != nil {
		b.onTap()
	}
}

func (b *DeviceDashboardHeaderButton) TappedSecondary(*fyne.PointEvent) {}

func (b *DeviceDashboardHeaderButton) Cursor() desktop.Cursor {
	return desktop.PointerCursor
}

func (b *DeviceDashboardHeaderButton) MouseIn(*desktop.MouseEvent) {
	b.hovered = true
	b.refreshVisuals()
}

func (b *DeviceDashboardHeaderButton) MouseMoved(*desktop.MouseEvent) {}

func (b *DeviceDashboardHeaderButton) MouseOut() {
	b.hovered = false
	b.refreshVisuals()
}

func (b *DeviceDashboardHeaderButton) refreshVisuals() {
	if b.bg == nil {
		return
	}
	fill := b.accent
	if b.hovered {
		fill = deviceDashboardAccentLimeHover
	}
	b.bg.FillColor = fill
	b.bg.Refresh()
}

func (b *DeviceDashboardHeaderButton) CreateRenderer() fyne.WidgetRenderer {
	b.bg = canvas.NewRectangle(b.accent)
	b.bg.CornerRadius = 6

	label := canvas.NewText(b.text, deviceDashboardHeaderButtonText)
	label.TextSize = 10
	label.TextStyle.Bold = true

	var row fyne.CanvasObject = label
	if b.icon != nil {
		iconImg := canvas.NewImageFromResource(b.icon)
		iconImg.FillMode = canvas.ImageFillContain
		iconImg.SetMinSize(fyne.NewSize(12, 12))
		row = container.New(&DeviceRowControlsLayout{Gap: 6}, iconImg, label)
	}

	// 4px top/bottom padding -- shorter than view.DeviceActionButton's own
	// (unexported) padding, per this button's own "shorter" requirement.
	return widget.NewSimpleRenderer(container.NewStack(b.bg, NewInsetExact(row, 10, 10, 4, 4)))
}

// NewDeviceDashboardModePicker is a Storage row's own USB Stick/CD-ROM
// mode picker -- the same small teal pill dropdown style used elsewhere in
// this app (see e.g. newVideoDialogPicker in video_start_dialog.go).
func NewDeviceDashboardModePicker(options []string, selected string, onSelected func(string)) *HeaderDropdown {
	d := NewHeaderDropdown(options, selected, onSelected)
	d.UltraCompact = true
	d.CornerRadius = 6
	d.BorderColor = design.ColorTailscaleChipBorder
	d.TextColor = design.ColorConnectionBadgeText
	d.IconColor = deviceDashboardDescColor
	d.TextSize = 9
	d.HoverBorderColor = design.ColorConnectionBadgeText
	d.HoverFillColor = design.ColorGray900
	return d
}

// DeviceDashboardIconButton is a small icon-only tappable action for a
// dashboard row (Storage's own Upload/Delete) -- muted by default,
// brightens on hover, dims further and stops responding once disabled.
// One neutral icon resource dimmed via Translucency for every state,
// rather than several pre-recolored resource variants.
type DeviceDashboardIconButton struct {
	widget.BaseWidget

	icon     fyne.Resource
	onTap    func()
	disabled bool
	hovered  bool

	img *canvas.Image
}

// NewDeviceDashboardIconButton builds an icon-only button showing icon,
// calling onTap when tapped (unless disabled -- see SetDisabled).
func NewDeviceDashboardIconButton(icon fyne.Resource, onTap func()) *DeviceDashboardIconButton {
	b := &DeviceDashboardIconButton{icon: icon, onTap: onTap}
	b.ExtendBaseWidget(b)
	return b
}

func (b *DeviceDashboardIconButton) SetDisabled(disabled bool) {
	if b.disabled == disabled {
		return
	}
	b.disabled = disabled
	b.Refresh()
}

func (b *DeviceDashboardIconButton) Tapped(*fyne.PointEvent) {
	if b.disabled {
		return
	}
	if b.onTap != nil {
		b.onTap()
	}
}

func (b *DeviceDashboardIconButton) TappedSecondary(*fyne.PointEvent) {}

func (b *DeviceDashboardIconButton) Cursor() desktop.Cursor {
	return desktop.PointerCursor
}

func (b *DeviceDashboardIconButton) MouseIn(*desktop.MouseEvent) {
	b.hovered = true
	b.Refresh()
}

func (b *DeviceDashboardIconButton) MouseMoved(*desktop.MouseEvent) {}

func (b *DeviceDashboardIconButton) MouseOut() {
	b.hovered = false
	b.Refresh()
}

func (b *DeviceDashboardIconButton) MinSize() fyne.Size {
	return fyne.NewSize(20, 20)
}

func (b *DeviceDashboardIconButton) CreateRenderer() fyne.WidgetRenderer {
	b.img = canvas.NewImageFromResource(b.icon)
	b.img.FillMode = canvas.ImageFillContain
	b.img.SetMinSize(fyne.NewSize(14, 14))
	b.applyState()
	return widget.NewSimpleRenderer(container.NewCenter(b.img))
}

func (b *DeviceDashboardIconButton) Refresh() {
	b.applyState()
	b.BaseWidget.Refresh()
}

func (b *DeviceDashboardIconButton) applyState() {
	if b.img == nil {
		return
	}
	switch {
	case b.disabled:
		b.img.Translucency = 0.75
	case b.hovered:
		b.img.Translucency = 0
	default:
		b.img.Translucency = 0.4
	}
	b.img.Refresh()
}

// NewDeviceDashboardStorageRow is NewDeviceDashboardRow's Storage-specific
// variant: the same dot+name+badge left side, but its right side can also
// carry a USB Stick/CD-ROM mode picker and Upload/Delete icon buttons --
// any of which may be nil to omit it -- alongside the same mount/unmount
// toggle every other mountable row has.
func NewDeviceDashboardStorageRow(name string, active bool, badgeText string, modePicker fyne.CanvasObject, uploadBtn fyne.CanvasObject, deleteBtn fyne.CanvasObject, toggle fyne.CanvasObject) fyne.CanvasObject {
	dotColor := color.Color(videoDialogHintColor)
	if active {
		dotColor = design.ColorConnectionBadgeText
	}
	dot := canvas.NewCircle(dotColor)
	dotWrap := container.NewGridWrap(fyne.NewSize(8, 8), dot)

	nameText := canvas.NewText(name, design.ColorTextLight)
	nameText.TextSize = 11

	left := container.New(&DeviceRowControlsLayout{Gap: 8}, dotWrap, nameText)

	rightParts := []fyne.CanvasObject{newConnectionPlatformChip(badgeText)}
	if modePicker != nil {
		rightParts = append(rightParts, modePicker)
	}
	if uploadBtn != nil {
		rightParts = append(rightParts, uploadBtn)
	}
	if deleteBtn != nil {
		rightParts = append(rightParts, deleteBtn)
	}
	if toggle != nil {
		rightParts = append(rightParts, toggle)
	}
	right := container.New(&DeviceRowControlsLayout{Gap: 8}, rightParts...)

	row := container.NewBorder(nil, nil, left, right)
	return NewInset(row, 0, 0, 5, 5)
}
