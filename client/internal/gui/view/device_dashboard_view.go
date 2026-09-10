// device_dashboard_view.go -- the Devices tab's card-grid layout: a narrow
// left column (HID & Input Hub, Video Pipe & EDID, Audio Pipeline) stacked
// above one another, and a wide right column (Virtual Mass Storage & ISO
// Media) beside them, both styled after the Connections grid's own cards
// (see connection_grid_card.go). Populated from controller/disk_widget_dashboard.go.
package view

import (
	"image/color"

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

// deviceDashboardCardSep is the same muted divider color used under a
// Connections card's own top row (see connection_grid_card.go's own divider
// rectangle).
var deviceDashboardCardSep = color.NRGBA{R: 0x29, G: 0x2d, B: 0x27, A: 0xff}

// NewDeviceDashboardCard builds one dashboard card in the Connections
// grid's own visual language: a dark rounded panel (design.ColorGray900,
// design.RadiusLG, a muted border) with an icon+title header, an optional
// element on the header's right edge, a hairline divider, then content.
func NewDeviceDashboardCard(icon fyne.Resource, title string, headerRight fyne.CanvasObject, content fyne.CanvasObject) fyne.CanvasObject {
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

	sep := canvas.NewRectangle(deviceDashboardCardSep)
	sep.SetMinSize(fyne.NewSize(0, 1))

	body := container.NewVBox(headerRow, sep, content)

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
