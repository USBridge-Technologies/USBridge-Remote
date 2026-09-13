package view

import (
	"image/color"
	"strings"
	"time"

	"usbridge-client/internal/gui/assets"
	"usbridge-client/internal/gui/design"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
)

var (
	deviceFirmwarePromoWebIcon = fyne.NewStaticResource("device_firmware_promo_web.svg", []byte(strings.ReplaceAll(string(assets.LanguageIcon.Content()), "#F5F5F5", "#4c6803")))
)

const (
	deviceFirmwarePromoTitle    = "USBridge Firmware"
	deviceFirmwarePromoSubtitle = "Turn your board into a hardware KVM"
)

// DeviceFirmwarePromo is the Devices-tab stand-in for Network and Backup
// Flash on a software agent: one card with a shared title,
// a small website button, a hover-only X, and three compact feature
// plaques inside. Shown only while connected to a software agent (not
// hardware KVM). Border stays idle until hover — same as the other
// dashboard cards (see NewDeviceDashboardCard).
type DeviceFirmwarePromo struct {
	widget.BaseWidget

	onDismiss    func()
	onOpen       func()
	hovered      bool
	closeHovered bool
	border       *canvas.Rectangle
	closeBtn     *iconChromeButton
}

var _ desktop.Hoverable = (*DeviceFirmwarePromo)(nil)

func NewDeviceFirmwarePromo() *DeviceFirmwarePromo {
	p := &DeviceFirmwarePromo{}
	p.ExtendBaseWidget(p)
	p.Hide()
	return p
}

func (p *DeviceFirmwarePromo) SetOnDismiss(fn func()) {
	p.onDismiss = fn
}

func (p *DeviceFirmwarePromo) SetOnOpen(fn func()) {
	p.onOpen = fn
}

func (p *DeviceFirmwarePromo) MouseIn(*desktop.MouseEvent) {
	p.setHovered(true)
}

func (p *DeviceFirmwarePromo) MouseOut() {
	p.setHovered(false)
}

func (p *DeviceFirmwarePromo) MouseMoved(*desktop.MouseEvent) {}

func (p *DeviceFirmwarePromo) setHovered(hovered bool) {
	if hovered {
		p.hovered = true
		p.syncChrome()
		return
	}
	p.hovered = false
	time.AfterFunc(50*time.Millisecond, func() {
		fyne.Do(p.syncChrome)
	})
}

func (p *DeviceFirmwarePromo) syncChrome() {
	if p.border != nil {
		if p.hovered || p.closeHovered {
			p.border.StrokeColor = design.ColorConnectionBadgeText
		} else {
			p.border.StrokeColor = design.ColorTailscaleChipBorder
		}
		p.border.Refresh()
	}
	if p.closeBtn == nil {
		return
	}
	if p.hovered || p.closeHovered {
		p.closeBtn.Show()
	} else {
		p.closeBtn.Hide()
	}
	p.closeBtn.Refresh()
}

