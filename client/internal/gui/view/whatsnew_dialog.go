package view

import (
	"image/color"
	"strings"

	"usbridge-client/internal/gui/design"
	"usbridge-client/internal/gui/i18n"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)

const (
	whatsNewDialogWidth  float32 = 360
	whatsNewBodyWidth    float32 = 320
	whatsNewScrollH      float32 = 260
	whatsNewScrollGutter float32 = 14
	whatsNewChipRadius   float32 = 3
)

var whatsNewHost fyne.Window

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

// ShowWhatsNewDialog is the post-update appeal: one branded card at a
// time, paged through whatsNewCatalog. Opened from the footer version.
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

	title := NewBrandText(i18n.Current.WhatsNewTitle, 13, design.ColorTextLight, true)
	titleRow := container.New(&DeviceRowControlsLayout{Gap: 8}, title, version)
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
		NewInsetExact(titleRow, 21, 44, 12, 17),
		headerSep,
	)

	gotIt := newVideoDialogApplyButton(i18n.Current.WhatsNewGotIt, closeDialog)
	footerInner := container.New(&whatsNewFooterLayout{}, older, gotIt, newer)
	footerBlock := container.NewVBox(footerSep, NewInsetExact(footerInner, 20, 20, 4, 4))

	form := container.NewBorder(headerBlock, footerBlock, nil, nil, NewInsetExact(body, 20, 20, 10, 8))
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
			w := minFloat32(maxFloat32(panelMin.Width, whatsNewDialogWidth), maxWidth)
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

func newWhatsNewCardView(card whatsNewCard) fyne.CanvasObject {
	var rows []fyne.CanvasObject
	for _, item := range sortWhatsNewItems(card.Items) {
		rows = append(rows, newWhatsNewItemView(item))
	}
	inner := container.New(&tightStatsVBoxLayout{Gap: 10}, rows...)
	padded := NewInsetExact(inner, 0, whatsNewScrollGutter, 0, 0)
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

	innerW := whatsNewBodyWidth - 24 - whatsNewScrollGutter

	var blocks []fyne.CanvasObject
	for i, pt := range item.Points {
		title := strings.TrimSpace(pt.Title.String())
		body := strings.TrimSpace(pt.Body.String())
		titleLbl := NewBrandText(title, 12, design.ColorTextLight, true)
		bodyLbl := newVideoDialogWrapText(innerW, 9, false, videoDialogWrapSpan{Text: body, Color: design.ColorConnectionsSectionSubtitle})
		var header fyne.CanvasObject = titleLbl
		if i == 0 {
			header = container.NewBorder(nil, nil, nil, NewInsetExact(badge, 8, 0, 3, 0), titleLbl)
		}
		blocks = append(blocks, container.New(&tightStatsVBoxLayout{Gap: 2}, header, bodyLbl))
	}
	copyCol := container.New(&tightStatsVBoxLayout{Gap: 10}, blocks...)
	return wrapWhatsNewPlaque(copyCol, kind)
}

func wrapWhatsNewPlaque(inner fyne.CanvasObject, kind whatsNewKind) fyne.CanvasObject {
	bg := canvas.NewRectangle(design.ColorGray950)
	bg.CornerRadius = 8
	bg.StrokeColor = whatsNewKindColor(kind)
	bg.StrokeWidth = 1
	return container.NewStack(bg, NewInsetExact(inner, 12, 12, 10, 10))
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
		return "Other"
	}
}

func whatsNewKindColor(kind whatsNewKind) color.Color {
	switch kind {
	case whatsNewKindOpensource:
		return design.ColorWhite
	case whatsNewKindFree:
		return design.ColorConnectionBadgeText
	case whatsNewKindPro, whatsNewKindEnterprise:
		return design.ColorPro
	case whatsNewKindHardwareAgent:
		return design.ColorAccent
	case whatsNewKindBeta:
		return design.ColorAlert
	default:
		return design.ColorConnectionsSectionSubtitle
	}
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
