package ui

import (
	"strings"
	"time"

	"usbridge-client/internal/api"
	"usbridge-client/internal/models"
	"usbridge-client/internal/service"
	"usbridge-client/internal/ui/i18n"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
	"github.com/sirupsen/logrus"
)

// MainWindow главное окно приложения
type MainWindow struct {
	app    fyne.App
	window fyne.Window

	// Виджеты
	diskWidget         *DiskWidget
	videoWidget        *VideoWidget
	backupWidget       *BackupWidget
	connectionManager  *ConnectionManager
	mainContent        *fyne.Container
	connectionContent  *fyne.Container
	tabs               *container.AppTabs
	deviceButtonsPanel *fyne.Container

	// Сервисы
	nbdServer        *service.NBDServer
	gstreamerService *service.GStreamerService
	usbClient        *api.USBClient
	frpService       *service.FRPService

	// Состояние
	config              *models.AppConfig
	appState            *models.AppState
	isConnected         bool
	isStreaming         bool
	isConnectionPending bool // Флаг блокировки кнопки подключения

	// Кнопка подключения/отключения
	connectionBtn *widget.Button

	// PC Panel (Power/Reset LED кнопки)
	pcpanelWidget *PCPanelWidget

	// Адресная строка
	hostEntry         *widget.Entry
	tokenEntry        *widget.Entry       // Скрыто, используется для подключения из сохранённых
	sdStorageProgress *StorageProgressBar // Прогресс места на флешке — на всех экранах
	deepLinkHandler   *DeepLinkHandler    // Глобальный handler для deep links

	// Иконки статуса (используем Button для поддержки tooltip)
	connectionIcon *widget.Button
	nbdIcon        *widget.Button
	videoIcon      *widget.Button
	keyboardIcon   *widget.Button
	mouseIcon      *widget.Button
	rndisIcon      *widget.Button
	cdromIcon      *widget.Button
	backupIcon     *widget.Button
	snapshotIcon   *widget.Button
	statusPanel    *fyne.Container
}

// NewMainWindow создает новое главное окно
func NewMainWindow(config *models.AppConfig) *MainWindow {
	myApp := newFyneApp()
	// Устанавливаем метаданные приложения
	myApp.SetIcon(nil) // Можно добавить иконку позже

	// Загружаем сохраненный язык из настроек
	savedLang := myApp.Preferences().StringWithFallback("language", "en")
	i18n.Init(savedLang)
	logrus.Infof("🌐 Localization initialized (%s)", savedLang)

	window := myApp.NewWindow(i18n.Current.AppTitle)
	// Размер из конфига; на десктопе — удобный по умолчанию для современных DPI/масштабирования
	w, h := config.WindowWidth, config.WindowHeight
	if w < 640 {
		w = 960
	}
	if h < 480 {
		h = 640
	}
	window.Resize(fyne.NewSize(float32(w), float32(h)))
	window.SetFixedSize(false)
	window.CenterOnScreen()

	mw := &MainWindow{
		app:    myApp,
		window: window,
		config: config,
		appState: &models.AppState{
			IsConnected:  false,
			IsStreaming:  false,
			IsNBDRunning: false,
		},
	}

	// Инициализируем сервисы
	mw.nbdServer = service.NewNBDServer(config) // Оставляем для совместимости, но не используем
	mw.gstreamerService = service.NewGStreamerService(config)

	logrus.Info("✅ GStreamer service initialized")

	// USB клиент будет создан при подключении с хостом из адресной строки
	mw.usbClient = nil

	// FRP сервис будет создан при подключении
	mw.frpService = nil

	// Создаем виджеты
	mw.diskWidget = NewDiskWidget(mw.usbClient, mw.updateStatus, myApp, config)
	mw.diskWidget.SetWindow(window)
	mw.videoWidget = NewVideoWidgetGStreamer(mw.usbClient, mw.gstreamerService, mw.updateStatus)
	mw.videoWidget.SetParentWindow(window)
	mw.backupWidget = NewBackupWidget(mw.usbClient, nil, mw.updateStatus) // hostEntry будет установлен позже
	mw.backupWidget.SetWindow(window)

	// На Android НЕ создаем UI контент до ShowAndRun() - это вызывает ошибки threading
	// Создание UI будет отложено до метода Show()

	return mw
}

