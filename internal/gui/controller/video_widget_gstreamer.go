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
func NewVideoWidgetGStreamer(parent fyne.Window, usbClient *api.USBClient, videoClient service.VideoClient, updateStatus func()) *VideoWidget {
	vw := &VideoWidget{
		parentWindow:     parent,
		usbClient:        usbClient,
		videoClient: videoClient,
		updateStatus:     updateStatus,
		isStreaming:      false,
		frameDecoder:     media.NewFrameDecoder(),
	}
	if usbClient != nil {
		usbClient.SetCursorUpdateHandler(vw.handleRemoteCursorUpdate)
	}

	vw.createInterface()
	if parent != nil {
		vw.SetParentWindow(parent)
	}
	vw.startVideoOpsLoop()
	vw.setupGStreamerCallbacks()
	return vw
}

// setupGStreamerCallbacks настраивает callbacks для GStreamer
func (vw *VideoWidget) setupGStreamerCallbacks() {
	if vw.videoClient == nil {
		logrus.Warn("⚠️ video service not initialized")
		return
	}

	// Callback для получения видео кадров
	vw.videoClient.SetOnFrameReceived(func(frame image.Image) {
		vw.handleVideoFrame(frame)
	})

	// Callback для изменения состояния соединения
	vw.videoClient.SetOnStateChanged(func(state string) {
		vw.handleGStreamerStateChange(state)
	})

	// Callback для ошибок
	vw.videoClient.SetOnError(func(err error) {
		logrus.Errorf("❌ [Video] stream error: %v", err)
		fyne.Do(func() {
			vw.statusLabel.SetText(fmt.Sprintf(i18n.Current.GStreamerError, err))
		})
	})
}

// handleGStreamerStateChange обрабатывает изменение состояния GStreamer
func (vw *VideoWidget) handleGStreamerStateChange(state string) {
	logrus.Infof("🎬 [VideoTrace #%d] video state=%s", vw.videoTraceID.Load(), state)
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
			logrus.Warnf("⚠️ [VideoTrace #%d] video EOS first_frame=%v first_paint=%v", traceID, firstFrameNs != 0, firstPaintNs != 0)
			vw.infoLabel.SetText("❌ " + i18n.Current.GStreamerEndOfStream)
		}
	})
}

