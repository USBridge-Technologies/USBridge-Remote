package ui

import (
	"image/color"
	"net/url"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"

	"usbridge_agent/assets"
	"usbridge_agent/internal/ui/design"
)

const (
	tariffDialogWidth     float32 = 500
	tariffDialogHeight    float32 = 420
	tariffFooterH         float32 = 32
	tariffGitHubURL               = "https://github.com/USBridge-Technologies/USBridge-Remote/"
	tariffPricePro                = "$8"
	tariffPriceEnterprise         = "$25"
	tariffSubtitle                = "Upgrade your USBridge agent for low-latency streaming, passthrough & mesh networks"
)

// Slightly above ColorGray900 so feature cards lift off the dialog fill.
var tariffCardFill = color.NRGBA{R: 0x22, G: 0x26, B: 0x29, A: 0xFF}

type tariffPlan struct {
	key      string
	tab      string
	buyLabel string
	price    string
	paid     bool
	features []tariffFeature
}

type tariffFeature struct {
	title    string
	subtitle string
}

var tariffPlans = []tariffPlan{
	{
		key: protocolOpensource,
		tab: "Sunshine",
		features: []tariffFeature{
			{"Ultra-low latency streaming", "Near-zero delay for mouse and video"},
			{"Shared clipboard", "Copy text, images, and files both ways"},
			{"Multi-monitor support", "Switch which host display you view"},
		},
	},
	{
		key: protocolFree,
		tab: "Free",
		features: []tariffFeature{
			{"Browser web client", "Connect from any modern browser"},
			{"Windows pre-login access", "Reach the host before anyone logs in"},
			{"Fast connect", "A session starts in seconds"},
			{"Virtual displays", "Extra screens without extra hardware"},
		},
	},
	{
		key:      protocolPro,
		tab:      "Pro",
		buyLabel: "Buy Pro",
		price:    tariffPricePro,
		paid:     true,
		features: []tariffFeature{
			{"4:4:4 color fidelity", "Full chroma for text and color-critical work"},
			{"USB device emulation", "Pass local USB devices through to the host"},
			{"Wacom tablet support", "Pen pressure and tilt pass through to the host"},
		},
	},
	{
		key:      protocolEnterprise,
		tab:      "Enterprise",
		buyLabel: "Buy Enterprise",
		price:    tariffPriceEnterprise,
		paid:     true,
		features: []tariffFeature{
			{"Session recording and audit logs", "Keep a record of every remote session"},
			{"Built for company-wide rollout", "Access and policy at company scale"},
		},
	},
}

func tariffIndexForKey(key string) int {
	for i, p := range tariffPlans {
		if p.key == key {
			return i
		}
	}
	return 0
}

func (w *Window) tariffDialogVersion() string {
	if w.token == nil {
		return formatStreamerVersion(appVersion, "", false)
	}
	st := w.token.EntitlementStatus()
	return formatStreamerVersion(appVersion, st.RustShineVersion, st.ActiveBackend == "rustshine")
}

