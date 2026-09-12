package view

import (
	"fmt"
	"image/color"
	"strings"

	"usbridge-client/internal/gui/design"
	"usbridge-client/internal/gui/i18n"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
)

// connectionsMobileSideMargin is the phone Connections inset — slightly
// tighter than the desktop header/card margin so a 360dp frame still has
// room for a full-width card.
const connectionsMobileSideMargin float32 = 14

// UseMobileConnections is the fork for the Connections screen: real
// phones, or the desktop Mobile preview. Desktop Grid/List stay on the
// original path; mobile gets its own layout that we grow separately
// (same widgets where they still fit, new ones where they don't).
//
// Reused for now: grid cards, Agent/KVM badges, firmware promo (compact
// renderer), footer chips, Add/QR/paste dialogs.
// Own for now: section header (title + badges + round "+"), single-column body.
// Likely rewrite later: cards themselves, promo, add flow, footer.
func UseMobileConnections() bool {
	return IsMobile()
}

func connectionsContentSideMargin() float32 {
	if UseMobileConnections() {
		return connectionsMobileSideMargin
	}
	return connectionsHeaderSideMargin
}

// applyMobileConnectionsContent is the mobile Connections body: a single
// column of the same connection cards, stretched to the phone width.
// Grid wrap and the six-column List table stay desktop-only. Mobile List
// is stacked rows (connection_list_table_mobile.go). The dashed add tile
// is the empty-state helper only (see refreshConnectionsList).
func (ui *ConnectionManagerUI) applyMobileConnectionsContent() {
	if ui.viewMode != "grid" {
		ui.ConnectionsBox.Add(newMobileConnectionsList(ui.lastRows, ui.addActions, ui.editIndex, ui.editPanel))
		return
	}
	if len(ui.lastCards) == 0 {
		return
	}
	rows := make([]fyne.CanvasObject, 0, len(ui.lastCards))
	for _, card := range ui.lastCards {
		rows = append(rows, container.New(&mobileFillWidthLayout{}, card))
	}
	ui.ConnectionsBox.Add(container.New(&tightStatsVBoxLayout{Gap: connectionCardGridGap}, rows...))
}

