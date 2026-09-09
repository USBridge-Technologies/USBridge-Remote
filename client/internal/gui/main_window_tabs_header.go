package gui

// main_window_tabs_header.go -- the Control/Devices/Snapshots/Scripts
// selector now living in createMainAddressBar's own left zone instead of
// mw.tabs' native tab strip (see MainWindow.tabHeaderButtons' own doc
// comment for why mw.tabs itself is never actually shown anymore, and
// applyTabVisualState for what drives both these buttons' selected look and
// mw.tabs.Items[i].Content's visibility).

import (
	"image/color"

	"usbridge-client/internal/gui/assets"
	"usbridge-client/internal/gui/view"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)

// headerTabButtonMuted/Selected/Hover are this button's own icon+text color
// scheme -- no background fill at all (unlike this header's other buttons),
// just these three text/icon colors plus a persistent underline while
// selected, matching a reference design handed over for this row
// specifically.
var (
	headerTabButtonMuted    = color.NRGBA{R: 0xc5, G: 0xc8, B: 0xb5, A: 0xff}
	headerTabButtonSelected = color.NRGBA{R: 0xeb, G: 0xff, B: 0xbc, A: 0xff}
	headerTabButtonHover    = color.NRGBA{R: 0xe0, G: 0xe3, B: 0xe7, A: 0xff}
)

const (
	headerTabButtonIconSize     = float32(15)
	headerTabButtonTextSize     = float32(10)
	headerTabButtonGap          = float32(6)
	headerTabButtonUnderlineGap = float32(4)
	headerTabButtonUnderlineH   = float32(1)
)

// headerTabButton is one entry in the header's tab selector -- an icon next
// to a text label, colored (not backgrounded) for its normal/hovered/
// selected state, with a persistent underline while selected.
type headerTabButton struct {
	widget.BaseWidget

	iconMuted    fyne.Resource
	iconSelected fyne.Resource
	iconHover    fyne.Resource
	label        string
	onTapped     func()
	selected     bool
	hovered      bool

	icon      *canvas.Image
	text      *canvas.Text
	underline *canvas.Rectangle
}

func newHeaderTabButton(iconMuted, iconSelected, iconHover fyne.Resource, label string, onTapped func()) *headerTabButton {
	b := &headerTabButton{
		iconMuted:    iconMuted,
		iconSelected: iconSelected,
		iconHover:    iconHover,
		label:        label,
		onTapped:     onTapped,
	}
	b.ExtendBaseWidget(b)
	return b
}

// SetSelected reflects applyTabVisualState's own activeIndex onto this
// button's color/underline.
func (b *headerTabButton) SetSelected(selected bool) {
	if b.selected == selected {
		return
	}
	b.selected = selected
	b.refreshVisuals()
}

func (b *headerTabButton) Tapped(*fyne.PointEvent) {
	if b.onTapped != nil {
		b.onTapped()
	}
}

func (b *headerTabButton) TappedSecondary(*fyne.PointEvent) {}

func (b *headerTabButton) MouseIn(*desktop.MouseEvent) {
	b.hovered = true
	b.refreshVisuals()
}

func (b *headerTabButton) MouseMoved(*desktop.MouseEvent) {}

func (b *headerTabButton) MouseOut() {
	b.hovered = false
	b.refreshVisuals()
}

func (b *headerTabButton) CreateRenderer() fyne.WidgetRenderer {
	b.icon = canvas.NewImageFromResource(b.iconMuted)
	b.icon.FillMode = canvas.ImageFillContain
	b.icon.SetMinSize(fyne.NewSize(headerTabButtonIconSize, headerTabButtonIconSize))

	// Never bold -- toggling weight by selected state used to shift each
	// button's own width (and everything after it) slightly on every tab
	// switch, so every state stays the same regular weight now; only the
	// color and the underline change.
	b.text = view.NewBrandText(b.label, headerTabButtonTextSize, headerTabButtonMuted, false)

	b.underline = canvas.NewRectangle(headerTabButtonSelected)
	b.underline.Hide()

	r := &headerTabButtonRenderer{
		button:  b,
		objects: []fyne.CanvasObject{b.icon, b.text, b.underline},
	}
	b.refreshVisuals()
	return r
}

func (b *headerTabButton) refreshVisuals() {
	if b.icon == nil || b.text == nil || b.underline == nil {
		return
	}

	textColor := headerTabButtonMuted
	iconRes := b.iconMuted
	switch {
	case b.selected:
		textColor = headerTabButtonSelected
		iconRes = b.iconSelected
	case b.hovered:
		textColor = headerTabButtonHover
		if b.iconHover != nil {
			iconRes = b.iconHover
		}
	}

	b.text.Color = textColor
	b.text.Refresh()
	b.icon.Resource = iconRes
	b.icon.Refresh()

	if b.selected {
		b.underline.Show()
	} else {
		b.underline.Hide()
	}
	b.underline.Refresh()
}