// showTariffPickerDialog is an info overlay: segmented plan switcher,
// two-column feature cards, and a footer with price on the left / Buy on
// the right. Tabs only change the copy — they do not switch the streamer.
func (w *Window) showTariffPickerDialog(parent fyne.Window, initialKey string) {
	if parent == nil {
		return
	}

	var popup *widget.PopUp
	closeDialog := func() {
		if popup != nil {
			popup.Hide()
		}
	}

	selected := tariffIndexForKey(initialKey)
	if initialKey == "" && w.token != nil {
		selected = tariffIndexForKey(protocolKeyFromStatus(w.token.EntitlementStatus()))
	}
	tabs := make([]*tariffTabButton, len(tariffPlans))
	tabItems := make([]fyne.CanvasObject, len(tariffPlans))
	featureScroll := container.NewVScroll(container.NewVBox())
	footerInner := container.NewMax()
	footerLock := canvas.NewRectangle(color.Transparent)
	footerLock.SetMinSize(fyne.NewSize(1, tariffFooterH))
	footerSlot := container.NewStack(footerLock, footerInner)

	var refresh func()
	refresh = func() {
		if selected < 0 || selected >= len(tariffPlans) {
			selected = 0
		}
		for i, tab := range tabs {
			if tab != nil {
				tab.SetSelected(i == selected)
			}
		}
		plan := tariffPlans[selected]
		featureScroll.Content = newTariffFeaturePane(plan)
		featureScroll.Offset = fyne.NewPos(0, 0)
		featureScroll.Refresh()
		footerInner.Objects = []fyne.CanvasObject{w.newTariffFooter(parent, plan)}
		footerInner.Refresh()
	}

	for i, plan := range tariffPlans {
		idx := i
		tab := newTariffTabButton(plan.tab, plan.key, func() {
			if selected == idx {
				return
			}
			selected = idx
			refresh()
		})
		tabs[i] = tab
		tabItems[i] = tab
	}
	track := canvas.NewRectangle(design.ColorGray950)
	track.CornerRadius = 8
	track.StrokeColor = design.ColorChromeOlive
	track.StrokeWidth = 1
	tabRow := container.NewStack(track, newExactInset(container.New(&evenHBoxLayout{gap: 4}, tabItems...), 3, 3, 3, 3))
	tabLead := container.New(&tariffLeadShareLayout{share: 2.0 / 3.0}, tabRow)
	refresh()

	cap := canvas.NewText("CAPABILITIES INCLUDED IN THIS TIER", design.ColorEmptyHint)
	cap.TextSize = 8
	cap.TextStyle.Bold = true
	capRule := canvas.NewRectangle(design.ColorDialogSep)
	capRule.SetMinSize(fyne.NewSize(0, 1))
	capRow := container.NewBorder(nil, nil, cap, nil, newExactInset(capRule, 10, 0, 8, 8))

	body := container.NewBorder(
		container.New(&tightVBoxLayout{gap: 10}, tabLead, capRow),
		nil, nil, nil,
		featureScroll,
	)
	panel := newTariffDialogPanel(w.tariffDialogVersion(), body, footerSlot, closeDialog)
	popup = showOverlayPopup(parent, overlayPopupSpec{
		Panel: panel,
		PanelSize: func(canvasSize fyne.Size, _ fyne.CanvasObject) fyne.Size {
			width := tariffDialogWidth
			height := tariffDialogHeight
			if width > canvasSize.Width {
				width = canvasSize.Width
			}
			if height > canvasSize.Height {
				height = canvasSize.Height
			}
			return fyne.NewSize(width, height)
		},
	})
}

func newTariffDialogPanel(version string, body, footer fyne.CanvasObject, onClose func()) fyne.CanvasObject {
	title := canvas.NewText("Tariffs & Licenses", design.ColorTextLight)
	title.TextSize = 13
	title.TextStyle.Bold = true
	titleBits := []fyne.CanvasObject{title}
	if version != "" {
		titleBits = append(titleBits, newTariffVersionBadge(version))
	}
	titleRow := container.New(&tightHBoxLayout{gap: 8}, titleBits...)

	sub := canvas.NewText(tariffSubtitle, design.ColorMutedOlive)
	sub.TextSize = 8

	headerInner := container.New(&tightVBoxLayout{gap: 4}, titleRow, sub)
	headerSep := canvas.NewRectangle(design.ColorDialogSep)
	headerSep.SetMinSize(fyne.NewSize(0, 1))
	headerBand := canvas.NewRectangle(color.Transparent)
	headerBand.SetMinSize(fyne.NewSize(0, 52))
	header := container.New(&tightVBoxLayout{gap: 0},
		newDialogTopAccentBar(),
		container.NewStack(headerBand, newExactInset(headerInner, 21, 44, 10, 4)),
		headerSep,
	)

	widthLock := canvas.NewRectangle(color.Transparent)
	widthLock.SetMinSize(fyne.NewSize(tariffDialogWidth, 1))
	center := container.NewBorder(
		widthLock, nil, nil, nil,
		newExactInset(body, 20, 20, 0, 6),
	)

	footerSep := canvas.NewRectangle(design.ColorDialogSep)
	footerSep.SetMinSize(fyne.NewSize(0, 1))
	footerBlock := container.NewVBox(
		footerSep,
		newExactInset(footer, 20, 20, 8, 10),
	)

	inner := container.NewBorder(header, footerBlock, nil, nil, center)
	bg := canvas.NewRectangle(design.ColorGray900)
	bg.CornerRadius = design.RadiusMD
	border := canvas.NewRectangle(color.Transparent)
	border.CornerRadius = design.RadiusMD
	border.StrokeColor = design.ColorBorder
	border.StrokeWidth = 1
	closeBtn := newAccountDialogIconButton(accountDialogCloseIcon, onClose)
	return container.NewStack(
		bg,
		inner,
		container.New(&accountDialogCornerButtonLayout{Top: 12, Right: 12}, closeBtn),
		border,
	)
}

