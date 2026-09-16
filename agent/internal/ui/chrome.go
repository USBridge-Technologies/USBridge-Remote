package ui

import (
	"fmt"
	"image/color"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"

	"usbridge_agent/assets"
	"usbridge_agent/internal/ui/design"
)

type chromePalette struct {
	Kind         string
	Accent       color.NRGBA
	AccentHover  color.NRGBA
	CardHover    color.NRGBA
	HeaderLine   color.NRGBA
	OnAccent     color.NRGBA
	IconHex      string
	Logo         fyne.Resource
	HeaderLineOn bool
}

func chromeForProtocol(kind string) chromePalette {
	switch kind {
	case protocolOpensource:
		return chromePalette{
			Kind:         protocolOpensource,
			Accent:       design.ColorTextLight,
			AccentHover:  design.ColorWhite,
			CardHover:    design.ColorMutedOlive,
			HeaderLine:   design.ColorAlphaWhite24,
			OnAccent:     design.ColorGray950,
			IconHex:      "#f5f5f5",
			Logo:         assets.LogoUSBridgeLockupOpen,
			HeaderLineOn: true,
		}
	case protocolPro, protocolEnterprise:
		return chromePalette{
			Kind:         kind,
			Accent:       design.ColorProSoft,
			AccentHover:  design.ColorProSoftHover,
			CardHover:    design.ColorProLine,
			HeaderLine:   design.ColorProLine,
			OnAccent:     design.ColorGray950,
			IconHex:      "#b39ef1",
			Logo:         assets.LogoUSBridgeLockupPro,
			HeaderLineOn: true,
		}
	default:
		return chromePalette{
			Kind:         protocolFree,
			Accent:       design.ColorTeal,
			AccentHover:  design.ColorTealHover,
			CardHover:    design.ColorTealLine,
			HeaderLine:   design.ColorTealLine,
			OnAccent:     design.ColorBackground,
			IconHex:      "#41e0c3",
			Logo:         assets.LogoUSBridgeLockupFree,
			HeaderLineOn: true,
		}
	}
}

var (
	chromeMu       sync.Mutex
	chromeNow      = chromeForProtocol(protocolFree)
	chromePin      string
	chromeProtocol = protocolFree
	chromeWidgets  []fyne.Widget
	chromePanels   []*themedPanel
	chromeUnhover  *time.Timer
	brandLockup    *canvas.Image
	overlayDepth   int
)

func currentChrome() chromePalette {
	chromeMu.Lock()
	defer chromeMu.Unlock()
	return chromeNow
}

func chromePinned() string {
	chromeMu.Lock()
	defer chromeMu.Unlock()
	return chromePin
}

func effectiveChromeKind(pin, proto string) string {
	switch pin {
	case protocolOpensource, protocolFree, protocolPro:
		return pin
	default:
		if proto == "" {
			return protocolFree
		}
		return proto
	}
}

func applyChromePalette(next chromePalette) {
	chromeMu.Lock()
	if chromeNow.Kind == next.Kind {
		chromeMu.Unlock()
		return
	}
	chromeNow = next
	logo := brandLockup
	widgets := append([]fyne.Widget(nil), chromeWidgets...)
	panels := append([]*themedPanel(nil), chromePanels...)
	chromeMu.Unlock()
	if logo != nil && next.Logo != nil {
		logo.Resource = next.Logo
		logo.Refresh()
	}
	for _, p := range panels {
		p.applyChrome()
	}
	for _, w := range widgets {
		if _, ok := w.(*themedPanel); ok {
			continue
		}
		w.Refresh()
	}
}

func setChromeKind(kind string) {
	chromeMu.Lock()
	chromeProtocol = kind
	pin := chromePin
	chromeMu.Unlock()
	applyChromePalette(chromeForProtocol(effectiveChromeKind(pin, kind)))
}

func setChromePin(pin string) {
	chromeMu.Lock()
	chromePin = pin
	proto := chromeProtocol
	chromeMu.Unlock()
	applyChromePalette(chromeForProtocol(effectiveChromeKind(pin, proto)))
}

func registerChromeWidget(w fyne.Widget) {
	if w == nil {
		return
	}
	chromeMu.Lock()
	chromeWidgets = append(chromeWidgets, w)
	chromeMu.Unlock()
}

