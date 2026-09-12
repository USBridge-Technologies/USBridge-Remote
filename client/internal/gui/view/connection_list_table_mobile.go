package view

import (
	"image/color"
	"strings"

	"usbridge-client/internal/gui/design"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
)

// newMobileConnectionsList is the phone List table: two columns (Name/Info
// | Action), no platform/state plaques, LAN/TS under the name. Edit
// replaces that row with the editor in place — rows above and below stay
// in the table, unlike the desktop side split.
func newMobileConnectionsList(items []ConnectionListItem, addActions AddConnectionCardActions, editIndex int, editPanel fyne.CanvasObject) fyne.CanvasObject {
	if len(items) == 0 {
		return NewAddConnectionGridCard(addActions)
	}

	header := newMobileListHeader()
	dividerColor := color.NRGBA{R: 0x29, G: 0x2d, B: 0x27, A: 0xff}
	children := []fyne.CanvasObject{header}
	for i, item := range items {
		sep := canvas.NewRectangle(dividerColor)
		sep.SetMinSize(fyne.NewSize(1, 1))
		children = append(children, NewInsetExact(sep, 0, 0, 8, 8))
		if editPanel != nil && i == editIndex {
			children = append(children, container.New(&mobileFillWidthLayout{}, editPanel))
			continue
		}
		children = append(children, newMobileConnectionListRow(item, false))
	}

	bg := canvas.NewRectangle(design.ColorGray900)
	bg.CornerRadius = design.RadiusLG
	bg.StrokeColor = design.ColorTailscaleChipBorder
	bg.StrokeWidth = 1
	table := container.NewStack(bg, NewInsetExact(
		container.New(&tightStatsVBoxLayout{Gap: 0}, children...),
		10, 10, 6, 6,
	))
	return container.New(&mobileFillWidthLayout{}, table)
}

func newMobileListHeader() fyne.CanvasObject {
	left := canvas.NewText("NAME / INFO", design.ColorConnectionsSectionSubtitle)
	left.TextSize = 8
	left.TextStyle.Monospace = true
	right := canvas.NewText("ACTION", design.ColorConnectionsSectionSubtitle)
	right.TextSize = 8
	right.TextStyle.Monospace = true
	right.Alignment = fyne.TextAlignTrailing
	return container.New(&mobileListRowLayout{gap: 8}, left, right)
}

func newMobileConnectionListRow(item ConnectionListItem, highlighted bool) fyne.CanvasObject {
	data := item.Data

	status := newConnectionCardStatusIndicatorSize(data.RemoteOS, 13)
	nameText := NewBrandText(strings.TrimSpace(data.Name), 11, design.ColorTextLight, true)
	nameRow := container.New(&DeviceRowControlsLayout{Gap: 6}, status, nameText)
	info := newMobileListInfoLine(data.LANAddress, data.TailscaleAddress)
	left := container.New(&tightStatsVBoxLayout{Gap: 1}, nameRow, container.New(&mobileFillWidthLayout{}, info))

	const btnH float32 = 26
	editIcon := fyne.NewStaticResource("connection-edit-mobile-list.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="#c5c8b5"><path d="M3 17.25V21h3.75L17.81 9.94l-3.75-3.75L3 17.25zm2.92 2.33H5v-.92l9.06-9.06.92.92L5.92 19.58zM20.71 7.04a1.003 1.003 0 0 0 0-1.42L18.37 3.29a1.003 1.003 0 0 0-1.42 0l-1.13 1.13 3.75 3.75 1.14-1.13z"/></svg>`))
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
		IconSize:     fyne.NewSize(12, 12),
		ButtonSize:   fyne.NewSize(btnH, btnH),
		OnTapped:     item.Actions.OnEdit,
	})
	editBtn.SetDisabled(item.State.Disabled)
	if highlighted {
		editBtn.Hide()
	}

	route := newConnectionListRouteCell(data, item.Actions.OnProtocolChange, item.State)

	connectColor := color.NRGBA{R: 0xc4, G: 0xe7, B: 0x7a, A: 0xff}
	connectHover := color.NRGBA{R: 0xd4, G: 0xf7, B: 0x8a, A: 0xff}
	connectBtn := newIconChromeButton(iconChromeButtonSpec{
		NormalFill:         connectColor,
		HoverFill:          connectHover,
		DisabledFill:       connectionActionBlockedFill,
		MuteDisabledVisual: true,
		Stroke:             color.Transparent,
		CornerRadius:       5,
		NormalIcon:         deviceDashboardConnectIconSVG,
		IconSize:           fyne.NewSize(13, 13),
		ButtonSize:         fyne.NewSize(btnH, btnH),
		OnTapped:           item.Actions.OnUse,
		LoadingFill:        connectLoadingFill,
		LoadingIcon:        deviceDashboardConnectIconSVG,
	})
	connectBtn.SetDisabled(item.State.Disabled)
	connectBtn.SetLoading(item.State.Loading)

	actions := container.New(&DeviceRowControlsLayout{Gap: 6}, editBtn, route, connectBtn)
	row := container.New(&mobileListRowLayout{gap: 8}, left, actions)
	if !highlighted {
		return row
	}

	highlightBorder := canvas.NewRectangle(color.Transparent)
	highlightBorder.StrokeColor = design.ColorConnectionBadgeText
	highlightBorder.StrokeWidth = 1
	highlightBorder.CornerRadius = 6
	return container.NewStack(highlightBorder, NewInsetExact(row, 4, 4, 2, 2))
}

func newMobileListInfoLine(lanAddress, tailscaleAddress string) fyne.CanvasObject {
	muted := color.NRGBA{R: 0xc5, G: 0xc8, B: 0xb5, A: 0xff}
	tsColor := color.NRGBA{R: 0xeb, G: 0xff, B: 0xbc, A: 0xff}
	lan := canvas.NewText("LAN "+connectionCardAddressOrNone(lanAddress), muted)
	lan.TextSize = 8
	lan.TextStyle.Monospace = true
	ts := canvas.NewText("TS "+connectionCardAddressOrNone(tailscaleAddress), tsColor)
	ts.TextSize = 8
	ts.TextStyle.Monospace = true
	return container.New(&DeviceRowControlsLayout{Gap: 8}, lan, ts)
}

// mobileListRowLayout is a two-column table row: left fills, right keeps
// its natural width and stays top-aligned (name line + action buttons).
type mobileListRowLayout struct {
	gap float32
}

func (l *mobileListRowLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	if len(objects) < 2 {
		return fyne.NewSize(0, 0)
	}
	left, right := objects[0].MinSize(), objects[1].MinSize()
	h := left.Height
	if right.Height > h {
		h = right.Height
	}
	return fyne.NewSize(left.Width+l.gap+right.Width, h)
}

func (l *mobileListRowLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) < 2 {
		return
	}
	rightMin := objects[1].MinSize()
	objects[1].Resize(rightMin)
	objects[1].Move(fyne.NewPos(size.Width-rightMin.Width, 0))
	leftW := size.Width - rightMin.Width - l.gap
	if leftW < 0 {
		leftW = 0
	}
	objects[0].Resize(fyne.NewSize(leftW, size.Height))
	objects[0].Move(fyne.NewPos(0, 0))
}
