package view

// snapshots_table.go -- Snapshots tab laid out like the Connections list
// table: a section header (title + count badge + Mount backup flash, the
// same lime Add-connection chrome) above one shared card (column titles,
// hairline row dividers, Connect in ACTIONS).

import (
	"fmt"
	"image/color"

	"usbridge-client/internal/gui/assets"
	"usbridge-client/internal/gui/design"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
)

const snapshotsHeaderSubtitle = "Immutable restore points of your data. Mount one without changing the original."

var (
	snapshotListColumnLabels = []string{"NAME", "SIZE", "", "STATE", "ACTIONS"}
	// NAME+SIZE stay packed on the left (NAME is fixed, not flex) so SIZE
	// sits right after the date/name instead of riding against STATE.
	// The empty flex column absorbs leftover width before STATE/ACTIONS.
	snapshotListColumnWidths = []float32{210, 128, 0, 90, 150}
	snapshotSizeTextColor    = color.NRGBA{R: 0xe9, G: 0xfd, B: 0xbb, A: 0xff}
)

// SnapshotTableRow is one restore-point row in NewSnapshotsListTable.
type SnapshotTableRow struct {
	Title          string
	Size           string
	Mounted        bool
	OnInfo         func()
	OnConnect      func()
	OnDisconnect   func()
	ConnectEnabled bool
	ConnectLoading bool
}

// SnapshotsSectionData is the Snapshots tab's header + table (or promo).
type SnapshotsSectionData struct {
	SnapshotCount int
	MountLabel    string
	MountEnabled  bool
	MountLoading  bool
	FlashMounted  bool
	// MountInactive is the software-agent treatment: the + button stays
	// visible but gray and unclickable (backup flash lives on the board).
	MountInactive bool
	OnMount       func()
	Rows          []SnapshotTableRow
	// Banner, when set, sits under the header -- the same USBridge
	// Firmware strip Connections uses, shown on a software agent.
	Banner fyne.CanvasObject
}

// NewSnapshotsSection builds the Snapshots tab: connections-style header
// pinned above a scrolling table (or promo card).
func NewSnapshotsSection(data SnapshotsSectionData) fyne.CanvasObject {
	header := newSnapshotsHeader(data)
	top := header
	if data.Banner != nil {
		top = container.NewVBox(header, data.Banner)
	}
	var body fyne.CanvasObject
	if data.Banner != nil {
		body = canvas.NewRectangle(color.Transparent)
	} else {
		body = NewSnapshotsListTable(data.Rows)
	}
	// Top-align the card so it stays as tall as its rows (not stretched to
	// the window). VScroll then only shows a thumb once the table is taller
	// than the leftover viewport -- same idea as Devices' storage list.
	padded := NewInsetExact(body, connectionsHeaderSideMargin, connectionsHeaderSideMargin+10, 8, 12)
	scroll := container.NewVScroll(container.New(&snapshotsBodyTopLayout{}, padded))
	return container.NewBorder(top, nil, nil, nil, scroll)
}

// snapshotsBodyTopLayout gives its child the child's own MinSize height and
// parks it at the top. Extra height from a parent VScroll stays empty tab
// background instead of stretching the table card.
type snapshotsBodyTopLayout struct{}

func (l *snapshotsBodyTopLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	if len(objects) == 0 || objects[0] == nil {
		return fyne.NewSize(0, 0)
	}
	return objects[0].MinSize()
}

func (l *snapshotsBodyTopLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) == 0 || objects[0] == nil {
		return
	}
	child := objects[0]
	min := child.MinSize()
	child.Move(fyne.NewPos(0, 0))
	child.Resize(fyne.NewSize(size.Width, min.Height))
}

func newSnapshotsHeader(data SnapshotsSectionData) fyne.CanvasObject {
	title := NewBrandText("Snapshots", 18, design.ColorConnectionsSectionTitle, true)
	titleGap := canvas.NewRectangle(color.Transparent)
	titleGap.SetMinSize(fyne.NewSize(10, 1))
	badge := newConnectionSortBadge(
		fmt.Sprintf("%d Snapshots", data.SnapshotCount),
		design.ColorConnectionBadgeText,
		false,
		nil,
	)
	titleRow := container.NewHBox(container.NewCenter(title), titleGap, container.NewCenter(badge))

	subtitle := canvas.NewText(snapshotsHeaderSubtitle, design.ColorConnectionsSectionSubtitle)
	subtitle.TextSize = 10
	left := container.NewVBox(titleRow, subtitle)

	mountBtn := newSnapshotsMountButton(data)
	right := container.NewCenter(mountBtn)
	row := container.NewHBox(container.NewCenter(left), layout.NewSpacer(), right)

	accentLine := canvas.NewRectangle(design.ColorConnectionsSectionUnderline)
	accentLine.SetMinSize(fyne.NewSize(1, 0.5))
	underlineRightGap := canvas.NewRectangle(color.Transparent)
	underlineRightGap.SetMinSize(fyne.NewSize(connectionsHeaderUnderlineRightPullback, 1))
	underline := container.NewBorder(nil, nil, nil, underlineRightGap, accentLine)

	content := container.NewBorder(NewInset(row, 0, 0, 4, 8), underline, nil, nil)
	return NewInset(content, connectionsHeaderSideMargin, connectionsHeaderSideMargin, 8, 0)
}