// createInterface инициализирует поля адресной строки
func (mw *MainWindow) createInterface() {
	// Создаем поля адресной строки
	mw.hostEntry = widget.NewEntry()
	mw.hostEntry.SetPlaceHolder(i18n.Current.ServerAddress)

	mw.tokenEntry = widget.NewEntry()
	mw.tokenEntry.SetPlaceHolder(i18n.Current.Token)
	mw.tokenEntry.Password = true // Скрыто, используется при подключении из сохранённых

	mw.connectionBtn = widget.NewButton("🔌", mw.handleConnectionToggle)
	mw.connectionBtn.Importance = widget.HighImportance // Синяя по умолчанию

	// Прогрессбар места на флешке (размер как у StorageProgressBar, не как поле ввода)
	mw.sdStorageProgress = NewStorageProgressBar()
	mw.sdStorageProgress.Hide()

	// Устанавливаем hostEntry в backupWidget после создания адресной строки
	if mw.backupWidget != nil {
		mw.backupWidget.UpdateHostEntry(mw.hostEntry)
	}
}

// setDefaultValues устанавливает начальные значения для полей (после создания UI)
func (mw *MainWindow) setDefaultValues() {
	// Поля должны быть пустыми по умолчанию
	mw.hostEntry.SetText("")
	mw.tokenEntry.SetText("")
}

// recreateContainers пересоздает контейнеры с менеджером подключений
func (mw *MainWindow) recreateContainers() {
	// Callback для обновления прогрессбара места на флешке (из disk/backup виджетов)
	storageUpdate := func(usedPct float64, available, total int64) {
		fyne.Do(func() {
			if mw.sdStorageProgress == nil {
				return
			}
			if total <= 0 {
				mw.sdStorageProgress.Hide()
				return
			}
			mw.sdStorageProgress.SetValue(usedPct)
			used := total - available
			if used < 0 {
				used = 0
			}
			mw.sdStorageProgress.SetSizeText(models.FormatStorageSizeOnly(used, total))
			mw.sdStorageProgress.Show()
		})
	}
	if mw.diskWidget != nil {
		mw.diskWidget.SetOnStorageInfoUpdate(storageUpdate)
		if mw.videoWidget != nil {
			mw.diskWidget.SetOnMouseTypeChanged(mw.videoWidget.SetMouseInputMode)
		}
	}
	if mw.backupWidget != nil {
		mw.backupWidget.SetOnStorageInfoUpdate(storageUpdate)
	}

	// Сначала создаём статус-бар (иконки устройств и statusPanel), затем адресную строку (она использует statusPanel)
	statusBar := mw.createStatusBar()
	addressBar := mw.createAddressBar()

	// Создаем вкладки для основного интерфейса
	mw.tabs = container.NewAppTabs(
		container.NewTabItem(i18n.Current.TabDevices, mw.diskWidget.GetContainer()),
		container.NewTabItem(i18n.Current.TabControl, mw.videoWidget.GetContainer()),
		container.NewTabItem(i18n.Current.TabSnapshots, mw.createBackupFlashTab()),
	)

	// Обработчик переключения вкладок для показа/скрытия кнопок устройств
	mw.tabs.OnSelected = func(tab *container.TabItem) {
		mw.updateDeviceButtonsVisibility()
	}

	// Обновляем основной контент (с вкладками)
	mw.mainContent = container.NewBorder(
		addressBar, // Верх
		statusBar,  // Низ
		nil,        // Лево
		nil,        // Право
		mw.tabs,    // Центр
	)

	// Обновляем контент менеджера подключений (тоже с адресной строкой сверху)
	mw.connectionContent = container.NewBorder(
		addressBar,                          // Верх - адресная строка
		statusBar,                           // Низ - статус бар
		nil,                                 // Лево
		nil,                                 // Право
		mw.connectionManager.GetContainer(), // Центр - менеджер подключений
	)

	// По умолчанию показываем менеджер подключений (не подключены)
	mw.window.SetContent(mw.connectionContent)
}

// createAddressBar создает адресную строку: хост | [Power LED] [HDD LED] | прогрессбар | иконки устройств | кнопка подключения
func (mw *MainWindow) createAddressBar() *fyne.Container {
	mw.pcpanelWidget = NewPCPanelWidget(mw.window)
	rightPart := container.NewHBox(mw.pcpanelWidget.GetContainer(), mw.sdStorageProgress, mw.statusPanel, mw.connectionBtn)
	return container.New(
		layout.NewBorderLayout(nil, nil, nil, rightPart),
		mw.hostEntry,
		rightPart,
	)
}

