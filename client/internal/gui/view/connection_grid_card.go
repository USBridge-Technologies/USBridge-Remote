package view

// connection_grid_card.go -- the Grid-mode connection card (see the
// connections section header's Grid/List toggle), modeled on a reference
// screenshot: dot+name+edit on top, a platform/capability chip row, a dark
// LAN/TS box, a divider, and a bottom row of protocol picker + Connect
// button.
//
// First pass -- colors/sizes are placeholders pending review, same as every
// other "let's sketch it" first draft this screen has gone through so far.
// A few fields from the original reference (an online/health-check status
// pill, a console/terminal quick-access button) were cut before this ever
// shipped -- see git history on this file if either comes back later.

import (
	"image/color"
	"strings"

	"usbridge-client/internal/gui/assets"
	"usbridge-client/internal/gui/design"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
)

// ConnectionCardData is what one grid card needs to render. RemoteOS reuses
// the same value/classification ConnectionRowData.RemoteOS and
// ClassifyConnectionRemoteOS already use -- KVM vs Agent isn't a separate
// concept here, it only changes the accent color (KVM: ColorAccent, Agent:
// ColorConnectionBadgeText -- both placeholders pending review).
type ConnectionCardData struct {
	Name     string
	RemoteOS string

	// PlatformLabel/CapabilityText: the chip row under the title
	// ("Radxa" * some capability note in the reference). Neither has a real
	// data source yet -- PlatformLabel falls back to the literal "Radxa"
	// (the only platform that exists right now) when empty; CapabilityText
	// just hides that half of the row when empty, nothing to guess at there.
	PlatformLabel  string
	CapabilityText string

	LANAddress       string
	TailscaleAddress string

	ProtocolBadge   string
	ProtocolOptions []string
}

// ConnectionCardActions are the events a grid card can report.
type ConnectionCardActions struct {
	OnSelect         func()
	OnEdit           func()
	OnUse            func()
	OnProtocolChange func(string)
}

// connectionCardWidth/Height are the card's target size -- "roughly square,
// 3-4 per screen row" per the brief, though the reference screenshot's
// cards actually read closer to a 6:5 rectangle than a true square. The
// caller arranges N of these in a container.NewGridWrap(fyne.NewSize(
// connectionCardWidth, connectionCardHeight), ...) (or similar) to actually
// get that many per row.
const (
	connectionCardWidth  float32 = 255
	connectionCardHeight float32 = 205
)

// NewConnectionGridCard builds one Grid-mode connection card.
func NewConnectionGridCard(data ConnectionCardData, state ConnectionRowState, actions ConnectionCardActions) fyne.CanvasObject {
	isAgent, _ := ClassifyConnectionRemoteOS(data.RemoteOS)
	accent := design.ColorAccent // KVM and the unclassified fallback
	if isAgent {
		accent = design.ColorConnectionBadgeText
	}

	dot := canvas.NewCircle(accent)
	dotWrap := container.NewGridWrap(fyne.NewSize(8, 8), dot)

	nameText := NewBrandText(strings.TrimSpace(data.Name), 12, design.ColorTextLight, true)

	editIcon := fyne.NewStaticResource("connection-edit-title.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="#e0e3e7"><path d="M3 17.25V21h3.75L17.81 9.94l-3.75-3.75L3 17.25zm2.92 2.33H5v-.92l9.06-9.06.92.92L5.92 19.58zM20.71 7.04a1.003 1.003 0 0 0 0-1.42L18.37 3.29a1.003 1.003 0 0 0-1.42 0l-1.13 1.13 3.75 3.75 1.14-1.13z"/></svg>`))
	editBtn := newIconChromeButton(iconChromeButtonSpec{
		NormalFill: color.Transparent,
		HoverFill:  design.ColorSurfaceLight,
		Stroke:     color.Transparent,
		NormalIcon: editIcon,
		IconSize:   fyne.NewSize(11, 11),
		ButtonSize: fyne.NewSize(20, 20),
		OnTapped:   actions.OnEdit,
	})

	topRow := container.New(&DeviceRowControlsLayout{Gap: 8}, dotWrap, nameText, editBtn)

	platformLabel := strings.TrimSpace(data.PlatformLabel)
	if platformLabel == "" {
		platformLabel = "Radxa"
	}
	chipsRow := NewInset(newConnectionCardChipsRow(platformLabel, strings.TrimSpace(data.CapabilityText), accent), 0, 0, 0, 8)

	statsBox := newConnectionCardStatsBox(data.LANAddress, data.TailscaleAddress)

	dividerLine := canvas.NewRectangle(design.ColorTailscaleChipBorder)
	dividerLine.SetMinSize(fyne.NewSize(1, 1))
	divider := NewInset(dividerLine, 0, 0, 4, 4)

	protocolDropdown := NewHeaderDropdown(data.ProtocolOptions, data.ProtocolBadge, actions.OnProtocolChange)
	protocolDropdown.UltraCompact = true
	protocolDropdown.CornerRadius = 6
	protocolDropdown.BorderColor = design.ColorTailscaleChipBorder
	protocolDropdown.TextColor = design.ColorConnectionBadgeText
	protocolDropdown.TextSize = 10
	protocolDropdown.HoverBorderColor = design.ColorConnectionBadgeText
	protocolDropdown.HoverFillColor = design.ColorGray900
	protocolDropdown.SetSelected(data.ProtocolBadge)
	protocolDropdown.SetDisabled(state.Disabled)

	connectColor := color.NRGBA{R: 0xc4, G: 0xe7, B: 0x7a, A: 0xff}
	connectHover := color.NRGBA{R: 0xb4, G: 0xd7, B: 0x6a, A: 0xff}

	connectBtn := newIconChromeButton(iconChromeButtonSpec{
		NormalFill:   connectColor,
		HoverFill:    connectHover,
		DisabledFill: connectionActionBlockedFill,
		Stroke:       color.Transparent,
		LabelColor:   design.ColorBackground,
		LabelBold:    true,
		CornerRadius: 6,
		NormalIcon:   assets.ConnectIconBoldBlack,
		IconSize:     fyne.NewSize(14, 14),
		ButtonSize:   fyne.NewSize(0, 26),
		OnTapped:     actions.OnUse,
	})
	connectBtn.SetText("Connect")
	connectBtn.SetDisabled(state.Disabled)
	connectBtn.SetLoading(state.Loading)

	bottomRow := container.New(&gridBottomRowLayout{}, protocolDropdown, connectBtn)

	content := NewInset(container.NewVBox(
		topRow,
		chipsRow,
		statsBox,
		divider,
		bottomRow,
	), 12, 12, 12, 8)

	cardBg := canvas.NewRectangle(design.ColorGray900)
	cardBg.CornerRadius = design.RadiusLG
	cardBg.StrokeColor = design.ColorTailscaleChipBorder
	cardBg.StrokeWidth = 1
	cardBg.SetMinSize(fyne.NewSize(connectionCardWidth, connectionCardHeight))

	card := container.NewStack(cardBg, content)
	if actions.OnSelect == nil {
		return card
	}
	// transparentTapOverlay underneath, not on top: on top would swallow
	// clicks meant for the dropdown/Connect button stacked above it.
	return container.NewStack(newTransparentTapOverlay(actions.OnSelect), card)
}

func newConnectionCardChipsRow(platformLabel, capabilityText string, accent color.Color) fyne.CanvasObject {
	items := []fyne.CanvasObject{newConnectionPlatformChip(platformLabel)}
	if capabilityText != "" {
		bullet := canvas.NewText("•", design.ColorTailscaleChipBorder)
		bullet.TextSize = 8
		capText := canvas.NewText(capabilityText, accent)
		capText.TextSize = 8
		items = append(items, bullet, capText)
	}
	return container.New(&DeviceRowControlsLayout{Gap: 8}, items...)
}

type tightPlatformChipLayout struct{}

func (l *tightPlatformChipLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	if len(objects) < 2 {
		return fyne.NewSize(0, 0)
	}
	labelSize := objects[1].MinSize()
	return fyne.NewSize(labelSize.Width+12, 14) // hardcode tight 14px height
}

func (l *tightPlatformChipLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) < 2 {
		return
	}
	objects[0].Resize(size)
	objects[0].Move(fyne.NewPos(0, 0))

	labelSize := objects[1].MinSize()
	// Visually center the text vertically, ignoring its large bounding box
	objects[1].Resize(labelSize)
	objects[1].Move(fyne.NewPos(6, (size.Height-labelSize.Height)/2))
}

