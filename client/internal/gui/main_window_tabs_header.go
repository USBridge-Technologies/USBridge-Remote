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
	"usbridge-client/internal/gui/design"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	fynetheme "fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// headerTabButton is one entry in the header's tab selector -- an icon-only
// button (headerCompactButtonSize, same as this header's other buttons)
// that swaps to iconActive and gets a persistent background fill while
// SetSelected(true), on top of the same transient hover fill every other
// button in this header already uses.
type headerTabButton struct {
	widget.BaseWidget

	iconNormal fyne.Resource
	// iconActive is nil for tabs with no distinct "selected" icon asset
	// (Scripts, see buildTabHeaderButtons) -- the background fill alone
	// still carries the selected state then.
	iconActive fyne.Resource
	onTapped   func()
	selected   bool
	hovered    bool

	bg   *canvas.Rectangle
	icon *canvas.Image
}

func newHeaderTabButton(iconNormal, iconActive fyne.Resource, onTapped func()) *headerTabButton {
	b := &headerTabButton{iconNormal: iconNormal, iconActive: iconActive, onTapped: onTapped}
	b.ExtendBaseWidget(b)
	return b
}

// SetSelected reflects applyTabVisualState's own activeIndex onto this
// button's persistent highlight/active-icon look.
func (b *headerTabButton) SetSelected(selected bool) {
	if b.selected == selected {
		return
	}
	b.selected = selected
	b.refreshVisuals()
}

func (b *headerTabButton) MinSize() fyne.Size {
	return headerCompactButtonSize
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
	b.bg = canvas.NewRectangle(color.Transparent)
	b.bg.CornerRadius = design.RadiusMD

	b.icon = canvas.NewImageFromResource(b.iconNormal)
	b.icon.FillMode = canvas.ImageFillContain
	b.icon.SetMinSize(fyne.NewSize(16, 16))

	b.refreshVisuals()
	return widget.NewSimpleRenderer(container.NewStack(b.bg, container.NewCenter(b.icon)))
}

func (b *headerTabButton) refreshVisuals() {
	if b.bg == nil || b.icon == nil {
		return
	}

	fill := color.Color(color.Transparent)
	iconRes := b.iconNormal
	switch {
	case b.selected:
		fill = design.ColorSurfaceLight
		if b.iconActive != nil {
			iconRes = b.iconActive
		}
	case b.hovered:
		fill = color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0x10}
	}

	b.bg.FillColor = fill
	b.icon.Resource = iconRes
	b.bg.Refresh()
	b.icon.Refresh()
}

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
		icon, iconActive fyne.Resource
		index            func() int
	}{
		{assets.MonitorTabIcon, assets.MonitorTabIconActive, mw.controlTabIndex},
		{assets.USBTabIcon, assets.USBTabIconActive, mw.devicesTabIndex},
		{assets.SnapshotsTabIcon, assets.SnapshotsTabIconActive, mw.snapshotsTabIndex},
		// No dedicated "active" scripts-tab asset (see mw.scriptIcon's own
		// status-panel icon, fynetheme.MediaPlayIcon() -- same situation) --
		// this button's selected background fill alone carries that state.
		{fynetheme.MediaPlayIcon(), nil, mw.scriptsTabIndex},
	}

	objs := make([]fyne.CanvasObject, 0, len(specs))
	for i, spec := range specs {
		idx := spec.index()
		btn := newHeaderTabButton(spec.icon, spec.iconActive, func() {
			if mw.tabs != nil && len(mw.tabs.Items) > idx {
				mw.tabs.Select(mw.tabs.Items[idx])
			}
		})
		mw.tabHeaderButtons[i] = btn
		objs = append(objs, container.NewGridWrap(headerCompactButtonSize, btn))
	}

	return container.New(&centeredInlineLayout{gap: 4, minGap: 2}, objs...)
}