func newTariffVersionBadge(version string) fyne.CanvasObject {
	label := canvas.NewText(version, design.ColorTeal)
	label.TextSize = 8
	label.TextStyle.Bold = true
	bg := canvas.NewRectangle(design.ColorGray950)
	bg.CornerRadius = 4
	bg.StrokeColor = design.ColorTeal
	bg.StrokeWidth = 1
	inner := newExactInset(container.NewCenter(label), 6, 6, 2, 2)
	return container.New(&tariffFitLayout{}, bg, inner)
}

func (w *Window) newTariffFooter(parent fyne.Window, plan tariffPlan) fyne.CanvasObject {
	var left fyne.CanvasObject
	var right fyne.CanvasObject
	switch {
	case plan.key == protocolOpensource:
		name := canvas.NewText("Open Source", design.ColorTextLight)
		name.TextSize = 13
		name.TextStyle.Bold = true
		left = name
		gh := newIconActionButton("GitHub", assets.GitHubIcon, func() {
			parsed, err := url.Parse(tariffGitHubURL)
			if err == nil && w.app != nil {
				_ = w.app.OpenURL(parsed)
			}
		})
		gh.Compact = true
		right = gh
	case plan.key == protocolFree:
		name := canvas.NewText("Free", design.ColorTeal)
		name.TextSize = 13
		name.TextStyle.Bold = true
		left = name
	default:
		price := canvas.NewText(plan.price, design.ColorTextLight)
		price.TextSize = 20
		price.TextStyle.Bold = true
		unit := canvas.NewText("/mo", design.ColorMutedOlive)
		unit.TextSize = 11
		left = container.New(&tightHBoxLayout{gap: 3}, price, container.NewCenter(unit))
		buy := newDialogCTA(plan.buyLabel, func() {
			tier := "pro"
			if plan.key == protocolEnterprise {
				tier = "enterprise"
			}
			w.openTariffCheckout(parent, tier)
		})
		buyLock := canvas.NewRectangle(color.Transparent)
		buyLock.SetMinSize(fyne.NewSize(108, 1))
		right = container.NewStack(buyLock, buy)
	}
	return container.NewBorder(nil, nil, left, right)
}

// openTariffCheckout opens Stripe in the browser. It does not stop or
// restart Sunshine/RustShine — protocol changes stay on the main picker.
func (w *Window) openTariffCheckout(parent fyne.Window, tier string) {
	if w.token == nil {
		return
	}
	go func() {
		checkoutURL, err := w.token.StartPurchase(tier)
		if err != nil {
			return
		}
		parsed, parseErr := url.Parse(checkoutURL)
		openErr := parseErr
		if parseErr == nil && w.app != nil {
			openErr = w.app.OpenURL(parsed)
		}
		if openErr != nil && parent != nil {
			fyne.Do(func() {
				dialog.ShowInformation("Checkout",
					"Couldn't open your browser automatically.\n"+checkoutURL, parent)
			})
		}
	}()
}

func newTariffFeaturePane(plan tariffPlan) fyne.CanvasObject {
	cards := make([]fyne.CanvasObject, 0, len(plan.features))
	for _, feat := range plan.features {
		cards = append(cards, newTariffFeatureCard(feat, plan))
	}
	return container.New(&tariffTwoColLayout{gap: 8}, cards...)
}

