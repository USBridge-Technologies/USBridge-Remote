package view

import (
	"image/color"
	"time"

	"usbridge-client/internal/gui/assets"
	"usbridge-client/internal/gui/design"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)

// FooterPromoChip is the left-side Connections footer stand-in for a
// dismissed promo (the Add New Connect tile, or the firmware banner): a
// "+" or a text label that opens the promo's own action, and on hover an
// expand icon that appears inside the same pill (not as a second button
// with footer background showing through the gap). Hidden while the promo
// itself is showing so the footer stays empty.
type FooterPromoChip struct {
	widget.BaseWidget

	label         string
	onOpen        func()
	onRestore     func()
	hovered       bool
	expandHovered bool
	plusBtn       *iconChromeButton
	expandBtn     *iconChromeButton
	bg            *canvas.Rectangle
	box           *fyne.Container
}

var _ desktop.Hoverable = (*FooterPromoChip)(nil)

var (
	footerPromoPlusIcon = fyne.NewStaticResource("footer-promo-plus.svg", []byte(
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="#8f9381"><path d="M19 13h-6v6h-2v-6H5v-2h6V5h2v6h6v2z"/></svg>`))
	footerPromoPlusHoverIcon = fyne.NewStaticResource("footer-promo-plus-hover.svg", []byte(
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="#c5c8b5"><path d="M19 13h-6v6h-2v-6H5v-2h6V5h2v6h6v2z"/></svg>`))
)

func NewFooterPromoChip() *FooterPromoChip {
	return newFooterPromoChip("")
}

// NewFooterLabelChip is FooterPromoChip with a text label instead of "+".
func NewFooterLabelChip(label string) *FooterPromoChip {
	return newFooterPromoChip(label)
}

// FooterTintChip is a always-visible footer text action (Connections'
// "Agent" button) -- same 14px row as the promo chips, tinted rather than
// lime, with no expand/restore affordance.
var (
	_ fyne.Tappable     = (*FooterTintChip)(nil)
	_ desktop.Hoverable = (*FooterTintChip)(nil)
)

type FooterTintChip struct {
	widget.BaseWidget

	label   string
	tint    color.Color
	onTap   func()
	hovered bool
	lbl     *canvas.Text
}

func NewFooterTintChip(label string, tint color.Color, onTap func()) *FooterTintChip {
	c := &FooterTintChip{label: label, tint: tint, onTap: onTap}
	c.ExtendBaseWidget(c)
	return c
}

func (c *FooterTintChip) SetLabel(label string) {
	c.label = label
	if c.lbl != nil {
		c.lbl.Text = label
		c.lbl.Refresh()
	}
	c.Refresh()
}

func (c *FooterTintChip) Tapped(*fyne.PointEvent) {
	if c.onTap != nil {
		c.onTap()
	}
}

func (c *FooterTintChip) TappedSecondary(*fyne.PointEvent) {}

func (c *FooterTintChip) Cursor() desktop.Cursor {
	return desktop.PointerCursor
}

func (c *FooterTintChip) MouseIn(*desktop.MouseEvent) {
	c.hovered = true
	c.refreshVisuals()
}

func (c *FooterTintChip) MouseMoved(*desktop.MouseEvent) {}

func (c *FooterTintChip) MouseOut() {
	c.hovered = false
	c.refreshVisuals()
}

func (c *FooterTintChip) refreshVisuals() {
	if c.lbl == nil {
		return
	}
	if c.hovered {
		c.lbl.Color = design.ColorTextLight
	} else {
		c.lbl.Color = c.tint
	}
	c.lbl.Refresh()
}

func (c *FooterTintChip) CreateRenderer() fyne.WidgetRenderer {
	c.lbl = canvas.NewText(c.label, c.tint)
	c.lbl.TextSize = 9
	c.lbl.TextStyle.Bold = true
	c.refreshVisuals()
	return widget.NewSimpleRenderer(c.lbl)
}

func newFooterPromoChip(label string) *FooterPromoChip {
	c := &FooterPromoChip{label: label}
	c.ExtendBaseWidget(c)
	c.Hide()
	return c
}

func (c *FooterPromoChip) SetOnOpen(fn func()) {
	c.onOpen = fn
}

func (c *FooterPromoChip) SetOnRestore(fn func()) {
	c.onRestore = fn
}