func newConnectionPlatformChip(text string) fyne.CanvasObject {
	label := canvas.NewText(text, design.ColorTextMuted)
	label.TextSize = 8
	label.TextStyle.Monospace = true

	bg := canvas.NewRectangle(color.Transparent)
	bg.CornerRadius = 3
	bg.StrokeColor = design.ColorTailscaleChipBorder
	bg.StrokeWidth = 1

	chip := container.New(&tightPlatformChipLayout{}, bg, label)
	return container.NewCenter(chip)
}

// newConnectionCardStatsBox is the dark LAN/TS readout.
func newConnectionCardStatsBox(lanAddress, tailscaleAddress string) fyne.CanvasObject {
	lanRow := newConnectionStatRow("LAN", connectionCardAddressOrNone(lanAddress))
	tsRow := newConnectionStatRow("TS", connectionCardAddressOrNone(tailscaleAddress))
	
	sep := canvas.NewRectangle(design.ColorTailscaleChipBorder)
	sep.SetMinSize(fyne.NewSize(1, 1))

	rows := []fyne.CanvasObject{lanRow, sep, tsRow}

	bg := canvas.NewRectangle(design.ColorGray950)
	bg.CornerRadius = 6
	bg.StrokeColor = design.ColorTailscaleChipBorder
	bg.StrokeWidth = 1

	return container.NewStack(bg, NewInset(container.NewVBox(rows...), 12, 12, 8, 8))
}

func newConnectionStatRow(label, value string) fyne.CanvasObject {
	labelText := canvas.NewText(label, design.ColorTextMuted)
	labelText.TextSize = 10
	labelText.TextStyle.Monospace = true

	valueText := canvas.NewText(value, design.ColorTextLight)
	valueText.TextSize = 10
	valueText.TextStyle.Monospace = true
	valueText.Alignment = fyne.TextAlignTrailing

	return container.NewBorder(nil, nil, labelText, nil, valueText)
}

func connectionCardAddressOrNone(address string) string {
	address = strings.TrimSpace(address)
	if address == "" {
		return "none"
	}
	return address
}

type gridBottomRowLayout struct{}

func (l *gridBottomRowLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	if len(objects) < 2 {
		return fyne.NewSize(0, 0)
	}
	return fyne.NewSize(objects[0].MinSize().Width+4+objects[1].MinSize().Width, 28)
}

func (l *gridBottomRowLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) < 2 {
		return
	}
	left := objects[0]
	right := objects[1]
	
	leftW := left.MinSize().Width
	left.Resize(fyne.NewSize(leftW, size.Height))
	left.Move(fyne.NewPos(0, 0))
	
	rightW := size.Width - leftW - 4
	if rightW < 0 {
		rightW = 0
	}
	right.Resize(fyne.NewSize(rightW, size.Height))
	right.Move(fyne.NewPos(leftW+4, 0))
}
