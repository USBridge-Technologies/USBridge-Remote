package controller

import (
	"fmt"
	"runtime"
	"strings"
	"time"

	"usbridge-client/internal/gui/i18n"
	"usbridge-client/internal/gui/view"
	"usbridge-client/internal/models"
	"usbridge-client/internal/service"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
	"github.com/sirupsen/logrus"
)

// startDevicesWithRetry выполняет StartDevicesBatchWithMerge с 3 попытками и паузой 3 с между ними.
func (dw *DiskWidget) startDevicesWithRetry(batchRequest models.DeviceStartBatchRequest, merge bool) (*models.APIResponse, error) {
	const maxAttempts = 3
	const retryDelay = 3 * time.Second

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		resp, err := dw.usbClient.StartDevicesBatchWithMerge(batchRequest, merge)
		if err == nil {
			if attempt > 1 {
				logrus.Infof("✅ [MOUNT-API-RETRY] Попытка %d/%d успешна", attempt, maxAttempts)
			}
			return resp, nil
		}
		lastErr = err
		errStr := err.Error()
		isRetryable := strings.Contains(errStr, "EOF") || strings.Contains(errStr, "NBD") ||
			strings.Contains(errStr, "Failed to connect NBD") || strings.Contains(errStr, "connection reset") ||
			strings.Contains(errStr, "connection refused")
		if !isRetryable || attempt == maxAttempts {
			return nil, err
		}
		logrus.Warnf("⚠️ [MOUNT-API-RETRY] Попытка %d/%d: %v, повтор через 3 с...", attempt, maxAttempts, err)
		dw.updateStatusAsync(fmt.Sprintf("Повтор подключения %d/%d через 3 с...", attempt, maxAttempts))
		time.Sleep(retryDelay)
	}
	return nil, lastErr
}

