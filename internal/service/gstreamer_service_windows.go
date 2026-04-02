//go:build windows
// +build windows

package service

import (
	"bytes"
	"fmt"
	"image"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"usbridge-client/internal/models"

	"github.com/sirupsen/logrus"
	"github.com/tinyzimmer/go-gst/gst"
	"github.com/tinyzimmer/go-gst/gst/app"
)

// GStreamerService сервис для работы с GStreamer H264 потоком на Windows
// Использует d3d11h264dec для аппаратного декодирования (Direct3D11/DXVA)
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
	expectedWidth          int
	expectedHeight         int

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

// NewGStreamerService создает новый GStreamer сервис для Windows
func NewGStreamerService(config *models.AppConfig) *GStreamerService {
	gs := &GStreamerService{
		config:                config,
		autoReconnect:         true,
		reconnectAttempts:     0,
		maxReconnectAttempts:  5,
		videoMode:             models.VideoModeH264,
		frameChan:             make(chan videoFramePacket, 1), // Один кадр — минимальная задержка
		stopChan:              make(chan struct{}),
		frameProcessorRunning: false,
		monitorRunning:        false,
	}

	logrus.Info("✅ GStreamer сервис для Windows инициализирован (d3d11h264dec - аппаратное декодирование)")
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

	logrus.Infof("🔗 [Windows] Подключение к RTP потоку mode=%s...", gs.videoMode)

	// Инициализируем GStreamer (gst.Init не возвращает ошибку)
	gst.Init(nil)

	// Создаем pipeline с аппаратным декодированием (d3d11h264dec + d3d11download для перехода в PLAYING)
	if err := gs.createPipeline(); err != nil {
		gs.mutex.Lock()
		gs.isConnecting = false
		gs.mutex.Unlock()
		return fmt.Errorf("ошибка создания pipeline: %v", err)
	}

	// Запускаем pipeline
	if err := gs.pipeline.SetState(gst.StatePlaying); err != nil {
		// Аппаратный декодер создался, но не перешёл в PLAYING (типично без d3d11download) — пробуем программный
		logrus.Warnf("⚠️ [Windows] d3d11h264dec не перешёл в PLAYING (%v), переходим на avdec_h264", err)
		gs.pipeline.SetState(gst.StateNull)
		gs.pipeline = nil
		gs.appsink = nil
		if errSW := gs.createPipelineSoftware(); errSW != nil {
			gs.mutex.Lock()
			gs.isConnecting = false
			gs.mutex.Unlock()
			return fmt.Errorf("ошибка запуска pipeline и fallback: %v", errSW)
		}
		if errPlay := gs.pipeline.SetState(gst.StatePlaying); errPlay != nil {
			gs.mutex.Lock()
			gs.isConnecting = false
			gs.mutex.Unlock()
			return fmt.Errorf("ошибка запуска pipeline: %v", errPlay)
		}
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

	logrus.Infof("✅ [Windows] GStreamer RTP подключение установлено (mode=%s)", gs.videoMode)
	return nil
}

// ConnectToUDPViaPipe — pipe mode для FRP relay (Windows: пока заглушка)
func (gs *GStreamerService) ConnectToUDPViaPipe(pipeReader *os.File) error {
	_ = pipeReader
	return fmt.Errorf("UDP relay (pipe) пока не реализован на Windows, используйте прямое подключение")
}

// createPipeline создает GStreamer pipeline для текущего RTP video mode на Windows.
func (gs *GStreamerService) createPipeline() error {
	udpPort := gs.config.VideoUDPPort
	if udpPort <= 0 {
		udpPort = models.DefaultVideoUDPPort
	}
	bindHost := gs.config.VideoBindHost
	if bindHost == "" {
		bindHost = "127.0.0.1"
	}
	logrus.Infof("📹 [Windows] UDP порт приёма RTP video: %d (mode=%s)", udpPort, gs.videoMode)

	if gs.videoMode == models.VideoModeJPEGRTP {
		return gs.createPipelineJPEG(udpPort)
	}
	if gs.videoMode == models.VideoModeRawYUYV {
		return gs.createPipelineRawYUYV(udpPort)
	}

	// Вариант 1: d3d11h264dec ! d3d11download — перевод D3D11-памяти в системную, иначе переход в PLAYING часто падает
	hwWithDownload := fmt.Sprintf(
		"udpsrc address=%s port=%d buffer-size=131072 caps=\"application/x-rtp,media=video,encoding-name=H264,payload=96\" ! "+
			"rtpjitterbuffer latency=15 faststart-min-packets=1 drop-on-latency=true ! "+
			"rtph264depay ! "+
			"h264parse config-interval=-1 ! "+
			"d3d11h264dec ! "+
			"d3d11download ! "+
			"videoconvert ! "+
			"video/x-raw,format=RGBA ! "+
			"appsink name=sink sync=false max-buffers=1 drop=true",
		bindHost, udpPort,
	)

	// Вариант 2: без d3d11download (старый пайплайн, на части систем может сработать)
	hwWithoutDownload := fmt.Sprintf(
		"udpsrc address=%s port=%d buffer-size=131072 caps=\"application/x-rtp,media=video,encoding-name=H264,payload=96\" ! "+
			"rtpjitterbuffer latency=15 faststart-min-packets=1 drop-on-latency=true ! "+
			"rtph264depay ! "+
			"h264parse config-interval=-1 ! "+
			"d3d11h264dec ! "+
			"videoconvert ! "+
			"video/x-raw,format=RGBA ! "+
			"appsink name=sink sync=false max-buffers=1 drop=true",
		bindHost, udpPort,
	)

	logrus.Info("🔧 [Windows] GStreamer pipeline (аппаратное декодирование d3d11h264dec + d3d11download)")

	pipeline, err := gst.NewPipelineFromString(hwWithDownload)
	if err != nil {
		logrus.Warnf("⚠️ [Windows] pipeline с d3d11download недоступен (%v), пробуем без d3d11download", err)
		pipeline, err = gst.NewPipelineFromString(hwWithoutDownload)
	}
	if err != nil {
		logrus.Warnf("⚠️ [Windows] d3d11h264dec недоступен (%v), используем avdec_h264 (программный декодер)", err)
		return gs.createPipelineSoftware()
	}

	logrus.Info("✅ [Windows] GStreamer pipeline создан (d3d11h264dec - аппаратное декодирование)")
	gs.pipeline = pipeline
	return gs.attachAppsink()
}

func (gs *GStreamerService) createPipelineRawYUYV(udpPort int) error {
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

	logrus.Infof("🔧 [Windows/RAW] Building RAW YUYV pipeline: %dx%d", width, height)

	hwPipeline := fmt.Sprintf(
		"udpsrc address=%s port=%d buffer-size=4194304 caps=\"application/x-rtp,media=(string)video,clock-rate=(int)90000,encoding-name=(string)RAW,sampling=(string)YCbCr-4:2:2,depth=(string)8,width=(string)%d,height=(string)%d,colorimetry=(string)BT601-5,payload=(int)96\" ! "+
			"rtpjitterbuffer latency=5 faststart-min-packets=1 drop-on-latency=true ! "+
			"rtpvrawdepay ! video/x-raw,format=UYVY,width=%d,height=%d ! "+
			"d3d11upload ! d3d11convert ! d3d11download ! "+
			"video/x-raw,format=RGBA ! "+
			"appsink name=sink sync=false max-buffers=1 drop=true",
		bindHost, udpPort, width, height, width, height,
	)

	swPipeline := fmt.Sprintf(
		"udpsrc address=%s port=%d buffer-size=4194304 caps=\"application/x-rtp,media=(string)video,clock-rate=(int)90000,encoding-name=(string)RAW,sampling=(string)YCbCr-4:2:2,depth=(string)8,width=(string)%d,height=(string)%d,colorimetry=(string)BT601-5,payload=(int)96\" ! "+
			"rtpjitterbuffer latency=5 faststart-min-packets=1 drop-on-latency=true ! "+
			"rtpvrawdepay ! video/x-raw,format=UYVY,width=%d,height=%d ! "+
			"videoconvert ! video/x-raw,format=RGBA ! "+
			"appsink name=sink sync=false max-buffers=1 drop=true",
		bindHost, udpPort, width, height, width, height,
	)

	var pipeline *gst.Pipeline
	var err error
	if gst.Find("d3d11upload") != nil && gst.Find("d3d11convert") != nil && gst.Find("d3d11download") != nil {
		pipeline, err = gst.NewPipelineFromString(hwPipeline)
		if err == nil {
			logrus.Info("✅ [Windows/RAW] GStreamer pipeline created: d3d11upload -> d3d11convert -> d3d11download")
			gs.pipeline = pipeline
			return gs.attachAppsink()
		}
		logrus.Warnf("⚠️ [Windows/RAW] D3D11 RAW pipeline unavailable (%v), falling back to software conversion", err)
	} else {
		logrus.Info("ℹ️ [Windows/RAW] D3D11 upload/convert/download elements not fully available, using software conversion")
	}

	pipeline, err = gst.NewPipelineFromString(swPipeline)
	if err != nil {
		return fmt.Errorf("ошибка создания RAW YUYV pipeline: %v", err)
	}

	logrus.Info("✅ [Windows/RAW] GStreamer pipeline created: videoconvert fallback")
	gs.pipeline = pipeline
	return gs.attachAppsink()
}

func (gs *GStreamerService) createPipelineJPEG(udpPort int) error {
	bindHost := gs.config.VideoBindHost
	if bindHost == "" {
		bindHost = "127.0.0.1"
	}
	pipelines := []struct {
		name            string
		requiredElement string
		str             string
	}{
		{
			name:            "qsvjpegdec (HW Intel)",
			requiredElement: "qsvjpegdec",
			str: fmt.Sprintf(
				"udpsrc address=%s port=%d buffer-size=65536 caps=\"application/x-rtp,media=video,encoding-name=JPEG,clock-rate=90000,payload=26\" ! "+
					"rtpjitterbuffer latency=15 faststart-min-packets=1 ! "+
					"rtpjpegdepay ! jpegparse ! qsvjpegdec ! videoconvert ! video/x-raw,format=RGBA ! "+
					"appsink name=sink sync=false max-buffers=2 drop=true",
				bindHost, udpPort,
			),
		},
		{
			name:            "nvjpegdec (HW NVIDIA)",
			requiredElement: "nvjpegdec",
			str: fmt.Sprintf(
				"udpsrc address=%s port=%d buffer-size=65536 caps=\"application/x-rtp,media=video,encoding-name=JPEG,clock-rate=90000,payload=26\" ! "+
					"rtpjitterbuffer latency=15 faststart-min-packets=1 ! "+
					"rtpjpegdepay ! jpegparse ! nvjpegdec ! videoconvert ! video/x-raw,format=RGBA ! "+
					"appsink name=sink sync=false max-buffers=2 drop=true",
				bindHost, udpPort,
			),
		},
		{
			name:            "wicjpegdec (HW/OS)",
			requiredElement: "wicjpegdec",
			str: fmt.Sprintf(
				"udpsrc address=%s port=%d buffer-size=65536 caps=\"application/x-rtp,media=video,encoding-name=JPEG,clock-rate=90000,payload=26\" ! "+
					"rtpjitterbuffer latency=15 faststart-min-packets=1 ! "+
					"rtpjpegdepay ! wicjpegdec ! videoconvert ! video/x-raw,format=RGBA ! "+
					"appsink name=sink sync=false max-buffers=2 drop=true",
				bindHost, udpPort,
			),
		},
		{
			name:            "msdkmjpegdec (legacy HW Intel)",
			requiredElement: "msdkmjpegdec",
			str: fmt.Sprintf(
				"udpsrc address=%s port=%d buffer-size=65536 caps=\"application/x-rtp,media=video,encoding-name=JPEG,clock-rate=90000,payload=26\" ! "+
					"rtpjitterbuffer latency=15 faststart-min-packets=1 ! "+
					"rtpjpegdepay ! jpegparse ! msdkmjpegdec ! videoconvert ! video/x-raw,format=RGBA ! "+
					"appsink name=sink sync=false max-buffers=2 drop=true",
				bindHost, udpPort,
			),
		},
		{
			name:            "jpegdec (SW preferred)",
			requiredElement: "jpegdec",
			str: fmt.Sprintf(
				"udpsrc address=%s port=%d buffer-size=65536 caps=\"application/x-rtp,media=video,encoding-name=JPEG,clock-rate=90000,payload=26\" ! "+
					"rtpjitterbuffer latency=15 faststart-min-packets=1 ! "+
					"rtpjpegdepay ! jpegdec ! videoconvert ! video/x-raw,format=RGBA ! "+
					"appsink name=sink sync=false max-buffers=2 drop=true",
				bindHost, udpPort,
			),
		},
		{
			name:            "avdec_mjpeg (SW libav)",
			requiredElement: "avdec_mjpeg",
			str: fmt.Sprintf(
				"udpsrc address=%s port=%d buffer-size=65536 caps=\"application/x-rtp,media=video,encoding-name=JPEG,clock-rate=90000,payload=26\" ! "+
					"rtpjitterbuffer latency=15 faststart-min-packets=1 ! "+
					"rtpjpegdepay ! jpegparse ! avdec_mjpeg ! videoconvert ! video/x-raw,format=RGBA ! "+
					"appsink name=sink sync=false max-buffers=2 drop=true",
				bindHost, udpPort,
			),
		},
		{
			name:            "decodebin",
			requiredElement: "decodebin",
			str: fmt.Sprintf(
				"udpsrc address=%s port=%d buffer-size=65536 caps=\"application/x-rtp,media=video,encoding-name=JPEG,clock-rate=90000,payload=26\" ! "+
					"rtpjitterbuffer latency=15 faststart-min-packets=1 ! "+
					"rtpjpegdepay ! jpegparse ! decodebin ! videoconvert ! video/x-raw,format=RGBA ! "+
					"appsink name=sink sync=false max-buffers=2 drop=true",
				bindHost, udpPort,
			),
		},
	}

	availableJPEGElements := []string{"qsvjpegdec", "nvjpegdec", "wicjpegdec", "msdkmjpegdec", "jpegdec", "avdec_mjpeg", "decodebin"}
	var detected []string
	for _, element := range availableJPEGElements {
		if gst.Find(element) != nil {
			detected = append(detected, element)
		}
	}
	if len(detected) > 0 {
		logrus.Infof("🔎 [Windows/JPEG] available decoders/elements: %s", strings.Join(detected, ", "))
	} else {
		logrus.Warn("⚠️ [Windows/JPEG] no JPEG decoders detected in current GStreamer runtime")
	}

	var lastErr error
	for idx := range pipelines {
		candidateIdx := (gs.jpegCandidateIndex + idx) % len(pipelines)
		candidate := pipelines[candidateIdx]
		if candidate.requiredElement != "" && gst.Find(candidate.requiredElement) == nil {
			logrus.Infof("ℹ️ [Windows/JPEG] пропускаем %s: element %q не установлен в текущем runtime", candidate.name, candidate.requiredElement)
			continue
		}
		pipeline, err := gst.NewPipelineFromString(candidate.str)
		if err != nil {
			lastErr = err
			logrus.Warnf("⚠️ [Windows/JPEG] pipeline недоступен (%s): %v", candidate.name, err)
			continue
		}

		gs.pipeline = pipeline
		if err := gs.attachAppsink(); err != nil {
			lastErr = err
			gs.pipeline.SetState(gst.StateNull)
			gs.pipeline = nil
			continue
		}
		gs.lastJPEGPipeline = candidate.name
		logrus.Infof("✅ [Windows/JPEG] GStreamer pipeline создан: %s", candidate.name)
		return nil
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("не найдено подходящих JPEG декодеров GStreamer")
	}
	return lastErr
}

// createPipelineSoftware создает pipeline только с программным декодером avdec_h264 (fallback при ошибке PLAYING или отсутствии d3d11).
func (gs *GStreamerService) createPipelineSoftware() error {
	udpPort := gs.config.VideoUDPPort
	if udpPort <= 0 {
		udpPort = models.DefaultVideoUDPPort
	}
	bindHost := gs.config.VideoBindHost
	if bindHost == "" {
		bindHost = "127.0.0.1"
	}
	swPipeline := fmt.Sprintf(
		"udpsrc address=%s port=%d buffer-size=131072 caps=\"application/x-rtp,media=video,encoding-name=H264,payload=96\" ! "+
			"rtpjitterbuffer latency=15 faststart-min-packets=1 drop-on-latency=true ! "+
			"rtph264depay ! "+
			"h264parse config-interval=-1 ! "+
			"avdec_h264 max-threads=4 ! "+
			"videoconvert ! "+
			"video/x-raw,format=RGBA ! "+
			"appsink name=sink sync=false max-buffers=1 drop=true",
		bindHost, udpPort,
	)
	pipeline, err := gst.NewPipelineFromString(swPipeline)
	if err != nil {
		return fmt.Errorf("ошибка создания pipeline (avdec_h264): %v", err)
	}
	logrus.Info("✅ [Windows] GStreamer pipeline создан (avdec_h264 - программный декодер)")
	gs.pipeline = pipeline
	return gs.attachAppsink()
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
			logrus.Debugf("🎬 [Windows] GStreamer: %d кадров | Пропущено: %d | Канал: %d/%d", frameNum, dropped, chanLen, chanCap)
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
				logrus.Debugf("⏭️ [Windows] GStreamer: пропущен кадр #%d (всего пропущено: %d, канал: %d/%d)", frameNum, dropped, chanLen, cap(gs.frameChan))
			}
		}
	}
}

