//go:build !android
// +build !android

package service

import (
	"bufio"
	"fmt"
	"image"
	"io"
	"os/exec"
	"sync"
	"time"

	"usbridge-client/internal/models"

	"github.com/sirupsen/logrus"
)

// GStreamerProcessService сервис через внешний процесс gst-launch
type GStreamerProcessService struct {
	config *models.AppConfig

	// Процесс GStreamer
	cmd    *exec.Cmd
	stdout io.ReadCloser

	// Состояние
	isConnected  bool
	isConnecting bool

	// Автоматическое переподключение
	autoReconnect        bool
	reconnectAttempts    int
	maxReconnectAttempts int
	manualDisconnect     bool

	// Каналы для видео кадров
	videoFrameChan chan image.Image
	stopChan       chan struct{}

	// Статистика
	frameDropCount int64
	lastFrameTime  time.Time
	frameCount     int64

	// Размеры кадра
	width  int
	height int

	// Мьютексы
	mutex sync.RWMutex

	// Callbacks
	onFrameReceived func(image.Image)
	onStateChanged  func(string)
	onError         func(error)
}

// NewGStreamerProcessService создает новый сервис через процесс
func NewGStreamerProcessService(config *models.AppConfig) *GStreamerProcessService {
	return &GStreamerProcessService{
		config:               config,
		videoFrameChan:       make(chan image.Image, config.BufferSize),
		stopChan:             make(chan struct{}),
		autoReconnect:        true,
		reconnectAttempts:    0,
		maxReconnectAttempts: 5,
		width:                640,  // По умолчанию
		height:               480, // По умолчанию
	}
}

// ConnectToRTSP подключается к RTSP потоку через gst-launch
func (gs *GStreamerProcessService) ConnectToRTSP() error {
	gs.mutex.Lock()
	defer gs.mutex.Unlock()

	if gs.isConnecting || gs.isConnected {
		return fmt.Errorf("уже подключен или подключается")
	}

	gs.manualDisconnect = false
	gs.frameDropCount = 0
	gs.lastFrameTime = time.Time{}

	gs.isConnecting = true
	logrus.Info("🔗 Подключение к RTSP потоку через gst-launch...")

	// Формируем RTSP URL
	rtspURL := fmt.Sprintf("rtsp://%s:%d/stream", gs.config.MediaMTXHost, gs.config.MediaMTXRTSP)
	logrus.Infof("📹 RTSP URL: %s", rtspURL)

	// Создаем команду gst-launch с выводом в stdout
	pipeline := fmt.Sprintf(
		"rtspsrc location=%s latency=0 protocols=tcp ! "+
			"rtph264depay ! "+
			"h264parse ! "+
			"avdec_h264 ! "+
			"videoscale ! "+
			"video/x-raw,width=%d,height=%d ! "+
			"videoconvert ! "+
			"video/x-raw,format=RGBA ! "+
			"fdsink fd=1",
		rtspURL,
		gs.width,
		gs.height,
	)

	logrus.Infof("🔧 GStreamer pipeline: %s", pipeline)

	gs.cmd = exec.Command("gst-launch-1.0", "-q", pipeline)

	// Получаем stdout
	stdout, err := gs.cmd.StdoutPipe()
	if err != nil {
		gs.isConnecting = false
		return fmt.Errorf("ошибка создания stdout pipe: %v", err)
	}
	gs.stdout = stdout

	// Запускаем процесс
	if err := gs.cmd.Start(); err != nil {
		gs.isConnecting = false
		return fmt.Errorf("ошибка запуска gst-launch: %v", err)
	}

	// Запускаем чтение кадров
	go gs.readFrames()

	// Запускаем обработку кадров
	go gs.processVideoFrames()

	gs.isConnecting = false
	gs.isConnected = true
	logrus.Info("✅ GStreamer RTSP подключение установлено")
	return nil
}

// readFrames читает RGBA кадры из stdout
func (gs *GStreamerProcessService) readFrames() {
	logrus.Info("🎞️ Начало чтения кадров из gst-launch")

	frameSize := gs.width * gs.height * 4 // RGBA
	buffer := make([]byte, frameSize)
	reader := bufio.NewReader(gs.stdout)

	for gs.isConnected {
		// Читаем ровно один кадр
		n, err := io.ReadFull(reader, buffer)
		if err != nil {
			if err == io.EOF {
				logrus.Warn("⚠️ EOF - поток завершён")
				break
			}
			logrus.Errorf("❌ Ошибка чтения кадра: %v", err)
			break
		}

		if n != frameSize {
			logrus.Warnf("⚠️ Неполный кадр: %d байт (ожидается %d)", n, frameSize)
			continue
		}

		// Копируем данные
		frameCopy := make([]byte, frameSize)
		copy(frameCopy, buffer)

		// Конвертируем в image.Image
		img := gs.rgbaToImage(frameCopy, gs.width, gs.height)
		if img != nil {
			gs.mutex.Lock()
			gs.frameCount++
			gs.lastFrameTime = time.Now()
			frameNum := gs.frameCount
			gs.mutex.Unlock()

			// Логируем первые 10 кадров
			if frameNum <= 10 {
				logrus.Infof("🎬 GStreamer получил кадр #%d (%dx%d)", frameNum, gs.width, gs.height)
			}

			// Отправляем кадр в канал
			gs.sendFrameWithDrop(img)
		}
	}

	logrus.Info("🛑 Остановка чтения кадров из gst-launch")
}

