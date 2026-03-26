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
	if vw.usbClient == nil {
		logrus.Warn("⚠️ USB client is not initialized")
		fyne.Do(func() {
			vw.statusLabel.SetText(i18n.Current.ErrorNoConnection)
		})
		return
	}

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
	if !vw.beginVideoOperation() {
		logrus.Warn("⚠️ video operation already in progress, skipping start")
		return
	}
	defer vw.endVideoOperation()

	vw.startVideoWithParamsInternal(request)
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
	if vw.usbClient == nil {
		logrus.Warn("⚠️ USB client not initialized")
		fyne.Do(func() {
			vw.statusLabel.SetText(i18n.Current.ErrorNoConnection)
		})
		return
	}

	if !vw.beginVideoOperation() {
		logrus.Warn("⚠️ video operation already in progress, skipping stop")
		return
	}
	defer vw.endVideoOperation()

	vw.stopVideoInternal()
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

	keyboardConnected := false
	mouseConnected := false
	storageConnected := false

	for _, device := range deviceInfo.Devices {
		if device.Status != "connected" {
			continue
		}

		switch {
		case device.Type == "keyboard" || strings.HasPrefix(device.Type, "keyboard:"):
			keyboardConnected = true
		case device.Type == "mouse" || device.Type == "touchscreen" || device.Type == "absolute" || strings.HasPrefix(device.Type, "mouse:"):
			mouseConnected = true
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
	needMouse := !mouseConnected
	if !keyboardConnected {
		requests = append(requests, models.DeviceStartRequest{
			Device:       "keyboard",
			VendorID:     "0x1d6b",
			ProductID:    "0x0104",
			ProductName:  "USBridge Keyboard",
			Manufacturer: "USBridge",
			KeyboardMode: true,
		})
	}
	if !mouseConnected {
		preferredMouseType := "absolute"
		if fyne.CurrentDevice().IsMobile() {
			preferredMouseType = "mouse"
		}
		requests = append(requests, models.DeviceStartRequest{
			Device:       "mouse",
			Type:         preferredMouseType,
			VendorID:     "0x1d6b",
			ProductID:    "0x0104",
			ProductName:  "USBridge Mouse",
			Manufacturer: "USBridge",
		})
		logrus.Infof("⌨️🖱️ Control HID auto-connect: preferring mouse type %q on this platform", preferredMouseType)
	}

	if len(requests) == 0 {
		logrus.Info("⌨️🖱️ Control HID auto-connect: keyboard and mouse already connected")
		return nil
	}

	logrus.Infof("⌨️🖱️ Control HID auto-connect: starting %d missing HID device(s)", len(requests))
	if _, err := vw.usbClient.StartDevicesBatch(requests); err != nil {
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
			if !mouseReady && (device.Type == "mouse" || device.Type == "touchscreen" || device.Type == "absolute" || strings.HasPrefix(device.Type, "mouse:")) {
				mouseReady = true
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
		if device.Status == "connected" &&
			(device.Type == "mouse" || device.Type == "touchscreen" || device.Type == "absolute" || strings.HasPrefix(device.Type, "mouse:")) {
			mouseConnected = true
			if device.Type == "touchscreen" {
				vw.SetMouseInputMode("touchscreen")
			} else if device.Type == "absolute" {
				vw.SetMouseInputMode("absolute")
			}
			logrus.Infof("🖱️ ✅ Pointer device connected: %s (type: %s)", device.Name, device.Type)
			break
		}
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

	vw.frameDecoder.IncrementFrameCount()

	if frameNum == 1 {
		logrus.Debug("✅ [VIDEO] Step 7: frame rendered in UI")
	}
	if frameNum%300 == 0 {
		logrus.Debugf("🖼️ [VIDEO] UI: processed %d frames", frameNum)
	}

	go fyne.Do(func() {
		mainWindowVisible := vw.fullscreenDialog == nil || !vw.fullscreenDialog.IsFullscreen()
		if mainWindowVisible && vw.videoCanvas != nil {
			vw.videoCanvas.Image = frame
			vw.videoCanvas.Refresh()
		}
		if mainWindowVisible && vw.touchpadWrapper != nil {
			vw.touchpadWrapper.Refresh()
		}
		if mainWindowVisible && frameNum == 1 && vw.container != nil {
			vw.container.Refresh()
		}
	})
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
			keyboardLayout.Show()
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
			logrus.Infof("⌨️ [DEBUG] contentContainer: Size=%v, Visible=%v", vw.contentContainer.Size(), vw.contentContainer.Visible())
			logrus.Info("⌨️ Virtual keyboard shown below video (Android mode)")
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
	vw.frameMutex.Unlock()
	vw.frameDecoder.Reset()

	fyne.Do(func() {
		vw.videoCanvas.Resource = nil
		vw.videoCanvas.Image = nil
		vw.videoCanvas.Refresh()
	})
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
				fyne.Do(func() {
					vw.updateStats()
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
	vw.videoOpMu.Unlock()
}
