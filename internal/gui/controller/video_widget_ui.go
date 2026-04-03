package controller

import (
	"fmt"
	"image"
	"math"
	"strings"
	"time"

	"usbridge-client/internal/api"
	"usbridge-client/internal/gui/graphics"
	"usbridge-client/internal/gui/i18n"
	"usbridge-client/internal/gui/view"
	"usbridge-client/internal/media"
	"usbridge-client/internal/models"
	"usbridge-client/internal/service"

	"fyne.io/fyne/v2"
	"github.com/sirupsen/logrus"
)

// createInterface создает интерфейс виджета.
func (vw *VideoWidget) createInterface() {
	vw.startInputWorker()
	vw.touchpadWrapper = NewTouchpadWrapper(vw)
	vw.registerMobileGestureTarget()
	vw.ui = view.NewVideoWidgetUI(vw.touchpadWrapper, vw.handleStartVideo, vw.handleStopVideo, vw.handleFullscreen)
	vw.container = vw.ui.Container
	vw.videoCanvas = vw.ui.VideoCanvas
	vw.touchpadWrapper.SetImage(vw.videoCanvas)
	vw.statusLabel = vw.ui.StatusLabel
	vw.infoLabel = vw.ui.InfoLabel
	vw.statsLabel = vw.ui.StatsLabel
	vw.contentContainer = vw.ui.ContentContainer

	vw.startStatsLoop()
	vw.updateButtons()
	vw.resetViewport()
}

// handleStartVideo обрабатывает запуск видео.
func (vw *VideoWidget) handleStartVideo() {
	vw.setDesiredStreaming(true)
	if !vw.beginVideoOperation() {
		logrus.Warn("⚠️ video operation already in progress, skipping start")
		return
	}
	go func() {
		defer vw.endVideoOperation()

		if vw.usbClient == nil {
			logrus.Warn("⚠️ USB client is not initialized")
			fyne.Do(func() {
				vw.statusLabel.SetText(i18n.Current.ErrorNoConnection)
			})
			return
		}

		fyne.Do(func() {
			if vw.statusLabel != nil {
				vw.statusLabel.SetText(i18n.Current.VideoWaitingConnection)
			}
		})

		if vw.startDialog == nil {
			if vw.parentWindow == nil {
				logrus.Warn("⚠️ Parent window not set")
				fyne.Do(func() {
					vw.statusLabel.SetText(i18n.Current.ErrorWindowNotInit)
				})
				return
			}
			vw.startDialog = view.NewVideoStartDialog(vw.parentWindow)
		}

		preferredConfig, preferredErr := vw.resolvePreferredVideoConfig()
		preferredDevicePath := ""
		if preferredErr == nil {
			preferredDevicePath = preferredConfig.DevicePath
		}

		videoInfo := vw.fetchVideoInfoForStartDialog(preferredDevicePath)

		defaultWidth := 800
		defaultHeight := 600
		defaultFPS := 30
		defaultBitrate := "2M"
		if cfg := vw.gstreamerService.GetConfig(); cfg != nil {
			if cfg.VideoWidth > 0 {
				defaultWidth = cfg.VideoWidth
			}
			if cfg.VideoHeight > 0 {
				defaultHeight = cfg.VideoHeight
			}
			if cfg.VideoFPS > 0 {
				defaultFPS = cfg.VideoFPS
			}
			if cfg.VideoBitrate > 0 {
				defaultBitrate = fmt.Sprintf("%dK", cfg.VideoBitrate)
			}
		}
		if videoInfo != nil {
			if videoInfo.Width > 0 {
				defaultWidth = videoInfo.Width
			}
			if videoInfo.Height > 0 {
				defaultHeight = videoInfo.Height
			}
			if videoInfo.FPS > 0 {
				defaultFPS = videoInfo.FPS
			}
			if videoInfo.Bitrate != "" {
				defaultBitrate = videoInfo.Bitrate
			}
		}

		fyne.Do(func() {
			vw.startDialog.Configure(videoInfo, defaultWidth, defaultHeight, defaultFPS, defaultBitrate)
			vw.startDialog.SetDeviceLabel("")
			vw.startDialog.SetPrimaryAction(i18n.Current.StartVideo)
			vw.startDialog.SetExtraAction("", nil)
			vw.startDialog.Show(func(request *models.VideoStartRequest) {
				if preferredDevicePath != "" {
					request.VideoDevice = preferredDevicePath
				}
				vw.handleVideoStartWithParams(request)
			})
			if vw.statusLabel != nil && !vw.isStreaming {
				vw.statusLabel.SetText("")
			}
		})
	}()
}

