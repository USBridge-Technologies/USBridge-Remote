//go:build !android && !darwin && !windows
// +build !android,!darwin,!windows

package service

import (
	"bytes"
	"fmt"
	"image"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"usbridge-client/internal/models"

	"github.com/sirupsen/logrus"
	"github.com/tinyzimmer/go-gst/gst"
	"github.com/tinyzimmer/go-gst/gst/app"
)

// GStreamerService сервис для работы с GStreamer H264 потоком
type GStreamerService struct {
	config    *models.AppConfig
	videoMode string

	// GStreamer pipeline
	pipeline               *gst.Pipeline
	appsink                *app.Sink
	nativeFullscreenCmd    *exec.Cmd
	nativeFullscreenActive bool
	jpegCandidateIndex     int
	lastJPEGPipeline       string
	failedJPEGPipelines    map[string]bool
	missingJPEGElements    map[string]bool
	expectedWidth          int
	expectedHeight         int
	detectedGPUVendor      string

	// Состояние
	isConnected    bool
	isConnecting   bool
	isReconnecting bool

	// Автоматическое переподключение
	autoReconnect        bool
	reconnectAttempts    int
	maxReconnectAttempts int
	manualDisconnect     bool

	// Канал для неблокирующей передачи кадров
	frameChan chan videoFramePacket
	stopChan  chan struct{}

	// Флаги для управления горутинами
	frameProcessorRunning bool
	monitorRunning        bool

	// Статистика
	lastFrameTime  time.Time
	frameCount     int64
	framesDropped  int64
	latencyProfile videoLatencyProfile

	// Мьютексы
	mutex sync.RWMutex

	// Callbacks
	onFrameReceived func(image.Image)
	onStateChanged  func(string)
	onError         func(error)
}

// NewGStreamerService создает новый GStreamer сервис
func NewGStreamerService(config *models.AppConfig) *GStreamerService {
	gs := &GStreamerService{
		config:                config,
		autoReconnect:         true,
		reconnectAttempts:     0,
		maxReconnectAttempts:  5,
		videoMode:             models.VideoModeH264,
		frameChan:             make(chan videoFramePacket, 1), // Один кадр — минимальная задержка
		stopChan:              make(chan struct{}),
		failedJPEGPipelines:   make(map[string]bool),
		missingJPEGElements:   make(map[string]bool),
		frameProcessorRunning: false,
		monitorRunning:        false,
	}
	gs.detectedGPUVendor = detectLinuxGPUVendor()
	if gs.detectedGPUVendor != "" {
		logrus.Infof("🎮 [Linux] Detected GPU vendor: %s", gs.detectedGPUVendor)
	}

	return gs
}

// ConnectToRTP подключается к RTP H.264 потоку (UDP, новый протокол)
func (gs *GStreamerService) ConnectToRTP() error {
	gs.mutex.Lock()

	if gs.isConnecting || gs.isConnected {
		gs.mutex.Unlock()
		return fmt.Errorf("уже подключен или подключается")
	}

	gs.manualDisconnect = false
	gs.lastFrameTime = time.Time{}

	// Закрываем старый stopChan если есть
	if gs.stopChan != nil {
		select {
		case <-gs.stopChan: // Канал уже закрыт
		default:
			close(gs.stopChan) // Закрываем канал
		}
	}

	// Ждем остановки горутин если они еще работают
	for gs.frameProcessorRunning || gs.monitorRunning {
		gs.mutex.Unlock()
		time.Sleep(50 * time.Millisecond)
		gs.mutex.Lock()
	}

	// Пересоздаем stopChan для нового соединения
	gs.stopChan = make(chan struct{})

	// Запускаем новый обработчик кадров ОДИН РАЗ
	gs.frameProcessorRunning = true
	go gs.frameProcessor()

	gs.isConnecting = true
	gs.mutex.Unlock()

	logrus.Debugf("🔗 Подключение к RTP потоку mode=%s...", gs.videoMode)

	// Инициализируем GStreamer (gst.Init не возвращает ошибку)
	gst.Init(nil)

	maxPipelineAttempts := 1
	if gs.videoMode == models.VideoModeJPEGRTP {
		maxPipelineAttempts = len(gs.buildJPEGPipelineCandidates(udpPortFromConfig(gs.config)))
		if maxPipelineAttempts < 1 {
			maxPipelineAttempts = 1
		}
	}

	var connectErr error
	for attempt := 0; attempt < maxPipelineAttempts; attempt++ {
		// Создаем pipeline
		if err := gs.createPipeline(); err != nil {
			connectErr = fmt.Errorf("ошибка создания pipeline: %v", err)
			break
		}

		// Запускаем pipeline
		if err := gs.pipeline.SetState(gst.StatePlaying); err != nil {
			connectErr = fmt.Errorf("ошибка запуска pipeline: %v", err)
			if gs.pipeline != nil {
				gs.pipeline.SetState(gst.StateNull)
			}
			gs.pipeline = nil
			gs.appsink = nil
			if gs.videoMode == models.VideoModeJPEGRTP {
				gs.handleFailedJPEGPipeline(connectErr)
				continue
			}
			break
		}

		connectErr = nil
		break
	}
	if connectErr != nil {
		gs.mutex.Lock()
		gs.isConnecting = false
		gs.mutex.Unlock()
		return connectErr
	}

	// Запускаем мониторинг состояния ОДИН РАЗ
	gs.mutex.Lock()
	gs.monitorRunning = true
	gs.mutex.Unlock()
	go gs.monitorPipeline()

	gs.mutex.Lock()
	gs.isConnecting = false
	gs.isConnected = true
	gs.mutex.Unlock()

	logrus.Infof("✅ GStreamer RTP подключение установлено (mode=%s)", gs.videoMode)
	return nil
}

// ConnectToUDPViaPipe — pipe mode для FRP relay (Linux: fdsrc, пока заглушка)
func (gs *GStreamerService) ConnectToUDPViaPipe(pipeReader *os.File) error {
	_ = pipeReader
	return fmt.Errorf("UDP relay (pipe) пока не реализован на Linux, используйте прямое подключение")
}