// rgbaToImage конвертирует RGBA данные в image.Image
func (gs *GStreamerService) rgbaToImage(data []byte, width, height int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, width, height))

	expectedSize := width * height * 4
	if len(data) < expectedSize {
		logrus.Warnf("⚠️ [Windows] Недостаточно данных для кадра: %d байт (ожидается %d)", len(data), expectedSize)
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
		logrus.Info("🛑 [Windows] frameProcessor завершен и очищен")
	}()

	processedCount := int64(0)
	lastLogTime := time.Now()

	for {
		select {
		case <-gs.stopChan:
			logrus.Info("🛑 [Windows] Остановка обработчика кадров по сигналу stopChan")
			return
		case packet, ok := <-gs.frameChan:
			if !ok {
				logrus.Info("🛑 [Windows] frameChan закрыт, остановка обработчика")
				return
			}

			processedCount++

			// Логируем каждую секунду
			if time.Since(lastLogTime) > 5*time.Second {
				chanLen := len(gs.frameChan)
				logrus.Debugf("📤 [Windows] frameProcessor: обработано %d кадров, канал: %d/%d", processedCount, chanLen, cap(gs.frameChan))
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

				gs.recordUIDelay(time.Since(packet.meta.producedAt), packet.meta, "Windows")
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
			logrus.Errorf("❌ [Windows] Паника в monitorPipeline: %v", r)
		}

		gs.mutex.Lock()
		gs.monitorRunning = false
		gs.mutex.Unlock()
		logrus.Info("🛑 [Windows] monitorPipeline завершен и очищен")
	}()

	logrus.Info("📊 [Windows] Запуск мониторинга GStreamer pipeline")

	// Получаем bus для мониторинга сообщений
	gs.mutex.RLock()
	pipeline := gs.pipeline
	stopChan := gs.stopChan
	gs.mutex.RUnlock()

	if pipeline == nil {
		logrus.Warn("⚠️ [Windows] Pipeline is nil, cannot monitor")
		return
	}

	bus := pipeline.GetPipelineBus()
	if bus == nil {
		logrus.Warn("⚠️ [Windows] Bus is nil, cannot monitor")
		return
	}

	// Используем TimedPop БЕЗ ручного Unref - сообщения освобождаются автоматически
	for {
		// Проверяем stopChan для немедленной остановки
		select {
		case <-stopChan:
			logrus.Info("🛑 [Windows] Остановка мониторинга по stopChan")
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
			logrus.Info("🛑 [Windows] Остановка мониторинга: pipeline изменился")
			return
		}

		if !connected || manualDisc {
			logrus.Info("🛑 [Windows] Остановка мониторинга: disconnected или manual disconnect")
			return
		}

		msg := bus.TimedPop(100 * time.Millisecond)
		if msg == nil {
			continue
		}

		switch msg.Type() {
		case gst.MessageEOS:
			logrus.Warn("⚠️ [Windows] GStreamer: конец потока (EOS)")
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
			logrus.Errorf("❌ [Windows] GStreamer ошибка: %v", err)
			gs.mutex.Lock()
			if gs.videoMode == models.VideoModeJPEGRTP && strings.Contains(fmt.Sprint(err), "Internal data stream error") {
				gs.jpegCandidateIndex++
				logrus.Warnf("⚠️ [Windows/JPEG] Decoder %s failed at runtime, switching to next JPEG candidate (index=%d)", gs.lastJPEGPipeline, gs.jpegCandidateIndex)
			}
			gs.mutex.Unlock()
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
			logrus.Warnf("⚠️ [Windows] GStreamer предупреждение: %v", warn)

		case gst.MessageStateChanged:
			old, new := msg.ParseStateChanged()
			logrus.Debugf("🔄 [Windows] GStreamer состояние: %s -> %s", old.String(), new.String())
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
		logrus.Info("🔌 [Windows] Disconnect: уже отключен")
		return nil
	}

	logrus.Info("🔌 [Windows] Отключение от RTP/UDP потока...")

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
			logrus.Info("🔌 [Windows] stopChan уже закрыт")
		default:
			close(stopChan)
			logrus.Info("🔌 [Windows] stopChan закрыт")
		}
	}

	// Отправляем EOS для корректного завершения pipeline
	if pipeline != nil {
		logrus.Info("🛑 [Windows] Отправка EOS в GStreamer pipeline...")
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
		logrus.Warn("⚠️ [Windows] Некоторые горутины не завершились в течение 2 секунд, продолжаем...")
	}

	// Останавливаем pipeline
	gs.mutex.Lock()
	if gs.pipeline != nil {
		logrus.Info("🛑 [Windows] Остановка GStreamer pipeline...")
		gs.pipeline.SetState(gst.StateNull)

		// Небольшая задержка для корректного перехода в StateNull
		time.Sleep(100 * time.Millisecond)

		// НЕ вызываем Unref() - pipeline освобождается автоматически при SetState(StateNull)
		gs.pipeline = nil
		logrus.Info("✅ [Windows] GStreamer pipeline остановлен")
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
		logrus.Info("✅ [Windows] Канал кадров очищен")
	}
	gs.mutex.Unlock()

	logrus.Info("✅ [Windows] GStreamer соединение закрыто")
	return nil
}

