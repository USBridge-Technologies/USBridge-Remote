package view

import (
	"fmt"
	"image/color"
	"net/url"
	"strings"

	"usbridge-client/internal/gui/assets"
	"usbridge-client/internal/gui/design"
	"usbridge-client/internal/gui/i18n"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)

const (
	whatsNewDialogWidthDesktop float32 = 520
	whatsNewBodyWidthDesktop   float32 = 480
	whatsNewScrollMaxDesktop   float32 = 400
	whatsNewScrollGutter       float32 = 14
	whatsNewChipRadius         float32 = 3
	whatsNewIconSize           float32 = 24
	whatsNewIconGlyph          float32 = 13
	whatsNewIconGap            float32 = 10
	whatsNewRowPad             float32 = 12
	whatsNewChangelogURL               = "https://github.com/USBridge-Technologies/USBridge-Remote/releases"
)

// Card outline is a step darker than the window's design.ColorBorder (#656565).
var whatsNewCardStroke = color.NRGBA{R: 0x3f, G: 0x3f, B: 0x3f, A: 0xff}

type whatsNewKindChrome struct {
	Fill   color.NRGBA
	Stroke color.NRGBA
	Accent color.NRGBA
}

var (
	whatsNewChromeBeta = whatsNewKindChrome{
		Fill:   color.NRGBA{R: 0x1f, G: 0x1b, B: 0x13, A: 0xff},
		Stroke: color.NRGBA{R: 0x40, G: 0x31, B: 0x14, A: 0xff},
		Accent: color.NRGBA{R: 0xde, G: 0x90, B: 0x0c, A: 0xff},
	}
	whatsNewChromeFree = whatsNewKindChrome{
		Fill:   color.NRGBA{R: 0x10, G: 0x25, B: 0x23, A: 0xff},
		Stroke: color.NRGBA{R: 0x1b, G: 0x4b, B: 0x45, A: 0xff},
		Accent: color.NRGBA{R: 0x30, G: 0xd4, B: 0xbd, A: 0xff},
	}
	whatsNewChromePro = whatsNewKindChrome{
		Fill:   color.NRGBA{R: 0x1f, G: 0x16, B: 0x2b, A: 0xff},
		Stroke: color.NRGBA{R: 0x36, G: 0x1e, B: 0x4e, A: 0xff},
		Accent: color.NRGBA{R: 0xb3, G: 0x9e, B: 0xf1, A: 0xff},
	}
	whatsNewChromeGray = whatsNewKindChrome{
		Fill:   color.NRGBA{R: 0x1d, G: 0x23, B: 0x2b, A: 0xff},
		Stroke: color.NRGBA{R: 0x4f, G: 0x51, B: 0x53, A: 0xff},
		Accent: color.NRGBA{R: 0xc5, G: 0xc8, B: 0xb5, A: 0xff},
	}
)

var whatsNewHost fyne.Window

