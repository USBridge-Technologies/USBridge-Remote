package controller

import (
	"context"
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

// beginOperation блокирует UI перед началом операции монтирования/размонтирования.
// Вызывается из потока Fyne.
func (dw *DiskWidget) beginOperation() {
	dw.userOperationInFlight.Store(true)
	dw.setButtonsEnabled(false)
}

// endOperation завершает операцию монтирования/размонтирования.
// Делает HTTP-запросы для получения свежих данных, затем в ОДНОМ fyne.Do:
//   - обновляет данные об устройствах
//   - сбрасывает все IsMounting-флаги
//   - безусловно сбрасывает userOperationInFlight и apiMountInProgress
//   - обновляет статус и кнопки
//
// Все изменения в одном fyne.Do — никакой другой асинхронный обновитель не может
// влезть между очисткой флагов и updateButtons(), что устраняет постоянную блокировку UI.
// Безопасно вызывать из любой горутины; используется как defer в горутинах операций.
func (dw *DiskWidget) endOperation() {
	var newMounted []*models.DeviceInfo
	var newLocalDrives []*models.LocalDrive
	var newAgentOS string

	if dw.usbClient != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if deviceInfo, err := dw.usbClient.GetDeviceInfoWithContext(ctx); err == nil {
			newMounted = make([]*models.DeviceInfo, len(deviceInfo.Devices))
			for i := range deviceInfo.Devices {
				newMounted[i] = &deviceInfo.Devices[i]
			}
			newAgentOS = deviceInfo.AgentOS
		} else {
			logrus.Errorf("endOperation: GetDeviceInfo: %v", err)
		}

		if localDrives, err := dw.usbClient.GetLocalDrives(); err == nil {
			newLocalDrives = make([]*models.LocalDrive, len(localDrives.Drives))
			for i := range localDrives.Drives {
				newLocalDrives[i] = &localDrives.Drives[i]
			}
		} else {
			logrus.Errorf("endOperation: GetLocalDrives: %v", err)
		}
	}

	// Все изменения в одном fyne.Do — атомарная с точки зрения event-loop операция.
	// Это исключает гонку: поздний ответ сервера (apiMountInProgress=true) в другом
	// fyne.Do не может оказаться между сбросом флагов и updateButtons().
	fyne.Do(func() {
		if newMounted != nil {
			dw.mountedDevices = newMounted
			dw.agentOS = newAgentOS
		}
		if newLocalDrives != nil {
			dw.localDrives = newLocalDrives
		}
		// Сбрасываем все анимации монтирования
		for i := range dw.allDrives {
			dw.allDrives[i].IsMounting = false
		}
		// Безусловно сбрасываем флаги ДО вызова updateDevicesStatus/updateButtons,
		// чтобы controlsLocked() вернул false и кнопки гарантированно включились.
		dw.userOperationInFlight.Store(false)
		dw.apiMountInProgress.Store(false)
		// Пересобираем статус и список из свежих данных
		dw.updateDevicesStatus() // обновляет IsMounted, вызывает updateButtons
		dw.lastDrivesTraceSig = ""
		dw.requestDevicesRefresh()
	})
	// Refresh the main window header status icons (keyboard/mouse/gamepad/rndis).
	if dw.updateStatus != nil {
		go dw.updateStatus()
	}
}

