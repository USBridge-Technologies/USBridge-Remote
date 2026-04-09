package gui

import (
	"fmt"
	"strings"
	"time"

	"usbridge-client/internal/api"
	"usbridge-client/internal/gui/i18n"
	"usbridge-client/internal/gui/view"
	"usbridge-client/internal/models"
	"usbridge-client/internal/service"

	"fyne.io/fyne/v2"
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

func (mw *MainWindow) canAttemptConnection() bool {
	return strings.TrimSpace(mw.hostEntry.Text) != ""
}

func (mw *MainWindow) setConnectionLoading(loading bool) {
	mw.isConnectionLoading = loading
	mw.refreshConnectionControls()
}

func (mw *MainWindow) clearConnectionPending() {
	mw.isConnectionPending = false
	mw.isConnectionLoading = false
	if mw.connectionManager != nil {
		mw.connectionManager.SetConnectionPending(false)
	}
}

func (mw *MainWindow) resolveConnectionToken(host, token string) string {
	resolved := strings.TrimSpace(token)
	if resolved != "" {
		return resolved
	}

	if mw.connectionManager != nil {
		resolved = mw.connectionManager.ResolveToken(host, token)
		if resolved != "" {
			logrus.Infof("🔍 [DEBUG] Resolved token from saved connection for host='%s'", host)
			return resolved
		}
	}

	resolved = strings.TrimSpace(mw.activeConnectionToken)
	if resolved != "" {
		logrus.Infof("🔍 [DEBUG] Reusing active session token for host='%s'", host)
		return resolved
	}

	resolved = strings.TrimSpace(mw.config.FRPAuthToken)
	if resolved != "" {
		logrus.Warnf("🔍 [DEBUG] Token is empty, falling back to config token for host='%s'", host)
	}
	return resolved
}

func (mw *MainWindow) attachUSBClient(client *api.USBClient) *api.USBClient {
	if client == nil {
		return nil
	}
	client.SetTransportErrorHandler(func(err error) {
		mw.handleTransportError(client, err)
	})
	return client
}

func (mw *MainWindow) handleTransportError(client *api.USBClient, err error) {
	if client == nil || err == nil || !api.IsConnectionLostError(err) {
		return
	}
	if mw.connectedProtocol == models.ConnectionProtocolWireGuard {
		// Для WireGuard потерю туннеля определяет отдельный peer-status monitor, а не фоновые HTTP-запросы.
		return
	}
	if mw.usbClient != client || !mw.isConnected {
		return
	}
	if !mw.connectionLossInProgress.CompareAndSwap(false, true) {
		return
	}

	logrus.Warnf("⚠️ Active connection lost: %v", err)
	go mw.handleConnectionLost(err, client)
}

func (mw *MainWindow) cleanupDeadConnectionState() {
	mw.stopWireGuardMonitor()

	if mw.videoWidget != nil {
		mw.videoWidget.HandleConnectionLost()
	}

	if mw.nbdServer.IsRunning() {
		if stopErr := mw.nbdServer.Stop(); stopErr != nil {
			logrus.Errorf("Failed to stop NBD server after connection loss: %v", stopErr)
		}
	}
	if mw.frpService != nil && mw.frpService.IsRunning() {
		if stopErr := mw.frpService.Disconnect(); stopErr != nil {
			logrus.Errorf("Failed to stop FRP tunnel after connection loss: %v", stopErr)
		}
		mw.frpService = nil
	}
	if mw.wgService != nil && mw.wgService.IsRunning() {
		if stopErr := mw.wgService.Disconnect(); stopErr != nil {
			logrus.Errorf("Failed to stop WireGuard tunnel after connection loss: %v", stopErr)
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
	mw.videoWidget.SetTailscaleService(nil)
	if mw.backupWidget != nil {
		mw.backupWidget.UpdateClient(nil)
	}
	if mw.pcpanelWidget != nil {
		mw.pcpanelWidget.SetClient(nil)
	}

	mw.config.NBDBindHost = "127.0.0.1"
	mw.config.VideoBindHost = "127.0.0.1"
}

func (mw *MainWindow) tryRecoverConnectionAfterLoss(client *api.USBClient, cause error) bool {
	if client == nil || client != mw.usbClient {
		return false
	}

	host := strings.TrimSpace(mw.hostEntry.Text)
	if host == "" {
		return false
	}

	token := mw.resolveConnectionToken(host, mw.tokenEntry.Text)

	protocol := mw.connectedProtocol
	if protocol == "" || protocol == "direct" {
		return false
	}

	retryDelays := []time.Duration{
		0,
		1500 * time.Millisecond,
		3 * time.Second,
	}

	for attempt, delay := range retryDelays {
		if delay > 0 {
			logrus.Infof("⏳ Waiting %v before recovery attempt %d/%d", delay, attempt+1, len(retryDelays))
			time.Sleep(delay)
		}

		logrus.Warnf("🔄 Attempting to recover lost %s connection (%d/%d): %v", protocol, attempt+1, len(retryDelays), cause)
		mw.cleanupDeadConnectionState()

		fyne.Do(func() {
			mw.clearConnectionPending()
			mw.setConnectionLoading(true)
			mw.hostEntry.Disable()
			mw.tokenEntry.Disable()
			mw.protocolSelect.Disable()
		})

		if err := mw.doConnectWithProtocol(host, token, protocol); err == nil {
			return true
		} else {
			logrus.Warnf("⚠️ Recovery attempt %d/%d failed: %v", attempt+1, len(retryDelays), err)
		}
	}

	return false
}

func (mw *MainWindow) handleConnectionLost(err error, client *api.USBClient) {
	if mw.isClosing.Load() {
		mw.connectionLossInProgress.Store(false)
		return
	}
	if client != nil && client != mw.usbClient {
		mw.connectionLossInProgress.Store(false)
		return
	}

	if mw.tryRecoverConnectionAfterLoss(client, err) {
		logrus.Infof("✅ Connection recovered automatically after transport loss")
		mw.connectionLossInProgress.Store(false)
		return
	}

	logrus.Errorf("❌ Connection lost, tearing down local state: %v", err)
	mw.cleanupDeadConnectionState()

	fyne.Do(func() {
		mw.clearConnectionPending()
		mw.refreshConnectionControls()
		mw.hostEntry.Enable()
		mw.tokenEntry.Enable()
		mw.protocolSelect.Enable()
		mw.updateStatus()
		mw.showConnectionManager()
		view.ShowErrorDialog(fmt.Errorf(i18n.Current.ConnectionLost, err), mw.window)
	})

	mw.connectionLossInProgress.Store(false)
}

func (mw *MainWindow) stopWireGuardMonitor() {
	if mw.wireGuardMonitorStop == nil {
		return
	}

	close(mw.wireGuardMonitorStop)
	mw.wireGuardMonitorStop = nil
}

func (mw *MainWindow) startWireGuardMonitor(client *api.USBClient) {
	mw.stopWireGuardMonitor()
	if client == nil || mw.connectedProtocol != models.ConnectionProtocolWireGuard {
		return
	}

	stopCh := make(chan struct{})
	mw.wireGuardMonitorStop = stopCh

	go func(expectedClient *api.USBClient, stop <-chan struct{}) {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()

		consecutiveFailures := 0
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
			}

			if expectedClient != mw.usbClient || !mw.isConnected || mw.connectedProtocol != models.ConnectionProtocolWireGuard {
				return
			}

			if err := mw.verifyWireGuardTunnel(); err != nil {
				consecutiveFailures++
				logrus.Warnf("⚠️ [WireGuard] peer status degraded (%d/3): %v", consecutiveFailures, err)
				if consecutiveFailures < 3 {
					continue
				}
				if mw.connectionLossInProgress.CompareAndSwap(false, true) {
					logrus.Warnf("⚠️ Active WireGuard tunnel lost: %v", err)
					go mw.handleConnectionLost(err, expectedClient)
				}
				return
			}

			consecutiveFailures = 0
		}
	}(client, stopCh)
}

func (mw *MainWindow) verifyWireGuardTunnel() error {
	if mw.wgService == nil || !mw.wgService.IsRunning() {
		return fmt.Errorf("wireguard service is not running")
	}

	status, err := mw.wgService.GetPeerStatus()
	if err != nil {
		return err
	}
	if status == nil {
		return fmt.Errorf("wireguard peer status is nil")
	}
	if status.PeerCount == 0 {
		return fmt.Errorf("wireguard peer is missing")
	}

	maxHandshakeAge := 90 * time.Second
	if status.PersistentKeepalive > 0 {
		candidate := time.Duration(status.PersistentKeepalive*3)*time.Second + 15*time.Second
		if candidate > maxHandshakeAge {
			maxHandshakeAge = candidate
		}
	}

	if status.LastHandshakeAt.IsZero() {
		return fmt.Errorf("wireguard peer has no successful handshake yet")
	}

	handshakeAge := time.Since(status.LastHandshakeAt)
	if handshakeAge > maxHandshakeAge {
		return fmt.Errorf("wireguard handshake is stale: age=%v rx=%d tx=%d", handshakeAge.Truncate(time.Second), status.RxBytes, status.TxBytes)
	}

	return nil
}

// handleConnectionToggle переключает состояние подключения
func (mw *MainWindow) handleConnectionToggle() {
	if mw.isConnectionPending {
		logrus.Warn("⚠️ Операция подключения/отключения уже выполняется, игнорируем повторное нажатие")
		return
	}

	if mw.isConnected {
		mw.isConnectionPending = true
		mw.refreshConnectionControls()
		mw.enqueueLifecycleOp("disconnect", func() {
			mw.handleDisconnect()
		})
		return
	}

	if !mw.canAttemptConnection() {
		return
	}

	mw.isConnectionPending = true
	mw.setConnectionLoading(true)
	host := mw.hostEntry.Text
	token := mw.tokenEntry.Text
	mw.hostEntry.Disable()
	mw.tokenEntry.Disable()
	mw.protocolSelect.Disable()

	mw.enqueueLifecycleOp("connect", func() {
		if err := mw.doConnect(host, token); err != nil {
			mw.handleConnectFailure("Connection failed", err)
		}
	})
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
		mw.clearConnectionPending()
		mw.refreshConnectionControls()
		return
	}

	if token != "" {
		logrus.Infof("🔍 [DEBUG] Token is not empty, using the value from the input: '%s'", token)
	} else {
		logrus.Warn("🔍 [DEBUG] Token is empty after resolving input, saved connections and config fallback")
	}

	logrus.Infof("🔍 [DEBUG] Final connection parameters: host='%s', token='%s'", host, token)
	logrus.Infof("Establishing connection with protocol=%s...", mw.protocolSelect.Selected)

	mw.hostEntry.Disable()
	mw.tokenEntry.Disable()
	mw.protocolSelect.Disable()

	go func() {
		time.Sleep(100 * time.Millisecond)
		if err := mw.doConnect(host, token); err != nil {
			mw.handleConnectFailure("Connection failed", err)
		}
	}()
}