// createStatusBar создает строку состояния
func (mw *MainWindow) createStatusBar() *fyne.Container {
	// Создаем иконки статуса как кнопки (для поддержки tooltip)
	mw.connectionIcon = widget.NewButton("🔌", func() {})
	mw.connectionIcon.Importance = widget.LowImportance

	mw.nbdIcon = widget.NewButton("💿", func() {})
	mw.nbdIcon.Importance = widget.LowImportance

	mw.videoIcon = widget.NewButton("📺", func() {})
	mw.videoIcon.Importance = widget.LowImportance

	mw.keyboardIcon = widget.NewButton("⌨️", func() {
		// Открываем виртуальную клавиатуру через videoWidget
		if mw.videoWidget != nil {
			mw.videoWidget.HandleVirtualKeyboard()
		}
	})
	mw.keyboardIcon.Importance = widget.LowImportance

	mw.mouseIcon = widget.NewButton("🖱️", func() {})
	mw.mouseIcon.Importance = widget.LowImportance

	mw.rndisIcon = widget.NewButton("🌐", func() {})
	mw.rndisIcon.Importance = widget.LowImportance

	mw.cdromIcon = widget.NewButton("💿", func() {})
	mw.cdromIcon.Importance = widget.LowImportance

	mw.backupIcon = widget.NewButton("🛡️", func() {})
	mw.backupIcon.Importance = widget.LowImportance

	mw.snapshotIcon = widget.NewButton("📸", func() {})
	mw.snapshotIcon.Importance = widget.LowImportance

	// Создаем панель статусов - она будет обновляться динамически
	mw.statusPanel = container.NewHBox()
	// НЕ вызываем updateStatusBar() здесь - она будет вызвана после ShowAndRun()
	// на Android это происходит до инициализации UI потока

	// Получаем кнопки устройств для размещения слева
	mountBtn, unmountBtn, addImageBtn := mw.diskWidget.GetButtons()

	// Компактная панель кнопок устройств - просто в ряд
	mw.deviceButtonsPanel = container.NewHBox(
		mountBtn,
		addImageBtn,
		unmountBtn,
	)

	// Изначально кнопки скрыты (будут показаны на вкладке "Устройство")
	mw.deviceButtonsPanel.Hide()

	// Нижняя строка: только кнопки устройств слева (иконки устройств перенесены в адресную строку)
	return container.NewBorder(
		nil, nil, // Верх, низ
		mw.deviceButtonsPanel, // Лево - кнопки устройств
		nil,                   // Право - пусто (иконки в адресной строке)
		nil,                   // Центр (пустой)
	)
}

// updateDeviceButtonsVisibility обновляет видимость кнопок устройств в зависимости от активной вкладки
func (mw *MainWindow) updateDeviceButtonsVisibility() {
	if mw.tabs == nil || mw.deviceButtonsPanel == nil {
		return
	}

	// Все UI операции через fyne.Do для безопасности на Android
	fyne.Do(func() {
		// Показываем кнопки только на вкладке "Устройство" (индекс 0)
		if mw.tabs.SelectedIndex() == 0 {
			mw.deviceButtonsPanel.Show()
		} else {
			mw.deviceButtonsPanel.Hide()
		}
		mw.deviceButtonsPanel.Refresh()
	})
}

