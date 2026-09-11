package view

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// BackupWidgetUI is the Snapshots tab: a connections-style header + table
// (see NewSnapshotsSection). Refresh() rebuilds that section from the
// controller's onRebuild callback.
type BackupWidgetUI struct {
	Container   *fyne.Container
	StatusLabel *widget.Label

	onRebuild func()
}

func NewBackupWidgetUI() *BackupWidgetUI {
	return &BackupWidgetUI{
		Container:   container.NewMax(),
		StatusLabel: widget.NewLabel(""),
	}
}

func (ui *BackupWidgetUI) SetOnRebuild(fn func()) {
	if ui == nil {
		return
	}
	ui.onRebuild = fn
}

func (ui *BackupWidgetUI) SetSection(section fyne.CanvasObject) {
	if ui == nil || ui.Container == nil {
		return
	}
	if section == nil {
		ui.Container.Objects = nil
	} else {
		ui.Container.Objects = []fyne.CanvasObject{section}
	}
	ui.Container.Refresh()
}

func (ui *BackupWidgetUI) Refresh() {
	if ui != nil && ui.onRebuild != nil {
		ui.onRebuild()
	}
}
