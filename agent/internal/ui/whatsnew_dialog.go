package ui

import (
	"image/color"
	"strings"
	"unicode"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"

	"usbridge_agent/internal/ui/design"
)

const (
	whatsNewDialogWidth  float32 = 360
	whatsNewBodyWidth    float32 = 320
	whatsNewScrollH      float32 = 260
	whatsNewScrollGutter float32 = 14
	whatsNewChipRadius   float32 = 3
)

// showWhatsNewDialog is the post-update appeal: one branded card at a
// time, paged through whatsNewCatalog. Opened from the footer version.
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
		version.SetVersion(formatWhatsNewVersion(cards[idx].Version))
		body.Objects = []fyne.CanvasObject{newWhatsNewCardView(cards[idx])}
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

	gotIt := newDialogCTA(loc().WhatsNewGotIt, closeDialog)
	footer := container.New(&whatsNewFooterLayout{}, older, gotIt, newer)
	panel := newBrandedDialogPanelChromeExtra(loc().WhatsNewTitle, "", version, whatsNewDialogWidth, 20, 10, 4, 7, body, footer, closeDialog)
	popup = showOverlayPopup(parent, overlayPopupSpec{
		Panel:        panel,
		OnOutsideTap: closeDialog,
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
	t := canvas.NewText(c.version, design.ColorTeal)
	t.TextSize = 8
	t.TextStyle.Bold = true
	s := t.MinSize()
	return fyne.NewSize(s.Width+12, s.Height+8)
}

func (c *whatsNewVersionChip) CreateRenderer() fyne.WidgetRenderer {
	c.bg = canvas.NewRectangle(design.ColorGray950)
	c.bg.CornerRadius = whatsNewChipRadius
	c.bg.StrokeColor = design.ColorTeal
	c.bg.StrokeWidth = 1
	c.label = canvas.NewText(c.version, design.ColorTeal)
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
		r.chip.label.Color = design.ColorTeal
		r.chip.label.Refresh()
	}
	if r.chip.bg != nil {
		r.chip.bg.FillColor = design.ColorGray950
		r.chip.bg.StrokeColor = design.ColorTeal
		r.chip.bg.CornerRadius = whatsNewChipRadius
		r.chip.bg.Refresh()
	}
	r.Layout(r.chip.Size())
}

func (r *whatsNewVersionChipRenderer) Objects() []fyne.CanvasObject { return r.objects }
func (r *whatsNewVersionChipRenderer) Destroy()                     {}

func newWhatsNewCardView(card whatsNewCard) fyne.CanvasObject {
	var rows []fyne.CanvasObject
	for _, item := range sortWhatsNewItems(card.Items) {
		rows = append(rows, newWhatsNewItemView(item))
	}
	inner := container.New(&tightVBoxLayout{gap: 10}, rows...)
	padded := newExactInset(inner, 0, whatsNewScrollGutter, 0, 0)
	scroll := container.NewVScroll(padded)
	lock := canvas.NewRectangle(color.Transparent)
	lock.SetMinSize(fyne.NewSize(whatsNewBodyWidth, whatsNewScrollH))
	return container.NewStack(lock, scroll)
}

func newWhatsNewItemView(item whatsNewItem) fyne.CanvasObject {
	if len(item.Points) == 0 {
		return canvas.NewRectangle(color.Transparent)
	}
	kind := item.Kind
	stroke := whatsNewKindColor(kind)
	badgeLbl := whatsNewKindLabel(kind)
	badge := canvas.NewText(badgeLbl, stroke)
	badge.TextSize = 8
	badge.TextStyle.Bold = true
	badgeW := fyne.MeasureText(badgeLbl, 8, fyne.TextStyle{Bold: true}).Width

	innerW := whatsNewBodyWidth - 24 - whatsNewScrollGutter
	titleW := innerW - badgeW - 10
	if titleW < 80 {
		titleW = 80
	}

	var blocks []fyne.CanvasObject
	for i, pt := range item.Points {
		title := strings.TrimSpace(pt.Title.String())
		body := strings.TrimSpace(pt.Body.String())
		tw := innerW
		if i == 0 {
			tw = titleW
		}
		titleLbl := whatsNewText(title, 12, design.ColorTextLight, fyne.TextStyle{Bold: true}, tw)
		bodyLbl := whatsNewText(body, 9, design.ColorMutedOlive, fyne.TextStyle{}, innerW)
		var header fyne.CanvasObject = titleLbl
		if i == 0 {
			header = container.NewBorder(nil, nil, nil, newExactInset(badge, 8, 0, 3, 0), titleLbl)
		}
		blocks = append(blocks, container.New(&tightVBoxLayout{gap: 2}, header, bodyLbl))
	}
	copyCol := container.New(&tightVBoxLayout{gap: 10}, blocks...)
	return wrapWhatsNewPlaque(copyCol, kind)
}

func wrapWhatsNewPlaque(inner fyne.CanvasObject, kind whatsNewKind) fyne.CanvasObject {
	bg := canvas.NewRectangle(design.ColorGray950)
	bg.CornerRadius = 8
	bg.StrokeColor = whatsNewKindColor(kind)
	bg.StrokeWidth = 1
	return container.NewStack(bg, newExactInset(inner, 12, 12, 10, 10))
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
		return "Other"
	}
}

func whatsNewKindColor(kind whatsNewKind) color.Color {
	switch kind {
	case whatsNewKindOpensource:
		return design.ColorWhite
	case whatsNewKindFree:
		return design.ColorTeal
	case whatsNewKindPro, whatsNewKindEnterprise:
		return design.ColorProSoft
	case whatsNewKindBeta:
		return design.ColorAlert
	default:
		return design.ColorMutedOlive
	}
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

type whatsNewFooterLayout struct{}

func (l *whatsNewFooterLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) < 3 {
		return
	}
	place := func(o fyne.CanvasObject, x float32) {
		if o == nil || !o.Visible() {
			return
		}
		m := o.MinSize()
		h := m.Height
		if h > size.Height {
			h = size.Height
		}
		w := m.Width
		if w > size.Width {
			w = size.Width
		}
		o.Resize(fyne.NewSize(w, h))
		y := (size.Height - h) / 2
		if y < 0 {
			y = 0
		}
		o.Move(fyne.NewPos(x, y))
	}
	left, center, right := objects[0], objects[1], objects[2]
	if left != nil && left.Visible() {
		place(left, 0)
	}
	if right != nil && right.Visible() {
		place(right, size.Width-right.MinSize().Width)
	}
	if center != nil && center.Visible() {
		place(center, (size.Width-center.MinSize().Width)/2)
	}
}

func (l *whatsNewFooterLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	var w, h float32
	for _, o := range objects {
		if o == nil || !o.Visible() {
			continue
		}
		m := o.MinSize()
		w += m.Width
		if m.Height > h {
			h = m.Height
		}
	}
	return fyne.NewSize(w, h)
}

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
	t := canvas.NewText(b.label, design.ColorMutedOlive)
	t.TextSize = 16
	s := t.MinSize()
	return fyne.NewSize(s.Width+8, s.Height)
}

func (b *whatsNewNav) CreateRenderer() fyne.WidgetRenderer {
	b.text = canvas.NewText(b.label, design.ColorMutedOlive)
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
		r.btn.text.Color = design.ColorMutedOlive
	}
	r.btn.text.Refresh()
	r.Layout(r.btn.Size())
}

func (r *whatsNewNavRenderer) Objects() []fyne.CanvasObject { return r.objects }
func (r *whatsNewNavRenderer) Destroy()                     {}
