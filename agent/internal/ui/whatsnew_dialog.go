package ui

import (
	"fmt"
	"image/color"
	"net/url"
	"strings"
	"unicode"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"

	"usbridge_agent/assets"
	"usbridge_agent/internal/ui/design"
)

const (
	whatsNewDialogWidth  float32 = 520
	whatsNewBodyWidth    float32 = 480
	whatsNewScrollMax    float32 = 400
	whatsNewScrollGutter float32 = 14
	whatsNewChipRadius   float32 = 3
	whatsNewIconSize     float32 = 24
	whatsNewIconGlyph    float32 = 13
	whatsNewIconGap      float32 = 10
	whatsNewRowPad       float32 = 12
	whatsNewChangelogURL         = "https://github.com/USBridge-Technologies/USBridge-Remote/releases"
)

var whatsNewCardStroke = color.NRGBA{R: 0x3f, G: 0x3f, B: 0x3f, A: 0xff}

// Slightly above ColorGray900 so feature rows lift off the dialog fill.
var whatsNewCardFill = color.NRGBA{R: 0x22, G: 0x26, B: 0x29, A: 0xff}

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

var whatsNewMetricsSVG = fyne.NewStaticResource("whatsnew_metrics.svg", []byte(
	`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="#c5c8b5" stroke-width="1.8" stroke-linecap="round"><path d="M5 19V11M10 19V6M15 19v-8M20 19V8"/></svg>`))
var whatsNewDisplaySVG = fyne.NewStaticResource("whatsnew_display.svg", []byte(
	`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="#c5c8b5" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="4" width="18" height="12" rx="2"/><path d="M8 20h8M12 16v4"/></svg>`))
var whatsNewProtocolSVG = fyne.NewStaticResource("whatsnew_protocol.svg", []byte(
	`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="#c5c8b5" stroke-width="1.8" stroke-linejoin="round"><path d="M12 3 3 8l9 5 9-5-9-5z"/><path d="M3 12l9 5 9-5"/><path d="M3 16l9 5 9-5"/></svg>`))

