package controller

import (
	"fmt"
	"image/color"
	"strings"
	"time"

	"usbridge-client/internal/gui/design"
	"usbridge-client/internal/gui/i18n"
	"usbridge-client/internal/gui/view"
	"usbridge-client/internal/models"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/sirupsen/logrus"
)

type snapshotInfoButtonsLayout struct {
	gap float32
}

func (l *snapshotInfoButtonsLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) < 2 {
		return
	}

	left := objects[0]
	right := objects[1]
	leftWidth := (size.Width - l.gap) / 2
	if leftWidth < 0 {
		leftWidth = 0
	}
	rightWidth := size.Width - leftWidth - l.gap
	if rightWidth < 0 {
		rightWidth = 0
	}

	left.Move(fyne.NewPos(0, 0))
	left.Resize(fyne.NewSize(leftWidth, size.Height))

	right.Move(fyne.NewPos(leftWidth+l.gap, 0))
	right.Resize(fyne.NewSize(rightWidth, size.Height))
}

func (l *snapshotInfoButtonsLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	if len(objects) < 2 {
		return fyne.NewSize(0, 0)
	}

	leftMin := objects[0].MinSize()
	rightMin := objects[1].MinSize()
	height := maxFloat32(leftMin.Height, rightMin.Height)
	return fyne.NewSize(leftMin.Width+rightMin.Width+l.gap, height)
}

func (bw *BackupWidget) buildBaseDeviceBatch() []models.DeviceStartRequest {
	var requests []models.DeviceStartRequest
	addedKeyboard, addedMouse, addedRndis := false, false, false

	deviceInfo, err := bw.usbClient.GetDeviceInfo()
	if err == nil {
		for _, device := range deviceInfo.Devices {
			if device.Status != "connected" {
				continue
			}

			switch {
			case device.Type == "mtp" && strings.Contains(device.Name, "data") && !strings.Contains(device.ProductName, "snapshot"):
				continue
			case (device.Type == "keyboard" || strings.HasPrefix(device.Type, "keyboard:")) && !addedKeyboard:
				requests = append(requests, newKeyboardStartRequest())
				addedKeyboard = true
			case isMouseDeviceType(device.Type) && !addedMouse:
				mouseType := mouseModeFromDeviceType(device.Type)
				requests = append(requests, newMouseStartRequest(mouseType))
				addedMouse = true
			case (device.Type == "rndis" || strings.HasPrefix(device.Type, "rndis:")) && !addedRndis:
				rndisMode := "auto"
				if strings.HasPrefix(device.Type, "rndis:") {
					rndisMode = strings.TrimPrefix(device.Type, "rndis:")
				}
				requests = append(requests, newRNDISStartRequest(rndisMode))
				addedRndis = true
			case device.Type == "local" && !strings.Contains(device.Name, "data"):
				requests = append(requests, models.DeviceStartRequest{
					Device:   "drive",
					Server:   device.Name,
					ReadOnly: true,
				})
			}
		}
	}
	return requests
}

func (bw *BackupWidget) buildDeviceBatchWithoutCurrentFlash() models.DeviceStartBatchRequest {
	return models.DeviceStartBatchRequest(bw.buildBaseDeviceBatch())
}

// canConnectBackupOrSnapshot checks if backup or snapshot can be connected.
func (bw *BackupWidget) canConnectBackupOrSnapshot() (bool, string) {
	deviceInfo, err := bw.usbClient.GetDeviceInfo()
	if err != nil {
		return true, ""
	}
	connectedCount := 0
	hasBackupOrSnapshot := false
	for _, device := range deviceInfo.Devices {
		if device.Status != "connected" {
			continue
		}
		connectedCount++
		if device.Type == "mtp" && strings.Contains(device.Name, "data") && !strings.Contains(device.ProductName, "snapshot") {
			hasBackupOrSnapshot = true
		}
		if device.Type == "iscsi" || (device.Type == "mtp" && (strings.Contains(device.ProductName, "snapshot") || strings.Contains(device.Name, "snapshot"))) {
			hasBackupOrSnapshot = true
		}
	}
	if connectedCount >= 5 && !hasBackupOrSnapshot {
		return false, i18n.Current.FreeDeviceSlotRequired
	}
	return true, ""
}

