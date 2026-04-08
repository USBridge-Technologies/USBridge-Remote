package controller

import (
	"fmt"
	"image"
	"time"

	"usbridge-client/internal/api"
	"usbridge-client/internal/gui/i18n"
	"usbridge-client/internal/media"
	"usbridge-client/internal/models"
	"usbridge-client/internal/service"

	"fyne.io/fyne/v2"
	"github.com/sirupsen/logrus"
)

// NewVideoWidgetGStreamer создает новый виджет видео с GStreamer
func NewVideoWidgetGStreamer(usbClient *api.USBClient, gstreamerService *service.GStreamerService, updateStatus func()) *VideoWidget {
	vw := &VideoWidget{
		usbClient:        usbClient,
		gstreamerService: gstreamerService,
		updateStatus:     updateStatus,
		isStreaming:      false,
		frameDecoder:     media.NewFrameDecoder(),
	}

	vw.createInterface()
	vw.startVideoOpsLoop()
	vw.setupGStreamerCallbacks()
	return vw
}

// VideoWidget обновлённая структура с GStreamer
type VideoWidgetExt struct {
	VideoWidget
	gstreamerService *service.GStreamerService
}

// setupGStreamerCallbacks настраивает callbacks для GStreamer
func (vw *VideoWidget) setupGStreamerCallbacks() {
	if vw.gstreamerService == nil {
		logrus.Warn("⚠️ GStreamer сервис не инициализирован")
		return
	}

	// Callback для получения видео кадров
	vw.gstreamerService.SetOnFrameReceived(func(frame image.Image) {
		vw.handleVideoFrame(frame)
	})

	// Callback для изменения состояния соединения
	vw.gstreamerService.SetOnStateChanged(func(state string) {
		vw.handleGStreamerStateChange(state)
	})

	// Callback для ошибок
	vw.gstreamerService.SetOnError(func(err error) {
		logrus.Errorf("GStreamer ошибка: %v", err)
		fyne.Do(func() {
			vw.statusLabel.SetText(fmt.Sprintf(i18n.Current.GStreamerError, err))
		})
	})
}

// handleGStreamerStateChange обрабатывает изменение состояния GStreamer
func (vw *VideoWidget) handleGStreamerStateChange(state string) {
	fyne.Do(func() {
		switch state {
		case "playing", "connected":
			// Android шлёт "connected" при ConnectToUDP, Darwin — "playing" при PLAYING
			vw.isGStreamerConnected = true
			vw.infoLabel.SetText("✅ " + i18n.Current.GStreamerConnected)
			vw.clearVideo()
			vw.frameMutex.Lock()
			vw.lastFrameTime = time.Time{}
			vw.frameMutex.Unlock()
		case "paused":
			vw.infoLabel.SetText("⏸️ " + i18n.Current.GStreamerPaused)
		case "null", "ready":
			vw.isGStreamerConnected = false
			vw.infoLabel.SetText("⚠️ " + i18n.Current.GStreamerDisconnected)
		case "eos":
			vw.isGStreamerConnected = false
			traceID := vw.videoTraceID.Load()
			firstFrameNs := vw.videoTraceFirstFrame.Load()
			firstPaintNs := vw.videoTraceFirstPaint.Load()
			logrus.Warnf("⚠️ [VideoTrace #%d] GStreamer EOS first_frame=%v first_paint=%v", traceID, firstFrameNs != 0, firstPaintNs != 0)
			vw.infoLabel.SetText("❌ " + i18n.Current.GStreamerEndOfStream)
		}
	})
}