// handleMount обрабатывает монтирование.
func (dw *DiskWidget) handleMount() {
	dw.setUserOperationInFlight(true)
	dw.setButtonsEnabled(false)
	logrus.Infof("📍 [MOUNT-1] handleMount вызван, GOOS: %s", runtime.GOOS)

	if dw.usbClient == nil {
		logrus.Warn("⚠️ USB клиент не инициализирован")
		if dw.window != nil {
			view.ShowErrorDialog(fmt.Errorf("%s", i18n.Current.ErrorNotConnected), dw.window)
		}
		dw.setUserOperationInFlight(false)
		dw.setButtonsEnabled(true)
		return
	}

	var mountedDrives []DriveItem
	var mountedVideoCount int
	for _, drive := range dw.allDrives {
		if drive.IsMounted {
			if drive.IsVideo {
				mountedVideoCount++
			} else {
				mountedDrives = append(mountedDrives, drive)
			}
		}
	}

	var selectedDrives []DriveItem
	dw.selectedItemsMu.RLock()
	for id, selected := range dw.selectedItems {
		if selected && id < len(dw.allDrives) {
			drive := dw.allDrives[id]
			if !drive.IsMounted && !drive.IsVideo {
				selectedDrives = append(selectedDrives, drive)
			}
		}
	}
	dw.selectedItemsMu.RUnlock()

	if len(selectedDrives) == 0 {
		logrus.Warnf("⚠️ Нет выбранных устройств для подключения")
		if dw.window != nil {
			view.ShowErrorDialog(fmt.Errorf("%s", i18n.Current.SelectDevicesToMount), dw.window)
		}
		dw.setUserOperationInFlight(false)
		dw.setButtonsEnabled(true)
		return
	}

	totalCount := len(mountedDrives) + len(selectedDrives)
	if totalCount > MaxDevicesToMount {
		logrus.Warnf("⚠️ Слишком много устройств: %d (максимум %d)", totalCount, MaxDevicesToMount)
		if dw.window != nil {
			dialog.ShowInformation(i18n.Current.Information, i18n.Current.MaxDevicesReached, dw.window)
		}
		dw.setUserOperationInFlight(false)
		dw.setButtonsEnabled(true)
		return
	}

	logrus.Infof("📁 Подключено: gadget=%d, video=%d, добавляем gadget=%d", len(mountedDrives), mountedVideoCount, len(selectedDrives))

	hasGoogleDriveFiles := false
	for _, drive := range selectedDrives {
		if (drive.Source == "local" || drive.Source == "user") && drive.DiskInfo != nil {
			if strings.Contains(drive.DiskInfo.Path, "com.google.android.apps.docs.storage") {
				hasGoogleDriveFiles = true
				break
			}
		}
	}

	var progressDialog dialog.Dialog
	if hasGoogleDriveFiles {
		logrus.Warnf("⚠️  Обнаружены файлы из Google Drive! Показываем предупреждение с прогрессом")

		progressBar := widget.NewProgressBarInfinite()
		progressContent := container.NewVBox(
			widget.NewLabel(i18n.Current.LoadingFromCloud),
			widget.NewLabel(""),
			widget.NewLabel(i18n.Current.CloudFilesDetected),
			widget.NewLabel(i18n.Current.AndroidBuffering),
			widget.NewLabel(i18n.Current.MayTake30Seconds),
			widget.NewLabel(""),
			progressBar,
			widget.NewLabel(""),
			widget.NewLabel(i18n.Current.PleaseWait),
		)

		progressDialog = dialog.NewCustomWithoutButtons(i18n.Current.PreparingToMount, progressContent, dw.window)
		progressDialog.Show()
	}

	logrus.Infof("Монтирование %d устройств...", len(selectedDrives))

	go func() {
		reEnableButtons := true
		defer func() {
			if progressDialog != nil {
				fyne.Do(func() {
					progressDialog.Hide()
				})
			}
			if reEnableButtons {
				dw.setUserOperationInFlight(false)
				dw.setButtonsEnabled(true)
			}
		}()

		var deviceRequests []models.DeviceStartRequest
		startedMouseMode := ""

		for _, selectedDrive := range selectedDrives {
			var deviceRequest *models.DeviceStartRequest

			if selectedDrive.Source == "keyboard" {
				req := newKeyboardStartRequest()
				deviceRequest = &req
				logrus.Infof("⌨️ Подготовка клавиатуры для монтирования")
			} else if selectedDrive.Source == "mouse" {
				mouseType := normalizeMouseMode(selectedDrive.MouseType)
				startedMouseMode = mouseType
				req := newMouseStartRequest(mouseType)
				deviceRequest = &req
				logrus.Infof("🖱️ Подготовка манипулятора для монтирования: ui_mode=%s transport=%s", mouseType, mouseTransportType(mouseType))
			} else if selectedDrive.Source == "rndis" {
				rndisMode := normalizeRNDISMode(selectedDrive.RNDISMode)
				req := newRNDISStartRequest(rndisMode)
				deviceRequest = &req
				logrus.Infof("🌐 Подготовка сетевой карты RNDIS для монтирования (mode=%s)", rndisMode)
			} else if selectedDrive.Source == "api" && selectedDrive.LocalDrive != nil {
				if selectedDrive.LocalDrive.SourceType == "mtp" {
					deviceRequest = &models.DeviceStartRequest{
						Device:       "mtp",
						Server:       selectedDrive.LocalDrive.Name,
						VendorID:     "0x1d6b",
						ProductID:    "0x0104",
						ProductName:  "BackupDrive",
						Manufacturer: "USBridge",
					}
					logrus.Infof("📱 Подготовка MTP устройства из API: %s", selectedDrive.Name)
				} else {
					deviceRequest = &models.DeviceStartRequest{
						Device:       "drive",
						Server:       selectedDrive.LocalDrive.Name,
						VendorID:     "0x1d6b",
						ProductID:    "0x0104",
						ProductName:  "USBridge Local CD-ROM",
						Manufacturer: "USBridge",
					}
					logrus.Infof("📱 Подготовка устройства из API: %s", selectedDrive.Name)
				}
			} else if (selectedDrive.Source == "local" || selectedDrive.Source == "user") && selectedDrive.DiskInfo != nil {
				localIP, err := dw.getLocalIP()
				if err != nil {
					dw.showErrorAsync(fmt.Errorf("ошибка получения локального IP: %v", err))
					return
				}

				nbdPort, err := dw.getAvailablePort()
				if err != nil {
					dw.showErrorAsync(fmt.Errorf("ошибка получения свободного порта: %v", err))
					return
				}

				exportName := selectedDrive.DiskInfo.Name
				if existingServer, exists := dw.nbdServers[exportName]; exists {
					logrus.Infof("⚠️ NBD сервер для экспорта '%s' уже существует, останавливаем его перед созданием нового", exportName)
					if existingServer.IsRunning() {
						if err := existingServer.Stop(); err != nil {
							logrus.Warnf("⚠️ Ошибка остановки существующего NBD сервера: %v", err)
						}
					}
					delete(dw.nbdServers, exportName)
				}

				nbdServer, err := dw.startNBDServer(selectedDrive.DiskInfo, nbdPort, exportName, selectedDrive.ReadOnly)
				if err != nil {
					dw.showErrorAsync(fmt.Errorf("ошибка запуска NBD сервера: %v", err))
					return
				}

				dw.nbdServers[exportName] = nbdServer
				exportNameForAPI := nbdServer.NBDExportNameForAPI()
				logrus.Infof("📁 Подготовка локального файла: %s, export_name для API: %s", selectedDrive.Name, exportNameForAPI)

				deviceRequest = &models.DeviceStartRequest{
					Device:                  "drive",
					Server:                  localIP,
					Port:                    nbdPort,
					ExportName:              exportNameForAPI,
					NBDHandshakeEmptyExport: nbdServer.NBDHandshakeEmptyExport(),
					ReadOnly:                selectedDrive.ReadOnly,
					VendorID:                "0x1d6b",
					ProductID:               "0x0104",
					ProductName:             "USBridge NBD CD-ROM",
					Manufacturer:            "USBridge",
				}
			} else {
				logrus.Warnf("⚠️ Неизвестный тип устройства: %s", selectedDrive.Name)
				continue
			}

			if deviceRequest != nil {
				deviceRequests = append(deviceRequests, *deviceRequest)
			}
		}

		if len(deviceRequests) == 0 {
			dw.showErrorAsync(fmt.Errorf("не удалось подготовить устройства для монтирования"))
			return
		}

		nbdExportNamesForUI := make(map[string]bool)
		for _, req := range deviceRequests {
			if req.Device != "drive" {
				continue
			}
			for name, srv := range dw.nbdServers {
				if !srv.IsRunning() {
					continue
				}
				st := srv.GetServerStatus()
				if p, ok := st["server_port"]; ok {
					var port int
					switch v := p.(type) {
					case int:
						port = v
					case int64:
						port = int(v)
					case float64:
						port = int(v)
					default:
						continue
					}
					if port == req.Port {
						nbdExportNamesForUI[name] = true
						break
					}
				}
			}
		}

		if len(dw.nbdServers) > 0 {
			logrus.Infof("📡 [MOUNT-NBD-1] Запуск проверки готовности для %d NBD серверов...", len(dw.nbdServers))
			for exportName, nbdServer := range dw.nbdServers {
				logrus.Infof("  📡 [MOUNT-NBD-2] Запуск проверки готовности для: %s", exportName)
				nbdServer.SignalReady()
			}

			logrus.Infof("⏱️ [MOUNT-NBD-3] Ожидание готовности %d NBD серверов (таймаут: 30 секунд)...", len(dw.nbdServers))

			readyCount := 0
			timeoutChan := time.After(30 * time.Second)
			ticker := time.NewTicker(100 * time.Millisecond)
			defer ticker.Stop()

			serversToWait := make(map[string]service.NBDRunner)
			for exportName, srv := range dw.nbdServers {
				serversToWait[exportName] = srv
			}

		waitLoop:
			for {
				select {
				case <-timeoutChan:
					notReadyServers := []string{}
					for exportName := range serversToWait {
						notReadyServers = append(notReadyServers, exportName)
					}
					logrus.Errorf("❌ [MOUNT-NBD-TIMEOUT] Таймаут ожидания готовности NBD серверов. Не готовы: %v", notReadyServers)
					dw.showErrorAsync(fmt.Errorf("таймаут ожидания готовности NBD серверов: %v", notReadyServers))
					return
				case <-ticker.C:
					for exportName, nbdServer := range serversToWait {
						select {
						case <-nbdServer.WaitReady():
							logrus.Infof("✅ [MOUNT-NBD-READY] NBD сервер %s готов к приему соединений", exportName)
							delete(serversToWait, exportName)
							readyCount++
						default:
						}
					}

					if len(serversToWait) == 0 {
						logrus.Infof("✅ [MOUNT-NBD-4] Все %d NBD серверов готовы к приему соединений", readyCount)
						break waitLoop
					}
				}
			}
		}

		if len(dw.nbdServers) > 0 && dw.frpService != nil && !dw.frpService.IsRunning() {
			logrus.Errorf("❌ [MOUNT-NBD-FRP] FRP туннель не активен, NBD proxies не зарегистрированы в frps")
			dw.showErrorAsync(fmt.Errorf("FRP туннель не активен — переподключитесь перед монтированием NBD"))
			return
		}
		if len(dw.nbdServers) > 0 && dw.frpService != nil {
			logrus.Infof("✅ [MOUNT-NBD-FRP] FRP туннель активен, NBD proxies (nbd_srv1-16) зарегистрированы")
		}

		if len(dw.nbdServers) > 0 {
			logrus.Infof("⏱️ [MOUNT-NBD-DELAY] Ожидание 1 с перед отправкой API (NBD готов, даём FRP/туннелю стабилизироваться)")
			time.Sleep(1 * time.Second)
		}

		batchRequest := models.DeviceStartBatchRequest(deviceRequests)

		mountingExportNames := nbdExportNamesForUI
		dw.updateUIAsync(func() {
			dw.setMountingStateByExportNames(mountingExportNames, true)
			dw.setAPIMountInProgress(true)
			dw.selectedItemsMu.Lock()
			dw.selectedItems = make(map[int]bool)
			dw.selectedItemsMu.Unlock()
			dw.requestDevicesRefresh()
		})

		if len(deviceRequests) > 0 {
			dw.updateStatusAsync("Запуск устройств...")
			logrus.Infof("🚀 [MOUNT-API-1] Запуск %d устройств (Merge), отправляем запрос /api/device/start", len(deviceRequests))
			for i, req := range deviceRequests {
				logrus.Infof("   📤 [MOUNT-API-1] Устройство %d: device=%s, server=%s, port=%d, export_name=%s, read_only=%v", i+1, req.Device, req.Server, req.Port, req.ExportName, req.ReadOnly)
			}

			deviceResp, err := executeDeviceBatch(dw.usbClient, dw.startDevicesWithRetry, batchRequest, true)
			if err != nil {
				logrus.Errorf("❌ [MOUNT-API-ERROR] Ошибка запуска устройств: %v", err)
				dw.updateUIAsync(func() {
					dw.setMountingStateByExportNames(mountingExportNames, false)
					dw.setAPIMountInProgress(false)
					dw.setUserOperationInFlight(false)
					dw.setButtonsEnabled(true)
				})
				dw.showErrorAsync(fmt.Errorf("ошибка запуска устройств: %v", err))
				return
			}

			logrus.Infof("✅ [MOUNT-API-2] API ответ от USBridge 2:")
			logrus.Infof("  - Success: %v", deviceResp.Success)
			logrus.Infof("  - Message: %s", deviceResp.Message)
			if deviceResp.Data != nil {
				logrus.Infof("  - Data: %+v", deviceResp.Data)
			}
		}

		if dw.onMouseTypeChanged != nil {
			if startedMouseMode != "" {
				dw.preferredMouseMode = startedMouseMode
				dw.onMouseTypeChanged(startedMouseMode)
			} else {
				for _, req := range deviceRequests {
					if req.Device == "mouse" {
						dw.preferredMouseMode = normalizeMouseMode(req.Type)
						dw.onMouseTypeChanged(dw.preferredMouseMode)
						break
					}
				}
			}
		}

		reEnableButtons = false

		// Поскольку StartDevicesBatch теперь синхронно дожидается завершения (до 30 сек),
		// мы сразу обновляем UI после возврата, pollMountStatus больше не нужен.
		dw.updateUIAsync(func() {
			dw.setAPIMountInProgress(false)
			dw.setUserOperationInFlight(false)
			dw.setMountingStateByExportNames(mountingExportNames, false)
			dw.updateStatus()
			dw.setButtonsEnabled(true) // разблокируем Mount после завершения монтирования
		})

		// Сразу обновляем данные один раз после завершения
		dw.refreshDataSync()

		logrus.Infof("✅ Запрос на монтирование gadget=%d выполнен", len(deviceRequests))
	}()
}