func newSnapshotsMountButton(data SnapshotsSectionData) *iconChromeButton {
	plusSVG := `<svg viewBox="0 0 24 24" fill="#4c6803"><path d="M19 13h-6v6h-2v-6H5v-2h6V5h2v6h6v2z"/></svg>`
	plusIcon := fyne.NewStaticResource("snapshots-mount.svg", []byte(plusSVG))
	plusIdle := fyne.NewStaticResource("snapshots-mount-idle.svg", []byte(
		`<svg viewBox="0 0 24 24" fill="#8f9381"><path d="M19 13h-6v6h-2v-6H5v-2h6V5h2v6h6v2z"/></svg>`))

	label := data.MountLabel
	if label == "" {
		label = "Mount backup flash"
	}

	spec := iconChromeButtonSpec{
		NormalFill:         design.ColorConnectionAddFill,
		HoverFill:          design.ColorConnectionAddFillHover,
		DisabledFill:       connectionActionBlockedFill,
		Stroke:             color.Transparent,
		LabelColor:         color.NRGBA{R: 0x4c, G: 0x68, B: 0x03, A: 0xff},
		LabelSize:          10,
		LabelBold:          true,
		NormalIcon:         plusIcon,
		HoverIcon:          plusIcon,
		IconSize:           fyne.NewSize(18, 18),
		ButtonSize:         fyne.NewSize(0, headerAddButtonHeight),
		OnTapped:           data.OnMount,
		LoadingFill:        connectLoadingFill,
		LoadingIcon:        plusIcon,
		LoadingLabelColor:  color.Black,
		MuteDisabledVisual: true,
	}
	if data.MountInactive {
		spec.NormalFill = design.ColorSurfaceLight
		spec.HoverFill = design.ColorSurfaceLight
		spec.DisabledFill = design.ColorSurfaceLight
		spec.LabelColor = design.ColorConnectionsSectionMutedText
		spec.NormalIcon = plusIdle
		spec.HoverIcon = plusIdle
		spec.DisabledIcon = plusIdle
		spec.MuteDisabledVisual = false
		spec.OnTapped = nil
	}
	if data.FlashMounted {
		spec.NormalFill = color.Transparent
		spec.HoverFill = design.ColorSurfaceLight
		spec.Stroke = design.ColorTailscaleChipBorder
		spec.StrokeWidth = 1
		spec.CornerRadius = 6
		spec.LabelColor = design.ColorTextLight
		spec.NormalIcon = assets.PowerOffFillRoundIcon
		spec.HoverIcon = assets.PowerOffFillRoundIcon
		spec.IconSize = fyne.NewSize(12, 12)
		spec.LoadingIcon = assets.PowerOffFillRoundIconBlack
		spec.LoadingLabelColor = color.Black
	}

	btn := newIconChromeButton(spec)
	btn.SetText(label)
	btn.SetDisabled(!data.MountEnabled)
	btn.SetLoading(data.MountLoading)
	return btn
}

// NewSnapshotsListTable is the connections-list card: header row, then one
// row per snapshot, hairline dividers, dark rounded chrome.
func NewSnapshotsListTable(rows []SnapshotTableRow) fyne.CanvasObject {
	labels, widths := snapshotListColumnLabels, snapshotListColumnWidths

	dividerColor := color.NRGBA{R: 0x29, G: 0x2d, B: 0x27, A: 0xff}
	newDivider := func() fyne.CanvasObject {
		sep := canvas.NewRectangle(dividerColor)
		sep.SetMinSize(fyne.NewSize(1, 1))
		return NewInset(sep, 0, 0, 2, 2)
	}

	header := newConnectionListHeaderRow(labels, widths)
	children := []fyne.CanvasObject{header}
	if len(rows) == 0 {
		children = append(children, newDivider(), newSnapshotsEmptyRow(widths))
	} else {
		for _, row := range rows {
			children = append(children, newDivider(), newSnapshotListRow(row, widths))
		}
	}
	rowsCol := container.New(&tightStatsVBoxLayout{Gap: 0}, children...)

	bg := canvas.NewRectangle(design.ColorGray900)
	bg.CornerRadius = design.RadiusLG
	bg.StrokeColor = design.ColorTailscaleChipBorder
	bg.StrokeWidth = 1

	return container.NewStack(bg, NewInset(rowsCol, 16, 16, 12, 12))
}

