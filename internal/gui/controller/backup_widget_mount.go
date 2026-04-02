package controller

import (
	"fmt"
	"strings"
	"time"

	"usbridge-client/internal/gui/i18n"
	"usbridge-client/internal/models"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
	"github.com/sirupsen/logrus"
)

// canConnectBackupOrSnapshot проверяет, можно ли подключить бэкап или снапшот.
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

// buildDeviceBatchWithMTP собирает batch-запрос с сохранением подключённых устройств и заменой MTP.
func (bw *BackupWidget) buildDeviceBatchWithMTP(mtpServer, mtpProductName string) models.DeviceStartBatchRequest {
	var requests []models.DeviceStartRequest
	addedKeyboard, addedMouse, addedRndis := false, false, false

	deviceInfo, err := bw.usbClient.GetDeviceInfo()
	if err == nil {
		for _, device := range deviceInfo.Devices {
			if device.Status != "connected" {
				continue
			}

			switch {
			case (device.Type == "keyboard" || strings.HasPrefix(device.Type, "keyboard:")) && !addedKeyboard:
				requests = append(requests, models.DeviceStartRequest{
					Device:       "keyboard",
					VendorID:     "0x1d6b",
					ProductID:    "0x0104",
					ProductName:  "USBridge Keyboard",
					Manufacturer: "USBridge",
					KeyboardMode: true,
				})
				addedKeyboard = true
			case isMouseDeviceType(device.Type) && !addedMouse:
				mouseType := mouseModeFromDeviceType(device.Type)
				requests = append(requests, models.DeviceStartRequest{
					Device:       "mouse",
					Type:         mouseTransportType(mouseType),
					VendorID:     "0x1d6b",
					ProductID:    "0x0104",
					ProductName:  "USBridge Mouse",
					Manufacturer: "USBridge",
				})
				addedMouse = true
			case (device.Type == "rndis" || strings.HasPrefix(device.Type, "rndis:")) && !addedRndis:
				rndisMode := "auto"
				if strings.HasPrefix(device.Type, "rndis:") {
					rndisMode = strings.TrimPrefix(device.Type, "rndis:")
				}
				requests = append(requests, models.DeviceStartRequest{
					Device:       "rndis",
					VendorID:     "0x1d6b",
					ProductID:    "0x0104",
					ProductName:  "USBridge RNDIS",
					Manufacturer: "USBridge",
					RNDISMode:    normalizeRNDISMode(rndisMode),
				})
				addedRndis = true
			case device.Type == "local" && !strings.Contains(device.Name, "data"):
				requests = append(requests, models.DeviceStartRequest{
					Device:       "drive",
					Server:       device.Name,
					ReadOnly:     true,
					VendorID:     "0x1d6b",
					ProductID:    "0x0104",
					ProductName:  "USBridge Local CD-ROM",
					Manufacturer: "USBridge",
				})
			}
		}
	}

	requests = append(requests, models.DeviceStartRequest{
		Device:       "mtp",
		Server:       mtpServer,
		VendorID:     "0x1d6b",
		ProductID:    "0x0104",
		ProductName:  mtpProductName,
		Manufacturer: "USBridge",
	})

	logrus.Infof("📋 Собран batch: %d устройств (включая MTP %s)", len(requests), mtpServer)
	return models.DeviceStartBatchRequest(requests)
}

// handleMountCurrentFlash обрабатывает монтирование актуальной флешки
func (bw *BackupWidget) handleMountCurrentFlash() {
	if bw.usbClient == nil {
		logrus.Warn("⚠️ USB client not initialized")
		if bw.window != nil {
			dialog.ShowError(fmt.Errorf(i18n.Current.ErrorNotConnected), bw.window)
		}
		return
	}

	if bw.currentFlash == nil {
		logrus.Warn("⚠️ Current flash not found")
		if bw.window != nil {
			dialog.ShowError(fmt.Errorf(i18n.Current.ErrorFlashNotFound), bw.window)
		}
		return
	}

	if ok, msg := bw.canConnectBackupOrSnapshot(); !ok {
		logrus.Warnf("⚠️ Нельзя подключить бэкап-флешку: %s", msg)
		bw.updateStatusAsync(msg)
		if bw.window != nil {
			dialog.ShowInformation(i18n.Current.Information, msg, bw.window)
		}
		return
	}

	logrus.Infof("🔧 Монтирование актуальной флешки: %s", bw.currentFlash.Name)
	bw.updateStatusAsync(fmt.Sprintf(i18n.Current.MountingFlash, bw.currentFlash.Name))

	go func() {
		batchRequest := bw.buildDeviceBatchWithMTP(bw.currentFlash.Name, "BackupDrive")
		logrus.Infof("🚀 Запуск монтирования актуальной флешки как MTP: %s (с сохранением остальных устройств)", bw.currentFlash.Name)

		deviceResp, err := bw.usbClient.StartDevicesBatch(batchRequest)
		if err != nil {
			logrus.Errorf("❌ Ошибка монтирования актуальной флешки: %v", err)
			bw.showErrorAsync(fmt.Errorf(i18n.Current.ErrorMountingFlashMsg, err))
			return
		}

		bw.logBatchResponse(deviceResp.Success, deviceResp.Message, deviceResp.Data)
		bw.updateUIAsync(func() {
			bw.ui.StatusLabel.SetText(fmt.Sprintf(i18n.Current.FlashMounted, bw.currentFlash.Name))
		})

		logrus.Infof("✅ Актуальная флешка %s успешно смонтирована", bw.currentFlash.Name)
		bw.finishMountRefresh()
	}()
}

