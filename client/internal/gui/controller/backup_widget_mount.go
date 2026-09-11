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
	"fyne.io/fyne/v2/widget"
	"github.com/sirupsen/logrus"
)

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
		if device.Type == "nbd" || (device.Type == "mtp" && (strings.Contains(device.ProductName, "snapshot") || strings.Contains(device.Name, "snapshot"))) {
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
	bw.updateUIAsync(func() { bw.ui.Refresh() })

	go func() {
		success := false
		defer func() {
			if !success {
				bw.isMounting.Store(false)
				bw.updateUIAsync(func() { bw.ui.Refresh() })
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
	bw.updateUIAsync(func() { bw.ui.Refresh() })

	go func() {
		success := false
		defer func() {
			if !success {
				bw.isMounting.Store(false)
				bw.updateUIAsync(func() { bw.ui.Refresh() })
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

func snapshotInfoFieldLabel(format string) string {
	s := strings.TrimSpace(strings.TrimSuffix(format, "%s"))
	return strings.TrimSpace(strings.TrimSuffix(s, ":"))
}

func newSnapshotInfoStatRow(label, value string, valueColor color.Color) fyne.CanvasObject {
	labelText := canvas.NewText(label, design.ColorConnectionsSectionSubtitle)
	labelText.TextSize = 10
	labelText.TextStyle.Monospace = true

	valueText := canvas.NewText(value, valueColor)
	valueText.TextSize = 10
	valueText.TextStyle.Monospace = true
	valueText.Alignment = fyne.TextAlignTrailing

	return container.NewBorder(nil, nil, labelText, nil, valueText)
}

func newSnapshotInfoSurface(content fyne.CanvasObject) fyne.CanvasObject {
	bg := canvas.NewRectangle(design.ColorGray950)
	bg.CornerRadius = 6
	bg.StrokeColor = design.ColorTailscaleChipBorder
	bg.StrokeWidth = 1
	return container.NewStack(bg, view.NewInset(content, 12, 12, 8, 8))
}

func newSnapshotInfoLogLine(text string, col color.Color) fyne.CanvasObject {
	line := canvas.NewText(text, col)
	line.TextSize = 10
	line.TextStyle.Monospace = true
	return line
}

const snapshotInfoLogHeight float32 = 152

// showSnapshotDetails shows snapshot date/size/changelog in the same panel
// chrome as Add Connection (accent hairline, left title+subtitle, corner X,
// gray-900 card, compact pill footer). Copy/OK sit where Connect/Save would.
func (bw *BackupWidget) showSnapshotDetails(snapshot *models.SnapshotInfo) {
	if bw.window == nil {
		return
	}

	title := "Snapshot Info"
	dateValue := snapshot.CreatedAt.In(time.Local).Format(i18n.Current.DateTimeFormat)
	sizeValue := snapshot.DisplaySize()
	dateLabel := snapshotInfoFieldLabel(i18n.Current.SnapshotDetailsDate)
	sizeLabel := snapshotInfoFieldLabel(i18n.Current.SnapshotDetailsSize)

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
		fmt.Sprintf("%s %s", i18n.Current.SnapshotDetailsDate, dateValue),
		fmt.Sprintf("%s %s", i18n.Current.SnapshotDetailsSize, sizeValue),
		"",
		"LOG",
		changelog,
	}, "\n")

	dividerColor := color.NRGBA{R: 0x29, G: 0x2d, B: 0x27, A: 0xff}
	metaSep := canvas.NewRectangle(dividerColor)
	metaSep.SetMinSize(fyne.NewSize(1, 1))
	metaBox := newSnapshotInfoSurface(container.New(&tightHeaderVBoxLayout{Gap: 4},
		newSnapshotInfoStatRow(dateLabel, dateValue, design.ColorTextLight),
		metaSep,
		newSnapshotInfoStatRow(sizeLabel, sizeValue, color.NRGBA{R: 0xe9, G: 0xfd, B: 0xbb, A: 0xff}),
	))

	logTitle := canvas.NewText("LOG", design.ColorConnectionsSectionSubtitle)
	logTitle.TextSize = 10
	logTitle.TextStyle.Monospace = true
	logLines := make([]fyne.CanvasObject, 0)
	for _, line := range strings.Split(changelog, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		logLines = append(logLines, newSnapshotInfoLogLine(line, design.ColorTextMuted))
	}
	if len(logLines) == 0 {
		logLines = append(logLines, newSnapshotInfoLogLine(i18n.Current.SnapshotChangelogEmpty, design.ColorBorder))
	}
	logScroll := container.NewVScroll(container.New(&tightHeaderVBoxLayout{Gap: 4}, logLines...))
	logScroll.SetMinSize(fyne.NewSize(0, snapshotInfoLogHeight))
	logBox := newSnapshotInfoSurface(container.New(&tightHeaderVBoxLayout{Gap: 8}, logTitle, logScroll))

	form := container.NewVBox(
		view.NewInset(metaBox, 0, 0, 12, 0),
		view.NewInset(logBox, 0, 0, 8, 4),
	)

	var popup *widget.PopUp
	closePopup := func() {
		if popup != nil {
			popup.Hide()
		}
	}

	titleText := view.NewBrandText(title, 13, design.ColorTextLight, true)
	var titleCol fyne.CanvasObject = titleText
	if name := strings.TrimSpace(snapshot.Name); name != "" {
		subtitleLbl := widget.NewLabel(name)
		subtitleLbl.Wrapping = fyne.TextWrapWord
		subtitleThemed := container.NewThemeOverride(subtitleLbl, &mutedForegroundTheme{design.NewBrandTheme()})
		nudgedSubtitle := container.New(&subtitleLeftNudgeLayout{Amount: 8}, subtitleThemed)
		titleCol = container.New(&tightHeaderVBoxLayout{Gap: -2}, titleText, nudgedSubtitle)
	}

	closeBtn := newConnectionDialogIconButton(connectionDialogCancelIconRes, closePopup)
	topAccent := newConnectionDialogTopAccentBar()
	sep := canvas.NewRectangle(color.NRGBA{R: 0x30, G: 0x34, B: 0x2e, A: 0xff})
	sep.SetMinSize(fyne.NewSize(0, 1))
	sepFooter := canvas.NewRectangle(color.NRGBA{R: 0x30, G: 0x34, B: 0x2e, A: 0xff})
	sepFooter.SetMinSize(fyne.NewSize(0, 1))
	headerBlock := container.New(&tightHeaderVBoxLayout{Gap: 0}, topAccent, view.NewInset(titleCol, 21, 44, 9, 4), sep)

	copyIcon := fyne.NewStaticResource("snapshot-info-copy.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="#e9fdbb"><path d="M16 1H4c-1.1 0-2 .9-2 2v14h2V3h12V1zm3 4H8c-1.1 0-2 .9-2 2v14c0 1.1.9 2 2 2h11c1.1 0 2-.9 2-2V7c0-1.1-.9-2-2-2zm0 16H8V7h11v14z"/></svg>`))
	copyBtn := newConnectionDialogIconButton(copyIcon, func() {
		if bw.window != nil && bw.window.Clipboard() != nil {
			bw.window.Clipboard().SetContent(copyContent)
		}
	})
	copyBtn.buttonSize = fyne.NewSize(32, 32)
	copyBtn.iconSize = fyne.NewSize(14, 14)
	copyBtn.customNormalFill = color.NRGBA{R: 0x22, G: 0x26, B: 0x2a, A: 0xff}
	copyBtn.customHoverFill = color.NRGBA{R: 0x31, G: 0x35, B: 0x39, A: 0xff}
	copyBtn.customNormalBorder = color.Transparent
	copyBtn.customHoverBorder = color.Transparent
	copyBtn.opaqueIcon = true

	okBtn := &connectionDialogSecondaryButton{
		labelText:      i18n.Current.OK,
		onTapped:       closePopup,
		compact:        true,
		fillColor:      design.ColorConnectionBadgeText,
		borderColor:    color.Transparent,
		textColor:      design.ColorGray950,
		hoverFillColor: color.NRGBA{R: 0x61, G: 0xf0, B: 0xd3, A: 0xff},
		hoverTextColor: design.ColorGray950,
	}
	okBtn.ExtendBaseWidget(okBtn)

	cancelBtn := &connectionDialogSecondaryButton{
		labelText:      i18n.Current.Cancel,
		onTapped:       closePopup,
		compact:        true,
		fillColor:      color.Transparent,
		borderColor:    color.Transparent,
		textColor:      color.NRGBA{R: 0x8f, G: 0x93, B: 0x81, A: 0xff},
		hoverFillColor: color.Transparent,
		hoverTextColor: design.ColorTextLight,
	}
	cancelBtn.ExtendBaseWidget(cancelBtn)

	rightGroup := container.New(&view.DeviceRowControlsLayout{Gap: connectionDialogButtonsGap}, copyBtn, okBtn)
	buttons := container.NewBorder(nil, nil, container.NewCenter(cancelBtn), rightGroup)
	footerBlock := container.NewVBox(
		sepFooter,
		view.NewInset(buttons, 12, 18, 14, 0),
	)

	scrollBody := container.NewVBox(form)
	scroll := container.NewVScroll(scrollBody)
	scroll.SetMinSize(fyne.NewSize(0, scrollBody.MinSize().Height))

	bg := canvas.NewRectangle(design.ColorGray900)
	bg.CornerRadius = design.RadiusMD
	border := canvas.NewRectangle(color.Transparent)
	border.CornerRadius = design.RadiusMD
	border.StrokeColor = design.ColorBorder
	border.StrokeWidth = 1

	inner := container.NewBorder(
		headerBlock,
		footerBlock,
		nil, nil,
		view.NewInset(scroll, 18, 18, 0, 0),
	)
	cornerBtn := container.New(&dialogCornerButtonLayout{Top: 12, Right: 12}, closeBtn)
	panel := container.NewStack(
		bg,
		view.NewInset(inner, 0, 0, 0, 16),
		cornerBtn,
		border,
	)

	popup = view.ShowOverlayPopup(bw.window, view.OverlayPopupSpec{
		Panel:    panel,
		DimColor: connectionDialogDimColor(),
		PanelSize: func(canvasSize fyne.Size, panel fyne.CanvasObject) fyne.Size {
			return connectionDialogPanelSize(panel, canvasSize)
		},
	})
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

	view.ShowConfirmToast(i18n.Current.UnmountSelectedConfirm, func(ok bool) {
		if !ok {
			return
		}
		bw.unmountBackupOrSnapshotMTP()
	}, bw.window)
}

func (bw *BackupWidget) handleUnmountSnapshot(snapshot *models.SnapshotInfo) {
	if bw.usbClient == nil {
		if bw.window != nil {
			view.ShowErrorDialog(fmt.Errorf("%s", i18n.Current.ErrorNotConnected), bw.window)
		}
		return
	}
	if snapshot == nil || !snapshot.Connected {
		return
	}

	view.ShowConfirmToast(i18n.Current.UnmountSelectedConfirm, func(ok bool) {
		if !ok {
			return
		}
		bw.unmountBackupOrSnapshotMTP()
	}, bw.window)
}

func (bw *BackupWidget) unmountBackupOrSnapshotMTP() {
	bw.isMounting.Store(true)
	bw.updateUIAsync(func() { bw.ui.Refresh() })
	bw.updateStatusAsync(i18n.Current.StoppingAllDevices)
	go func() {
		success := false
		defer func() {
			if !success {
				bw.isMounting.Store(false)
				bw.updateUIAsync(func() { bw.ui.Refresh() })
			}
		}()

		client := bw.usbClient
		if client == nil {
			return
		}
		batchRequest := bw.buildDeviceBatchWithoutCurrentFlash()
		if _, err := executeDeviceBatch(client, client.StartDevicesBatchWithMerge, batchRequest, false); err != nil {
			logrus.Errorf("❌ Error unmounting backup MTP: %v", err)
			bw.showErrorAsync(fmt.Errorf(i18n.Current.ErrorMounting, err))
			return
		}

		time.Sleep(2 * time.Second)
		bw.updateStatusAsync(i18n.Current.AllDevicesUnmounted)
		success = true
		bw.finishMountRefresh()
	}()
}

func (bw *BackupWidget) logBatchResponse(success bool, message string, data any) {
	logrus.Infof("✅ API response from USBridge 2:")
	logrus.Infof("  - Success: %v", success)
	logrus.Infof("  - Message: %s", message)
	if data != nil {
		logrus.Infof("  - Data: %+v", data)
	}
}
