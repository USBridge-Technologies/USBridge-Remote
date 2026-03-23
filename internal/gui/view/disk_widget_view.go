package view

import (
	"usbridge-client/internal/gui/i18n"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

type DiskWidgetUI struct {
	Container   *fyne.Container
	DevicesList *widget.List
}

func NewDiskWidgetUI(
	listLength func() int,
	createItem func() fyne.CanvasObject,
	updateItem func(id widget.ListItemID, obj fyne.CanvasObject),
) *DiskWidgetUI {
	devicesList := widget.NewList(listLength, createItem, updateItem)

	headerLabel := widget.NewRichTextFromMarkdown("## " + i18n.Current.Devices)
	subtitleLabel := widget.NewLabel(i18n.Current.AllAvailableDevices)
	subtitleLabel.TextStyle.Italic = true
	headerContainer := container.NewVBox(headerLabel, subtitleLabel)

	return &DiskWidgetUI{
		Container:   container.NewBorder(headerContainer, nil, nil, nil, devicesList),
		DevicesList: devicesList,
	}
}

func NewDiskRowTemplate() fyne.CanvasObject {
	checkbox := widget.NewCheck("", nil)
	nameLabel := widget.NewLabel(i18n.Current.DeviceRowTemplateName)
	nameLabel.Wrapping = fyne.TextWrapOff

	statusLabel := widget.NewLabel(i18n.Current.DeviceRowTemplateStatus)
	statusLabel.Alignment = fyne.TextAlignTrailing

	roRwBtn := widget.NewButton("RO", nil)
	roRwBtn.Hide()

	uploadBtn := widget.NewButton("⬆️", nil)
	uploadBtn.Hide()

	deleteBtn := widget.NewButton("🗑️", nil)
	deleteBtn.Hide()

	settingsBtn := widget.NewButton("⚙", nil)
	settingsBtn.Hide()

	modeRowIconText := canvas.NewText("🖱️", theme.Color(theme.ColorNameForeground))
	modeRowIconText.TextSize = theme.TextSize()
	modeRowIconText.Hide()
	modeTitleLabel := widget.NewLabel("Mouse")
	modeTitleLabel.Hide()
	modeSelect := widget.NewSelect([]string{i18n.Current.DeviceMouse, i18n.Current.DeviceTouch, i18n.Current.DeviceAbsolute}, nil)
	modeSelect.Hide()

	checkboxContainer := container.NewPadded(checkbox)
	checkboxContainer.Resize(fyne.NewSize(50, 50))

	modeLabelWrap := container.NewHBox(modeRowIconText, modeTitleLabel)
	centerContainer := container.NewStack(nameLabel, modeLabelWrap)
	rightContainer := container.NewHBox(roRwBtn, modeSelect, uploadBtn, deleteBtn, settingsBtn, statusLabel)

	return container.NewBorder(nil, nil, checkboxContainer, rightContainer, centerContainer)
}
