package gui

import (
	"fmt"
	"time"

	"usbridge-client/internal/api"
	"usbridge-client/internal/models"
	"usbridge-client/internal/service"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"
	"github.com/sirupsen/logrus"
)

// handleConnectionFromManager обрабатывает подключение из менеджера (стрелка на карточке).
// Заполняет поля и вызывает единый обработчик handleConnectionToggle для защиты от множественных нажатий.
func (mw *MainWindow) handleConnectionFromManager(host, token, protocol, wireGuardInvite string) {
	mw.hostEntry.SetText(host)
	mw.tokenEntry.SetText(token)
	mw.pendingWireGuardInvite = wireGuardInvite
	if protocol != "" {
		mw.protocolSelect.SetSelected(protocol)
	}
	mw.handleConnectionToggle()
}

// handleSaveFromDeepLink сохраняет данные из deep link БЕЗ подключения
func (mw *MainWindow) handleSaveFromDeepLink(name, host, token, protocol, wireGuardInvite string) {
	logrus.Infof("💾 handleSaveFromDeepLink вызван с: name='%s', host='%s', token='%s', protocol='%s'", name, host, token, protocol)

	fyne.Do(func() {
		mw.hostEntry.SetText(host)
		mw.tokenEntry.SetText(token)
		if protocol != "" {
			mw.protocolSelect.SetSelected(protocol)
		}
	})

	if mw.connectionManager != nil {
		mw.pendingWireGuardInvite = wireGuardInvite
		generatedName := mw.connectionManager.SaveConnection(name, host, token, protocol, wireGuardInvite)
		logrus.Infof("✅ Подключение '%s' сохранено", generatedName)
		fyne.Do(func() {
			logrus.Infof("💾 Сохранено как: %s", generatedName)
		})
	} else {
		logrus.Warn("⚠️ ConnectionManager не инициализирован")
	}
}