// buildDeviceBatchWithMTP builds a batch request saving connected devices and replacing MTP.
func (bw *BackupWidget) buildDeviceBatchWithMTP(mtpServer, mtpProductName string) models.DeviceStartBatchRequest {
	requests := bw.buildBaseDeviceBatch()

	requests = append(requests, models.DeviceStartRequest{
		Device: "mtp",
		Server: mtpServer,
	})

	logrus.Infof("📋 Built batch: %d devices (including MTP %s)", len(requests), mtpServer)
	return models.DeviceStartBatchRequest(requests)
}

// handleMountCurrentFlash handles mounting the current flash drive
func (bw *BackupWidget) handleMountCurrentFlash() {
	if bw.usbClient == nil {
		if bw.window != nil {
			view.ShowErrorDialog(fmt.Errorf("%s", i18n.Current.ErrorNotConnected), bw.window)
		}
		return
	}

	if bw.currentFlash == nil {
		if bw.window != nil {
			view.ShowErrorDialog(fmt.Errorf("%s", i18n.Current.ErrorFlashNotFound), bw.window)
		}
		return
	}

	bw.isMounting.Store(true)
	bw.updateUIAsync(func() { bw.ui.SnapshotsList.Refresh() })

	go func() {
		success := false
		defer func() {
			if !success {
				bw.isMounting.Store(false)
				bw.updateUIAsync(func() { bw.ui.SnapshotsList.Refresh() })
			}
		}()

		if ok, msg := bw.canConnectBackupOrSnapshot(); !ok {
			logrus.Warnf("⚠️ Cannot connect backup flash drive: %s", msg)
			bw.updateStatusAsync(msg)
			if bw.window != nil {
				bw.updateUIAsync(func() {
					view.ShowInfoDialog(i18n.Current.Information, msg, bw.window)
				})
			}
			return
		}

		bw.updateStatusAsync(fmt.Sprintf(i18n.Current.MountingFlash, bw.currentFlash.Name))

		batchRequest := bw.buildDeviceBatchWithMTP("data", "BackupDrive")
		logrus.Infof("🚀 Starting mount of current flash drive as MTP: %s", bw.currentFlash.Name)

		deviceResp, err := bw.usbClient.StartDevicesBatch(batchRequest)
		if err != nil {
			logrus.Errorf("❌ Error mounting current flash drive: %v", err)
			bw.showErrorAsync(fmt.Errorf(i18n.Current.ErrorMountingFlashMsg, err))
			return
		}

		bw.logBatchResponse(deviceResp.Success, deviceResp.Message, deviceResp.Data)
		bw.updateUIAsync(func() {
			bw.ui.StatusLabel.SetText(fmt.Sprintf(i18n.Current.FlashMounted, bw.currentFlash.Name))
		})

		logrus.Infof("✅ Current flash drive %s successfully mounted", bw.currentFlash.Name)
		success = true
		bw.finishMountRefresh()
	}()
}

// handleMountSnapshot handles mounting a snapshot
func (bw *BackupWidget) handleMountSnapshot(snapshot *models.SnapshotInfo) {
	if bw.usbClient == nil {
		if bw.window != nil {
			view.ShowErrorDialog(fmt.Errorf("%s", i18n.Current.ErrorNotConnected), bw.window)
		}
		return
	}

	bw.isMounting.Store(true)
	bw.updateUIAsync(func() { bw.ui.SnapshotsList.Refresh() })

	go func() {
		success := false
		defer func() {
			if !success {
				bw.isMounting.Store(false)
				bw.updateUIAsync(func() { bw.ui.SnapshotsList.Refresh() })
			}
		}()

		if ok, msg := bw.canConnectBackupOrSnapshot(); !ok {
			logrus.Warnf("⚠️ Cannot connect snapshot: %s", msg)
			bw.updateStatusAsync(msg)
			if bw.window != nil {
				bw.updateUIAsync(func() {
					view.ShowInfoDialog(i18n.Current.Information, msg, bw.window)
				})
			}
			return
		}

		bw.updateStatusAsync(fmt.Sprintf(i18n.Current.MountingSnapshot, snapshot.Name))

		batchRequest := bw.buildDeviceBatchWithMTP(snapshot.Name, snapshot.Name)
		logrus.Infof("🚀 Starting mount of snapshot as MTP: %s", snapshot.Name)

		deviceResp, err := bw.usbClient.StartDevicesBatch(batchRequest)
		if err != nil {
			logrus.Errorf("❌ Error mounting snapshot: %v", err)
			bw.showErrorAsync(fmt.Errorf(i18n.Current.ErrorMountingSnapshotMsg, err))
			return
		}

		bw.logBatchResponse(deviceResp.Success, deviceResp.Message, deviceResp.Data)
		bw.updateUIAsync(func() {
			bw.ui.StatusLabel.SetText(fmt.Sprintf(i18n.Current.SnapshotMounted, snapshot.Name))
		})

		logrus.Infof("✅ Snapshot %s successfully mounted", snapshot.Name)
		success = true
		bw.finishMountRefresh()
	}()
}