var whatsNewMetricsSVG = fyne.NewStaticResource("whatsnew_metrics.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="#c5c8b5" stroke-width="1.8" stroke-linecap="round"><path d="M5 19V11M10 19V6M15 19v-8M20 19V8"/></svg>`))

// SetWhatsNewHost records the main window so the footer version can open
// the What's new overlay.
func SetWhatsNewHost(win fyne.Window) {
	whatsNewHost = win
}

func whatsNewParentWindow() fyne.Window {
	if whatsNewHost != nil {
		return whatsNewHost
	}
	if fyne.CurrentApp() == nil || fyne.CurrentApp().Driver() == nil {
		return nil
	}
	wins := fyne.CurrentApp().Driver().AllWindows()
	if len(wins) == 0 {
		return nil
	}
	return wins[0]
}

func whatsNewCanvasSize() fyne.Size {
	if win := whatsNewParentWindow(); win != nil && win.Canvas() != nil {
		if sz := win.Canvas().Size(); sz.Width > 1 && sz.Height > 1 {
			return sz
		}
	}
	if IsMobile() {
		return fyne.NewSize(360, 640)
	}
	return fyne.NewSize(900, 700)
}

func whatsNewDialogMetrics() (dialogW, bodyW, scrollMax float32) {
	return whatsNewDialogMetricsFor(whatsNewCanvasSize())
}

func whatsNewDialogMetricsFor(canvas fyne.Size) (dialogW, bodyW, scrollMax float32) {
	if !IsMobile() {
		return whatsNewDialogWidthDesktop, whatsNewBodyWidthDesktop, whatsNewScrollMaxDesktop
	}
	if canvas.Width < 1 {
		canvas.Width = 360
	}
	if canvas.Height < 1 {
		canvas.Height = 640
	}
	side := float32(12)
	dialogW = canvas.Width - side*2
	if dialogW < 1 {
		dialogW = canvas.Width
	}
	bodyW = dialogW - 24
	if bodyW < 1 {
		bodyW = dialogW
	}
	// Cap at the canvas so MinSize doesn't force a taller panel than
	// PanelSize can actually give; leftover height still goes to the
	// scroll body via Border.
	scrollMax = maxFloat32(180, canvas.Height-side*2-160)
	return dialogW, bodyW, scrollMax
}

// ShowWhatsNewDialog is the post-update appeal: one branded card at a
// time, paged through whatsNewCatalog. Opened from the footer version
// and the phone Connections settings menu.
func ShowWhatsNewDialog(parent fyne.Window) {
	if parent == nil {
		parent = whatsNewParentWindow()
	}
	if parent == nil || i18n.Current == nil {
		return
	}
	cards := whatsNewCatalog()
	if len(cards) == 0 {
		return
	}
	markWhatsNewCatalogSeen()
	dialogW, bodyW, scrollMax := whatsNewDialogMetrics()

	idx := 0
	body := container.NewStack()
	version := newWhatsNewVersionChip()
	dateLbl := canvas.NewText("", design.ColorConnectionsSectionSubtitle)
	dateLbl.TextSize = 9
	var popup *widget.PopUp
	closeDialog := func() {
		if popup != nil {
			popup.Hide()
		}
	}

	var paint func()
	older := newWhatsNewNav("‹", func() {
		if idx < len(cards)-1 {
			idx++
			paint()
		}
	})
	newer := newWhatsNewNav("›", func() {
		if idx > 0 {
			idx--
			paint()
		}
	})
	paint = func() {
		if idx < 0 {
			idx = 0
		}
		if idx >= len(cards) {
			idx = len(cards) - 1
		}
		card := cards[idx]
		version.SetVersion(formatWhatsNewVersion(card.Version))
		dateLbl.Text = strings.TrimSpace(card.Date)
		if dateLbl.Text == "" {
			dateLbl.Hide()
		} else {
			dateLbl.Show()
		}
		dateLbl.Refresh()
		body.Objects = []fyne.CanvasObject{newWhatsNewCardView(card, bodyW, scrollMax)}
		body.Refresh()
		if idx >= len(cards)-1 {
			older.Hide()
		} else {
			older.Show()
		}
		if idx <= 0 {
			newer.Hide()
		} else {
			newer.Show()
		}
		if popup != nil {
			popup.Refresh()
		}
	}
	paint()

	titleSize := float32(16)
	headerPadL, headerPadR, headerPadT, headerPadB := float32(21), float32(44), float32(14), float32(14)
	bodyPad := float32(20)
	footerPad := float32(20)
	if IsMobile() {
		titleSize = 14
		headerPadL, headerPadR, headerPadT, headerPadB = 16, 40, 10, 10
		bodyPad = 12
		footerPad = 14
	}
	title := NewBrandText(i18n.Current.WhatsNewTitle, titleSize, design.ColorTextLight, true)
	subtitleW := bodyW - 80
	if IsMobile() {
		subtitleW = dialogW - headerPadL - headerPadR
		if subtitleW < 160 {
			subtitleW = 160
		}
	}
	subtitle := newVideoDialogWrapText(subtitleW, 10, false, videoDialogWrapSpan{
		Text:  i18n.Current.WhatsNewSubtitle,
		Color: design.ColorConnectionsSectionSubtitle,
	})
	var headerInner fyne.CanvasObject
	if IsMobile() {
		meta := container.New(&DeviceRowControlsLayout{Gap: 8}, version, dateLbl, older, newer)
		headerInner = container.New(&tightStatsVBoxLayout{Gap: 4}, title, meta, subtitle)
	} else {
		titleRow := container.New(&DeviceRowControlsLayout{Gap: 8}, title, version, dateLbl, older, newer)
		headerInner = container.New(&tightStatsVBoxLayout{Gap: 4}, titleRow, subtitle)
	}
	closeBtn := newIconChromeButton(iconChromeButtonSpec{
		NormalFill: color.Transparent,
		HoverFill:  design.ColorSurfaceLight,
		NormalIcon: videoDialogCancelIconSVG,
		HoverIcon:  videoDialogCancelIconSVG,
		IconSize:   fyne.NewSize(18, 18),
		ButtonSize: fyne.NewSize(28, 28),
		OnTapped:   closeDialog,
	})
	headerSepLine := color.NRGBA{R: 0x30, G: 0x34, B: 0x2e, A: 0xff}
	headerSep := canvas.NewRectangle(headerSepLine)
	headerSep.SetMinSize(fyne.NewSize(0, 1))
	footerSep := canvas.NewRectangle(headerSepLine)
	footerSep.SetMinSize(fyne.NewSize(0, 1))
	headerBlock := container.NewVBox(
		newVideoDialogTopAccentBar(),
		NewInsetExact(headerInner, headerPadL, headerPadR, headerPadT, headerPadB),
		headerSep,
	)

	gotIt := newVideoDialogApplyButton(i18n.Current.WhatsNewGotIt, closeDialog)
	github := newWhatsNewGitHubLink()
	var footerInner fyne.CanvasObject
	if IsMobile() {
		footerInner = container.NewVBox(
			github,
			NewInsetExact(container.NewBorder(nil, nil, nil, gotIt), 0, 0, 8, 0),
		)
	} else {
		footerInner = container.NewBorder(nil, nil, github, gotIt)
	}
	footerBlock := container.NewVBox(footerSep, NewInsetExact(footerInner, footerPad, footerPad, 8, 10))

	form := container.NewBorder(headerBlock, footerBlock, nil, nil, NewInsetExact(body, bodyPad, bodyPad, 12, 8))
	bg := canvas.NewRectangle(design.ColorGray900)
	bg.CornerRadius = design.RadiusMD
	border := canvas.NewRectangle(color.Transparent)
	border.CornerRadius = design.RadiusMD
	border.StrokeColor = design.ColorBorder
	border.StrokeWidth = 1
	cornerBtn := container.New(&videoDialogCornerButtonLayout{Top: 12, Right: 12}, closeBtn)
	panel := container.NewStack(bg, NewInsetExact(form, 0, 0, 0, 3), cornerBtn, border)

	popup = ShowOverlayPopup(parent, OverlayPopupSpec{
		Panel:        panel,
		DimColor:     color.NRGBA{R: 0x00, G: 0x00, B: 0x00, A: 0x72},
		OnOutsideTap: closeDialog,
		PanelSize: func(canvasSize fyne.Size, panel fyne.CanvasObject) fyne.Size {
			if IsMobile() {
				side := float32(12)
				w := canvasSize.Width - side*2
				if w < 1 {
					w = canvasSize.Width
				}
				maxH := canvasSize.Height - side*2
				if maxH < 1 {
					maxH = canvasSize.Height
				}
				h := panel.MinSize().Height
				if h > maxH {
					h = maxH
				}
				return fyne.NewSize(w, h)
			}
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
			w := minFloat32(maxFloat32(panelMin.Width, dialogW), maxWidth)
			h := minFloat32(panelMin.Height, maxHeight)
			return fyne.NewSize(w, h)
		},
	})
}

func formatWhatsNewVersion(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	if !strings.HasPrefix(strings.ToLower(v), "v") {
		return "v" + v
	}
	return v
}

type whatsNewVersionChip struct {
	widget.BaseWidget
	version string
	label   *canvas.Text
	bg      *canvas.Rectangle
}

func newWhatsNewVersionChip() *whatsNewVersionChip {
	c := &whatsNewVersionChip{}
	c.ExtendBaseWidget(c)
	return c
}

func (c *whatsNewVersionChip) SetVersion(v string) {
	c.version = v
	if v == "" {
		c.Hide()
	} else {
		c.Show()
	}
	c.Refresh()
}

func (c *whatsNewVersionChip) MinSize() fyne.Size {
	t := canvas.NewText(c.version, design.ColorConnectionBadgeText)
	t.TextSize = 8
	t.TextStyle.Bold = true
	s := t.MinSize()
	return fyne.NewSize(s.Width+12, s.Height+8)
}

func (c *whatsNewVersionChip) CreateRenderer() fyne.WidgetRenderer {
	c.bg = canvas.NewRectangle(design.ColorGray950)
	c.bg.CornerRadius = whatsNewChipRadius
	c.bg.StrokeColor = design.ColorConnectionBadgeText
	c.bg.StrokeWidth = 1
	c.label = canvas.NewText(c.version, design.ColorConnectionBadgeText)
	c.label.TextSize = 8
	c.label.TextStyle.Bold = true
	c.label.Alignment = fyne.TextAlignCenter
	return &whatsNewVersionChipRenderer{chip: c, objects: []fyne.CanvasObject{c.bg, c.label}}
}

type whatsNewVersionChipRenderer struct {
	chip    *whatsNewVersionChip
	objects []fyne.CanvasObject
}

func (r *whatsNewVersionChipRenderer) Layout(size fyne.Size) {
	if r.chip.bg != nil {
		r.chip.bg.Resize(size)
		r.chip.bg.Move(fyne.NewPos(0, 0))
	}
	if r.chip.label == nil {
		return
	}
	ts := r.chip.label.MinSize()
	r.chip.label.Resize(ts)
	r.chip.label.Move(fyne.NewPos((size.Width-ts.Width)/2, (size.Height-ts.Height)/2))
}

func (r *whatsNewVersionChipRenderer) MinSize() fyne.Size { return r.chip.MinSize() }

func (r *whatsNewVersionChipRenderer) Refresh() {
	if r.chip.label != nil {
		r.chip.label.Text = r.chip.version
		r.chip.label.Color = design.ColorConnectionBadgeText
		r.chip.label.Refresh()
	}
	if r.chip.bg != nil {
		r.chip.bg.FillColor = design.ColorGray950
		r.chip.bg.StrokeColor = design.ColorConnectionBadgeText
		r.chip.bg.CornerRadius = whatsNewChipRadius
		r.chip.bg.Refresh()
	}
	r.Layout(r.chip.Size())
}

func (r *whatsNewVersionChipRenderer) Objects() []fyne.CanvasObject { return r.objects }
func (r *whatsNewVersionChipRenderer) Destroy()                     {}

func newWhatsNewCardView(card whatsNewCard, bodyW, scrollMax float32) fyne.CanvasObject {
	var rows []fyne.CanvasObject
	rowW := bodyW
	for _, item := range sortWhatsNewItems(card.Items) {
		for _, pt := range item.Points {
			rows = append(rows, newWhatsNewFeatureRow(item.Kind, pt, rowW))
		}
	}
	inner := container.New(&tightStatsVBoxLayout{Gap: 8}, rows...)
	return newWhatsNewOverflowBody(inner, bodyW, scrollMax)
}

func newWhatsNewFeatureRow(kind whatsNewKind, pt whatsNewPoint, rowW float32) fyne.CanvasObject {
	chrome := whatsNewKindChromeFor(kind)
	icon := newWhatsNewIconTile(whatsNewGlyphResource(pt.Glyph, chrome.Accent), chrome, whatsNewIconSize, whatsNewIconGlyph)
	title := NewBrandText(strings.TrimSpace(pt.Title.String()), 12, design.ColorTextLight, true)
	badge := newWhatsNewKindBadge(kind)
	textW := rowW - whatsNewRowPad*2 - whatsNewIconSize - whatsNewIconGap
	if textW < 80 {
		textW = 80
	}
	body := newVideoDialogWrapText(textW, 9, false, videoDialogWrapSpan{
		Text:  strings.TrimSpace(pt.Body.String()),
		Color: design.ColorConnectionsSectionSubtitle,
	})
	row := container.New(&whatsNewFeatureLayout{}, icon, title, badge, body)
	bg := canvas.NewRectangle(design.ColorConnectionBadgeFill)
	bg.CornerRadius = design.RadiusMD
	bg.StrokeColor = whatsNewCardStroke
	bg.StrokeWidth = 1
	return container.NewStack(bg, NewInsetExact(row, whatsNewRowPad, whatsNewRowPad, 10, 10))
}

type whatsNewFeatureLayout struct{}

func (l *whatsNewFeatureLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) < 4 {
		return
	}
	icon, title, badge, body := objects[0], objects[1], objects[2], objects[3]
	icon.Resize(fyne.NewSize(whatsNewIconSize, whatsNewIconSize))
	icon.Move(fyne.NewPos(0, 0))
	textX := whatsNewIconSize + whatsNewIconGap
	textW := size.Width - textX
	if textW < 0 {
		textW = 0
	}
	badgeSize := badge.MinSize()
	titleSize := title.MinSize()
	badge.Resize(badgeSize)
	badge.Move(fyne.NewPos(size.Width-badgeSize.Width, maxFloat32(0, (titleSize.Height-badgeSize.Height)/2)))
	titleW := textW - badgeSize.Width - 8
	if titleW < 0 {
		titleW = 0
	}
	title.Resize(fyne.NewSize(titleW, titleSize.Height))
	title.Move(fyne.NewPos(textX, 0))
	body.Resize(fyne.NewSize(textW, body.MinSize().Height))
	body.Move(fyne.NewPos(textX, titleSize.Height+4))
}

