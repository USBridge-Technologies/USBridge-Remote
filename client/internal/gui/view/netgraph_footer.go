package view

import (
	"fmt"
	"image/color"
	"sync/atomic"

	"usbridge-client/internal/gui/design"
	"usbridge-client/internal/gui/i18n"
	"usbridge-client/internal/service"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)

// Control Net Graph controls: a graph toggle (teal when the HUD is on,
// gray when off) and a metrics-settings chip that opens a panel with
// size + background-opacity sliders. Desktop places them in the Control
// footer after mouse (NewNetGraphDesktopFooterButtons); mobile places the
// same pair in the Control footer after fullscreen
// (NewNetGraphMobileFooterButtons). Both return nils when the build has
// no HUD push path.
const (
	netGraphHeaderIconSide  = float32(11)
	netGraphHeaderHitSide   = float32(14)
	netGraphMobileIconSide  = float32(16)
	netGraphMobileHitSide   = float32(32)
	netGraphHeaderHoverR    = float32(4)
	netGraphFooterGray      = "#8f9381"
	netGraphFooterGrayHover = "#c5c8b5"
	netGraphFooterTeal      = "#41e0c3"
	netGraphFooterTealHover = "#7aecd4"
	netGraphChartPath       = "M3.5 18.49l6-6.01 4 4L22 6.92l-1.41-1.41-7.09 7.97-4-4L2 16.99z"
	netGraphTunePath        = "M3 17v2h6v-2H3zM3 5v2h10V5H3zm10 16v-2h8v-2h-8v-2h-2v6h2zM7 9v2H3v2h4v2h2V9H7zm14 4v-2H11v2h10zm-6-4h2V7h4V5h-4V3h-2v6z"
	netGraphSettingsSliderW = float32(72)
)

var (
	netGraphIconOff      = netGraphFooterSVG("netgraph-off.svg", netGraphFooterGray, netGraphChartPath)
	netGraphIconOffHover = netGraphFooterSVG("netgraph-off-hover.svg", netGraphFooterGrayHover, netGraphChartPath)
	netGraphIconOn       = netGraphFooterSVG("netgraph-on.svg", netGraphFooterTeal, netGraphChartPath)
	netGraphIconOnHover  = netGraphFooterSVG("netgraph-on-hover.svg", netGraphFooterTealHover, netGraphChartPath)
	netGraphTuneIcon     = netGraphFooterSVG("netgraph-tune.svg", netGraphFooterGray, netGraphTunePath)
	netGraphTuneHover    = netGraphFooterSVG("netgraph-tune-hover.svg", netGraphFooterGrayHover, netGraphTunePath)

	liveNetGraphToggle      atomic.Pointer[netGraphFooterIcon]
	liveNetGraphDialogCheck atomic.Pointer[videoDialogCheckbox]
)

