package controller

import (
	"fmt"

	"usbridge-client/internal/gui/assets"
	"usbridge-client/internal/gui/i18n"
	"usbridge-client/internal/gui/view"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
)

// createInterface создает интерфейс виджета
func (bw *BackupWidget) createInterface() {
	bw.ui = view.NewBackupWidgetUI(
		func() []fyne.CanvasObject {
			rows := make([]fyne.CanvasObject, 0, len(bw.snapshots)+1)
			mounting := bw.isMounting.Load()

			if bw.currentFlash != nil {
				subtitle := bw.currentFlash.FormatSize()
				if subtitle == "" {
					subtitle = i18n.Current.CurrentFlash
				} else {
					subtitle = fmt.Sprintf("%s  %s", i18n.Current.CurrentFlash, subtitle)
				}

				rows = append(rows, view.NewBackupListRow(view.BackupListRowSpec{
					Icon:          connectedBackupIcon(bw.currentFlashConnected),
					Title:         i18n.Current.BackupFlashName,
					Subtitle:      subtitle,
					Connected:     bw.currentFlashConnected,
					ActionIcon:    currentFlashActionIcon(bw.currentFlashConnected),
					ActionIconDim: currentFlashActionIconMuted(bw.currentFlashConnected),
					ActionTapped:  bw.currentFlashAction(),
					ActionEnabled: !mounting,
					ActionLoading: mounting,
				}))
			}

			for _, snapshot := range bw.snapshots {
				snap := snapshot
				title := snap.CreatedAt.Format("02 Jan 2006, 15:04")
				subtitle := snap.DisplaySize()
				rows = append(rows, view.NewBackupListRow(view.BackupListRowSpec{
					Icon:          connectedSnapshotIcon(snap.Connected),
					Title:         title,
					Subtitle:      subtitle,
					Connected:     snap.Connected,
					ShowInfo:      true,
					InfoTapped:    func() { bw.showSnapshotDetails(snap) },
					ActionPassive: snap.Connected,
					ActionIcon:    assets.ConnectIcon,
					ActionIconDim: assets.ConnectIconMuted,
					ActionTapped:  func() { bw.handleMountSnapshot(snap) },
					ActionEnabled: !snap.Connected && !mounting,
					ActionLoading: !snap.Connected && mounting,
				}))
			}

			return rows
		},
		func() {
			if bw.window == nil {
				return
			}
			dialog.ShowInformation(i18n.Current.BackupFlashName, i18n.Current.CurrentFlashAndSnapshots, bw.window)
		},
	)
}

func connectedBackupIcon(connected bool) fyne.Resource {
	if connected {
		return assets.SDCardIconActive
	}
	return assets.SDCardIcon
}

func connectedSnapshotIcon(connected bool) fyne.Resource {
	if connected {
		return assets.SnapshotsTabIconActive
	}
	return assets.SnapshotsTabIcon
}

func currentFlashActionIcon(connected bool) fyne.Resource {
	if connected {
		return assets.PowerOffFillRoundIcon
	}
	return assets.ConnectIcon
}

func currentFlashActionIconMuted(connected bool) fyne.Resource {
	if connected {
		return assets.PowerOffFillRoundIconMuted
	}
	return assets.ConnectIconMuted
}

func (bw *BackupWidget) currentFlashAction() func() {
	if bw.currentFlashConnected {
		return bw.handleDisconnectCurrentFlash
	}
	return bw.handleMountCurrentFlash
}