// handleVideoStartWithParamsGStreamer обновлённая версия для GStreamer (новый UDP протокол)
func (vw *VideoWidget) handleVideoStartWithParamsGStreamer(request *models.VideoStartRequest) {
	vw.enableVSync = request.EnableVSync
	fyne.Do(func() {
		vw.statusLabel.SetText(i18n.Current.StartingVideoCapture)
	})
	traceID := vw.beginVideoTrace(fmt.Sprintf("mode=%s device=%s", request.VideoMode, request.VideoDevice))
	logrus.Infof("🎯 [VideoTrace #%d] preparing video start", traceID)
	request.TraceID = vw.currentVideoTraceLabel()
	request.ShowMouse = vw.showMouseCursor

	if vw.videoClient != nil {
		if cfg := vw.videoClient.GetConfig(); cfg != nil {
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

	if vw.videoClient != nil {
		vw.videoClient.SetAutoReconnect(false)
		vw.videoClient.SetMaxReconnectAttempts(1)
		vw.videoClient.SetVideoMode(request.VideoMode)
		vw.videoClient.SetExpectedVideoSize(request.VideoWidth, request.VideoHeight)
		if err := vw.videoClient.Disconnect(); err != nil {
			logrus.Warnf("⚠️ Ошибка отключения локального потока перед новым стартом: %v", err)
		}
		// No sleep needed: Disconnect() is synchronous on all platforms
		// (Android: waits on processDone channel; Linux/Darwin: kills process and waits).
	}
	if vw.usbClient != nil && vw.isStreaming {
		// Only stop+flush when video is actually running (restart-pending path).
		// On fresh connect isStreaming=false and the server is already idle — skipping
		// the stop call avoids a redundant round-trip and the 300 ms flush sleep.
		if err := vw.usbClient.StopVideo(); err != nil {
			logrus.Debugf("stop stale remote video before restart: %v", err)
		} else {
			// Brief pause to let the server flush its encoder pipeline and prevent
			// stale RTP packets from confusing the new GStreamer session.
			time.Sleep(300 * time.Millisecond)
		}
	}

	clientPort := models.DefaultVideoUDPPort
	transportKind := "tailscale-direct-udp"
	if vw.frpService != nil && vw.frpService.IsRunning() {
		_, clientPort, _ = vw.frpService.GetServerPorts()
		transportKind = "frp-video_sudp"
		request.ClientHost = "127.0.0.1"
		if vw.videoClient != nil && vw.videoClient.GetConfig().VideoProtocol != models.VideoProtocolMoonlight {
			vw.videoClient.UpdateHost("127.0.0.1")
		}
	} else {
		preferredPort := clientPort
		if cfg := vw.videoClient.GetConfig(); cfg != nil && cfg.VideoUDPPort > 0 {
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
		if vw.videoClient != nil && vw.videoClient.GetConfig().VideoProtocol != models.VideoProtocolMoonlight {
			vw.videoClient.UpdateVideoPort(clientPort)
			vw.videoClient.UpdateVideoUDPPort(clientPort)
			vw.videoClient.UpdateHost("0.0.0.0")
		}
	}
	request.ClientPort = clientPort
	request.CapturePixelFormat = "pkt_size=1200"

	mode := request.VideoMode
	if mode == "" {
		mode = models.VideoModeH264
	}

	if vw.frpService == nil {
		if vw.tailscaleService != nil && vw.tailscaleVideoEnabled {
			vw.tailscaleService.SetVideoRelayTraceID(request.TraceID)
			systemIP := vw.tailscaleService.GetSystemTailscaleIP()
			if systemIP != "" {
				request.ClientHost = systemIP
				if vw.videoClient != nil && vw.videoClient.GetConfig().VideoProtocol != models.VideoProtocolMoonlight {
					vw.videoClient.UpdateHost(systemIP)
				}
				logrus.Infof("🚀 [Tailscale/SystemStack] SUCCESS: Found system stack IP %s. Connecting directly...", systemIP)
			} else {
				logrus.Warn("⚠️ [Tailscale/SystemStack] No system Tailscale IP found, trying userspace relay...")
				actualPort, err := vw.tailscaleService.EnsureVideoUDPRelay(clientPort)
				if err == nil {
					clientPort = actualPort
					request.ClientPort = actualPort
					if vw.videoClient != nil {
						vw.videoClient.UpdateVideoPort(clientPort)
						vw.videoClient.UpdateVideoUDPPort(clientPort)
					}
					tailIP, _ := vw.tailscaleService.TailnetIPv4(context.Background())
					request.ClientHost = tailIP
					if vw.videoClient != nil && vw.videoClient.GetConfig().VideoProtocol != models.VideoProtocolMoonlight {
						vw.videoClient.UpdateHost("127.0.0.1")
					}
				}
			}
		}

		if request.ClientHost == "" && vw.usbClient != nil {
			agentHost := vw.usbClient.GetBaseURL()
			if strings.Contains(agentHost, "://") {
				hostPart := strings.Split(strings.Split(agentHost, "://")[1], ":")[0]
				// Resolve the client's local IP that can reach the bridge, whether via
				// LAN (192.168.x.x) or Tailscale (100.x.x.x).
				if hostPart != "" && !strings.HasPrefix(hostPart, "127.") {
					localIP := service.GetLocalIPForTarget(hostPart)
					if localIP != "" {
						request.ClientHost = localIP
						if vw.videoClient != nil && vw.videoClient.GetConfig().VideoProtocol != models.VideoProtocolMoonlight {
							vw.videoClient.UpdateHost(localIP)
						}
						logrus.Infof("🎯 [VideoRoute] Resolved local client IP %s for bridge host %s", localIP, hostPart)
					}
				}
			}
		}

		if request.ClientHost == "" {
			request.ClientHost = "127.0.0.1"
			logrus.Warn("⚠️ [VideoRoute] Could not determine client IP, falling back to 127.0.0.1")
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
		transportKind, vw.videoClient.GetBindHost(), clientPort,
		request.ClientHost, request.ClientPort,
	)

	// Run HID auto-connect in parallel with video server start — they are independent
	// subsystems (USB gadget vs V4L2 capture). This saves ~2s on fresh connect.
	hidDone := make(chan error, 1)
	go func() {
		logrus.Debug("⌨️🖱️ [VIDEO] HID auto-connect running in parallel with video start...")
		hidDone <- vw.ensureControlHIDDevices()
	}()

	logrus.Infof("🎥 [VIDEO %s] start capture mode=%s client=%s:%d", request.TraceID, mode, request.ClientHost, request.ClientPort)

	if err := vw.usbClient.StartVideo(request); err != nil {
		logrus.Errorf("❌ Error starting video on server: %v", err)
		fyne.Do(func() {
			vw.statusLabel.SetText(fmt.Sprintf(i18n.Current.ErrorVideoStart, err))
		})
		return
	}

	if !vw.connectToGStreamerWithRetries() {
		logrus.Error("❌ Failed to start video pipeline")
		_ = vw.usbClient.StopVideo()
		fyne.Do(func() {
			vw.statusLabel.SetText(i18n.Current.ErrorVideoStart)
		})
		return
	}

	// Wait for HID — by the time video starts streaming it's usually already done.
	if err := <-hidDone; err != nil {
		logrus.Errorf("❌ [VIDEO] HID auto-connect failed: %v", err)
		// Non-fatal: video is streaming; checkMouseConnected() below will handle input setup.
	}

	logrus.Infof("✅ Video capture initiated (mode=%s, UDP port %d)", mode, clientPort)
	if vw.tailscaleService != nil {
		relayInfo := vw.tailscaleService.VideoRelayDebugInfo(request.ClientHost)
		logrus.Infof("🎬 [VIDEO %s] client relay after start: %s", request.TraceID, relayInfo)
		connMode := vw.tailscaleService.PeerConnectionMode(request.ClientHost)
		if strings.HasPrefix(connMode, "derp:") {
			logrus.Warnf("⚠️ [Tailscale] Video goes through DERP-relay (%s) — this is OK, but latency is higher.", connMode)
		} else if strings.HasPrefix(connMode, "direct:") {
			logrus.Infof("✅ [Tailscale] Video goes P2P direct (%s) — optimal.", connMode)
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

	if err := vw.videoClient.ConnectToRTP(); err != nil {
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
	if vw.videoClient == nil || !vw.isGStreamerConnected {
		return
	}

	stats := vw.videoClient.GetStats()
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
