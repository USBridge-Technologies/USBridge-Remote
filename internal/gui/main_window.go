package gui

import (
	"usbridge-client/internal/api"
	"usbridge-client/internal/gui/controller"
	"usbridge-client/internal/gui/i18n"
	"usbridge-client/internal/gui/view"
	"usbridge-client/internal/models"
	"usbridge-client/internal/service"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
	"github.com/sirupsen/logrus"
)

// MainWindow главное окно приложения
type MainWindow struct {
	app    fyne.App
	window fyne.Window

	// Виджеты
	diskWidget         *controller.DiskWidget
	videoWidget        *controller.VideoWidget
	backupWidget       *controller.BackupWidget
	connectionManager  *controller.ConnectionManager
	mainContent        *fyne.Container
	connectionContent  *fyne.Container
	tabs               *container.AppTabs
	deviceButtonsPanel *fyne.Container

	// Сервисы
	nbdServer        *service.NBDServer
	gstreamerService *service.GStreamerService
	usbClient        *api.USBClient
	frpService       *service.FRPService
	wgService        service.WireGuardService

	// Состояние
	config                 *models.AppConfig
	appState               *models.AppState
	isConnected            bool
	isStreaming            bool
	isConnectionPending    bool // Флаг блокировки кнопки подключения
	connectedProtocol      string
	pendingWireGuardInvite string

	// Кнопка подключения/отключения
	connectionBtn  *widget.Button
	protocolSelect *widget.Select

	// PC Panel (Power/Reset LED кнопки)
	pcpanelWidget *controller.PCPanelWidget

	// Адресная строка
	hostEntry         *widget.Entry
	tokenEntry        *widget.Entry            // Скрыто, используется для подключения из сохранённых
	sdStorageProgress *view.StorageProgressBar // Прогресс места на флешке — на всех экранах
	deepLinkHandler   *DeepLinkHandler         // Глобальный handler для deep links

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

func protocolButtonState(protocol string) (string, widget.ButtonImportance) {
	switch protocol {
	case models.ConnectionProtocolWireGuard:
		return "🛑 WG", widget.SuccessImportance
	case models.ConnectionProtocolQUIC:
		return "🛑 QUIC", widget.WarningImportance
	case "direct":
		return "🛑 LAN", widget.MediumImportance
	case models.ConnectionProtocolAuto:
		return "🛑 AUTO", widget.MediumImportance
	default:
		return "🔌", widget.HighImportance
	}
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
	window.SetPadded(false)
	// Размер из конфига; на десктопе — удобный по умолчанию для современных DPI/масштабирования
	scale := startupWindowScale(myApp)
	size := dpiAwareWindowSize(
		config.WindowWidth,
		config.WindowHeight,
		scale,
		fyne.NewSize(0, 0),
	)
	size = expandWindowSizeToPreferredArea(size, startupWindowPreferredSize(scale))
	size = clampWindowSizeToAvailableArea(size, startupWindowMaxSize(scale))
	window.Resize(size)
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
	mw.wgService = nil

	// Создаем виджеты
	mw.diskWidget = controller.NewDiskWidget(mw.usbClient, mw.updateStatus, myApp, config)
	mw.diskWidget.SetWindow(window)
	mw.videoWidget = controller.NewVideoWidgetGStreamer(mw.usbClient, mw.gstreamerService, mw.updateStatus)
	mw.videoWidget.SetParentWindow(window)
	mw.backupWidget = controller.NewBackupWidget(mw.usbClient, nil, mw.updateStatus) // hostEntry будет установлен позже
	mw.backupWidget.SetWindow(window)

	// На Android НЕ создаем UI контент до ShowAndRun() - это вызывает ошибки threading
	// Создание UI будет отложено до метода Show()

	return mw
}
