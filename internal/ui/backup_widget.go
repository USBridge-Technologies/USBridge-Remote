package ui

import (
	"fmt"
	"strings"
	"time"

	"usbridge-client/internal/api"
	"usbridge-client/internal/models"
	"usbridge-client/internal/ui/i18n"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
	"github.com/sirupsen/logrus"
)

// BackupWidget виджет для отображения списка снапшотов
type BackupWidget struct {
	container           *fyne.Container
	snapshotsList       *widget.List
	statusLabel         *widget.Label
	onStorageInfoUpdate func(usedPct float64, available, total int64) // Callback для main window
	window              fyne.Window

	// Данные
	snapshots             []*models.SnapshotInfo
	sdSpaceInfo           *models.ISOSpaceInfo // Информация о месте на SD-карте (iso/data/backup)
	currentFlash          *models.LocalDrive   // Актуальная бэкап флешка
	currentFlashConnected bool                 // Подключена ли бэкап-флешка (mtp:data)
	usbClient             *api.USBClient
	hostEntry             *widget.Entry
	updateStatus          func() // Callback для обновления статуса
}

// NewBackupWidget создает новый виджет backup
func NewBackupWidget(usbClient *api.USBClient, hostEntry *widget.Entry, updateStatus func()) *BackupWidget {
	bw := &BackupWidget{
		usbClient:    usbClient,
		hostEntry:    hostEntry,
		snapshots:    make([]*models.SnapshotInfo, 0),
		currentFlash: nil,
		updateStatus: updateStatus,
	}

	bw.createInterface()
	bw.loadCurrentFlash()
	bw.loadSnapshots()

	// Запускаем периодическое обновление
	bw.startPeriodicRefresh()

	return bw
}

// SetWindow устанавливает окно для диалогов
func (bw *BackupWidget) SetWindow(window fyne.Window) {
	bw.window = window
}

// createInterface создает интерфейс виджета
func (bw *BackupWidget) createInterface() {
	// Список снапшотов (включая актуальную флешку)
	bw.snapshotsList = widget.NewList(
		func() int {
			count := len(bw.snapshots)
			if bw.currentFlash != nil {
				count++ // Добавляем актуальную флешку
			}
			return count
		},
		func() fyne.CanvasObject {
			// Создаем лейблы и кнопки для отображения информации о снапшоте
			statusLabel := widget.NewLabel("⭕")

			sizeLabel := widget.NewLabel(i18n.Current.SnapshotRowTemplateSize)

			dateLabel := widget.NewLabel(i18n.Current.SnapshotRowTemplateDate)
			dateLabel.Alignment = fyne.TextAlignTrailing

			infoBtn := widget.NewButton("ℹ️", nil)
			infoBtn.Importance = widget.LowImportance

			mountBtn := widget.NewButton("🔌", nil)
			mountBtn.Importance = widget.MediumImportance

			// Border контейнер: галочка + размер слева, дата + info + кнопка справа
			leftContainer := container.NewHBox(statusLabel, sizeLabel)
			rightContainer := container.NewHBox(dateLabel, infoBtn, mountBtn)

			return container.NewBorder(
				nil, nil, // top, bottom
				leftContainer, rightContainer, // left, right
				nil, // center
			)
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			borderContainer := obj.(*fyne.Container)

			// Проверяем, это актуальная флешка или снапшот
			if bw.currentFlash != nil && id == 0 {
				// Первый элемент - актуальная флешка
				bw.renderCurrentFlash(borderContainer)
			} else {
				// Остальные элементы - снапшоты
				snapshotIndex := id
				if bw.currentFlash != nil {
					snapshotIndex = id - 1 // Смещаем индекс для снапшотов
				}

				if snapshotIndex < len(bw.snapshots) {
					snapshot := bw.snapshots[snapshotIndex]
					bw.renderSnapshot(borderContainer, snapshotIndex, snapshot)
				}
			}
		},
	)

	// Статус
	bw.statusLabel = widget.NewLabel(i18n.Current.ReadyToWork)

	// Создаем главный контейнер (прогрессбар места на флешке в main window)
	headerLabel := widget.NewRichTextFromMarkdown("## " + i18n.Current.BackupFlash)
	subtitleLabel := widget.NewLabel(i18n.Current.CurrentFlashAndSnapshots)
	subtitleLabel.TextStyle.Italic = true

	headerContainer := container.NewVBox(headerLabel, subtitleLabel)

	bw.container = container.NewBorder(
		headerContainer,  // top
		nil,              // bottom
		nil,              // left
		nil,              // right
		bw.snapshotsList, // center
	)
}