func (vw *VideoWidget) fetchVideoInfoForStartDialog(devicePath string) *models.VideoInfoData {
	const maxAttempts = 5

	var lastInfo *models.VideoInfoData
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		resp, err := vw.usbClient.GetVideoInfoForDevice(devicePath)
		if err != nil {
			logrus.Warnf("⚠️ Failed to get video info before opening start dialog (attempt %d/%d, device=%s): %v", attempt, maxAttempts, devicePath, err)
		} else if resp != nil && resp.Success && resp.Data != nil {
			parsed, parseErr := models.ParseVideoInfoData(resp.Data)
			if parseErr != nil {
				logrus.Warnf("⚠️ Failed to parse video info for start dialog (attempt %d/%d, device=%s): %v", attempt, maxAttempts, devicePath, parseErr)
			} else {
				lastInfo = parsed
				if len(parsed.CaptureModes) > 0 {
					logrus.Infof("✅ Video info for start dialog ready on attempt %d/%d: %d capture modes (device=%s)", attempt, maxAttempts, len(parsed.CaptureModes), devicePath)
					return parsed
				}
				logrus.Infof("ℹ️ Video info for start dialog attempt %d/%d returned no capture modes yet, retrying... (device=%s)", attempt, maxAttempts, devicePath)
			}
		}

		if attempt < maxAttempts {
			time.Sleep(200 * time.Millisecond)
		}
	}

	return lastInfo
}

// handleVideoStartWithParams обрабатывает запуск видео с параметрами из диалога.
func (vw *VideoWidget) handleVideoStartWithParams(request *models.VideoStartRequest) {
	vw.setDesiredStreaming(true)
	if !vw.beginVideoOperation() {
		logrus.Warn("⚠️ video operation already in progress, skipping start")
		return
	}
	go func() {
		defer vw.endVideoOperation()
		vw.startVideoWithParamsInternal(request)
	}()
}

func (vw *VideoWidget) startVideoWithParamsInternal(request *models.VideoStartRequest) {
	if vw.gstreamerService == nil {
		logrus.Warn("⚠️ GStreamer service is not initialized")
		fyne.Do(func() {
			vw.statusLabel.SetText(i18n.Current.VideoLaunchFailed)
		})
		return
	}

	vw.handleVideoStartWithParamsGStreamer(request)
}

// handleStopVideo обрабатывает остановку видео.
func (vw *VideoWidget) handleStopVideo() {
	vw.setDesiredStreaming(false)
	if !vw.beginVideoOperation() {
		logrus.Warn("⚠️ video operation already in progress, skipping stop")
		return
	}
	go func() {
		defer vw.endVideoOperation()
		if vw.usbClient == nil {
			logrus.Warn("⚠️ USB client not initialized")
			fyne.Do(func() {
				vw.statusLabel.SetText(i18n.Current.ErrorNoConnection)
			})
			return
		}
		vw.stopVideoInternal()
	}()
}

func (vw *VideoWidget) StopVideoSync() error {
	vw.setDesiredStreaming(false)
	for attempt := 0; attempt < 2; attempt++ {
		if vw.beginVideoOperation() {
			defer vw.endVideoOperation()
			if vw.usbClient == nil {
				return nil
			}
			vw.stopVideoInternal()
			return nil
		}
		if err := vw.waitForVideoOperation(5 * time.Second); err != nil {
			return err
		}
	}

	if vw.IsStreaming() {
		return fmt.Errorf("video stop operation is busy")
	}
	return nil
}