// handleConnectionToggle переключает состояние подключения
func (mw *MainWindow) handleConnectionToggle() {
	if mw.isConnectionPending {
		logrus.Warn("⚠️ Операция подключения/отключения уже выполняется, игнорируем повторное нажатие")
		return
	}

	mw.isConnectionPending = true
	mw.connectionBtn.Disable()

	go func() {
		time.Sleep(5 * time.Second)
		mw.isConnectionPending = false
		fyne.Do(func() {
			if !mw.connectionBtn.Disabled() {
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
		token = mw.config.FRPAuthToken
	} else {
		logrus.Infof("🔍 [DEBUG] Token is not empty, using the value from the input: '%s'", token)
	}

	logrus.Infof("🔍 [DEBUG] Final connection parameters: host='%s', token='%s'", host, token)
	logrus.Infof("Establishing connection with protocol=%s...", mw.protocolSelect.Selected)

	mw.connectionBtn.SetText("⏳")
	mw.connectionBtn.Importance = widget.WarningImportance
	mw.connectionBtn.Disable()
	mw.hostEntry.Disable()
	mw.tokenEntry.Disable()
	mw.protocolSelect.Disable()
	mw.window.Canvas().Refresh(mw.connectionBtn)

	go func() {
		time.Sleep(100 * time.Millisecond)
		mw.doConnect(host, token)
	}()
}

// doConnect выполняет блокирующую логику подключения (вызывается из горутины)
func (mw *MainWindow) doConnect(host, token string) {
	protocol := mw.protocolSelect.Selected
	if protocol == "" {
		protocol = models.ConnectionProtocolAuto
	}

	connectQUIC := func() error {
		if !mw.config.FRPEnabled {
			return fmt.Errorf("FRP disabled in config")
		}
		logrus.Infof("🔍 [DEBUG] Creating FRP service: host='%s', port=%d, token='%s'", host, mw.config.FRPServerPort, token)
		mw.frpService = service.NewFRPService(
			host,
			mw.config.FRPServerPort,
			token,
		)
		logrus.Infof("🔍 [DEBUG] FRP service created")

		if err := mw.frpService.Connect(mw.config.USBPort, mw.config.NBDPort, mw.config.VideoUDPPort); err != nil {
			return err
		}

		logrus.Info("✅ QUIC tunnel established via FRP")
		logrus.Info("Waiting for tunnel stabilization...")
		time.Sleep(2 * time.Second)

		mw.hostEntry.Disable()
		mw.tokenEntry.Disable()

		httpPort, videoPort, _ := mw.frpService.GetServerPorts()
		mw.usbClient = api.NewUSBClient("127.0.0.1", httpPort, mw.config.APITimeout)

		mw.gstreamerService.UpdateHost("127.0.0.1")
		mw.gstreamerService.UpdateVideoPort(videoPort)
		mw.gstreamerService.UpdateVideoUDPPort(videoPort)
		mw.config.VideoBindHost = "127.0.0.1"
		mw.videoWidget.SetFRPService(mw.frpService)
		mw.diskWidget.SetFRPService(mw.frpService)
		mw.connectedProtocol = models.ConnectionProtocolQUIC
		mw.config.NBDBindHost = "127.0.0.1"

		logrus.Info("🔌 Ready to connect through FRP tunnel")
		return nil
	}

	connectWireGuard := func() error {
		if !mw.config.WireGuardEnabled {
			return fmt.Errorf("WireGuard disabled in config")
		}
		if !mw.config.FRPEnabled {
			return fmt.Errorf("FRP bootstrap is required for WireGuard mode but FRP is disabled")
		}
		logrus.Info("🔐 [WireGuard] STEP 1: starting FRP bootstrap transport")
		if err := connectQUIC(); err != nil {
			return fmt.Errorf("wireguard bootstrap over FRP failed at transport stage: %w", err)
		}
		if mw.frpService == nil || !mw.frpService.IsRunning() {
			return fmt.Errorf("FRP bootstrap transport is not running")
		}

		logrus.Info("🔐 [WireGuard] STEP 2: creating WireGuard client keypair")
		wg := service.NewWireGuardService(mw.config)
		pub, err := wg.GeneratePublicKey()
		if err != nil {
			return err
		}
		httpPort, _, _ := mw.frpService.GetServerPorts()
		logrus.Infof("🔐 [WireGuard] STEP 3: requesting bootstrap via FRP localhost visitor port=%d", httpPort)
		bootstrapClient := api.NewUSBClient("127.0.0.1", httpPort, mw.config.APITimeout)
		bootstrap, err := bootstrapClient.BootstrapWireGuard(&models.WireGuardBootstrapRequest{
			Token:           token,
			ClientName:      "usbridge-client",
			ClientPublicKey: pub,
			EndpointHost:    host,
			ServerHost:      host,
		})
		if err != nil {
			return fmt.Errorf("wireguard bootstrap API failed: %w", err)
		}
		logrus.Infof("🔐 [WireGuard] STEP 4: bootstrap received server=%s client=%s endpoint=%s:%d", bootstrap.ServerAddress, bootstrap.ClientAddress, bootstrap.ServerEndpointHost, bootstrap.ServerEndpointPort)
		logrus.Info("🔐 [WireGuard] STEP 5: bringing up local WireGuard interface")
		if err := wg.Connect(bootstrap); err != nil {
			return err
		}
		mw.wgService = wg
		logrus.Infof("✅ [WireGuard] STEP 6: interface up client=%s server=%s", wg.GetClientHost(), wg.GetServerHost())
		mw.config.NBDBindHost = wg.GetClientHost()
		mw.usbClient = api.NewUSBClient(wg.GetServerHost(), mw.config.USBPort, mw.config.APITimeout)
		mw.gstreamerService.UpdateHost(wg.GetServerHost())
		mw.gstreamerService.UpdateVideoPort(mw.config.VideoUDPPort)
		mw.gstreamerService.UpdateVideoUDPPort(mw.config.VideoUDPPort)
		mw.config.VideoBindHost = wg.GetClientHost()
		mw.videoWidget.SetFRPService(nil)
		mw.diskWidget.SetFRPService(nil)
		mw.connectedProtocol = models.ConnectionProtocolWireGuard
		logrus.Info("🔐 [WireGuard] STEP 7: switching application traffic to WireGuard")
		logrus.Infof("   📥 HTTP API switched to WireGuard: %s:%d", wg.GetServerHost(), mw.config.USBPort)
		logrus.Infof("   📤 Local WireGuard client address: %s", wg.GetClientHost())
		return nil
	}

	switch protocol {
	case models.ConnectionProtocolWireGuard:
		if err := connectWireGuard(); err != nil {
			mw.handleConnectFailure("❌ Failed to establish WireGuard tunnel", err)
			return
		}
	case models.ConnectionProtocolQUIC:
		if err := connectQUIC(); err != nil {
			mw.handleConnectFailure("❌ Failed to establish QUIC tunnel", err)
			return
		}
	case models.ConnectionProtocolAuto:
		if err := connectWireGuard(); err != nil {
			logrus.Warnf("⚠️ WireGuard auto-connect failed, falling back to QUIC: %v", err)
			if err := connectQUIC(); err != nil {
				mw.handleConnectFailure("❌ Failed to establish connection in auto mode", err)
				return
			}
		}
	default:
		logrus.Info("Testing connection...")
		tempClient := api.NewUSBClient(host, mw.config.USBPort, mw.config.APITimeout)
		if err := tempClient.TestConnection(); err != nil {
			mw.handleConnectFailure("Connection failed", err)
			return
		}

		mw.usbClient = tempClient
		mw.gstreamerService.UpdateHost(host)
		mw.config.VideoBindHost = "127.0.0.1"
		mw.diskWidget.SetFRPService(nil)
		mw.connectedProtocol = "direct"

		if err := tempClient.TestConnection(); err != nil {
			logrus.Errorf("Connection failed: %v", err)
			fyne.Do(func() { mw.connectionBtn.Enable() })
			return
		}
	}

	logrus.Info("Verifying connection...")
	if err := mw.usbClient.TestConnection(); err != nil {
		logrus.Errorf("❌ Connection verification failed: %v", err)
		if mw.frpService != nil && mw.frpService.IsRunning() {
			mw.frpService.Disconnect()
			mw.frpService = nil
		}
		if mw.wgService != nil && mw.wgService.IsRunning() {
			mw.wgService.Disconnect()
			mw.wgService = nil
			mw.config.NBDBindHost = "127.0.0.1"
		}
		mw.usbClient = nil
		fyne.Do(func() {
			mw.isConnected = false
			mw.connectedProtocol = ""
			mw.refreshConnectionControls()
			mw.connectionBtn.Enable()
			mw.hostEntry.Enable()
			mw.tokenEntry.Enable()
			mw.protocolSelect.Enable()
		})
		return
	}

	logrus.Info("Loading devices...")
	if mw.connectedProtocol == models.ConnectionProtocolWireGuard && mw.frpService != nil && mw.frpService.IsRunning() {
		logrus.Info("🔐 [WireGuard] STEP 8: WireGuard verified, stopping FRP bootstrap transport")
		if err := mw.frpService.Disconnect(); err != nil {
			logrus.Warnf("⚠️ Failed to stop FRP after WireGuard handoff: %v", err)
		}
		mw.frpService = nil
	}

	mw.diskWidget.UpdateClient(mw.usbClient)
	mw.videoWidget.UpdateClient(mw.usbClient)
	if mw.backupWidget != nil {
		mw.backupWidget.UpdateClient(mw.usbClient)
	}

	mw.isConnected = true
	mw.appState.IsConnected = true
	mw.appState.LastConnected = time.Now()

	fyne.Do(func() {
		mw.refreshConnectionControls()
		mw.connectionBtn.Enable()
		if mw.pcpanelWidget != nil {
			mw.pcpanelWidget.SetClient(mw.usbClient)
		}
		mw.updateStatus()
		mw.showMainContent()
	})

	logrus.Infof("✅ Connected to USBridge via %s", mw.connectedProtocol)
}

func (mw *MainWindow) handleConnectFailure(message string, err error) {
	logrus.Errorf("%s: %v", message, err)
	fyne.Do(func() {
		mw.isConnected = false
		mw.connectedProtocol = ""
		mw.refreshConnectionControls()
		mw.connectionBtn.Enable()
		mw.hostEntry.Enable()
		mw.tokenEntry.Enable()
		mw.protocolSelect.Enable()
	})
}

// handleDisconnect обрабатывает отключение
func (mw *MainWindow) handleDisconnect() {
	logrus.Info("Disconnecting...")

	if mw.videoWidget != nil && mw.videoWidget.IsStreaming() {
		logrus.Info("🛑 Stopping video before disconnect...")
		mw.videoWidget.StopVideo()
	}

	if mw.usbClient != nil {
		if err := mw.usbClient.StopService(); err != nil {
			logrus.Errorf("Failed to stop service: %v", err)
		}
	}

	if mw.nbdServer.IsRunning() {
		if err := mw.nbdServer.Stop(); err != nil {
			logrus.Errorf("Failed to stop NBD server: %v", err)
		}
	}

	if mw.frpService != nil && mw.frpService.IsRunning() {
		if err := mw.frpService.Disconnect(); err != nil {
			logrus.Errorf("Failed to stop FRP tunnel: %v", err)
		}
		mw.frpService = nil
	}
	if mw.wgService != nil && mw.wgService.IsRunning() {
		if err := mw.wgService.Disconnect(); err != nil {
			logrus.Errorf("Failed to stop WireGuard tunnel: %v", err)
		}
		mw.wgService = nil
	}

	mw.isConnected = false
	mw.isStreaming = false
	mw.connectedProtocol = ""
	mw.appState.IsConnected = false
	mw.appState.IsStreaming = false
	mw.appState.IsNBDRunning = false
	mw.appState.LastDisconnected = time.Now()
	mw.usbClient = nil

	mw.diskWidget.UpdateClient(nil)
	mw.diskWidget.SetFRPService(nil)
	mw.videoWidget.UpdateClient(nil)
	mw.videoWidget.SetFRPService(nil)
	if mw.backupWidget != nil {
		mw.backupWidget.UpdateClient(nil)
	}

	mw.refreshConnectionControls()
	mw.connectionBtn.Enable()

	if mw.pcpanelWidget != nil {
		mw.pcpanelWidget.SetClient(nil)
	}

	mw.updateStatus()
	mw.hostEntry.Enable()
	mw.tokenEntry.Enable()
	mw.protocolSelect.Enable()
	mw.config.NBDBindHost = "127.0.0.1"
	mw.config.VideoBindHost = "127.0.0.1"
	mw.showConnectionManager()

	logrus.Info("🛑 Disconnected from USBridge")
}

// handleRefresh обрабатывает обновление
func (mw *MainWindow) handleRefresh() {
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
	nbdConnected := false
	if mw.nbdServer.IsRunning() {
		clients := mw.nbdServer.GetClients()
		nbdConnected = len(clients) > 0
	}
	mw.appState.IsNBDRunning = nbdConnected

	if mw.videoWidget != nil && mw.videoWidget.IsStreaming() {
		mw.appState.IsStreaming = true
		mw.isStreaming = true
	} else {
		mw.appState.IsStreaming = false
		mw.isStreaming = false
	}

	mw.updateStatusBar()
}
