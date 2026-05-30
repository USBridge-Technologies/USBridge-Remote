package controller

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"usbridge-client/internal/gui/i18n"
	"usbridge-client/internal/models"
	"usbridge-client/internal/platform"
	"usbridge-client/internal/service"

	"github.com/sirupsen/logrus"
)

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

// uploadState сохраняет состояние загрузки для восстановления после combineDrives
type uploadState struct {
	progress float64
	speed    float64
}

// loadLocalDrives загружает локальные устройства через API
func (dw *DiskWidget) loadLocalDrives() {
	if !dw.loadingLocalDrives.CompareAndSwap(false, true) {
		logrus.Debug("loadLocalDrives already in flight, skipping overlapping refresh")
		return
	}
	go func() {
		defer dw.loadingLocalDrives.Store(false)
		if dw.usbClient == nil {
			logrus.Debug("USB клиент не инициализирован, пропускаем загрузку локальных устройств")
			return
		}

		localDrives, err := dw.usbClient.GetLocalDrives()
		if err != nil {
			logrus.Errorf("Ошибка загрузки локальных устройств: %v", err)
			return
		}

		// Конвертируем в указатели
		dw.localDrives = make([]*models.LocalDrive, len(localDrives.Drives))
		for i := range localDrives.Drives {
			dw.localDrives[i] = &localDrives.Drives[i]
		}

		logrus.Debugf("Загружено %d устройств из API", len(dw.localDrives))
		dw.updateUIAsync(func() {
			dw.combineDrives() // вызывает rebuildListItems → requestDevicesRefresh
		})

		// Загружаем информацию о месте на SD-карте (раздел iso/data/backup)
		dw.loadISOSpace()
	}()
}

// loadISOSpace загружает информацию о месте на SD-карте
func (dw *DiskWidget) loadISOSpace() {
	if dw.usbClient == nil {
		return
	}
	spaceInfo, err := dw.usbClient.GetISOSpace()
	if err != nil {
		logrus.Debugf("Информация о месте на SD-карте недоступна: %v", err)
		dw.updateUIAsync(func() {
			dw.sdSpaceInfo = nil
			dw.updateSDStorageInfo()
		})
		return
	}
	dw.updateUIAsync(func() {
		dw.sdSpaceInfo = spaceInfo
		dw.updateSDStorageInfo()
	})
}

// updateSDStorageInfo обновляет прогрессбар в main window через callback
func (dw *DiskWidget) updateSDStorageInfo() {
	if dw.sdSpaceInfo == nil || dw.sdSpaceInfo.TotalSpace <= 0 {
		if dw.onStorageInfoUpdate != nil {
			dw.onStorageInfoUpdate(0, 0, 0)
		}
		return
	}
	usedPct := dw.sdSpaceInfo.UsedPercent
	total := dw.sdSpaceInfo.TotalSpace
	available := dw.sdSpaceInfo.AvailableSpace
	if dw.onStorageInfoUpdate != nil {
		dw.onStorageInfoUpdate(usedPct/100, available, total)
	}
}

// loadLocalFiles загружает локальные файлы из папки isos
func (dw *DiskWidget) loadLocalFiles() {
	if !dw.loadingLocalFiles.CompareAndSwap(false, true) {
		logrus.Debug("loadLocalFiles already in flight, skipping overlapping refresh")
		return
	}

	go func() {
		defer dw.loadingLocalFiles.Store(false)

		var foundFiles []*models.DiskInfo

		for _, scanPath := range dw.scanPaths {
			if _, err := os.Stat(scanPath); os.IsNotExist(err) {
				continue
			}

			err := filepath.Walk(scanPath, func(path string, info os.FileInfo, err error) error {
				if err != nil {
					return nil
				}

				if info.IsDir() {
					return nil
				}

				if dw.isSupportedFile(path) {
					diskInfo, err := models.NewDiskInfo(path)
					if err != nil {
						logrus.Errorf("Ошибка создания DiskInfo для %s: %v", path, err)
						return nil
					}

					foundFiles = append(foundFiles, diskInfo)
				}

				return nil
			})

			if err != nil {
				logrus.Errorf("Ошибка сканирования %s: %v", scanPath, err)
			}
		}

		logrus.Debugf("Найдено %d локальных файлов", len(foundFiles))
		dw.updateUIAsync(func() {
			dw.localFiles = foundFiles
			dw.combineDrives() // вызывает rebuildListItems → requestDevicesRefresh
		})
	}()
}

