package view

// firmware_promo.go -- the slim "USBridge Firmware" strip on the Connections
// screen, between the section header and the Grid/List. Isolated from the
// cards/table so dismissing it, rotating features, and the board menu
// cannot leak into connection-row state. The controller only wires
// dismiss/restore + opening FirmwarePromoURL; everything visual lives here.

import (
	"image/color"
	"time"

	"usbridge-client/internal/gui/assets"
	"usbridge-client/internal/gui/design"
	"usbridge-client/internal/gui/i18n"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
)

// FirmwarePromoURL is the 24h-trial / board-download landing page -- the
// banner CTA and the footer's "software" chip both open it.
const FirmwarePromoURL = "https://www.usbridge.io/kvm-software"

var firmwarePromoBoardsList = []string{
	"Radxa Zero 3W / 3E",
	"Radxa Cubie A7Z",
}

func firmwarePromoBoardDetails() map[string]string {
	return map[string]string{
		"Radxa Zero 3W / 3E": i18n.Current.FirmwarePromoSDCardEMMC,
		"Radxa Cubie A7Z":    i18n.Current.FirmwarePromoSDCardOnly,
	}
}

func firmwarePromoFeatures() []string {
	return []string{
		i18n.Current.FirmwarePromoFeatureBIOS,
		i18n.Current.FirmwarePromoFeatureLatency,
		i18n.Current.FirmwarePromoFeatureScripts,
		i18n.Current.FirmwarePromoFeatureSnapshot,
		i18n.Current.FirmwarePromoFeatureL0,
	}
}

const firmwarePromoFeatureInterval = 2800 * time.Millisecond

