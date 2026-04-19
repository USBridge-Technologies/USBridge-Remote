package gui

import (
	"context"
	"fmt"
	"net"
	"net/url"
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

func (mw *MainWindow) handleSelectionFromManager(wireGuardInvite string, tailscaleRegister bool) {
	mw.pendingWireGuardInvite = wireGuardInvite
	mw.pendingTailscaleRegister = tailscaleRegister
}

// handleConnectionFromManager обрабатывает подключение из менеджера (стрелка на карточке).
// Заполняет поля и вызывает единый обработчик handleConnectionToggle для защиты от множественных нажатий.
func (mw *MainWindow) handleConnectionFromManager(host, token, protocol, wireGuardInvite string, tailscaleRegister bool) {
	mw.hostEntry.SetText(host)
	mw.tokenEntry.SetText(token)
	mw.pendingWireGuardInvite = wireGuardInvite
	mw.pendingTailscaleRegister = tailscaleRegister
	if protocol != "" {
		mw.protocolSelect.SetSelected(protocol)
	}
	mw.handleConnectionToggle()
}

// handleSaveFromDeepLink сохраняет данные из deep link БЕЗ подключения
func (mw *MainWindow) handleSaveFromDeepLink(name, internalHost, tailscaleHost, token, protocol, wireGuardInvite string, tailscaleRegister bool) {
	host := strings.TrimSpace(tailscaleHost)
	if host == "" {
		host = strings.TrimSpace(internalHost)
	}
	logrus.Infof("💾 handleSaveFromDeepLink: name='%s' internal='%s' tailscale='%s' token='%s' protocol='%s' register=%v", name, internalHost, tailscaleHost, token, protocol, tailscaleRegister)

	fyne.Do(func() {
		mw.hostEntry.SetText(host)
		mw.tokenEntry.SetText(token)
		if protocol != "" {
			mw.protocolSelect.SetSelected(protocol)
		}
	})

	if mw.connectionManager != nil {
		mw.pendingWireGuardInvite = wireGuardInvite
		generatedName := mw.connectionManager.SaveConnection(name, internalHost, tailscaleHost, token, protocol, wireGuardInvite, tailscaleRegister)
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
	mw.pendingTailscaleRegister = false
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

func (mw *MainWindow) resolveBootstrapHost(host string) string {
	host = strings.TrimSpace(host)
	if host == "" {
		return ""
	}
	if mw.connectionManager != nil {
		if resolved := strings.TrimSpace(mw.connectionManager.ResolveInternalHost(host)); resolved != "" {
			return resolved
		}
	}
	if !strings.HasSuffix(strings.ToLower(host), ".ts.net") {
		return host
	}
	return ""
}

func isLikelyTailscaleAuthKey(token string) bool {
	token = strings.ToLower(strings.TrimSpace(token))
	return strings.HasPrefix(token, "tskey-")
}

func isLikelyTailscaleHost(host string) bool {
	host = strings.TrimSpace(strings.ToLower(host))
	return host != "" && (strings.HasSuffix(host, ".ts.net") || strings.HasPrefix(host, "100."))
}

func splitBridgeAuthInputs(raw string) (deviceToken, tailscaleAuthKey string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ""
	}

	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == '|' || r == ',' || r == ';' || r == '\n'
	})
	if len(parts) == 0 {
		return "", ""
	}

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if isLikelyTailscaleAuthKey(part) {
			tailscaleAuthKey = part
			continue
		}
		if deviceToken == "" {
			deviceToken = part
		}
	}

	if tailscaleAuthKey == "" && isLikelyTailscaleAuthKey(raw) {
		tailscaleAuthKey = raw
		deviceToken = ""
	}
	if deviceToken == "" && tailscaleAuthKey == "" {
		deviceToken = raw
	}
	return strings.TrimSpace(deviceToken), strings.TrimSpace(tailscaleAuthKey)
}