func netGraphFooterSVG(name, fill, path string) fyne.Resource {
	svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="%s"><path d="%s"/></svg>`, fill, path)
	return fyne.NewStaticResource(name, []byte(svg))
}

func applyNetGraphEnabledUI(on bool) {
	if t := liveNetGraphToggle.Load(); t != nil {
		t.setActive(on)
	}
	if c := liveNetGraphDialogCheck.Load(); c != nil {
		c.SetChecked(on)
	}
}

func registerNetGraphDialogCheck(c *videoDialogCheckbox) {
	liveNetGraphDialogCheck.Store(c)
}

// NewNetGraphDesktopFooterButtons is the desktop Control footer pair:
// graph toggle + metrics settings, after keyboard/mouse. Settings open
// upward so the panel is not clipped under the footer. Nils on mobile
// (see NewNetGraphMobileFooterButtons) and when Net Graph isn't available.
func NewNetGraphDesktopFooterButtons() (graph, settings fyne.CanvasObject) {
	if IsMobile() || !service.NetGraphSupported() {
		return nil, nil
	}
	return newNetGraphControlButtons(netGraphHeaderHitSide, netGraphHeaderIconSide, true)
}

// NewNetGraphMobileFooterButtons is the same pair for the mobile Control
// footer (after fullscreen, with video / pan / mouse / keyboard). The
// settings panel opens upward so it isn't clipped under the footer.
// Nils when Net Graph isn't available here.
func NewNetGraphMobileFooterButtons() (graph, settings fyne.CanvasObject) {
	if !IsMobile() || !service.NetGraphSupported() {
		return nil, nil
	}
	return newNetGraphControlButtons(netGraphMobileHitSide, netGraphMobileIconSide, true)
}

func newNetGraphControlButtons(hit, icon float32, settingsAbove bool) (graph, settings fyne.CanvasObject) {
	g := newNetGraphFooterIcon(hit, icon, service.NetGraphEnabled(), func(c *netGraphFooterIcon) {
		on := !service.NetGraphEnabled()
		service.SetNetGraphEnabled(on)
		applyNetGraphEnabledUI(on)
	})
	liveNetGraphToggle.Store(g)
	s := newNetGraphFooterIcon(hit, icon, false, func(c *netGraphFooterIcon) {
		showNetGraphSettingsPanel(c, settingsAbove)
	})
	s.setIcons(netGraphTuneIcon, netGraphTuneHover)
	return g, s
}

type netGraphFooterIcon struct {
	widget.BaseWidget

	active    bool
	hovered   bool
	hitSide   float32
	iconSide  float32
	icon      fyne.Resource
	hoverIcon fyne.Resource
	onTap     func(*netGraphFooterIcon)
	img       *canvas.Image
	bg        *canvas.Rectangle
}

var (
	_ fyne.Tappable      = (*netGraphFooterIcon)(nil)
	_ desktop.Hoverable  = (*netGraphFooterIcon)(nil)
	_ desktop.Cursorable = (*netGraphFooterIcon)(nil)
)

func newNetGraphFooterIcon(hit, icon float32, active bool, onTap func(*netGraphFooterIcon)) *netGraphFooterIcon {
	c := &netGraphFooterIcon{onTap: onTap, hitSide: hit, iconSide: icon}
	c.ExtendBaseWidget(c)
	c.setActive(active)
	return c
}

func (c *netGraphFooterIcon) setActive(on bool) {
	c.active = on
	if on {
		c.setIcons(netGraphIconOn, netGraphIconOnHover)
		return
	}
	c.setIcons(netGraphIconOff, netGraphIconOffHover)
}

func (c *netGraphFooterIcon) setIcons(icon, hover fyne.Resource) {
	c.icon = icon
	c.hoverIcon = hover
	c.refreshVisuals()
}

func (c *netGraphFooterIcon) Tapped(*fyne.PointEvent) {
	if c.onTap != nil {
		c.onTap(c)
	}
}

func (c *netGraphFooterIcon) TappedSecondary(*fyne.PointEvent) {}

func (c *netGraphFooterIcon) Cursor() desktop.Cursor {
	return desktop.PointerCursor
}

func (c *netGraphFooterIcon) MouseIn(*desktop.MouseEvent) {
	c.hovered = true
	c.refreshVisuals()
}

func (c *netGraphFooterIcon) MouseMoved(*desktop.MouseEvent) {}

func (c *netGraphFooterIcon) MouseOut() {
	c.hovered = false
	c.refreshVisuals()
}

func (c *netGraphFooterIcon) hoverFill() color.Color {
	if c.hitSide >= netGraphMobileHitSide {
		return design.ColorAlphaWhite07
	}
	return design.ColorStatusBarIconChip
}

func (c *netGraphFooterIcon) hoverRadius() float32 {
	if c.hitSide >= netGraphMobileHitSide {
		return c.hitSide / 2
	}
	return netGraphHeaderHoverR
}

func (c *netGraphFooterIcon) refreshVisuals() {
	if c.bg != nil {
		if c.hovered {
			c.bg.FillColor = c.hoverFill()
		} else {
			c.bg.FillColor = color.Transparent
		}
		c.bg.CornerRadius = c.hoverRadius()
		c.bg.Refresh()
	}
	if c.img == nil || c.icon == nil {
		return
	}
	res := c.icon
	if c.hovered && c.hoverIcon != nil {
		res = c.hoverIcon
	}
	c.img.Resource = res
	c.img.Refresh()
}

func (c *netGraphFooterIcon) MinSize() fyne.Size {
	return fyne.NewSize(c.hitSide, c.hitSide)
}

func (c *netGraphFooterIcon) CreateRenderer() fyne.WidgetRenderer {
	c.bg = canvas.NewRectangle(color.Transparent)
	c.bg.CornerRadius = c.hoverRadius()
	c.img = canvas.NewImageFromResource(c.icon)
	c.img.FillMode = canvas.ImageFillContain
	c.refreshVisuals()
	return &netGraphFooterIconRenderer{icon: c, objects: []fyne.CanvasObject{c.bg, c.img}}
}

type netGraphFooterIconRenderer struct {
	icon    *netGraphFooterIcon
	objects []fyne.CanvasObject
}

func (r *netGraphFooterIconRenderer) Layout(size fyne.Size) {
	r.icon.bg.Resize(size)
	r.icon.bg.Move(fyne.NewPos(0, 0))
	side := r.icon.iconSide
	r.icon.img.Resize(fyne.NewSize(side, side))
	r.icon.img.Move(fyne.NewPos((size.Width-side)/2, (size.Height-side)/2))
}

func (r *netGraphFooterIconRenderer) MinSize() fyne.Size {
	return fyne.NewSize(r.icon.hitSide, r.icon.hitSide)
}

func (r *netGraphFooterIconRenderer) Refresh() {
	r.icon.refreshVisuals()
	r.Layout(r.icon.Size())
	canvas.Refresh(r.icon)
}

func (r *netGraphFooterIconRenderer) Objects() []fyne.CanvasObject { return r.objects }

func (r *netGraphFooterIconRenderer) Destroy() {}

func showNetGraphSettingsPanel(anchor fyne.CanvasObject, openAbove bool) {
	sizeLbl := "Size"
	bgLbl := "Background"
	if i18n.Current != nil {
		if s := i18n.Current.NetGraphSize; s != "" {
			sizeLbl = s
		}
		if s := i18n.Current.NetGraphBackground; s != "" {
			bgLbl = s
		}
	}

	mkLabel := func(s string) *canvas.Text {
		t := canvas.NewText(s, design.ColorTextMuted)
		t.TextSize = 8
		return t
	}
	sizeTitle := mkLabel(sizeLbl)
	bgTitle := mkLabel(bgLbl)
	labelW := sizeTitle.MinSize().Width
	if w := bgTitle.MinSize().Width; w > labelW {
		labelW = w
	}
	pctW := mkLabel("150%").MinSize().Width

	sizePct := mkLabel(fmt.Sprintf("%d%%", service.NetGraphScalePercent()))
	sizePct.Alignment = fyne.TextAlignTrailing
	sizeSlider := newSizeMenuSlider(50, 150, 5, float64(service.NetGraphScalePercent()))
	sizeSlider.OnChanged = func(v float64) {
		service.SetNetGraphScale(int(v))
		sizePct.Text = fmt.Sprintf("%d%%", service.NetGraphScalePercent())
		sizePct.Refresh()
	}

	alphaPct := int((float64(service.NetGraphBgAlpha())*100.0)/255.0 + 0.5)
	bgVal := mkLabel(fmt.Sprintf("%d%%", alphaPct))
	bgVal.Alignment = fyne.TextAlignTrailing
	bgSlider := newSizeMenuSlider(0, 100, 1, float64(alphaPct))
	bgSlider.OnChanged = func(v float64) {
		service.SetNetGraphBgAlpha(uint8(v*255.0/100.0 + 0.5))
		bgVal.Text = fmt.Sprintf("%d%%", int(v))
		bgVal.Refresh()
	}

	row := func(title *canvas.Text, slider fyne.CanvasObject, pct *canvas.Text) fyne.CanvasObject {
		return container.New(&DeviceRowControlsLayout{Gap: 8},
			container.NewGridWrap(fyne.NewSize(labelW, sizeMenuSliderH), title),
			container.NewGridWrap(fyne.NewSize(netGraphSettingsSliderW, sizeMenuSliderH), slider),
			container.NewGridWrap(fyne.NewSize(pctW, sizeMenuSliderH), pct),
		)
	}
	content := container.NewVBox(
		row(sizeTitle, sizeSlider, sizePct),
		NewInset(row(bgTitle, bgSlider, bgVal), 0, 0, 8, 0),
	)
	showStyledPanel(anchor, content, 0, openAbove)
}