// updateStatusBar обновляет панель статусов - показывает только подключенные иконки в одну строку
func (mw *MainWindow) updateStatusBar() {
	// Проверяем статус клавиатуры, мыши, RNDIS и дисков (НЕ UI операции)
	keyboardConnected := false
	mouseConnected := false
	rndisConnected := false
	cdromConnected := false
	backupConnected := false
	snapshotConnected := false

	// Добавляем иконку NBD только если есть подключенные клиенты
	nbdConnected := false
	if mw.nbdServer.IsRunning() {
		clients := mw.nbdServer.GetClients()
		nbdConnected = len(clients) > 0
	}

	// Проверяем статус видео
	videoStreaming := false
	if mw.videoWidget != nil && mw.videoWidget.IsStreaming() {
		videoStreaming = true
	}

	if mw.usbClient != nil {
		deviceInfo, err := mw.usbClient.GetDeviceInfo()
		if err == nil {
			logrus.Infof("🔍 updateStatusBar: найдено %d устройств", len(deviceInfo.Devices))
			for _, device := range deviceInfo.Devices {
				logrus.Infof("🔍 Устройство: Type=%s, Status=%s, Name=%s, ProductName=%s",
					device.Type, device.Status, device.Name, device.ProductName)
				if device.Status == "connected" {
					if device.Type == "keyboard" || strings.HasPrefix(device.Type, "keyboard:") {
						keyboardConnected = true
					}
					// Мышь, тачскрин или absolute — показываем иконку манипулятора
					if device.Type == "mouse" || device.Type == "touchscreen" || device.Type == "absolute" || strings.HasPrefix(device.Type, "mouse:") {
						mouseConnected = true
					}
					if device.Type == "rndis" || strings.HasPrefix(device.Type, "rndis:") {
						rndisConnected = true
					}
					// Проверяем CD-ROM образы (образы с SD карты - local, но не data)
					if device.Type == "local" && !strings.Contains(device.Name, "data") {
						cdromConnected = true
					}
					// Проверяем Backup Flash (MTP устройство с именем data, без snapshot)
					if device.Type == "mtp" && strings.Contains(device.Name, "data") && !strings.Contains(device.ProductName, "snapshot") {
						backupConnected = true
					}
					// Проверяем снапшоты (NBD устройства ИЛИ MTP со snapshot в ProductName или Name)
					if device.Type == "nbd" || (device.Type == "mtp" && (strings.Contains(device.ProductName, "snapshot") || strings.Contains(device.Name, "snapshot"))) {
						snapshotConnected = true
						logrus.Infof("📸 Найден снапшот: Type=%s, Name=%s, ProductName=%s", device.Type, device.Name, device.ProductName)
					}
				}
			}
		}

		// Дополнительно проверяем снапшоты через backupWidget (т.к. сервер отключает устройства)
		if mw.backupWidget != nil && !snapshotConnected {
			// Получаем список снапшотов и проверяем поле Connected
			snapshotsResp, err := mw.usbClient.GetSnapshots()
			if err == nil {
				for _, snapshot := range snapshotsResp.Snapshots {
					if snapshot.Connected {
						snapshotConnected = true
						logrus.Infof("📸 Найден подключенный снапшот через API снапшотов: %s", snapshot.Name)
						break
					}
				}
			}
		}
	}

	logrus.Infof("🔍 Статусы: keyboard=%v, mouse=%v, rndis=%v, cdrom=%v, backup=%v, snapshot=%v",
		keyboardConnected, mouseConnected, rndisConnected, cdromConnected, backupConnected, snapshotConnected)

	// ВСЕ UI операции обязательно через fyne.Do для безопасности на Android
	fyne.Do(func() {
		// Собираем все иконки
		var allIcons []fyne.CanvasObject

		if nbdConnected {
			mw.nbdIcon.Importance = widget.HighImportance
			allIcons = append(allIcons, mw.nbdIcon)
		}

		// Добавляем иконку видео если запущено
		if videoStreaming {
			mw.videoIcon.Importance = widget.HighImportance
			allIcons = append(allIcons, mw.videoIcon)
		}

		// Добавляем иконку клавиатуры если подключена
		if keyboardConnected {
			mw.keyboardIcon.Importance = widget.HighImportance
			allIcons = append(allIcons, mw.keyboardIcon)
		}

		// Добавляем иконку мыши если подключена
		if mouseConnected {
			mw.mouseIcon.Importance = widget.HighImportance
			allIcons = append(allIcons, mw.mouseIcon)
		}

		// Добавляем иконку RNDIS если подключена
		if rndisConnected {
			mw.rndisIcon.Importance = widget.HighImportance
			allIcons = append(allIcons, mw.rndisIcon)
		}

		// Добавляем иконку CD-ROM если подключен
		if cdromConnected {
			mw.cdromIcon.Importance = widget.HighImportance
			allIcons = append(allIcons, mw.cdromIcon)
		}

		// Добавляем иконку Backup Flash если подключена
		if backupConnected {
			mw.backupIcon.Importance = widget.HighImportance
			allIcons = append(allIcons, mw.backupIcon)
		}

		// Добавляем иконку снапшота если подключен
		if snapshotConnected {
			mw.snapshotIcon.Importance = widget.HighImportance
			allIcons = append(allIcons, mw.snapshotIcon)
		}

		// Обновляем панель статусов - компактные иконки без разделителей
		if len(allIcons) > 0 {
			// Размещаем иконки вплотную друг к другу БЕЗ разделителей
			mw.statusPanel.Objects = allIcons
		} else {
			mw.statusPanel.Objects = []fyne.CanvasObject{}
		}
		mw.statusPanel.Refresh()
	})
}

// createBackupFlashTab создает вкладку Backup Flash
func (mw *MainWindow) createBackupFlashTab() fyne.CanvasObject {
	return mw.backupWidget.GetContainer()
}

// setupEventHandlers настраивает обработчики событий
func (mw *MainWindow) setupEventHandlers() {
	mw.window.SetCloseIntercept(func() {
		mw.handleClose()
	})
}

// handleHostChanged обрабатывает изменение IP адреса
func (mw *MainWindow) handleHostChanged(host string) {
	if host == "" {
		return
	}

	// Создаем временный USB клиент для проверки соединения и загрузки устройств
	tempClient := api.NewUSBClient(host, mw.config.USBPort, mw.config.APITimeout)

	// Обновляем GStreamer сервис с новым хостом
	mw.gstreamerService.UpdateHost(host)

	// Обновляем виджеты с новым клиентом для загрузки устройств
	mw.diskWidget.UpdateClient(tempClient)
	mw.videoWidget.UpdateClient(tempClient)
	if mw.backupWidget != nil {
		mw.backupWidget.UpdateClient(tempClient)
	}

	logrus.Infof("🔄 Обновлен хост: %s", host)
}