// createPipeline создает GStreamer pipeline для RTP video (через FRP туннель)
func (gs *GStreamerService) createPipeline() error {
	udpPort := udpPortFromConfig(gs.config)
	bindHost := gs.config.VideoBindHost
	if bindHost == "" {
		bindHost = "127.0.0.1"
	}
	logrus.Debugf("📹 UDP порт приёма RTP video: %d (mode=%s)", udpPort, gs.videoMode)

	if gs.videoMode == models.VideoModeJPEGRTP {
		return gs.createJPEGPipeline(udpPort)
	}
	if gs.videoMode == models.VideoModeRawYUYV {
		return gs.createRawYUYVPipeline(udpPort)
	}

	// Linux: пытаемся аппаратные декодеры сначала, затем программный fallback.
	// Также даем варианты без h264parse, т.к. на некоторых дистрибутивах отсутствует gst-plugins-bad.
	base := fmt.Sprintf(
		"udpsrc address=%s port=%d buffer-size=131072 caps=\"application/x-rtp,media=video,encoding-name=H264,payload=96\" ! "+
			"rtpjitterbuffer latency=15 faststart-min-packets=1 drop-on-latency=true ! "+
			"rtph264depay ! ",
		bindHost, udpPort,
	)

	decoderPath := "videoconvert ! video/x-raw,format=RGBA ! appsink name=sink sync=false max-buffers=1 drop=true"
	pipelines := []struct {
		name string
		str  string
	}{
		{
			name: "vah264dec + h264parse (HW)",
			str:  base + "h264parse config-interval=-1 ! vah264dec ! " + decoderPath,
		},
		{
			name: "vaapih264dec + h264parse (HW)",
			str:  base + "h264parse config-interval=-1 ! vaapih264dec ! " + decoderPath,
		},
		{
			name: "v4l2h264dec + h264parse (HW)",
			str:  base + "h264parse config-interval=-1 ! v4l2h264dec capture-io-mode=dmabuf ! " + decoderPath,
		},
		{
			name: "nvh264dec + h264parse (HW)",
			str:  base + "h264parse config-interval=-1 ! nvh264dec ! " + decoderPath,
		},
		{
			name: "vah264dec без h264parse (HW)",
			str:  base + "vah264dec ! " + decoderPath,
		},
		{
			name: "vaapih264dec без h264parse (HW)",
			str:  base + "vaapih264dec ! " + decoderPath,
		},
		{
			name: "v4l2h264dec без h264parse (HW)",
			str:  base + "v4l2h264dec capture-io-mode=dmabuf ! " + decoderPath,
		},
		{
			name: "nvh264dec без h264parse (HW)",
			str:  base + "nvh264dec ! " + decoderPath,
		},
		{
			name: "avdec_h264 + h264parse (SW fallback)",
			str:  base + "h264parse config-interval=-1 ! avdec_h264 max-threads=0 ! " + decoderPath,
		},
		{
			name: "avdec_h264 без h264parse (SW fallback)",
			str:  base + "avdec_h264 max-threads=0 ! " + decoderPath,
		},
		{
			name: "openh264dec (SW fallback)",
			str:  base + "openh264dec ! " + decoderPath,
		},
		{
			name: "decodebin3 (generic fallback)",
			str:  base + "decodebin3 ! " + decoderPath,
		},
		{
			name: "decodebin (generic fallback)",
			str:  base + "decodebin ! " + decoderPath,
		},
	}

	var lastErr error
	for idx, candidate := range pipelines {
		logrus.Infof("🔄 [Linux] Pipeline попытка %d/%d: %s", idx+1, len(pipelines), candidate.name)
		logrus.Infof("🔧 [Linux] GStreamer pipeline: %s", candidate.str)

		pipeline, err := gst.NewPipelineFromString(candidate.str)
		if err != nil {
			lastErr = err
			logrus.Warnf("⚠️ [Linux] Pipeline недоступен (%s): %v", candidate.name, err)
			continue
		}

		gs.pipeline = pipeline
		if err := gs.attachAppsink(); err != nil {
			lastErr = err
			gs.pipeline.SetState(gst.StateNull)
			gs.pipeline = nil
			logrus.Warnf("⚠️ [Linux] appsink attach не удался (%s): %v", candidate.name, err)
			continue
		}

		logrus.Infof("✅ [Linux] GStreamer pipeline создан: %s", candidate.name)
		return nil
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("не найдено подходящих элементов GStreamer")
	}
	return fmt.Errorf("ошибка создания pipeline: %v", lastErr)
}

func (gs *GStreamerService) createRawYUYVPipeline(udpPort int) error {
	bindHost := gs.config.VideoBindHost
	if bindHost == "" {
		bindHost = "127.0.0.1"
	}
	width := gs.expectedWidth
	height := gs.expectedHeight
	if width <= 0 {
		width = gs.config.VideoWidth
	}
	if height <= 0 {
		height = gs.config.VideoHeight
	}
	if width <= 0 {
		width = 1280
	}
	if height <= 0 {
		height = 720
	}
	pipelineStr := fmt.Sprintf(
		"udpsrc address=%s port=%d buffer-size=4194304 caps=\"application/x-rtp,media=(string)video,clock-rate=(int)90000,encoding-name=(string)RAW,sampling=(string)YCbCr-4:2:2,depth=(string)8,width=(string)%d,height=(string)%d,colorimetry=(string)BT601-5,payload=(int)96\" ! "+
			"rtpjitterbuffer latency=5 faststart-min-packets=1 drop-on-latency=true ! "+
			"rtpvrawdepay ! video/x-raw,format=UYVY,width=%d,height=%d ! videoconvert ! video/x-raw,format=RGBA ! "+
			"appsink name=sink sync=false max-buffers=1 drop=true",
		bindHost, udpPort, width, height, width, height,
	)
	pipeline, err := gst.NewPipelineFromString(pipelineStr)
	if err != nil {
		return fmt.Errorf("ошибка создания RAW YUYV pipeline: %v", err)
	}
	gs.pipeline = pipeline
	if err := gs.attachAppsink(); err != nil {
		gs.pipeline.SetState(gst.StateNull)
		gs.pipeline = nil
		return fmt.Errorf("ошибка подключения appsink для RAW YUYV: %v", err)
	}
	logrus.Infof("✅ [Linux/RAW] GStreamer pipeline создан: %s", pipelineStr)
	return nil
}