// handleMount обрабатывает нажатие кнопки Connect.
func (dw *DiskWidget) handleMount() {
	logrus.Infof("📍 [MOUNT] handleMount вызван, GOOS: %s", runtime.GOOS)

	if dw.usbClient == nil {
		if dw.window != nil {
			view.ShowErrorDialog(fmt.Errorf("%s", i18n.Current.ErrorNotConnected), dw.window)
		}
		return
	}

	// Собираем выбранные несмонтированные не-видео/не-аудио устройства
	var selectedDrives []DriveItem
	dw.selectedItemsMu.RLock()
	for id, selected := range dw.selectedItems {
		if selected && id < len(dw.allDrives) {
			d := dw.allDrives[id]
			if !d.IsMounted && !d.IsVideo && !d.IsAudio {
				selectedDrives = append(selectedDrives, d)
			}
		}
	}
	dw.selectedItemsMu.RUnlock()

	if len(selectedDrives) == 0 {
		if dw.window != nil {
			view.ShowErrorDialog(fmt.Errorf("%s", i18n.Current.SelectDevicesToMount), dw.window)
		}
		return
	}

	mountedGadgetCount := 0
	for _, d := range dw.allDrives {
		if d.IsMounted && !d.IsVideo {
			mountedGadgetCount++
		}
	}
	if mountedGadgetCount+len(selectedDrives) > MaxDevicesToMount {
		if dw.window != nil {
			dialog.ShowInformation(i18n.Current.Information, i18n.Current.MaxDevicesReached, dw.window)
		}
		return
	}

	logrus.Infof("📁 [MOUNT] смонтировано: %d, добавляем: %d", mountedGadgetCount, len(selectedDrives))

	// XInput геймпад несовместим с клавиатурой/мышью в одном композитном устройстве:
	// Windows не инициализирует остальные HID-интерфейсы под Xbox VID/PID.
	// Проверяем как в новом выборе, так и среди уже смонтированных устройств.
	if dw.window != nil {
		hasXInputSelected := false
		hasHIDSelected := false
		for _, d := range selectedDrives {
			if d.IsGamepad && normalizeGamepadMode(d.GamepadMode) == gamepadModeXInput {
				hasXInputSelected = true
			}
			if d.IsKeyboard || d.IsMouse {
				hasHIDSelected = true
			}
		}
		hasHIDMounted := false
		for _, d := range dw.allDrives {
			if d.IsMounted && (d.IsKeyboard || d.IsMouse) {
				hasHIDMounted = true
			}
		}
		if hasXInputSelected && (hasHIDSelected || hasHIDMounted) {
			view.ShowErrorDialog(fmt.Errorf("%s", i18n.Current.XInputIncompatibleWithHID), dw.window)
			return
		}
	}

	// Прогресс-диалог для файлов из Google Drive
	var progressDialog dialog.Dialog
	for _, d := range selectedDrives {
		if (d.Source == "local" || d.Source == "user") && d.DiskInfo != nil &&
			strings.Contains(d.DiskInfo.Path, "com.google.android.apps.docs.storage") {
			pb := widget.NewProgressBarInfinite()
			content := container.NewVBox(
				widget.NewLabel(i18n.Current.LoadingFromCloud), widget.NewLabel(""),
				widget.NewLabel(i18n.Current.CloudFilesDetected),
				widget.NewLabel(i18n.Current.AndroidBuffering),
				widget.NewLabel(i18n.Current.MayTake30Seconds),
				widget.NewLabel(""), pb, widget.NewLabel(""),
				widget.NewLabel(i18n.Current.PleaseWait),
			)
			progressDialog = dialog.NewCustomWithoutButtons(i18n.Current.PreparingToMount, content, dw.window)
			progressDialog.Show()
			break
		}
	}

	dw.beginOperation()

	go func() {
		defer func() {
			if progressDialog != nil {
				fyne.Do(progressDialog.Hide)
			}
			dw.endOperation()
		}()

		// Формируем запросы, запускаем NBD-серверы
		var deviceRequests []models.DeviceStartRequest
		startedMouseMode := ""

		for _, sel := range selectedDrives {
			req, mouseMode, err := dw.buildMountRequest(sel)
			if err != nil {
				dw.showErrorAsync(fmt.Errorf("ошибка подготовки %s: %v", sel.Name, err))
				return
			}
			if mouseMode != "" {
				startedMouseMode = mouseMode
			}
			if req != nil {
				deviceRequests = append(deviceRequests, *req)
			}
		}

		if len(deviceRequests) == 0 {
			dw.showErrorAsync(fmt.Errorf("не удалось подготовить устройства для монтирования"))
			return
		}

		// Определяем имена NBD-экспортов для анимации монтирования
		mountingExportNames := dw.nbdExportNamesForRequests(deviceRequests)

		// Ждём готовности NBD-серверов
		if err := dw.waitForNBDServers(30 * time.Second); err != nil {
			dw.showErrorAsync(err)
			return
		}

		// Проверяем FRP-туннель для NBD
		if len(dw.nbdServers) > 0 && dw.frpService != nil && !dw.frpService.IsRunning() {
			dw.showErrorAsync(fmt.Errorf("FRP туннель не активен — переподключитесь перед монтированием NBD"))
			return
		}
		if len(dw.nbdServers) > 0 {
			logrus.Infof("⏱️ [MOUNT-NBD] Ожидание 1 с (стабилизация FRP/туннеля)")
			time.Sleep(1 * time.Second)
		}

		// Показываем анимацию монтирования и снимаем выделение
		fyne.Do(func() {
			dw.setMountingStateByExportNames(mountingExportNames, true)
			dw.setAPIMountInProgress(true)
			dw.selectedItemsMu.Lock()
			dw.selectedItems = make(map[int]bool)
			dw.selectedItemsMu.Unlock()
			dw.requestDevicesRefresh()
		})

		// Вызываем API
		logrus.Infof("🚀 [MOUNT-API] Запуск %d устройств (Merge)", len(deviceRequests))
		for i, req := range deviceRequests {
			logrus.Infof("   📤 [MOUNT-API] [%d] device=%s server=%s port=%d export=%s ro=%v",
				i+1, req.Device, req.Server, req.Port, req.ExportName, req.ReadOnly)
		}
		dw.updateStatusAsync("Запуск устройств...")
		if resp, err := executeDeviceBatch(dw.usbClient, dw.startDevicesWithRetry, models.DeviceStartBatchRequest(deviceRequests), true); err != nil {
			logrus.Errorf("❌ [MOUNT-API] Ошибка: %v", err)
			dw.showErrorAsync(fmt.Errorf("ошибка запуска устройств: %v", err))
			return
		} else {
			logrus.Infof("✅ [MOUNT-API] Success=%v Message=%s", resp.Success, resp.Message)
		}

		// Сохраняем режим мыши
		if dw.onMouseTypeChanged != nil && startedMouseMode != "" {
			dw.preferredMouseMode = startedMouseMode
			dw.onMouseTypeChanged(startedMouseMode)
		}

		logrus.Infof("✅ [MOUNT] Монтирование завершено, ждём endOperation()")
	}()
}