// handleUnmount обрабатывает размонтирование.
func (dw *DiskWidget) handleUnmount() {
	dw.setUserOperationInFlight(true)
	dw.setButtonsEnabled(false)
	if dw.usbClient == nil {
		logrus.Warn("⚠️ USB клиент не инициализирован")
		if dw.window != nil {
			view.ShowErrorDialog(fmt.Errorf("%s", i18n.Current.ErrorNotConnected), dw.window)
		}
		dw.setUserOperationInFlight(false)
		dw.setButtonsEnabled(true)
		return
	}

	var mountedDrives []DriveItem
	var mountedIndices []int
	for i, drive := range dw.allDrives {
		if drive.IsMounted {
			if !drive.IsVideo {
				mountedDrives = append(mountedDrives, drive)
				mountedIndices = append(mountedIndices, i)
			}
		}
	}

	videoMounted := false
	for _, drive := range dw.allDrives {
		if drive.IsVideo && drive.IsMounted {
			videoMounted = true
			break
		}
	}

	if len(mountedDrives) == 0 && !videoMounted {
		logrus.Warnf("⚠️ Нет подключенных устройств для размонтирования")
		if dw.window != nil {
			dialog.ShowInformation(i18n.Current.Information, i18n.Current.NoMountedDevices, dw.window)
		}
		dw.setUserOperationInFlight(false)
		dw.setButtonsEnabled(true)
		return
	}

	selectedIndices := make(map[int]bool)
	selectedMountedVideo := false
	dw.selectedItemsMu.RLock()
	for id, selected := range dw.selectedItems {
		if selected && id < len(dw.allDrives) && dw.allDrives[id].IsMounted {
			if dw.allDrives[id].IsVideo {
				selectedMountedVideo = true
			} else {
				selectedIndices[id] = true
			}
		}
	}
	dw.selectedItemsMu.RUnlock()

	confirmMsg := i18n.Current.UnmountAllConfirm
	unmountAll := len(selectedIndices) == 0 && !selectedMountedVideo
	if !unmountAll {
		confirmMsg = i18n.Current.UnmountSelectedConfirm
	}

	finalUnmountAll := unmountAll
	finalSelectedIndices := make(map[int]bool)
	for k, v := range selectedIndices {
		finalSelectedIndices[k] = v
	}
	finalMountedDrives := make([]DriveItem, len(mountedDrives))
	copy(finalMountedDrives, mountedDrives)
	finalMountedIndices := make([]int, len(mountedIndices))
	copy(finalMountedIndices, mountedIndices)

	view.ShowConfirmYesLeft(i18n.Current.Confirmation, confirmMsg, func(ok bool) {
		if !ok {
			dw.setUserOperationInFlight(false)
			dw.setButtonsEnabled(true)
			return
		}
		go dw.doUnmount(finalUnmountAll, finalSelectedIndices, finalMountedDrives, finalMountedIndices)
	}, dw.window)
}