// handleMountSnapshot обрабатывает монтирование снапшота
func (bw *BackupWidget) handleMountSnapshot(id widget.ListItemID, snapshot *models.SnapshotInfo) {
	_ = id
	if bw.usbClient == nil {
		logrus.Warn("⚠️ USB client not initialized")
		if bw.window != nil {
			dialog.ShowError(fmt.Errorf(i18n.Current.ErrorNotConnected), bw.window)
		}
		return
	}

	if ok, msg := bw.canConnectBackupOrSnapshot(); !ok {
		logrus.Warnf("⚠️ Нельзя подключить снапшот: %s", msg)
		bw.updateStatusAsync(msg)
		if bw.window != nil {
			dialog.ShowInformation(i18n.Current.Information, msg, bw.window)
		}
		return
	}

	logrus.Infof("🔧 Монтирование снапшота: %s", snapshot.Name)
	bw.updateStatusAsync(fmt.Sprintf(i18n.Current.MountingSnapshot, snapshot.Name))

	go func() {
		batchRequest := bw.buildDeviceBatchWithMTP(snapshot.Name, snapshot.Name)
		logrus.Infof("🚀 Запуск монтирования снапшота как MTP: %s (с сохранением остальных устройств)", snapshot.Name)

		deviceResp, err := bw.usbClient.StartDevicesBatch(batchRequest)
		if err != nil {
			logrus.Errorf("❌ Ошибка монтирования снапшота: %v", err)
			bw.showErrorAsync(fmt.Errorf(i18n.Current.ErrorMountingSnapshotMsg, err))
			return
		}

		bw.logBatchResponse(deviceResp.Success, deviceResp.Message, deviceResp.Data)
		bw.updateUIAsync(func() {
			bw.ui.StatusLabel.SetText(fmt.Sprintf(i18n.Current.SnapshotMounted, snapshot.Name))
		})

		logrus.Infof("✅ Снапшот %s успешно смонтирован", snapshot.Name)
		bw.finishMountRefresh()
	}()
}

// showSnapshotDetails показывает диалог с деталями снапшота
func (bw *BackupWidget) showSnapshotDetails(snapshot *models.SnapshotInfo) {
	if bw.window == nil {
		return
	}

	title := fmt.Sprintf(i18n.Current.SnapshotDetailsTitle, snapshot.Name)
	lines := []string{
		fmt.Sprintf(i18n.Current.SnapshotDetailsDate, snapshot.CreatedAt.Format(i18n.Current.DateTimeFormat)),
		fmt.Sprintf(i18n.Current.SnapshotDetailsSize, snapshot.DisplaySize()),
	}
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
	if changelog != "" {
		lines = append(lines, "", i18n.Current.SnapshotChangelogTitle, changelog)
	} else {
		lines = append(lines, "", i18n.Current.SnapshotChangelogEmpty)
	}

	content := strings.Join(lines, "\n")
	label := widget.NewLabel(content)
	label.Wrapping = fyne.TextWrapWord
	scroll := container.NewScroll(label)
	scroll.SetMinSize(fyne.NewSize(0, 200))

	d := dialog.NewCustom(title, i18n.Current.OK, scroll, bw.window)
	d.Resize(fyne.NewSize(500, 450))
	d.Show()
}

// showErrorAsync безопасно показывает ошибку из горутины
func (bw *BackupWidget) showErrorAsync(err error) {
	bw.updateUIAsync(func() {
		if bw.window != nil {
			dialog.ShowError(err, bw.window)
		}
		bw.ui.StatusLabel.SetText(fmt.Sprintf(i18n.Current.ErrorStatusFormat, err))
	})
}

func (bw *BackupWidget) finishMountRefresh() {
	logrus.Info("⏳ Ожидание обновления списка устройств (2 секунды)...")
	time.Sleep(2 * time.Second)

	bw.loadCurrentFlash()
	bw.loadSnapshots()

	if bw.updateStatus != nil {
		logrus.Info("🔄 Вызов updateStatus() для обновления иконок")
		fyne.Do(func() {
			bw.updateStatus()
		})
	}
}

func (bw *BackupWidget) logBatchResponse(success bool, message string, data any) {
	logrus.Infof("✅ API ответ от USBridge 2:")
	logrus.Infof("  - Success: %v", success)
	logrus.Infof("  - Message: %s", message)
	if data != nil {
		logrus.Infof("  - Data: %+v", data)
	}
}