// buildMountRequest строит DeviceStartRequest для монтируемого устройства.
// Для NBD-устройств запускает NBD-сервер.
// Возвращает (запрос, mouseType, ошибка).
func (dw *DiskWidget) buildMountRequest(sel DriveItem) (*models.DeviceStartRequest, string, error) {
	switch sel.Source {
	case "keyboard":
		req := newKeyboardStartRequest()
		return &req, "", nil
	case "mouse":
		mouseType := normalizeMouseMode(sel.MouseType)
		req := newMouseStartRequest(mouseType)
		return &req, mouseType, nil
	case "rndis":
		rndisMode := normalizeRNDISMode(sel.RNDISMode)
		req := newRNDISStartRequest(rndisMode)
		return &req, "", nil
	case "gamepad":
		req := newGamepadStartRequest(sel.GamepadMode, sel.GamepadVendorID, sel.GamepadProductID)
		return &req, "", nil
	case "usbaudio":
		mode := sel.USBAudioMode
		if mode == "" {
			mode = "uac1"
		}
		req := models.DeviceStartRequest{
			Device: "usbaudio", Type: mode,
		}
		return &req, "", nil
	case "api":
		if sel.LocalDrive == nil {
			return nil, "", fmt.Errorf("LocalDrive == nil для api-устройства: %s", sel.Name)
		}
		if sel.LocalDrive.SourceType == "mtp" {
			return &models.DeviceStartRequest{
				Device: "mtp", Server: sel.LocalDrive.Name,
			}, "", nil
		}
		return &models.DeviceStartRequest{
			Device: "drive", Server: sel.LocalDrive.Name,
		}, "", nil
	case "local", "user":
		if sel.DiskInfo == nil {
			return nil, "", fmt.Errorf("DiskInfo == nil для %s-устройства: %s", sel.Source, sel.Name)
		}
		localIP, err := dw.getLocalIP()
		if err != nil {
			return nil, "", fmt.Errorf("ошибка получения локального IP: %v", err)
		}
		nbdPort, err := dw.getAvailablePort()
		if err != nil {
			return nil, "", fmt.Errorf("ошибка получения свободного порта: %v", err)
		}
		exportName := sel.DiskInfo.Name
		if existing, ok := dw.nbdServers[exportName]; ok {
			if existing.IsRunning() {
				_ = existing.Stop()
			}
			delete(dw.nbdServers, exportName)
		}
		nbdServer, err := dw.startNBDServer(sel.DiskInfo, nbdPort, exportName, sel.ReadOnly)
		if err != nil {
			return nil, "", fmt.Errorf("ошибка запуска NBD сервера: %v", err)
		}
		dw.nbdServers[exportName] = nbdServer
		return &models.DeviceStartRequest{
			Device:                  "drive",
			Server:                  localIP,
			Port:                    nbdPort,
			ExportName:              nbdServer.NBDExportNameForAPI(),
			NBDHandshakeEmptyExport: nbdServer.NBDHandshakeEmptyExport(),
			ReadOnly:                sel.ReadOnly,
		}, "", nil
	}
	return nil, "", fmt.Errorf("неизвестный тип устройства: %s (source=%s)", sel.Name, sel.Source)
}