func newTariffFeatureCard(feat tariffFeature, plan tariffPlan) fyne.CanvasObject {
	plusClr := design.ColorTeal
	switch plan.key {
	case protocolOpensource:
		plusClr = design.ColorWhite
	case protocolPro, protocolEnterprise:
		plusClr = design.ColorCTA
	}
	plus := canvas.NewText("+", plusClr)
	plus.TextSize = 13
	plus.TextStyle.Bold = true
	title := canvas.NewText(feat.title, design.ColorSectionTitle)
	title.TextSize = 11
	title.TextStyle.Bold = true
	sub := canvas.NewText(feat.subtitle, design.ColorMutedOlive)
	sub.TextSize = 8
	copyCol := container.New(&tightVBoxLayout{gap: 1}, title, sub)
	inner := container.New(&tightHBoxLayout{gap: 6}, plus, copyCol)

	bg := canvas.NewRectangle(tariffCardFill)
	bg.CornerRadius = 8
	bg.StrokeColor = design.ColorDialogSep
	bg.StrokeWidth = 1
	lock := canvas.NewRectangle(color.Transparent)
	lock.SetMinSize(fyne.NewSize(1, 48))
	return container.NewStack(bg, lock, newExactInset(inner, 8, 8, 6, 6))
}

type tariffTabButton struct {
	widget.BaseWidget
	label    string
	key      string
	selected bool
	hovered  bool
	onTap    func()

	text *canvas.Text
	bg   *canvas.Rectangle
}

func newTariffTabButton(label, key string, onTap func()) *tariffTabButton {
	b := &tariffTabButton{label: label, key: key, onTap: onTap}
	b.ExtendBaseWidget(b)
	return b
}

func (b *tariffTabButton) SetSelected(on bool) {
	b.selected = on
	b.refreshVisuals()
}

func (b *tariffTabButton) Tapped(*fyne.PointEvent) {
	if b.onTap != nil {
		b.onTap()
	}
}

func (b *tariffTabButton) TappedSecondary(*fyne.PointEvent) {}

func (b *tariffTabButton) Cursor() desktop.Cursor { return desktop.PointerCursor }

func (b *tariffTabButton) MouseIn(*desktop.MouseEvent) {
	b.hovered = true
	b.refreshVisuals()
}

func (b *tariffTabButton) MouseMoved(*desktop.MouseEvent) {}

func (b *tariffTabButton) MouseOut() {
	b.hovered = false
	b.refreshVisuals()
}

func (b *tariffTabButton) selectedFill() (fill, text color.Color) {
	switch b.key {
	case protocolOpensource:
		return design.ColorWhite, design.ColorGray950
	case protocolFree:
		return design.ColorTeal, design.ColorGray950
	default:
		return design.ColorProSoft, design.ColorGray950
	}
}

func (b *tariffTabButton) refreshVisuals() {
	if b.text == nil || b.bg == nil {
		return
	}
	switch {
	case b.selected:
		fill, text := b.selectedFill()
		b.bg.FillColor = fill
		b.bg.StrokeColor = color.Transparent
		b.text.Color = text
	case b.hovered:
		b.bg.FillColor = design.ColorSurfaceLight
		b.bg.StrokeColor = color.Transparent
		b.text.Color = design.ColorTextLight
	default:
		b.bg.FillColor = color.Transparent
		b.bg.StrokeColor = color.Transparent
		switch b.key {
		case protocolPro, protocolEnterprise:
			b.text.Color = design.ColorProSoft
		case protocolFree:
			b.text.Color = design.ColorTeal
		default:
			b.text.Color = design.ColorMutedOlive
		}
	}
	b.bg.StrokeWidth = 0
	b.bg.Refresh()
	b.text.Refresh()
}

func (b *tariffTabButton) CreateRenderer() fyne.WidgetRenderer {
	b.bg = canvas.NewRectangle(color.Transparent)
	b.bg.CornerRadius = 6
	b.text = canvas.NewText(b.label, design.ColorMutedOlive)
	b.text.TextSize = 11
	b.text.TextStyle.Bold = true
	b.text.Alignment = fyne.TextAlignCenter
	lock := canvas.NewRectangle(color.Transparent)
	lock.SetMinSize(fyne.NewSize(1, 26))
	b.refreshVisuals()
	return widget.NewSimpleRenderer(container.NewMax(b.bg, lock, container.NewCenter(b.text)))
}

var (
	_ fyne.Tappable     = (*tariffTabButton)(nil)
	_ desktop.Hoverable = (*tariffTabButton)(nil)
)

type evenHBoxLayout struct {
	gap float32
}