var (
	firmwarePromoCheckIcon = fyne.NewStaticResource("firmware-promo-check.svg", []byte(
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="#41e0c3" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="9"/><path d="M7.8 12.4l2.8 2.8 5.6-5.8"/></svg>`))
	firmwarePromoFeatureColor = design.ColorConnectionBadgeText
)

// FirmwarePromoBanner is the Connections-screen firmware promo: CPU icon,
// title/subtitle, available-board menu, a rotating "included" feature, and
// a 24h Trial button. Hover reveals a top-right X that dismisses it into
// the footer chip (see FooterPromoChip / NewFooterLabelChip).
type FirmwarePromoBanner struct {
	widget.BaseWidget

	onDismiss func()
	onTrial   func()

	hovered      bool
	closeHovered bool
	closeBtn     *iconChromeButton
	featureIcon  *canvas.Image
	featureLabel *canvas.Text
	featureIdx   int
	featureAnim  *fyne.Animation
	rotStop      chan struct{}
	// flushMargins skips the Connections-header side inset so the strip
	// can sit inside an already-padded column (Scripts' automation half).
	flushMargins bool
}

var _ desktop.Hoverable = (*FirmwarePromoBanner)(nil)

func NewFirmwarePromoBanner() *FirmwarePromoBanner {
	b := &FirmwarePromoBanner{}
	b.ExtendBaseWidget(b)
	return b
}

func (b *FirmwarePromoBanner) SetOnDismiss(fn func()) {
	b.onDismiss = fn
}

func (b *FirmwarePromoBanner) SetOnTrial(fn func()) {
	b.onTrial = fn
}

func (b *FirmwarePromoBanner) SetFlushMargins(on bool) {
	b.flushMargins = on
}

func (b *FirmwarePromoBanner) Show() {
	b.BaseWidget.Show()
	b.startRotation()
}

func (b *FirmwarePromoBanner) Hide() {
	b.stopRotation()
	b.hovered = false
	b.closeHovered = false
	b.syncClose()
	b.BaseWidget.Hide()
}

func (b *FirmwarePromoBanner) MouseIn(*desktop.MouseEvent) {
	b.setHovered(true)
}

func (b *FirmwarePromoBanner) MouseOut() {
	b.setHovered(false)
}

func (b *FirmwarePromoBanner) MouseMoved(*desktop.MouseEvent) {}

func (b *FirmwarePromoBanner) setHovered(hovered bool) {
	if hovered {
		b.hovered = true
		b.syncClose()
		return
	}
	b.hovered = false
	time.AfterFunc(80*time.Millisecond, func() {
		fyne.Do(b.syncClose)
	})
}

func (b *FirmwarePromoBanner) syncClose() {
	if b.closeBtn == nil {
		return
	}
	if b.hovered || b.closeHovered {
		b.closeBtn.Show()
	} else {
		b.closeBtn.Hide()
	}
	b.closeBtn.Refresh()
}

func (b *FirmwarePromoBanner) startRotation() {
	b.stopRotation()
	if !b.Visible() {
		return
	}
	stop := make(chan struct{})
	b.rotStop = stop
	go func() {
		t := time.NewTicker(firmwarePromoFeatureInterval)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				fyne.Do(b.advanceFeature)
			}
		}
	}()
}

func (b *FirmwarePromoBanner) stopRotation() {
	if b.rotStop != nil {
		close(b.rotStop)
		b.rotStop = nil
	}
	if b.featureAnim != nil {
		b.featureAnim.Stop()
		b.featureAnim = nil
	}
}

func (b *FirmwarePromoBanner) advanceFeature() {
	features := firmwarePromoFeatures()
	if !b.Visible() || b.featureLabel == nil || len(features) == 0 {
		return
	}
	next := (b.featureIdx + 1) % len(features)
	if b.featureAnim != nil {
		b.featureAnim.Stop()
	}
	swapped := false
	anim := fyne.NewAnimation(180*time.Millisecond, func(done float32) {
		if b.featureLabel == nil {
			return
		}
		if done < 0.5 {
			a := 1 - done*2
			b.applyFeatureAlpha(a)
			return
		}
		if !swapped {
			b.featureIdx = next
			b.featureLabel.Text = features[b.featureIdx]
			swapped = true
		}
		b.applyFeatureAlpha((done - 0.5) * 2)
	})
	b.featureAnim = anim
	anim.Start()
}

func (b *FirmwarePromoBanner) applyFeatureAlpha(a float32) {
	if a < 0 {
		a = 0
	}
	if a > 1 {
		a = 1
	}
	if b.featureLabel != nil {
		b.featureLabel.Color = firmwarePromoFade(firmwarePromoFeatureColor, a)
		b.featureLabel.Refresh()
	}
	if b.featureIcon != nil {
		b.featureIcon.Translucency = 1 - float64(a)
		b.featureIcon.Refresh()
	}
}

func firmwarePromoFade(c color.Color, alpha float32) color.Color {
	n, ok := c.(color.NRGBA)
	if !ok {
		r, g, b, _ := c.RGBA()
		n = color.NRGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: 0xff}
	}
	n.A = uint8(alpha * 255)
	return n
}

func (b *FirmwarePromoBanner) CreateRenderer() fyne.WidgetRenderer {
	cpu := canvas.NewImageFromResource(assets.USBridgeOSIconAccent)
	cpu.FillMode = canvas.ImageFillContain
	cpu.SetMinSize(fyne.NewSize(18, 18))

	title := NewBrandText(i18n.Current.FirmwarePromoTitle, 12, design.ColorConnectionsSectionTitle, true)
	subtitle := canvas.NewText(i18n.Current.FirmwarePromoSubtitle, design.ColorConnectionsSectionSubtitle)
	subtitle.TextSize = 9
	titleBlock := container.New(&tightStatsVBoxLayout{Gap: 1}, title, subtitle)

	b.featureIcon = canvas.NewImageFromResource(firmwarePromoCheckIcon)
	b.featureIcon.FillMode = canvas.ImageFillContain
	b.featureIcon.SetMinSize(fyne.NewSize(12, 12))
	features := firmwarePromoFeatures()
	featureText := ""
	if len(features) > 0 {
		featureText = features[b.featureIdx%len(features)]
	}
	b.featureLabel = canvas.NewText(featureText, firmwarePromoFeatureColor)
	b.featureLabel.TextSize = 10
	featureWidth := firmwarePromoFeatureMinWidth()
	featureInner := container.New(&DeviceRowControlsLayout{Gap: 6}, b.featureIcon, b.featureLabel)
	featureSlot := canvas.NewRectangle(color.Transparent)
	featureSlot.SetMinSize(fyne.NewSize(featureWidth, 14))
	featureRow := container.NewMax(featureSlot, featureInner)

	trialBtn := newIconChromeButton(iconChromeButtonSpec{
		NormalFill:      design.ColorConnectionAddFill,
		HoverFill:       design.ColorConnectionAddFillHover,
		Stroke:          color.Transparent,
		LabelColor:      color.NRGBA{R: 0x4c, G: 0x68, B: 0x03, A: 0xff},
		HoverLabelColor: color.NRGBA{R: 0x4c, G: 0x68, B: 0x03, A: 0xff},
		LabelSize:       10,
		LabelBold:       true,
		CornerRadius:    6,
		ButtonSize:      fyne.NewSize(0, 26),
		OnHover:         b.setHovered,
		OnTapped: func() {
			if b.onTrial != nil {
				b.onTrial()
			}
		},
	})
	trialBtn.SetText(i18n.Current.FirmwarePromoTrial)

	titleCluster := container.New(&DeviceRowControlsLayout{Gap: 10},
		container.NewCenter(cpu),
		titleBlock,
	)
	// Scripts' half-width column cannot fit the board picker; Connections
	// keeps the extra gap so "Radxa …" sits a bit right of the subtitle.
	var left fyne.CanvasObject = titleCluster
	if !b.flushMargins {
		boards := NewHeaderDropdown(nil, "", nil)
		boards.UltraCompact = true
		boards.CornerRadius = 6
		boards.BorderColor = design.ColorTailscaleChipBorder
		boards.TextColor = design.ColorConnectionBadgeText
		boards.IconColor = color.NRGBA{R: 0xc5, G: 0xc8, B: 0xb5, A: 0xff}
		boards.TextSize = 10
		boards.HoverBorderColor = design.ColorConnectionBadgeText
		boards.HoverFillColor = design.ColorGray900
		boards.SetDetails(firmwarePromoBoardDetails())
		boards.OnHover = b.setHovered
		boards.SetOptions(firmwarePromoBoardsList)
		boards.SetSelected(firmwarePromoBoardsList[0])
		left = container.New(&DeviceRowControlsLayout{Gap: 22}, titleCluster, boards)
	}
	right := container.New(&DeviceRowControlsLayout{Gap: 12}, featureRow, trialBtn)
	row := container.NewBorder(nil, nil, left, right)

	bg := canvas.NewRectangle(design.ColorGray900)
	bg.CornerRadius = design.RadiusMD
	border := canvas.NewRectangle(color.Transparent)
	border.CornerRadius = design.RadiusMD
	border.StrokeColor = design.ColorTailscaleChipBorder
	border.StrokeWidth = 1

	b.closeBtn = newIconChromeButton(iconChromeButtonSpec{
		NormalFill:   color.Transparent,
		HoverFill:    design.ColorSurfaceLight,
		Stroke:       color.Transparent,
		CornerRadius: 3,
		NormalIcon:   scriptFooterCloseIcon,
		HoverIcon:    scriptFooterCloseHoverIcon,
		IconSize:     fyne.NewSize(10, 10),
		ButtonSize:   fyne.NewSize(16, 16),
		OnHover: func(on bool) {
			b.closeHovered = on
			if on {
				b.hovered = true
			}
			b.syncClose()
			if !on {
				b.setHovered(false)
			}
		},
		OnTapped: func() {
			if b.onDismiss != nil {
				b.onDismiss()
			}
		},
	})
	b.closeBtn.Hide()
	closeSlot := container.NewBorder(
		NewInsetExact(container.NewHBox(layout.NewSpacer(), b.closeBtn), 0, 6, 4, 0),
		nil, nil, nil,
	)

	// Extra right inset keeps Trial/features clear of the hover X; matching
	// extra left inset so the strip still reads even, not shifted.
	inner := NewInsetExact(row, 20, 28, 7, 7)
	bar := container.NewStack(bg, inner, border, closeSlot)
	// Side margins match the section header / cards so the strip lines up
	// with them instead of hugging the window edge. Scripts' automation
	// column is already inset, so flushMargins keeps only the top gap.
	side := connectionsHeaderSideMargin
	if b.flushMargins {
		side = 0
	}
	content := NewInset(bar, side, side, 6, 0)

	b.syncClose()
	if b.Visible() {
		b.startRotation()
	}
	return widget.NewSimpleRenderer(content)
}

func firmwarePromoFeatureMinWidth() float32 {
	max := float32(0)
	for _, text := range firmwarePromoFeatures() {
		measure := canvas.NewText(text, firmwarePromoFeatureColor)
		measure.TextSize = 10
		if w := measure.MinSize().Width; w > max {
			max = w
		}
	}
	return 12 + 6 + max
}
