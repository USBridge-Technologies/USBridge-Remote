package gui

import (
	"image/color"
	"sync"
	"sync/atomic"

	"usbridge-client/internal/api"
	"usbridge-client/internal/gui/controller"
	"usbridge-client/internal/gui/design"
	"usbridge-client/internal/gui/i18n"
	"usbridge-client/internal/gui/view"
	"usbridge-client/internal/models"
	"usbridge-client/internal/service"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
	"github.com/sirupsen/logrus"
)

// MainWindow главное окно приложения.
type MainWindow struct {
	app    fyne.App
	window fyne.Window

	// Виджеты
	diskWidget          *controller.DiskWidget
	videoWidget         *controller.VideoWidget
	backupWidget        *controller.BackupWidget
	connectionManager   *controller.ConnectionManager
	mainContent         *fyne.Container
	connectionContent   *fyne.Container
	tabs                *container.AppTabs
	deviceButtonsPanel  *fyne.Container
	deviceFooterBar     *fyne.Container
	deviceMountBtn      fyne.CanvasObject
	deviceUnmountBtn    fyne.CanvasObject
	deviceVideoBtn      *view.DeviceActionButton
	mainExitBtn         *view.HeaderActionButton
	connectionFooterBar *fyne.Container

	// Сервисы
	nbdServer        *service.NBDServer
	gstreamerService *service.GStreamerService
	usbClient        *api.USBClient
	frpService       *service.FRPService
	wgService        service.WireGuardService
	tailscaleService *service.TailscaleService

	// Состояние
	config                  *models.AppConfig
	appState                *models.AppState
	isConnected             bool
	isStreaming             bool
	isConnectionPending     bool
	isConnectionLoading     bool
	connectedProtocol       string
	activeConnectionToken   string
	pendingWireGuardInvite  string
	pendingTailscaleRegister bool
	currentVideoFPS         float64
	currentStorageDir       string
	currentStorageTotal     int64
	currentStorageAvailable int64

	// Кнопка подключения/отключения
	connectionBtn    *view.HeaderActionButton
	protocolSelect   *widget.Select
	protocolDropdown *view.HeaderDropdown

	// PC Panel (Power/Reset LED кнопки)
	pcpanelWidget *controller.PCPanelWidget

	// Адресная строка
	hostEntry         *widget.Entry
	tokenEntry        *widget.Entry
	sdStorageProgress *view.StorageProgressBar
	deepLinkHandler   *DeepLinkHandler

	// Иконки статуса
	connectionIcon *widget.Button
	nbdIcon        *widget.Button
	videoIcon      *headerStatusBadgeButton
	captureIcon    *widget.Button
	keyboardIcon   *widget.Button
	mouseIcon      *widget.Button
	rndisIcon      *widget.Button
	cdromIcon      *widget.Button
	backupIcon     fyne.CanvasObject
	snapshotIcon   *widget.Button
	statusPanel    *fyne.Container
	protocolPanel  *fyne.Container

	connectionLossInProgress atomic.Bool
	shutdownInProgress       atomic.Bool
	wireGuardMonitorStop     chan struct{}
	deepLinkMonitorStop      chan struct{}
	lifecycleMu              sync.Mutex
	lifecycleOps             chan func()
	isClosing                atomic.Bool
}

func protocolButtonState(protocol string) (string, color.Color, color.Color) {
	switch protocol {
	case models.ConnectionProtocolWireGuard:
		return "wireguard", design.ColorAccent, design.ColorBackground
	case models.ConnectionProtocolTailscale:
		return "tailscale", design.ColorProtocolQUIC, design.ColorBackground
	case models.ConnectionProtocolQUIC:
		return "quic", design.ColorProtocolQUIC, design.ColorBackground
	case "direct":
		return "online", design.ColorSurfaceLight, design.ColorTextLight
	case models.ConnectionProtocolAuto:
		return "online", design.ColorSurfaceLight, design.ColorTextLight
	default:
		return "online", design.ColorSurfaceLight, design.ColorTextLight
	}
}

func protocolStatusText(protocol string) string {
	text, _, _ := protocolButtonState(protocol)
	return text
}

// NewMainWindow создает новое главное окно.
func NewMainWindow(config *models.AppConfig) *MainWindow {
	myApp := newFyneApp()
	myApp.SetIcon(nil)

	savedLang := myApp.Preferences().StringWithFallback("language", "en")
	i18n.Init(savedLang)
	logrus.Infof("Localization initialized (%s)", savedLang)

	window := myApp.NewWindow(i18n.Current.AppTitle)
	window.SetPadded(false)

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
		app:          myApp,
		window:       window,
		config:       config,
		lifecycleOps: make(chan func(), 32),
		appState: &models.AppState{
			IsConnected:  false,
			IsStreaming:  false,
			IsNBDRunning: false,
		},
	}

	mw.nbdServer = service.NewNBDServer(config)
	mw.gstreamerService = service.NewGStreamerService(config)
	mw.tailscaleService = service.NewTailscaleService(config.TailscaleUserspace)

	logrus.Info("GStreamer service initialized")

	mw.usbClient = nil
	mw.frpService = nil
	mw.wgService = nil

	mw.diskWidget = controller.NewDiskWidget(mw.usbClient, mw.updateStatus, myApp, config)
	mw.diskWidget.SetWindow(window)
	mw.videoWidget = controller.NewVideoWidgetGStreamer(mw.usbClient, mw.gstreamerService, mw.updateStatus)
	mw.videoWidget.SetTailscaleService(mw.tailscaleService)
	mw.videoWidget.SetParentWindow(window)
	mw.backupWidget = controller.NewBackupWidget(mw.usbClient, nil, mw.updateStatus)
	mw.backupWidget.SetWindow(window)

	go mw.runLifecycleLoop()

	return mw
}
