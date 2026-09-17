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

func newMobileScriptsSection(data ScriptsSectionData) fyne.CanvasObject {
	mcpHeader := newMobileScriptsColumnHeader("MCP", i18n.Current.ScriptsMCPSubtitle, nil, nil)
	autoHeader := newMobileScriptsColumnHeader(
		i18n.Current.ScriptsAutomationTitle,
		i18n.Current.ScriptsAutomationSubtitle,
		newConnectionSortBadge(
			fmt.Sprintf(i18n.Current.ScriptsCountFmt, data.ScriptCount),
			design.ColorConnectionBadgeText,
			false,
			nil,
		),
		newScriptsNewButtons(data),
	)

	mcpCard := NewMobileFillWidth(NewScriptsMCPCard(data.MCP))
	table := NewMobileFillWidth(newMobileScriptsTableBody(data))
	scriptsGap := canvas.NewRectangle(color.Transparent)
	scriptsGap.SetMinSize(fyne.NewSize(0, 18))

	body := container.New(&tightStatsVBoxLayout{Gap: 10},
		mcpHeader,
		mcpCard,
		scriptsGap,
		autoHeader,
		table,
	)
	padded := NewInsetExact(NewMobileFillWidth(body), connectionsMobileSideMargin, connectionsMobileSideMargin, 8, 12)
	scroll := container.NewVScroll(container.New(&snapshotsBodyTopLayout{}, padded))
	return NewMobileFillWidth(scroll)
}

func newMobileScriptsColumnHeader(title, subtitle string, badge, action fyne.CanvasObject) fyne.CanvasObject {
	titleText := NewBrandText(title, 14, design.ColorConnectionsSectionTitle, true)
	titleItems := []fyne.CanvasObject{container.NewCenter(titleText)}
	if badge != nil {
		gap := canvas.NewRectangle(color.Transparent)
		gap.SetMinSize(fyne.NewSize(6, 1))
		titleItems = append(titleItems, gap, container.NewCenter(badge))
	}
	titleRow := container.NewHBox(titleItems...)

	sub := canvas.NewText(subtitle, design.ColorConnectionsSectionSubtitle)
	sub.TextSize = 9
	left := container.New(&tightStatsVBoxLayout{Gap: 1}, titleRow, sub)

	var row fyne.CanvasObject
	if action != nil {
		row = container.New(&mobileHeaderRowLayout{}, left, action)
	} else {
		row = left
	}

	accentLine := canvas.NewRectangle(design.ColorConnectionsSectionUnderline)
	accentLine.SetMinSize(fyne.NewSize(1, 0.5))
	underlineRightGap := canvas.NewRectangle(color.Transparent)
	underlineRightGap.SetMinSize(fyne.NewSize(connectionsHeaderUnderlineRightPullback, 1))
	underline := container.NewBorder(nil, nil, nil, underlineRightGap, accentLine)

	return container.NewBorder(NewInsetExact(row, 0, 0, 3, 6), underline, nil, nil)
}

func newMobileScriptsTableBody(data ScriptsSectionData) fyne.CanvasObject {
	if data.Banner != nil {
		return NewMobileFillWidth(data.Banner)
	}
	if strings.TrimSpace(data.LockedMessage) != "" {
		return newScriptsLockedCard(data.LockedMessage)
	}
	return newMobileScriptsListTable(data.Rows)
}

func newMobileScriptsListTable(rows []ScriptTableRow) fyne.CanvasObject {
	dividerColor := color.NRGBA{R: 0x29, G: 0x2d, B: 0x27, A: 0xff}
	header := newMobileTwoColHeader(
		i18n.Current.ConnectionColName+" / "+i18n.Current.ScriptsColSource,
		i18n.Current.MobileColAction,
	)

	children := []fyne.CanvasObject{header}
	if len(rows) == 0 {
		sep := canvas.NewRectangle(dividerColor)
		sep.SetMinSize(fyne.NewSize(1, 1))
		empty := canvas.NewText(i18n.Current.ScriptsEmpty, design.ColorConnectionsSectionSubtitle)
		empty.TextSize = 11
		children = append(children, NewInsetExact(sep, 0, 0, 8, 8), empty)
	} else {
		for _, row := range rows {
			sep := canvas.NewRectangle(dividerColor)
			sep.SetMinSize(fyne.NewSize(1, 1))
			children = append(children, NewInsetExact(sep, 0, 0, 8, 8), newMobileScriptListRow(row))
		}
	}

	bg := canvas.NewRectangle(design.ColorGray900)
	bg.CornerRadius = design.RadiusLG
	bg.StrokeColor = design.ColorTailscaleChipBorder
	bg.StrokeWidth = 1
	return container.NewStack(bg, NewInsetExact(
		container.New(&tightStatsVBoxLayout{Gap: 0}, children...),
		10, 10, 6, 6,
	))
}

func newMobileScriptListRow(row ScriptTableRow) fyne.CanvasObject {
	nameCell := newScriptsNameCell(row.Name, scriptNameColor(row.Running, row.Error))
	sourceCell := newScriptsSourceChip(row.Source)
	stateChip := newScriptsLiveStateChip(row.Running, row.Error)
	actions := newScriptsActionsCell(row)

	if row.BindStatus != nil {
		row.BindStatus(func(running bool, errStr string) {
			nameCell.SetColor(scriptNameColor(running, errStr))
			stateChip.Set(running, errStr)
			actions.SetActive(running, errStr)
		})
	}

	left := container.New(&tightStatsVBoxLayout{Gap: 2},
		nameCell,
		container.New(&DeviceRowControlsLayout{Gap: 8}, sourceCell, stateChip),
	)
	return container.New(&mobileListRowLayout{gap: 8}, left, actions)
}