// doUnmount выполняет размонтирование.
func (dw *DiskWidget) doUnmount(unmountAll bool, selectedIndices map[int]bool, mountedDrives []DriveItem, mountedIndices []int) {
	// Снимаем выделение сразу для визуального отклика
	dw.updateUIAsync(func() {
		dw.selectedItemsMu.Lock()
		if unmountAll {
			dw.selectedItems = make(map[int]bool)
		} else {
			for idx := range selectedIndices {
				delete(dw.selectedItems, idx)
			}
		}
		dw.selectedItemsMu.Unlock()
		dw.updateButtons()
		dw.requestDevicesRefresh()
	})

	defer func() {
		dw.updateUIAsync(func() {
			dw.setUserOperationInFlight(false)
			dw.setButtonsEnabled(true)
		})
	}()

	if unmountAll {
		if dw.onVideoDisconnect != nil {
			dw.onVideoDisconnect()
		}
		dw.updateStatusAsync(i18n.Current.StoppingAllDevices)
		if _, err := executeDeviceBatch(dw.usbClient, dw.startDevicesWithRetry, nil, false); err != nil {
			logrus.Warnf("⚠️ Ошибка остановки устройств: %v", err)
		} else {
			logrus.Infof("✅ Все устройства остановлены")
		}
		dw.stopNBDAndCleanup(mountedDrives, true)
	} else {
		videoSelected := false
		for idx := range selectedIndices {
			if idx < len(dw.allDrives) && dw.allDrives[idx].IsVideo {
				videoSelected = true
				break
			}
		}
		if videoSelected && dw.onVideoDisconnect != nil {
			dw.onVideoDisconnect()
		}

		keepIndices := make(map[int]bool)
		for _, idx := range mountedIndices {
			if !selectedIndices[idx] {
				keepIndices[idx] = true
			}
		}

		drivesToUnmount := make([]DriveItem, 0, len(selectedIndices))
		for idx := range selectedIndices {
			if idx < len(dw.allDrives) {
				drivesToUnmount = append(drivesToUnmount, dw.allDrives[idx])
			}
		}

		if len(keepIndices) == 0 {
			dw.updateStatusAsync(i18n.Current.StoppingAllDevices)
			if _, err := executeDeviceBatch(dw.usbClient, dw.startDevicesWithRetry, nil, false); err != nil {
				logrus.Warnf("⚠️ Ошибка остановки устройств: %v", err)
			}
			dw.stopNBDAndCleanup(drivesToUnmount, true)
		} else {
			var deviceRequests []models.DeviceStartRequest
			mountingExportNames := make(map[string]bool)

			for idx := range keepIndices {
				if idx >= len(dw.allDrives) {
					continue
				}
				req, err := dw.buildDeviceRequestForDrive(dw.allDrives[idx], true)
				if err != nil {
					logrus.Warnf("⚠️ Не удалось построить запрос для %s: %v", dw.allDrives[idx].Name, err)
					continue
				}
				deviceRequests = append(deviceRequests, *req)

				// Добавляем имя для отслеживания состояния Mounting
				drive := dw.allDrives[idx]
				if drive.DiskInfo != nil {
					mountingExportNames[drive.DiskInfo.Name] = true
				} else if drive.LocalDrive != nil {
					mountingExportNames[drive.LocalDrive.Name] = true
				} else {
					mountingExportNames[drive.Name] = true
				}
			}
			
			if len(deviceRequests) > 0 {
				batchRequest := models.DeviceStartBatchRequest(deviceRequests)
				
				dw.updateUIAsync(func() {
					dw.setMountingStateByExportNames(mountingExportNames, true)
					dw.setAPIMountInProgress(true)
					dw.requestDevicesRefresh()
				})
				
				dw.updateStatusAsync(i18n.Current.StoppingAllDevices)
				if _, err := executeDeviceBatch(dw.usbClient, dw.startDevicesWithRetry, batchRequest, false); err != nil {
					logrus.Warnf("⚠️ Ошибка переподключения устройств: %v", err)
				}
				
				dw.updateUIAsync(func() {
					dw.setMountingStateByExportNames(mountingExportNames, false)
					dw.setAPIMountInProgress(false)
				})
			}
			dw.stopNBDAndCleanup(drivesToUnmount, false)
		}
	}

	logrus.Infof("⏳ [UNMOUNT] Waiting for operation to complete...")
	
	// Ожидание здесь больше не нужно, так как StartDevicesBatch сам ждёт завершения на сервере.
	// Оставляем маленькую паузу только для плавности UI, если нужно.
	time.Sleep(200 * time.Millisecond)

	dw.updateUIAsync(func() {
		// Сбрасываем сигнатуру чтобы форсировать обновление UI
		dw.lastDrivesTraceSig = ""
		dw.requestDevicesRefresh()
	})

	// Сразу обновляем данные один раз после завершения
	dw.refreshDataSync()

	if dw.updateStatus != nil {
		dw.updateStatus()
	}
	dw.updateStatusAsync(i18n.Current.AllDevicesUnmounted)
	logrus.Infof("✅ Размонтирование завершено")
}