func (vw *VideoWidget) waitForVideoOperation(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		vw.videoOpMu.Lock()
		running := vw.videoOpRunning
		vw.videoOpMu.Unlock()
		if !running {
			return nil
		}
		time.Sleep(25 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for video operation")
}

func (vw *VideoWidget) stopVideoInternal() {

	fyne.Do(func() {
		vw.statusLabel.SetText(i18n.Current.StoppingVideoCapture)
	})
	resetVideoInfoCache()

	vw.usbClient.DisconnectMouseWebSocket()

	if vw.gstreamerService != nil {
		if err := vw.gstreamerService.Disconnect(); err != nil {
			logrus.Errorf("Failed to disconnect GStreamer: %v", err)
		}
	}

	if err := vw.usbClient.StopVideo(); err != nil {
		logrus.Warnf("⚠️ Failed to stop video on the server: %v (ignoring because it may already be stopped)", err)
	}

	vw.isStreaming = false
	vw.isGStreamerConnected = false
	vw.isMouseConnected = false

	vw.resetViewport()
	vw.clearVideo()

	fyne.Do(func() {
		vw.updateButtons()
		vw.statusLabel.SetText(i18n.Current.VideoStopped)
	})

	vw.updateStatus()
	logrus.Info("🛑 Video capture stopped")
}

func isConnectedStorageDevice(device models.DeviceInfo) bool {
	if device.Status != "connected" {
		return false
	}

	switch {
	case device.Type == "local":
		return true
	case device.Type == "mtp":
		return true
	case device.Type == "nbd":
		return true
	case strings.HasPrefix(device.Device, "disk:"):
		return true
	case strings.HasPrefix(device.Device, "drive"):
		return true
	case strings.HasPrefix(device.Device, "mtp"):
		return true
	}

	return false
}

func (vw *VideoWidget) ensureControlHIDDevices() error {
	if vw.usbClient == nil {
		return nil
	}

	deviceInfo, err := vw.usbClient.GetDeviceInfo()
	if err != nil {
		return fmt.Errorf("failed to get device info before HID auto-connect: %w", err)
	}
	if deviceInfo.MountInProgress {
		logrus.Infof("⌨️🖱️ Control HID auto-connect skipped: gadget reconfiguration already in progress (desired=%s)", vw.GetMouseInputMode())
		return nil
	}

	keyboardConnected := false
	mouseConnected := false
	mouseModeMatches := false
	storageConnected := false
	desiredMouseType := vw.GetMouseInputMode()

	for _, device := range deviceInfo.Devices {
		if device.Status != "connected" {
			continue
		}

		switch {
		case device.Type == "keyboard" || strings.HasPrefix(device.Type, "keyboard:"):
			keyboardConnected = true
		case isMouseDeviceType(device.Type):
			mouseConnected = true
			observedMode := mouseModeFromDeviceType(device.Type)
			vw.setObservedMouseMode(observedMode)
			mouseModeMatches = observedMode == desiredMouseType
		}

		if isConnectedStorageDevice(device) {
			storageConnected = true
		}
	}

	if storageConnected {
		logrus.Info("💿 Control HID auto-connect skipped: storage devices are connected, avoiding gadget reconfiguration")
		return nil
	}

	var requests models.DeviceStartBatchRequest
	needKeyboard := !keyboardConnected
	needMouse := !mouseConnected || !mouseModeMatches
	if !keyboardConnected {
		requests = append(requests, newKeyboardStartRequest())
	}
	if needMouse {
		requests = append(requests, newMouseStartRequest(desiredMouseType))
		logrus.Infof("⌨️🖱️ Control HID auto-connect: desired=%q connected=%v mode_matches=%v", desiredMouseType, mouseConnected, mouseModeMatches)
	}

	if len(requests) == 0 {
		logrus.Info("⌨️🖱️ Control HID auto-connect: keyboard and mouse already connected")
		return nil
	}

	logrus.Infof("⌨️🖱️ Control HID auto-connect: starting %d missing HID device(s)", len(requests))
	if _, err := rebuildUSBGadgetDevices(vw.usbClient, vw.usbClient.StartDevicesBatch, requests); err != nil {
		return fmt.Errorf("failed to auto-connect HID devices: %w", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		info, err := vw.usbClient.GetDeviceInfo()
		if err != nil {
			time.Sleep(250 * time.Millisecond)
			continue
		}

		keyboardReady := !needKeyboard
		mouseReady := !needMouse
		for _, device := range info.Devices {
			if device.Status != "connected" {
				continue
			}
			if !keyboardReady && (device.Type == "keyboard" || strings.HasPrefix(device.Type, "keyboard:")) {
				keyboardReady = true
			}
			if !mouseReady && isMouseDeviceType(device.Type) {
				observedMode := mouseModeFromDeviceType(device.Type)
				vw.setObservedMouseMode(observedMode)
				mouseReady = observedMode == desiredMouseType
			}
		}

		if keyboardReady && mouseReady {
			logrus.Info("✅ Control HID auto-connect completed and devices are visible in device/info")
			return nil
		}

		time.Sleep(250 * time.Millisecond)
	}

	return fmt.Errorf("timed out waiting for HID devices after auto-connect")
}

func (vw *VideoWidget) controlHIDReady() (bool, error) {
	if vw.usbClient == nil {
		return false, nil
	}

	deviceInfo, err := vw.usbClient.GetDeviceInfo()
	if err != nil {
		return false, err
	}
	if deviceInfo.MountInProgress {
		return false, nil
	}

	keyboardConnected := false
	mouseConnected := false
	mouseModeMatches := false
	desiredMouseType := vw.GetMouseInputMode()

	for _, device := range deviceInfo.Devices {
		if device.Status != "connected" {
			continue
		}
		switch {
		case device.Type == "keyboard" || strings.HasPrefix(device.Type, "keyboard:"):
			keyboardConnected = true
		case isMouseDeviceType(device.Type):
			mouseConnected = true
			observedMode := mouseModeFromDeviceType(device.Type)
			vw.setObservedMouseMode(observedMode)
			mouseModeMatches = observedMode == desiredMouseType
		}
	}

	return keyboardConnected && mouseConnected && mouseModeMatches, nil
}

func (vw *VideoWidget) BootstrapControlSessionAsync() {
	vw.setDesiredStreaming(true)

	go func() {
		const maxAttempts = 8
		for attempt := 1; attempt <= maxAttempts; attempt++ {
			if vw.usbClient == nil || !vw.desiredStreamingState() {
				return
			}

			ready, err := vw.controlHIDReady()
			if err != nil {
				logrus.Warnf("⚠️ control bootstrap: failed to inspect HID state on attempt %d/%d: %v", attempt, maxAttempts, err)
			} else if !ready {
				if hidErr := vw.ensureControlHIDDevices(); hidErr != nil {
					logrus.Warnf("⚠️ control bootstrap: HID auto-connect attempt %d/%d failed: %v", attempt, maxAttempts, hidErr)
				}
			}

			ready, err = vw.controlHIDReady()
			if err == nil && ready {
				vw.ReconcileDesiredStreaming()
				if vw.IsStreaming() {
					logrus.Infof("✅ control bootstrap completed on attempt %d/%d", attempt, maxAttempts)
					return
				}
			}

			time.Sleep(700 * time.Millisecond)
		}
	}()
}

// updateButtons обновляет состояние кнопок.
func (vw *VideoWidget) updateButtons() {
	if vw.onFPSChanged != nil && !vw.isStreaming {
		vw.onFPSChanged(0)
	}
}

// Refresh обновляет виджет.
func (vw *VideoWidget) Refresh() {
	if vw.usbClient == nil {
		logrus.Debug("USB client is not initialized, skipping video refresh")
		fyne.Do(func() {
			vw.infoLabel.SetText(i18n.Current.VideoWaitingConnection)
		})
		return
	}

	vw.checkMouseConnected()

	videoInfo, err := vw.usbClient.GetVideoInfo()
	if err != nil {
		logrus.Errorf("Failed to get video information: %v", err)
		fyne.Do(func() {
			vw.infoLabel.SetText(i18n.Current.ErrorVideoInfo)
		})
		return
	}

	fyne.Do(func() {
		if videoInfo.Success && videoInfo.Data != nil {
			vw.infoLabel.SetText(i18n.Current.VideoInfoReceived)
		} else {
			vw.infoLabel.SetText(i18n.Current.VideoInfoUnavailable)
		}
	})

	if vw.gstreamerService != nil {
		vw.updateGStreamerStats()
	}
}

// checkMouseConnected проверяет, подключена ли мышь.
func (vw *VideoWidget) checkMouseConnected() {
	if vw.usbClient == nil {
		logrus.Debug("🖱️ checkMouseConnected: USB client is not initialized")
		vw.isMouseConnected = false
		return
	}

	deviceInfo, err := vw.usbClient.GetDeviceInfo()
	if err != nil {
		logrus.Infof("🖱️ Failed to get device information: %v", err)
		vw.isMouseConnected = false
		return
	}

	logrus.Debugf("🖱️ checkMouseConnected: received %d devices", len(deviceInfo.Devices))

	mouseConnected := false
	for _, device := range deviceInfo.Devices {
		logrus.Debugf("🖱️ Inspecting device: type=%s, status=%s, name=%s", device.Type, device.Status, device.Name)
		if device.Status == "connected" && isMouseDeviceType(device.Type) {
			mouseConnected = true
			vw.setObservedMouseMode(mouseModeFromDeviceType(device.Type))
			logrus.Infof("🖱️ ✅ Pointer device connected: %s (type: %s)", device.Name, device.Type)
			break
		}
	}
	if !mouseConnected {
		vw.setObservedMouseMode("")
	}

	logrus.Debugf("🖱️ checkMouseConnected: mouseConnected=%v (previously %v)", mouseConnected, vw.isMouseConnected)

	if vw.isMouseConnected != mouseConnected {
		vw.isMouseConnected = mouseConnected
		if mouseConnected {
			logrus.Info("🖱️ Touchpad activated: pointer device connected")
			go func() {
				if err := vw.usbClient.ConnectMouseWebSocket(); err != nil {
					logrus.Warnf("⚠️ Failed to connect mouse WebSocket: %v (HTTP fallback will be used)", err)
				} else {
					logrus.Info("✅ Mouse WebSocket connected successfully")
				}
			}()
			vw.startDesktopMousePolling()
			logrus.Info("🖱️ Pointer device connected (WebSocket)")
		} else {
			logrus.Info("🖱️ Touchpad deactivated: pointer device disconnected")
			vw.stopDesktopMousePolling()
			vw.usbClient.DisconnectMouseWebSocket()
			fyne.Do(func() {
				if vw.statusLabel != nil {
					vw.statusLabel.SetText("")
				}
			})
		}
	}
}

// handleVideoFrame обрабатывает полученный видео кадр.
func (vw *VideoWidget) handleVideoFrame(frame image.Image) {
	if frame == nil {
		return
	}

	vw.frameMutex.Lock()
	vw.currentFrame = frame
	vw.frameCount++
	frameNum := vw.frameCount
	vw.lastFrameTime = time.Now()
	vw.frameMutex.Unlock()

	if frameNum <= 10 || frameNum%120 == 0 {
		vw.updateFrameContentRect(frame)
	}

	vw.frameDecoder.IncrementFrameCount()

	if frameNum == 1 {
		logrus.Debug("✅ [VIDEO] Step 7: frame rendered in UI")
	}
	if frameNum%300 == 0 {
		logrus.Debugf("🖼️ [VIDEO] UI: processed %d frames", frameNum)
	}

	vw.scheduleFrameRender(frameNum)
}

// handleFullscreen обрабатывает переключение в полноэкранный режим.
func (vw *VideoWidget) handleFullscreen() {
	vw.ShowFullscreen()
}

func (vw *VideoWidget) ShowFullscreen() {
	if vw.fullscreenDialog == nil {
		if vw.parentWindow == nil {
			logrus.Warn("⚠️ Parent window is not set")
			return
		}
		vw.fullscreenDialog = NewFullscreenDialog(vw.parentWindow)
		vw.fullscreenDialog.SetVideoWidget(vw)
		vw.fullscreenDialog.SetGStreamerService(vw.gstreamerService)
		if vw.usbClient != nil {
			vw.fullscreenDialog.SetUSBClient(vw.usbClient)
		}
	}

	vw.fullscreenDialog.Show()
}

// HandleVirtualKeyboard обрабатывает открытие/закрытие виртуальной клавиатуры.
func (vw *VideoWidget) HandleVirtualKeyboard() {
	if vw.virtualKeyboard == nil {
		if vw.parentWindow == nil {
			logrus.Warn("⚠️ Parent window is not set")
			return
		}
		vw.virtualKeyboard = graphics.NewVirtualKeyboard(vw.parentWindow, vw.handleVirtualKeyPress, vw.handlePhysicalRunePress)
	}

	isAndroid := fyne.CurrentDevice().IsMobile()

	if vw.virtualKeyboard.IsVisible() {
		vw.virtualKeyboard.Hide()

		if isAndroid {
			vw.contentContainer.Hide()
			vw.container.Refresh()
			logrus.Info("⌨️ Virtual keyboard hidden (Android mode)")
		} else {
			logrus.Info("⌨️ Virtual keyboard hidden (desktop mode)")
		}
	} else {
		if isAndroid {
			keyboardLayout := vw.virtualKeyboard.GetKeyboardLayout()
			logrus.Infof("⌨️ [DEBUG] keyboardLayout MinSize: %v", keyboardLayout.MinSize())
			vw.virtualKeyboard.SetVisibleState(true)

			canvasSize := vw.parentWindow.Canvas().Size()
			logrus.Infof("⌨️ [DEBUG] Canvas Size: %v", canvasSize)

			keyboardSize := fyne.NewSize(canvasSize.Width, 300)
			keyboardLayout.Resize(keyboardSize)
			keyboardLayout.Move(fyne.NewPos(0, 0))
			logrus.Infof("⌨️ [DEBUG] keyboardLayout after resize: size=%v, position=%v", keyboardLayout.Size(), keyboardLayout.Position())

			vw.contentContainer.Objects = []fyne.CanvasObject{keyboardLayout}
			vw.contentContainer.Resize(keyboardSize)
			vw.contentContainer.Show()
			vw.container.Refresh()
			go func() {
				time.Sleep(50 * time.Millisecond)
				fyne.Do(func() {
					if vw.virtualKeyboard != nil && vw.virtualKeyboard.IsVisible() {
						vw.virtualKeyboard.FocusInput()
					}
				})
			}()
			logrus.Infof("⌨️ [DEBUG] contentContainer: Size=%v, Visible=%v", vw.contentContainer.Size(), vw.contentContainer.Visible())
			logrus.Info("⌨️ Virtual keyboard shown with Android IME")
		} else {
			vw.virtualKeyboard.ShowInSeparateWindow()
			logrus.Info("⌨️ Virtual keyboard shown in a separate window (desktop mode)")
		}
	}
}

// updateStats обновляет статистику.
func (vw *VideoWidget) updateStats() {
	vw.frameMutex.RLock()
	lastFrameTime := vw.lastFrameTime
	vw.frameMutex.RUnlock()

	decoderStats := vw.frameDecoder.GetFrameStats()
	fps := decoderStats["fps"].(float64)

	stats := fmt.Sprintf("FPS: %.1f | %s", fps, lastFrameTime.Format("15:04:05"))
	vw.statsLabel.SetText(stats)
	if vw.onFPSChanged != nil {
		vw.onFPSChanged(math.Round(fps*10) / 10)
	}
}

// SetParentWindow устанавливает родительское окно для диалогов.
func (vw *VideoWidget) SetParentWindow(window fyne.Window) {
	vw.parentWindow = window

	vw.touchpadWrapper.SetKeyHandlers(vw.handlePhysicalKeyPress, vw.handlePhysicalRunePress)
	vw.touchpadWrapper.SetWindowForFocus(window)

	window.Canvas().SetOnTypedKey(func(event *fyne.KeyEvent) {
		if event.Name == fyne.KeyF11 && vw.isStreaming {
			logrus.Info("🔍 F11 pressed, entering fullscreen mode")
			vw.ShowFullscreen()
		}
	})
}

func (vw *VideoWidget) SetOnFPSChanged(fn func(float64)) {
	vw.onFPSChanged = fn
}

// UpdateClient обновляет USB клиент.
func (vw *VideoWidget) UpdateClient(usbClient *api.USBClient) {
	vw.usbClient = usbClient
	if vw.fullscreenDialog != nil {
		vw.fullscreenDialog.SetUSBClient(usbClient)
	}
	vw.updateButtons()
}

// SetFRPService устанавливает FRP сервис.
func (vw *VideoWidget) SetFRPService(frp *service.FRPService) {
	vw.frpService = frp
}

// GetContainer возвращает контейнер виджета.
func (vw *VideoWidget) GetContainer() *fyne.Container {
	return vw.container
}

// IsStreaming возвращает состояние захвата.
func (vw *VideoWidget) IsStreaming() bool {
	return vw.isStreaming
}

// SetStreaming устанавливает состояние захвата.
func (vw *VideoWidget) SetStreaming(streaming bool) {
	vw.isStreaming = streaming
	vw.updateButtons()
}

// StopVideo останавливает видеопоток через публичный API виджета.
func (vw *VideoWidget) StopVideo() {
	vw.handleStopVideo()
}

// HandleConnectionLost останавливает локальные video/input ресурсы без запроса к серверу.
func (vw *VideoWidget) HandleConnectionLost() {
	resetVideoInfoCache()

	if vw.usbClient != nil {
		vw.usbClient.DisconnectMouseWebSocket()
	}
	if vw.gstreamerService != nil {
		if err := vw.gstreamerService.Disconnect(); err != nil {
			logrus.Warnf("⚠️ Failed to disconnect GStreamer after transport loss: %v", err)
		}
	}

	vw.isStreaming = false
	vw.isGStreamerConnected = false
	vw.isMouseConnected = false
	vw.clearVideo()

	fyne.Do(func() {
		vw.updateButtons()
		if vw.statusLabel != nil {
			vw.statusLabel.SetText(i18n.Current.ErrorNoConnection)
		}
	})

	vw.updateStatus()
}

func (vw *VideoWidget) handleDeviceRebuildLocally() {
	resetVideoInfoCache()

	if vw.usbClient != nil {
		vw.usbClient.DisconnectMouseWebSocket()
	}
	vw.stopDesktopMousePolling()
	if vw.gstreamerService != nil {
		if err := vw.gstreamerService.Disconnect(); err != nil {
			logrus.Warnf("⚠️ Failed to disconnect GStreamer after device rebuild: %v", err)
		}
	}

	vw.isStreaming = false
	vw.isGStreamerConnected = false
	vw.isMouseConnected = false

	fyne.Do(func() {
		vw.updateButtons()
		if vw.statusLabel != nil {
			vw.statusLabel.SetText(i18n.Current.VideoWaitingConnection)
		}
	})
}

// ExitFullscreenIfNeeded закрывает fullscreen-режим, если он активен.
func (vw *VideoWidget) ExitFullscreenIfNeeded() bool {
	if vw.fullscreenDialog == nil || !vw.fullscreenDialog.IsFullscreen() {
		return false
	}
	vw.fullscreenDialog.exitFullscreen()
	return true
}

// clearVideo очищает видео.
func (vw *VideoWidget) clearVideo() {
	vw.frameMutex.Lock()
	vw.currentFrame = nil
	vw.frameCount = 0
	vw.lastFrameTime = time.Time{}
	vw.frameContentX = 0
	vw.frameContentY = 0
	vw.frameContentW = 0
	vw.frameContentH = 0
	vw.frameMutex.Unlock()
	vw.frameDecoder.Reset()

	fyne.Do(func() {
		vw.videoCanvas.Resource = nil
		vw.videoCanvas.Image = nil
		vw.videoCanvas.Refresh()
	})
	vw.lastUIFrameRenderAt.Store(0)
	vw.forceCanvasRefresh.Store(true)
}

// GetCurrentFrame возвращает текущий кадр для полноэкранного режима.
func (vw *VideoWidget) GetCurrentFrame() image.Image {
	vw.frameMutex.RLock()
	defer vw.frameMutex.RUnlock()
	return vw.currentFrame
}

// GetFrameDecoder возвращает декодер кадров для полноэкранного режима.
func (vw *VideoWidget) GetFrameDecoder() *media.FrameDecoder {
	return vw.frameDecoder
}

func (vw *VideoWidget) startStatsLoop() {
	if vw.statsTickerStop != nil {
		close(vw.statsTickerStop)
	}
	vw.statsTickerStop = make(chan struct{})

	go func(stop <-chan struct{}) {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				if !vw.IsStreaming() {
					continue
				}
				fyne.Do(func() {
					if vw.IsStreaming() {
						vw.updateStats()
					}
				})
			case <-stop:
				return
			}
		}
	}(vw.statsTickerStop)
}