func registerThemedPanel(p *themedPanel) {
	if p == nil {
		return
	}
	chromeMu.Lock()
	chromePanels = append(chromePanels, p)
	chromeMu.Unlock()
}

func recolorSVGFill(res fyne.Resource, hex string) fyne.Resource {
	if res == nil {
		return nil
	}
	s := string(res.Content())
	s = strings.ReplaceAll(s, `fill="#41e0c3"`, `fill="`+hex+`"`)
	s = strings.ReplaceAll(s, `fill="#41E0C3"`, `fill="`+hex+`"`)
	return fyne.NewStaticResource(fmt.Sprintf("%s-%s", res.Name(), hex), []byte(s))
}

func absInObject(o fyne.CanvasObject, abs fyne.Position) bool {
	if o == nil || !o.Visible() {
		return false
	}
	app := fyne.CurrentApp()
	if app == nil {
		return false
	}
	pos := app.Driver().AbsolutePositionForObject(o)
	size := o.Size()
	return abs.X >= pos.X && abs.Y >= pos.Y && abs.X <= pos.X+size.Width && abs.Y <= pos.Y+size.Height
}

func applyCardHovers(abs fyne.Position) {
	chromeMu.Lock()
	panels := append([]*themedPanel(nil), chromePanels...)
	chromeMu.Unlock()
	for _, p := range panels {
		p.setHovered(absInObject(p, abs))
	}
}

func clearCardHovers() {
	chromeMu.Lock()
	panels := append([]*themedPanel(nil), chromePanels...)
	chromeMu.Unlock()
	for _, p := range panels {
		p.setHovered(false)
	}
}

func beginOverlay() {
	chromeMu.Lock()
	overlayDepth++
	chromeMu.Unlock()
	clearCardHovers()
}

func endOverlay() {
	chromeMu.Lock()
	if overlayDepth > 0 {
		overlayDepth--
	}
	chromeMu.Unlock()
}

func overlayBlockingChrome() bool {
	chromeMu.Lock()
	defer chromeMu.Unlock()
	return overlayDepth > 0
}

func noteChromeHoverIn(abs fyne.Position) {
	if overlayBlockingChrome() {
		return
	}
	chromeMu.Lock()
	if chromeUnhover != nil {
		chromeUnhover.Stop()
		chromeUnhover = nil
	}
	chromeMu.Unlock()
	applyCardHovers(abs)
}

func noteChromeHoverOut() {
	chromeMu.Lock()
	if chromeUnhover != nil {
		chromeUnhover.Stop()
	}
	chromeUnhover = time.AfterFunc(50*time.Millisecond, func() {
		fyne.Do(clearCardHovers)
	})
	chromeMu.Unlock()
}

type themedPanel struct {
	widget.BaseWidget
	bg       *canvas.Rectangle
	body     fyne.CanvasObject
	stack    *fyne.Container
	icon     *canvas.Image
	iconBase fyne.Resource
	hovered  bool
}

func (p *themedPanel) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(p.stack)
}

func (p *themedPanel) MouseIn(ev *desktop.MouseEvent) {
	if ev != nil {
		noteChromeHoverIn(ev.AbsolutePosition)
	}
}

func (p *themedPanel) MouseMoved(ev *desktop.MouseEvent) {
	if ev != nil {
		noteChromeHoverIn(ev.AbsolutePosition)
	}
}

func (p *themedPanel) MouseOut() { noteChromeHoverOut() }

func (p *themedPanel) setHovered(on bool) {
	if p.hovered == on {
		return
	}
	p.hovered = on
	p.refreshStroke()
}

func (p *themedPanel) refreshStroke() {
	if p.bg == nil {
		return
	}
	if p.hovered {
		p.bg.StrokeColor = currentChrome().CardHover
	} else {
		p.bg.StrokeColor = design.ColorTailscaleChipBorder
	}
	p.bg.Refresh()
}

func (p *themedPanel) applyChrome() {
	if p.icon != nil && p.iconBase != nil {
		p.icon.Resource = recolorSVGFill(p.iconBase, currentChrome().IconHex)
		p.icon.Refresh()
	}
	p.refreshStroke()
}