func (gs *GStreamerService) createJPEGPipeline(udpPort int) error {
	pipelines := gs.buildJPEGPipelineCandidates(udpPort)

	var lastErr error
	for idx := range pipelines {
		candidateIdx := (gs.jpegCandidateIndex + idx) % len(pipelines)
		candidate := pipelines[candidateIdx]
		if gs.failedJPEGPipelines[candidate.name] {
			logrus.Debugf("⏭️ [Linux/JPEG] Pipeline пропущен после runtime-fail: %s", candidate.name)
			continue
		}
		if candidate.requiredElement != "" {
			if gs.missingJPEGElements[candidate.requiredElement] {
				logrus.Debugf("⏭️ [Linux/JPEG] Элемент уже помечен как отсутствующий: %s", candidate.requiredElement)
				continue
			}
			if gst.Find(candidate.requiredElement) == nil {
				gs.missingJPEGElements[candidate.requiredElement] = true
				logrus.Debugf("⏭️ [Linux/JPEG] Элемент недоступен в registry: %s", candidate.requiredElement)
				continue
			}
		}
		logrus.Infof("🔄 [Linux/JPEG] Pipeline попытка %d/%d: %s", idx+1, len(pipelines), candidate.name)
		logrus.Debugf("🔧 [Linux/JPEG] GStreamer pipeline: %s", candidate.str)

		pipeline, err := gst.NewPipelineFromString(candidate.str)
		if err != nil {
			lastErr = err
			logrus.Warnf("⚠️ [Linux/JPEG] Pipeline недоступен (%s): %v", candidate.name, err)
			continue
		}

		gs.pipeline = pipeline
		if err := gs.attachAppsink(); err != nil {
			lastErr = err
			gs.pipeline.SetState(gst.StateNull)
			gs.pipeline = nil
			logrus.Warnf("⚠️ [Linux/JPEG] appsink attach не удался (%s): %v", candidate.name, err)
			continue
		}

		gs.lastJPEGPipeline = candidate.name
		logrus.Infof("✅ [Linux/JPEG] GStreamer pipeline создан: %s", candidate.name)
		return nil
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("не найдено подходящих JPEG декодеров GStreamer")
	}
	return fmt.Errorf("ошибка создания JPEG pipeline: %v", lastErr)
}

// attachAppsink находит appsink в текущем pipeline и подключает callback для кадров.
func (gs *GStreamerService) attachAppsink() error {
	sinkElement, err := gs.pipeline.GetElementByName("sink")
	if err != nil {
		return fmt.Errorf("ошибка получения appsink: %v", err)
	}

	gs.appsink = app.SinkFromElement(sinkElement)
	gs.appsink.SetCallbacks(&app.SinkCallbacks{
		NewSampleFunc: func(sink *app.Sink) gst.FlowReturn {
			sample := sink.PullSample()
			if sample == nil {
				return gst.FlowEOS
			}

			gs.processSample(sample)
			return gst.FlowOK
		},
	})

	return nil
}

// processSample обрабатывает один sample
func (gs *GStreamerService) processSample(sample *gst.Sample) {
	producedAt := time.Now()
	// Получаем buffer
	buffer := sample.GetBuffer()
	if buffer == nil {
		return
	}

	// Получаем caps для определения размера кадра
	caps := sample.GetCaps()
	if caps == nil {
		return
	}

	structure := caps.GetStructureAt(0)
	width, _ := structure.GetValue("width")
	height, _ := structure.GetValue("height")

	w := width.(int)
	h := height.(int)

	// Читаем данные из buffer
	mapInfo := buffer.Map(gst.MapRead)
	if mapInfo == nil {
		return
	}

	data := mapInfo.Bytes()
	expectedSize := w * h * 4

	// Копируем данные в новый slice ДО Unmap
	dataCopy := make([]byte, expectedSize)
	if len(data) >= expectedSize {
		copy(dataCopy, data[:expectedSize])
	} else {
		buffer.Unmap()
		return
	}

	// Освобождаем map
	buffer.Unmap()

	// Конвертируем в image.Image используя скопированные данные
	img := gs.rgbaToImage(dataCopy, w, h)
	if img != nil {
		meta := videoLatencyFrameMeta{
			producedAt:  producedAt,
			copyTime:    time.Since(producedAt),
			frameWidth:  w,
			frameHeight: h,
		}
		gs.mutex.Lock()
		gs.frameCount++
		frameNum := gs.frameCount
		gs.mutex.Unlock()

		// Логируем каждый 300-й кадр (~10 сек при 30fps)
		if frameNum%300 == 0 {
			gs.mutex.Lock()
			dropped := gs.framesDropped
			gs.mutex.Unlock()
			chanLen := len(gs.frameChan)
			chanCap := cap(gs.frameChan)
			logrus.Debugf("🎬 GStreamer: %d кадров | Пропущено: %d | Канал: %d/%d", frameNum, dropped, chanLen, chanCap)
		}

		// Отправляем кадр в канал НЕБЛОКИРУЮЩИМ способом
		select {
		case gs.frameChan <- videoFramePacket{img: img, meta: meta}:
			// Кадр отправлен успешно
			gs.recordIngressLatency(meta)
		default:
			// Канал полон - пропускаем кадр (критично для реалтайма!)
			gs.mutex.Lock()
			gs.framesDropped++
			dropped := gs.framesDropped
			gs.mutex.Unlock()
			// Логируем каждый 30-й пропущенный кадр
			if dropped%120 == 1 {
				chanLen := len(gs.frameChan)
				logrus.Debugf("⏭️ GStreamer: пропущен кадр #%d (всего пропущено: %d, канал: %d/%d)", frameNum, dropped, chanLen, cap(gs.frameChan))
			}
		}
	}
}

// rgbaToImage конвертирует RGBA данные в image.Image
func (gs *GStreamerService) rgbaToImage(data []byte, width, height int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, width, height))

	expectedSize := width * height * 4
	if len(data) < expectedSize {
		logrus.Warnf("⚠️ Недостаточно данных для кадра: %d байт (ожидается %d)", len(data), expectedSize)
		return nil
	}

	copy(img.Pix, data[:expectedSize])
	return img
}