func (l *whatsNewFeatureLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	if len(objects) < 4 {
		return fyne.NewSize(whatsNewIconSize, whatsNewIconSize)
	}
	title := objects[1].MinSize()
	badge := objects[2].MinSize()
	body := objects[3].MinSize()
	titleH := maxFloat32(title.Height, badge.Height)
	textH := titleH + 4 + body.Height
	h := whatsNewIconSize
	if textH > h {
		h = textH
	}
	// Body is wrap-sized to the card; don't let an unwrapped title
	// push the panel wider than the phone canvas.
	w := whatsNewIconSize + whatsNewIconGap + maxFloat32(body.Width, 80)
	return fyne.NewSize(w, h)
}

func newWhatsNewKindBadge(kind whatsNewKind) fyne.CanvasObject {
	chrome := whatsNewKindChromeFor(kind)
	bg := canvas.NewRectangle(chrome.Fill)
	bg.CornerRadius = 3
	border := canvas.NewRectangle(color.Transparent)
	border.CornerRadius = 3
	border.StrokeColor = chrome.Stroke
	border.StrokeWidth = 1
	label := canvas.NewText(strings.ToUpper(whatsNewKindLabel(kind)), chrome.Accent)
	label.TextSize = 7
	label.TextStyle = fyne.TextStyle{Bold: true, Monospace: true}
	return container.NewStack(bg, border, NewInsetExact(label, 5, 5, 4, 4))
}