// nbdExportNamesForRequests возвращает имена экспортов NBD по запросам — для анимации монтирования.
func (dw *DiskWidget) nbdExportNamesForRequests(requests []models.DeviceStartRequest) map[string]bool {
	names := make(map[string]bool)
	for _, req := range requests {
		if req.Device != "drive" || req.Port == 0 {
			continue
		}
		for name, srv := range dw.nbdServers {
			if !srv.IsRunning() {
				continue
			}
			st := srv.GetServerStatus()
			p, ok := st["server_port"]
			if !ok {
				continue
			}
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
				names[name] = true
				break
			}
		}
	}
	return names
}

// waitForNBDServers сигнализирует NBD-серверам принимать соединения и ждёт их готовности.
func (dw *DiskWidget) waitForNBDServers(timeout time.Duration) error {
	if len(dw.nbdServers) == 0 {
		return nil
	}

	logrus.Infof("📡 [NBD] Сигнализируем готовность %d серверам", len(dw.nbdServers))
	for name, srv := range dw.nbdServers {
		logrus.Infof("  📡 [NBD] SignalReady: %s", name)
		srv.SignalReady()
	}

	remaining := make(map[string]service.NBDRunner, len(dw.nbdServers))
	for k, v := range dw.nbdServers {
		remaining[k] = v
	}

	deadline := time.After(timeout)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-deadline:
			names := make([]string, 0, len(remaining))
			for n := range remaining {
				names = append(names, n)
			}
			return fmt.Errorf("таймаут ожидания NBD серверов: %v", names)
		case <-ticker.C:
			for name, srv := range remaining {
				select {
				case <-srv.WaitReady():
					logrus.Infof("✅ [NBD] Сервер %s готов", name)
					delete(remaining, name)
				default:
				}
			}
			if len(remaining) == 0 {
				logrus.Infof("✅ [NBD] Все серверы готовы")
				return nil
			}
		}
	}
}

