package controller

import (
	"context"
	"fmt"
	"image"
	"net"
	"strings"
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
	logrus.Infof("🎬 [VideoTrace #%d] GStreamer state=%s", vw.videoTraceID.Load(), state)
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
	request.TraceID = vw.currentVideoTraceLabel()

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

	if vw.gstreamerService != nil {
		vw.gstreamerService.SetAutoReconnect(false)
		vw.gstreamerService.SetMaxReconnectAttempts(1)
		vw.gstreamerService.SetVideoMode(request.VideoMode)
		vw.gstreamerService.SetExpectedVideoSize(request.VideoWidth, request.VideoHeight)
		if err := vw.gstreamerService.Disconnect(); err != nil {
			logrus.Warnf("⚠️ Ошибка отключения локального потока перед новым стартом: %v", err)
		}
		time.Sleep(150 * time.Millisecond)
	}
	if vw.usbClient != nil {
		if err := vw.usbClient.StopVideo(); err != nil {
			logrus.Debugf("stop stale remote video before restart: %v", err)
		} else {
			// Ждём достаточно времени, чтобы сервер успел убить ffmpeg/gst-launch
			// (StopStreaming делает: SIGINT → sleep 500ms → SIGKILL → Wait).
			// 200ms было недостаточно: новый StartStreaming мог прийти раньше,
			// чем сервер сбросил isStreaming, и получал "already running" без
			// фактического старта нового ffmpeg.
			time.Sleep(700 * time.Millisecond)
		}
	}

	clientPort := models.DefaultVideoUDPPort
	transportKind := "tailscale-direct-udp"
	if vw.frpService != nil && vw.frpService.IsRunning() {
		_, clientPort, _ = vw.frpService.GetServerPorts()
		transportKind = "frp-video_sudp"
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

	var systemIP string
	if vw.frpService == nil && vw.tailscaleService != nil {
		vw.tailscaleService.SetVideoRelayTraceID(request.TraceID)
		
		// Nuclear Mode for all OS: try system Tailscale IP first
		systemIP = vw.tailscaleService.GetSystemTailscaleIP()
		if systemIP != "" {
			request.ClientHost = systemIP
			logrus.Infof("🚀 [Tailscale/NuclearMode] SUCCESS: Found system stack IP %s. Connecting directly...", systemIP)
		} else {
			logrus.Warn("⚠️ [Tailscale/NuclearMode] FAILED: No system Tailscale IP found (is tailscaled running?). Falling back to userspace relay...")
			
			// Fallback to userspace relay
			actualPort, err := vw.tailscaleService.EnsureVideoUDPRelay(clientPort)
			if err != nil {
				logrus.Errorf("❌ Не удалось запустить Tailscale video relay: %v", err)
				fyne.Do(func() {
					vw.statusLabel.SetText(fmt.Sprintf(i18n.Current.ErrorVideoStart, err))
				})
				return
			}
			clientPort = actualPort
			// Обновляем порт в GStreamer, так как релей теперь пересылает на этот локальный порт
			if vw.gstreamerService != nil {
				vw.gstreamerService.UpdateVideoPort(clientPort)
				vw.gstreamerService.UpdateVideoUDPPort(clientPort)
			}

			tailIP, err := vw.tailscaleService.TailnetIPv4(context.Background())
			if err != nil {
				logrus.Errorf("❌ Не удалось определить Tailscale IP клиента для видео: %v", err)
				fyne.Do(func() {
					vw.statusLabel.SetText(fmt.Sprintf(i18n.Current.ErrorVideoStart, err))
				})
				return
			}
			request.ClientHost = tailIP
		}
		logrus.Infof("🎬 [VIDEO %s] tailscale transport target=%s:%d relay=%s", request.TraceID, request.ClientHost, request.ClientPort, vw.tailscaleService.VideoRelayDebugInfo(request.ClientHost))
	}
	logrus.Infof("🧭 [VideoRoute %s] client-request mode=%s device=%s capture_pixel_format=%q size=%dx%d fps=%d bitrate=%s transport=%s listen_bind=%s:%d send_target=%s:%d",
		request.TraceID,
		mode,
		request.VideoDevice,
		request.CapturePixelFormat,
		request.VideoWidth,
		request.VideoHeight,
		request.VideoFPS,
		request.VideoBitrate,
		transportKind,
		vw.gstreamerService.GetBindHost(),
		clientPort,
		request.ClientHost,
		request.ClientPort,
	)

	// 1. Сначала поднимаем HID и ждём завершения переконфигурации gadget.
	logrus.Debug("⌨️🖱️ [VIDEO] Проверка и автоподключение HID перед стартом видео...")
	if err := vw.ensureControlHIDDevices(); err != nil {
		logrus.Errorf("❌ [VIDEO] HID auto-connect before video failed: %v", err)
		fyne.Do(func() {
			vw.statusLabel.SetText(fmt.Sprintf(i18n.Current.ErrorVideoStart, err))
		})
		return
	}

	// 3. Запускаем GStreamer и видео на сервере
	logrus.Infof("🎥 [VIDEO %s] start capture mode=%s client=%s:%d", request.TraceID, mode, request.ClientHost, request.ClientPort)
	
	// Hole Punching: посылаем UDP пакет агенту ПЕРЕД запуском GStreamer
	if request.ClientHost != "127.0.0.1" && request.ClientHost != "localhost" {
		agentHost := vw.usbClient.GetBaseURL()
		if strings.Contains(agentHost, "://") {
			parts := strings.Split(strings.Split(agentHost, "://")[1], ":")
			agentIP := parts[0]
			
			// Пытаемся отправить пакет именно с того порта, на который ждем видео
			laddr, _ := net.ResolveUDPAddr("udp4", fmt.Sprintf(":%d", clientPort))
			raddr, _ := net.ResolveUDPAddr("udp4", fmt.Sprintf("%s:%d", agentIP, clientPort))
			
			// Используем ListenUDP + WriteTo, чтобы контролировать локальный порт
			conn, err := net.ListenUDP("udp4", laddr)
			if err == nil {
				logrus.Infof("🎯 [Tailscale/NuclearMode] Punching hole from %s to agent %s:%d", conn.LocalAddr().String(), agentIP, clientPort)
				_, _ = conn.WriteTo([]byte("PUNCH"), raddr)
				// Даем системе время зафиксировать соединение
				time.Sleep(50 * time.Millisecond)
				conn.Close()
				// Небольшая пауза, чтобы ОС освободила порт для GStreamer
				time.Sleep(50 * time.Millisecond)
			} else {
				logrus.Warnf("⚠️ [Tailscale/NuclearMode] Could not bind to %d for punching: %v", clientPort, err)
			}
		}
	}

	if !vw.connectToGStreamerWithRetries() {
		logrus.Error("❌ Не удалось запустить GStreamer")
		fyne.Do(func() {
			vw.statusLabel.SetText(i18n.Current.ErrorVideoStart)
		})
		return
	}

	if err := vw.usbClient.StartVideo(request); err != nil {
		vw.gstreamerService.Disconnect()
		logrus.Errorf("❌ Ошибка запуска видео на сервере: %v", err)
		fyne.Do(func() {
			vw.statusLabel.SetText(fmt.Sprintf(i18n.Current.ErrorVideoStart, err))
		})
		return
	}

	logrus.Infof("✅ Видео захват инициирован (mode=%s, UDP порт %d)", mode, clientPort)
	if vw.tailscaleService != nil {
		relayInfo := vw.tailscaleService.VideoRelayDebugInfo(request.ClientHost)
		logrus.Infof("🎬 [VIDEO %s] client relay after start: %s", request.TraceID, relayInfo)

		// Проверяем P2P vs DERP — важно для диагностики видео через Android/userspace Tailscale
		connMode := vw.tailscaleService.PeerConnectionMode(request.ClientHost)
		if strings.HasPrefix(connMode, "derp:") {
			logrus.Warnf("⚠️ [Tailscale] Видео идёт через DERP-релей (%s) — это OK, но latency выше. Для P2P нужен открытый UDP между устройствами.", connMode)
		} else if strings.HasPrefix(connMode, "direct:") {
			logrus.Infof("✅ [Tailscale] Видео идёт P2P direct (%s) — оптимально.", connMode)
		} else {
			logrus.Infof("ℹ️ [Tailscale] Режим соединения: %s (возможно, сервер не в списке peers этого клиента)", connMode)
		}
	}

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
	fyne.Do(func() {
		vw.statusLabel.SetText(fmt.Sprintf(i18n.Current.ConnectingRTP, 1, 1))
	})
	logrus.Debug("🔄 Single-attempt RTP video pipeline connect")

	if err := vw.gstreamerService.ConnectToRTP(); err != nil {
		logrus.Errorf("❌ RTP pipeline connect failed: %v", err)
		fyne.Do(func() {
			vw.statusLabel.SetText(fmt.Sprintf(i18n.Current.VideoLaunchFailed, 1))
		})
		return false
	}

	vw.isGStreamerConnected = true
	fyne.Do(func() {
		vw.statusLabel.SetText(i18n.Current.VideoActive)
	})
	logrus.Info("✅ Video pipeline ready")
	return true
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