func showWhatsNewDialog(parent fyne.Window) {
	if parent == nil {
		return
	}
	cards := whatsNewCatalog()
	if len(cards) == 0 {
		return
	}

	idx := 0
	body := container.NewStack()
	version := newWhatsNewVersionChip()
	dateLbl := canvas.NewText("", design.ColorEmptyHint)
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
		body.Objects = []fyne.CanvasObject{newWhatsNewCardView(card)}
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

	titleExtra := container.New(&tightHBoxLayout{gap: 8}, version, dateLbl, older, newer)
	gotIt := newDialogCTA(loc().WhatsNewGotIt, closeDialog)
	github := newWhatsNewGitHubLink()
	var footer fyne.CanvasObject = container.NewBorder(nil, nil, github, gotIt)
	if updatesBackend != nil {
		channelLbl := canvas.NewText(loc().UpdatesChannelLabel, design.ColorEmptyHint)
		channelLbl.TextSize = 10
		picker := newChannelPicker(updatesBackend, nil)
		checkBtn := newIconActionButton(loc().UpdatesLink, nil, func() {
			closeDialog()
			showUpdatesDialog(parent)
		})
		checkBtn.Compact = true
		left := container.New(&tightHBoxLayout{gap: 8}, channelLbl, picker)
		footer = container.NewVBox(container.NewBorder(nil, nil, left, checkBtn), footer)
	}
	panel := newBrandedDialogPanelChromeExtra(loc().WhatsNewTitle, loc().WhatsNewSubtitle, titleExtra, whatsNewDialogWidth, 20, 8, 8, 10, body, footer, closeDialog)
	popup = showOverlayPopup(parent, overlayPopupSpec{
		Panel:        panel,
		OnOutsideTap: closeDialog,
		PanelSize: func(canvasSize fyne.Size, p fyne.CanvasObject) fyne.Size {
			margin := float32(24)
			maxW := canvasSize.Width - margin*2
			maxH := canvasSize.Height - margin*2
			if maxW < 1 {
				maxW = canvasSize.Width
			}
			if maxH < 1 {
				maxH = canvasSize.Height
			}
			min := p.MinSize()
			w := min.Width
			if w < whatsNewDialogWidth {
				w = whatsNewDialogWidth
			}
			if w > maxW {
				w = maxW
			}
			h := min.Height
			if h > maxH {
				h = maxH
			}
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
	t := canvas.NewText(c.version, design.ColorMutedOlive)
	t.TextSize = 8
	t.TextStyle.Bold = true
	s := t.MinSize()
	return fyne.NewSize(s.Width+12, s.Height+8)
}

func (c *whatsNewVersionChip) CreateRenderer() fyne.WidgetRenderer {
	c.bg = canvas.NewRectangle(design.ColorGray950)
	c.bg.CornerRadius = whatsNewChipRadius
	c.bg.StrokeColor = design.ColorMutedOlive
	c.bg.StrokeWidth = 1
	c.label = canvas.NewText(c.version, design.ColorMutedOlive)
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
		r.chip.label.Color = design.ColorMutedOlive
		r.chip.label.Refresh()
	}
	if r.chip.bg != nil {
		r.chip.bg.FillColor = design.ColorGray950
		r.chip.bg.StrokeColor = design.ColorMutedOlive
		r.chip.bg.CornerRadius = whatsNewChipRadius
		r.chip.bg.Refresh()
	}
	r.Layout(r.chip.Size())
}

func (r *whatsNewVersionChipRenderer) Objects() []fyne.CanvasObject { return r.objects }
func (r *whatsNewVersionChipRenderer) Destroy()                     {}

func newWhatsNewCardView(card whatsNewCard) fyne.CanvasObject {
	var rows []fyne.CanvasObject
	rowW := whatsNewBodyWidth - whatsNewScrollGutter
	for _, item := range sortWhatsNewItems(card.Items) {
		for _, pt := range item.Points {
			rows = append(rows, newWhatsNewFeatureRow(item.Kind, pt, rowW))
		}
	}
	inner := container.New(&tightVBoxLayout{gap: 8}, rows...)
	return newWhatsNewOverflowBody(inner, whatsNewBodyWidth, whatsNewScrollMax)
}

func newWhatsNewFeatureRow(kind whatsNewKind, pt whatsNewPoint, rowW float32) fyne.CanvasObject {
	chrome := whatsNewKindChromeFor(kind)
	icon := newWhatsNewIconTile(whatsNewGlyphResource(pt.Glyph, chrome.Accent), chrome)
	title := canvas.NewText(strings.TrimSpace(pt.Title.String()), design.ColorTextLight)
	title.TextSize = 12
	title.TextStyle.Bold = true
	badge := newWhatsNewKindBadge(kind)
	textW := rowW - whatsNewRowPad*2 - whatsNewIconSize - whatsNewIconGap
	if textW < 80 {
		textW = 80
	}
	body := whatsNewText(strings.TrimSpace(pt.Body.String()), 9, design.ColorEmptyHint, fyne.TextStyle{}, textW)
	row := container.New(&whatsNewFeatureLayout{}, icon, title, badge, body)
	bg := canvas.NewRectangle(whatsNewCardFill)
	bg.CornerRadius = design.RadiusMD
	bg.StrokeColor = whatsNewCardStroke
	bg.StrokeWidth = 1
	return container.NewStack(bg, newExactInset(row, whatsNewRowPad, whatsNewRowPad, 10, 10))
}

type whatsNewFeatureLayout struct{}

func (l *whatsNewFeatureLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) < 4 {
		return
	}
	icon, title, badge, body := objects[0], objects[1], objects[2], objects[3]
	icon.Resize(fyne.NewSize(whatsNewIconSize, whatsNewIconSize))
	iconY := (size.Height - whatsNewIconSize) / 2
	if iconY < 0 {
		iconY = 0
	}
	icon.Move(fyne.NewPos(0, iconY))
	textX := whatsNewIconSize + whatsNewIconGap
	textW := size.Width - textX
	if textW < 0 {
		textW = 0
	}
	badgeSize := badge.MinSize()
	titleSize := title.MinSize()
	badge.Resize(badgeSize)
	y := (titleSize.Height - badgeSize.Height) / 2
	if y < 0 {
		y = 0
	}
	badge.Move(fyne.NewPos(size.Width-badgeSize.Width, y))
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
	titleH := title.Height
	if badge.Height > titleH {
		titleH = badge.Height
	}
	textH := titleH + 4 + body.Height
	h := whatsNewIconSize
	if textH > h {
		h = textH
	}
	w := whatsNewIconSize + whatsNewIconGap + body.Width
	if w < 80 {
		w = 80
	}
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
	return container.NewStack(bg, border, newExactInset(label, 5, 5, 4, 4))
}

func newWhatsNewIconTile(icon fyne.Resource, chrome whatsNewKindChrome) fyne.CanvasObject {
	bg := canvas.NewRectangle(chrome.Fill)
	bg.CornerRadius = 6
	border := canvas.NewRectangle(color.Transparent)
	border.CornerRadius = 6
	border.StrokeColor = chrome.Stroke
	border.StrokeWidth = 1
	img := canvas.NewImageFromResource(icon)
	img.FillMode = canvas.ImageFillContain
	img.SetMinSize(fyne.NewSize(whatsNewIconGlyph, whatsNewIconGlyph))
	lock := canvas.NewRectangle(color.Transparent)
	lock.SetMinSize(fyne.NewSize(whatsNewIconSize, whatsNewIconSize))
	return container.NewStack(lock, bg, border, container.NewCenter(img))
}

func whatsNewGlyphResource(glyph whatsNewGlyph, accent color.Color) fyne.Resource {
	hex := whatsNewHex(accent)
	switch glyph {
	case whatsNewGlyphUSB:
		return whatsNewRecolor(assets.USBGlyphIcon, "#c3c6b4", hex)
	case whatsNewGlyphProtocol:
		return whatsNewRecolor(whatsNewProtocolSVG, "#c5c8b5", hex)
	case whatsNewGlyphDisplay:
		return whatsNewRecolor(whatsNewDisplaySVG, "#c5c8b5", hex)
	case whatsNewGlyphCloud:
		return whatsNewRecolor(assets.CloudUploadIcon, "#c3c6b4", hex)
	case whatsNewGlyphMetrics:
		return whatsNewRecolor(whatsNewMetricsSVG, "#c5c8b5", hex)
	default:
		return whatsNewRecolor(whatsNewMetricsSVG, "#c5c8b5", hex)
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
	case whatsNewKindFree:
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

func whatsNewHex(c color.Color) string {
	n, ok := c.(color.NRGBA)
	if !ok {
		r, g, b, _ := c.RGBA()
		n = color.NRGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: 0xff}
	}
	return fmt.Sprintf("#%02x%02x%02x", n.R, n.G, n.B)
}

func whatsNewText(msg string, size float32, col color.Color, style fyne.TextStyle, wrapW float32) fyne.CanvasObject {
	lbl := widget.NewLabel(msg)
	lbl.Wrapping = fyne.TextWrapWord
	lbl.Alignment = fyne.TextAlignLeading
	lbl.TextStyle = style
	h := whatsNewWrapHeight(msg, size, style, wrapW)
	return container.New(&accountLicenseHintLayout{height: h}, wrapDialogLabel(lbl, size, col))
}

func whatsNewWrapHeight(text string, size float32, style fyne.TextStyle, maxWidth float32) float32 {
	lineH := fyne.MeasureText("Ag", size, style).Height
	if lineH < 1 {
		lineH = size + 4
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return lineH
	}
	spaceW := fyne.MeasureText(" ", size, style).Width
	lines := 0
	for _, para := range strings.Split(text, "\n") {
		para = strings.TrimSpace(para)
		if para == "" {
			lines++
			continue
		}
		var lineW float32
		paraLines := 1
		start := 0
		for i, r := range para + " " {
			if !unicode.IsSpace(r) {
				continue
			}
			word := strings.TrimSpace(para[start:i])
			start = i + 1
			if word == "" {
				continue
			}
			ww := fyne.MeasureText(word, size, style).Width
			if lineW == 0 {
				lineW = ww
				continue
			}
			if maxWidth > 0 && lineW+spaceW+ww > maxWidth {
				paraLines++
				lineW = ww
				continue
			}
			lineW += spaceW + ww
		}
		lines += paraLines
	}
	if lines < 1 {
		lines = 1
	}
	return lineH*float32(lines) + 2
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
	h := float32(0)
	if b.inner != nil {
		h = b.inner.MinSize().Height
	}
	if h > b.maxH {
		h = b.maxH
	}
	return fyne.NewSize(b.minW, h)
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
	return objects[0].MinSize()
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
	label := loc().WhatsNewGitHub
	if strings.TrimSpace(label) == "" {
		label = "GitHub"
	}
	return label + " ↗"
}

func (b *whatsNewGitHubLink) MinSize() fyne.Size {
	t := canvas.NewText(b.linkText(), design.ColorEmptyHint)
	t.TextSize = 10
	s := t.MinSize()
	return fyne.NewSize(s.Width, s.Height+4)
}

func (b *whatsNewGitHubLink) CreateRenderer() fyne.WidgetRenderer {
	b.label = canvas.NewText(b.linkText(), design.ColorEmptyHint)
	b.label.TextSize = 10
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
	if r.btn.hovered {
		r.btn.label.Color = design.ColorTextLight
	} else {
		r.btn.label.Color = design.ColorEmptyHint
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
	t := canvas.NewText(b.label, design.ColorEmptyHint)
	t.TextSize = 16
	s := t.MinSize()
	return fyne.NewSize(s.Width+8, s.Height)
}

func (b *whatsNewNav) CreateRenderer() fyne.WidgetRenderer {
	b.text = canvas.NewText(b.label, design.ColorEmptyHint)
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
		r.btn.text.Color = design.ColorEmptyHint
	}
	r.btn.text.Refresh()
	r.Layout(r.btn.Size())
}

func (r *whatsNewNavRenderer) Objects() []fyne.CanvasObject { return r.objects }
func (r *whatsNewNavRenderer) Destroy()                     {}