// showSnapshotDetails shows a dialog with snapshot details
func (bw *BackupWidget) showSnapshotDetails(snapshot *models.SnapshotInfo) {
	if bw.window == nil {
		return
	}

	title := "Snapshot Info"
	dateText := view.NewBrandText(snapshot.CreatedAt.Format(i18n.Current.DateTimeFormat), 14, design.ColorTextLight, false)
	sizeText := view.NewBrandText(snapshot.DisplaySize(), 14, design.ColorTextLight, false)
	dateLabel := strings.TrimSpace(strings.TrimSuffix(i18n.Current.SnapshotDetailsDate, "%s"))
	sizeLabel := strings.TrimSpace(strings.TrimSuffix(i18n.Current.SnapshotDetailsSize, "%s"))
	metaBlock := container.NewVBox(
		view.NewBrandText(dateLabel, 11, design.ColorTextMuted, true),
		dateText,
		view.NewBrandText(sizeLabel, 11, design.ColorTextMuted, true),
		sizeText,
	)

	changelogOpts := &models.ChangelogFormatOptions{
		OpNames: map[string]string{
			"snapshot": i18n.Current.ChangelogOpSnapshot,
			"utimes":   i18n.Current.ChangelogOpUtimes,
			"mkfile":   i18n.Current.ChangelogOpMkfile,
			"rename":   i18n.Current.ChangelogOpRename,
			"truncate": i18n.Current.ChangelogOpTruncate,
			"clone":    i18n.Current.ChangelogOpClone,
			"chown":    i18n.Current.ChangelogOpChown,
			"chmod":    i18n.Current.ChangelogOpChmod,
		},
		TempFileLabel: i18n.Current.SnapshotTempFile,
	}
	changelog := snapshot.FormatChangelog(changelogOpts)
	copyContent := strings.Join([]string{
		title,
		fmt.Sprintf("%s %s", i18n.Current.SnapshotDetailsDate, snapshot.CreatedAt.Format(i18n.Current.DateTimeFormat)),
		fmt.Sprintf("%s %s", i18n.Current.SnapshotDetailsSize, snapshot.DisplaySize()),
		"",
		"LOG",
		changelog,
	}, "\n")

	logRows := make([]fyne.CanvasObject, 0)
	for _, line := range strings.Split(changelog, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		logRows = append(logRows, view.NewBrandText(line, 13, design.ColorTextMuted, false))
	}
	if len(logRows) == 0 {
		logRows = append(logRows, view.NewBrandText(i18n.Current.SnapshotChangelogEmpty, 13, design.ColorBorder, false))
	}

	logContent := container.NewVBox(logRows...)
	scrollContent := view.NewInset(logContent, 0, 14, 0, 12)
	scroll := container.NewScroll(scrollContent)
	scroll.SetMinSize(fyne.NewSize(0, 170))

	logTitle := view.NewBrandText("LOG", 11, design.ColorTextMuted, true)
	logCard := view.NewCompactSurfacePanel(
		view.NewInset(
			container.NewVBox(
				logTitle,
				view.NewInset(scroll, 0, 0, 10, 0),
			),
			12, 12, 12, 12,
		),
		design.ColorGray950,
		design.RadiusMD,
	)

	var popup *widget.PopUp
	closePopup := func() {
		if popup != nil {
			popup.Hide()
		}
	}

	titleText := view.NewBrandText(title, 19, design.ColorTextLight, true)
	titleText.Alignment = fyne.TextAlignCenter
	nameText := view.NewBrandText(snapshot.Name, 13, design.ColorTextMuted, false)
	nameText.Alignment = fyne.TextAlignCenter
	closeBtn := newConnectionDialogIconButton(theme.CancelIcon(), closePopup)
	closeSlot := container.NewGridWrap(fyne.NewSize(28, 28), closeBtn)
	leftSlot := canvas.NewRectangle(color.Transparent)
	leftSlot.SetMinSize(fyne.NewSize(28, 28))
	titleBarContent := container.NewBorder(nil, nil, leftSlot, closeSlot,
		container.NewVBox(
			container.NewCenter(titleText),
			view.NewInset(container.NewCenter(nameText), 0, 0, 2, 0),
		),
	)

	copyBtn := widget.NewButton(i18n.Current.Copy, func() {
		if bw.window != nil && bw.window.Clipboard() != nil {
			bw.window.Clipboard().SetContent(copyContent)
		}
	})
	okBtn := widget.NewButton(i18n.Current.OK, closePopup)
	okBtn.Importance = widget.HighImportance
	buttons := container.New(&snapshotInfoButtonsLayout{gap: 12}, copyBtn, okBtn)

	body := container.NewBorder(
		nil,
		view.NewInset(buttons, 0, 0, 10, 0),
		nil,
		nil,
		container.NewVBox(
			titleBarContent,
			view.NewInset(metaBlock, 0, 0, 16, 14),
			logCard,
		),
	)

	bg := canvas.NewRectangle(design.ColorGray900)
	bg.CornerRadius = design.RadiusMD
	border := canvas.NewRectangle(color.Transparent)
	border.CornerRadius = design.RadiusMD
	border.StrokeColor = design.ColorBorder
	border.StrokeWidth = 1
	panel := container.NewStack(
		bg,
		view.NewInset(body, 18, 18, 16, 16),
		border,
	)

	popup = view.NewOverlayPopup(bw.window, view.OverlayPopupSpec{
		Panel:    panel,
		DimColor: color.NRGBA{R: 0x00, G: 0x00, B: 0x00, A: 0x72},
		PanelSize: func(canvasSize fyne.Size, panel fyne.CanvasObject) fyne.Size {
			margin := float32(24)
			maxWidth := canvasSize.Width - margin*2
			maxHeight := canvasSize.Height - margin*2
			if maxWidth <= 0 {
				maxWidth = canvasSize.Width
			}
			if maxHeight <= 0 {
				maxHeight = canvasSize.Height
			}

			panelMin := panel.MinSize()
			panelWidth := minFloat32(maxFloat32(panelMin.Width, 500), maxWidth)
			panelHeight := minFloat32(maxFloat32(panelMin.Height, 380), maxHeight)
			return fyne.NewSize(panelWidth, panelHeight)
		},
	})
	popup.Show()
}