// attemptReconnect пытается переподключиться к RTP/UDP потоку
func (gs *GStreamerService) attemptReconnect() {
	gs.mutex.Lock()

	// Проверяем условия для переподключения
	if !gs.autoReconnect || gs.manualDisconnect || gs.isConnecting || gs.isReconnecting {
		gs.mutex.Unlock()
		logrus.Infof("🔄 [Windows] Переподключение пропущено: autoReconnect=%v, manualDisconnect=%v, isConnecting=%v, isReconnecting=%v",
			gs.autoReconnect, gs.manualDisconnect, gs.isConnecting, gs.isReconnecting)
		return
	}

	if gs.reconnectAttempts >= gs.maxReconnectAttempts {
		logrus.Errorf("❌ [Windows] Превышено максимальное количество попыток переподключения (%d)", gs.maxReconnectAttempts)
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

	logrus.Info("🧹 [Windows] Очистка старого pipeline перед переподключением...")

	// Закрываем stopChan чтобы остановить горутины
	if stopChan != nil {
		select {
		case <-stopChan:
			logrus.Info("🔌 [Windows] stopChan уже был закрыт")
		default:
			close(stopChan)
			logrus.Info("🔌 [Windows] stopChan закрыт для переподключения")
		}
	}

	// Останавливаем старый pipeline
	if oldPipeline != nil {
		logrus.Info("🛑 [Windows] Остановка старого pipeline...")
		oldPipeline.SetState(gst.StateNull)
		time.Sleep(200 * time.Millisecond) // Увеличенная задержка для полной остановки
		logrus.Info("✅ [Windows] Старый pipeline остановлен")
	}

	// Ждем завершения горутин
	logrus.Info("⏳ [Windows] Ожидание завершения горутин...")
	deadline := time.Now().Add(3 * time.Second) // Увеличено до 3 секунд
	for time.Now().Before(deadline) {
		gs.mutex.RLock()
		running := gs.frameProcessorRunning || gs.monitorRunning
		gs.mutex.RUnlock()

		if !running {
			logrus.Info("✅ [Windows] Все горутины завершены")
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Проверяем, завершились ли горутины
	gs.mutex.RLock()
	stillRunning := gs.frameProcessorRunning || gs.monitorRunning
	gs.mutex.RUnlock()

	if stillRunning {
		logrus.Warn("⚠️ [Windows] Некоторые горутины все еще работают, но продолжаем переподключение")
	}

	gs.mutex.Lock()
	gs.reconnectAttempts++
	attempt := gs.reconnectAttempts
	maxAttempts := gs.maxReconnectAttempts
	gs.mutex.Unlock()

	logrus.Infof("🔄 [Windows] Попытка переподключения GStreamer #%d/%d...", attempt, maxAttempts)

	// Задержка перед переподключением (увеличивается с каждой попыткой)
	delay := time.Duration(attempt) * 2 * time.Second
	if delay > 10*time.Second {
		delay = 10 * time.Second // Максимум 10 секунд
	}
	logrus.Infof("⏳ [Windows] Задержка перед переподключением: %v", delay)
	time.Sleep(delay)

	gs.mutex.RLock()
	abortReconnect := !gs.autoReconnect || gs.manualDisconnect
	gs.mutex.RUnlock()
	if abortReconnect {
		logrus.Info("🛑 [Windows] Переподключение отменено до нового ConnectToRTP")
		gs.mutex.Lock()
		gs.isReconnecting = false
		gs.mutex.Unlock()
		return
	}

	if err := gs.ConnectToRTP(); err != nil {
		logrus.Errorf("❌ [Windows] Ошибка переподключения GStreamer #%d: %v", attempt, err)
		gs.mutex.Lock()
		gs.isReconnecting = false
		gs.mutex.Unlock()

		// Пробуем еще раз если не достигли лимита
		if attempt < maxAttempts {
			logrus.Info("🔄 [Windows] Запланирована следующая попытка переподключения...")
			// Запускаем следующую попытку асинхронно
			go gs.attemptReconnect()
		}
	} else {
		logrus.Info("✅ [Windows] Успешное переподключение GStreamer!")
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
	logrus.Debugf("🔧 [Windows] GStreamer сервис: хост обновлен на %s", host)
}

// UpdateVideoPort обновляет порт видеопотока (RTP/UDP)
func (gs *GStreamerService) UpdateVideoPort(port int) {
	gs.mutex.Lock()
	defer gs.mutex.Unlock()

	gs.config.VideoUDPPort = port
	logrus.Debugf("🔧 [Windows] GStreamer сервис: видео UDP порт обновлен на %d", port)
}

// UpdateVideoUDPPort обновляет порт приёма UDP видео
func (gs *GStreamerService) UpdateVideoUDPPort(port int) {
	gs.mutex.Lock()
	defer gs.mutex.Unlock()

	gs.config.VideoUDPPort = port
	logrus.Debugf("🔧 [Windows] GStreamer сервис: видео UDP порт обновлен на %d", port)
}

func (gs *GStreamerService) SetVideoMode(mode string) {
	gs.mutex.Lock()
	defer gs.mutex.Unlock()
	if mode == "" {
		mode = models.VideoModeH264
	}
	gs.videoMode = mode
}

func (gs *GStreamerService) SetExpectedVideoSize(width, height int) {
	gs.mutex.Lock()
	defer gs.mutex.Unlock()
	if width > 0 {
		gs.expectedWidth = width
	}
	if height > 0 {
		gs.expectedHeight = height
	}
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
			logrus.Infof("✅ [Windows] native fullscreen started via %s", candidate.name)
			return nil
		}
		lastErr = err
		logrus.Warnf("⚠️ [Windows] native fullscreen candidate %s failed: %v", candidate.name, err)
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
	} else if gs.videoMode == models.VideoModeRawYUYV {
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
		base = append(base,
			fmt.Sprintf(`caps=application/x-rtp,media=video,clock-rate=90000,encoding-name=RAW,sampling=YCbCr-4:2:2,depth=8,width=%d,height=%d,colorimetry=BT601-5,payload=96`, width, height),
			"!",
			"rtpjitterbuffer", "latency=10", "faststart-min-packets=1", "drop-on-latency=true",
			"!",
			"rtpvrawdepay",
			"!",
			"d3d11upload",
			"!",
			"d3d11convert",
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
		{name: "d3d11videosink-fullscreen", args: append(append([]string{}, base...), "d3d11videosink", "fullscreen-toggle-mode=property", "fullscreen=true", "sync=false")},
		{name: "d3d11videosink", args: append(append([]string{}, base...), "d3d11videosink", "sync=false")},
		{name: "glimagesink", args: append(append([]string{}, base...), "glimagesink", "fullscreen=true", "sync=false")},
		{name: "autovideosink", args: append(append([]string{}, base...), "autovideosink", "sync=false")},
	}
}

func (gs *GStreamerService) startNativeFullscreenProcess(candidate nativeFullscreenCandidate) (*exec.Cmd, error) {
	path, err := findBundledGStreamerTool("gst-launch-1.0")
	if err != nil {
		return nil, fmt.Errorf("gst-launch-1.0 not found: %w", err)
	}

	cmd := exec.Command(path, candidate.args...)
	cmd.Env = getWindowsNativeFullscreenEnv(path)
	var stderr bytes.Buffer
	cmd.Stdout = os.Stdout
	cmd.Stderr = &stderr

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

func findBundledGStreamerTool(name string) (string, error) {
	exePath, err := os.Executable()
	if err == nil {
		exeDir := filepath.Dir(exePath)
		candidates := []string{
			filepath.Join(exeDir, "bin", name+".exe"),
			filepath.Join(exeDir, "bin", name),
			filepath.Join(exeDir, name+".exe"),
			filepath.Join(exeDir, name),
		}
		for _, candidate := range candidates {
			if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() {
				return candidate, nil
			}
		}
	}

	return exec.LookPath(name)
}

func getWindowsNativeFullscreenEnv(gstLaunchPath string) []string {
	env := os.Environ()
	for _, pluginDir := range nativeFullscreenPluginDirs(gstLaunchPath) {
		env = appendOrPrependEnv(env, "GST_PLUGIN_PATH", pluginDir)
		env = appendOrPrependEnv(env, "GST_PLUGIN_SYSTEM_PATH", pluginDir)
		logrus.Infof("🔧 [Windows] native fullscreen GST plugin dir: %s", pluginDir)
	}

	baseDir := filepath.Dir(filepath.Clean(gstLaunchPath))
	scannerCandidates := []string{
		filepath.Join(baseDir, "..", "libexec", "gstreamer-1.0", "gst-plugin-scanner.exe"),
		filepath.Join(baseDir, "..", "libexec", "gstreamer-1.0", "gst-plugin-scanner"),
		filepath.Join(baseDir, "gst-plugin-scanner.exe"),
		filepath.Join(baseDir, "gst-plugin-scanner"),
	}
	for _, scannerPath := range scannerCandidates {
		if info, err := os.Stat(scannerPath); err == nil && !info.IsDir() {
			env = appendOrPrependEnv(env, "GST_PLUGIN_SCANNER", scannerPath)
			logrus.Infof("🔧 [Windows] native fullscreen GST plugin scanner: %s", scannerPath)
			break
		}
	}

	env = appendOrPrependEnv(env, "PATH", baseDir)
	return env
}

func nativeFullscreenPluginDirs(gstLaunchPath string) []string {
	baseDir := filepath.Dir(filepath.Clean(gstLaunchPath))
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
	logrus.Info("🔄 [Windows] Принудительное переподключение (смена устройства)...")

	// Сначала отключаемся
	if err := gs.Disconnect(); err != nil {
		logrus.Warnf("⚠️ [Windows] Ошибка при отключении перед переподключением: %v", err)
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
	logrus.Info("🔗 [Windows] Подключаемся к новому устройству...")
	return gs.ConnectToRTP()
}