// stopNBDAndCleanup останавливает NBD серверы.
func (dw *DiskWidget) stopNBDAndCleanup(drives []DriveItem, stopAll bool) {
	dw.updateStatusAsync(i18n.Current.StoppingNBDServers)
	toStop := make(map[string]bool)
	if stopAll {
		for exportName := range dw.nbdServers {
			toStop[exportName] = true
		}
	} else {
		for _, drive := range drives {
			if drive.DiskInfo != nil {
				exportName := drive.DiskInfo.Name
				if _, exists := dw.nbdServers[exportName]; exists {
					toStop[exportName] = true
				}
			}
		}
	}
	for exportName := range toStop {
		if nbdServer, exists := dw.nbdServers[exportName]; exists {
			if nbdServer.IsRunning() {
				if err := nbdServer.Stop(); err != nil {
					logrus.Warnf("⚠️ Ошибка остановки NBD сервера %s: %v", exportName, err)
				}
			}
			delete(dw.nbdServers, exportName)
		}
	}
	if stopAll {
		dw.nbdServers = make(map[string]service.NBDRunner)
	}
	if runtime.GOOS == "android" && dw.safHelper != nil {
		for _, drive := range drives {
			if drive.DiskInfo != nil && strings.HasPrefix(drive.DiskInfo.URI, "content://") {
				_ = dw.safHelper.CloseFD(drive.DiskInfo.URI)
			}
		}
	}
}