// showConnectionManager показывает менеджер подключений
func (mw *MainWindow) showConnectionManager() {
	// Все UI операции через fyne.Do для безопасности на Android
	fyne.Do(func() {
		// Скрываем кнопки устройств при показе менеджера подключений
		if mw.deviceButtonsPanel != nil {
			mw.deviceButtonsPanel.Hide()
		}
		mw.window.SetContent(mw.connectionContent)
	})
}

// showMainContent показывает основной интерфейс
func (mw *MainWindow) showMainContent() {
	// Все UI операции через fyne.Do для безопасности на Android
	fyne.Do(func() {
		mw.window.SetContent(mw.mainContent)
	})
	// Обновляем видимость кнопок устройств при показе основного контента
	mw.updateDeviceButtonsVisibility()
}

// handleConnectionFromManager обрабатывает подключение из менеджера (стрелка на карточке).
// Заполняет поля и вызывает единый обработчик handleConnectionToggle для защиты от множественных нажатий.
func (mw *MainWindow) handleConnectionFromManager(host, token string) {
	mw.hostEntry.SetText(host)
	mw.tokenEntry.SetText(token)
	mw.handleConnectionToggle()
}

// handleSaveFromDeepLink сохраняет данные из deep link БЕЗ подключения
func (mw *MainWindow) handleSaveFromDeepLink(name, host, token string) {
	logrus.Infof("💾 handleSaveFromDeepLink вызван с: name='%s', host='%s', token='%s'", name, host, token)

	// Заполняем поля
	fyne.Do(func() {
		mw.hostEntry.SetText(host)
		mw.tokenEntry.SetText(token)
	})

	// Сохраняем через ConnectionManager (name будет пустой - сгенерируется автоматически)
	if mw.connectionManager != nil {
		generatedName := mw.connectionManager.SaveConnection(name, host, token)
		logrus.Infof("✅ Подключение '%s' сохранено", generatedName)

		// Показываем информационное сообщение о сохранении
		fyne.Do(func() {
			// Используем простой лог вместо диалога, чтобы не блокировать UI
			logrus.Infof("💾 Сохранено как: %s", generatedName)
		})
	} else {
		logrus.Warn("⚠️ ConnectionManager не инициализирован")
	}
}

// handleConnectionToggle переключает состояние подключения
func (mw *MainWindow) handleConnectionToggle() {
	// Проверяем, не выполняется ли уже операция подключения/отключения
	if mw.isConnectionPending {
		logrus.Warn("⚠️ Операция подключения/отключения уже выполняется, игнорируем повторное нажатие")
		return
	}

	// Устанавливаем флаг блокировки
	mw.isConnectionPending = true
	mw.connectionBtn.Disable()

	// Разблокируем через 5 секунд
	go func() {
		time.Sleep(5 * time.Second)
		mw.isConnectionPending = false
		fyne.Do(func() {
			if !mw.connectionBtn.Disabled() {
				// Если кнопка уже разблокирована внутри - не трогаем
				return
			}
			mw.connectionBtn.Enable()
		})
	}()

	if mw.isConnected {
		mw.handleDisconnect()
	} else {
		mw.handleConnect()
	}
}

// handleConnect обрабатывает подключение
func (mw *MainWindow) handleConnect() {
	logrus.Infof("🔍 [DEBUG] handleConnect() called")

	// Получаем IP адрес и токен из полей
	host := mw.hostEntry.Text
	token := mw.tokenEntry.Text

	logrus.Infof("🔍 [DEBUG] Read from inputs: host='%s', token='%s'", host, token)
	logrus.Infof("🔍 [DEBUG] Token from config: '%s'", mw.config.FRPAuthToken)

	if host == "" {
		logrus.Warn("Enter a server address")
		return
	}

	if token == "" {
		logrus.Warnf("🔍 [DEBUG] Token is empty, using token from config: '%s'", mw.config.FRPAuthToken)
		token = mw.config.FRPAuthToken // Используем токен из конфига если не указан
	} else {
		logrus.Infof("🔍 [DEBUG] Token is not empty, using the value from the input: '%s'", token)
	}

	logrus.Infof("🔍 [DEBUG] Final connection parameters: host='%s', token='%s'", host, token)
	logrus.Info("Establishing QUIC connection...")

	// Визуальная обратная связь - блокируем кнопку и меняем текст
	originalText := mw.connectionBtn.Text
	mw.connectionBtn.SetText("⏳")
	mw.connectionBtn.Importance = widget.WarningImportance
	mw.connectionBtn.Disable()

	// Блокируем поля ввода на время подключения
	mw.hostEntry.Disable()
	mw.tokenEntry.Disable()

	// ВАЖНО: Обновляем UI и даём время отрисовать песочные часы перед блокирующими операциями
	mw.window.Canvas().Refresh(mw.connectionBtn)

	_ = originalText // Для совместимости

	// Запускаем подключение в горутине, чтобы UI успел показать ⏳
	go func() {
		time.Sleep(100 * time.Millisecond) // даём UI отрисовать песочные часы
		mw.doConnect(host, token)
	}()
}