// SetActive shows the chip when its promo is dismissed, and hides it
// when the promo is back on screen.
func (c *FooterPromoChip) SetActive(on bool) {
	if on {
		c.Show()
	} else {
		c.hovered = false
		c.expandHovered = false
		c.Hide()
	}
	c.syncExpand()
	c.Refresh()
}

func (c *FooterPromoChip) MouseIn(*desktop.MouseEvent) {
	c.hovered = true
	c.syncExpand()
}

func (c *FooterPromoChip) MouseOut() {
	c.hovered = false
	// Delay hide so moving onto the expand button (a child widget) doesn't
	// collapse it before the child's own hover arrives -- same as
	// ScriptFooterStatus's dismiss X.
	time.AfterFunc(80*time.Millisecond, func() {
		fyne.Do(c.syncExpand)
	})
}

func (c *FooterPromoChip) MouseMoved(*desktop.MouseEvent) {}

func (c *FooterPromoChip) syncExpand() {
	if c.expandBtn != nil {
		if c.hovered || c.expandHovered {
			c.expandBtn.Show()
		} else {
			c.expandBtn.Hide()
		}
	}
	if c.bg != nil {
		if c.hovered || c.expandHovered {
			c.bg.FillColor = design.ColorSurfaceLight
		} else {
			c.bg.FillColor = color.Transparent
		}
		c.bg.Refresh()
	}
	if c.box != nil {
		c.box.Refresh()
	}
}

func (c *FooterPromoChip) CreateRenderer() fyne.WidgetRenderer {
	btnSize := fyne.NewSize(deviceDashboardBusySpinnerSize, deviceDashboardBusySpinnerSize)
	// Child buttons stay fill-transparent so hover chrome is the shared
	// pill (c.bg), not two separate gray tiles with footer showing between.
	c.expandBtn = newIconChromeButton(iconChromeButtonSpec{
		NormalFill:   color.Transparent,
		HoverFill:    color.Transparent,
		Stroke:       color.Transparent,
		CornerRadius: 3,
		NormalIcon:   assets.ExpandIconMuted,
		HoverIcon:    assets.ExpandIconHover,
		IconSize:     fyne.NewSize(10, 10),
		ButtonSize:   btnSize,
		OnHover: func(on bool) {
			c.expandHovered = on
			c.syncExpand()
		},
		OnTapped: func() {
			if c.onRestore != nil {
				c.onRestore()
			}
		},
	})
	c.expandBtn.Hide()
	openHover := func(on bool) {
		c.hovered = on
		if on {
			c.syncExpand()
			return
		}
		time.AfterFunc(80*time.Millisecond, func() {
			fyne.Do(c.syncExpand)
		})
	}
	openTap := func() {
		if c.onOpen != nil {
			c.onOpen()
		}
	}
	if c.label != "" {
		c.plusBtn = newIconChromeButton(iconChromeButtonSpec{
			NormalFill:      color.Transparent,
			HoverFill:       color.Transparent,
			Stroke:          color.Transparent,
			CornerRadius:    3,
			LabelColor:      design.ColorConnectionAddFill,
			HoverLabelColor: design.ColorConnectionAddFill,
			LabelSize:       9,
			ButtonSize:      fyne.NewSize(0, deviceDashboardBusySpinnerSize),
			OnHover:         openHover,
			OnTapped:        openTap,
		})
		c.plusBtn.SetText(c.label)
	} else {
		c.plusBtn = newIconChromeButton(iconChromeButtonSpec{
			NormalFill:   color.Transparent,
			HoverFill:    color.Transparent,
			Stroke:       color.Transparent,
			CornerRadius: 3,
			NormalIcon:   footerPromoPlusIcon,
			HoverIcon:    footerPromoPlusHoverIcon,
			IconSize:     fyne.NewSize(10, 10),
			ButtonSize:   btnSize,
			OnHover:      openHover,
			OnTapped:     openTap,
		})
	}
	c.bg = canvas.NewRectangle(color.Transparent)
	c.bg.CornerRadius = 3
	c.box = container.New(&DeviceRowControlsLayout{Gap: 2}, c.plusBtn, c.expandBtn)
	c.syncExpand()
	return widget.NewSimpleRenderer(container.NewStack(c.bg, NewInsetExact(c.box, 4, 2, 0, 0)))
}