// renderCurrentFlash отображает актуальную флешку
func (bw *BackupWidget) renderCurrentFlash(borderContainer *fyne.Container) {
	// Находим элементы в leftContainer и rightContainer
	var statusLabel *widget.Label
	var sizeLabel *widget.Label
	var dateLabel *widget.Label
	var infoBtn *widget.Button
	var mountBtn *widget.Button

	// Проходим по border container
	for _, child := range borderContainer.Objects {
		if container, ok := child.(*fyne.Container); ok {
			btnIdx := 0
			for _, innerChild := range container.Objects {
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

	if statusLabel == nil || sizeLabel == nil || dateLabel == nil {
		logrus.Warnf("⚠️ Not all UI elements were found in renderCurrentFlash")
		return
	}

	// Скрываем кнопку info для актуальной флешки (changelog только у снапшотов)
	if infoBtn != nil {
		infoBtn.Hide()
	}

	// Обновляем содержимое для актуальной флешки
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

	// Настраиваем кнопку монтирования
	if mountBtn != nil {
		mountBtn.OnTapped = func() {
			bw.handleMountCurrentFlash()
		}
	}
}

// renderSnapshot отображает снапшот
func (bw *BackupWidget) renderSnapshot(borderContainer *fyne.Container, id widget.ListItemID, snapshot *models.SnapshotInfo) {
	// Находим элементы в leftContainer и rightContainer
	var statusLabel *widget.Label
	var sizeLabel *widget.Label
	var dateLabel *widget.Label
	var infoBtn *widget.Button
	var mountBtn *widget.Button

	// Проходим по border container
	for _, child := range borderContainer.Objects {
		if container, ok := child.(*fyne.Container); ok {
			btnIdx := 0
			for _, innerChild := range container.Objects {
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

	if statusLabel == nil || sizeLabel == nil || dateLabel == nil {
		logrus.Warnf("⚠️ Not all UI elements were found in renderSnapshot")
		return
	}

	// Показываем кнопку info для снапшотов
	if infoBtn != nil {
		infoBtn.Show()
		snap := snapshot // для замыкания
		infoBtn.OnTapped = func() {
			bw.showSnapshotDetails(snap)
		}
	}

	// Обновляем содержимое для снапшота (используем size_human от API)
	sizeLabel.SetText(snapshot.DisplaySize())
	dateLabel.SetText(snapshot.CreatedAt.Format(i18n.Current.DateTimeFormat))

	// Устанавливаем иконку статуса подключения (как на экране дисков)
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

	// Настраиваем кнопку монтирования
	if mountBtn != nil {
		mountBtn.OnTapped = func() {
			bw.handleMountSnapshot(id, snapshot)
		}
	}
}

// loadCurrentFlash загружает актуальную бэкап флешку
func (bw *BackupWidget) loadCurrentFlash() {
	go func() {
		if bw.usbClient == nil {
			logrus.Debug("USB клиент не инициализирован, пропускаем загрузку актуальной флешки")
			return
		}

		logrus.Info("📱 Загрузка актуальной бэкап флешки...")

		localDrives, err := bw.usbClient.GetLocalDrives()
		if err != nil {
			logrus.Errorf(i18n.Current.ErrorLoadingLocalDevices, err)
			return
		}

		// Ищем MTP устройство с source_type "mtp"
		for _, drive := range localDrives.Drives {
			if drive.SourceType == "mtp" {
				bw.currentFlash = &drive
				logrus.Infof("✅ Найдена актуальная бэкап флешка: %s", drive.Name)
				break
			}
		}

		// Проверяем, подключена ли бэкап-флешка (mtp:data) через /api/device/info
		bw.currentFlashConnected = false
		deviceInfo, err := bw.usbClient.GetDeviceInfo()
		if err == nil {
			for _, device := range deviceInfo.Devices {
				if device.Status == "connected" &&
					device.Type == "mtp" &&
					strings.Contains(device.Name, "data") &&
					!strings.Contains(device.ProductName, "snapshot") {
					bw.currentFlashConnected = true
					logrus.Infof("✅ Бэкап-флешка подключена: %s", device.Name)
					break
				}
			}
		}

		// Загружаем информацию о месте на SD-карте (как на экране устройств)
		bw.loadISOSpace()

		bw.updateUIAsync(func() {
			bw.snapshotsList.Refresh()
		})
	}()
}

// loadISOSpace загружает информацию о месте на SD-карте (раздел iso/data/backup)
func (bw *BackupWidget) loadISOSpace() {
	if bw.usbClient == nil {
		return
	}
	spaceInfo, err := bw.usbClient.GetISOSpace()
	if err != nil {
		logrus.Debugf("Информация о месте на SD-карте недоступна: %v", err)
		bw.updateUIAsync(func() {
			bw.sdSpaceInfo = nil
			bw.updateSDStorageInfo()
		})
		return
	}
	bw.updateUIAsync(func() {
		bw.sdSpaceInfo = spaceInfo
		bw.updateSDStorageInfo()
	})
}

// updateSDStorageInfo обновляет прогрессбар в main window через callback
func (bw *BackupWidget) updateSDStorageInfo() {
	if bw.sdSpaceInfo == nil || bw.sdSpaceInfo.TotalSpace <= 0 {
		if bw.onStorageInfoUpdate != nil {
			bw.onStorageInfoUpdate(0, 0, 0)
		}
		return
	}
	usedPct := bw.sdSpaceInfo.UsedPercent
	total := bw.sdSpaceInfo.TotalSpace
	available := bw.sdSpaceInfo.AvailableSpace
	if bw.onStorageInfoUpdate != nil {
		bw.onStorageInfoUpdate(usedPct/100, available, total)
	}
}

// SetOnStorageInfoUpdate устанавливает callback для обновления прогрессбара в main window
func (bw *BackupWidget) SetOnStorageInfoUpdate(fn func(usedPct float64, available, total int64)) {
	bw.onStorageInfoUpdate = fn
}

// loadSnapshots загружает список снапшотов
func (bw *BackupWidget) loadSnapshots() {
	go func() {
		if bw.usbClient == nil {
			logrus.Debug("USB client not initialized, skipping snapshots loading")
			bw.updateUIAsync(func() {
				bw.statusLabel.SetText(i18n.Current.WaitingConnection)
			})
			return
		}

		bw.updateStatusAsync(i18n.Current.LoadingSnapshots)
		logrus.Info("📦 Загрузка списка снапшотов...")

		snapshotsResp, err := bw.usbClient.GetSnapshots()
		if err != nil {
			logrus.Errorf("Error loading snapshots: %v", err)
			bw.updateUIAsync(func() {
				bw.statusLabel.SetText(i18n.Current.ErrorLoadingSnapshots)
			})
			return
		}

		// Конвертируем в указатели
		bw.snapshots = make([]*models.SnapshotInfo, len(snapshotsResp.Snapshots))
		for i := range snapshotsResp.Snapshots {
			bw.snapshots[i] = &snapshotsResp.Snapshots[i]
		}

		bw.updateUIAsync(func() {
			bw.snapshotsList.Refresh()
			bw.statusLabel.SetText(fmt.Sprintf(i18n.Current.LoadedSnapshots, len(bw.snapshots)))
		})

		logrus.Infof("✅ Загружено %d снапшотов", len(bw.snapshots))
	}()
}

// UpdateClient обновляет USB клиент
func (bw *BackupWidget) UpdateClient(usbClient *api.USBClient) {
	bw.usbClient = usbClient
	if usbClient == nil {
		bw.sdSpaceInfo = nil
		bw.updateSDStorageInfo()
	}
	// Обновляем данные при смене клиента
	bw.loadCurrentFlash()
	bw.loadSnapshots()
}

// UpdateHostEntry обновляет ссылку на поле ввода хоста
func (bw *BackupWidget) UpdateHostEntry(hostEntry *widget.Entry) {
	bw.hostEntry = hostEntry
}

// GetContainer возвращает контейнер виджета
func (bw *BackupWidget) GetContainer() *fyne.Container {
	return bw.container
}

// Refresh обновляет виджет
func (bw *BackupWidget) Refresh() {
	bw.loadCurrentFlash()
	bw.loadSnapshots()
}

// updateUIAsync безопасно обновляет UI из горутины
func (bw *BackupWidget) updateUIAsync(updateFunc func()) {
	// В Fyne используем fyne.Do для обновления UI из горутин
	fyne.Do(updateFunc)
}

// updateStatusAsync безопасно обновляет статус из горутины
func (bw *BackupWidget) updateStatusAsync(status string) {
	bw.updateUIAsync(func() {
		bw.statusLabel.SetText(status)
	})
}

// canConnectBackupOrSnapshot проверяет, можно ли подключить бэкап или снапшот.
// Блокирует, если подключено 5 устройств и ни одно из них не бэкап/снапшот.
func (bw *BackupWidget) canConnectBackupOrSnapshot() (bool, string) {
	deviceInfo, err := bw.usbClient.GetDeviceInfo()
	if err != nil {
		return true, "" // При ошибке разрешаем попытку
	}
	connectedCount := 0
	hasBackupOrSnapshot := false
	for _, device := range deviceInfo.Devices {
		if device.Status != "connected" {
			continue
		}
		connectedCount++
		// Бэкап-флешка: mtp с data, без snapshot
		if device.Type == "mtp" && strings.Contains(device.Name, "data") && !strings.Contains(device.ProductName, "snapshot") {
			hasBackupOrSnapshot = true
		}
		// Снапшот: mtp со snapshot или nbd
		if device.Type == "nbd" || (device.Type == "mtp" && (strings.Contains(device.ProductName, "snapshot") || strings.Contains(device.Name, "snapshot"))) {
			hasBackupOrSnapshot = true
		}
	}
	if connectedCount >= 5 && !hasBackupOrSnapshot {
		return false, i18n.Current.FreeDeviceSlotRequired
	}
	return true, ""
}

// buildDeviceBatchWithMTP собирает batch-запрос с сохранением подключённых устройств
// (клавиатура, мышь, RNDIS, локальные диски) и заменой MTP на новый (снапшот или бэкап-флешка).
func (bw *BackupWidget) buildDeviceBatchWithMTP(mtpServer, mtpProductName string) models.DeviceStartBatchRequest {
	var requests []models.DeviceStartRequest
	addedKeyboard, addedMouse, addedRndis := false, false, false

	deviceInfo, err := bw.usbClient.GetDeviceInfo()
	if err == nil {
		for _, device := range deviceInfo.Devices {
			if device.Status != "connected" {
				continue
			}
			// Сохраняем клавиатуру, мышь, RNDIS, локальные диски (CD-ROM)
			// MTP и NBD не добавляем — заменяем на новый MTP
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
			case (device.Type == "mouse" || device.Type == "touchscreen" || device.Type == "absolute" || strings.HasPrefix(device.Type, "mouse:")) && !addedMouse:
				mouseType := "mouse"
				if device.Type == "touchscreen" {
					mouseType = "touchscreen"
				} else if device.Type == "absolute" {
					mouseType = "absolute"
				}
				requests = append(requests, models.DeviceStartRequest{
					Device:       "mouse",
					Type:         mouseType,
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
				// Локальный CD-ROM (ISO/IMG) — сохраняем (режим только чтение для бэкапа)
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
			// mtp и nbd не добавляем — заменяем на новый MTP
		}
	}

	// Добавляем новый MTP (снапшот или бэкап-флешка)
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

	// Блокируем интерфейс на время операции
	bw.updateStatusAsync(fmt.Sprintf(i18n.Current.MountingFlash, bw.currentFlash.Name))

	// Выполняем монтирование в горутине
	go func() {
		// Собираем batch с сохранением keyboard, mouse, rndis, local и заменой MTP
		batchRequest := bw.buildDeviceBatchWithMTP(bw.currentFlash.Name, "BackupDrive")

		logrus.Infof("🚀 Запуск монтирования актуальной флешки как MTP: %s (с сохранением остальных устройств)", bw.currentFlash.Name)

		deviceResp, err := bw.usbClient.StartDevicesBatch(batchRequest)
		if err != nil {
			logrus.Errorf("❌ Ошибка монтирования актуальной флешки: %v", err)
			bw.showErrorAsync(fmt.Errorf(i18n.Current.ErrorMountingFlashMsg, err))
			return
		}

		// Логируем успешный ответ API
		logrus.Infof("✅ API ответ от USBridge 2:")
		logrus.Infof("  - Success: %v", deviceResp.Success)
		logrus.Infof("  - Message: %s", deviceResp.Message)
		if deviceResp.Data != nil {
			logrus.Infof("  - Data: %+v", deviceResp.Data)
		}

		bw.updateUIAsync(func() {
			bw.statusLabel.SetText(fmt.Sprintf(i18n.Current.FlashMounted, bw.currentFlash.Name))
		})

		logrus.Infof("✅ Актуальная флешка %s успешно смонтирована", bw.currentFlash.Name)

		// Даем время серверу обновить список устройств (увеличено до 2 секунд)
		logrus.Info("⏳ Ожидание обновления списка устройств (2 секунды)...")
		time.Sleep(2 * time.Second)

		// Обновляем список бэкап-флешки и снапшотов (для отображения зелёной галочки)
		bw.loadCurrentFlash()
		bw.loadSnapshots()

		// Обновляем статус иконок в главном окне (в UI потоке)
		if bw.updateStatus != nil {
			logrus.Info("🔄 Вызов updateStatus() для обновления иконок")
			fyne.Do(func() {
				bw.updateStatus()
			})
		}
	}()
}

// handleMountSnapshot обрабатывает монтирование снапшота
func (bw *BackupWidget) handleMountSnapshot(id widget.ListItemID, snapshot *models.SnapshotInfo) {
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

	// Блокируем интерфейс на время операции
	bw.updateStatusAsync(fmt.Sprintf(i18n.Current.MountingSnapshot, snapshot.Name))

	// Выполняем монтирование в горутине
	go func() {
		// Собираем batch с сохранением keyboard, mouse, rndis, local и заменой MTP
		batchRequest := bw.buildDeviceBatchWithMTP(snapshot.Name, snapshot.Name)

		logrus.Infof("🚀 Запуск монтирования снапшота как MTP: %s (с сохранением остальных устройств)", snapshot.Name)

		deviceResp, err := bw.usbClient.StartDevicesBatch(batchRequest)
		if err != nil {
			logrus.Errorf("❌ Ошибка монтирования снапшота: %v", err)
			bw.showErrorAsync(fmt.Errorf(i18n.Current.ErrorMountingSnapshotMsg, err))
			return
		}

		// Логируем успешный ответ API
		logrus.Infof("✅ API ответ от USBridge 2:")
		logrus.Infof("  - Success: %v", deviceResp.Success)
		logrus.Infof("  - Message: %s", deviceResp.Message)
		if deviceResp.Data != nil {
			logrus.Infof("  - Data: %+v", deviceResp.Data)
		}

		bw.updateUIAsync(func() {
			bw.statusLabel.SetText(fmt.Sprintf(i18n.Current.SnapshotMounted, snapshot.Name))
		})

		logrus.Infof("✅ Снапшот %s успешно смонтирован", snapshot.Name)

		// Даем время серверу обновить список устройств (увеличено до 2 секунд)
		logrus.Info("⏳ Ожидание обновления списка устройств (2 секунды)...")
		time.Sleep(2 * time.Second)

		// Обновляем список снапшотов (для отображения зелёной галочки)
		bw.loadCurrentFlash()
		bw.loadSnapshots()

		// Обновляем статус иконок в главном окне (в UI потоке)
		if bw.updateStatus != nil {
			logrus.Info("🔄 Вызов updateStatus() для обновления иконок")
			fyne.Do(func() {
				bw.updateStatus()
			})
		}
	}()
}

// showSnapshotDetails показывает диалог с деталями снапшота (размер, changelog)
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

	// Окно фиксированного размера с прокруткой внутри
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
		bw.statusLabel.SetText(fmt.Sprintf(i18n.Current.ErrorStatusFormat, err))
	})
}

// startPeriodicRefresh запускает периодическое обновление снапшотов
func (bw *BackupWidget) startPeriodicRefresh() {
	go func() {
		ticker := time.NewTicker(30 * time.Second) // Обновляем каждые 30 секунд
		defer ticker.Stop()

		for range ticker.C {
			// Обновляем данные через API
			bw.updateUIAsync(func() {
				bw.loadCurrentFlash()
				bw.loadSnapshots()
			})
		}
	}()
}