// doConnect выполняет блокирующую логику подключения (вызывается из горутины)
func (mw *MainWindow) doConnect(host, token string) {
	// Создаем FRP клиент с QUIC если включено
	if mw.config.FRPEnabled {
		logrus.Infof("🔍 [DEBUG] Creating FRP service: host='%s', port=%d, token='%s'", host, mw.config.FRPServerPort, token)
		mw.frpService = service.NewFRPService(
			host,
			mw.config.FRPServerPort,
			token,
		)
		logrus.Infof("🔍 [DEBUG] FRP service created")

		// Устанавливаем QUIC туннель через FRP
		if err := mw.frpService.Connect(mw.config.USBPort, mw.config.NBDPort, mw.config.VideoUDPPort); err != nil {
			logrus.Errorf("❌ Failed to establish QUIC tunnel: %v", err)
			fyne.Do(func() {
				mw.connectionBtn.SetText("🔌")
				mw.connectionBtn.Importance = widget.MediumImportance
				mw.connectionBtn.Enable()
				mw.hostEntry.Enable()
				mw.tokenEntry.Enable()
			})
			return
		}

		logrus.Info("✅ QUIC tunnel established via FRP")
		logrus.Info("Waiting for tunnel stabilization...")

		// Ждем стабилизации портов (FRP нужно время на полную настройку проброса)
		time.Sleep(2 * time.Second)

		// Блокируем адресную строку и токен
		mw.hostEntry.Disable()
		mw.tokenEntry.Disable()

		// Подключаемся к локальным портам которые слушают STCP visitors
		httpPort, videoPort, _ := mw.frpService.GetServerPorts()

		// Приложение подключается к локальным visitor портам
		mw.usbClient = api.NewUSBClient("127.0.0.1", httpPort, mw.config.APITimeout)

		// GStreamer подключается к локальным visitor портам (UDP видео)
		mw.gstreamerService.UpdateHost("127.0.0.1")
		mw.gstreamerService.UpdateVideoPort(videoPort) // FRP туннелирует видео порт
		mw.gstreamerService.UpdateVideoUDPPort(videoPort)
		mw.videoWidget.SetFRPService(mw.frpService)
		mw.diskWidget.SetFRPService(mw.frpService)

		logrus.Info("🔌 Ready to connect through FRP tunnel")

		// NBD серверы работают локально на клиенте localhost:10809-10824
		// Сервер подключается к нам через FRP туннель (все 16 портов уже настроены)
	} else {
		// Прямое подключение без FRP (старый метод)
		logrus.Info("Testing connection...")
		tempClient := api.NewUSBClient(host, mw.config.USBPort, mw.config.APITimeout)

		// Тестируем соединение
		if err := tempClient.TestConnection(); err != nil {
			logrus.Errorf("Connection failed: %v", err)
			fyne.Do(func() {
				mw.connectionBtn.SetText("🔌")
				mw.connectionBtn.Importance = widget.MediumImportance
				mw.connectionBtn.Enable()
				mw.hostEntry.Enable()
				mw.tokenEntry.Enable()
			})
			return
		}

		mw.usbClient = tempClient
		mw.gstreamerService.UpdateHost(host)
		mw.diskWidget.SetFRPService(nil) // Прямое подключение — FRP не используется

		// Тестируем соединение только для прямого подключения
		if err := tempClient.TestConnection(); err != nil {
			logrus.Errorf("Connection failed: %v", err)
			fyne.Do(func() { mw.connectionBtn.Enable() })
			return
		}
	}

	// Для FRP туннеля проверяем соединение после стабилизации
	logrus.Info("Verifying connection...")
	if err := mw.usbClient.TestConnection(); err != nil {
		logrus.Errorf("❌ Connection verification failed: %v", err)
		if mw.frpService != nil && mw.frpService.IsRunning() {
			mw.frpService.Disconnect()
			mw.frpService = nil
		}
		mw.usbClient = nil
		fyne.Do(func() {
			mw.connectionBtn.SetText("🔌")
			mw.connectionBtn.Importance = widget.HighImportance
			mw.connectionBtn.Enable()
			mw.hostEntry.Enable()
			mw.tokenEntry.Enable()
		})
		return
	}
	logrus.Info("Loading devices...")

	// Загружаем устройства и обновляем виджеты
	mw.diskWidget.UpdateClient(mw.usbClient)
	mw.videoWidget.UpdateClient(mw.usbClient)
	if mw.backupWidget != nil {
		mw.backupWidget.UpdateClient(mw.usbClient)
	}

	mw.isConnected = true
	mw.appState.IsConnected = true
	mw.appState.LastConnected = time.Now()

	fyne.Do(func() {
		mw.connectionBtn.SetText("❌")
		mw.connectionBtn.Importance = widget.SuccessImportance
		mw.connectionBtn.Enable()
		if mw.pcpanelWidget != nil {
			mw.pcpanelWidget.SetClient(mw.usbClient)
		}
		mw.updateStatus()
		mw.showMainContent()
	})

	logrus.Info("✅ Connected to USBridge via QUIC")
}