func newWhatsNewIconTile(icon fyne.Resource, chrome whatsNewKindChrome, tile, glyph float32) fyne.CanvasObject {
	bg := canvas.NewRectangle(chrome.Fill)
	bg.CornerRadius = 6
	border := canvas.NewRectangle(color.Transparent)
	border.CornerRadius = 6
	border.StrokeColor = chrome.Stroke
	border.StrokeWidth = 1
	img := canvas.NewImageFromResource(icon)
	img.FillMode = canvas.ImageFillContain
	img.SetMinSize(fyne.NewSize(glyph, glyph))
	lock := canvas.NewRectangle(color.Transparent)
	lock.SetMinSize(fyne.NewSize(tile, tile))
	return container.NewStack(lock, bg, border, container.NewCenter(img))
}

func whatsNewGlyphResource(glyph whatsNewGlyph, accent color.Color) fyne.Resource {
	hex := whatsNewHex(accent)
	switch glyph {
	case whatsNewGlyphAI:
		return whatsNewRecolor(assets.ScriptsTabIconMuted, "#c5c8b5", hex)
	case whatsNewGlyphUSB:
		return whatsNewRecolor(assets.USBTabIcon, "#F5F5F5", hex)
	case whatsNewGlyphColor:
		return whatsNewRecolor(assets.StarProIcon, "#9c58f9", hex)
	case whatsNewGlyphDisplay:
		return whatsNewRecolor(assets.MonitorTabIcon, "#F5F5F5", hex)
	case whatsNewGlyphCloud:
		return whatsNewRecolor(connectionSyncCloudIcon, "#c5c8b5", hex)
	case whatsNewGlyphMetrics:
		return whatsNewRecolor(whatsNewMetricsSVG, "#c5c8b5", hex)
	default:
		return whatsNewRecolor(assets.USBTabIcon, "#F5F5F5", hex)
	}
}