// handleUnmount обрабатывает нажатие кнопки Disconnect.
func (dw *DiskWidget) handleUnmount() {
	if dw.usbClient == nil {
		if dw.window != nil {
			view.ShowErrorDialog(fmt.Errorf("%s", i18n.Current.ErrorNotConnected), dw.window)
		}
		return
	}

	// Собираем смонтированные не-видео/не-аудио устройства
	var mountedDrives []DriveItem
	var mountedIndices []int
	for i, d := range dw.allDrives {
		if d.IsMounted && !d.IsVideo && !d.IsAudio {
			mountedDrives = append(mountedDrives, d)
			mountedIndices = append(mountedIndices, i)
		}
	}
	videoMounted := false
	audioMounted := false
	for _, d := range dw.allDrives {
		if d.IsVideo && d.IsMounted {
			videoMounted = true
		}
		if d.IsAudio && d.IsMounted {
			audioMounted = true
		}
	}

	if len(mountedDrives) == 0 && !videoMounted && !audioMounted {
		if dw.window != nil {
			dialog.ShowInformation(i18n.Current.Information, i18n.Current.NoMountedDevices, dw.window)
		}
		return
	}

	// Определяем что именно отключаем (выбранное или всё)
	selectedIndices := make(map[int]bool)
	selectedMountedVideo := false
	dw.selectedItemsMu.RLock()
	for id, sel := range dw.selectedItems {
		if sel && id < len(dw.allDrives) && dw.allDrives[id].IsMounted {
			if dw.allDrives[id].IsVideo {
				selectedMountedVideo = true
			} else {
				selectedIndices[id] = true
			}
		}
	}
	dw.selectedItemsMu.RUnlock()

	unmountAll := len(selectedIndices) == 0 && !selectedMountedVideo
	confirmMsg := i18n.Current.UnmountAllConfirm
	if !unmountAll {
		confirmMsg = i18n.Current.UnmountSelectedConfirm
	}

	// Снапшот состояния для горутины
	snapMountedDrives := make([]DriveItem, len(mountedDrives))
	copy(snapMountedDrives, mountedDrives)
	snapMountedIndices := make([]int, len(mountedIndices))
	copy(snapMountedIndices, mountedIndices)
	snapSelected := make(map[int]bool, len(selectedIndices))
	for k, v := range selectedIndices {
		snapSelected[k] = v
	}

	view.ShowConfirmYesLeft(i18n.Current.Confirmation, confirmMsg, func(ok bool) {
		if !ok {
			return
		}
		dw.beginOperation()
		// Сразу снимаем выделение для визуального отклика
		dw.selectedItemsMu.Lock()
		if unmountAll {
			dw.selectedItems = make(map[int]bool)
		} else {
			for idx := range snapSelected {
				delete(dw.selectedItems, idx)
			}
		}
		dw.selectedItemsMu.Unlock()
		dw.updateButtons()
		dw.requestDevicesRefresh()

		go dw.doUnmount(unmountAll, snapSelected, snapMountedDrives, snapMountedIndices)
	}, dw.window)
}

