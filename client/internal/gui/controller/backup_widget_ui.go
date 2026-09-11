package controller

import (
	"time"

	"usbridge-client/internal/gui/i18n"
	"usbridge-client/internal/gui/view"
)

func (bw *BackupWidget) createInterface() {
	bw.ui = view.NewBackupWidgetUI()
	bw.ui.SetOnRebuild(func() {
		bw.ui.SetSection(view.NewSnapshotsSection(bw.snapshotsSectionData()))
	})
	bw.ui.Refresh()
}

func (bw *BackupWidget) snapshotsSectionData() view.SnapshotsSectionData {
	if !isUSBridgeAgentOS(bw.agentOS) && bw.usbClient != nil {
		return view.SnapshotsSectionData{
			SnapshotCount: 0,
			MountLabel:    "Mount backup flash",
			MountEnabled:  false,
			OnMount:       bw.currentFlashAction(),
			Promo:         view.NewEmptyStatePromoCard(bw.openHardwarePromo),
		}
	}

	mounting := bw.isMounting.Load()
	data := view.SnapshotsSectionData{
		SnapshotCount: len(bw.snapshots),
		MountLabel:    "Mount backup flash",
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
			Subtitle:       snap.Name,
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
