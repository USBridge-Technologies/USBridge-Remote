package view

import (
	"usbridge-client/internal/gui/i18n"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

type ConnectionManagerUI struct {
	Container         *fyne.Container
	ConnectionsScroll *container.Scroll
	ConnectionsBox    *fyne.Container
	QRBtn             *widget.Button
	AddBtn            *widget.Button
}

type ConnectionRowData struct {
	Name          string
	Host          string
	ProtocolBadge string
	EditLabel     string
}

type ConnectionRowActions struct {
	OnSelect       func()
	OnUse          func()
	OnEdit         func()
	OnDelete       func()
	OnProtocolMenu func(*widget.Button)
}

func NewConnectionManagerUI(onLanguageMenu func(*widget.Button), onQR func(), onAdd func()) *ConnectionManagerUI {
	savedLabel := widget.NewLabelWithStyle(i18n.Current.SavedConnections, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})

	langBtn := widget.NewButton("🌐", nil)
	langBtn.Importance = widget.MediumImportance
	langBtn.OnTapped = func() {
		onLanguageMenu(langBtn)
	}

	qrBtn := widget.NewButton(i18n.Current.QRScannerButton, onQR)
	qrBtn.Importance = widget.MediumImportance

	addBtn := widget.NewButton("➕ "+i18n.Current.AddConnectionTitle, onAdd)
	addBtn.Importance = widget.MediumImportance

	header := container.NewBorder(nil, nil, savedLabel, langBtn)
	connectionsBox := container.NewVBox()
	connectionsScroll := container.NewScroll(connectionsBox)
	connectionsScroll.SetMinSize(fyne.NewSize(0, 220))

	actionBar := container.NewHBox(qrBtn, addBtn)
	mainContent := container.NewPadded(
		container.NewBorder(header, actionBar, nil, nil, connectionsScroll),
	)

	return &ConnectionManagerUI{
		Container:         container.NewPadded(mainContent),
		ConnectionsScroll: connectionsScroll,
		ConnectionsBox:    connectionsBox,
		QRBtn:             qrBtn,
		AddBtn:            addBtn,
	}
}

func (ui *ConnectionManagerUI) SetEmptyState() {
	ui.ConnectionsBox.RemoveAll()
	emptyLabel := widget.NewLabel(i18n.Current.NoSavedConnections)
	emptyLabel.Alignment = fyne.TextAlignCenter
	ui.ConnectionsBox.Add(container.NewCenter(emptyLabel))
	ui.ConnectionsBox.Refresh()
}

func (ui *ConnectionManagerUI) SetRows(rows []*fyne.Container) {
	ui.ConnectionsBox.RemoveAll()
	for _, row := range rows {
		ui.ConnectionsBox.Add(row)
	}
	ui.ConnectionsBox.Refresh()
}

func NewConnectionRow(data ConnectionRowData, actions ConnectionRowActions) *fyne.Container {
	nameText := canvas.NewText(data.Name, theme.Color(theme.ColorNameForeground))
	nameText.TextSize = theme.TextSubHeadingSize()
	nameText.TextStyle.Bold = true

	nameSelectBtn := widget.NewButton("", actions.OnSelect)
	nameSelectBtn.Importance = widget.LowImportance
	nameRow := container.NewStack(nameText, container.NewMax(nameSelectBtn))

	hostLabel := widget.NewLabel(data.Host)
	hostLabel.TextStyle.Italic = true
	hostSelectBtn := widget.NewButton("", actions.OnSelect)
	hostSelectBtn.Importance = widget.LowImportance
	hostLabelWithClick := container.NewStack(hostLabel, container.NewMax(hostSelectBtn))

	protocolBtn := widget.NewButton(data.ProtocolBadge, nil)
	protocolBtn.Importance = widget.LowImportance
	protocolBtn.OnTapped = func() {
		actions.OnProtocolMenu(protocolBtn)
	}

	useBtn := widget.NewButton("→", actions.OnUse)
	useBtn.Importance = widget.MediumImportance

	editBtn := widget.NewButton(data.EditLabel, actions.OnEdit)
	editBtn.Importance = widget.LowImportance

	deleteBtn := widget.NewButton("🗑️", actions.OnDelete)
	deleteBtn.Importance = widget.LowImportance

	centerSelectBtn := widget.NewButton("", actions.OnSelect)
	centerSelectBtn.Importance = widget.LowImportance
	centerArea := container.NewStack(layout.NewSpacer(), container.NewMax(centerSelectBtn))

	bottomRow := container.NewBorder(nil, nil,
		container.NewHBox(useBtn, protocolBtn, hostLabelWithClick),
		container.NewHBox(editBtn, deleteBtn),
		centerArea,
	)

	card := widget.NewCard("", "", container.NewVBox(
		container.NewPadded(nameRow),
		bottomRow,
	))
	return container.NewPadded(card)
}
