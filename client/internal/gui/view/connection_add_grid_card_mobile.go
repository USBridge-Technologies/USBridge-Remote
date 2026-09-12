package view

import (
	"image/color"

	"usbridge-client/internal/gui/assets"
	"usbridge-client/internal/gui/design"
	"usbridge-client/internal/gui/i18n"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
)

// newMobileAddConnectionGridCard is the phone empty-state add tile: same
// "+" / QR / paste-link actions as desktop, tighter padding, no fixed
// 280×205 footprint, and an always-visible dismiss X (no hover on a phone).
func newMobileAddConnectionGridCard(actions AddConnectionCardActions) fyne.CanvasObject {
	plusImg := canvas.NewImageFromResource(addConnectionPlusIconNormal)
	plusImg.FillMode = canvas.ImageFillContain
	plusImg.SetMinSize(fyne.NewSize(16, 16))

	const ringSize float32 = 40
	addRing := canvas.NewCircle(color.Transparent)
	addRing.StrokeColor = addConnectionCardMutedColor
	addRing.StrokeWidth = 1.5
	addRingSized := container.NewGridWrap(fyne.NewSize(ringSize, ringSize), addRing)

	addBtn := newIconChromeButton(iconChromeButtonSpec{
		NormalFill:   color.Transparent,
		HoverFill:    addConnectionCardButtonHoverTint,
		Stroke:       color.Transparent,
		CornerRadius: ringSize / 2,
		ButtonSize:   fyne.NewSize(ringSize, ringSize),
		OnTapped:     actions.OnAdd,
	})
	addControl := container.NewStack(addRingSized, container.NewCenter(plusImg), addBtn)

	title := NewBrandText(i18n.Current.AddNewConnectTitle, 12, design.ColorTextLight, true)
	subtitleLine1 := canvas.NewText(i18n.Current.AddConnectHintLine1, addConnectionCardMutedColor)
	subtitleLine2 := canvas.NewText(i18n.Current.AddConnectHintLine2, addConnectionCardMutedColor)
	for _, line := range []*canvas.Text{subtitleLine1, subtitleLine2} {
		line.TextSize = 9
		line.Alignment = fyne.TextAlignCenter
	}
	titleGroup := container.New(&tightStatsVBoxLayout{Gap: 3},
		container.NewCenter(title),
		container.NewCenter(container.New(&connectionSubtitleLayout{gap: 1}, subtitleLine1, subtitleLine2)),
	)

	const btnH float32 = 30
	qrBtn := newIconChromeButton(iconChromeButtonSpec{
		NormalFill:      color.Transparent,
		HoverFill:       color.Transparent,
		DisabledFill:    connectionActionBlockedFill,
		Stroke:          design.ColorTailscaleChipBorder,
		StrokeWidth:     1,
		CornerRadius:    6,
		LabelColor:      addConnectionCardMutedColor,
		HoverLabelColor: addConnectionCardHoverColor,
		HoverStroke:     addConnectionCardHoverColor,
		LabelSize:       10,
		NormalIcon:      assets.QRCodeLight,
		HoverIcon:       assets.QRCodeAccent,
		IconSize:        fyne.NewSize(12, 12),
		ButtonSize:      fyne.NewSize(0, btnH),
		OnTapped:        actions.OnQR,
	})
	qrBtn.SetText(i18n.Current.ScanQR)

	pasteBtn := newIconChromeButton(iconChromeButtonSpec{
		NormalFill:      color.Transparent,
		HoverFill:       color.Transparent,
		DisabledFill:    connectionActionBlockedFill,
		Stroke:          design.ColorTailscaleChipBorder,
		StrokeWidth:     1,
		CornerRadius:    6,
		LabelColor:      addConnectionCardMutedColor,
		HoverLabelColor: addConnectionCardHoverColor,
		HoverStroke:     addConnectionCardHoverColor,
		LabelSize:       10,
		NormalIcon:      assets.LinkIconMuted,
		HoverIcon:       assets.LinkIconLime,
		IconSize:        fyne.NewSize(12, 12),
		ButtonSize:      fyne.NewSize(0, btnH),
		OnTapped:        actions.OnPasteLink,
	})
	pasteBtn.SetText(i18n.Current.PasteLink)

	buttonsRow := container.New(&DeviceRowControlsLayout{Gap: 12}, qrBtn, pasteBtn)
	inner := container.New(&tightStatsVBoxLayout{Gap: 8},
		container.NewCenter(addControl),
		titleGroup,
		container.NewCenter(buttonsRow),
	)
	content := NewInsetExact(inner, 12, 12, 12, 12)

	cardBg := canvas.NewRectangle(design.ColorGray900)
	cardBg.CornerRadius = design.RadiusLG
	dashedBorder, setBorderHovered := newAddConnectionCardDashedBorder()

	applyHoverVisuals := func(hovered bool) {
		setBorderHovered(hovered)
		if hovered {
			title.Color = addConnectionCardHoverColor
			addRing.StrokeColor = addConnectionCardHoverColor
			addRing.FillColor = addConnectionCardRingHoverTint
			plusImg.Resource = addConnectionPlusIconHover
		} else {
			title.Color = design.ColorTextLight
			addRing.StrokeColor = addConnectionCardMutedColor
			addRing.FillColor = color.Transparent
			plusImg.Resource = addConnectionPlusIconNormal
		}
		title.Refresh()
		addRing.Refresh()
		plusImg.Refresh()
	}
	addBtn.spec.OnHover = applyHoverVisuals
	qrBtn.spec.OnHover = applyHoverVisuals
	pasteBtn.spec.OnHover = applyHoverVisuals
	overlay := newConnectionCardOverlay(nil, applyHoverVisuals)

	closeBtn := newIconChromeButton(iconChromeButtonSpec{
		NormalFill:   color.Transparent,
		HoverFill:    design.ColorSurfaceLight,
		Stroke:       color.Transparent,
		CornerRadius: 3,
		NormalIcon:   scriptFooterCloseIcon,
		HoverIcon:    scriptFooterCloseHoverIcon,
		IconSize:     fyne.NewSize(14, 14),
		ButtonSize:   fyne.NewSize(24, 24),
		OnTapped:     actions.OnDismiss,
	})
	closeSlot := container.NewBorder(
		NewInsetExact(container.NewHBox(layout.NewSpacer(), closeBtn), 0, 8, 8, 0),
		nil, nil, nil,
	)

	return container.NewStack(overlay, cardBg, content, dashedBorder, closeSlot)
}
