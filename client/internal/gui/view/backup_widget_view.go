package view

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// BackupWidgetUI is the Snapshots tab: a connections-style header + table
// (see NewSnapshotsSection) above the same Devices footer (busy spinner
// on the left, build version on the right). Refresh() rebuilds the
// section from the controller's onRebuild callback; the footer persists.
type BackupWidgetUI struct {
	Container   *fyne.Container
	StatusLabel *widget.Label
	BusySpinner *DeviceDashboardBusySpinner

	body      *fyne.Container
	onRebuild func()
}

func NewBackupWidgetUI() *BackupWidgetUI {
	spinner := NewDeviceDashboardBusySpinner()
	body := container.NewMax()
	footer := NewDeviceDashboardFooter(AppVersion(), nil, spinner)
	return &BackupWidgetUI{
		Container:   container.NewBorder(nil, footer, nil, nil, body),
		body:        body,
		BusySpinner: spinner,
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
	if ui == nil || ui.body == nil {
		return
	}
	if section == nil {
		ui.body.Objects = nil
	} else {
		ui.body.Objects = []fyne.CanvasObject{section}
	}
	ui.body.Refresh()
}

func (ui *BackupWidgetUI) SetBusy(busy bool) {
	if ui == nil || ui.BusySpinner == nil {
		return
	}
	if busy {
		ui.BusySpinner.Start()
		return
	}
	ui.BusySpinner.Stop()
}

func (ui *BackupWidgetUI) Refresh() {
	if ui != nil && ui.onRebuild != nil {
		ui.onRebuild()
	}
}