func (l *evenHBoxLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	visible := make([]fyne.CanvasObject, 0, len(objects))
	for _, o := range objects {
		if o != nil && o.Visible() {
			visible = append(visible, o)
		}
	}
	n := len(visible)
	if n == 0 {
		return
	}
	slot := (size.Width - l.gap*float32(n-1)) / float32(n)
	if slot < 0 {
		slot = 0
	}
	x := float32(0)
	for _, o := range visible {
		o.Move(fyne.NewPos(x, 0))
		o.Resize(fyne.NewSize(slot, size.Height))
		x += slot + l.gap
	}
}

func (l *evenHBoxLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	var cellW, cellH float32
	n := 0
	for _, o := range objects {
		if o == nil || !o.Visible() {
			continue
		}
		m := o.MinSize()
		if m.Width > cellW {
			cellW = m.Width
		}
		if m.Height > cellH {
			cellH = m.Height
		}
		n++
	}
	if n == 0 {
		return fyne.NewSize(0, 0)
	}
	return fyne.NewSize(cellW*float32(n)+l.gap*float32(n-1), cellH)
}

type tariffLeadShareLayout struct {
	share float32
}

func (l *tariffLeadShareLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) == 0 || objects[0] == nil || !objects[0].Visible() {
		return
	}
	share := l.share
	if share <= 0 || share > 1 {
		share = 2.0 / 3.0
	}
	w := size.Width * share
	h := objects[0].MinSize().Height
	if h > size.Height {
		h = size.Height
	}
	objects[0].Move(fyne.NewPos(0, 0))
	objects[0].Resize(fyne.NewSize(w, h))
}

func (l *tariffLeadShareLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	if len(objects) == 0 || objects[0] == nil || !objects[0].Visible() {
		return fyne.NewSize(0, 0)
	}
	return objects[0].MinSize()
}

type tariffTwoColLayout struct {
	gap float32
}

func (l *tariffTwoColLayout) visible(objects []fyne.CanvasObject) []fyne.CanvasObject {
	out := make([]fyne.CanvasObject, 0, len(objects))
	for _, o := range objects {
		if o != nil && o.Visible() {
			out = append(out, o)
		}
	}
	return out
}

func (l *tariffTwoColLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	vis := l.visible(objects)
	n := len(vis)
	if n == 0 {
		return
	}
	colW := (size.Width - l.gap) / 2
	if colW < 0 {
		colW = 0
	}
	y := float32(0)
	for i := 0; i < n; i += 2 {
		left := vis[i]
		h := left.MinSize().Height
		var right fyne.CanvasObject
		if i+1 < n {
			right = vis[i+1]
			if rh := right.MinSize().Height; rh > h {
				h = rh
			}
		}
		left.Move(fyne.NewPos(0, y))
		left.Resize(fyne.NewSize(colW, h))
		if right != nil {
			right.Move(fyne.NewPos(colW+l.gap, y))
			right.Resize(fyne.NewSize(colW, h))
		}
		y += h + l.gap
	}
}

func (l *tariffTwoColLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	vis := l.visible(objects)
	if len(vis) == 0 {
		return fyne.NewSize(0, 0)
	}
	var totalH, maxRowW float32
	for i := 0; i < len(vis); i += 2 {
		lm := vis[i].MinSize()
		h := lm.Height
		w := lm.Width
		if i+1 < len(vis) {
			rm := vis[i+1].MinSize()
			if rm.Height > h {
				h = rm.Height
			}
			w += l.gap + rm.Width
		}
		if w > maxRowW {
			maxRowW = w
		}
		if i > 0 {
			totalH += l.gap
		}
		totalH += h
	}
	return fyne.NewSize(maxRowW, totalH)
}

type tariffFitLayout struct{}

func (l *tariffFitLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	var w, h float32
	for _, o := range objects {
		if o == nil || !o.Visible() {
			continue
		}
		m := o.MinSize()
		if m.Width > w {
			w = m.Width
		}
		if m.Height > h {
			h = m.Height
		}
	}
	return fyne.NewSize(w, h)
}

func (l *tariffFitLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	for _, o := range objects {
		if o == nil || !o.Visible() {
			continue
		}
		m := o.MinSize()
		y := (size.Height - m.Height) / 2
		if y < 0 {
			y = 0
		}
		o.Move(fyne.NewPos(0, y))
		o.Resize(m)
	}
}
