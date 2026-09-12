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
	firmwareChip    *FooterPromoChip
	onRebuild       func()
}

func NewBackupWidgetUI() *BackupWidgetUI {
	spinner := NewDeviceDashboardBusyHint("connecting device")
	body := container.NewMax()
	var footerHost *fyne.Container
	var footer fyne.CanvasObject
	var tabBody fyne.CanvasObject = body
	if IsMobile() {
		tabBody = NewMobileFillWidth(body)
	} else {
		footerHost = container.NewMax(NewAppFooter(AppVersion(), nil, spinner))
		footer = footerHost
	}
	return &BackupWidgetUI{
		Container:   NewEdgeStack(nil, footer, tabBody),
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
	if ui == nil || ui.footerHost == nil || IsMobile() {
		return
	}
	var extra []fyne.CanvasObject
	if ui.connectingHint != nil {
		extra = append(extra, ui.connectingHint)
	}
	if ui.scriptFooter != nil {
		extra = append(extra, ui.scriptFooter)
	}
	if ui.firmwareChip != nil {
		extra = append(extra, ui.firmwareChip)
	}
	footer := NewAppFooter(AppVersion(), nil, ui.BusySpinner, extra...)
	if IsMobile() {
		footer = NewMobileFillWidth(footer)
	}
	ui.footerHost.Objects = []fyne.CanvasObject{footer}
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

func (ui *BackupWidgetUI) SetFirmwareChip(chip *FooterPromoChip) {
	if ui == nil {
		return
	}
	ui.firmwareChip = chip
	ui.rebuildFooter()
}

func (ui *BackupWidgetUI) Refresh() {
	if ui != nil && ui.onRebuild != nil {
		ui.onRebuild()
	}
}
