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
	tariffDialogWidth     float32 = 540
	tariffDialogHeight    float32 = 360
	tariffFooterH         float32 = 32
	tariffGitHubURL               = "https://github.com/USBridge-Technologies/USBridge-Remote/"
	tariffPricePro                = "$8/mo"
	tariffPriceEnterprise         = "$25/mo"
)

type tariffPlan struct {
	key      string
	tab      string
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
		key:   protocolPro,
		tab:   "Pro",
		price: tariffPricePro,
		paid:  true,
		features: []tariffFeature{
			{"4:4:4 color fidelity", "Full chroma for text and color-critical work"},
			{"USB device emulation", "Pass local USB devices through to the host"},
			{"Wacom tablet support", "Pen pressure and tilt pass through to the host"},
		},
	},
	{
		key:   protocolEnterprise,
		tab:   "Enterprise",
		price: tariffPriceEnterprise,
		paid:  true,
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

// showTariffPickerDialog is an info overlay: four tariff tabs, the selected
// plan's features, and a footer with Buy+$ except Free / Open Source
// (GitHub on Open Source). initialKey selects that tab (Protocol info /
// muted Buy Pro). Tabs only change the copy — they do not switch the streamer.
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
	tabRow := container.New(&evenHBoxLayout{gap: 8}, tabItems...)
	refresh()

	body := container.NewBorder(tabRow, nil, nil, nil, newExactInset(featureScroll, 0, 0, 10, 0))
	panel := newBrandedDialogPanelInsets("Tariffs", tariffDialogWidth, 20, 0, body, footerSlot, closeDialog)
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

func (w *Window) newTariffFooter(parent fyne.Window, plan tariffPlan) fyne.CanvasObject {
	right := make([]fyne.CanvasObject, 0, 3)
	if plan.key == protocolOpensource {
		gh := newIconActionButton("GitHub", assets.GitHubIcon, func() {
			parsed, err := url.Parse(tariffGitHubURL)
			if err == nil && w.app != nil {
				_ = w.app.OpenURL(parsed)
			}
		})
		gh.Compact = true
		ossLbl := canvas.NewText("Open Source", design.ColorTextLight)
		ossLbl.TextSize = 12
		ossLbl.TextStyle.Bold = true
		right = append(right, gh, container.NewCenter(ossLbl))
	} else if plan.key == protocolFree {
		freeLbl := canvas.NewText("Free", design.ColorTeal)
		freeLbl.TextSize = 12
		freeLbl.TextStyle.Bold = true
		right = append(right, container.NewCenter(freeLbl))
	} else {
		price := canvas.NewText(plan.price, design.ColorTextLight)
		price.TextSize = 12
		price.TextStyle.Bold = true
		buy := newDialogCTA("Buy", func() {
			tier := "pro"
			if plan.key == protocolEnterprise {
				tier = "enterprise"
			}
			w.openTariffCheckout(parent, tier)
		})
		buyLock := canvas.NewRectangle(color.Transparent)
		buyLock.SetMinSize(fyne.NewSize(80, 1))
		right = append(right, container.NewCenter(price), container.NewStack(buyLock, buy))
	}
	return container.NewBorder(nil, nil, nil, container.NewCenter(container.New(&tightHBoxLayout{gap: 10}, right...)))
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
	items := make([]fyne.CanvasObject, 0, len(plan.features))
	for _, feat := range plan.features {
		items = append(items, newTariffFeatureRow(feat, plan))
	}
	return container.NewVBox(items...)
}

func newTariffFeatureRow(feat tariffFeature, plan tariffPlan) fyne.CanvasObject {
	var mark fyne.CanvasObject
	if plan.paid {
		plus := canvas.NewText("+", design.ColorPro)
		plus.TextSize = 12
		plus.TextStyle.Bold = true
		mark = plus
	} else {
		dotClr := design.ColorTeal
		if plan.key == protocolOpensource {
			dotClr = design.ColorWhite
		}
		dot := canvas.NewRectangle(dotClr)
		dot.CornerRadius = 5
		dot.SetMinSize(fyne.NewSize(5, 5))
		mark = container.NewCenter(dot)
	}
	title := canvas.NewText(feat.title, design.ColorSectionTitle)
	title.TextSize = 11
	title.TextStyle.Bold = true
	sub := canvas.NewText(feat.subtitle, design.ColorMutedOlive)
	sub.TextSize = 9
	copyCol := container.New(&tightVBoxLayout{gap: 1}, title, sub)
	return newExactInset(container.New(&tightHBoxLayout{gap: 8}, mark, copyCol), 0, 0, 3, 3)
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
		b.bg.StrokeColor = design.ColorChromeOlive
		b.text.Color = design.ColorTextLight
	default:
		b.bg.FillColor = design.ColorGray950
		b.bg.StrokeColor = design.ColorChromeOlive
		switch b.key {
		case protocolPro, protocolEnterprise:
			b.text.Color = design.ColorProSoft
		case protocolFree:
			b.text.Color = design.ColorTeal
		default:
			b.text.Color = design.ColorMutedOlive
		}
	}
	b.bg.StrokeWidth = 1
	b.bg.Refresh()
	b.text.Refresh()
}

func (b *tariffTabButton) CreateRenderer() fyne.WidgetRenderer {
	b.bg = canvas.NewRectangle(design.ColorGray950)
	b.bg.CornerRadius = 6
	b.bg.StrokeWidth = 1
	b.text = canvas.NewText(b.label, design.ColorMutedOlive)
	b.text.TextSize = 11
	b.text.TextStyle.Bold = true
	b.text.Alignment = fyne.TextAlignCenter
	lock := canvas.NewRectangle(color.Transparent)
	lock.SetMinSize(fyne.NewSize(1, 28))
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