func whatsNewRecolor(src fyne.Resource, from, to string) fyne.Resource {
	if src == nil {
		return nil
	}
	name := src.Name() + "-" + strings.TrimPrefix(to, "#")
	return fyne.NewStaticResource(name, []byte(strings.ReplaceAll(string(src.Content()), from, to)))
}

func whatsNewKindLabel(kind whatsNewKind) string {
	switch kind {
	case whatsNewKindOpensource:
		return "Open Source"
	case whatsNewKindFree:
		return "Free"
	case whatsNewKindPro:
		return "Pro"
	case whatsNewKindEnterprise:
		return "Enterprise"
	case whatsNewKindHardwareAgent:
		return "Hardware Agent"
	case whatsNewKindBeta:
		return "Beta"
	default:
		return "Included"
	}
}

func whatsNewKindChromeFor(kind whatsNewKind) whatsNewKindChrome {
	switch kind {
	case whatsNewKindBeta:
		return whatsNewChromeBeta
	case whatsNewKindFree, whatsNewKindHardwareAgent:
		return whatsNewChromeFree
	case whatsNewKindPro, whatsNewKindEnterprise:
		return whatsNewChromePro
	default:
		return whatsNewChromeGray
	}
}

func whatsNewKindColor(kind whatsNewKind) color.Color {
	return whatsNewKindChromeFor(kind).Accent
}

