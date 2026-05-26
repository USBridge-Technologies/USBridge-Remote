package gui

import (
	"context"
	"fmt"
	"image/color"
	"net"
	"net/url"
	"strings"
	"time"

	"usbridge-client/internal/api"
	"usbridge-client/internal/gui/assets"
	"usbridge-client/internal/gui/design"
	"usbridge-client/internal/gui/i18n"
	"usbridge-client/internal/gui/view"
	"usbridge-client/internal/models"
	"usbridge-client/internal/service"

	"fyne.io/fyne/v2"
	"github.com/sirupsen/logrus"
)

func (mw *MainWindow) handleSelectionFromManager(tailscaleRegister bool) {
	mw.pendingTailscaleRegister = tailscaleRegister
}

// handleConnectionFromManager handles connection from the manager (arrow on the card).
// It fills the fields and calls the unified handleConnectionToggle handler to protect against multiple clicks.
func (mw *MainWindow) handleConnectionFromManager(host, quicToken, protocol string, quicPort int, tailscaleRegister bool) {
	mw.hostEntry.SetText(host)
	mw.tokenEntry.SetText(quicToken)
	mw.pendingTailscaleRegister = tailscaleRegister
	mw.pendingQUICPort = quicPort
	if protocol != "" {
		mw.protocolSelect.SetSelected(protocol)
	}
	mw.handleConnectionToggle()
}