// buildDeviceRequestForDrive строит DeviceStartRequest для drive.
func (dw *DiskWidget) buildDeviceRequestForDrive(drive DriveItem, useExistingNBD bool) (*models.DeviceStartRequest, error) {
	if drive.Source == "keyboard" {
		req := newKeyboardStartRequest()
		return &req, nil
	}
	if drive.Source == "mouse" {
		req := newMouseStartRequest(drive.MouseType)
		return &req, nil
	}
	if drive.Source == "rndis" {
		req := newRNDISStartRequest(drive.RNDISMode)
		return &req, nil
	}
	if drive.Source == "api" && drive.LocalDrive != nil {
		if drive.LocalDrive.SourceType == "mtp" {
			return &models.DeviceStartRequest{
				Device: "mtp", Server: drive.LocalDrive.Name,
				VendorID: "0x1d6b", ProductID: "0x0104", ProductName: "BackupDrive", Manufacturer: "USBridge",
			}, nil
		}
		return &models.DeviceStartRequest{
			Device: "drive", Server: drive.LocalDrive.Name,
			VendorID: "0x1d6b", ProductID: "0x0104", ProductName: "USBridge Local CD-ROM", Manufacturer: "USBridge",
		}, nil
	}
	if (drive.Source == "local" || drive.Source == "user") && drive.DiskInfo != nil && useExistingNBD {
		exportName := drive.DiskInfo.Name
		nbdServer, exists := dw.nbdServers[exportName]
		if !exists || !nbdServer.IsRunning() {
			return nil, fmt.Errorf("NBD сервер для %s не найден или не запущен", exportName)
		}
		status := nbdServer.GetServerStatus()
		portVal, ok := status["server_port"]
		if !ok {
			return nil, fmt.Errorf("порт NBD сервера %s не найден", exportName)
		}
		var port int
		switch p := portVal.(type) {
		case int:
			port = p
		case int64:
			port = int(p)
		case float64:
			port = int(p)
		default:
			return nil, fmt.Errorf("неверный тип порта для %s", exportName)
		}
		localIP, err := dw.getLocalIP()
		if err != nil {
			return nil, err
		}
		exportNameForAPI := nbdServer.NBDExportNameForAPI()
		return &models.DeviceStartRequest{
			Device:                  "drive",
			Server:                  localIP,
			Port:                    port,
			ExportName:              exportNameForAPI,
			NBDHandshakeEmptyExport: nbdServer.NBDHandshakeEmptyExport(),
			ReadOnly:                drive.ReadOnly,
			VendorID:                "0x1d6b",
			ProductID:               "0x0104",
			ProductName:             "USBridge NBD CD-ROM",
			Manufacturer:            "USBridge",
		}, nil
	}
	return nil, fmt.Errorf("неизвестный тип устройства: %s", drive.Name)
}