// frameProcessor обрабатывает кадры из канала и отправляет в UI
func (gs *GStreamerService) frameProcessor() {
	defer func() {
		// Очищаем оставшиеся кадры из канала при завершении
		for {
			select {
			case _, ok := <-gs.frameChan:
				if !ok {
					// Канал закрыт
					goto cleanup
				}
				// Игнорируем оставшиеся кадры
			default:
				// Канал пуст
				goto cleanup
			}
		}
	cleanup:
		gs.mutex.Lock()
		gs.frameProcessorRunning = false
		gs.mutex.Unlock()
		logrus.Debug("🛑 frameProcessor завершен и очищен")
	}()

	processedCount := int64(0)
	lastLogTime := time.Now()

	for {
		select {
		case <-gs.stopChan:
			logrus.Debug("🛑 Остановка обработчика кадров по сигналу stopChan")
			return
		case packet, ok := <-gs.frameChan:
			if !ok {
				logrus.Debug("🛑 frameChan закрыт, остановка обработчика")
				return
			}

			processedCount++

			// Логируем каждую секунду
			if time.Since(lastLogTime) > 5*time.Second {
				chanLen := len(gs.frameChan)
				logrus.Debugf("📤 frameProcessor: обработано %d кадров, канал: %d/%d", processedCount, chanLen, cap(gs.frameChan))
				lastLogTime = time.Now()
			}

			// Отправляем кадр в callback
			gs.mutex.RLock()
			callback := gs.onFrameReceived
			gs.mutex.RUnlock()

			if callback != nil {
				gs.mutex.Lock()
				gs.lastFrameTime = time.Now()
				gs.mutex.Unlock()

				gs.recordUIDelay(time.Since(packet.meta.producedAt), packet.meta, "Linux")
				callback(packet.img)
			}
		}
	}
}

// monitorPipeline мониторит состояние GStreamer pipeline
func (gs *GStreamerService) monitorPipeline() {
	defer func() {
		// Обработка паники для безопасности
		if r := recover(); r != nil {
			logrus.Errorf("❌ Паника в monitorPipeline: %v", r)
		}

		gs.mutex.Lock()
		gs.monitorRunning = false
		gs.mutex.Unlock()
		logrus.Debug("🛑 monitorPipeline завершен и очищен")
	}()

	logrus.Debug("📊 Запуск мониторинга GStreamer pipeline")

	// Получаем bus для мониторинга сообщений
	gs.mutex.RLock()
	pipeline := gs.pipeline
	stopChan := gs.stopChan
	gs.mutex.RUnlock()

	if pipeline == nil {
		logrus.Warn("⚠️ Pipeline is nil, cannot monitor")
		return
	}

	bus := pipeline.GetPipelineBus()
	if bus == nil {
		logrus.Warn("⚠️ Bus is nil, cannot monitor")
		return
	}

	// Используем TimedPop БЕЗ ручного Unref - сообщения освобождаются автоматически
	for {
		// Проверяем stopChan для немедленной остановки
		select {
		case <-stopChan:
			logrus.Debug("🛑 Остановка мониторинга по stopChan")
			return
		default:
		}

		// Проверяем состояние подключения перед каждым циклом
		gs.mutex.RLock()
		connected := gs.isConnected
		manualDisc := gs.manualDisconnect
		currentPipeline := gs.pipeline
		gs.mutex.RUnlock()

		// Если pipeline изменился или был удален, завершаем мониторинг
		if currentPipeline != pipeline {
			logrus.Debug("🛑 Остановка мониторинга: pipeline изменился")
			return
		}

		if !connected || manualDisc {
			logrus.Debug("🛑 Остановка мониторинга: disconnected или manual disconnect")
			return
		}

		msg := bus.TimedPop(100 * time.Millisecond)
		if msg == nil {
			continue
		}

		switch msg.Type() {
		case gst.MessageEOS:
			logrus.Warn("⚠️ GStreamer: конец потока (EOS)")
			gs.mutex.RLock()
			callback := gs.onStateChanged
			gs.mutex.RUnlock()
			if callback != nil {
				callback("eos")
			}
			// Запускаем переподключение только если не было ручного отключения
			gs.mutex.RLock()
			shouldReconnect := !gs.manualDisconnect && gs.autoReconnect
			gs.mutex.RUnlock()
			if shouldReconnect {
				go gs.attemptReconnect()
			}

		case gst.MessageError:
			err := msg.ParseError()
			logrus.Errorf("❌ GStreamer ошибка: %v", err)
			if gs.videoMode == models.VideoModeJPEGRTP && strings.Contains(fmt.Sprint(err), "Internal data stream error") {
				gs.handleFailedJPEGPipeline(err)
			}
			gs.mutex.RLock()
			errCallback := gs.onError
			gs.mutex.RUnlock()
			if errCallback != nil {
				errCallback(fmt.Errorf("%v", err))
			}
			// Запускаем переподключение только если не было ручного отключения
			gs.mutex.RLock()
			shouldReconnect := !gs.manualDisconnect && gs.autoReconnect
			gs.mutex.RUnlock()
			if shouldReconnect {
				go gs.attemptReconnect()
			}

		case gst.MessageWarning:
			warn := msg.ParseWarning()
			logrus.Warnf("⚠️ GStreamer предупреждение: %v", warn)

		case gst.MessageStateChanged:
			old, new := msg.ParseStateChanged()
			logrus.Debugf("🔄 GStreamer состояние: %s -> %s", old.String(), new.String())
			gs.mutex.RLock()
			stateCallback := gs.onStateChanged
			gs.mutex.RUnlock()
			if stateCallback != nil {
				stateCallback(new.String())
			}
		}

		// НЕ вызываем msg.Unref() - сообщение освобождается автоматически!
	}
}