// showErrorAsync safely shows an error from a goroutine
func (bw *BackupWidget) showErrorAsync(err error) {
	bw.updateUIAsync(func() {
		if bw.window != nil {
			view.ShowErrorDialog(err, bw.window)
		}
		bw.ui.StatusLabel.SetText(fmt.Sprintf(i18n.Current.ErrorStatusFormat, err))
	})
}

func (bw *BackupWidget) finishMountRefresh() {
	logrus.Info("⏳ Waiting for device list update (2 seconds)...")
	time.Sleep(2 * time.Second)

	bw.isMounting.Store(false)
	bw.loadCurrentFlash()
	bw.loadSnapshots()

	if bw.updateStatus != nil {
		logrus.Info("🔄 Calling updateStatus() to update icons")
		fyne.Do(func() {
			bw.updateStatus()
		})
	}
}

func (bw *BackupWidget) handleDisconnectCurrentFlash() {
	if bw.usbClient == nil {
		if bw.window != nil {
			view.ShowErrorDialog(fmt.Errorf("%s", i18n.Current.ErrorNotConnected), bw.window)
		}
		return
	}

	if !bw.currentFlashConnected {
		return
	}

	view.ShowConfirmYesLeft(i18n.Current.Confirmation, i18n.Current.UnmountSelectedConfirm, func(ok bool) {
		if !ok {
			return
		}

		bw.updateStatusAsync(i18n.Current.StoppingAllDevices)
		go func() {
			client := bw.usbClient
			if client == nil {
				return
			}
			batchRequest := bw.buildDeviceBatchWithoutCurrentFlash()
			if _, err := executeDeviceBatch(client, client.StartDevicesBatchWithMerge, batchRequest, false); err != nil {
				logrus.Errorf("❌ Error unmounting backup flash: %v", err)
				bw.showErrorAsync(fmt.Errorf(i18n.Current.ErrorMounting, err))
				return
			}

			time.Sleep(2 * time.Second)
			bw.updateStatusAsync(i18n.Current.AllDevicesUnmounted)
			bw.finishMountRefresh()
		}()
	}, bw.window)
}

func (bw *BackupWidget) logBatchResponse(success bool, message string, data any) {
	logrus.Infof("✅ API response from USBridge 2:")
	logrus.Infof("  - Success: %v", success)
	logrus.Infof("  - Message: %s", message)
	if data != nil {
		logrus.Infof("  - Data: %+v", data)
	}
}
