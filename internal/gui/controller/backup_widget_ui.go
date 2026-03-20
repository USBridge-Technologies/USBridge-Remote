package controller

import (
	"usbridge-client/internal/gui/i18n"
	"usbridge-client/internal/gui/view"
	"usbridge-client/internal/models"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"
	"github.com/sirupsen/logrus"
)

// createInterface создает интерфейс виджета
func (bw *BackupWidget) createInterface() {
	bw.ui = view.NewBackupWidgetUI(
		func() int {
			count := len(bw.snapshots)
			if bw.currentFlash != nil {
				count++
			}
			return count
		},
		view.NewBackupRowTemplate,
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			borderContainer, ok := obj.(*fyne.Container)
			if !ok {
				logrus.Warnf("⚠️ backup row has unexpected type %T", obj)
				return
			}
			if bw.currentFlash != nil && id == 0 {
				bw.renderCurrentFlash(borderContainer)
				return
			}

			snapshotIndex := id
			if bw.currentFlash != nil {
				snapshotIndex = id - 1
			}
			if snapshotIndex < len(bw.snapshots) {
				bw.renderSnapshot(borderContainer, snapshotIndex, bw.snapshots[snapshotIndex])
			}
		},
	)
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