// Disconnect отключается от RTP/UDP потока
func (gs *GStreamerService) Disconnect() error {
	gs.mutex.Lock()

	if !gs.isConnected && !gs.isConnecting {
		gs.mutex.Unlock()
		logrus.Debug("🔌 Disconnect: уже отключен")
		return nil
	}

	logrus.Debug("🔌 Отключение от RTP/UDP потока...")

	gs.manualDisconnect = true

	// Устанавливаем флаги остановки ПЕРЕД остановкой pipeline
	gs.isConnected = false
	gs.isConnecting = false

	// Сохраняем pipeline перед unlock
	pipeline := gs.pipeline
	stopChan := gs.stopChan
	gs.mutex.Unlock()

	// Сигнализируем горутинам об остановке через закрытие stopChan
	if stopChan != nil {
		select {
		case <-stopChan: // Канал уже закрыт
			logrus.Debug("🔌 stopChan уже закрыт")
		default:
			close(stopChan)
			logrus.Debug("🔌 stopChan закрыт")
		}
	}

	// Отправляем EOS для корректного завершения pipeline
	if pipeline != nil {
		logrus.Debug("🛑 Отправка EOS в GStreamer pipeline...")
		pipeline.SendEvent(gst.NewEOSEvent())
	}

	// Ждем завершения горутин (не более 2 секунд)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		gs.mutex.RLock()
		running := gs.frameProcessorRunning || gs.monitorRunning
		gs.mutex.RUnlock()

		if !running {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Проверяем, завершились ли горутины
	gs.mutex.RLock()
	stillRunning := gs.frameProcessorRunning || gs.monitorRunning
	gs.mutex.RUnlock()

	if stillRunning {
		logrus.Warn("⚠️ Некоторые горутины не завершились в течение 2 секунд, продолжаем...")
	}

	// Останавливаем pipeline
	gs.mutex.Lock()
	if gs.pipeline != nil {
		logrus.Debug("🛑 Остановка GStreamer pipeline...")
		gs.pipeline.SetState(gst.StateNull)

		// Небольшая задержка для корректного перехода в StateNull
		time.Sleep(100 * time.Millisecond)

		// НЕ вызываем Unref() - pipeline освобождается автоматически при SetState(StateNull)
		gs.pipeline = nil
		logrus.Debug("✅ GStreamer pipeline остановлен")
	}

	// Очищаем канал кадров от оставшихся данных
	if gs.frameChan != nil {
		// Неблокирующая очистка канала
		for {
			select {
			case <-gs.frameChan:
				// Игнорируем оставшиеся кадры
			default:
				// Канал пуст
				goto doneClearing
			}
		}
	doneClearing:
		logrus.Debug("✅ Канал кадров очищен")
	}
	gs.mutex.Unlock()

	logrus.Debug("✅ GStreamer соединение закрыто")
	return nil
}

// attemptReconnect пытается переподключиться к RTP/UDP потоку
func (gs *GStreamerService) attemptReconnect() {
	gs.mutex.Lock()

	// Проверяем условия для переподключения
	if !gs.autoReconnect || gs.manualDisconnect || gs.isConnecting || gs.isReconnecting {
		gs.mutex.Unlock()
		logrus.Infof("🔄 Переподключение пропущено: autoReconnect=%v, manualDisconnect=%v, isConnecting=%v, isReconnecting=%v",
			gs.autoReconnect, gs.manualDisconnect, gs.isConnecting, gs.isReconnecting)
		return
	}

	if gs.reconnectAttempts >= gs.maxReconnectAttempts {
		logrus.Errorf("❌ Превышено максимальное количество попыток переподключения (%d)", gs.maxReconnectAttempts)
		gs.autoReconnect = false
		gs.mutex.Unlock()
		return
	}

	// Устанавливаем флаг переподключения чтобы избежать множественных попыток
	gs.isReconnecting = true
	gs.isConnected = false
	gs.isConnecting = false

	// Принудительно очищаем старый pipeline перед переподключением
	oldPipeline := gs.pipeline
	stopChan := gs.stopChan
	gs.pipeline = nil
	gs.mutex.Unlock()

	logrus.Debug("🧹 Очистка старого pipeline перед переподключением...")

	// Закрываем stopChan чтобы остановить горутины
	if stopChan != nil {
		select {
		case <-stopChan:
			logrus.Debug("🔌 stopChan уже был закрыт")
		default:
			close(stopChan)
			logrus.Debug("🔌 stopChan закрыт для переподключения")
		}
	}

	// Останавливаем старый pipeline
	if oldPipeline != nil {
		logrus.Debug("🛑 Остановка старого pipeline...")
		oldPipeline.SetState(gst.StateNull)
		time.Sleep(200 * time.Millisecond) // Увеличенная задержка для полной остановки
		logrus.Debug("✅ Старый pipeline остановлен")
	}

	// Ждем завершения горутин
	logrus.Debug("⏳ Ожидание завершения горутин...")
	deadline := time.Now().Add(3 * time.Second) // Увеличено до 3 секунд
	for time.Now().Before(deadline) {
		gs.mutex.RLock()
		running := gs.frameProcessorRunning || gs.monitorRunning
		gs.mutex.RUnlock()

		if !running {
			logrus.Debug("✅ Все горутины завершены")
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Проверяем, завершились ли горутины
	gs.mutex.RLock()
	stillRunning := gs.frameProcessorRunning || gs.monitorRunning
	gs.mutex.RUnlock()

	if stillRunning {
		logrus.Warn("⚠️ Некоторые горутины все еще работают, но продолжаем переподключение")
	}

	gs.mutex.Lock()
	gs.reconnectAttempts++
	attempt := gs.reconnectAttempts
	maxAttempts := gs.maxReconnectAttempts
	gs.mutex.Unlock()

	logrus.Infof("🔄 Переподключение GStreamer #%d/%d...", attempt, maxAttempts)

	// Задержка перед переподключением (увеличивается с каждой попыткой)
	delay := time.Duration(attempt) * 2 * time.Second
	if delay > 10*time.Second {
		delay = 10 * time.Second // Максимум 10 секунд
	}
	logrus.Debugf("⏳ Задержка перед переподключением: %v", delay)
	time.Sleep(delay)

	gs.mutex.RLock()
	abortReconnect := !gs.autoReconnect || gs.manualDisconnect
	gs.mutex.RUnlock()
	if abortReconnect {
		logrus.Debug("🛑 Переподключение отменено до нового ConnectToRTP")
		gs.mutex.Lock()
		gs.isReconnecting = false
		gs.mutex.Unlock()
		return
	}

	if err := gs.ConnectToRTP(); err != nil {
		logrus.Errorf("❌ Ошибка переподключения GStreamer #%d: %v", attempt, err)
		gs.mutex.Lock()
		gs.isReconnecting = false
		gs.mutex.Unlock()

		// Пробуем еще раз если не достигли лимита
		if attempt < maxAttempts {
			logrus.Info("🔄 Запланирована следующая попытка переподключения...")
			// Запускаем следующую попытку асинхронно
			go gs.attemptReconnect()
		}
	} else {
		logrus.Info("✅ Успешное переподключение GStreamer!")
		gs.mutex.Lock()
		gs.reconnectAttempts = 0
		gs.isReconnecting = false
		gs.mutex.Unlock()
	}
}

// SetOnFrameReceived устанавливает callback для получения кадров
func (gs *GStreamerService) SetOnFrameReceived(callback func(image.Image)) {
	gs.onFrameReceived = callback
}

// SetOnStateChanged устанавливает callback для изменения состояния
func (gs *GStreamerService) SetOnStateChanged(callback func(string)) {
	gs.onStateChanged = callback
}

// SetOnError устанавливает callback для ошибок
func (gs *GStreamerService) SetOnError(callback func(error)) {
	gs.onError = callback
}

// IsConnected возвращает состояние подключения
func (gs *GStreamerService) IsConnected() bool {
	gs.mutex.RLock()
	defer gs.mutex.RUnlock()
	return gs.isConnected
}

// GetStats возвращает статистику соединения
func (gs *GStreamerService) GetStats() map[string]interface{} {
	gs.mutex.RLock()
	defer gs.mutex.RUnlock()

	return map[string]interface{}{
		"connected":        gs.isConnected,
		"connecting":       gs.isConnecting,
		"frame_count":      gs.frameCount,
		"frames_dropped":   gs.framesDropped,
		"last_frame_time":  gs.lastFrameTime,
		"low_latency_mode": gs.config.LowLatencyMode,
	}
}

// UpdateHost обновляет хост видеопотока
func (gs *GStreamerService) UpdateHost(host string) {
	gs.mutex.Lock()
	defer gs.mutex.Unlock()

	gs.config.VideoHost = host
	logrus.Debugf("🔧 GStreamer сервис: хост обновлен на %s", host)
}

// UpdateVideoPort обновляет порт видеопотока (RTP/UDP)
func (gs *GStreamerService) UpdateVideoPort(port int) {
	gs.mutex.Lock()
	defer gs.mutex.Unlock()

	gs.config.VideoUDPPort = port
	logrus.Debugf("🔧 GStreamer сервис: видео UDP порт обновлен на %d", port)
}

// UpdateVideoUDPPort обновляет порт приёма UDP видео
func (gs *GStreamerService) UpdateVideoUDPPort(port int) {
	gs.mutex.Lock()
	defer gs.mutex.Unlock()

	gs.config.VideoUDPPort = port
	logrus.Debugf("🔧 GStreamer сервис: видео UDP порт обновлен на %d", port)
}

func (gs *GStreamerService) SetVideoMode(mode string) {
	gs.mutex.Lock()
	defer gs.mutex.Unlock()
	if mode == "" {
		mode = models.VideoModeH264
	}
	if gs.videoMode != mode {
		gs.jpegCandidateIndex = 0
		gs.lastJPEGPipeline = ""
		gs.failedJPEGPipelines = make(map[string]bool)
	}
	gs.videoMode = mode
}

type jpegPipelineCandidate struct {
	name            string
	requiredElement string
	str             string
}

func (gs *GStreamerService) buildJPEGPipelineCandidates(udpPort int) []jpegPipelineCandidate {
	bindHost := gs.config.VideoBindHost
	if bindHost == "" {
		bindHost = "127.0.0.1"
	}
	base := fmt.Sprintf(
		"udpsrc address=%s port=%d buffer-size=65536 caps=\"application/x-rtp,media=video,encoding-name=JPEG,clock-rate=90000,payload=26\" ! "+
			"rtpjitterbuffer latency=15 faststart-min-packets=1 drop-on-latency=true ! "+
			"rtpjpegdepay ! ",
		bindHost, udpPort,
	)

	softwareDecoderPath := "videoconvert ! video/x-raw,format=RGBA ! appsink name=sink sync=false max-buffers=2 drop=true"
	vaapiDecoderPath := "vapostproc ! videoconvert ! video/x-raw,format=RGBA ! appsink name=sink sync=false max-buffers=2 drop=true"
	vaapiDirectPath := "vapostproc ! video/x-raw,format=RGBA ! appsink name=sink sync=false max-buffers=2 drop=true"

	all := map[string]jpegPipelineCandidate{
		"vajpegdec":        {name: "vajpegdec + vapostproc (HW)", requiredElement: "vajpegdec", str: base + "jpegparse ! vajpegdec ! " + vaapiDecoderPath},
		"vajpegdec_direct": {name: "vajpegdec + vapostproc direct (HW)", requiredElement: "vajpegdec", str: base + "jpegparse ! vajpegdec ! " + vaapiDirectPath},
		"qsvjpegdec":       {name: "qsvjpegdec (HW)", requiredElement: "qsvjpegdec", str: base + "jpegparse ! qsvjpegdec ! " + softwareDecoderPath},
		"nvjpegdec":        {name: "nvjpegdec (HW)", requiredElement: "nvjpegdec", str: base + "jpegparse ! nvjpegdec ! " + softwareDecoderPath},
		"jpegdec":          {name: "jpegdec (SW preferred)", requiredElement: "jpegdec", str: base + "jpegdec ! " + softwareDecoderPath},
		"avdec_mjpeg":      {name: "avdec_mjpeg (SW fallback)", requiredElement: "avdec_mjpeg", str: base + "jpegparse ! avdec_mjpeg ! " + softwareDecoderPath},
		"decodebin":        {name: "decodebin (generic fallback)", requiredElement: "decodebin", str: base + "jpegparse ! decodebin ! " + softwareDecoderPath},
	}

	order := []string{"jpegdec", "avdec_mjpeg", "decodebin"}
	switch gs.detectedGPUVendor {
	case "intel":
		order = []string{"vajpegdec", "vajpegdec_direct", "qsvjpegdec", "jpegdec", "avdec_mjpeg", "decodebin"}
	case "nvidia":
		order = []string{"nvjpegdec", "jpegdec", "avdec_mjpeg", "decodebin", "vajpegdec", "vajpegdec_direct", "qsvjpegdec"}
	case "amd":
		order = []string{"jpegdec", "avdec_mjpeg", "decodebin", "vajpegdec", "vajpegdec_direct", "qsvjpegdec", "nvjpegdec"}
	case "virt", "software", "unknown", "":
		order = []string{"jpegdec", "avdec_mjpeg", "decodebin", "vajpegdec", "vajpegdec_direct", "qsvjpegdec", "nvjpegdec"}
	default:
		order = []string{"jpegdec", "avdec_mjpeg", "decodebin", "vajpegdec", "vajpegdec_direct", "qsvjpegdec", "nvjpegdec"}
	}

	result := make([]jpegPipelineCandidate, 0, len(order))
	seen := make(map[string]bool, len(order))
	for _, key := range order {
		candidate, ok := all[key]
		if !ok || seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, candidate)
	}
	for key, candidate := range all {
		if seen[key] {
			continue
		}
		result = append(result, candidate)
	}
	return result
}

func (gs *GStreamerService) handleFailedJPEGPipeline(err error) {
	gs.mutex.Lock()
	defer gs.mutex.Unlock()

	if gs.lastJPEGPipeline == "" {
		return
	}
	gs.failedJPEGPipelines[gs.lastJPEGPipeline] = true
	gs.jpegCandidateIndex++
	logrus.Warnf("⚠️ [Linux/JPEG] Decoder %s failed, switching to next JPEG candidate (index=%d): %v", gs.lastJPEGPipeline, gs.jpegCandidateIndex, err)
}

func udpPortFromConfig(cfg *models.AppConfig) int {
	if cfg != nil && cfg.VideoUDPPort > 0 {
		return cfg.VideoUDPPort
	}
	return models.DefaultVideoUDPPort
}

func detectLinuxGPUVendor() string {
	vendors := make(map[string]bool)
	entries, err := filepath.Glob("/sys/class/drm/card*/device/vendor")
	if err != nil {
		return "unknown"
	}
	sort.Strings(entries)
	for _, vendorPath := range entries {
		data, readErr := os.ReadFile(vendorPath)
		if readErr != nil {
			continue
		}
		switch strings.TrimSpace(strings.ToLower(string(data))) {
		case "0x8086":
			vendors["intel"] = true
		case "0x10de":
			vendors["nvidia"] = true
		case "0x1002", "0x1022":
			vendors["amd"] = true
		case "0x1af4", "0x1234":
			vendors["virt"] = true
		}
	}
	switch {
	case vendors["intel"]:
		return "intel"
	case vendors["nvidia"]:
		return "nvidia"
	case vendors["amd"]:
		return "amd"
	case vendors["virt"]:
		return "virt"
	case len(vendors) == 0:
		return "unknown"
	default:
		return "unknown"
	}
}

func (gs *GStreamerService) SetExpectedVideoSize(width, height int) {
	gs.mutex.Lock()
	defer gs.mutex.Unlock()
	gs.expectedWidth = width
	gs.expectedHeight = height
}

// GetConfig возвращает конфигурацию
func (gs *GStreamerService) GetConfig() *models.AppConfig {
	gs.mutex.RLock()
	defer gs.mutex.RUnlock()
	return gs.config
}

func (gs *GStreamerService) SupportsNativeFullscreen() bool {
	return true
}

func (gs *GStreamerService) IsNativeFullscreenActive() bool {
	gs.mutex.RLock()
	defer gs.mutex.RUnlock()
	return gs.nativeFullscreenActive
}

func (gs *GStreamerService) StartNativeFullscreen() error {
	if err := gs.Disconnect(); err != nil {
		return err
	}

	udpPort := gs.config.VideoUDPPort
	if udpPort <= 0 {
		udpPort = models.DefaultVideoUDPPort
	}
	bindHost := gs.config.VideoBindHost
	if bindHost == "" {
		bindHost = "127.0.0.1"
	}

	candidates := gs.nativeFullscreenCandidates(bindHost, udpPort)
	var lastErr error
	for _, candidate := range candidates {
		cmd, err := gs.startNativeFullscreenProcess(candidate)
		if err == nil {
			gs.mutex.Lock()
			gs.nativeFullscreenCmd = cmd
			gs.nativeFullscreenActive = true
			gs.mutex.Unlock()
			logrus.Infof("✅ [Linux] native fullscreen started via %s", candidate.name)
			return nil
		}
		lastErr = err
		logrus.Warnf("⚠️ [Linux] native fullscreen candidate %s failed: %v", candidate.name, err)
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("no native fullscreen candidates available")
	}
	return lastErr
}

func (gs *GStreamerService) StopNativeFullscreen() error {
	gs.mutex.Lock()
	cmd := gs.nativeFullscreenCmd
	gs.nativeFullscreenCmd = nil
	gs.nativeFullscreenActive = false
	gs.mutex.Unlock()

	if cmd == nil || cmd.Process == nil {
		return nil
	}

	_ = cmd.Process.Signal(os.Interrupt)
	time.Sleep(200 * time.Millisecond)
	_ = cmd.Process.Kill()
	return nil
}

type nativeFullscreenCandidate struct {
	name string
	args []string
}

func (gs *GStreamerService) nativeFullscreenCandidates(bindHost string, udpPort int) []nativeFullscreenCandidate {
	base := []string{
		"-q",
		"udpsrc",
		fmt.Sprintf("address=%s", bindHost),
		fmt.Sprintf("port=%d", udpPort),
		"buffer-size=65536",
	}

	if gs.videoMode == models.VideoModeJPEGRTP {
		base = append(base,
			`caps=application/x-rtp,media=video,encoding-name=JPEG,clock-rate=90000,payload=26`,
			"!",
			"rtpjitterbuffer", "latency=10", "faststart-min-packets=1", "drop-on-latency=true",
			"!",
			"rtpjpegdepay",
			"!",
			"jpegparse",
			"!",
			"decodebin",
			"!",
			"queue", "max-size-buffers=2", "leaky=downstream",
			"!",
		)
	} else {
		base = append(base,
			`caps=application/x-rtp,media=video,encoding-name=H264,payload=96`,
			"!",
			"rtpjitterbuffer", "latency=10", "faststart-min-packets=1", "drop-on-latency=true",
			"!",
			"rtph264depay",
			"!",
			"h264parse", "config-interval=-1",
			"!",
			"decodebin",
			"!",
			"queue", "max-size-buffers=2", "leaky=downstream",
			"!",
		)
	}

	return []nativeFullscreenCandidate{
		{
			name: "kmssink",
			args: append(
				append([]string{}, base...),
				"videoconvert",
				"!",
				"kmssink",
				"force-modesetting=true",
				"sync=false",
			),
		},
		{
			name: "fbdevsink",
			args: append(
				append([]string{}, base...),
				"videoconvert",
				"!",
				"fbdevsink",
				"sync=false",
			),
		},
	}
}

func (gs *GStreamerService) startNativeFullscreenProcess(candidate nativeFullscreenCandidate) (*exec.Cmd, error) {
	path, err := exec.LookPath("gst-launch-1.0")
	if err != nil {
		return nil, fmt.Errorf("gst-launch-1.0 not found: %w", err)
	}

	cmd := exec.Command(path, candidate.args...)
	cmd.Env = gs.getNativeFullscreenEnv(path)
	var stderr bytes.Buffer
	cmd.Stdout = os.Stdout
	cmd.Stderr = io.MultiWriter(os.Stderr, &stderr)

	done := make(chan error, 1)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	go func() {
		err := cmd.Wait()
		gs.mutex.Lock()
		if gs.nativeFullscreenCmd == cmd {
			gs.nativeFullscreenCmd = nil
			gs.nativeFullscreenActive = false
		}
		gs.mutex.Unlock()
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			err = fmt.Errorf("native fullscreen process exited immediately")
		}
		if details := strings.TrimSpace(stderr.String()); details != "" {
			err = fmt.Errorf("%w: %s", err, details)
		}
		return nil, err
	case <-time.After(1200 * time.Millisecond):
		return cmd, nil
	}
}

func (gs *GStreamerService) getNativeFullscreenEnv(gstLaunchPath string) []string {
	env := os.Environ()
	for _, pluginDir := range nativeFullscreenPluginDirs(gstLaunchPath) {
		env = appendOrPrependEnv(env, "GST_PLUGIN_PATH", pluginDir)
		env = appendOrPrependEnv(env, "GST_PLUGIN_SYSTEM_PATH", pluginDir)
		logrus.Infof("🔧 [Linux] native fullscreen GST plugin dir: %s", pluginDir)
	}
	return env
}

func nativeFullscreenPluginDirs(gstLaunchPath string) []string {
	baseDir := filepath.Dir(gstLaunchPath)
	patterns := []string{
		filepath.Join(baseDir, "..", "lib", "gstreamer-1.0"),
		filepath.Join(baseDir, "..", "lib64", "gstreamer-1.0"),
		filepath.Join(baseDir, "..", "lib", "*", "gstreamer-1.0"),
		filepath.Join(baseDir, "..", "lib64", "*", "gstreamer-1.0"),
	}

	var dirs []string
	seen := make(map[string]struct{})
	for _, pattern := range patterns {
		matches, err := filepath.Glob(filepath.Clean(pattern))
		if err != nil {
			continue
		}
		for _, match := range matches {
			info, err := os.Stat(match)
			if err != nil || !info.IsDir() {
				continue
			}
			if _, exists := seen[match]; exists {
				continue
			}
			seen[match] = struct{}{}
			dirs = append(dirs, match)
		}
	}
	return dirs
}

func appendOrPrependEnv(env []string, key, value string) []string {
	prefix := key + "="
	for i, entry := range env {
		if !strings.HasPrefix(entry, prefix) {
			continue
		}
		current := strings.TrimPrefix(entry, prefix)
		if current == "" {
			env[i] = prefix + value
			return env
		}
		for _, part := range strings.Split(current, string(os.PathListSeparator)) {
			if part == value {
				return env
			}
		}
		env[i] = prefix + value + string(os.PathListSeparator) + current
		return env
	}
	return append(env, prefix+value)
}

// SetAutoReconnect включает/выключает автоматическое переподключение
func (gs *GStreamerService) SetAutoReconnect(enabled bool) {
	gs.mutex.Lock()
	defer gs.mutex.Unlock()
	gs.autoReconnect = enabled
}

// SetMaxReconnectAttempts устанавливает максимальное количество попыток переподключения
func (gs *GStreamerService) SetMaxReconnectAttempts(max int) {
	gs.mutex.Lock()
	defer gs.mutex.Unlock()
	gs.maxReconnectAttempts = max
	gs.reconnectAttempts = 0 // Сбрасываем счетчик при изменении максимума
}

// Reconnect принудительно переподключается к RTP/UDP потоку (для смены устройств)
func (gs *GStreamerService) Reconnect() error {
	logrus.Info("🔄 Принудительное переподключение (смена устройства)...")

	// Сначала отключаемся
	if err := gs.Disconnect(); err != nil {
		logrus.Warnf("⚠️ Ошибка при отключении перед переподключением: %v", err)
	}

	// Ждем немного для корректного отключения
	time.Sleep(500 * time.Millisecond)

	// Сбрасываем счетчик попыток переподключения
	gs.mutex.Lock()
	gs.reconnectAttempts = 0
	gs.autoReconnect = true
	gs.manualDisconnect = false
	gs.mutex.Unlock()

	// Подключаемся заново
	logrus.Info("🔗 Подключаемся к новому устройству...")
	return gs.ConnectToRTP()
}
