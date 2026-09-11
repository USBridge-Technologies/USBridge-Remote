package view

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// BackupWidgetUI is the Snapshots tab: a connections-style header + table
// (see NewSnapshotsSection) above the shared app footer (busy spinner
// on the left, build version on the right). Refresh() rebuilds the
// section from the controller's onRebuild callback; the footer persists.
type BackupWidgetUI struct {
	Container   *fyne.Container
	StatusLabel *widget.Label
	BusySpinner *DeviceDashboardBusySpinner

	body            *fyne.Container
	footerHost      *fyne.Container
	scriptFooter    *ScriptFooterStatus
	connectingHint  *DeviceDashboardBusySpinner
	onRebuild       func()
}

func NewBackupWidgetUI() *BackupWidgetUI {
	spinner := NewDeviceDashboardBusyHint("connecting device")
	body := container.NewMax()
	footerHost := container.NewMax(NewAppFooter(AppVersion(), nil, spinner))
	return &BackupWidgetUI{
		Container:   NewEdgeStack(nil, footerHost, body),
		body:        body,
		footerHost:  footerHost,
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

func (ui *BackupWidgetUI) rebuildFooter() {
	if ui == nil || ui.footerHost == nil {
		return
	}
	var extra []fyne.CanvasObject
	if ui.connectingHint != nil {
		extra = append(extra, ui.connectingHint)
	}
	if ui.scriptFooter != nil {
		extra = append(extra, ui.scriptFooter)
	}
	ui.footerHost.Objects = []fyne.CanvasObject{NewAppFooter(AppVersion(), nil, ui.BusySpinner, extra...)}
	ui.footerHost.Refresh()
}

func (ui *BackupWidgetUI) SetScriptFooter(chip *ScriptFooterStatus) {
	if ui == nil {
		return
	}
	ui.scriptFooter = chip
	ui.rebuildFooter()
}

func (ui *BackupWidgetUI) SetConnectingHint(hint *DeviceDashboardBusySpinner) {
	if ui == nil {
		return
	}
	ui.connectingHint = hint
	ui.rebuildFooter()
}

func (ui *BackupWidgetUI) Refresh() {
	if ui != nil && ui.onRebuild != nil {
		ui.onRebuild()
	}
}
