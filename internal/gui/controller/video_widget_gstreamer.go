package controller

import (
	"context"
	"fmt"
	"image"
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
func NewVideoWidgetGStreamer(parent fyne.Window, usbClient *api.USBClient, gstreamerService *service.GStreamerService, updateStatus func()) *VideoWidget {
	vw := &VideoWidget{
		parentWindow:     parent,
		usbClient:        usbClient,
		gstreamerService: gstreamerService,
		updateStatus:     updateStatus,
		isStreaming:      false,
		frameDecoder:     media.NewFrameDecoder(),
	}
	if usbClient != nil {
		usbClient.SetCursorUpdateHandler(vw.handleRemoteCursorUpdate)
	}

	vw.createInterface()
	vw.startVideoOpsLoop()
	vw.setupGStreamerCallbacks()
	return vw
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

// handleVideoStartWithParamsGStreamer обновлённая версия для GStreamer (новый UDP протокол)
func (vw *VideoWidget) handleVideoStartWithParamsGStreamer(request *models.VideoStartRequest) {
	fyne.Do(func() {
		vw.statusLabel.SetText(i18n.Current.StartingVideoCapture)
	})
	traceID := vw.beginVideoTrace(fmt.Sprintf("mode=%s device=%s", request.VideoMode, request.VideoDevice))
	logrus.Infof("🎯 [VideoTrace #%d] preparing GStreamer/video start", traceID)
	request.TraceID = vw.currentVideoTraceLabel()
	request.ShowMouse = vw.showMouseCursor

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
			time.Sleep(700 * time.Millisecond)
		}
	}

	clientPort := models.DefaultVideoUDPPort
	transportKind := "tailscale-direct-udp"
	if vw.frpService != nil && vw.frpService.IsRunning() {
		_, clientPort, _ = vw.frpService.GetServerPorts()
		transportKind = "frp-video_sudp"
		request.ClientHost = "127.0.0.1"
		if vw.gstreamerService != nil {
			vw.gstreamerService.UpdateHost("127.0.0.1")
		}
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
			vw.gstreamerService.UpdateHost("0.0.0.0")
		}
	}
	request.ClientPort = clientPort
	request.CapturePixelFormat = "pkt_size=1200"

	mode := request.VideoMode
	if mode == "" {
		mode = models.VideoModeH264
	}

	if vw.frpService == nil {
		if vw.tailscaleService != nil {
			vw.tailscaleService.SetVideoRelayTraceID(request.TraceID)
			systemIP := vw.tailscaleService.GetSystemTailscaleIP()
			if systemIP != "" {
				request.ClientHost = systemIP
				if vw.gstreamerService != nil {
					vw.gstreamerService.UpdateHost(systemIP)
				}
				logrus.Infof("🚀 [Tailscale/SystemStack] SUCCESS: Found system stack IP %s. Connecting directly...", systemIP)
			} else {
				logrus.Warn("⚠️ [Tailscale/SystemStack] No system Tailscale IP found, trying userspace relay...")
				actualPort, err := vw.tailscaleService.EnsureVideoUDPRelay(clientPort)
				if err == nil {
					clientPort = actualPort
					request.ClientPort = actualPort
					if vw.gstreamerService != nil {
						vw.gstreamerService.UpdateVideoPort(clientPort)
						vw.gstreamerService.UpdateVideoUDPPort(clientPort)
					}
					tailIP, _ := vw.tailscaleService.TailnetIPv4(context.Background())
					request.ClientHost = tailIP
					if vw.gstreamerService != nil {
						vw.gstreamerService.UpdateHost("127.0.0.1")
					}
				}
			}
		}

		if request.ClientHost == "" && vw.usbClient != nil {
			agentHost := vw.usbClient.GetBaseURL()
			if strings.Contains(agentHost, "://") {
				hostPart := strings.Split(strings.Split(agentHost, "://")[1], ":")[0]
				if strings.HasPrefix(hostPart, "100.") {
					localIP := service.GetLocalIPForTarget(hostPart)
					if localIP != "" {
						request.ClientHost = localIP
						if vw.gstreamerService != nil {
							vw.gstreamerService.UpdateHost(localIP)
						}
						logrus.Infof("🎯 [VideoRoute] Resolved local Tailscale IP %s from agent host %s", localIP, hostPart)
					}
				}
			}
		}

		if request.ClientHost == "" {
			request.ClientHost = "127.0.0.1"
			logrus.Warn("⚠️ [VideoRoute] Could not determine Tailscale IP, falling back to 127.0.0.1")
		}
	}

	if vw.tailscaleService != nil && vw.usbClient != nil {
		agentHost := vw.usbClient.GetBaseURL()
		if strings.Contains(agentHost, "://") {
			parts := strings.Split(strings.Split(agentHost, "://")[1], ":")
			agentIP := parts[0]
			for i := 0; i < 3; i++ {
				vw.tailscaleService.PunchVideoHole(agentIP, clientPort)
				if i < 2 {
					time.Sleep(50 * time.Millisecond)
				}
			}
		}
		logrus.Infof("🎬 [VIDEO %s] tailscale transport target=%s:%d relay=%s", request.TraceID, request.ClientHost, request.ClientPort, vw.tailscaleService.VideoRelayDebugInfo(request.ClientHost))
	}

	logrus.Infof("🧭 [VideoRoute %s] client-request mode=%s device=%s capture_pixel_format=%q size=%dx%d fps=%d bitrate=%s transport=%s listen_bind=%s:%d send_target=%s:%d",
		request.TraceID, mode, request.VideoDevice, request.CapturePixelFormat,
		request.VideoWidth, request.VideoHeight, request.VideoFPS, request.VideoBitrate,
		transportKind, vw.gstreamerService.GetBindHost(), clientPort,
		request.ClientHost, request.ClientPort,
	)

	logrus.Debug("⌨️🖱️ [VIDEO] Проверка и автоподключение HID перед стартом видео...")
	if err := vw.ensureControlHIDDevices(); err != nil {
		logrus.Errorf("❌ [VIDEO] HID auto-connect before video failed: %v", err)
		fyne.Do(func() {
			vw.statusLabel.SetText(fmt.Sprintf(i18n.Current.ErrorVideoStart, err))
		})
		return
	}

	logrus.Infof("🎥 [VIDEO %s] start capture mode=%s client=%s:%d", request.TraceID, mode, request.ClientHost, request.ClientPort)

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
		connMode := vw.tailscaleService.PeerConnectionMode(request.ClientHost)
		if strings.HasPrefix(connMode, "derp:") {
			logrus.Warnf("⚠️ [Tailscale] Видео идёт через DERP-релей (%s) — это OK, но latency выше.", connMode)
		} else if strings.HasPrefix(connMode, "direct:") {
			logrus.Infof("✅ [Tailscale] Видео идёт P2P direct (%s) — оптимально.", connMode)
		}
	}

	vw.isStreaming = true
	fyne.Do(func() {
		vw.updateButtons()
	})
	vw.updateStatus()
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
	if vw.gstreamerService == nil || !vw.isGStreamerConnected {
		return
	}

	stats := vw.gstreamerService.GetStats()
	if stats == nil {
		return
	}

	frameCount, _ := stats["frame_count"].(uint64)
	lastFrame, _ := stats["last_frame_time"].(time.Time)

	if frameCount > 0 && !lastFrame.IsZero() {
		vw.frameMutex.Lock()
		vw.frameCount = int64(frameCount)
		vw.lastFrameTime = lastFrame
		vw.frameMutex.Unlock()
	}
}