func fallbackText(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func maskSensitiveToken(token string) string {
	token = strings.TrimSpace(token)
	if token == "" {
		return "<empty>"
	}
	if len(token) <= 8 {
		return token[:2] + "..." + token[len(token)-2:]
	}
	return token[:4] + "..." + token[len(token)-4:]
}

func (mw *MainWindow) resolveBridgeAuthInputs(host, rawToken string) (deviceToken, tailscaleAuthKey string) {
	deviceToken, tailscaleAuthKey = splitBridgeAuthInputs(rawToken)
	if strings.TrimSpace(deviceToken) == "" {
		deviceToken = mw.resolveConnectionToken(host, "")
	}
	return strings.TrimSpace(deviceToken), strings.TrimSpace(tailscaleAuthKey)
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

	mw.config.NBDBindHost = "0.0.0.0"
	mw.config.VideoBindHost = "0.0.0.0"
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

		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(mw.config.APITimeout)*time.Second)
		err := mw.doConnectWithProtocol(ctx, host, token, protocol)
		cancel()
		if err == nil {
			return true
		}
		logrus.Warnf("⚠️ Recovery attempt %d/%d failed: %v", attempt+1, len(retryDelays), err)
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
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(mw.config.APITimeout)*time.Second)
		defer cancel()

		if err := mw.doConnect(ctx, host, token); err != nil {
			mw.handleConnectFailure("Connection failed", err)
		}
	})
}

// handleConnect обрабатывает подключение
func (mw *MainWindow) handleConnect() {
	logrus.Infof("🔍 [DEBUG] handleConnect() called")

	host := mw.hostEntry.Text
	token := mw.tokenEntry.Text

	if host == "" {
		logrus.Warn("Enter a server address")
		mw.clearConnectionPending()
		mw.refreshConnectionControls()
		return
	}

	mw.hostEntry.Disable()
	mw.tokenEntry.Disable()
	mw.protocolSelect.Disable()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(mw.config.APITimeout)*time.Second)
		defer cancel()

		if err := mw.doConnect(ctx, host, token); err != nil {
			mw.handleConnectFailure("Connection failed", err)
		}
	}()
}

// getFreeVideoUDPPort finds an available UDP port dynamically
func getFreeVideoUDPPort() int {
	addr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	if err != nil {
		return models.DefaultVideoUDPPort
	}
	l, err := net.ListenUDP("udp", addr)
	if err != nil {
		return models.DefaultVideoUDPPort
	}
	port := l.LocalAddr().(*net.UDPAddr).Port
	l.Close()
	return port
}

// doConnect выполняет блокирующую логику подключения (вызывается из горутины)
func (mw *MainWindow) doConnect(ctx context.Context, host, token string) error {
	// Принудительно останавливаем видео и сбрасываем GStreamer перед новым подключением
	if mw.videoWidget != nil {
		_ = mw.videoWidget.StopVideoSync()
	}
	if mw.gstreamerService != nil {
		_ = mw.gstreamerService.Disconnect()
	}

	// Генерируем новый UDP порт для видео, чтобы избежать коллизий пакетов с предыдущими хостами
	mw.config.VideoUDPPort = getFreeVideoUDPPort()
	if mw.gstreamerService != nil {
		mw.gstreamerService.UpdateVideoPort(mw.config.VideoUDPPort)
		mw.gstreamerService.UpdateVideoUDPPort(mw.config.VideoUDPPort)
	}

	protocol := mw.protocolSelect.Selected
	if protocol == "" {
		protocol = models.ConnectionProtocolAuto
	}
	mw.activeConnectionToken = strings.TrimSpace(token)
	logrus.Infof("🔗 [CONNECT] start host=%s protocol=%s raw_token=%s timeout=%ds",
		strings.TrimSpace(host), protocol, maskSensitiveToken(token), mw.config.APITimeout)
	return mw.doConnectWithProtocol(ctx, host, token, protocol)
}

