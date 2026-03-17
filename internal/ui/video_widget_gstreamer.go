package ui

import (
	"fmt"
	"image"
	"time"

	"usbridge-client/internal/api"
	"usbridge-client/internal/models"
	"usbridge-client/internal/service"
	"usbridge-client/internal/ui/i18n"

	"fyne.io/fyne/v2"
	"github.com/sirupsen/logrus"
)

// NewVideoWidgetGStreamer создает новый виджет видео с GStreamer
func NewVideoWidgetGStreamer(usbClient *api.USBClient, gstreamerService *service.GStreamerService, updateStatus func()) *VideoWidget {
	vw := &VideoWidget{
		usbClient:          usbClient,
		gstreamerService:   gstreamerService,
		updateStatus:       updateStatus,
		isStreaming:        false,
		frameDecoder:       NewVideoFrameDecoder(),
	}

	vw.createInterface()
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
			vw.infoLabel.SetText("❌ " + i18n.Current.GStreamerEndOfStream)
		}
	})
}

// handleVideoStartWithParams обновлённая версия для GStreamer (новый UDP протокол)
func (vw *VideoWidget) handleVideoStartWithParamsGStreamer(request *models.VideoStartRequest) {
	fyne.Do(func() {
		vw.statusLabel.SetText(i18n.Current.StartingVideoCapture)
		vw.startBtn.Disable()
	})

	// Если видео уже запущено, значит это переключение устройства - переподключаемся
	wasStreaming := vw.isStreaming
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

	// Сбрасываем reconnect счетчик и включаем autoReconnect при новом запуске
	if vw.gstreamerService != nil {
		vw.gstreamerService.SetAutoReconnect(true)
		vw.gstreamerService.SetMaxReconnectAttempts(5)
	}

	// Порт приёма UDP (статический DefaultVideoUDPPort, proxy при FRP, прямой при локальном)
	clientPort := models.DefaultVideoUDPPort
	if cfg := vw.gstreamerService.GetConfig(); cfg != nil && cfg.VideoUDPPort > 0 {
		clientPort = cfg.VideoUDPPort
	}
	request.ClientPort = clientPort

	// 1. Запускаем GStreamer: udpsrc RTP на clientPort (при FRP — proxy слушает, Bridge visitor шлёт)
	logrus.Infof("🎬 [VIDEO] Шаг 1: Запуск GStreamer (udpsrc RTP port=%d)...", clientPort)
	if !vw.connectToGStreamerWithRetries() {
		logrus.Error("❌ Не удалось запустить GStreamer")
		fyne.Do(func() {
			vw.startBtn.Enable()
			vw.statusLabel.SetText("❌ " + i18n.Current.VideoLaunchFailed)
		})
		return
	}

	// 1.5 Ждём привязки udpsrc к порту (gst-launch запускается асинхронно, POST — только после готовности приёма)
	time.Sleep(500 * time.Millisecond)
	logrus.Infof("🔗 [VIDEO] udpsrc port=%d готов к приёму, запуск сервера...", clientPort)

	// 1.6 Останавливаем предыдущий стрим на сервере (если был) — иначе FFmpeg может не перезапуститься
	logrus.Info("🛑 [VIDEO] Шаг 1.6: POST /api/video/stop (сброс предыдущего стрима)")
	_ = vw.usbClient.StopVideo()
	time.Sleep(2 * time.Second)

	// 2. ПОТОМ запускаем видео на сервере — Bridge FFmpeg шлёт RTP, visitor пересылает в proxy video_sudp
	logrus.Infof("🎥 [VIDEO] Шаг 2: POST /api/video/start (client_port=%d)", request.ClientPort)
	if err := vw.usbClient.StartVideo(request); err != nil {
		vw.gstreamerService.Disconnect()
		logrus.Errorf("Ошибка запуска видео: %v", err)
		fyne.Do(func() {
			vw.statusLabel.SetText(fmt.Sprintf(i18n.Current.ErrorVideoStart, err))
			vw.startBtn.Enable()
		})
		return
	}

	logrus.Infof("✅ Видео захват запущен (UDP порт %d)", clientPort)

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
		logrus.Infof("🔄 Попытка подключения к UDP H.264 через GStreamer #%d/%d", attempt, maxRetries)

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
			logrus.Infof("⏳ Ожидание %v перед следующей попыткой...", retryDelay)
			time.Sleep(retryDelay)
			continue
		}

		// Успешное подключение
		vw.isGStreamerConnected = true
		fyne.Do(func() {
			vw.statusLabel.SetText(i18n.Current.VideoActive)
		})
		logrus.Info("✅ GStreamer UDP H.264 подключение установлено успешно")
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
	gstreamerService      *service.GStreamerService
	isGStreamerConnected  bool
}