func whatsNewNRGBA(c color.Color) color.NRGBA {
	if n, ok := c.(color.NRGBA); ok {
		return n
	}
	r, g, b, a := c.RGBA()
	return color.NRGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: uint8(a >> 8)}
}

func whatsNewHex(c color.Color) string {
	n := whatsNewNRGBA(c)
	return fmt.Sprintf("#%02x%02x%02x", n.R, n.G, n.B)
}

type whatsNewOverflowBody struct {
	widget.BaseWidget
	inner  fyne.CanvasObject
	minW   float32
	maxH   float32
	pad    *whatsNewRightPadLayout
	holder *fyne.Container
	scroll *container.Scroll
}

func newWhatsNewOverflowBody(inner fyne.CanvasObject, minW, maxH float32) fyne.CanvasObject {
	b := &whatsNewOverflowBody{inner: inner, minW: minW, maxH: maxH}
	b.ExtendBaseWidget(b)
	return b
}

func (b *whatsNewOverflowBody) MinSize() fyne.Size {
	m := fyne.NewSize(b.minW, 0)
	if b.inner != nil {
		im := b.inner.MinSize()
		if im.Width > m.Width {
			m.Width = im.Width
		}
		m.Height = im.Height
	}
	if m.Height > b.maxH {
		m.Height = b.maxH
	}
	return m
}

func (b *whatsNewOverflowBody) CreateRenderer() fyne.WidgetRenderer {
	b.pad = &whatsNewRightPadLayout{}
	b.holder = container.New(b.pad, b.inner)
	b.scroll = container.NewVScroll(b.holder)
	return &whatsNewOverflowBodyRenderer{b: b, objects: []fyne.CanvasObject{b.scroll}}
}