func (vw *VideoWidget) beginVideoOperation() bool {
	vw.videoOpMu.Lock()
	defer vw.videoOpMu.Unlock()
	if vw.videoOpRunning {
		return false
	}
	vw.videoOpRunning = true
	return true
}

func (vw *VideoWidget) endVideoOperation() {
	vw.videoOpMu.Lock()
	vw.videoOpRunning = false
	desiredStreaming := vw.desiredStreaming
	streaming := vw.isStreaming
	vw.videoOpMu.Unlock()

	if desiredStreaming && !streaming {
		vw.StartConfiguredVideoAsync()
		return
	}
	if !desiredStreaming && streaming {
		vw.StopVideoAsync()
	}
}

func (vw *VideoWidget) scheduleFrameRender(frameNum int64) {
	if !vw.frameRenderScheduled.CompareAndSwap(false, true) {
		return
	}
	vw.scheduleFrameRenderAfter(vw.nextFrameRenderDelay())
}

func (vw *VideoWidget) scheduleFrameRenderAfter(delay time.Duration) {
	run := func() {
		fyne.Do(func() {
			if nextDelay := vw.nextFrameRenderDelay(); nextDelay > 0 {
				vw.scheduleFrameRenderAfter(nextDelay)
				return
			}
			vw.renderLatestFrame()
		})
	}
	if delay <= 0 {
		run()
		return
	}
	time.AfterFunc(delay, run)
}