type headerTabButtonRenderer struct {
	button  *headerTabButton
	objects []fyne.CanvasObject
}

func (r *headerTabButtonRenderer) contentSize() (iconSize, textSize fyne.Size) {
	iconSize = fyne.NewSize(headerTabButtonIconSize, headerTabButtonIconSize)
	textSize = r.button.text.MinSize()
	return
}

func (r *headerTabButtonRenderer) Layout(size fyne.Size) {
	iconSize, textSize := r.contentSize()
	rowHeight := iconSize.Height
	if textSize.Height > rowHeight {
		rowHeight = textSize.Height
	}
	rowWidth := iconSize.Width + headerTabButtonGap + textSize.Width

	x := (size.Width - rowWidth) / 2
	if x < 0 {
		x = 0
	}
	rowY := float32(0)

	r.button.icon.Move(fyne.NewPos(x, rowY+(rowHeight-iconSize.Height)/2))
	r.button.icon.Resize(iconSize)

	textX := x + iconSize.Width + headerTabButtonGap
	r.button.text.Move(fyne.NewPos(textX, rowY+(rowHeight-textSize.Height)/2))
	r.button.text.Resize(textSize)

	underlineY := rowY + rowHeight + headerTabButtonUnderlineGap
	r.button.underline.Move(fyne.NewPos(x, underlineY))
	r.button.underline.Resize(fyne.NewSize(rowWidth, headerTabButtonUnderlineH))
}

func (r *headerTabButtonRenderer) MinSize() fyne.Size {
	iconSize, textSize := r.contentSize()
	rowHeight := iconSize.Height
	if textSize.Height > rowHeight {
		rowHeight = textSize.Height
	}
	rowWidth := iconSize.Width + headerTabButtonGap + textSize.Width
	height := rowHeight + headerTabButtonUnderlineGap + headerTabButtonUnderlineH
	return fyne.NewSize(rowWidth, height)
}

func (r *headerTabButtonRenderer) Refresh() {
	r.button.refreshVisuals()
	r.Layout(r.button.Size())
	canvas.Refresh(r.button)
}

func (r *headerTabButtonRenderer) BackgroundColor() color.Color {
	return color.Transparent
}

func (r *headerTabButtonRenderer) Objects() []fyne.CanvasObject {
	return r.objects
}

func (r *headerTabButtonRenderer) Destroy() {}

var (
	_ fyne.Tappable     = (*headerTabButton)(nil)
	_ desktop.Hoverable = (*headerTabButton)(nil)
	_ fyne.Widget       = (*headerTabButton)(nil)
)

// buildTabHeaderButtons builds the Control/Devices/Snapshots/Scripts row and
// fills mw.tabHeaderButtons, wiring each button to select the matching
// mw.tabs item -- the exact same jump every existing icon (mouseIcon,
// gamepadIcon, snapshotIcon, ...) already makes, just now the primary way to
// switch tabs at all rather than a shortcut into a native tab strip.
func (mw *MainWindow) buildTabHeaderButtons() fyne.CanvasObject {
	specs := [4]struct {
		iconMuted, iconSelected, iconHover fyne.Resource
		label                              string
		index                              func() int
	}{
		{assets.MonitorTabIconMuted, assets.MonitorTabIconSelected, assets.MonitorTabIconHover, "Control", mw.controlTabIndex},
		{assets.USBTabIconMuted, assets.USBTabIconSelected, assets.USBTabIconHover, "Devices", mw.devicesTabIndex},
		{assets.SnapshotsTabIconMuted, assets.SnapshotsTabIconSelected, assets.SnapshotsTabIconHover, "Snapshots", mw.snapshotsTabIndex},
		{assets.ScriptsTabIconMuted, assets.ScriptsTabIconSelected, assets.ScriptsTabIconHover, "Scripts", mw.scriptsTabIndex},
	}

	objs := make([]fyne.CanvasObject, 0, len(specs))
	for i, spec := range specs {
		idx := spec.index()
		btn := newHeaderTabButton(spec.iconMuted, spec.iconSelected, spec.iconHover, spec.label, func() {
			if mw.tabs != nil && len(mw.tabs.Items) > idx {
				mw.tabs.Select(mw.tabs.Items[idx])
			}
		})
		mw.tabHeaderButtons[i] = btn
		objs = append(objs, btn)
	}

	return container.New(&centeredInlineLayout{gap: 16, minGap: 8}, objs...)
}