// rgbaToImage конвертирует RGBA данные в image.Image
func (gs *GStreamerProcessService) rgbaToImage(data []byte, width, height int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	copy(img.Pix, data)
	return img
}

// sendFrameWithDrop отправляет кадр с умным сбросом накопленных кадров
func (gs *GStreamerProcessService) sendFrameWithDrop(frame image.Image) {
	gs.mutex.Lock()
	defer gs.mutex.Unlock()

	now := time.Now()

	// Если включен режим низкой задержки, умно сбрасываем накопленные кадры
	if gs.config.LowLatencyMode {
		bufferedCount := 0
		for {
			select {
			case <-gs.videoFrameChan:
				bufferedCount++
				gs.frameDropCount++
			default:
				goto sendNewFrame
			}
		}

	sendNewFrame:
		if bufferedCount > 0 {
			logrus.Debugf("🔥 Сброшено %d накопленных кадров для реалтайма", bufferedCount)
		}
	}

	// Отправляем новый кадр
	select {
	case gs.videoFrameChan <- frame:
		gs.lastFrameTime = now
	default:
		// Если буфер полон, заменяем один старый кадр новым
		select {
		case <-gs.videoFrameChan:
			gs.videoFrameChan <- frame
			gs.frameDropCount++
			gs.lastFrameTime = now
		default:
			gs.frameDropCount++
		}
	}
}

// processVideoFrames обрабатывает видео кадры
func (gs *GStreamerProcessService) processVideoFrames() {
	logrus.Info("🎞️ Начало обработки видео кадров GStreamer")
	processedFrameCount := 0

	for {
		select {
		case <-gs.stopChan:
			logrus.Info("🛑 Остановка обработки видео кадров")
			return
		case frame := <-gs.videoFrameChan:
			processedFrameCount++

			if processedFrameCount%30 == 1 {
				logrus.Debugf("GStreamer: кадр #%d -> UI", processedFrameCount)
			}

			if processedFrameCount <= 10 {
				logrus.Infof("🎞️ GStreamer обрабатывает кадр #%d", processedFrameCount)
			}

			if gs.onFrameReceived != nil {
				gs.onFrameReceived(frame)
			}
		}
	}
}

// Disconnect отключается от RTSP потока
func (gs *GStreamerProcessService) Disconnect() error {
	gs.mutex.Lock()
	defer gs.mutex.Unlock()

	if !gs.isConnected {
		return nil
	}

	logrus.Info("🔌 Отключение от RTSP потока...")

	gs.manualDisconnect = true

	// Останавливаем обработку кадров
	select {
	case gs.stopChan <- struct{}{}:
	default:
	}

	// Останавливаем процесс
	if gs.cmd != nil && gs.cmd.Process != nil {
		gs.cmd.Process.Kill()
		gs.cmd.Wait()
		gs.cmd = nil
	}

	gs.isConnected = false
	gs.isConnecting = false

	logrus.Info("✅ GStreamer соединение закрыто")
	return nil
}

// SetOnFrameReceived устанавливает callback для получения кадров
func (gs *GStreamerProcessService) SetOnFrameReceived(callback func(image.Image)) {
	gs.onFrameReceived = callback
}

// SetOnStateChanged устанавливает callback для изменения состояния
func (gs *GStreamerProcessService) SetOnStateChanged(callback func(string)) {
	gs.onStateChanged = callback
}

// SetOnError устанавливает callback для ошибок
func (gs *GStreamerProcessService) SetOnError(callback func(error)) {
	gs.onError = callback
}

// IsConnected возвращает состояние подключения
func (gs *GStreamerProcessService) IsConnected() bool {
	gs.mutex.RLock()
	defer gs.mutex.RUnlock()
	return gs.isConnected
}

// GetStats возвращает статистику соединения
func (gs *GStreamerProcessService) GetStats() map[string]interface{} {
	gs.mutex.RLock()
	defer gs.mutex.RUnlock()

	return map[string]interface{}{
		"connected":        gs.isConnected,
		"connecting":       gs.isConnecting,
		"frame_count":      gs.frameCount,
		"frames_dropped":   gs.frameDropCount,
		"last_frame_time":  gs.lastFrameTime,
		"low_latency_mode": gs.config.LowLatencyMode,
	}
}

// UpdateHost обновляет хост MediaMTX
func (gs *GStreamerProcessService) UpdateHost(host string) {
	gs.mutex.Lock()
	defer gs.mutex.Unlock()

	gs.config.MediaMTXHost = host
	logrus.Infof("🔧 GStreamer сервис: хост обновлен на %s", host)
}

// GetConfig возвращает конфигурацию
func (gs *GStreamerProcessService) GetConfig() *models.AppConfig {
	gs.mutex.RLock()
	defer gs.mutex.RUnlock()
	return gs.config
}

// SetAutoReconnect включает/выключает автоматическое переподключение
func (gs *GStreamerProcessService) SetAutoReconnect(enabled bool) {
	gs.mutex.Lock()
	defer gs.mutex.Unlock()
	gs.autoReconnect = enabled
}

// SetMaxReconnectAttempts устанавливает максимальное количество попыток переподключения
func (gs *GStreamerProcessService) SetMaxReconnectAttempts(max int) {
	gs.mutex.Lock()
	defer gs.mutex.Unlock()
	gs.maxReconnectAttempts = max
}