func (vw *VideoWidget) nextFrameRenderDelay() time.Duration {
	interval := vw.targetUIFrameInterval()

	last := vw.lastUIFrameRenderAt.Load()
	if last == 0 {
		return 0
	}

	elapsed := time.Since(time.Unix(0, last))
	if elapsed >= interval {
		return 0
	}
	return interval - elapsed
}

func (vw *VideoWidget) targetUIFrameInterval() time.Duration {
	targetFPS := 45
	if fyne.CurrentDevice().IsMobile() {
		targetFPS = 30
	}

	if vw.gstreamerService != nil {
		if cfg := vw.gstreamerService.GetConfig(); cfg != nil && cfg.VideoFPS > 0 {
			targetFPS = cfg.VideoFPS
		}
	}

	switch {
	case targetFPS < 15:
		targetFPS = 15
	case targetFPS > 60:
		targetFPS = 60
	}

	return time.Second / time.Duration(targetFPS)
}

func (vw *VideoWidget) renderLatestFrame() {
	defer vw.frameRenderScheduled.Store(false)

	vw.frameMutex.RLock()
	frame := vw.currentFrame
	frameNum := vw.frameCount
	vw.frameMutex.RUnlock()
	if frame == nil {
		return
	}

	mainWindowVisible := vw.fullscreenDialog == nil || !vw.fullscreenDialog.IsFullscreen()
	needsFullRefresh := vw.forceCanvasRefresh.Swap(false)
	if mainWindowVisible && vw.videoCanvas != nil {
		vw.videoCanvas.Image = frame
		vw.videoCanvas.Refresh()
	}
	if mainWindowVisible && vw.touchpadWrapper != nil {
		vw.touchpadWrapper.Refresh()
	}
	if mainWindowVisible && (frameNum == 1 || needsFullRefresh) && vw.container != nil {
		vw.container.Refresh()
	}
	vw.lastUIFrameRenderAt.Store(time.Now().UnixNano())
}
