package view

import (
	"fmt"
	"image/color"

	"usbridge-client/internal/gui/design"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
)

const snapshotsMobileSubtitle = "Restore points. Mount without changing the original."

func newMobileSnapshotsSection(data SnapshotsSectionData) fyne.CanvasObject {
	header := newMobileSnapshotsHeader(data)
	top := header
	if data.Banner != nil {
		top = container.NewVBox(header, NewMobileFillWidth(data.Banner))
	}
	var body fyne.CanvasObject
	if data.Banner != nil {
		body = canvas.NewRectangle(color.Transparent)
	} else {
		body = newMobileSnapshotsList(data.Rows)
	}
	padded := NewInsetExact(body, connectionsMobileSideMargin, connectionsMobileSideMargin, 8, 12)
	scroll := container.NewVScroll(container.New(&snapshotsBodyTopLayout{}, NewMobileFillWidth(padded)))
	return NewMobileFillWidth(container.NewBorder(top, nil, nil, nil, scroll))
}

func newMobileSnapshotsHeader(data SnapshotsSectionData) fyne.CanvasObject {
	title := NewBrandText("Snapshots", 14, design.ColorConnectionsSectionTitle, true)
	titleGap := canvas.NewRectangle(color.Transparent)
	titleGap.SetMinSize(fyne.NewSize(6, 1))
	badge := newConnectionSortBadge(
		fmt.Sprintf("%d Snapshots", data.SnapshotCount),
		design.ColorConnectionBadgeText,
		false,
		nil,
	)
	titleRow := container.NewHBox(container.NewCenter(title), titleGap, container.NewCenter(badge))

	subtitle := canvas.NewText(snapshotsMobileSubtitle, design.ColorConnectionsSectionSubtitle)
	subtitle.TextSize = 9

	left := container.New(&tightStatsVBoxLayout{Gap: 1},
		titleRow,
		NewMobileFillWidth(subtitle),
	)

	mountBtn := newSnapshotsMountButton(mobileSnapshotsMountData(data))
	row := container.New(&mobileHeaderRowLayout{}, left, mountBtn)

	accentLine := canvas.NewRectangle(design.ColorConnectionsSectionUnderline)
	accentLine.SetMinSize(fyne.NewSize(1, 0.5))
	underlineRightGap := canvas.NewRectangle(color.Transparent)
	underlineRightGap.SetMinSize(fyne.NewSize(connectionsHeaderUnderlineRightPullback, 1))
	underline := container.NewBorder(nil, nil, nil, underlineRightGap, accentLine)

	content := container.NewBorder(NewInsetExact(row, 0, 0, 3, 6), underline, nil, nil)
	return NewInsetExact(content, connectionsMobileSideMargin, connectionsMobileSideMargin, 6, 0)
}

func mobileSnapshotsMountData(data SnapshotsSectionData) SnapshotsSectionData {
	out := data
	if out.FlashMounted {
		if out.MountLabel == "" {
			out.MountLabel = "Disconnect"
		}
		return out
	}
	out.MountLabel = "Mount"
	return out
}

func newMobileSnapshotsList(rows []SnapshotTableRow) fyne.CanvasObject {
	dividerColor := color.NRGBA{R: 0x29, G: 0x2d, B: 0x27, A: 0xff}
	header := newMobileListHeaderSnapshots()
	children := []fyne.CanvasObject{header}
	if len(rows) == 0 {
		sep := canvas.NewRectangle(dividerColor)
		sep.SetMinSize(fyne.NewSize(1, 1))
		empty := canvas.NewText("No snapshots yet", design.ColorConnectionsSectionSubtitle)
		empty.TextSize = 11
		children = append(children, NewInsetExact(sep, 0, 0, 8, 8), empty)
	} else {
		for _, row := range rows {
			sep := canvas.NewRectangle(dividerColor)
			sep.SetMinSize(fyne.NewSize(1, 1))
			children = append(children, NewInsetExact(sep, 0, 0, 8, 8), newMobileSnapshotListRow(row))
		}
	}

	bg := canvas.NewRectangle(design.ColorGray900)
	bg.CornerRadius = design.RadiusLG
	bg.StrokeColor = design.ColorTailscaleChipBorder
	bg.StrokeWidth = 1
	table := container.NewStack(bg, NewInsetExact(
		container.New(&tightStatsVBoxLayout{Gap: 0}, children...),
		10, 10, 6, 6,
	))
	return NewMobileFillWidth(table)
}

func newMobileSnapshotListRow(row SnapshotTableRow) fyne.CanvasObject {
	name := newSnapshotListNameCell(row.Title, row.Mounted)
	size := newSnapshotListSizeCell(row.Size)
	state := newSnapshotListStateCell(row.Mounted)
	left := container.New(&tightStatsVBoxLayout{Gap: 2},
		name,
		container.New(&DeviceRowControlsLayout{Gap: 8}, size, state),
	)
	actions := newSnapshotListActionsCell(row)
	return container.New(&mobileListRowLayout{gap: 8}, left, actions)
}

func newMobileListHeaderSnapshots() fyne.CanvasObject {
	left := canvas.NewText("NAME / SIZE", design.ColorConnectionsSectionSubtitle)
	left.TextSize = 8
	left.TextStyle.Monospace = true
	right := canvas.NewText("ACTION", design.ColorConnectionsSectionSubtitle)
	right.TextSize = 8
	right.TextStyle.Monospace = true
	right.Alignment = fyne.TextAlignTrailing
	return container.New(&mobileListRowLayout{gap: 8}, left, right)
}