// handleSaveFromDeepLink saves data from a deep link WITHOUT connecting.
func (mw *MainWindow) handleSaveFromDeepLink(name, internalHost, tailscaleHost, quicToken, protocol string, quicPort int, tailscaleRegister bool) {
	host := strings.TrimSpace(tailscaleHost)
	if host == "" {
		host = strings.TrimSpace(internalHost)
	}
	logrus.Infof("💾 handleSaveFromDeepLink: name='%s' internal='%s' tailscale='%s' quicPort=%d quicToken='%s' protocol='%s' register=%v", name, internalHost, tailscaleHost, quicPort, maskSensitiveToken(quicToken), protocol, tailscaleRegister)

	fyne.Do(func() {
		mw.hostEntry.SetText(host)
		mw.tokenEntry.SetText(quicToken)
		if protocol != "" {
			mw.protocolSelect.SetSelected(protocol)
		}
	})

	if mw.connectionManager != nil {
		generatedName := mw.connectionManager.SaveConnection(name, internalHost, tailscaleHost, quicToken, protocol, quicPort, tailscaleRegister)
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
	mw.isConnectionPending.Store(false)
	mw.isConnectionLoading = false
	mw.pendingTailscaleRegister = false
	if mw.connectionManager != nil {
		mw.connectionManager.SetConnectionPending(false)
	}
}

func (mw *MainWindow) resolveConnectionToken(host, quicToken string) string {
	resolved := strings.TrimSpace(quicToken)
	if resolved != "" {
		return resolved
	}

	if mw.connectionManager != nil {
		resolved = mw.connectionManager.ResolveQUICToken(host, quicToken)
		if resolved != "" {
			logrus.Infof("🔍 [DEBUG] Resolved QUIC token from saved connection for host='%s'", host)
			return resolved
		}
	}

	resolved = strings.TrimSpace(mw.activeQUICToken)
	if resolved != "" {
		logrus.Infof("🔍 [DEBUG] Reusing active session QUIC token for host='%s'", host)
		return resolved
	}

	return ""
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

func (mw *MainWindow) tryRecoverConnectionAfterLoss(client *api.USBClient, lastErr error) bool {
	if client == nil || client != mw.usbClient || !mw.isConnected {
		return false
	}

	protocol := mw.connectedProtocol
	if protocol == "" {
		protocol = models.ConnectionProtocolAuto
	}

	logrus.Infof("🔄 Attempting automatic connection recovery for host=%s protocol=%s", mw.hostEntry.Text, protocol)

	retryDelays := []time.Duration{1 * time.Second, 2 * time.Second, 5 * time.Second}
	for attempt, delay := range retryDelays {
		if !mw.isConnected || client != mw.usbClient || mw.isClosing.Load() {
			return false
		}

		select {
		case <-time.After(delay):
		}

		fyne.Do(func() {
			mw.isConnectionPending.Store(true)
			mw.isConnectionLoading = true
			mw.refreshConnectionControls()
			mw.hostEntry.Disable()
			mw.tokenEntry.Disable()
			mw.protocolSelect.Disable()
		})

		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(mw.config.APITimeout)*time.Second)
		err := mw.doConnectWithProtocol(ctx, mw.hostEntry.Text, mw.tokenEntry.Text, protocol)
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

func (mw *MainWindow) cleanupDeadConnectionState() {
	mw.isConnected = false
	mw.isStreaming = false

	if mw.videoWidget != nil {
		mw.videoWidget.HandleConnectionLost()
	}

	if mw.frpService != nil {
		_ = mw.frpService.Disconnect()
		mw.frpService = nil
	}

	mw.usbClient = nil
}

func isLikelyTailscaleAuthKey(quicToken string) bool {
	quicToken = strings.ToLower(strings.TrimSpace(quicToken))
	return strings.HasPrefix(quicToken, "tskey-")
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

	deviceToken = strings.TrimSpace(parts[0])
	if len(parts) > 1 {
		tailscaleAuthKey = strings.TrimSpace(parts[1])
	}
	return deviceToken, tailscaleAuthKey
}

func (mw *MainWindow) resolveBridgeAuthInputs(host, quicToken string) (deviceToken, tailscaleAuthKey string) {
	deviceToken, tailscaleAuthKey = splitBridgeAuthInputs(quicToken)
	if deviceToken == "" {
		deviceToken = mw.resolveConnectionToken(host, "")
	}
	return deviceToken, tailscaleAuthKey
}

func (mw *MainWindow) handleConnectionToggle() {
	if mw.isConnectionPending.Load() {
		logrus.Warn("⚠️ Операция подключения/отключения уже выполняется, игнорируем повторное нажатие")
		return
	}

	if mw.isConnected {
		// Immediately provide visual feedback
		if mw.mainExitBtn != nil {
			mw.mainExitBtn.ApplySpec(view.HeaderActionButtonSpec{
				Disabled:      true,
				Fill:          design.ColorSurfaceLight,
				Foreground:    design.ColorTextLight,
				Stroke:        color.NRGBA{R: 0xd6, G: 0x6d, B: 0x6d, A: 0xff},
				StrokeWidth:   1.2,
				SpinnerFrames: assets.LoadingGrayFrames,
			})
		}
		mw.isConnectionPending.Store(true)
		mw.refreshConnectionControls()
		mw.enqueueLifecycleOp("disconnect", func() {
			mw.handleDisconnect()
		})
		return
	}

	if !mw.canAttemptConnection() {
		return
	}

	mw.isConnectionPending.Store(true)
	mw.setConnectionLoading(true)
	mw.hostEntry.Disable()
	mw.tokenEntry.Disable()
	mw.protocolSelect.Disable()

	go mw.handleConnect()
}

// handleConnect обрабатывает подключение
func (mw *MainWindow) handleConnect() {
	logrus.Infof("🔍 [DEBUG] handleConnect() called")

	host := mw.hostEntry.Text
	quicToken := mw.tokenEntry.Text

	if host == "" {
		logrus.Warn("Enter a server address")
		mw.clearConnectionPending()
		mw.refreshConnectionControls()
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(mw.config.APITimeout)*time.Second)
	defer cancel()

	if err := mw.doConnect(ctx, host, quicToken); err != nil {
		mw.handleConnectFailure("Connection failed", err)
	}
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
func (mw *MainWindow) doConnect(ctx context.Context, host, quicToken string) error {
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
	
	// Используем токен СТРОГО из аргумента функции (то что ввел юзер или выбрал в менеджере)
	cleanToken := strings.TrimSpace(quicToken)
	mw.activeQUICToken = cleanToken
	
	logrus.Infof("🔗 [CONNECT] start host=%s protocol=%s raw_quic_token=%s timeout=%ds",
		strings.TrimSpace(host), protocol, maskSensitiveToken(cleanToken), mw.config.APITimeout)
	return mw.doConnectWithProtocol(ctx, host, cleanToken, protocol)
}

func (mw *MainWindow) doConnectWithProtocol(ctx context.Context, host, quicToken, protocol string) error {
	connectQUICTo := func(ctx context.Context, quicHost, quicTokenParam string) error {
		if !mw.config.FRPEnabled {
			return fmt.Errorf("FRP disabled in config")
		}

		port := mw.config.FRPServerPort
		if mw.pendingQUICPort > 0 {
			port = mw.pendingQUICPort
		}

		logrus.Infof("🚇 [QUIC] creating FRP service host=%s port=%d quicToken=%s", quicHost, port, maskSensitiveToken(quicTokenParam))
		mw.frpService = service.NewFRPService(
			quicHost,
			port,
			quicTokenParam,
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
		mw.config.VideoBindHost = mw.resolveVideoBindHost()
		mw.videoWidget.SetFRPService(mw.frpService)
		mw.diskWidget.SetFRPService(mw.frpService)
		mw.connectedProtocol = models.ConnectionProtocolQUIC

		return nil
	}

	connectQUIC := func(ctx context.Context) error {
		if err := connectQUICTo(ctx, host, quicToken); err != nil {
			return err
		}

		if mw.pendingTailscaleRegister {
			quicTokenInput, tailscaleAuthKey := mw.resolveBridgeAuthInputs(host, quicToken)
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
					DeviceToken: quicTokenInput,
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
			// If registered (LoggedIn=true) but IP not yet assigned, poll briefly
			if tsStatus != nil && tsStatus.LoggedIn && strings.TrimSpace(tsStatus.IP4) == "" {
			pollQUIC:
				for attempt := 0; attempt < 5; attempt++ {
					select {
					case <-time.After(2 * time.Second):
					case <-ctx.Done():
						break pollQUIC
					}
					if fresh, statusErr := bootstrapClient.GetTailscaleStatusWithContext(ctx); statusErr == nil && fresh != nil && strings.TrimSpace(fresh.IP4) != "" {
						tsStatus = fresh
						break pollQUIC
					}
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
			if tsStatus != nil && (strings.TrimSpace(tsStatus.IP4) != "" || strings.TrimSpace(tsStatus.DNSName) != "") {
				resolvedHost := fallbackText(tsStatus.IP4, tsStatus.DNSName)
				if mw.connectionManager != nil {
					mw.connectionManager.RememberResolvedTailscaleHost(strings.TrimSpace(host), host, resolvedHost, quicTokenInput)
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
		quicTokenInput, tailscaleAuthKey := mw.resolveBridgeAuthInputs(host, quicToken)

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

		if err := connectQUICTo(ctx, bootstrapHost, quicTokenInput); err != nil {
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
				DeviceToken: quicTokenInput,
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
		// If registered (LoggedIn=true) but IP not yet assigned, poll briefly
		if tsStatus != nil && tsStatus.LoggedIn && strings.TrimSpace(tsStatus.IP4) == "" {
		pollTS:
			for attempt := 0; attempt < 5; attempt++ {
				select {
				case <-time.After(2 * time.Second):
				case <-ctx.Done():
					break pollTS
				}
				if fresh, statusErr := bootstrapClient.GetTailscaleStatusWithContext(ctx); statusErr == nil && fresh != nil && strings.TrimSpace(fresh.IP4) != "" {
					tsStatus = fresh
					break pollTS
				}
			}
		}

		resolvedHost = strings.TrimSpace(tsStatus.IP4)
		if resolvedHost == "" {
			resolvedHost = strings.TrimSpace(tsStatus.DNSName)
		}
		if resolvedHost == "" {
			return fmt.Errorf("bridge registered in tailscale but no address found")
		}

		if mw.connectionManager != nil {
			mw.connectionManager.RememberResolvedTailscaleHost(strings.TrimSpace(host), bootstrapHost, resolvedHost, quicTokenInput)
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

	mw.diskWidget.UpdateClient(mw.usbClient)
	mw.videoWidget.UpdateClient(mw.usbClient)
	if mw.backupWidget != nil {
		mw.backupWidget.UpdateClient(mw.usbClient)
	}

	mw.isConnected = true
	mw.appState.IsConnected = true
	mw.appState.LastConnected = time.Now()
	mw.connectionLossInProgress.Store(false)

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

	if mw.usbClient != nil && mw.connectionManager != nil {
		client := mw.usbClient
		connMgr := mw.connectionManager
		connHost := strings.TrimSpace(host)
		go func() {
			deviceInfo, err := client.GetDeviceInfo()
			if err == nil && deviceInfo != nil {
				osName := strings.TrimSpace(deviceInfo.AgentOS)
				if osName != "" {
					connMgr.UpdateConnectionOS(connHost, osName)
					return
				}
			}
			status, err := client.GetStatus()
			if err != nil || status == nil || status.Data == nil {
				return
			}
			osName := strings.TrimSpace(status.Data.OS)
			if osName != "" {
				connMgr.UpdateConnectionOS(connHost, osName)
			}
		}()
	}

	return nil
}

func (mw *MainWindow) verifyActiveConnectionWithContext(ctx context.Context) error {
	if mw.usbClient == nil {
		return fmt.Errorf("usb client is not initialized")
	}

	return mw.usbClient.TestConnectionWithContext(ctx)
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
		mw.activeQUICToken = ""
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
	logrus.Infof("[shutdown] handleDisconnect: start connected=%v", mw.isConnected)

	// Копируем ссылки для фоновой очистки
	client := mw.usbClient
	video := mw.videoWidget
	backup := mw.backupWidget
	frp := mw.frpService
	nbd := mw.nbdServer

	// 1. Немедленно сбрасываем состояние
	mw.isConnected = false
	mw.isStreaming = false
	mw.connectedProtocol = ""
	mw.activeQUICToken = ""
	mw.appState.IsConnected = false
	mw.appState.IsStreaming = false
	mw.appState.IsNBDRunning = false
	mw.connectionLossInProgress.Store(false)
	mw.appState.LastDisconnected = time.Now()

	// 2. Немедленно обновляем UI (уходим на экран логина)
	fyne.Do(func() {
		mw.showConnectionManager()
		if mw.mainExitBtn != nil {
			mw.mainExitBtn.ApplySpec(view.HeaderActionButtonSpec{
				Fill:        design.ColorSurfaceLight,
				Foreground:  design.ColorTextLight,
				Stroke:      color.NRGBA{R: 0xd6, G: 0x6d, B: 0x6d, A: 0xff},
				StrokeWidth: 1.2,
				Icon:        assets.ExitIcon,
				IconSize:    fyne.NewSize(24, 24),
			})
		}
		
		if mw.diskWidget != nil {
			mw.diskWidget.UpdateClient(nil)
			mw.diskWidget.SetFRPService(nil)
		}
		if video != nil {
			video.UpdateClient(nil)
			video.SetFRPService(nil)
		}
		if backup != nil {
			backup.UpdateClient(nil)
		}

		mw.usbClient = nil
		mw.frpService = nil

		mw.clearConnectionPending()
		mw.refreshConnectionControls()

		if mw.pcpanelWidget != nil {
			mw.pcpanelWidget.SetClient(nil)
		}

		mw.updateStatus()
		mw.config.VideoBindHost = "127.0.0.1"

		if !mw.isClosing.Load() {
			mw.hostEntry.Enable()
			mw.tokenEntry.Enable()
			mw.protocolSelect.Enable()
		}
		
		mw.updateStatusBar()
	})

	// 3. Выполняем тяжелую работу в ФОНЕ
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logrus.Errorf("🔥 PANIC in background disconnect cleanup: %v", r)
			}
			logrus.Info("✅ [shutdown] Background disconnect cleanup complete")
		}()

		logrus.Info("⏳ [shutdown] Background cleanup starting...")

		if video != nil {
			logrus.Info("🛑 [shutdown] Stopping video...")
			_ = video.StopVideoSync()
			video.Close() 
		}

		if backup != nil {
			backup.Close()
		}

		if client != nil {
			logrus.Info("🛑 [shutdown] Stopping remote devices...")
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = client.StopAllDevicesWithContext(cleanupCtx)
			cancel()
			client.Disconnect()
		}

		if nbd != nil && nbd.IsRunning() {
			logrus.Info("🛑 [shutdown] Stopping NBD server...")
			_ = nbd.Stop()
		}

		if frp != nil && frp.IsRunning() {
			logrus.Info("🛑 [shutdown] Stopping FRP service...")
			_ = frp.Disconnect()
		}
	}()
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

// resolveVideoBindHost returns the address on which GStreamer should listen for video.
// QUIC/FRP: FRP connects locally → 127.0.0.1.
// Tailscale: Tailscale interface (100.x.x.x), otherwise 127.0.0.1.
func (mw *MainWindow) resolveVideoBindHost() string {
	if mw.frpService != nil {
		return "127.0.0.1"
	}
	ifaces, _ := net.Interfaces()
	for _, iface := range ifaces {
		name := strings.ToLower(iface.Name)
		if !strings.Contains(name, "tailscale") && !strings.Contains(name, "wg") && !strings.Contains(name, "tun") {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok {
				if ip := ipnet.IP.To4(); ip != nil && ip[0] == 100 {
					return ip.String()
				}
			}
		}
	}
	return "127.0.0.1"
}