func (dw *DiskWidget) loadVideoDevices() {
	if !dw.loadingVideoDevices.CompareAndSwap(false, true) {
		logrus.Debug("loadVideoDevices already in flight, skipping overlapping refresh")
		return
	}
	go func() {
		defer dw.loadingVideoDevices.Store(false)
		devices, err := getAvailableVideoDevices(dw.usbClient)
		if err != nil {
			logrus.Debugf("video devices unavailable: %v", err)
			dw.updateUIAsync(func() {
				dw.videoDevices = nil
				dw.combineDrives() // вызывает rebuildListItems → requestDevicesRefresh
			})
			return
		}

		dw.updateUIAsync(func() {
			dw.videoDevices = devices
			dw.combineDrives() // вызывает rebuildListItems → requestDevicesRefresh
		})
	}()
}

// isSupportedFile проверяет, поддерживается ли файл
func (dw *DiskWidget) isSupportedFile(filePath string) bool {
	diskInfo := &models.DiskInfo{Path: filePath}
	return diskInfo.IsSupported(dw.supportedTypes)
}

// combineDrives объединяет устройства из API, локальные файлы и клавиатуру
func (dw *DiskWidget) combineDrives() {
	// Сохраняем состояние загрузки и монтирования перед перестроением
	uploadingByPath := make(map[string]uploadState)
	mountingByExportName := make(map[string]bool)
	oldReadOnly := make(map[string]bool)
	selectedKeys := make(map[string]bool)
	oldMouseType := normalizeMouseMode(dw.preferredMouseMode) // сохраняем выбор пользователя (touchpad/touchscreen/absolute)
	oldRNDISMode := "auto"
	for i, d := range dw.allDrives {
		if d.IsMouse && d.MouseType != "" {
			oldMouseType = d.MouseType
		}
		if d.IsRNDIS && d.RNDISMode != "" {
			oldRNDISMode = d.RNDISMode
		}
		if d.IsUploading && d.DiskInfo != nil && d.DiskInfo.Path != "" {
			uploadingByPath[d.DiskInfo.Path] = uploadState{progress: d.UploadProgress, speed: d.UploadSpeed}
		}
		if d.IsMounting {
			if d.DiskInfo != nil {
				mountingByExportName[d.DiskInfo.Name] = true
			} else if d.LocalDrive != nil {
				mountingByExportName[d.LocalDrive.Name] = true
			} else {
				mountingByExportName[d.Name] = true
			}
		}
		// Сохраняем RO/RW для overlay-образов (vdi/vmdk/qcow2)
		key := ""
		if d.DiskInfo != nil {
			key = d.DiskInfo.Path
		} else if d.LocalDrive != nil && d.LocalDrive.SourceType != "mtp" {
			key = "api:" + d.LocalDrive.Name
		}
		if key != "" {
			ext := ""
			if d.DiskInfo != nil {
				ext = strings.ToLower(filepath.Ext(d.DiskInfo.Path))
			} else if d.LocalDrive != nil {
				ext = strings.ToLower(filepath.Ext(d.LocalDrive.Name))
			}
			if service.IsOverlayCapableExtension(ext) {
				oldReadOnly[key] = d.ReadOnly
			}
		}
		dw.selectedItemsMu.RLock()
		isSelected := dw.selectedItems[i]
		dw.selectedItemsMu.RUnlock()
		if isSelected {
			selectedKeys[driveSelectionKey(d)] = true
		}
	}

	dw.allDrives = make([]DriveItem, 0)

	if dw.devicesTraceBudget > 0 {
		logrus.Infof("[devices-ui] combineDrives: api localDrives=%d", len(dw.localDrives))
		for idx, drive := range dw.localDrives {
			if dw.devicesTraceBudget <= 0 {
				break
			}
			logrus.Infof(
				"[devices-ui] api drive[%d]: name=%q source_type=%q file_type=%q mounted=%v source_path=%q",
				idx,
				drive.Name,
				drive.SourceType,
				drive.FileType,
				drive.IsMounted,
				drive.SourcePath,
			)
			dw.devicesTraceBudget--
		}
	}

	// Добавляем устройства из API
	for _, drive := range dw.localDrives {
		// По умолчанию: overlay-образы (vdi/vmdk/qcow2) — RW, иначе RO
		readOnly := true
		if drive.SourceType != "mtp" {
			ext := strings.ToLower(filepath.Ext(drive.Name))
			if service.IsOverlayCapableExtension(ext) && ext != ".iso" {
				readOnly = false
			}
			if ro, ok := oldReadOnly["api:"+drive.Name]; ok {
				readOnly = ro
			}
		}
		item := DriveItem{
			Name:       drive.Name,
			Size:       drive.FormatSize(),
			Source:     "api",
			IsMounted:  drive.IsMounted,
			LocalDrive: drive,
			ReadOnly:   readOnly,
		}
		dw.allDrives = append(dw.allDrives, item)
	}

	// Добавляем локальные файлы
	for _, file := range dw.localFiles {
		ext := strings.ToLower(filepath.Ext(file.Path))
		readOnly := !service.IsOverlayCapableExtension(ext) || ext == ".iso"
		if ro, ok := oldReadOnly[file.Path]; ok {
			readOnly = ro
		}
		item := DriveItem{
			Name:      file.Name,
			Size:      file.FormatSize(),
			Source:    "local",
			IsMounted: false,
			DiskInfo:  file,
			ReadOnly:  readOnly,
		}
		dw.allDrives = append(dw.allDrives, item)
	}

	// Добавляем пользовательские образы
	for _, file := range dw.userImages {
		logrus.Debugf("🔍 [combineDrives] Обработка пользовательского образа: Name='%s', Path='%s', Type='%s'", file.Name, file.Path, file.Type)
		displayName := dw.formatPathForDisplay(file.Path, file.Name)
		logrus.Debugf("🔍 [combineDrives] После formatPathForDisplay: displayName='%s'", displayName)

		ext := strings.ToLower(filepath.Ext(file.Path))
		readOnly := !service.IsOverlayCapableExtension(ext) || ext == ".iso"
		if ro, ok := oldReadOnly[file.Path]; ok {
			readOnly = ro
		}
		item := DriveItem{
			Name:      displayName,
			Size:      file.FormatSize(),
			Source:    "user",
			IsMounted: false,
			DiskInfo:  file,
			ReadOnly:  readOnly,
		}
		dw.allDrives = append(dw.allDrives, item)
	}

	// Добавляем видеоустройства
	for i := range dw.videoDevices {
		device := dw.videoDevices[i]
		item := DriveItem{
			Name:        fmt.Sprintf("%s [%s]", firstNonEmpty(device.Description, device.Name, device.Path), device.Path),
			Size:        device.Path,
			Source:      "video",
			IsMounted:   false,
			IsVideo:     true,
			VideoDevice: &dw.videoDevices[i],
		}
		dw.allDrives = append(dw.allDrives, item)
	}

	// Добавляем клавиатуру
	keyboardItem := DriveItem{
		Name:       i18n.Current.DeviceKeyboard,
		Size:       "N/A",
		Source:     "keyboard",
		IsMounted:  false,
		IsKeyboard: true,
	}
	dw.allDrives = append(dw.allDrives, keyboardItem)

	// Добавляем мышь
	oldMouseType = normalizeMouseMode(oldMouseType)
	oldRNDISMode = normalizeRNDISMode(oldRNDISMode)
	mouseItem := DriveItem{
		Name:      i18n.Current.DeviceMouse,
		Size:      "N/A",
		Source:    "mouse",
		IsMounted: false,
		IsMouse:   true,
		MouseType: oldMouseType,
	}
	dw.allDrives = append(dw.allDrives, mouseItem)

	// Добавляем сетевую карту (RNDIS) - только если ОС хоста "usbridge"
	osName := strings.ToLower(dw.agentOS)
	if strings.Contains(osName, "usbridge") {
		rndisItem := DriveItem{
			Name:      i18n.Current.DeviceNetworkCard,
			Size:      "N/A",
			Source:    "rndis",
			IsMounted: false,
			IsRNDIS:   true,
			RNDISMode: oldRNDISMode,
		}
		dw.allDrives = append(dw.allDrives, rndisItem)
	}

	// Добавляем геймпады из системы (macOS: IOKit; другие платформы: пусто)
	for _, gpad := range dw.gamepadDevices {
		gamepadItem := DriveItem{
			Name:      gpad.Name,
			Size:      "N/A",
			Source:    "gamepad",
			IsMounted: false,
			IsGamepad: true,
			GamepadID: gpad.ID,
		}
		dw.allDrives = append(dw.allDrives, gamepadItem)
	}

	// Восстанавливаем состояние загрузки и монтирования
	for i := range dw.allDrives {
		if dw.allDrives[i].DiskInfo != nil {
			if state, ok := uploadingByPath[dw.allDrives[i].DiskInfo.Path]; ok {
				dw.allDrives[i].IsUploading = true
				dw.allDrives[i].UploadProgress = state.progress
				dw.allDrives[i].UploadSpeed = state.speed
			}
			if mountingByExportName[dw.allDrives[i].DiskInfo.Name] {
				dw.allDrives[i].IsMounting = true
			}
		}
		if dw.allDrives[i].LocalDrive != nil && mountingByExportName[dw.allDrives[i].LocalDrive.Name] {
			dw.allDrives[i].IsMounting = true
		}
	}
	remappedSelection := make(map[int]bool, len(selectedKeys))
	for i := range dw.allDrives {
		if dw.allDrives[i].IsVideo {
			continue
		}
		if selectedKeys[driveSelectionKey(dw.allDrives[i])] {
			remappedSelection[i] = true
		}
	}
	dw.selectedItemsMu.Lock()
	dw.selectedItems = remappedSelection
	dw.selectedItemsMu.Unlock()

	dw.updateDevicesStatus()
	dw.rebuildListItems()

	logrus.Debugf("Объединено %d элементов (API: %d, локальные: %d, пользовательские: %d, video: %d, клавиатура: 1, мышь: 1, RNDIS: %v), agentOS: %q",
		len(dw.allDrives), len(dw.localDrives), len(dw.localFiles), len(dw.userImages), len(dw.videoDevices), strings.Contains(osName, "usbridge"), dw.agentOS)
}


