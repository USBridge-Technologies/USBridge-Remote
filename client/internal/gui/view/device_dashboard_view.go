// device_dashboard_view.go -- the Devices tab's card-grid layout: a narrow
// left column (HID & Input Hub, Video Pipe & EDID, Audio Pipeline) stacked
// above one another, and a wide right column (Virtual Mass Storage & ISO
// Media) beside them, both styled after the Connections grid's own cards
// (see connection_grid_card.go). Populated from controller/disk_widget_dashboard.go.
package view

import (
	"image/color"
	"strings"

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

// NewDeviceDashboardCard builds one dashboard card in the Connections
// grid's own visual language: a dark rounded panel (design.ColorGray900,
// design.RadiusLG, a muted border) with an icon+title header (an optional
// element on the header's right edge, e.g. a header button) and an
// optional one-line description below it, a full-width hairline divider,
// then content -- the header/content each carry their own padding
// independently so the divider between them can span edge to edge.
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

	body := container.NewVBox(
		NewInset(headerBlock, 14, 14, 12, 8),
		sep,
		NewInset(content, 14, 14, 8, 12),
	)

	cardBg := canvas.NewRectangle(design.ColorGray900)
	cardBg.CornerRadius = design.RadiusLG
	cardBg.StrokeColor = design.ColorTailscaleChipBorder
	cardBg.StrokeWidth = 1

	return container.NewStack(cardBg, body)
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

// NewDeviceDashboardRow is one compact device line inside a dashboard card:
// its type icon (see controller/disk_widget_dashboard.go's driveIconResource
// -- a folder/disc/SD-card/keyboard/... glyph matching the drive's own
// kind, already brightened when active, same as the old list's own per-row
// icon), the device's display name (brighter when active), and an optional
// connect/disconnect button trailing on the right. connectBtn is nil for
// device kinds this dashboard doesn't drive mount/unmount for (Video,
// Audio -- see disk_widget_mount.go's own IsVideo/IsAudio exclusions).
func NewDeviceDashboardRow(icon fyne.Resource, name string, active bool, connectBtn fyne.CanvasObject) fyne.CanvasObject {
	left := newDeviceDashboardRowLeft(icon, name, active)
	row := container.NewBorder(nil, nil, left, connectBtn)
	return NewInsetExact(row, 0, 0, 2, 2)
}

// newDeviceDashboardRowLeft is the icon+name group shared by
// NewDeviceDashboardRow and NewDeviceDashboardStorageRow.
func newDeviceDashboardRowLeft(icon fyne.Resource, name string, active bool) fyne.CanvasObject {
	nameColor := design.ColorTextLight
	if active {
		nameColor = design.ColorAccent
	}
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
var DeviceDashboardStorageIconSVG = fyne.NewStaticResource("device_dashboard_ssd.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" fill="#c4e77a" viewBox="0 0 24 24"><path fill-rule="evenodd" clip-rule="evenodd" d="M5.56216 2.87174C6.14861 2.41455 6.82355 2.25 7.5 2.25H16.5C17.1765 2.25 17.8514 2.41455 18.4378 2.87174C19.0172 3.32344 19.4352 4.0024 19.7154 4.89243L19.7225 4.91513L22.2604 15.2159C22.5738 15.8383 22.75 16.5463 22.75 17.2941C22.75 19.7141 20.887 21.75 18.5 21.75H5.5C3.11298 21.75 1.25 19.7141 1.25 17.2941C1.25 16.5463 1.42621 15.8383 1.73961 15.2159L4.27747 4.91513L4.28461 4.89243C4.56481 4.0024 4.98276 3.32344 5.56216 2.87174ZM3.77626 13.2197C4.30106 12.9752 4.88373 12.8382 5.5 12.8382H18.5C19.1163 12.8382 19.6989 12.9752 20.2237 13.2197L18.2777 5.32094C18.0589 4.63669 17.7822 4.26258 17.5156 4.05473C17.2532 3.85015 16.9281 3.75 16.5 3.75H7.5C7.07188 3.75 6.74682 3.85015 6.48441 4.05473C6.21779 4.26258 5.94107 4.63669 5.72234 5.32094L3.77626 13.2197ZM5.5 14.3382C4.49271 14.3382 3.59139 14.9242 3.10912 15.8329C2.88147 16.2618 2.75 16.7597 2.75 17.2941C2.75 18.9676 4.02103 20.25 5.5 20.25H18.5C19.979 20.25 21.25 18.9676 21.25 17.2941C21.25 16.7597 21.1185 16.2618 20.8909 15.8329C20.4086 14.9242 19.5073 14.3382 18.5 14.3382H5.5ZM10.5 16.25C10.9142 16.25 11.25 16.5858 11.25 17V18C11.25 18.4142 10.9142 18.75 10.5 18.75C10.0858 18.75 9.75 18.4142 9.75 18V17C9.75 16.5858 10.0858 16.25 10.5 16.25ZM13 16.25C13.4142 16.25 13.75 16.5858 13.75 17V18C13.75 18.4142 13.4142 18.75 13 18.75C12.5858 18.75 12.25 18.4142 12.25 18V17C12.25 16.5858 12.5858 16.25 13 16.25ZM15.5 16.25C15.9142 16.25 16.25 16.5858 16.25 17V18C16.25 18.4142 15.9142 18.75 15.5 18.75C15.0858 18.75 14.75 18.4142 14.75 18V17C14.75 16.5858 15.0858 16.25 15.5 16.25ZM18 16.25C18.4142 16.25 18.75 16.5858 18.75 17V18C18.75 18.4142 18.4142 18.75 18 18.75C17.5858 18.75 17.25 18.4142 17.25 18V17C17.25 16.5858 17.5858 16.25 18 16.25Z"/></svg>`))

// DeviceDashboardAccentLime is the lime accent used for the Storage card's
// SSD icon and as its "Mount New ISO" header button's own fill -- must
// match the hex inlined into DeviceDashboardStorageIconSVG above.
var DeviceDashboardAccentLime = color.NRGBA{R: 0xc4, G: 0xe7, B: 0x7a, A: 0xff}

// deviceDashboardAccentLimeHover is DeviceDashboardAccentLime lightened,
// for a header button's own hover fill.
var deviceDashboardAccentLimeHover = color.NRGBA{R: 0xd9, G: 0xf2, B: 0xa3, A: 0xff}

// DeviceDashboardHeaderButtonTextColor is the dark text/icon color read
// against a header button's own bright accent fill -- design.ColorGray950,
// the same "dark text on a bright pill" convention the app's Apply/
// active-codec buttons already use. Exported so a caller building its own
// icon glyph (e.g. NewDeviceDashboardPlusGlyph) for a header button can
// match it.
var DeviceDashboardHeaderButtonTextColor = design.ColorGray950

// NewDeviceDashboardPlusGlyph draws a "+" from two plain rectangles rather
// than an SVG resource -- an SVG circle+plus icon here rendered either
// fully invisible or blending into the button's own fill across two
// separate attempts (see this repo's own history); a shape this simple
// doesn't need SVG parsing at all, which sidesteps whatever the actual
// cause was.
func NewDeviceDashboardPlusGlyph(size float32, col color.Color) fyne.CanvasObject {
	thickness := size / 3
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

	text    string
	icon    fyne.CanvasObject
	accent  color.Color
	onTap   func()
	hovered bool

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

	label := canvas.NewText(b.text, DeviceDashboardHeaderButtonTextColor)
	label.TextSize = 10
	label.TextStyle.Bold = true

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
// family instead of a one-off.
func NewDeviceDashboardDeleteButton(onTap func()) *iconChromeButton {
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
	})
}

// NewDeviceDashboardUploadButton is NewDeviceDashboardDeleteButton's own
// chrome (same fill/border/hover/size) with an upload glyph instead --
// Storage's own Upload action, styled as the same button family.
func NewDeviceDashboardUploadButton(onTap func()) *iconChromeButton {
	return newIconChromeButton(iconChromeButtonSpec{
		NormalFill:   color.Transparent,
		HoverFill:    design.ColorSurfaceLight,
		DisabledFill: connectionActionBlockedFill,
		Stroke:       design.ColorTailscaleChipBorder,
		StrokeWidth:  1,
		CornerRadius: 6,
		NormalIcon:   assets.UploadIcon,
		HoverIcon:    assets.UploadIcon,
		DisabledIcon: assets.UploadIconMuted,
		IconSize:     fyne.NewSize(11, 11),
		ButtonSize:   fyne.NewSize(23, 23),
		OnTapped:     onTap,
	})
}

// DeviceDashboardConnectButton is a row's own mount/unmount action --
// replaces an earlier on/off switch with a proper button, consistent with
// the rest of this dialog's button-driven controls: a muted outline
// reading "Connect" while unmounted, filled bright accent reading
// "Connected" once it is.
type DeviceDashboardConnectButton struct {
	widget.BaseWidget

	Mounted bool
	OnTap   func()

	hovered bool

	bg    *canvas.Rectangle
	label *canvas.Text
}

// NewDeviceDashboardConnectButton builds a connect button reflecting
// mounted, calling onTap when tapped.
func NewDeviceDashboardConnectButton(mounted bool, onTap func()) *DeviceDashboardConnectButton {
	b := &DeviceDashboardConnectButton{Mounted: mounted, OnTap: onTap}
	b.ExtendBaseWidget(b)
	return b
}

func (b *DeviceDashboardConnectButton) Tapped(*fyne.PointEvent) {
	if b.OnTap != nil {
		b.OnTap()
	}
}

func (b *DeviceDashboardConnectButton) TappedSecondary(*fyne.PointEvent) {}

func (b *DeviceDashboardConnectButton) Cursor() desktop.Cursor {
	return desktop.PointerCursor
}

func (b *DeviceDashboardConnectButton) MouseIn(*desktop.MouseEvent) {
	b.hovered = true
	b.refreshVisuals()
}

func (b *DeviceDashboardConnectButton) MouseMoved(*desktop.MouseEvent) {}

func (b *DeviceDashboardConnectButton) MouseOut() {
	b.hovered = false
	b.refreshVisuals()
}

func (b *DeviceDashboardConnectButton) refreshVisuals() {
	if b.bg == nil || b.label == nil {
		return
	}
	if b.Mounted {
		fill := color.Color(design.ColorConnectionBadgeText)
		if b.hovered {
			fill = color.NRGBA{R: 0x61, G: 0xf0, B: 0xd3, A: 0xff}
		}
		b.bg.FillColor = fill
		b.bg.StrokeColor = color.Transparent
		b.label.Text = "Connected"
		b.label.Color = design.ColorGray950
	} else {
		b.bg.FillColor = color.Transparent
		stroke := color.Color(design.ColorTailscaleChipBorder)
		if b.hovered {
			stroke = design.ColorConnectionBadgeText
		}
		b.bg.StrokeColor = stroke
		b.label.Text = "Connect"
		b.label.Color = design.ColorConnectionBadgeText
	}
	b.bg.Refresh()
	b.label.Refresh()
}

func (b *DeviceDashboardConnectButton) CreateRenderer() fyne.WidgetRenderer {
	b.bg = canvas.NewRectangle(color.Transparent)
	b.bg.CornerRadius = 6
	b.bg.StrokeWidth = 1

	b.label = canvas.NewText("", design.ColorConnectionBadgeText)
	b.label.TextSize = 9
	b.label.TextStyle.Bold = true

	b.refreshVisuals()
	return widget.NewSimpleRenderer(container.NewStack(b.bg, NewInsetExact(b.label, 8, 8, 3, 3)))
}

// NewDeviceDashboardStorageRow is NewDeviceDashboardRow's Storage-specific
// variant: the same icon+name left side, but its right side can also carry
// a USB Stick/CD-ROM mode picker and Delete/Upload icon buttons -- any of
// which may be nil to omit it -- alongside the same connect button every
// other mountable row has.
func NewDeviceDashboardStorageRow(icon fyne.Resource, name string, active bool, modePicker fyne.CanvasObject, deleteBtn fyne.CanvasObject, uploadBtn fyne.CanvasObject, connectBtn fyne.CanvasObject) fyne.CanvasObject {
	left := newDeviceDashboardRowLeft(icon, name, active)

	var rightParts []fyne.CanvasObject
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
	right := container.New(&DeviceRowControlsLayout{Gap: 8}, rightParts...)

	row := container.NewBorder(nil, nil, left, right)
	return NewInsetExact(row, 0, 0, 2, 2)
}
