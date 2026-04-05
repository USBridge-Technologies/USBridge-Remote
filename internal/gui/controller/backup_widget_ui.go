package controller

import (
	"fmt"

	"usbridge-client/internal/gui/assets"
	"usbridge-client/internal/gui/i18n"
	"usbridge-client/internal/gui/view"
	"usbridge-client/internal/models"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
	"github.com/sirupsen/logrus"
)

// createInterface создает интерфейс виджета
func (bw *BackupWidget) createInterface() {
	bw.ui = view.NewBackupWidgetUI(
		func() []fyne.CanvasObject {
			rows := make([]fyne.CanvasObject, 0, len(bw.snapshots)+1)

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
					ActionEnabled: true,
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
					ActionTapped:  func() { bw.handleMountSnapshot(0, snap) },
					ActionEnabled: !snap.Connected,
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

// renderCurrentFlash отображает актуальную флешку
func (bw *BackupWidget) renderCurrentFlash(borderContainer *fyne.Container) {
	statusLabel, sizeLabel, dateLabel, infoBtn, mountBtn := bw.resolveRowWidgets(borderContainer)
	if statusLabel == nil || sizeLabel == nil || dateLabel == nil {
		logrus.Warnf("⚠️ Not all UI elements were found in renderCurrentFlash")
		return
	}

	if infoBtn != nil {
		infoBtn.Hide()
	}

	if bw.currentFlashConnected {
		statusLabel.SetText("✅")
		statusLabel.Importance = widget.HighImportance
		statusLabel.TextStyle.Bold = true
		sizeLabel.TextStyle.Bold = true
		dateLabel.TextStyle.Bold = true
	} else {
		statusLabel.SetText("⭕")
		statusLabel.Importance = widget.MediumImportance
		statusLabel.TextStyle.Bold = false
		sizeLabel.TextStyle.Bold = false
		dateLabel.TextStyle.Bold = false
	}
	sizeLabel.SetText(bw.currentFlash.FormatSize())
	dateLabel.SetText(i18n.Current.CurrentFlash)

	if mountBtn != nil {
		mountBtn.OnTapped = func() {
			bw.handleMountCurrentFlash()
		}
	}
}

// renderSnapshot отображает снапшот
func (bw *BackupWidget) renderSnapshot(borderContainer *fyne.Container, id widget.ListItemID, snapshot *models.SnapshotInfo) {
	statusLabel, sizeLabel, dateLabel, infoBtn, mountBtn := bw.resolveRowWidgets(borderContainer)
	if statusLabel == nil || sizeLabel == nil || dateLabel == nil {
		logrus.Warnf("⚠️ Not all UI elements were found in renderSnapshot")
		return
	}

	if infoBtn != nil {
		infoBtn.Show()
		snap := snapshot
		infoBtn.OnTapped = func() {
			bw.showSnapshotDetails(snap)
		}
	}

	sizeLabel.SetText(snapshot.DisplaySize())
	dateLabel.SetText(snapshot.CreatedAt.Format(i18n.Current.DateTimeFormat))

	if snapshot.Connected {
		statusLabel.SetText("✅")
		statusLabel.Importance = widget.HighImportance
		statusLabel.TextStyle.Bold = true
		sizeLabel.TextStyle.Bold = true
		dateLabel.TextStyle.Bold = true
	} else {
		statusLabel.SetText("⭕")
		statusLabel.Importance = widget.MediumImportance
		statusLabel.TextStyle.Bold = false
		sizeLabel.TextStyle.Bold = false
		dateLabel.TextStyle.Bold = false
	}

	if mountBtn != nil {
		mountBtn.OnTapped = func() {
			bw.handleMountSnapshot(id, snapshot)
		}
	}
}

func (bw *BackupWidget) resolveRowWidgets(borderContainer *fyne.Container) (*widget.Label, *widget.Label, *widget.Label, *widget.Button, *widget.Button) {
	var statusLabel, sizeLabel, dateLabel *widget.Label
	var infoBtn, mountBtn *widget.Button

	for _, child := range borderContainer.Objects {
		if innerContainer, ok := child.(*fyne.Container); ok {
			btnIdx := 0
			for _, innerChild := range innerContainer.Objects {
				switch v := innerChild.(type) {
				case *widget.Label:
					if statusLabel == nil {
						statusLabel = v
					} else if sizeLabel == nil {
						sizeLabel = v
					} else if dateLabel == nil {
						dateLabel = v
					}
				case *widget.Button:
					if btnIdx == 0 {
						infoBtn = v
						btnIdx++
					} else {
						mountBtn = v
					}
				}
			}
		}
	}

	return statusLabel, sizeLabel, dateLabel, infoBtn, mountBtn
}
