package controller

import (
	"time"

	"usbridge-client/internal/gui/i18n"
	"usbridge-client/internal/gui/view"
)

func (bw *BackupWidget) createInterface() {
	bw.firmwarePromoDismissed = bw.firmwarePromoDismissedPref()
	bw.firmwareBanner = view.NewFirmwarePromoBanner()
	bw.firmwareBanner.SetOnDismiss(bw.dismissFirmwarePromo)
	bw.firmwareBanner.SetOnTrial(bw.openFirmwarePromo)
	bw.firmwareChip = view.NewFooterHardwareChip("Hardware Agent")
	bw.firmwareChip.SetOnOpen(bw.openFirmwarePromo)
	bw.firmwareChip.SetOnRestore(bw.restoreFirmwarePromo)
	bw.ui = view.NewBackupWidgetUI()
	bw.ui.SetFirmwareChip(bw.firmwareChip)
	bw.ui.SetOnRebuild(func() {
		bw.ui.SetSection(view.NewSnapshotsSection(bw.snapshotsSectionData()))
		bw.syncFirmwareChip()
	})
	bw.ui.Refresh()
}

func (bw *BackupWidget) snapshotsSectionData() view.SnapshotsSectionData {
	if !isUSBridgeAgentOS(bw.agentOS) && bw.usbClient != nil {
		if bw.ui != nil {
			bw.ui.SetBusy(false)
		}
		data := view.SnapshotsSectionData{
			SnapshotCount: 0,
			MountLabel:    i18n.Current.SnapshotsMountBackupFlash,
			MountEnabled:  false,
			MountInactive: true,
		}
		if !bw.firmwarePromoDismissed && bw.firmwareBanner != nil {
			bw.firmwareBanner.Show()
			data.Banner = bw.firmwareBanner
		} else if bw.firmwareBanner != nil {
			bw.firmwareBanner.Hide()
		}
		return data
	}
	if bw.firmwareBanner != nil {
		bw.firmwareBanner.Hide()
	}

	mounting := bw.isMounting.Load()
	if bw.ui != nil {
		bw.ui.SetBusy(mounting)
	}
	data := view.SnapshotsSectionData{
		SnapshotCount: len(bw.snapshots),
		MountLabel:    i18n.Current.SnapshotsMountBackupFlash,
		MountEnabled:  bw.currentFlash != nil && !mounting,
		MountLoading:  mounting && !bw.currentFlashConnected,
		FlashMounted:  bw.currentFlashConnected,
		OnMount:       bw.currentFlashAction(),
	}
	if bw.currentFlashConnected {
		data.MountLabel = i18n.Current.DisconnectButton
		data.MountEnabled = !mounting
		data.MountLoading = mounting
	}

	rows := make([]view.SnapshotTableRow, 0, len(bw.snapshots))
	for _, snapshot := range bw.snapshots {
		snap := snapshot
		title := snap.CreatedAt.In(time.Local).Format("02 Jan 2006, 15:04")
		if title == "" {
			title = snap.Name
		}
		rows = append(rows, view.SnapshotTableRow{
			Title:          title,
			Size:           snap.DisplaySize(),
			Mounted:        snap.Connected,
			OnInfo:         func() { bw.showSnapshotDetails(snap) },
			OnConnect:      func() { bw.handleMountSnapshot(snap) },
			OnDisconnect:   func() { bw.handleUnmountSnapshot(snap) },
			ConnectEnabled: !mounting,
			ConnectLoading: mounting,
		})
	}
	data.Rows = rows
	return data
}

func (bw *BackupWidget) currentFlashAction() func() {
	if bw.currentFlashConnected {
		return bw.handleDisconnectCurrentFlash
	}
	return bw.handleMountCurrentFlash
}