func (p *DeviceFirmwarePromo) CreateRenderer() fyne.WidgetRenderer {
	cpu := canvas.NewImageFromResource(assets.USBridgeOSIconAccent)
	cpu.FillMode = canvas.ImageFillContain
	cpu.SetMinSize(fyne.NewSize(16, 16))

	title := NewBrandText(deviceFirmwarePromoTitle, 12, design.ColorConnectionsSectionTitle, true)
	subtitle := canvas.NewText(deviceFirmwarePromoSubtitle, design.ColorConnectionsSectionSubtitle)
	subtitle.TextSize = 9
	titleBlock := container.New(&tightStatsVBoxLayout{Gap: 1}, title, subtitle)
	titleCluster := container.New(&DeviceRowControlsLayout{Gap: 13}, container.NewCenter(cpu), titleBlock)

	webBtn := newIconChromeButton(iconChromeButtonSpec{
		NormalFill:   design.ColorConnectionAddFill,
		HoverFill:    design.ColorConnectionAddFillHover,
		Stroke:       color.Transparent,
		CornerRadius: 6,
		NormalIcon:   deviceFirmwarePromoWebIcon,
		HoverIcon:    deviceFirmwarePromoWebIcon,
		IconSize:     fyne.NewSize(12, 12),
		ButtonSize:   fyne.NewSize(22, 22),
		OnHover:      p.setHovered,
		OnTapped: func() {
			if p.onOpen != nil {
				p.onOpen()
			}
		},
	})
	p.closeBtn = newIconChromeButton(iconChromeButtonSpec{
		NormalFill:   color.Transparent,
		HoverFill:    design.ColorSurfaceLight,
		Stroke:       color.Transparent,
		CornerRadius: 3,
		NormalIcon:   scriptFooterCloseIcon,
		HoverIcon:    scriptFooterCloseHoverIcon,
		IconSize:     fyne.NewSize(10, 10),
		ButtonSize:   fyne.NewSize(16, 16),
		OnHover: func(on bool) {
			p.closeHovered = on
			if on {
				p.hovered = true
			}
			p.syncChrome()
			if !on {
				p.setHovered(false)
			}
		},
		OnTapped: func() {
			if p.onDismiss != nil {
				p.onDismiss()
			}
		},
	})
	p.closeBtn.Hide()
	// Fixed-width slot so the website button never jumps left when the
	// hover X appears -- Hide() on the X itself would collapse the row.
	// The X itself is overlaid on the card (see closeOverlay) so it can
	// sit a bit higher and further right than this header slot.
	closeReserve := canvas.NewRectangle(color.Transparent)
	closeReserve.SetMinSize(fyne.NewSize(16, 16))
	headerRight := container.New(&DeviceRowControlsLayout{Gap: 4}, webBtn, closeReserve)
	header := container.NewBorder(nil, nil, titleCluster, headerRight)

	plaques := container.New(&DeviceDashboardPairLayout{Gap: 8},
		newFirmwareFeaturePlaque(DeviceDashboardNetworkIconSVG, "Network"),
		newFirmwareFeaturePlaque(DeviceDashboardBackupsIconSVG, "Backup Flash"),
		newFirmwareFeaturePlaque(DeviceDashboardStorageIconSVG, "ISO Media"),
	)
	inner := NewInsetExact(container.New(&tightStatsVBoxLayout{Gap: 10}, header, plaques), 12, 12, 10, 10)

	p.border = canvas.NewRectangle(design.ColorGray900)
	p.border.CornerRadius = design.RadiusLG
	p.border.StrokeColor = design.ColorTailscaleChipBorder
	p.border.StrokeWidth = 1
	// Behind the content so website/X still get first claim on the cursor;
	// covers the empty card area the interactive controls don't occupy.
	overlay := newConnectionCardOverlay(nil, p.setHovered)

	closeOverlay := container.NewBorder(
		NewInsetExact(container.NewHBox(layout.NewSpacer(), p.closeBtn), 0, 4, 4, 0),
		nil, nil, nil,
	)

	p.syncChrome()
	return widget.NewSimpleRenderer(container.NewStack(overlay, p.border, inner, closeOverlay))
}

func newFirmwareFeaturePlaque(icon fyne.Resource, label string) fyne.CanvasObject {
	img := canvas.NewImageFromResource(icon)
	img.FillMode = canvas.ImageFillContain
	img.SetMinSize(fyne.NewSize(12, 12))
	text := canvas.NewText(label, design.ColorTextLight)
	text.TextSize = 10
	row := container.New(&DeviceRowControlsLayout{Gap: 6}, img, text)
	outline := canvas.NewRectangle(color.Transparent)
	outline.CornerRadius = 6
	outline.StrokeColor = design.ColorTailscaleChipBorder
	outline.StrokeWidth = 1
	return container.NewStack(outline, NewInsetExact(container.NewCenter(row), 10, 10, 6, 6))
}