// handleVideoStartWithParams обновлённая версия для GStreamer (новый UDP протокол)
func (vw *VideoWidget) handleVideoStartWithParamsGStreamer(request *models.VideoStartRequest) {
	fyne.Do(func() {
		vw.statusLabel.SetText(i18n.Current.StartingVideoCapture)
	})
	traceID := vw.beginVideoTrace(fmt.Sprintf("mode=%s device=%s", request.VideoMode, request.VideoDevice))
	logrus.Infof("🎯 [VideoTrace #%d] preparing GStreamer/video start", traceID)

	if vw.gstreamerService != nil {
		if cfg := vw.gstreamerService.GetConfig(); cfg != nil {
			if request.VideoWidth > 0 {
				cfg.VideoWidth = request.VideoWidth
			}
			if request.VideoHeight > 0 {
				cfg.VideoHeight = request.VideoHeight
			}
			if request.VideoFPS > 0 {
				cfg.VideoFPS = request.VideoFPS
			}
		}
	}

	// Если видео уже запущено, значит это переключение устройства - переподключаемся
	wasStreaming := vw.isStreaming
	if vw.gstreamerService != nil {
		// Гасим старый reconnect-loop до нового ручного старта.
		vw.gstreamerService.SetAutoReconnect(false)
	}
	if wasStreaming && vw.gstreamerService != nil {
		logrus.Info("🔄 Видео уже запущено - переключение устройства...")
		fyne.Do(func() {
			vw.statusLabel.SetText(i18n.Current.SwitchingDevice)
		})

		// Отключаем старый поток
		if err := vw.gstreamerService.Disconnect(); err != nil {
			logrus.Warnf("⚠️ Ошибка отключения старого потока: %v", err)
		}

		// Небольшая задержка для корректного отключения
		time.Sleep(300 * time.Millisecond)
	}
	if !wasStreaming && vw.gstreamerService != nil {
		if err := vw.gstreamerService.Disconnect(); err != nil {
			logrus.Warnf("⚠️ Ошибка отключения старого/зависшего потока перед новым стартом: %v", err)
		}
		time.Sleep(150 * time.Millisecond)
	}

	// Сбрасываем reconnect счетчик и включаем autoReconnect при новом запуске
	if vw.gstreamerService != nil {
		vw.gstreamerService.SetAutoReconnect(true)
		vw.gstreamerService.SetMaxReconnectAttempts(5)
		vw.gstreamerService.SetVideoMode(request.VideoMode)
		vw.gstreamerService.SetExpectedVideoSize(request.VideoWidth, request.VideoHeight)
	}

	clientPort := models.DefaultVideoUDPPort
	if vw.frpService != nil && vw.frpService.IsRunning() {
		_, clientPort, _ = vw.frpService.GetServerPorts()
	} else {
		preferredPort := clientPort
		if cfg := vw.gstreamerService.GetConfig(); cfg != nil && cfg.VideoUDPPort > 0 {
			preferredPort = cfg.VideoUDPPort
		}
		allocatedPort, err := service.FindAvailableUDPPort(preferredPort)
		if err != nil {
			logrus.Errorf("❌ Не удалось выбрать свободный UDP порт для видео: %v", err)
			fyne.Do(func() {
				vw.statusLabel.SetText(fmt.Sprintf(i18n.Current.ErrorVideoStart, err))
			})
			return
		}
		clientPort = allocatedPort
		if vw.gstreamerService != nil {
			vw.gstreamerService.UpdateVideoPort(clientPort)
			vw.gstreamerService.UpdateVideoUDPPort(clientPort)
		}
	}
	request.ClientPort = clientPort

	mode := request.VideoMode
	if mode == "" {
		mode = models.VideoModeH264
	}

	// 1. Сначала, при необходимости, поднимаем HID и ждём завершения переконфигурации gadget.
	logrus.Debug("⌨️🖱️ [VIDEO] Проверка и автоподключение HID перед стартом видео...")
	if err := vw.ensureControlHIDDevices(); err != nil {
		logrus.Warnf("⚠️ [VIDEO] HID auto-connect before video failed: %v", err)
	}

	// 2. Запускаем GStreamer: udpsrc RTP на clientPort (при FRP — proxy слушает, Bridge visitor шлёт)
	logrus.Infof("🎬 [VIDEO] Подготовка video pipeline (mode=%s, port=%d)...", mode, clientPort)
	if !vw.connectToGStreamerWithRetries() {
		logrus.Error("❌ Не удалось запустить GStreamer")
		fyne.Do(func() {
			vw.statusLabel.SetText("❌ " + i18n.Current.VideoLaunchFailed)
		})
		return
	}

	// 2.5 Короткая пауза, чтобы локальный RTP listener успел перейти в рабочее состояние
	time.Sleep(150 * time.Millisecond)
	logrus.Debugf("🔗 [VIDEO] udpsrc port=%d готов к приёму, запуск сервера...", clientPort)

	// 2.6 Сбрасываем предыдущий стрим на сервере только при реальном переключении уже активного видео.
	if wasStreaming {
		logrus.Debug("🛑 [VIDEO] POST /api/video/stop (сброс предыдущего стрима)")
		_ = vw.usbClient.StopVideo()
		time.Sleep(300 * time.Millisecond)
	}

	// 3. ПОТОМ запускаем видео на сервере — Bridge FFmpeg шлёт RTP, visitor пересылает в proxy video_sudp
	logrus.Infof("🎥 [VIDEO] Запуск video capture (mode=%s, client_port=%d)", mode, request.ClientPort)
	if err := vw.usbClient.StartVideo(request); err != nil {
		vw.gstreamerService.Disconnect()
		logrus.Errorf("Ошибка запуска видео: %v", err)
		fyne.Do(func() {
			vw.statusLabel.SetText(fmt.Sprintf(i18n.Current.ErrorVideoStart, err))
		})
		return
	}

	logrus.Infof("✅ Видео захват запущен (mode=%s, UDP порт %d)", mode, clientPort)

	vw.isStreaming = true
	fyne.Do(func() {
		vw.updateButtons()
	})
	vw.updateStatus()

	// Проверяем статус мыши сразу после запуска видео
	vw.checkMouseConnected()
}

