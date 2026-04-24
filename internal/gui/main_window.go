package gui

import (
	"strings"
	"sync"
	"sync/atomic"

	"usbridge-client/internal/api"
	"usbridge-client/internal/gui/controller"
	"usbridge-client/internal/gui/i18n"
	"usbridge-client/internal/gui/view"
	"usbridge-client/internal/models"
	"usbridge-client/internal/service"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
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
	mainExitBtn         *view.HeaderActionButton
	connectionFooterBar *fyne.Container

	// Сервисы
	nbdServer        *service.NBDServer
	gstreamerService *service.GStreamerService
	usbClient        *api.USBClient
	frpService       *service.FRPService
	tailscaleService *service.TailscaleService

	// Состояние
	config                  *models.AppConfig
	appState                *models.AppState
	isConnected             bool
	isStreaming             bool
	isConnectionPending     atomic.Bool
	isConnectionLoading     bool
	connectedProtocol       string
	activeQUICToken         string
	pendingTailscaleRegister bool
	pendingQUICPort         int
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
	deepLinkHandler        *DeepLinkHandler
	deepLinkMonitorStop    chan struct{}

	lifecycleMu              sync.Mutex
	lifecycleOps             chan func()

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
	isClosing                atomic.Bool
	onReadyCallback          func()
}

func NewMainWindow(cfg *models.AppConfig) *MainWindow {
	if i18n.Current == nil {
		i18n.Init("en")
	}
	a := app.NewWithID("com.usbridge.client")
	w := a.NewWindow("USBridge Client")

	mw := &MainWindow{
		app:    a,
		window: w,
		config: cfg,
		appState: &models.AppState{
			IsConnected: false,
		},
		lifecycleOps: make(chan func(), 32),
	}

	mw.nbdServer = service.NewNBDServer(cfg)
	mw.gstreamerService = service.NewGStreamerService(cfg)
	mw.tailscaleService = service.NewTailscaleService(cfg.TailscaleUserspace)

	// Initialize UI fields
	mw.createInterface()

	// Initialize widgets
	mw.diskWidget = controller.NewDiskWidget(nil, mw.updateStatus, a, cfg)
	mw.videoWidget = controller.NewVideoWidgetGStreamer(w, nil, mw.gstreamerService, mw.updateStatus)
	mw.videoWidget.SetTailscaleService(mw.tailscaleService)
	mw.backupWidget = controller.NewBackupWidget(nil, mw.hostEntry, mw.updateStatus)
	mw.pcpanelWidget = controller.NewPCPanelWidget(w)

	// Initialize connection manager
	mw.connectionManager = controller.NewConnectionManager(
		a, w, cfg,
		mw.hostEntry, mw.tokenEntry, mw.protocolSelect,
		mw.tailscaleService,
		mw.handleConnectionFromManager, mw.handleSelectionFromManager,
	)

	go mw.runLifecycleLoop()

	return mw
}

func maskSensitiveToken(quicToken string) string {
	if len(quicToken) <= 4 {
		return "****"
	}
	return quicToken[:2] + "..." + quicToken[len(quicToken)-2:]
}

func fallbackText(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func (mw *MainWindow) attachUSBClient(client *api.USBClient) *api.USBClient {
	if client == nil {
		return nil
	}

	client.SetOnTransportError(func(err error) {
		logrus.Errorf("📡 [Transport] Network error detected: %v", err)
		if mw.connectionLossInProgress.CompareAndSwap(false, true) {
			go mw.handleConnectionLost(err, client)
		}
	})

	return client
}
