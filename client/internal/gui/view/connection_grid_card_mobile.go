package view

import (
	"image/color"
	"strings"

	"usbridge-client/internal/gui/design"
	"usbridge-client/internal/gui/i18n"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
)

// newMobileConnectionGridCard is the phone Grid card: platform plaque sits
// after the name, Edit joins LAN + Connect on the bottom row, and vertical
// padding is tighter than the desktop card. Edit-mode chrome stays the
// desktop one for now (inline fields + delete/cancel/save).
func newMobileConnectionGridCard(data ConnectionCardData, state ConnectionRowState, actions ConnectionCardActions) fyne.CanvasObject {
	isAgent, isKVM := ClassifyConnectionRemoteOS(data.RemoteOS)
	accent := design.ColorAccent
	if isAgent {
		accent = design.ColorConnectionBadgeText
	}

	statusIndicator := newConnectionCardStatusIndicator(data.RemoteOS)
	typeBadge := newConnectionTypeBadge(isAgent, isKVM, accent)
	nameText := NewBrandText(strings.TrimSpace(data.Name), 12, design.ColorTextLight, true)
	platformChip := newConnectionPlatformChip(mobileConnectionPlatformLabel(data, isAgent, isKVM))

	nameCluster := NewInsetExact(
		container.New(&DeviceRowControlsLayout{Gap: 6}, statusIndicator, nameText),
		6, 0, 0, 0,
	)
	leftTop := container.New(&DeviceRowControlsLayout{Gap: 16}, nameCluster, platformChip)
	topRow := container.NewBorder(nil, nil, leftTop, typeBadge)

	statsBox := newMobileConnectionCardStatsBox(data.LANAddress, data.TailscaleAddress)

	dividerColor := color.NRGBA{R: 0x29, G: 0x2d, B: 0x27, A: 0xff}
	dividerLine := canvas.NewRectangle(dividerColor)
	dividerLine.SetMinSize(fyne.NewSize(1, 1))
	divider := NewInsetExact(dividerLine, 0, 0, 4, 4)

	editIcon := fyne.NewStaticResource("connection-edit-mobile.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="#c5c8b5"><path d="M3 17.25V21h3.75L17.81 9.94l-3.75-3.75L3 17.25zm2.92 2.33H5v-.92l9.06-9.06.92.92L5.92 19.58zM20.71 7.04a1.003 1.003 0 0 0 0-1.42L18.37 3.29a1.003 1.003 0 0 0-1.42 0l-1.13 1.13 3.75 3.75 1.14-1.13z"/></svg>`))
	const mobileCardBtnH float32 = 30
	editBtn := newIconChromeButton(iconChromeButtonSpec{
		NormalFill:   color.Transparent,
		HoverFill:    design.ColorGray900,
		DisabledFill: connectionActionBlockedFill,
		Stroke:       design.ColorTailscaleChipBorder,
		StrokeWidth:  1,
		CornerRadius: 5,
		HoverStroke:  design.ColorConnectionBadgeText,
		NormalIcon:   editIcon,
		HoverIcon:    editIcon,
		IconSize:     fyne.NewSize(14, 14),
		ButtonSize:   fyne.NewSize(mobileCardBtnH, mobileCardBtnH),
		OnTapped:     actions.OnEdit,
	})
	editBtn.SetDisabled(state.Disabled)

	protocolDropdown := NewHeaderDropdown(data.ProtocolOptions, data.ProtocolBadge, actions.OnProtocolChange)
	protocolDropdown.UltraCompact = true
	protocolDropdown.CornerRadius = 6
	protocolDropdown.BorderColor = design.ColorTailscaleChipBorder
	protocolDropdown.TextColor = design.ColorConnectionBadgeText
	protocolDropdown.IconColor = color.NRGBA{R: 0xc5, G: 0xc8, B: 0xb5, A: 0xff}
	protocolDropdown.TextSize = 10
	protocolDropdown.HoverBorderColor = design.ColorConnectionBadgeText
	protocolDropdown.HoverFillColor = design.ColorGray900
	protocolDropdown.SetSelected(data.ProtocolBadge)
	protocolDropdown.SetDisabled(state.Disabled)

	connectColor := color.NRGBA{R: 0xc4, G: 0xe7, B: 0x7a, A: 0xff}
	connectHover := color.NRGBA{R: 0xd4, G: 0xf7, B: 0x8a, A: 0xff}
	connectBtn := newIconChromeButton(iconChromeButtonSpec{
		NormalFill:         connectColor,
		HoverFill:          connectHover,
		DisabledFill:       connectionActionBlockedFill,
		MuteDisabledVisual: true,
		Stroke:             color.Transparent,
		LabelColor:         color.NRGBA{R: 0x4c, G: 0x68, B: 0x03, A: 0xff},
		LabelBold:          true,
		CornerRadius:       6,
		NormalIcon:         deviceDashboardConnectIconSVG,
		IconSize:           fyne.NewSize(14, 14),
		ButtonSize:         fyne.NewSize(0, mobileCardBtnH),
		OnTapped:           actions.OnUse,
		LoadingFill:        connectLoadingFill,
		LoadingIcon:        deviceDashboardConnectIconSVG,
		LoadingLabelColor:  color.Black,
	})
	connectBtn.SetText(i18n.Current.ConnectButton)
	connectBtn.SetDisabled(state.Disabled)
	connectBtn.SetLoading(state.Loading)

	bottomRow := container.New(&mobileCardBottomRowLayout{}, editBtn, protocolDropdown, connectBtn)

	content := NewInsetExact(
		container.New(&tightStatsVBoxLayout{Gap: 6}, topRow, statsBox, divider, bottomRow),
		12, 12, 10, 10,
	)

	cardBg := canvas.NewRectangle(design.ColorGray900)
	cardBg.CornerRadius = design.RadiusLG
	cardBg.StrokeColor = design.ColorTailscaleChipBorder
	cardBg.StrokeWidth = 1

	card := container.NewStack(cardBg, content)

	setCardHovered := func(hovered bool) {
		if hovered {
			cardBg.StrokeColor = design.ColorConnectionBadgeText
		} else {
			cardBg.StrokeColor = design.ColorTailscaleChipBorder
		}
		cardBg.Refresh()
	}
	editBtn.spec.OnHover = setCardHovered
	protocolDropdown.OnHover = setCardHovered
	connectBtn.spec.OnHover = setCardHovered

	overlay := newConnectionCardOverlay(actions.OnSelect, setCardHovered)
	return container.NewStack(overlay, card)
}

func mobileConnectionPlatformLabel(data ConnectionCardData, isAgent, isKVM bool) string {
	if label := strings.TrimSpace(data.PlatformLabel); label != "" {
		return label
	}
	switch {
	case isKVM:
		return "Radxa"
	case isAgent:
		return "Opensource/Pro"
	default:
		return i18n.Current.AwaitingConnection
	}
}

func newMobileConnectionCardStatsBox(lanAddress, tailscaleAddress string) fyne.CanvasObject {
	tsValueColor := color.NRGBA{R: 0xeb, G: 0xff, B: 0xbc, A: 0xff}
	lanRow := newConnectionStatRow("LAN", connectionCardAddressOrNone(lanAddress), design.ColorTextLight)
	tsRow := newConnectionStatRow("TS", connectionCardAddressOrNone(tailscaleAddress), tsValueColor)

	dividerColor := color.NRGBA{R: 0x29, G: 0x2d, B: 0x27, A: 0xff}
	sep := canvas.NewRectangle(dividerColor)
	sep.SetMinSize(fyne.NewSize(1, 1))

	bg := canvas.NewRectangle(design.ColorGray950)
	bg.CornerRadius = 6
	bg.StrokeColor = design.ColorTailscaleChipBorder
	bg.StrokeWidth = 1

	return container.NewStack(bg, NewInsetExact(container.New(&tightStatsVBoxLayout{Gap: 4}, lanRow, sep, tsRow), 12, 12, 6, 6))
}

// mobileCardBottomRowLayout pins Edit on the left and packs the protocol
// picker + a compact Connect on the right, so Connect stays near its
// natural width instead of stretching across the card.
type mobileCardBottomRowLayout struct{}

func (l *mobileCardBottomRowLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	if len(objects) < 3 {
		return fyne.NewSize(0, 30)
	}
	const gap float32 = 12
	w := objects[0].MinSize().Width + gap + objects[1].MinSize().Width + gap + objects[2].MinSize().Width
	return fyne.NewSize(w, 30)
}

func (l *mobileCardBottomRowLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) < 3 {
		return
	}
	const gap float32 = 12
	edit, proto, connect := objects[0], objects[1], objects[2]
	editW := edit.MinSize().Width
	protoW := proto.MinSize().Width
	connectW := connect.MinSize().Width

	edit.Resize(fyne.NewSize(editW, size.Height))
	edit.Move(fyne.NewPos(0, 0))

	connect.Resize(fyne.NewSize(connectW, size.Height))
	connect.Move(fyne.NewPos(size.Width-connectW, 0))

	proto.Resize(fyne.NewSize(protoW, size.Height))
	proto.Move(fyne.NewPos(size.Width-connectW-gap-protoW, 0))
}
