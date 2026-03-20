package view

import (
	"usbridge-client/internal/gui/i18n"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

type BackupWidgetUI struct {
	Container     *fyne.Container
	SnapshotsList *widget.List
	StatusLabel   *widget.Label
}

func NewBackupWidgetUI(
	listLength func() int,
	createItem func() fyne.CanvasObject,
	updateItem func(id widget.ListItemID, obj fyne.CanvasObject),
) *BackupWidgetUI {
	snapshotsList := widget.NewList(listLength, createItem, updateItem)
	statusLabel := widget.NewLabel(i18n.Current.ReadyToWork)

	headerLabel := widget.NewRichTextFromMarkdown("## " + i18n.Current.BackupFlash)
	subtitleLabel := widget.NewLabel(i18n.Current.CurrentFlashAndSnapshots)
	subtitleLabel.TextStyle.Italic = true
	headerContainer := container.NewVBox(headerLabel, subtitleLabel)

	return &BackupWidgetUI{
		Container:     container.NewBorder(headerContainer, nil, nil, nil, snapshotsList),
		SnapshotsList: snapshotsList,
		StatusLabel:   statusLabel,
	}
}

func NewBackupRowTemplate() fyne.CanvasObject {
	statusLabel := widget.NewLabel("⭕")
	sizeLabel := widget.NewLabel(i18n.Current.SnapshotRowTemplateSize)
	dateLabel := widget.NewLabel(i18n.Current.SnapshotRowTemplateDate)
	dateLabel.Alignment = fyne.TextAlignTrailing

	infoBtn := widget.NewButton("ℹ️", nil)
	infoBtn.Importance = widget.LowImportance
	mountBtn := widget.NewButton("🔌", nil)
	mountBtn.Importance = widget.MediumImportance

	leftContainer := container.NewHBox(statusLabel, sizeLabel)
	rightContainer := container.NewHBox(dateLabel, infoBtn, mountBtn)

	return container.NewBorder(nil, nil, leftContainer, rightContainer, nil)
}
