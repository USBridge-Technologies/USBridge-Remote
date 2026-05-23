package controller

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"usbridge-client/internal/api"
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
	vw.platformRegisterGestureTarget()
	vw.ui = view.NewVideoWidgetUI(vw.touchpadWrapper, nil, vw.handleStartVideo, vw.handleStopVideo, vw.handleFullscreen)
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
		// Another op (likely stop/reconcile) is running. Mark restart so the
		// coalesced reconcile will start video once the current op finishes.
		vw.videoOpMu.Lock()
		vw.videoRestartPending = true
		vw.videoOpMu.Unlock()
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

		// Проверяем, не закрылся ли виджет пока шли HTTP-запросы
		if vw.isClosing.Load() {
			return
		}

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
				if vw.usbClient == nil {
					logrus.Warn("⚠️ video start dialog submitted but USB client is gone (disconnected)")
					return
				}
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
	cfg := models.VideoDeviceConfig{
		DevicePath:   request.VideoDevice,
		VideoWidth:   request.VideoWidth,
		VideoHeight:  request.VideoHeight,
		VideoFPS:     request.VideoFPS,
		VideoQuality: request.VideoQuality,
		VideoBitrate: request.VideoBitrate,
		VideoMode:    request.VideoMode,
	}
	if err := vw.applyVideoDeviceConfig(cfg, true); err != nil {
		logrus.Warnf("⚠️ cannot start video from request: %v", err)
		fyne.Do(func() {
			vw.statusLabel.SetText(fmt.Sprintf("❌ %v", err))
		})
	}
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
	vw.setDesiredStreaming(false)
	vw.scheduleVideoReconcile("handle-stop-video")
}

func (vw *VideoWidget) StopVideoSync() error {
	vw.setDesiredStreaming(false)

	// Используем таймаут для синхронной операции, чтобы не повесить вызывающий поток (lifecycle loop)
	done := make(chan struct{})
	go func() {
		vw.runVideoOpSync("stop-video-sync", func() {
			if vw.usbClient != nil {
				vw.stopVideoInternal()
			} else {
				// Клиент уже ушёл — чистим только локальное состояние
				vw.isStreaming = false
				vw.isGStreamerConnected = false
				vw.isMouseConnected = false
				vw.clearVideo()
				fyne.Do(func() {
					if vw.statusLabel != nil {
						vw.statusLabel.SetText(i18n.Current.VideoStopped)
					}
					vw.updateButtons()
				})
				vw.updateStatus()
			}
		})
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-time.After(4 * time.Second):
		logrus.Warn("⚠️ StopVideoSync timed out, forcing local cleanup")
		vw.isStreaming = false
		vw.isGStreamerConnected = false
		if vw.gstreamerService != nil {
			_ = vw.gstreamerService.Disconnect()
		}
		return fmt.Errorf("stop video timeout")
	}
}