type whatsNewOverflowBodyRenderer struct {
	b       *whatsNewOverflowBody
	objects []fyne.CanvasObject
}

func (r *whatsNewOverflowBodyRenderer) Layout(size fyne.Size) {
	if r.b.inner == nil || r.b.scroll == nil || r.b.pad == nil {
		return
	}
	overflow := r.b.inner.MinSize().Height > size.Height+1
	next := float32(0)
	if overflow {
		next = whatsNewScrollGutter
	}
	if r.b.pad.Pad != next {
		r.b.pad.Pad = next
		r.b.holder.Refresh()
	}
	r.b.scroll.Move(fyne.NewPos(0, 0))
	r.b.scroll.Resize(size)
}

func (r *whatsNewOverflowBodyRenderer) MinSize() fyne.Size { return r.b.MinSize() }

func (r *whatsNewOverflowBodyRenderer) Refresh() {
	r.Layout(r.b.Size())
	canvas.Refresh(r.b)
}

func (r *whatsNewOverflowBodyRenderer) Objects() []fyne.CanvasObject { return r.objects }
func (r *whatsNewOverflowBodyRenderer) Destroy()                     {}

type whatsNewRightPadLayout struct {
	Pad float32
}

func (l *whatsNewRightPadLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) == 0 || objects[0] == nil {
		return
	}
	w := size.Width - l.Pad
	if w < 0 {
		w = 0
	}
	obj := objects[0]
	obj.Move(fyne.NewPos(0, 0))
	obj.Resize(fyne.NewSize(w, obj.MinSize().Height))
}

func (l *whatsNewRightPadLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	if len(objects) == 0 || objects[0] == nil {
		return fyne.NewSize(0, 0)
	}
	m := objects[0].MinSize()
	return fyne.NewSize(m.Width, m.Height)
}

type whatsNewGitHubLink struct {
	widget.BaseWidget
	hovered bool
	label   *canvas.Text
}

func newWhatsNewGitHubLink() *whatsNewGitHubLink {
	b := &whatsNewGitHubLink{}
	b.ExtendBaseWidget(b)
	return b
}

func (b *whatsNewGitHubLink) Tapped(*fyne.PointEvent) {
	u, err := url.Parse(whatsNewChangelogURL)
	if err != nil {
		return
	}
	if app := fyne.CurrentApp(); app != nil {
		_ = app.OpenURL(u)
	}
}

func (b *whatsNewGitHubLink) TappedSecondary(*fyne.PointEvent) {}

func (b *whatsNewGitHubLink) MouseIn(*desktop.MouseEvent) {
	b.hovered = true
	b.Refresh()
}

func (b *whatsNewGitHubLink) MouseOut() {
	b.hovered = false
	b.Refresh()
}

func (b *whatsNewGitHubLink) MouseMoved(*desktop.MouseEvent) {}

func (b *whatsNewGitHubLink) Cursor() desktop.Cursor { return desktop.PointerCursor }

func (b *whatsNewGitHubLink) linkText() string {
	label := "GitHub"
	if i18n.Current != nil && strings.TrimSpace(i18n.Current.WhatsNewGitHub) != "" {
		label = i18n.Current.WhatsNewGitHub
	}
	return label + " ↗"
}

func (b *whatsNewGitHubLink) textSize() float32 {
	if IsMobile() {
		return 9
	}
	return 10
}

func (b *whatsNewGitHubLink) MinSize() fyne.Size {
	t := canvas.NewText(b.linkText(), design.ColorConnectionsSectionSubtitle)
	t.TextSize = b.textSize()
	s := t.MinSize()
	h := s.Height + 4
	if IsMobile() {
		return fyne.NewSize(1, h)
	}
	return fyne.NewSize(s.Width, h)
}

func (b *whatsNewGitHubLink) CreateRenderer() fyne.WidgetRenderer {
	b.label = canvas.NewText(b.linkText(), design.ColorConnectionsSectionSubtitle)
	b.label.TextSize = b.textSize()
	return &whatsNewGitHubLinkRenderer{btn: b, objects: []fyne.CanvasObject{b.label}}
}