// loadGamepadDevices обновляет список геймпадов из ОС и перестраивает список устройств.
func (dw *DiskWidget) loadGamepadDevices() {
	gamepads := platform.EnumerateGamepads()
	dw.updateUIAsync(func() {
		dw.gamepadDevices = gamepads
		dw.combineDrives()
	})
}

// loadMountedDevices загружает смонтированные устройства через API
func (dw *DiskWidget) loadMountedDevices() {
	if !dw.loadingMountedInfo.CompareAndSwap(false, true) {
		logrus.Debug("loadMountedDevices already in flight, skipping overlapping refresh")
		return
	}
	go func() {
		defer dw.loadingMountedInfo.Store(false)
		if dw.usbClient == nil {
			logrus.Debug("USB клиент не инициализирован, пропускаем загрузку устройств")
			return
		}

		deviceInfo, err := dw.usbClient.GetDeviceInfo()
		if err != nil {
			logrus.Errorf("Ошибка загрузки информации об устройствах: %v", err)
			return
		}

		dw.mountedDevices = make([]*models.DeviceInfo, len(deviceInfo.Devices))
		for i := range deviceInfo.Devices {
			dw.mountedDevices[i] = &deviceInfo.Devices[i]
		}

		dw.agentOS = deviceInfo.AgentOS
		logrus.Debugf("Загружено %d смонтированных устройств, agentOS='%s'", len(dw.mountedDevices), dw.agentOS)
		dw.updateUIAsync(func() {
			// Only propagate the server's MountInProgress flag when no local user
			// operation is in flight. If we unconditionally overwrite, a stale
			// background-poll response arriving after endOperation() has already
			// cleared the flag will re-lock the UI permanently.
			if !dw.userOperationInFlight.Load() {
				dw.setAPIMountInProgress(deviceInfo.MountInProgress)
			}
			dw.updateDevicesStatus()
			dw.requestDevicesRefresh()
		})
	}()
}