// newMobileConnectionsHeader is the phone section bar: title + Agent/KVM
// badges + subtitle on the left, round Add on the right. Grid/List lives
// in the app-header settings menu, not here.
func newMobileConnectionsHeader(summary ConnectionsSummary, actions connectionsHeaderActions, activeSort string) (fyne.CanvasObject, *connectionsHeaderButtons) {
	title := NewBrandText(strings.TrimSpace(i18n.Current.SavedConnections), 14, design.ColorConnectionsSectionTitle, true)

	toggleSort := func(kind string) func() {
		return func() {
			next := kind
			if activeSort == kind {
				next = ""
			}
			if actions.OnSortToggle != nil {
				actions.OnSortToggle(next)
			}
		}
	}

	titleGap := canvas.NewRectangle(color.Transparent)
	titleGap.SetMinSize(fyne.NewSize(6, 1))
	titleItems := []fyne.CanvasObject{container.NewCenter(title), titleGap}
	if summary.AgentCount > 0 || alwaysShowConnectionsBadges {
		titleItems = append(titleItems, container.NewCenter(newConnectionSortBadge(
			fmt.Sprintf("%d Agent", summary.AgentCount), design.ColorConnectionBadgeText,
			activeSort == "agent", toggleSort("agent"))))
	}
	if summary.KVMCount > 0 || alwaysShowConnectionsBadges {
		titleItems = append(titleItems, container.NewCenter(newConnectionSortBadge(
			fmt.Sprintf("%d KVM", summary.KVMCount), design.ColorConnectionAddFill,
			activeSort == "kvm", toggleSort("kvm"))))
	}
	titleRow := container.NewHBox(titleItems...)

	subtitle := canvas.NewText(i18n.Current.ConnectionsHeaderSubtitle, design.ColorConnectionsSectionSubtitle)
	subtitle.TextSize = 9

	left := container.New(&tightStatsVBoxLayout{Gap: 1},
		titleRow,
		container.New(&mobileFillWidthLayout{}, subtitle),
	)

	plusSVG := `<svg viewBox="0 0 24 24" fill="#4c6803"><path d="M19 13h-6v6h-2v-6H5v-2h6V5h2v6h6v2z"/></svg>`
	plusIcon := fyne.NewStaticResource("add-mobile.svg", []byte(plusSVG))
	const addSize float32 = 32
	addBtn := newIconChromeButton(iconChromeButtonSpec{
		NormalFill:   design.ColorConnectionAddFill,
		HoverFill:    design.ColorConnectionAddFillHover,
		DisabledFill: connectionActionBlockedFill,
		Stroke:       color.Transparent,
		NormalIcon:   plusIcon,
		HoverIcon:    plusIcon,
		IconSize:     fyne.NewSize(18, 18),
		ButtonSize:   fyne.NewSize(addSize, addSize),
		CornerRadius: addSize / 2,
		OnTapped:     actions.OnAdd,
	})

	row := container.New(&mobileHeaderRowLayout{}, left, addBtn)

	accentLine := canvas.NewRectangle(design.ColorConnectionsSectionUnderline)
	accentLine.SetMinSize(fyne.NewSize(1, 0.5))
	underlineRightGap := canvas.NewRectangle(color.Transparent)
	underlineRightGap.SetMinSize(fyne.NewSize(connectionsHeaderUnderlineRightPullback, 1))
	underline := container.NewBorder(nil, nil, nil, underlineRightGap, accentLine)

	content := container.NewBorder(NewInsetExact(row, 0, 0, 3, 6), underline, nil, nil)
	return NewInsetExact(content, connectionsMobileSideMargin, connectionsMobileSideMargin, 6, 0), &connectionsHeaderButtons{add: addBtn}
}

// NewMobileFillWidth stretches one child to the available width and
// reports MinSize.Width=1 so a wide desktop table/card cannot grow the
// phone preview window.
func NewMobileFillWidth(obj fyne.CanvasObject) fyne.CanvasObject {
	return container.New(&mobileFillWidthLayout{}, obj)
}

// mobileFillWidthLayout stretches one child to the available width so
// desktop-sized cards (fixed 280 MinSize) fill a phone column without
// forcing the window itself to 280+margins.
type mobileFillWidthLayout struct{}

func (l *mobileFillWidthLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	height := float32(0)
	for _, obj := range objects {
		if h := obj.MinSize().Height; h > height {
			height = h
		}
	}
	return fyne.NewSize(1, height)
}

func (l *mobileFillWidthLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	for _, obj := range objects {
		obj.Resize(size)
		obj.Move(fyne.NewPos(0, 0))
	}
}

// mobileHeaderRowLayout keeps the section header's height on the title
// block so a slightly larger "+" can sit over the right padding without
// stretching the bar. The button is inset from the right edge so it
// reads a bit left of the card column.
type mobileHeaderRowLayout struct{}

func (l *mobileHeaderRowLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	if len(objects) < 2 {
		return fyne.NewSize(0, 0)
	}
	left := objects[0].MinSize()
	add := objects[1].MinSize()
	return fyne.NewSize(left.Width+add.Width+mobileHeaderAddRightInset, left.Height)
}

const mobileHeaderAddRightInset float32 = 8

func (l *mobileHeaderRowLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) < 2 {
		return
	}
	left, add := objects[0], objects[1]
	addMin := add.MinSize()
	add.Resize(addMin)
	addY := (size.Height - addMin.Height) / 2
	add.Move(fyne.NewPos(size.Width-addMin.Width-mobileHeaderAddRightInset, addY))
	leftW := size.Width - addMin.Width - mobileHeaderAddRightInset - 8
	if leftW < 0 {
		leftW = 0
	}
	left.Resize(fyne.NewSize(leftW, size.Height))
	left.Move(fyne.NewPos(0, 0))
}