type whatsNewGitHubLinkRenderer struct {
	btn     *whatsNewGitHubLink
	objects []fyne.CanvasObject
}

func (r *whatsNewGitHubLinkRenderer) Layout(size fyne.Size) {
	if r.btn.label == nil {
		return
	}
	ts := r.btn.label.MinSize()
	w := ts.Width
	if size.Width > 0 && w > size.Width {
		w = size.Width
	}
	r.btn.label.Resize(fyne.NewSize(w, ts.Height))
	r.btn.label.Move(fyne.NewPos(0, (size.Height-ts.Height)/2))
}

func (r *whatsNewGitHubLinkRenderer) MinSize() fyne.Size { return r.btn.MinSize() }

func (r *whatsNewGitHubLinkRenderer) Refresh() {
	if r.btn.label == nil {
		return
	}
	r.btn.label.Text = r.btn.linkText()
	r.btn.label.TextSize = r.btn.textSize()
	if r.btn.hovered {
		r.btn.label.Color = design.ColorTextLight
	} else {
		r.btn.label.Color = design.ColorConnectionsSectionSubtitle
	}
	r.btn.label.Refresh()
	r.Layout(r.btn.Size())
}

func (r *whatsNewGitHubLinkRenderer) Objects() []fyne.CanvasObject { return r.objects }
func (r *whatsNewGitHubLinkRenderer) Destroy()                     {}

type whatsNewNav struct {
	widget.BaseWidget
	label   string
	hovered bool
	onTap   func()
	text    *canvas.Text
}

func newWhatsNewNav(label string, onTap func()) *whatsNewNav {
	b := &whatsNewNav{label: label, onTap: onTap}
	b.ExtendBaseWidget(b)
	return b
}

func (b *whatsNewNav) Tapped(*fyne.PointEvent) {
	if b.onTap != nil {
		b.onTap()
	}
}

func (b *whatsNewNav) TappedSecondary(*fyne.PointEvent) {}

func (b *whatsNewNav) MouseIn(*desktop.MouseEvent) {
	b.hovered = true
	b.Refresh()
}

func (b *whatsNewNav) MouseOut() {
	b.hovered = false
	b.Refresh()
}

func (b *whatsNewNav) MouseMoved(*desktop.MouseEvent) {}

func (b *whatsNewNav) Cursor() desktop.Cursor { return desktop.PointerCursor }

func (b *whatsNewNav) MinSize() fyne.Size {
	t := canvas.NewText(b.label, design.ColorConnectionsSectionSubtitle)
	t.TextSize = 16
	s := t.MinSize()
	return fyne.NewSize(s.Width+8, s.Height)
}

func (b *whatsNewNav) CreateRenderer() fyne.WidgetRenderer {
	b.text = canvas.NewText(b.label, design.ColorConnectionsSectionSubtitle)
	b.text.TextSize = 16
	return &whatsNewNavRenderer{btn: b, objects: []fyne.CanvasObject{b.text}}
}

type whatsNewNavRenderer struct {
	btn     *whatsNewNav
	objects []fyne.CanvasObject
}

func (r *whatsNewNavRenderer) Layout(size fyne.Size) {
	if r.btn.text == nil {
		return
	}
	ts := r.btn.text.MinSize()
	r.btn.text.Resize(ts)
	r.btn.text.Move(fyne.NewPos((size.Width-ts.Width)/2, (size.Height-ts.Height)/2))
}

func (r *whatsNewNavRenderer) MinSize() fyne.Size { return r.btn.MinSize() }

func (r *whatsNewNavRenderer) Refresh() {
	if r.btn.text == nil {
		return
	}
	r.btn.text.Text = r.btn.label
	if r.btn.hovered {
		r.btn.text.Color = design.ColorTextLight
	} else {
		r.btn.text.Color = design.ColorConnectionsSectionSubtitle
	}
	r.btn.text.Refresh()
	r.Layout(r.btn.Size())
}

func (r *whatsNewNavRenderer) Objects() []fyne.CanvasObject { return r.objects }
func (r *whatsNewNavRenderer) Destroy()                     {}