// connectToGStreamerWithRetries пытается подключиться к GStreamer с множественными попытками.
func (vw *VideoWidget) connectToGStreamerWithRetries() bool {
	const maxRetries = 5
	const retryDelay = 3 * time.Second

	for attempt := 1; attempt <= maxRetries; attempt++ {
		fyne.Do(func() {
			vw.statusLabel.SetText(fmt.Sprintf(i18n.Current.ConnectingRTP, attempt, maxRetries))
		})
		logrus.Debugf("🔄 Попытка подключения к RTP video через GStreamer #%d/%d", attempt, maxRetries)

		err := vw.gstreamerService.ConnectToRTP()
		if err != nil {
			logrus.Errorf("❌ Попытка подключения #%d неудачна: %v", attempt, err)

			if attempt == maxRetries {
				// Последняя попытка неудачна
				fyne.Do(func() {
					vw.statusLabel.SetText(fmt.Sprintf(i18n.Current.VideoLaunchFailed, maxRetries))
				})
				logrus.Errorf("❌ Все %d попыток подключения к RTP/UDP неудачны", maxRetries)
				return false
			}

			// Ждем перед следующей попыткой
			logrus.Debugf("⏳ Ожидание %v перед следующей попыткой...", retryDelay)
			time.Sleep(retryDelay)
			continue
		}

		// Успешное подключение
		vw.isGStreamerConnected = true
		fyne.Do(func() {
			vw.statusLabel.SetText(i18n.Current.VideoActive)
		})
		logrus.Info("✅ Video pipeline ready")
		return true
	}

	// Этот код никогда не должен выполниться, но на всякий случай
	return false
}

// updateGStreamerStats обновляет статистику RTP/UDP потока
func (vw *VideoWidget) updateGStreamerStats() {
	if vw.gstreamerService == nil {
		return
	}

	stats := vw.gstreamerService.GetStats()
	connected := stats["connected"].(bool)
	framesDropped := stats["frames_dropped"].(int64)
	lowLatencyMode := stats["low_latency_mode"].(bool)

	gstreamerInfo := fmt.Sprintf("GStreamer: %s | %s: %d | %s: %s",
		map[bool]string{true: "✅", false: "❌"}[connected],
		i18n.Current.FramesDropped,
		framesDropped,
		i18n.Current.LowLatencyMode,
		map[bool]string{true: "✅", false: "❌"}[lowLatencyMode])

	fyne.Do(func() {
		vw.infoLabel.SetText(gstreamerInfo)
	})
}

// Добавляем поле в VideoWidget
type VideoWidgetWithGStreamer struct {
	*VideoWidget
	gstreamerService     *service.GStreamerService
	isGStreamerConnected bool
}