func (mw *MainWindow) doConnectWithProtocol(ctx context.Context, host, token, protocol string) error {
	connectQUICTo := func(ctx context.Context, quicHost, quicToken string) error {
		if !mw.config.FRPEnabled {
			return fmt.Errorf("FRP disabled in config")
		}
		logrus.Infof("🚇 [QUIC] creating FRP service host=%s port=%d token=%s", quicHost, mw.config.FRPServerPort, maskSensitiveToken(quicToken))
		mw.frpService = service.NewFRPService(
			quicHost,
			mw.config.FRPServerPort,
			quicToken,
		)

		if err := mw.frpService.Connect(mw.config.USBPort, mw.config.NBDPort, mw.config.VideoUDPPort); err != nil {
			return err
		}

		logrus.Info("✅ [QUIC] tunnel established via FRP")
		
		// Wait for stabilization or context cancel
		select {
		case <-time.After(2 * time.Second):
		case <-ctx.Done():
			return ctx.Err()
		}

		mw.hostEntry.Disable()
		mw.tokenEntry.Disable()

		httpPort, videoPort, _ := mw.frpService.GetServerPorts()
		mw.usbClient = mw.attachUSBClient(api.NewUSBClient("127.0.0.1", httpPort, mw.config.APITimeout))

		mw.gstreamerService.UpdateHost("127.0.0.1")
		mw.gstreamerService.UpdateVideoPort(videoPort)
		mw.gstreamerService.UpdateVideoUDPPort(videoPort)
		mw.config.VideoBindHost = "0.0.0.0"
		mw.videoWidget.SetFRPService(mw.frpService)
		mw.diskWidget.SetFRPService(mw.frpService)
		mw.connectedProtocol = models.ConnectionProtocolQUIC
		mw.config.NBDBindHost = "0.0.0.0"

		return nil
	}

	connectQUIC := func(ctx context.Context) error {
		if err := connectQUICTo(ctx, host, token); err != nil {
			return err
		}

		if mw.pendingTailscaleRegister {
			quicToken, tailscaleAuthKey := mw.resolveBridgeAuthInputs(host, token)
			httpPort, _, _ := mw.frpService.GetServerPorts()
			bootstrapClient := api.NewUSBClient("127.0.0.1", httpPort, mw.config.APITimeout)
			tsStatus, err := bootstrapClient.GetTailscaleStatusWithContext(ctx)
			needsRegister := err != nil || tsStatus == nil || !tsStatus.LoggedIn
			if needsRegister && tsStatus != nil && strings.TrimSpace(tsStatus.AuthURL) != "" {
				// Status already has auth_url — skip register, go straight to open URL
				needsRegister = false
			}
			if needsRegister {
				regStatus, regErr := bootstrapClient.RegisterTailscaleWithContext(ctx, &models.TailscaleRegistrationRequest{
					DeviceToken: quicToken,
					AuthKey:     tailscaleAuthKey,
					Hostname:    "usbridge",
				})
				if regErr != nil {
					if api.IsHTTPNotFound(regErr) {
						// Register endpoint disabled (TailscaleRegistrationEnabled=false) — re-fetch status
						logrus.Warnf("⚠️ Tailscale register endpoint disabled (404), checking current status...")
						if fresh, statusErr := bootstrapClient.GetTailscaleStatusWithContext(ctx); statusErr == nil && fresh != nil {
							tsStatus = fresh
						} else {
							logrus.Warnf("⚠️ Tailscale registration requested but API failed: %v", regErr)
						}
					} else {
						logrus.Warnf("⚠️ Tailscale registration requested but API failed: %v", regErr)
					}
				} else {
					tsStatus = regStatus
				}
			}
			if tsStatus != nil && !tsStatus.LoggedIn {
				// We don't block QUIC connection if Tailscale registration fails or is interactive,
				// but we try to open the auth URL.
				if strings.TrimSpace(tsStatus.AuthURL) != "" {
					if parsedURL, parseErr := url.Parse(tsStatus.AuthURL); parseErr == nil {
						_ = mw.app.OpenURL(parsedURL)
					}
				}
			}
			if tsStatus != nil && (strings.TrimSpace(tsStatus.DNSName) != "" || strings.TrimSpace(tsStatus.IP4) != "") {
				resolvedHost := fallbackText(tsStatus.DNSName, tsStatus.IP4)
				if mw.connectionManager != nil {
					mw.connectionManager.RememberResolvedTailscaleHost(strings.TrimSpace(host), host, resolvedHost, quicToken)
				}
			}
		}
		return nil
	}

	connectTailscale := func(ctx context.Context) error {
		if !mw.config.TailscaleEnabled {
			return fmt.Errorf("Tailscale disabled in config")
		}
		if mw.tailscaleService == nil {
			mw.tailscaleService = service.NewTailscaleService(mw.config.TailscaleUserspace)
		}

		status, err := mw.tailscaleService.Status(ctx)
		if err != nil {
			return fmt.Errorf("tailscale is not ready: %w", err)
		}
		if !status.LoggedIn {
			return fmt.Errorf("tailscale is signed out, use Google login in Connection Manager first")
		}

		resolvedHost := strings.TrimSpace(host)
		bootstrapHost := mw.resolveBootstrapHost(host)
		quicToken, tailscaleAuthKey := mw.resolveBridgeAuthInputs(host, token)

		tryDirect := func(ctx context.Context, target string) error {
			if target == "" {
				return fmt.Errorf("target host is empty")
			}
			if !isLikelyTailscaleHost(target) {
				return fmt.Errorf("not a tailscale host")
			}
			if err := mw.tailscaleService.ValidateAddress(target); err != nil {
				return err
			}
			httpClient, err := mw.tailscaleService.HTTPClient()
			if err != nil {
				return err
			}

			tsClient := api.NewUSBClientWithHTTPClient(target, mw.config.USBPort, mw.config.APITimeout, httpClient)
			
			// На Android userspace-Tailscale (tsnet) первый запрос может провалиться,
			// пока tsnet не установил маршрут до пира.
			var statusErr error
			const maxStatusAttempts = 6
			for attempt := 1; attempt <= maxStatusAttempts; attempt++ {
				// Check context before each attempt
				if err := ctx.Err(); err != nil {
					return err
				}

				_, statusErr = tsClient.GetTailscaleStatusWithContext(ctx)
				if statusErr == nil {
					break
				}
				if api.IsHTTPNotFound(statusErr) {
					break // 404 = агент без bridge-регистрации, не retry-able
				}
				
				if attempt < maxStatusAttempts {
					pause := time.Duration(attempt*attempt) * time.Second
					if pause > 10*time.Second {
						pause = 10 * time.Second
					}
					select {
					case <-time.After(pause):
					case <-ctx.Done():
						return ctx.Err()
					}
				}
			}

			if statusErr != nil {
				if api.IsHTTPNotFound(statusErr) {
					mw.usbClient = mw.attachUSBClient(tsClient)
					mw.gstreamerService.UpdateHost(target)
					mw.connectedProtocol = models.ConnectionProtocolTailscale
					return nil
				}
				return fmt.Errorf("direct tailscale connect succeeded but bridge status check failed: %w", statusErr)
			}

			if mw.frpService != nil && mw.frpService.IsRunning() {
				_ = mw.frpService.Disconnect()
			}
			mw.frpService = nil
			mw.usbClient = mw.attachUSBClient(tsClient)
			mw.gstreamerService.UpdateHost(target)
			mw.connectedProtocol = models.ConnectionProtocolTailscale
			mw.videoWidget.SetTailscaleService(mw.tailscaleService)
			return nil
		}

		waitForBridgeInteractiveLogin := func(ctx context.Context, bootstrapClient *api.USBClient, initialStatus *models.TailscaleStatus) (*models.TailscaleStatus, error) {
			if initialStatus == nil || strings.TrimSpace(initialStatus.AuthURL) == "" {
				return initialStatus, fmt.Errorf("bridge did not return an auth URL")
			}

			authURL := strings.TrimSpace(initialStatus.AuthURL)
			if parsedURL, parseErr := url.Parse(authURL); parseErr == nil {
				_ = mw.app.OpenURL(parsedURL)
			}

			lastStatus := initialStatus
			for {
				select {
				case <-ctx.Done():
					return lastStatus, ctx.Err()
				case <-time.After(2 * time.Second):
					status, statusErr := bootstrapClient.GetTailscaleStatusWithContext(ctx)
					if statusErr != nil {
						continue
					}
					lastStatus = status
					if status.LoggedIn {
						return status, nil
					}
				}
			}
		}

		if resolvedHost != "" && isLikelyTailscaleHost(resolvedHost) {
			if err := tryDirect(ctx, resolvedHost); err == nil {
				return nil
			} else if protocol == models.ConnectionProtocolTailscale && !mw.pendingTailscaleRegister {
				// Если пользователь явно выбрал Tailscale и регистрация не заказана - возвращаем ошибку
				return err
			}
		}

		// Если мы здесь, значит прямое подключение по Tailscale не удалось (или хост не похож на Tailscale).
		// Мы пробуем "бутстрап" через QUIC в следующих случаях:
		// 1. Протокол Auto
		// 2. Протокол Tailscale И заказана регистрация (mw.pendingTailscaleRegister)
		// 3. Протокол Tailscale И хост пустой (подразумеваем что надо зарегистрировать)
		
		canBootstrap := protocol == models.ConnectionProtocolAuto || 
			(protocol == models.ConnectionProtocolTailscale && (mw.pendingTailscaleRegister || resolvedHost == ""))

		if !canBootstrap {
			if protocol == models.ConnectionProtocolTailscale && resolvedHost != "" {
				return fmt.Errorf("tailscale connection failed and bootstrap not requested")
			}
			return fmt.Errorf("tailscale host is empty and bootstrap not allowed for protocol %s", protocol)
		}

		if bootstrapHost == "" {
			return fmt.Errorf("tailscale host is empty and no QUIC bootstrap host is available")
		}

		if err := connectQUICTo(ctx, bootstrapHost, quicToken); err != nil {
			return fmt.Errorf("tailscale bootstrap over QUIC failed: %w", err)
		}

		httpPort, _, _ := mw.frpService.GetServerPorts()
		bootstrapClient := api.NewUSBClient("127.0.0.1", httpPort, mw.config.APITimeout)
		tsStatus, err := bootstrapClient.GetTailscaleStatusWithContext(ctx)
		needsRegister := err != nil || tsStatus == nil || !tsStatus.LoggedIn
		if needsRegister && tsStatus != nil && strings.TrimSpace(tsStatus.AuthURL) != "" {
			// Status already has auth_url — skip register, go straight to interactive login
			needsRegister = false
		}
		if needsRegister {
			regStatus, regErr := bootstrapClient.RegisterTailscaleWithContext(ctx, &models.TailscaleRegistrationRequest{
				DeviceToken: quicToken,
				AuthKey:     tailscaleAuthKey,
				Hostname:    "usbridge",
			})
			if regErr != nil {
				if api.IsHTTPNotFound(regErr) {
					// Register endpoint disabled (TailscaleRegistrationEnabled=false) — re-fetch status
					logrus.Warnf("⚠️ Tailscale register endpoint disabled (404), re-checking status...")
					if fresh, statusErr := bootstrapClient.GetTailscaleStatusWithContext(ctx); statusErr == nil && fresh != nil {
						tsStatus = fresh
					} else if tsStatus == nil || (!tsStatus.LoggedIn && strings.TrimSpace(tsStatus.AuthURL) == "") {
						return fmt.Errorf("tailscale registration is disabled on device and device is not logged in")
					}
				} else {
					return fmt.Errorf("tailscale registration API failed: %w", regErr)
				}
			} else {
				tsStatus = regStatus
			}
		}
		if tsStatus != nil && !tsStatus.LoggedIn {
			tsStatus, err = waitForBridgeInteractiveLogin(ctx, bootstrapClient, tsStatus)
			if err != nil {
				return fmt.Errorf("tailscale interactive registration failed: %w", err)
			}
		}

		resolvedHost = strings.TrimSpace(tsStatus.DNSName)
		if resolvedHost == "" {
			resolvedHost = strings.TrimSpace(tsStatus.IP4)
		}
		if resolvedHost == "" {
			return fmt.Errorf("bridge registered in tailscale but no address found")
		}

		if mw.connectionManager != nil {
			mw.connectionManager.RememberResolvedTailscaleHost(strings.TrimSpace(host), bootstrapHost, resolvedHost, quicToken)
		}

		return tryDirect(ctx, resolvedHost)
	}

	switch protocol {
	case models.ConnectionProtocolTailscale:
		if err := connectTailscale(ctx); err != nil {
			return err
		}
	case models.ConnectionProtocolQUIC:
		if err := connectQUIC(ctx); err != nil {
			return err
		}
	case models.ConnectionProtocolAuto:
		if err := connectTailscale(ctx); err != nil {
			logrus.Warnf("⚠️ Tailscale auto-connect failed, falling back to QUIC: %v", err)
			if err := connectQUIC(ctx); err != nil {
				return fmt.Errorf("failed to establish connection in auto mode: %w", err)
			}
		}
	default:
		tempClient := api.NewUSBClient(host, mw.config.USBPort, mw.config.APITimeout)
		if err := tempClient.TestConnectionWithContext(ctx); err != nil {
			return err
		}
		mw.usbClient = mw.attachUSBClient(tempClient)
		mw.gstreamerService.UpdateHost(host)
		mw.connectedProtocol = "direct"
	}

	if err := mw.verifyActiveConnectionWithContext(ctx); err != nil {
		logrus.Errorf("❌ Connection verification failed: %v", err)
		if mw.frpService != nil && mw.frpService.IsRunning() {
			_ = mw.frpService.Disconnect()
			mw.frpService = nil
		}
		if mw.wgService != nil && mw.wgService.IsRunning() {
			_ = mw.wgService.Disconnect()
			mw.wgService = nil
			mw.config.NBDBindHost = "0.0.0.0"
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

func (mw *MainWindow) verifyActiveConnectionWithContext(ctx context.Context) error {
	if mw.usbClient == nil {
		return fmt.Errorf("usb client is not initialized")
	}

	if mw.connectedProtocol != models.ConnectionProtocolWireGuard {
		return mw.usbClient.TestConnectionWithContext(ctx)
	}

	var lastErr error
	for attempt := 1; attempt <= 12; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}

		if attempt > 1 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(1 * time.Second):
			}
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

func (mw *MainWindow) verifyActiveConnection() error {
	return mw.verifyActiveConnectionWithContext(context.Background())
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
		if !mw.isClosing.Load() {
			view.ShowErrorDialog(fmt.Errorf("%s: %w", message, err), mw.window)
		}
	})
}