// handleDisconnect обрабатывает отключение
func (mw *MainWindow) handleDisconnect() {
	logrus.Info("Disconnecting...")

	// Останавливаем видео если запущено (проверяем через videoWidget)
	if mw.videoWidget != nil && mw.videoWidget.IsStreaming() {
		logrus.Info("🛑 Stopping video before disconnect...")
		mw.videoWidget.handleStopVideo()
	}

	// Останавливаем сервис USBridge 2
	if mw.usbClient != nil {
		if err := mw.usbClient.StopService(); err != nil {
			logrus.Errorf("Failed to stop service: %v", err)
		}
	}

	// Останавливаем NBD сервер
	if mw.nbdServer.IsRunning() {
		if err := mw.nbdServer.Stop(); err != nil {
			logrus.Errorf("Failed to stop NBD server: %v", err)
		}
	}

	// Отключаем FRP туннель только по кнопке отключения (не зависит от видео старт/стоп)
	if mw.frpService != nil && mw.frpService.IsRunning() {
		if err := mw.frpService.Disconnect(); err != nil {
			logrus.Errorf("Failed to stop FRP tunnel: %v", err)
		}
		mw.frpService = nil
	}

	mw.isConnected = false
	mw.isStreaming = false
	mw.appState.IsConnected = false
	mw.appState.IsStreaming = false
	mw.appState.IsNBDRunning = false
	mw.appState.LastDisconnected = time.Now()
	mw.usbClient = nil

	// Очищаем виджеты от данных подключения
	mw.diskWidget.UpdateClient(nil)
	mw.diskWidget.SetFRPService(nil)
	mw.videoWidget.UpdateClient(nil)
	mw.videoWidget.SetFRPService(nil)
	if mw.backupWidget != nil {
		mw.backupWidget.UpdateClient(nil)
	}

	// Обновляем иконку кнопки на "подключиться" - синяя
	mw.connectionBtn.SetText("🔌")
	mw.connectionBtn.Importance = widget.HighImportance
	mw.connectionBtn.Enable()

	if mw.pcpanelWidget != nil {
		mw.pcpanelWidget.SetClient(nil)
	}

	mw.updateStatus()
	mw.hostEntry.Enable()  // Разблокируем адресную строку для изменения
	mw.tokenEntry.Enable() // Разблокируем поле токена

	// Переключаемся на менеджер подключений
	mw.showConnectionManager()

	logrus.Info("🛑 Disconnected from USBridge")
}

// handleRefresh обрабатывает обновление
func (mw *MainWindow) handleRefresh() {
	// Проверяем что есть активное подключение перед обновлением
	if !mw.isConnected || mw.usbClient == nil {
		logrus.Warn("Cannot refresh: no active connection")
		return
	}

	mw.diskWidget.Refresh()
	mw.videoWidget.Refresh()
	if mw.backupWidget != nil {
		mw.backupWidget.Refresh()
	}
}

// updateStatus обновляет статус в интерфейсе
func (mw *MainWindow) updateStatus() {
	// Обновляем статус NBD для appState
	nbdConnected := false
	if mw.nbdServer.IsRunning() {
		clients := mw.nbdServer.GetClients()
		nbdConnected = len(clients) > 0
	}
	mw.appState.IsNBDRunning = nbdConnected

	// Обновляем статус видео для appState
	if mw.videoWidget != nil && mw.videoWidget.IsStreaming() {
		mw.appState.IsStreaming = true
		mw.isStreaming = true
	} else {
		mw.appState.IsStreaming = false
		mw.isStreaming = false
	}

	// Обновляем панель статусов
	mw.updateStatusBar()
}