// doConnect выполняет блокирующую логику подключения (вызывается из горутины)
func (mw *MainWindow) doConnect(host, token string) error {
	protocol := mw.protocolSelect.Selected
	if protocol == "" {
		protocol = models.ConnectionProtocolAuto
	}
	mw.activeConnectionToken = strings.TrimSpace(token)
	return mw.doConnectWithProtocol(host, token, protocol)
}

func (mw *MainWindow) doConnectWithProtocol(host, token, protocol string) error {
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
		logrus.Debug("Waiting for tunnel stabilization...")
		time.Sleep(2 * time.Second)

		mw.hostEntry.Disable()
		mw.tokenEntry.Disable()

		httpPort, videoPort, _ := mw.frpService.GetServerPorts()
		mw.usbClient = mw.attachUSBClient(api.NewUSBClient("127.0.0.1", httpPort, mw.config.APITimeout))

		mw.gstreamerService.UpdateHost("127.0.0.1")
		mw.gstreamerService.UpdateVideoPort(videoPort)
		mw.gstreamerService.UpdateVideoUDPPort(videoPort)
		mw.config.VideoBindHost = "127.0.0.1"
		mw.videoWidget.SetFRPService(mw.frpService)
		mw.diskWidget.SetFRPService(mw.frpService)
		mw.connectedProtocol = models.ConnectionProtocolQUIC
		mw.config.NBDBindHost = "127.0.0.1"

		logrus.Debug("🔌 Ready to connect through FRP tunnel")
		return nil
	}

	connectWireGuard := func() error {
		if !mw.config.WireGuardEnabled {
			return fmt.Errorf("WireGuard disabled in config")
		}

		resetWireGuardAttempt := func() {
			if mw.wgService != nil && mw.wgService.IsRunning() {
				_ = mw.wgService.Disconnect()
			}
			mw.wgService = nil
			mw.usbClient = nil
			mw.connectedProtocol = ""
			mw.config.NBDBindHost = "127.0.0.1"
			mw.config.VideoBindHost = "127.0.0.1"
			mw.videoWidget.SetFRPService(nil)
			mw.diskWidget.SetFRPService(nil)
		}

		activateWireGuard := func(bootstrap *models.WireGuardBootstrapResponse, privateKey, source string) error {
			if bootstrap == nil {
				return fmt.Errorf("WireGuard bootstrap is nil")
			}

			wg := service.NewWireGuardService(mw.config)
			privateKey = strings.TrimSpace(privateKey)
			if privateKey != "" {
				if err := wg.SetPrivateKey(privateKey); err != nil {
					return fmt.Errorf("invalid saved WireGuard private key: %w", err)
				}
			}

			logrus.Infof("🔐 [WireGuard] bringing up local interface from %s registration", source)
			if err := wg.Connect(bootstrap); err != nil {
				return err
			}

			mw.wgService = wg
			mw.config.NBDBindHost = wg.GetClientHost()
			mw.usbClient = mw.attachUSBClient(api.NewUSBClient(wg.GetServerHost(), mw.config.USBPort, mw.config.APITimeout))
			mw.gstreamerService.UpdateHost(wg.GetServerHost())
			mw.gstreamerService.UpdateVideoPort(mw.config.VideoUDPPort)
			mw.gstreamerService.UpdateVideoUDPPort(mw.config.VideoUDPPort)
			mw.config.VideoBindHost = wg.GetClientHost()
			mw.videoWidget.SetFRPService(nil)
			mw.diskWidget.SetFRPService(nil)
			mw.connectedProtocol = models.ConnectionProtocolWireGuard
			logrus.Infof("✅ [WireGuard] interface up client=%s server=%s source=%s", wg.GetClientHost(), wg.GetServerHost(), source)
			return nil
		}

		if invite := strings.TrimSpace(mw.pendingWireGuardInvite); invite != "" {
			logrus.Info("🔐 [WireGuard] found pending invite, trying direct invite registration first")
			bootstrap, err := models.DecodeWireGuardInvite(invite)
			if err != nil {
				logrus.Warnf("⚠️ failed to decode pending WireGuard invite: %v", err)
			} else if err := activateWireGuard(bootstrap, bootstrap.ClientPrivateKey, "invite"); err != nil {
				logrus.Warnf("⚠️ WireGuard invite activation failed: %v", err)
				resetWireGuardAttempt()
			} else if err := mw.verifyActiveConnection(); err != nil {
				logrus.Warnf("⚠️ WireGuard invite registration is no longer valid, falling back to bootstrap: %v", err)
				resetWireGuardAttempt()
			} else {
				mw.storeWireGuardRegistration(host, mw.wgService.GetPrivateKey(), bootstrap)
				mw.pendingWireGuardInvite = ""
				return nil
			}
		}

		if registration, ok := mw.getWireGuardRegistration(host); ok {
			logrus.Infof("🔐 [WireGuard] trying saved registration for host=%s", host)
			if err := activateWireGuard(&registration.Bootstrap, registration.PrivateKey, "saved"); err != nil {
				logrus.Warnf("⚠️ saved WireGuard activation failed: %v", err)
				resetWireGuardAttempt()
				mw.deleteWireGuardRegistration(host)
			} else if err := mw.verifyActiveConnection(); err != nil {
				logrus.Warnf("⚠️ saved WireGuard registration is stale, re-registering: %v", err)
				resetWireGuardAttempt()
				mw.deleteWireGuardRegistration(host)
			} else {
				return nil
			}
		}

		if !mw.config.FRPEnabled {
			return fmt.Errorf("FRP bootstrap is required when no valid WireGuard registration exists")
		}

		logrus.Info("🔐 [WireGuard] STEP 1: starting FRP bootstrap transport")
		if err := connectQUIC(); err != nil {
			return fmt.Errorf("wireguard bootstrap over FRP failed at transport stage: %w", err)
		}
		if mw.frpService == nil || !mw.frpService.IsRunning() {
			return fmt.Errorf("FRP bootstrap transport is not running")
		}

		logrus.Info("🔐 [WireGuard] STEP 2: creating or reusing WireGuard client identity")
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
		if err := activateWireGuard(bootstrap, wg.GetPrivateKey(), "bootstrap"); err != nil {
			return err
		}
		mw.storeWireGuardRegistration(host, mw.wgService.GetPrivateKey(), bootstrap)
		mw.pendingWireGuardInvite = ""
		logrus.Info("🔐 [WireGuard] STEP 5: switching application traffic to WireGuard")
		logrus.Infof("   📥 HTTP API switched to WireGuard: %s:%d", mw.wgService.GetServerHost(), mw.config.USBPort)
		logrus.Infof("   📤 Local WireGuard client address: %s", mw.wgService.GetClientHost())
		return nil
	}

	connectTailscale := func() error {
		if !mw.config.TailscaleEnabled {
			return fmt.Errorf("Tailscale disabled in config")
		}
		if mw.tailscaleService == nil {
			mw.tailscaleService = service.NewTailscaleService()
		}

		status, err := mw.tailscaleService.Status(nil)
		if err != nil {
			return fmt.Errorf("tailscale is not ready: %w", err)
		}
		if !status.LoggedIn {
			return fmt.Errorf("tailscale is signed out, use Google login in Connection Manager first")
		}

		resolvedHost := strings.TrimSpace(host)
		if resolvedHost == "" {
			return fmt.Errorf("tailscale host is empty")
		}
		if err := mw.tailscaleService.ValidateAddress(resolvedHost); err != nil {
			return err
		}
		httpClient, err := mw.tailscaleService.HTTPClient()
		if err != nil {
			return err
		}

		mw.usbClient = mw.attachUSBClient(api.NewUSBClientWithHTTPClient(resolvedHost, mw.config.USBPort, mw.config.APITimeout, httpClient))
		mw.gstreamerService.UpdateHost(resolvedHost)
		mw.gstreamerService.UpdateVideoPort(mw.config.VideoUDPPort)
		mw.gstreamerService.UpdateVideoUDPPort(mw.config.VideoUDPPort)
		mw.config.NBDBindHost = "127.0.0.1"
		mw.config.VideoBindHost = "127.0.0.1"
		mw.videoWidget.SetFRPService(nil)
		mw.videoWidget.SetTailscaleService(mw.tailscaleService)
		mw.diskWidget.SetFRPService(nil)
		mw.connectedProtocol = models.ConnectionProtocolTailscale
		return nil
	}

	switch protocol {
	case models.ConnectionProtocolWireGuard:
		if err := connectWireGuard(); err != nil {
			return fmt.Errorf("failed to establish WireGuard tunnel: %w", err)
		}
	case models.ConnectionProtocolTailscale:
		if err := connectTailscale(); err != nil {
			return fmt.Errorf("failed to establish Tailscale connection: %w", err)
		}
	case models.ConnectionProtocolQUIC:
		if err := connectQUIC(); err != nil {
			return fmt.Errorf("failed to establish QUIC tunnel: %w", err)
		}
	case models.ConnectionProtocolAuto:
		if err := connectTailscale(); err != nil {
			logrus.Warnf("⚠️ Tailscale auto-connect failed, trying WireGuard: %v", err)
			if err := connectWireGuard(); err != nil {
				logrus.Warnf("⚠️ WireGuard auto-connect failed, falling back to QUIC: %v", err)
				if err := connectQUIC(); err != nil {
					return fmt.Errorf("failed to establish connection in auto mode: %w", err)
				}
			}
		}
	default:
		logrus.Info("Testing connection...")
		tempClient := api.NewUSBClient(host, mw.config.USBPort, mw.config.APITimeout)
		if err := tempClient.TestConnection(); err != nil {
			return fmt.Errorf("connection failed: %w", err)
		}

		mw.usbClient = mw.attachUSBClient(tempClient)
		mw.gstreamerService.UpdateHost(host)
		mw.config.VideoBindHost = "127.0.0.1"
		mw.diskWidget.SetFRPService(nil)
		mw.videoWidget.SetTailscaleService(nil)
		mw.connectedProtocol = "direct"

		if err := tempClient.TestConnection(); err != nil {
			return fmt.Errorf("connection failed: %w", err)
		}
	}

	logrus.Info("Verifying connection...")
	if err := mw.verifyActiveConnection(); err != nil {
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
			mw.clearConnectionPending()
			mw.isConnected = false
			mw.connectedProtocol = ""
			mw.refreshConnectionControls()
			mw.hostEntry.Enable()
			mw.tokenEntry.Enable()
			mw.protocolSelect.Enable()
		})
		return fmt.Errorf("connection verification failed: %w", err)
	}

	logrus.Debug("Loading devices...")
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
	mw.connectionLossInProgress.Store(false)
	if mw.connectedProtocol == models.ConnectionProtocolWireGuard {
		mw.startWireGuardMonitor(mw.usbClient)
	}

	fyne.Do(func() {
		mw.clearConnectionPending()
		mw.refreshConnectionControls()
		if mw.pcpanelWidget != nil {
			mw.pcpanelWidget.SetClient(mw.usbClient)
		}
		mw.updateStatus()
		mw.showMainContent()
	})

	logrus.Infof("✅ Connected to USBridge via %s", mw.connectedProtocol)
	return nil
}