func newSnapshotsEmptyRow(widths []float32) fyne.CanvasObject {
	label := canvas.NewText("No snapshots yet", design.ColorConnectionsSectionSubtitle)
	label.TextSize = 11
	empty := canvas.NewRectangle(color.Transparent)
	return container.New(&connectionsTableRowLayout{Widths: widths, Gap: connectionListColumnGap},
		label, empty, empty, empty, empty)
}

func newSnapshotListRow(row SnapshotTableRow, widths []float32) fyne.CanvasObject {
	nameCell := newSnapshotListNameCell(row.Title, row.Mounted)
	sizeCell := newSnapshotListSizeCell(row.Size)
	gap := canvas.NewRectangle(color.Transparent)
	stateCell := container.NewCenter(newSnapshotListStateCell(row.Mounted))
	actionsCell := container.NewBorder(nil, nil, nil, newSnapshotListActionsCell(row))
	return container.New(&connectionsTableRowLayout{Widths: widths, Gap: connectionListColumnGap},
		nameCell, sizeCell, gap, stateCell, actionsCell)
}

func newSnapshotListNameCell(title string, mounted bool) fyne.CanvasObject {
	titleColor := color.Color(design.ColorTextLight)
	if mounted {
		titleColor = DeviceDashboardAccentLime
	}
	return NewBrandText(title, 11, titleColor, true)
}

func newSnapshotListSizeCell(size string) fyne.CanvasObject {
	t := canvas.NewText(size, snapshotSizeTextColor)
	t.TextSize = 8
	t.TextStyle.Monospace = true
	return t
}

func newSnapshotListStateCell(mounted bool) fyne.CanvasObject {
	text := "Available"
	accent := color.Color(design.ColorConnectionsSectionSubtitle)
	if mounted {
		text = "Mounted"
		accent = design.ColorConnectionAddFill
	}
	dot := canvas.NewCircle(accent)
	label := canvas.NewText(text, accent)
	label.TextSize = 9
	label.TextStyle.Monospace = true
	bg := canvas.NewRectangle(color.Transparent)
	bg.CornerRadius = 3
	bg.StrokeColor = design.ColorTailscaleChipBorder
	bg.StrokeWidth = 1
	chip := container.New(&typeBadgeLayout{}, bg, dot, label)
	return container.NewCenter(chip)
}

func newSnapshotListActionsCell(row SnapshotTableRow) fyne.CanvasObject {
	infoBtn := newIconChromeButton(iconChromeButtonSpec{
		NormalFill:   color.Transparent,
		HoverFill:    design.ColorSurfaceLight,
		DisabledFill: connectionActionBlockedFill,
		Stroke:       design.ColorTailscaleChipBorder,
		StrokeWidth:  1,
		CornerRadius: 6,
		NormalIcon:   assets.InfoIcon,
		HoverIcon:    assets.InfoIcon,
		IconSize:     fyne.NewSize(11, 11),
		ButtonSize:   fyne.NewSize(23, 23),
		OnTapped:     row.OnInfo,
	})

	if row.Mounted {
		disconnectBtn := newIconChromeButton(iconChromeButtonSpec{
			NormalFill:         color.Transparent,
			HoverFill:          deviceDashboardDisconnectHoverFill,
			DisabledFill:       deviceDashboardDisabledFill,
			Stroke:             design.ColorTailscaleChipBorder,
			HoverStroke:        deviceDashboardDisconnectHoverStroke,
			StrokeWidth:        1,
			CornerRadius:       6,
			NormalIcon:         deviceDashboardDisconnectIconSVG,
			HoverIcon:          deviceDashboardDisconnectHoverIconSVG,
			DisabledIcon:       assets.ConnectIconBoldBlack,
			IconSize:           fyne.NewSize(11, 11),
			ButtonSize:         fyne.NewSize(23, 23),
			OnTapped:           row.OnDisconnect,
			LoadingFill:        connectLoadingFill,
			LoadingIcon:        assets.ConnectIconBoldBlack,
			LoadingLabelColor:  color.Black,
			MuteDisabledVisual: true,
		})
		disconnectBtn.SetDisabled(!row.ConnectEnabled)
		disconnectBtn.SetLoading(row.ConnectLoading)
		return container.New(&DeviceRowControlsLayout{Gap: 6}, infoBtn, disconnectBtn)
	}

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
		LabelSize:          10,
		CornerRadius:       6,
		NormalIcon:         deviceDashboardConnectIconSVG,
		IconSize:           fyne.NewSize(10, 10),
		ButtonSize:         fyne.NewSize(0, 23),
		OnTapped:           row.OnConnect,
		LoadingFill:        connectLoadingFill,
		LoadingIcon:        deviceDashboardConnectIconSVG,
		LoadingLabelColor:  color.Black,
	})
	connectBtn.SetText("Connect")
	connectBtn.SetDisabled(!row.ConnectEnabled)
	connectBtn.SetLoading(row.ConnectLoading)

	return container.New(&DeviceRowControlsLayout{Gap: 6}, infoBtn, connectBtn)
}