func (vw *VideoWidget) stopVideoInternal() {
	logrus.Info("🛑 [VideoWidget] Internal stop starting...")

	fyne.Do(func() {
		if vw.statusLabel != nil {
			vw.statusLabel.SetText(i18n.Current.StoppingVideoCapture)
		}
	})
	resetVideoInfoCache()

	if vw.usbClient != nil {
		vw.usbClient.DisconnectMouseWebSocket()
		// Сначала говорим серверу СТОП, чтобы он перестал слать пакеты
		if err := vw.usbClient.StopVideo(); err != nil {
			logrus.Warnf("⚠️ Failed to stop video on the server: %v", err)
		}
	}

	if vw.gstreamerService != nil {
		if err := vw.gstreamerService.Disconnect(); err != nil {
			logrus.Errorf("Failed to disconnect GStreamer: %v", err)
		}
	}

	if vw.tailscaleService != nil {
		if err := vw.tailscaleService.StopVideoUDPRelay(); err != nil {
			logrus.Errorf("Failed to stop Tailscale video relay: %v", err)
		}
	}

	vw.isStreaming = false
	vw.isGStreamerConnected = false
	vw.isMouseConnected = false

	vw.resetViewport()
	vw.clearVideo()

	fyne.Do(func() {
		vw.updateButtons()
		if vw.statusLabel != nil {
			vw.statusLabel.SetText(i18n.Current.VideoStopped)
		}
	})

	vw.updateStatus()
	logrus.Info("✅ [VideoWidget] Internal stop complete")
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
	vw.SetAgentEnvironment(deviceInfo.AgentOS, deviceInfo.AgentDisplay)
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
	if needKeyboard || needMouse {
		requests = append(requests, newKeyboardStartRequest())
		requests = append(requests, newMouseStartRequest(desiredMouseType))
		logrus.Infof("⌨️🖱️ Control HID auto-connect: desired=%q keyboard_connected=%v mouse_connected=%v mode_matches=%v", desiredMouseType, keyboardConnected, mouseConnected, mouseModeMatches)
	}

	if len(requests) == 0 {
		logrus.Info("⌨️🖱️ Control HID auto-connect: keyboard and mouse already connected")
		return nil
	}

	logrus.Infof("⌨️🖱️ Control HID auto-connect: starting %d missing HID device(s)", len(requests))
	if _, err := executeDeviceBatch(vw.usbClient, vw.usbClient.StartDevicesBatchWithMerge, requests, true); err != nil {
		return fmt.Errorf("failed to auto-connect HID devices: %w", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		info, err := vw.usbClient.GetDeviceInfo()
		if err != nil {
			time.Sleep(250 * time.Millisecond)
			continue
		}

		keyboardReady := false
		mouseReady := false
		for _, device := range info.Devices {
			if device.Status != "connected" {
				continue
			}
			if device.Type == "keyboard" || strings.HasPrefix(device.Type, "keyboard:") {
				keyboardReady = true
			}
			if isMouseDeviceType(device.Type) {
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
	vw.scheduleVideoReconcile("control-bootstrap")
}

// updateButtons обновляет состояние кнопок.
func (vw *VideoWidget) updateButtons() {
	if vw.onFPSChanged != nil && !vw.isStreaming {
		vw.onFPSChanged(0)
	}
}

// Refresh обновляет виджет.
func (vw *VideoWidget) Refresh() {
	if vw.isClosing.Load() {
		return
	}
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
	if vw.isClosing.Load() || vw.usbClient == nil {
		if vw.usbClient == nil {
			logrus.Debug("🖱️ checkMouseConnected: USB client is not initialized")
		}
		vw.isMouseConnected = false
		return
	}

	deviceInfo, err := vw.usbClient.GetDeviceInfo()
	if err != nil {
		logrus.Infof("🖱️ Failed to get device information: %v", err)
		vw.isMouseConnected = false
		vw.refreshCursorOverlay()
		return
	}
	vw.SetAgentEnvironment(deviceInfo.AgentOS, deviceInfo.AgentDisplay)

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

	defer func() {
		if r := recover(); r != nil {
			logrus.Errorf("🔥 PANIC in handleVideoFrame: %v", r)
		}
	}()

	vw.frameMutex.Lock()

	// Reuse existing frame if bounds match to avoid Fyne interface tear data race
	if vw.currentFrame == nil || vw.currentFrame.Bounds() != frame.Bounds() {
		newFrame := image.NewRGBA(frame.Bounds())
		if rgba, ok := frame.(*image.RGBA); ok {
			copy(newFrame.Pix, rgba.Pix)
		} else {
			draw.Draw(newFrame, newFrame.Bounds(), frame, frame.Bounds().Min, draw.Src)
		}
		vw.currentFrame = newFrame
	} else {
		if currentRgba, ok := vw.currentFrame.(*image.RGBA); ok {
			if srcRgba, ok := frame.(*image.RGBA); ok {
				copy(currentRgba.Pix, srcRgba.Pix)
			} else {
				draw.Draw(currentRgba, currentRgba.Bounds(), frame, frame.Bounds().Min, draw.Src)
			}
		}
	}

	vw.frameCount++
	frameNum := vw.frameCount
	vw.lastFrameTime = time.Now()
	vw.frameMutex.Unlock()

	if frameNum <= 10 || frameNum%120 == 0 {
		vw.updateFrameContentRect(vw.currentFrame)
	}

	vw.frameDecoder.IncrementFrameCount()
	vw.noteVideoTraceFirstFrame(frameNum)

	if frameNum == 1 {
		bounds := vw.currentFrame.Bounds()
		logrus.Infof("✅ [VIDEO] first frame reached client trace=%s frame=%d size=%dx%d", vw.currentVideoTraceLabel(), frameNum, bounds.Dx(), bounds.Dy())
		vw.dumpFrameSnapshot(vw.currentFrame, frameNum)
	}
	if frameNum <= 5 || frameNum%300 == 0 {
		bounds := vw.currentFrame.Bounds()
		logrus.Infof("🖼️ [VIDEO] client frame trace=%s frame=%d size=%dx%d stats=%s", vw.currentVideoTraceLabel(), frameNum, bounds.Dx(), bounds.Dy(), summarizeImage(vw.currentFrame))
	}

	vw.scheduleFrameRender()
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
	vw.platformHandleVirtualKeyboard()
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

	vw.touchpadWrapper.SetKeyHandlers(vw.handlePhysicalKeyDown, vw.handlePhysicalKeyUp, vw.handlePhysicalKeyPress, vw.handlePhysicalRunePress)
	vw.touchpadWrapper.SetWindowForFocus(window)
	vw.touchpadWrapper.SetWindowFocusTarget(nil)
	vw.ensureInputFocusAsync("set-parent-window", 150*time.Millisecond)
}

func (vw *VideoWidget) ensureInputFocusAsync(reason string, delay time.Duration) {
	if vw.parentWindow == nil || vw.touchpadWrapper == nil || fyne.CurrentDevice().IsMobile() {
		return
	}
	go func() {
		if delay > 0 {
			time.Sleep(delay)
		}
		fyne.Do(func() {
			if vw.parentWindow == nil || vw.touchpadWrapper == nil {
				return
			}
			logrus.Infof("⌨️ [WINDOW][FOCUS] requesting focus reason=%s current=%T", reason, vw.parentWindow.Canvas().Focused())
			vw.parentWindow.RequestFocus()
			vw.parentWindow.Canvas().Focus(vw.touchpadWrapper)
			logrus.Infof("⌨️ [WINDOW][FOCUS] focused=%T reason=%s", vw.parentWindow.Canvas().Focused(), reason)
		})
	}()
}

func (vw *VideoWidget) SetOnFPSChanged(fn func(float64)) {
	vw.onFPSChanged = fn
}

// UpdateClient обновляет USB клиент.
func (vw *VideoWidget) UpdateClient(usbClient *api.USBClient) {
	vw.usbClient = usbClient
	if usbClient != nil {
		vw.isClosing.Store(false)
		usbClient.SetCursorUpdateHandler(vw.handleRemoteCursorUpdate)
	}
	if vw.fullscreenDialog != nil {
		vw.fullscreenDialog.SetUSBClient(usbClient)
	}
	vw.updateButtons()
}

// SetFRPService устанавливает FRP сервис.
func (vw *VideoWidget) SetFRPService(frp *service.FRPService) {
	vw.frpService = frp
}

func (vw *VideoWidget) SetTailscaleService(ts *service.TailscaleService) {
	vw.tailscaleService = ts
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
	if streaming {
		vw.ensureInputFocusAsync("set-streaming", 300*time.Millisecond)
	}
}

// StopVideo останавливает видеопоток через публичный API виджета.
func (vw *VideoWidget) StopVideo() {
	if vw.usbClient == nil {
		return
	}
	vw.runVideoOpSync("stop-video-sync", func() {
		vw.stopVideoInternal()
	})
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
	logrus.Infof("🔄 [VideoTrace #%d] local control rebuild reset", vw.videoTraceID.Load())

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
	vw.clearVideo()

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
	vw.frameRenderScheduled.Store(false)
	vw.UpdateCursorOverlayPointer(0, 0, false)

	fyne.Do(func() {
		if vw.videoCanvas != nil {
			vw.videoCanvas.Resource = nil
			vw.videoCanvas.Image = nil
			vw.videoCanvas.Refresh()
		}
		if vw.touchpadWrapper != nil {
			vw.touchpadWrapper.Refresh()
		}
		if vw.container != nil {
			vw.container.Refresh()
		}
	})
	vw.lastUIFrameRenderAt.Store(0)
	vw.forceCanvasRefresh.Store(true)
	logrus.Infof("🧹 [VideoTrace #%d] video canvas cleared", vw.videoTraceID.Load())
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
	restartPending := vw.videoRestartPending
	vw.videoOpMu.Unlock()

	if restartPending || desiredStreaming != streaming {
		vw.scheduleVideoReconcile("end-video-operation")
	}
}

func (vw *VideoWidget) finishVideoOperation() {
	vw.videoOpMu.Lock()
	vw.videoOpRunning = false
	vw.videoOpMu.Unlock()
}

func (vw *VideoWidget) scheduleFrameRender() {
	if !vw.frameRenderScheduled.CompareAndSwap(false, true) {
		return
	}
	fyne.Do(func() {
		defer func() {
			vw.frameRenderScheduled.Store(false)
			if r := recover(); r != nil {
				logrus.Errorf("🔥 PANIC in scheduleFrameRender/fyne.Do: %v", r)
			}
		}()
		vw.renderLatestFrame()
	})
}

func (vw *VideoWidget) renderLatestFrame() {
	defer func() {
		if r := recover(); r != nil {
			logrus.Errorf("🔥 PANIC in renderLatestFrame: %v", r)
		}
	}()
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
		if frameNum <= 5 || frameNum%300 == 0 {
			logrus.Infof("🪟 [VIDEO] canvas render trace=%s frame=%d stats=%s", vw.currentVideoTraceLabel(), frameNum, summarizeImage(frame))
		}
		if vw.videoCanvas.Image != frame {
			vw.videoCanvas.Image = frame
		}
		vw.videoCanvas.Refresh()
	}
	if mainWindowVisible && vw.touchpadWrapper != nil {
		vw.touchpadWrapper.Refresh()
	}
	if mainWindowVisible && (frameNum == 1 || needsFullRefresh) {
		vw.noteVideoTraceFirstPaint(frameNum)
		if vw.contentContainer != nil {
			vw.contentContainer.Refresh()
		}
		if vw.container != nil {
			vw.container.Refresh()
		}
		if vw.parentWindow != nil {
			if content := vw.parentWindow.Content(); content != nil {
				content.Refresh()
				vw.parentWindow.Canvas().Refresh(content)
			}
		}
	}
	vw.lastUIFrameRenderAt.Store(time.Now().UnixNano())
	vw.frameRenderScheduled.Store(false)

	vw.frameMutex.RLock()
	hasNewerFrame := vw.currentFrame != nil && vw.frameCount != frameNum
	vw.frameMutex.RUnlock()
	if hasNewerFrame {
		vw.scheduleFrameRender()
	}
}

func summarizeImage(img image.Image) string {
	if img == nil {
		return "none"
	}
	bounds := img.Bounds()
	if bounds.Dx() == 0 || bounds.Dy() == 0 {
		return "empty"
	}

	points := []image.Point{
		{X: bounds.Min.X, Y: bounds.Min.Y},
		{X: bounds.Min.X + bounds.Dx()/2, Y: bounds.Min.Y + bounds.Dy()/2},
		{X: bounds.Max.X - 1, Y: bounds.Min.Y},
		{X: bounds.Min.X, Y: bounds.Max.Y - 1},
		{X: bounds.Max.X - 1, Y: bounds.Max.Y - 1},
	}

	samples := make([]string, 0, len(points))
	minR, minG, minB, minA := 255, 255, 255, 255
	maxR, maxG, maxB, maxA := 0, 0, 0, 0
	nonGrayCount := 0
	opaqueCount := 0
	pixelCount := 0
	stepX := maxInt(bounds.Dx()/6, 1)
	stepY := maxInt(bounds.Dy()/6, 1)

	for y := bounds.Min.Y; y < bounds.Max.Y; y += stepY {
		for x := bounds.Min.X; x < bounds.Max.X; x += stepX {
			c := color.RGBAModel.Convert(img.At(x, y)).(color.RGBA)
			if int(c.R) < minR {
				minR = int(c.R)
			}
			if int(c.G) < minG {
				minG = int(c.G)
			}
			if int(c.B) < minB {
				minB = int(c.B)
			}
			if int(c.A) < minA {
				minA = int(c.A)
			}
			if int(c.R) > maxR {
				maxR = int(c.R)
			}
			if int(c.G) > maxG {
				maxG = int(c.G)
			}
			if int(c.B) > maxB {
				maxB = int(c.B)
			}
			if int(c.A) > maxA {
				maxA = int(c.A)
			}
			if c.R != c.G || c.G != c.B {
				nonGrayCount++
			}
			if c.A == 0xff {
				opaqueCount++
			}
			pixelCount++
		}
	}

	for _, pt := range points {
		c := color.RGBAModel.Convert(img.At(pt.X, pt.Y)).(color.RGBA)
		samples = append(samples, fmt.Sprintf("(%d,%d)=%d,%d,%d,%d", pt.X, pt.Y, c.R, c.G, c.B, c.A))
	}

	return fmt.Sprintf(
		"samples=[%s] min=%d,%d,%d,%d max=%d,%d,%d,%d non_gray=%d/%d opaque=%d/%d type=%T",
		strings.Join(samples, " "),
		minR, minG, minB, minA,
		maxR, maxG, maxB, maxA,
		nonGrayCount, pixelCount,
		opaqueCount, pixelCount,
		img,
	)
}

func (vw *VideoWidget) dumpFrameSnapshot(img image.Image, frameNum int64) {
	if img == nil {
		return
	}

	trace := vw.currentVideoTraceLabel()
	if trace == "" {
		trace = "unknown"
	}
	path := filepath.Join(os.TempDir(), fmt.Sprintf("usbridge-%s-frame-%d.png", trace, frameNum))
	file, err := os.Create(path)
	if err != nil {
		logrus.Warnf("⚠️ [VIDEO] cannot create frame snapshot %s: %v", path, err)
		return
	}
	defer file.Close()

	if err := png.Encode(file, img); err != nil {
		logrus.Warnf("⚠️ [VIDEO] cannot encode frame snapshot %s: %v", path, err)
		return
	}

	logrus.Infof("📸 [VIDEO] frame snapshot saved trace=%s frame=%d path=%s stats=%s", trace, frameNum, path, summarizeImage(img))
}