// doUnmount выполняет размонтирование в горутине.
func (dw *DiskWidget) doUnmount(unmountAll bool, selectedIndices map[int]bool, mountedDrives []DriveItem, mountedIndices []int) {
	defer dw.endOperation() // ВСЕГДА сбрасывает все флаги и обновляет UI

	if unmountAll {
		if dw.onVideoDisconnect != nil {
			dw.onVideoDisconnect()
		}
		if dw.onAudioDisconnect != nil {
			dw.onAudioDisconnect()
		}
		dw.updateStatusAsync(i18n.Current.StoppingAllDevices)
		if _, err := executeDeviceBatch(dw.usbClient, dw.startDevicesWithRetry, nil, false); err != nil {
			logrus.Warnf("⚠️ [UNMOUNT-ALL] Ошибка остановки: %v", err)
		} else {
			logrus.Infof("✅ [UNMOUNT-ALL] Все устройства остановлены")
		}
		dw.stopNBDAndCleanup(mountedDrives, true)
		dw.updateStatusAsync(i18n.Current.AllDevicesUnmounted)
		return
	}

	// Частичное размонтирование: определяем что оставить, что удалить
	keepIndices := make(map[int]bool)
	for _, idx := range mountedIndices {
		if !selectedIndices[idx] {
			keepIndices[idx] = true
		}
	}

	drivesToUnmount := make([]DriveItem, 0, len(selectedIndices))
	for idx := range selectedIndices {
		if idx >= len(dw.allDrives) {
			continue
		}
		if dw.allDrives[idx].IsVideo && dw.onVideoDisconnect != nil {
			dw.onVideoDisconnect()
		}
		if dw.allDrives[idx].IsAudio && dw.onAudioDisconnect != nil {
			dw.onAudioDisconnect()
		}
		drivesToUnmount = append(drivesToUnmount, dw.allDrives[idx])
	}

	dw.updateStatusAsync(i18n.Current.StoppingAllDevices)

	if len(keepIndices) == 0 {
		// Удаляем все — простая остановка
		if _, err := executeDeviceBatch(dw.usbClient, dw.startDevicesWithRetry, nil, false); err != nil {
			logrus.Warnf("⚠️ [UNMOUNT-SEL] Ошибка остановки: %v", err)
		}
	} else {
		// Часть устройств остаётся — Full Replace только с оставляемыми
		var keepRequests []models.DeviceStartRequest
		for idx := range keepIndices {
			if idx >= len(dw.allDrives) {
				continue
			}
			req, err := dw.buildDeviceRequestForDrive(dw.allDrives[idx], true)
			if err != nil {
				logrus.Warnf("⚠️ [UNMOUNT-SEL] Пропускаем %s: %v", dw.allDrives[idx].Name, err)
				continue
			}
			keepRequests = append(keepRequests, *req)
		}
		logrus.Infof("🔄 [UNMOUNT-SEL] Full Replace с %d оставляемыми устройствами", len(keepRequests))
		if _, err := executeDeviceBatch(dw.usbClient, dw.startDevicesWithRetry, models.DeviceStartBatchRequest(keepRequests), false); err != nil {
			logrus.Warnf("⚠️ [UNMOUNT-SEL] Ошибка переподключения: %v", err)
		}
	}

	dw.stopNBDAndCleanup(drivesToUnmount, false)
	dw.updateStatusAsync(i18n.Current.AllDevicesUnmounted)
	logrus.Infof("✅ [UNMOUNT-SEL] Частичное размонтирование завершено")
}

// stopNBDAndCleanup останавливает NBD-серверы и освобождает ресурсы.
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

// buildDeviceRequestForDrive строит DeviceStartRequest для уже смонтированного устройства
// (использует существующие NBD-серверы).
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
	if drive.Source == "gamepad" {
		req := newGamepadStartRequest(drive.GamepadMode, drive.GamepadVendorID, drive.GamepadProductID)
		return &req, nil
	}
	if drive.Source == "api" && drive.LocalDrive != nil {
		if drive.LocalDrive.SourceType == "mtp" {
			return &models.DeviceStartRequest{
				Device: "mtp", Server: drive.LocalDrive.Name,
			}, nil
		}
		return &models.DeviceStartRequest{
			Device: "drive", Server: drive.LocalDrive.Name,
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

	dw.beginOperation()

	go func() {
		defer dw.endOperation()

		var deviceRequests []models.DeviceStartRequest
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
				dw.showErrorAsync(fmt.Errorf("не удалось перестроить конфиг для %s: %w", current.Name, err))
				return
			}
			deviceRequests = append(deviceRequests, *req)
			if current.IsMouse {
				mouseIncluded = true
			}
		}

		if !mouseIncluded && dw.isMouseMountedActual() {
			mouseReq := newMouseStartRequest(newMode)
			deviceRequests = append(deviceRequests, mouseReq)
		}

		if len(deviceRequests) == 0 {
			dw.showErrorAsync(fmt.Errorf("нет смонтированных устройств для перенастройки"))
			return
		}

		dw.updateStatusAsync("Reconfiguring USB gadget...")
		if _, err := executeDeviceBatch(dw.usbClient, dw.startDevicesWithRetry, models.DeviceStartBatchRequest(deviceRequests), false); err != nil {
			dw.showErrorAsync(fmt.Errorf("ошибка перенастройки мыши: %w", err))
			return
		}

		if dw.onMouseTypeChanged != nil {
			dw.preferredMouseMode = newMode
			dw.onMouseTypeChanged(newMode)
		}
		if dw.onMouseModeReconfigured != nil {
			dw.onMouseModeReconfigured()
		}
	}()
}