func (mw *MainWindow) verifyActiveConnection() error {
	if mw.usbClient == nil {
		return fmt.Errorf("usb client is not initialized")
	}

	if mw.connectedProtocol != models.ConnectionProtocolWireGuard {
		return mw.usbClient.TestConnection()
	}

	var lastErr error
	for attempt := 1; attempt <= 12; attempt++ {
		if attempt > 1 {
			time.Sleep(1 * time.Second)
		}

		err := mw.verifyWireGuardTunnel()
		if err == nil {
			if attempt > 1 {
				logrus.Infof("✅ [WireGuard] connection verified on retry %d/12", attempt)
			}
			return nil
		}

		lastErr = err
		logrus.Warnf("⚠️ [WireGuard] verification attempt %d/12 failed: %v", attempt, err)
	}

	return lastErr
}

func (mw *MainWindow) handleConnectFailure(message string, err error) {
	logrus.Errorf("%s: %v", message, err)
	fyne.Do(func() {
		mw.clearConnectionPending()
		mw.isConnected = false
		mw.connectedProtocol = ""
		mw.activeConnectionToken = ""
		mw.refreshConnectionControls()
		mw.hostEntry.Enable()
		mw.tokenEntry.Enable()
		mw.protocolSelect.Enable()
		view.ShowErrorDialog(fmt.Errorf("%s: %w", message, err), mw.window)
		if !mw.isClosing.Load() {
			view.ShowErrorDialog(fmt.Errorf("%s: %w", message, err), mw.window)
		}
	})
}