// handleClose обрабатывает закрытие приложения
func (mw *MainWindow) handleClose() {
	// Проверяем, находимся ли мы в полноэкранном режиме
	if mw.videoWidget != nil && mw.videoWidget.fullscreenDialog != nil {
		if mw.videoWidget.fullscreenDialog.IsFullscreen() {
			// Если в полноэкранном режиме, выходим из него вместо закрытия приложения
			logrus.Info("🔍 handleClose: обнаружен полноэкранный режим, выходим из него")
			mw.videoWidget.fullscreenDialog.exitFullscreen()
			return
		}
	}

	// Обычное закрытие приложения
	if mw.isConnected {
		mw.handleDisconnect()
	}
	mw.window.Close()
}

// Show показывает окно
func (mw *MainWindow) Show() {
	// Запускаем UI поток БЕЗ создания loading экрана
	// На Android любое создание UI до ShowAndRun() вызывает ошибки threading
	go func() {
		// Ждем инициализации UI потока (увеличили задержку для Android)
		time.Sleep(200 * time.Millisecond)

		// ВСЁ создание UI ПОСЛЕ ShowAndRun в fyne.Do
		fyne.Do(func() {
			// Создаем интерфейс (сначала, чтобы были hostEntry и tokenEntry)
			mw.createInterface()

			// Создаем менеджер подключений (после создания hostEntry и tokenEntry)
			mw.connectionManager = NewConnectionManager(mw.app, mw.window, mw.hostEntry, mw.tokenEntry, mw.handleConnectionFromManager)

			// Пересоздаем контейнеры с менеджером
			mw.recreateContainers()

			// Настраиваем обработчики событий
			mw.setupEventHandlers()

			// Устанавливаем начальные значения для полей (после создания UI)
			mw.setDefaultValues()

			// Показываем менеджер подключений по умолчанию (не подключены)
			mw.showConnectionManager()

			// Обновляем статус бар
			mw.updateStatusBar()

			// Создаем ОДИН глобальный handler для deep links
			mw.deepLinkHandler = NewDeepLinkHandler(mw.handleConnectionFromManager, mw.handleSaveFromDeepLink)

			// Проверяем наличие deep link (для Android)
			mw.checkDeepLink()

			// Запускаем мониторинг deep links в фоне
			mw.startDeepLinkMonitoring()

			// Устанавливаем callback для изменения языка
			mw.connectionManager.SetLanguageChangeCallback(mw.reloadUI)
		})
	}()

	mw.window.ShowAndRun()
}

// reloadUI перезагружает весь UI с новым языком
func (mw *MainWindow) reloadUI() {
	logrus.Info("🔄 Reloading UI with new language...")

	// Сохраняем текущие значения полей
	currentHost := mw.hostEntry.Text
	currentToken := mw.tokenEntry.Text
	wasConnected := mw.isConnected

	// Пересоздаем интерфейс
	mw.createInterface()

	// Восстанавливаем значения
	mw.hostEntry.SetText(currentHost)
	mw.tokenEntry.SetText(currentToken)

	// Пересоздаем менеджер подключений с новым языком
	mw.connectionManager = NewConnectionManager(mw.app, mw.window, mw.hostEntry, mw.tokenEntry, mw.handleConnectionFromManager)
	mw.connectionManager.SetLanguageChangeCallback(mw.reloadUI)

	// Пересоздаем контейнеры
	mw.recreateContainers()

	// Обновляем заголовок окна
	mw.window.SetTitle(i18n.Current.AppTitle)

	// Показываем правильный экран
	if wasConnected {
		if mw.pcpanelWidget != nil && mw.usbClient != nil {
			mw.pcpanelWidget.SetClient(mw.usbClient)
		}
		mw.showMainContent()
	} else {
		mw.showConnectionManager()
	}

	// Обновляем статус бар
	mw.updateStatusBar()

	logrus.Info("✅ UI reloaded successfully")
}

// checkDeepLink проверяет наличие deep link при запуске
func (mw *MainWindow) checkDeepLink() {
	// Используем глобальный handler вместо создания нового
	if mw.deepLinkHandler != nil {
		mw.deepLinkHandler.CheckAndHandleDeepLink(mw.window)
	}
}

// startDeepLinkMonitoring запускает мониторинг deep links в фоне
func (mw *MainWindow) startDeepLinkMonitoring() {
	// Запускаем горутину для периодической проверки deep links
	go func() {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()

		for range ticker.C {
			// Используем глобальный handler вместо создания нового
			if mw.deepLinkHandler != nil {
				mw.deepLinkHandler.CheckAndHandleDeepLink(mw.window)
			}
		}
	}()
}