// handleDisconnect обрабатывает отключение
func (mw *MainWindow) handleDisconnect() {
	logrus.Infof("[shutdown] handleDisconnect: start connected=%v wg_running=%v frp_running=%v", mw.isConnected, mw.wgService != nil && mw.wgService.IsRunning(), mw.frpService != nil && mw.frpService.IsRunning())
	
	// Сразу сбрасываем состояние, чтобы прервать фоновые Refresh циклы
	mw.isConnected = false
	mw.isStreaming = false
	mw.connectionLossInProgress.Store(false)
	
	if mw.videoWidget != nil {
		logrus.Info("🛑 Stopping video before disconnect...")
		_ = mw.videoWidget.StopVideoSync()
		mw.videoWidget.Close() // Останавливаем фоновые опросы после завершения стопа
	}

	if mw.backupWidget != nil {
		mw.backupWidget.Close() // Останавливаем фоновые опросы
	}

	mw.stopWireGuardMonitor()

	// Выполняем сетевое завершение в фоне с таймаутом, чтобы не вешать UI
	if mw.usbClient != nil {
		client := mw.usbClient
		go func() {
			defer func() {
				if r := recover(); r != nil {
					logrus.Errorf("🔥 PANIC in remote resource cleanup: %v", r)
				}
			}()
			logrus.Info("[shutdown] Remote resource cleanup...")
			_ = client.StopAllDevices()
			client.Disconnect()
		}()
	}

	if mw.nbdServer.IsRunning() {
		logrus.Info("[shutdown] stopping NBD server")
		if err := mw.nbdServer.Stop(); err != nil {
			logrus.Errorf("Failed to stop NBD server: %v", err)
		}
	}

	if mw.frpService != nil {
		if mw.frpService.IsRunning() {
			logrus.Info("[shutdown] stopping FRP service")
			_ = mw.frpService.Disconnect()
		}
		mw.frpService = nil
	}
	if mw.wgService != nil {
		if mw.wgService.IsRunning() {
			logrus.Info("[shutdown] stopping WireGuard service")
			_ = mw.wgService.Disconnect()
		}
		mw.wgService = nil
	}

	mw.connectedProtocol = ""
	mw.activeConnectionToken = ""
	mw.appState.IsConnected = false
	mw.appState.IsStreaming = false
	mw.appState.IsNBDRunning = false
	mw.appState.LastDisconnected = time.Now()

	// Все операции с UI переносим в поток Fyne
	fyne.Do(func() {
		if mw.diskWidget != nil {
			mw.diskWidget.UpdateClient(nil)
			mw.diskWidget.SetFRPService(nil)
		}
		if mw.videoWidget != nil {
			mw.videoWidget.UpdateClient(nil)
			mw.videoWidget.SetFRPService(nil)
		}
		if mw.backupWidget != nil {
			mw.backupWidget.UpdateClient(nil)
		}

		mw.usbClient = nil

		mw.clearConnectionPending()
		mw.refreshConnectionControls()

		if mw.pcpanelWidget != nil {
			mw.pcpanelWidget.SetClient(nil)
		}

		mw.updateStatus()
		mw.config.NBDBindHost = "0.0.0.0"
		mw.config.VideoBindHost = "0.0.0.0"

		if !mw.isClosing.Load() {
			mw.hostEntry.Enable()
			mw.tokenEntry.Enable()
			mw.protocolSelect.Enable()
			mw.showConnectionManager()
		}
	})
	logrus.Infof("[shutdown] handleDisconnect: completed")
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