// countSelectedItems возвращает количество выбранных элементов.
func (dw *DiskWidget) countSelectedItems() int {
	dw.selectedItemsMu.RLock()
	defer dw.selectedItemsMu.RUnlock()
	count := 0
	for id, selected := range dw.selectedItems {
		if selected && id < len(dw.allDrives) {
			count++
		}
	}
	return count
}

func (dw *DiskWidget) countSelectedGadgetItems() int {
	dw.selectedItemsMu.RLock()
	defer dw.selectedItemsMu.RUnlock()
	count := 0
	for id, selected := range dw.selectedItems {
		if selected && id < len(dw.allDrives) && !dw.allDrives[id].IsVideo {
			count++
		}
	}
	return count
}

// updateButtons обновляет состояние кнопок.
func (dw *DiskWidget) updateButtons() {
	dw.selectedItemsMu.RLock()
	selectedCount := 0
	selectedNotMountedCount := 0
	selectedGadgetNotMountedCount := 0
	mountedCount := 0

	for id, selected := range dw.selectedItems {
		if selected && id < len(dw.allDrives) && !dw.allDrives[id].IsVideo {
			selectedCount++
			if !dw.allDrives[id].IsMounted {
				selectedNotMountedCount++
			}
			if !dw.allDrives[id].IsVideo && !dw.allDrives[id].IsMounted {
				selectedGadgetNotMountedCount++
			}
		}
	}
	dw.selectedItemsMu.RUnlock()
	for _, drive := range dw.allDrives {
		if drive.IsMounted && !drive.IsVideo {
			mountedCount++
		}
	}

	hasMountedDevices := mountedCount > 0
	videoMounted := false
	for _, drive := range dw.allDrives {
		if drive.IsVideo && drive.IsMounted {
			videoMounted = true
			break
		}
	}
	hasMountedDevices = hasMountedDevices || videoMounted

	canAdd := selectedNotMountedCount > 0 && (mountedCount+selectedGadgetNotMountedCount) <= MaxDevicesToMount
	controlsLocked := dw.controlsLocked()

	fyne.Do(func() {
		if dw.unmountBtn == nil || dw.mountBtn == nil {
			return
		}

		disconnectLabel := i18n.Current.DisconnectButton
		if selectedCount == 0 && hasMountedDevices {
			disconnectLabel = i18n.Current.DisconnectAllButton
		}
		dw.unmountBtn.SetText(disconnectLabel)
		if dw.compactUnmountBtn != nil {
			dw.compactUnmountBtn.SetLabel(disconnectLabel)
		}

		if selectedNotMountedCount == 0 {
			dw.mountBtn.Hide()
			if dw.compactMountBtn != nil {
				dw.compactMountBtn.Hide()
				dw.compactMountBtn.Disable()
			}
		} else {
			dw.mountBtn.Show()
			if dw.compactMountBtn != nil {
				dw.compactMountBtn.Show()
				if canAdd && !controlsLocked {
					dw.compactMountBtn.Enable()
				} else {
					dw.compactMountBtn.Disable()
				}
			}
		}

		if hasMountedDevices || selectedCount > 0 {
			dw.unmountBtn.Show()
			if controlsLocked {
				dw.unmountBtn.Disable()
			} else {
				dw.unmountBtn.Enable()
			}
			if dw.compactUnmountBtn != nil {
				dw.compactUnmountBtn.Show()
				if controlsLocked {
					dw.compactUnmountBtn.Disable()
				} else {
					dw.compactUnmountBtn.Enable()
				}
			}
		} else {
			dw.unmountBtn.Hide()
			if dw.compactUnmountBtn != nil {
				dw.compactUnmountBtn.Hide()
				dw.compactUnmountBtn.Disable()
			}
		}

		if selectedCount == 0 {
			dw.mountBtn.Disable()
			if dw.onButtonsChanged != nil {
				dw.onButtonsChanged()
			}
			return
		}

		if canAdd && !controlsLocked {
			dw.mountBtn.Enable()
		} else {
			dw.mountBtn.Disable()
		}
		if dw.onButtonsChanged != nil {
			dw.onButtonsChanged()
		}
	})
}