// handleDisconnect обрабатывает отключение
func (mw *MainWindow) handleDisconnect() {
	logrus.Infof("[shutdown] handleDisconnect: start connected=%v wg_running=%v frp_running=%v", mw.isConnected, mw.wgService != nil && mw.wgService.IsRunning(), mw.frpService != nil && mw.frpService.IsRunning())
	mw.connectionLossInProgress.Store(false)
	mw.stopWireGuardMonitor()

	if mw.videoWidget != nil && mw.videoWidget.IsStreaming() {
		logrus.Info("🛑 Stopping video before disconnect...")
		if err := mw.videoWidget.StopVideoSync(); err != nil {
			logrus.Errorf("Failed to stop video before disconnect: %v", err)
		}
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

	if mw.frpService != nil {
		if mw.frpService.IsRunning() {
			logrus.Info("[shutdown] handleDisconnect: stopping FRP service")
			if err := mw.frpService.Disconnect(); err != nil {
				logrus.Errorf("Failed to stop FRP tunnel: %v", err)
			}
			logrus.Info("[shutdown] handleDisconnect: FRP service stopped")
		}
		mw.frpService = nil
	}
	if mw.wgService != nil {
		if mw.wgService.IsRunning() {
			logrus.Info("[shutdown] handleDisconnect: stopping WireGuard service")
			if err := mw.wgService.Disconnect(); err != nil {
				logrus.Errorf("Failed to stop WireGuard tunnel: %v", err)
			}
			logrus.Info("[shutdown] handleDisconnect: WireGuard service stopped")
		}
		mw.wgService = nil
	}

	mw.isConnected = false
	mw.isStreaming = false
	mw.connectedProtocol = ""
	mw.activeConnectionToken = ""
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

	mw.clearConnectionPending()
	mw.refreshConnectionControls()

	if mw.pcpanelWidget != nil {
		mw.pcpanelWidget.SetClient(nil)
	}

	mw.updateStatus()
	mw.config.NBDBindHost = "127.0.0.1"
	mw.config.VideoBindHost = "127.0.0.1"
	if !mw.isClosing.Load() {
		mw.hostEntry.Enable()
		mw.tokenEntry.Enable()
		mw.protocolSelect.Enable()
		mw.showConnectionManager()
	}

	logrus.Info("[shutdown] handleDisconnect: completed")
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
