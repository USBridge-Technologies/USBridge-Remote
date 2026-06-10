package gui

import (
	"context"
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

// MainWindow is the main window of the application.
type MainWindow struct {
	app    fyne.App
	window fyne.Window

	// Widgets
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

	// Services
	nbdServer        *service.NBDServer
	videoClient service.VideoClient
	usbClient        *api.USBClient
	frpService       *service.FRPService
	tailscaleService *service.TailscaleService

	// State
	config                  *models.AppConfig
	appState                *models.AppState
	isConnected             bool
	isStreaming             bool
        activeAPISecret         []byte
	isConnectionPending     atomic.Bool
	isConnectionLoading     bool
	connectedProtocol       string
	activeFRPToken          string
	pendingFRPToken         string // direct FRP token override from saved connection (skips sync)
	pendingTailscaleRegister bool
	pendingQUICPort         int
	lastTailscaleAuthURL    string
	tailscalePollCancel     context.CancelFunc
	currentVideoFPS         float64
	currentStorageDir       string
	currentStorageTotal     int64
	currentStorageAvailable int64
	storageStatus           *models.StorageStatusData

	// Connection/Disconnection button
	connectionBtn    *view.HeaderActionButton
	protocolSelect   *widget.Select
	protocolDropdown *view.HeaderDropdown

	// PC Panel (Power/Reset LED buttons)
	pcpanelWidget *controller.PCPanelWidget

	// Address bar
	hostEntry         *widget.Entry
	tokenEntry        *widget.Entry
	sdStorageProgress *view.StorageProgressBar
	deepLinkHandler        *DeepLinkHandler
	deepLinkMonitorStop    chan struct{}

	lifecycleMu              sync.Mutex
	lifecycleOps             chan func()

	// Status icons
	connectionIcon *widget.Button
	nbdIcon        *widget.Button
	videoIcon      *headerStatusBadgeButton
	audioIcon      *headerStatusBadgeButton
	captureIcon    *widget.Button
	keyboardIcon   *widget.Button
	mouseIcon      *widget.Button
	rndisIcon      *widget.Button
	gamepadIcon    *widget.Button
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
	w.SetPadded(false)

	mw := &MainWindow{
		app:    a,
		window: w,
		config: cfg,
		appState: &models.AppState{
			IsConnected: false,
		},
		lifecycleOps: make(chan func(), 32),
	}

	mw.nbdServer = service.NewNBDServer("127.0.0.1")
	mw.tailscaleService = service.NewTailscaleService(cfg.TailscaleUserspace)
	if cfg.VideoProtocol == models.VideoProtocolMoonlight {
		ms := service.NewMoonlightService(cfg)
		ms.SetTailscaleService(mw.tailscaleService)
		mw.videoClient = ms
	} else {
		mw.videoClient = service.NewGStreamerService(cfg)
	}

	// Initialize UI fields
	mw.createInterface()

	// Initialize widgets
	mw.diskWidget = controller.NewDiskWidget(nil, mw.updateStatus, a, cfg)
	mw.diskWidget.SetWindow(w)
	mw.videoWidget = controller.NewVideoWidgetGStreamer(w, nil, mw.videoClient, mw.updateStatus)
	mw.videoWidget.SetShowMouseCursor(a.Preferences().BoolWithFallback("show_mouse_cursor", false))
	mw.videoWidget.SetTailscaleService(mw.tailscaleService)
	mw.diskWidget.SetMoonlightProvider(mw.videoWidget.GetMoonlightInput)
	mw.backupWidget = controller.NewBackupWidget(nil, mw.hostEntry, mw.updateStatus)
	mw.backupWidget.SetWindow(w)
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

func maskSensitiveToken(token string) string {
	if len(token) <= 4 {
		return "****"
	}
	return token[:2] + "..." + token[len(token)-2:]
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

	if len(mw.activeAPISecret) > 0 {
		client.SetAPISecretV2(mw.activeAPISecret)
		// Keep Moonlight's PIN relay in sync so it uses the same HMAC key.
		if ms, ok := mw.videoClient.(interface{ SetAPISecret([]byte) }); ok {
			ms.SetAPISecret(mw.activeAPISecret)
		}
	}

	return client
}