func (dw *DiskWidget) reconfigureMountedDevicesForMouseMode(newMode string) {
	if dw.usbClient == nil {
		dw.showErrorAsync(fmt.Errorf("%s", i18n.Current.ErrorNotConnected))
		return
	}

	dw.setUserOperationInFlight(true)
	dw.setButtonsEnabled(false)

	go func() {
		reEnableButtons := true
		defer func() {
			if reEnableButtons {
				dw.updateUIAsync(func() {
					dw.setUserOperationInFlight(false)
					dw.setButtonsEnabled(true)
				})
			}
		}()

		var deviceRequests []models.DeviceStartRequest
		mountingExportNames := make(map[string]bool)
		hasStorageRequests := false
		mouseIncluded := false

		for _, drive := range dw.allDrives {
			if !drive.IsMounted || drive.IsVideo {
				continue
			}

			current := drive
			if current.IsMouse {
				current.MouseType = newMode
			}

			req, err := dw.buildDeviceRequestForDrive(current, true)
			if err != nil {
				dw.showErrorAsync(fmt.Errorf("failed to rebuild mounted gadget config for %s: %w", current.Name, err))
				return
			}
			deviceRequests = append(deviceRequests, *req)
			if current.IsMouse {
				mouseIncluded = true
			}

			if current.IsKeyboard || current.IsMouse || current.IsRNDIS {
				continue
			}
			hasStorageRequests = true
			switch {
			case current.DiskInfo != nil:
				mountingExportNames[current.DiskInfo.Name] = true
			case current.LocalDrive != nil:
				mountingExportNames[current.LocalDrive.Name] = true
			default:
				mountingExportNames[current.Name] = true
			}
		}

		if !mouseIncluded && dw.isMouseMountedActual() {
			mouseReq := newMouseStartRequest(newMode)
			deviceRequests = append(deviceRequests, mouseReq)
			mouseIncluded = true
		}

		if len(deviceRequests) == 0 {
			dw.showErrorAsync(fmt.Errorf("no mounted devices available for gadget reconfiguration"))
			return
		}

		if hasStorageRequests {
			dw.updateUIAsync(func() {
				dw.setMountingStateByExportNames(mountingExportNames, true)
				dw.setAPIMountInProgress(true)
				dw.requestDevicesRefresh()
			})
		}

		dw.updateStatusAsync("Reconfiguring USB gadget...")
		if _, err := executeDeviceBatch(dw.usbClient, dw.startDevicesWithRetry, models.DeviceStartBatchRequest(deviceRequests), false); err != nil {
			dw.showErrorAsync(fmt.Errorf("failed to reconfigure mouse mode: %w", err))
			if hasStorageRequests {
				dw.updateUIAsync(func() {
					dw.setMountingStateByExportNames(mountingExportNames, false)
					dw.setAPIMountInProgress(false)
					dw.requestDevicesRefresh()
				})
			}
			return
		}

		if dw.onMouseTypeChanged != nil {
			dw.preferredMouseMode = newMode
			dw.onMouseTypeChanged(newMode)
		}
		if dw.onMouseModeReconfigured != nil {
			dw.onMouseModeReconfigured()
		}

		if hasStorageRequests {
			dw.updateUIAsync(func() {
				dw.setMountingStateByExportNames(mountingExportNames, false)
				dw.setAPIMountInProgress(false)
				dw.requestDevicesRefresh()
			})
		}

		// Сразу обновляем данные один раз после завершения
		dw.refreshDataSync()
	}()
}