// updateDevicesStatus обновляет статус всех устройств в списке
func (dw *DiskWidget) updateDevicesStatus() {
	logrus.Debugf("🔄 Обновление статуса устройств. Всего устройств: %d, смонтированных: %d", len(dw.allDrives), len(dw.mountedDevices))

	for _, device := range dw.mountedDevices {
		logrus.Debugf("📡 Смонтированное устройство: name='%s', device='%s', type='%s', status='%s'",
			device.Name, device.Device, device.Type, device.Status)
	}

	nbdExportToDriveName := make(map[string]string)
	for name, srv := range dw.nbdServers {
		if srv != nil && srv.IsRunning() {
			apiName := srv.NBDExportNameForAPI()
			if apiName != "" {
				nbdExportToDriveName[apiName] = name
			}
		}
	}

	var currentVideoPath string
	videoStreaming := false
	if info, err := getVideoInfoData(dw.usbClient); err == nil && info != nil {
		currentVideoPath = info.Device
		videoStreaming = info.Streaming
	}

	drives := dw.allDrives
	for i := range drives {
		drive := &drives[i]
		oldStatus := drive.IsMounted
		isMounted := false

		if drive.IsVideo {
			if drive.VideoDevice != nil && videoStreaming && drive.VideoDevice.Path == currentVideoPath {
				isMounted = true
			}
			drive.IsMounted = isMounted
			logrus.Debugf("📺 %s (%s): %v -> %v", drive.Name, drive.Source, oldStatus, drive.IsMounted)
			continue
		}

		for _, device := range dw.mountedDevices {
			if device.Status != "connected" {
				continue
			}

			if drive.IsKeyboard && (device.Type == "keyboard" || strings.HasPrefix(device.Type, "keyboard:")) {
				isMounted = true
				logrus.Debugf("⌨️ Найдена подключенная клавиатура: %s (type: %s, device: %s)", device.Name, device.Type, device.Device)
				break
			}

			if drive.IsMouse && isMouseDeviceType(device.Type) {
				isMounted = true
				dw.observedMouseMode = mouseModeFromDeviceType(device.Type)
				drive.MouseType = normalizeMouseMode(dw.preferredMouseMode)
				if drive.MouseType == "" {
					drive.MouseType = dw.observedMouseMode
				}
				logrus.Debugf("🖱️ Найдена подключенная мышь: %s (type: %s, device: %s)", device.Name, device.Type, device.Device)
				break
			}

			if drive.IsRNDIS && (device.Type == "rndis" || strings.HasPrefix(device.Type, "rndis:")) {
				isMounted = true
				if strings.HasPrefix(device.Type, "rndis:") {
					rndisMode := strings.TrimPrefix(device.Type, "rndis:")
					drive.RNDISMode = normalizeRNDISMode(rndisMode)
				}
				logrus.Debugf("🌐 Найдена подключенная сетевая карта: %s (type: %s, device: %s)", device.Name, device.Type, device.Device)
				break
			}

			if drive.IsGamepad && (device.Type == "gamepad" || strings.HasPrefix(device.Type, "gamepad:")) {
				isMounted = true
				logrus.Debugf("🎮 Найден подключенный геймпад: %s (type: %s, device: %s)", device.Name, device.Type, device.Device)
				break
			}

			if drive.IsKeyboard || drive.IsMouse || drive.IsRNDIS || drive.IsGamepad {
				continue
			}

			expectedServerType := ""
			switch drive.Source {
			case "api":
				if drive.LocalDrive != nil && drive.LocalDrive.SourceType == "mtp" {
					expectedServerType = "mtp"
				} else {
					expectedServerType = "local"
				}
			case "local", "user":
				expectedServerType = "nbd"
			}

			if expectedServerType == "" || device.Type != expectedServerType {
				continue
			}

			var driveName string
			if drive.Source == "user" && drive.DiskInfo != nil {
				driveName = filepath.Base(drive.DiskInfo.Path)
			} else if drive.Source == "api" && drive.LocalDrive != nil {
				driveName = drive.LocalDrive.Name
			} else {
				driveName = drive.Name
			}

			if device.Type == "nbd" && device.Name != "" {
				expectedName := nbdExportToDriveName[device.Name]
				matchesDrive := expectedName != "" && (driveName == expectedName || drive.Name == expectedName ||
					(drive.DiskInfo != nil && drive.DiskInfo.Name == expectedName))
				if matchesDrive {
					isMounted = true
					logrus.Debugf("🌐 Найден подключенный NBD диск (по export_name): %s (device: %s, API name: %s)", drive.Name, device.Device, device.Name)
					break
				}
			}

			if drive.Source == "api" && drive.LocalDrive != nil {
				expectedDeviceKey := fmt.Sprintf("%s:%s", expectedServerType, driveName)
				if device.Device == expectedDeviceKey || device.Name == driveName {
					isMounted = true
					if device.Type == "mtp" {
						logrus.Debugf("📱 Найден подключенный MTP диск: %s (device: %s, API name: %s)", drive.Name, device.Device, device.Name)
					} else {
						logrus.Debugf("💿 Найден подключенный локальный диск bridge: %s (device: %s, API name: %s)", drive.Name, device.Device, device.Name)
					}
					break
				}
			}

			if device.Type == "nbd" && (strings.Contains(device.Name, driveName) || strings.Contains(driveName, device.Name)) {
				isMounted = true
				logrus.Debugf("🌐 Найден подключенный NBD диск: %s (device: %s, API name: %s)", drive.Name, device.Device, device.Name)
				break
			}
		}

		drive.IsMounted = isMounted
		logrus.Debugf("📊 %s (%s): %v -> %v", drive.Name, drive.Source, oldStatus, drive.IsMounted)
	}
	dw.updateButtons()
	dw.syncGamepadCaptures()
}
