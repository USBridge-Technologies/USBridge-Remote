//go:build darwin
// +build darwin

package service

import (
	"bufio"
	"fmt"
	"image"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"usbridge-client/internal/models"

	"github.com/sirupsen/logrus"
)

// GStreamerService сервис для работы с GStreamer на macOS через внешний процесс
type GStreamerService struct {
	config *models.AppConfig

	// Процесс GStreamer
	cmd    *exec.Cmd
	stdout io.ReadCloser

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
	frameChan chan image.Image
	stop      *stopSignal // безопасное однократное закрытие

	// Флаги для управления горутинами
	frameProcessorRunning bool
	frameReaderRunning    bool
	monitorRunning        bool

	// Статистика
	lastFrameTime time.Time
	frameCount    int64
	framesDropped int64

	// Размеры кадра (по умолчанию 1280x720 для HD захвата)
	width  int
	height int

	// Мьютексы
	mutex sync.RWMutex

	// Callbacks
	onFrameReceived func(image.Image)
	onStateChanged  func(string)
	onError         func(error)
}

// NewGStreamerService создает новый GStreamer сервис для macOS
func NewGStreamerService(config *models.AppConfig) *GStreamerService {
	gs := &GStreamerService{
		config:                config,
		autoReconnect:         true,
		reconnectAttempts:     0,
		maxReconnectAttempts:  5,
		frameChan:             make(chan image.Image, 1),
		stop:                  newStopSignal(),
		frameProcessorRunning: false,
		frameReaderRunning:    false,
		monitorRunning:        false,
		width:                 1920,
		height:                1080,
	}

	logrus.Info("✅ GStreamer сервис для macOS инициализирован (внешний процесс)")
	return gs
}

// getGStreamerEnv возвращает окружение для gst-launch с путями к плагинам (applemedia/vtdec)
func (gs *GStreamerService) getGStreamerEnv() []string {
	env := os.Environ()
	prefix := "GST_PLUGIN_PATH="
	for _, pluginPath := range []string{
		"/opt/homebrew/lib/gstreamer-1.0",
		"/usr/local/lib/gstreamer-1.0",
	} {
		if info, err := os.Stat(pluginPath); err == nil && info.IsDir() {
			existing := os.Getenv("GST_PLUGIN_PATH")
			val := pluginPath
			if existing != "" {
				val = pluginPath + ":" + existing
			}
			// Убираем старый GST_PLUGIN_PATH и добавляем новый
			filtered := make([]string, 0, len(env)+1)
			for _, e := range env {
				if !strings.HasPrefix(e, prefix) {
					filtered = append(filtered, e)
				}
			}
			filtered = append(filtered, prefix+val)
			logrus.Infof("🔧 macOS: GST_PLUGIN_PATH=%s (vtdec/applemedia)", pluginPath)
			return filtered
		}
	}
	return env
}

// buildPipelineArgs формирует аргументы pipeline для RTP H.264 (через QUIC/SUDP туннель)
func (gs *GStreamerService) buildPipelineArgs(udpPort int) []string {
	// Низкая задержка: udpsrc (малый буфер) -> rtpjitterbuffer latency=50 -> rtph264depay -> vtdec -> fdsink
	return []string{
		"-q",
		"udpsrc",
		fmt.Sprintf("port=%d", udpPort),
		"buffer-size=131072", // 128KB вместо 2MB — меньше буферизация
		`caps=application/x-rtp,media=video,encoding-name=H264,payload=96`,
		"!",
		"rtpjitterbuffer",
		"latency=50", // 50ms jitter — компромисс между задержкой и устойчивостью
		"faststart-min-packets=3",
		"!",
		"rtph264depay",
		"!",
		"h264parse",
		"config-interval=-1",
		"!",
		"video/x-h264,stream-format=avc,alignment=au",
		"!",
		"vtdec",
		"!",
		"videoscale",
		"!",
		fmt.Sprintf("video/x-raw,width=%d,height=%d", gs.width, gs.height),
		"!",
		"videoconvert",
		"!",
		"video/x-raw,format=RGBA",
		"!",
		"fdsink",
		"fd=1",
		"sync=false",
	}
}

// buildPipelineArgsPipe — fdsrc для RTP H.264 из pipe (UDP relay с keepalive)
func (gs *GStreamerService) buildPipelineArgsPipe(fd int) []string {
	return []string{
		"-q",
		"fdsrc",
		fmt.Sprintf("fd=%d", fd),
		`caps=application/x-rtp,media=video,encoding-name=H264,payload=96`,
		"!",
		"rtpjitterbuffer",
		"latency=50",
		"faststart-min-packets=3",
		"!",
		"rtph264depay",
		"!",
		"h264parse",
		"config-interval=-1",
		"!",
		"video/x-h264,stream-format=avc,alignment=au",
		"!",
		"vtdec",
		"!",
		"videoscale",
		"!",
		fmt.Sprintf("video/x-raw,width=%d,height=%d", gs.width, gs.height),
		"!",
		"videoconvert",
		"!",
		"video/x-raw,format=RGBA",
		"!",
		"fdsink",
		"fd=1",
		"sync=false",
	}
}

// ConnectToUDP подключается к UDP H.264 потоку (новый протокол, минимальная задержка)
func (gs *GStreamerService) ConnectToUDP(udpPort int) error {
	gs.mutex.Lock()

	if gs.isConnecting || gs.isConnected {
		gs.mutex.Unlock()
		logrus.Warnf("⚠️ macOS: Уже подключен или подключается (isConnecting=%v, isConnected=%v)", gs.isConnecting, gs.isConnected)
		return fmt.Errorf("уже подключен или подключается")
	}

	gs.manualDisconnect = false
	gs.lastFrameTime = time.Time{}

	// Сигнализируем остановку и ждём завершения горутин
	if gs.stop != nil {
		gs.stop.signal()
	}
	for gs.frameProcessorRunning || gs.frameReaderRunning || gs.monitorRunning {
		gs.mutex.Unlock()
		time.Sleep(50 * time.Millisecond)
		gs.mutex.Lock()
	}

	gs.stop = newStopSignal()

	// Запускаем новый обработчик кадров ОДИН РАЗ
	gs.frameProcessorRunning = true
	go gs.frameProcessor()
	logrus.Info("🔍 macOS DEBUG: frameProcessor запущен")

	gs.isConnecting = true
	gs.mutex.Unlock()

	logrus.Infof("🔗 [VIDEO] Шаг 2: Подключение к RTP H.264 (port=%d)", udpPort)
	return gs.runGStreamerPipeline(gs.buildPipelineArgs(udpPort), nil)
}

// ConnectToUDPViaPipe подключается к H.264 через pipe (UDP relay с keepalive для FRP).
func (gs *GStreamerService) ConnectToUDPViaPipe(pipeReader *os.File) error {
	gs.mutex.Lock()
	if gs.isConnecting || gs.isConnected {
		gs.mutex.Unlock()
		return fmt.Errorf("уже подключен или подключается")
	}
	gs.manualDisconnect = false
	gs.lastFrameTime = time.Time{}
	if gs.stop != nil {
		gs.stop.signal()
	}
	for gs.frameProcessorRunning || gs.frameReaderRunning || gs.monitorRunning {
		gs.mutex.Unlock()
		time.Sleep(50 * time.Millisecond)
		gs.mutex.Lock()
	}
	gs.stop = newStopSignal()
	gs.frameProcessorRunning = true
	go gs.frameProcessor()
	gs.isConnecting = true
	gs.mutex.Unlock()

	logrus.Info("🔗 [VIDEO] Подключение через pipe (UDP relay, raw H.264)")
	// ExtraFiles[0] → fd 3 в дочернем процессе
	return gs.runGStreamerPipeline(gs.buildPipelineArgsPipe(3), pipeReader)
}

// killStaleGStreamerProcesses убивает осиротевшие gst-launch процессы на том же UDP-порту.
// Без этого старый зомби-процесс перехватывает все UDP-пакеты и новый GStreamer ничего не получает.
func (gs *GStreamerService) killStaleGStreamerProcesses(udpPort int) {
	myPID := os.Getpid()
	portStr := fmt.Sprintf("port=%d", udpPort)

	// Ищем все gst-launch процессы
	out, err := exec.Command("pgrep", "-f", "gst-launch.*udpsrc").Output()
	if err != nil {
		return // нет процессов — нечего убивать
	}

	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		pid, err := strconv.Atoi(strings.TrimSpace(line))
		if err != nil || pid == 0 {
			continue
		}

		// Не убиваем свой текущий дочерний процесс
		gs.mutex.RLock()
		ownCmd := gs.cmd
		gs.mutex.RUnlock()
		if ownCmd != nil && ownCmd.Process != nil && ownCmd.Process.Pid == pid {
			continue
		}

		// Проверяем командную строку — совпадает ли порт
		cmdline, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "command=").Output()
		if err != nil {
			continue
		}
		cmdStr := string(cmdline)
		if !strings.Contains(cmdStr, portStr) {
			continue
		}

		// Проверяем PPID — осиротевший (PPID=1) или чужой (PPID != наш PID)
		ppidOut, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "ppid=").Output()
		if err != nil {
			continue
		}
		ppid, err := strconv.Atoi(strings.TrimSpace(string(ppidOut)))
		if err != nil {
			continue
		}
		if ppid == myPID {
			continue // наш дочерний — не трогаем
		}

		logrus.Warnf("🧹 Убиваем старый gst-launch (PID=%d, PPID=%d) на порту %d", pid, ppid, udpPort)
		if proc, err := os.FindProcess(pid); err == nil {
			_ = proc.Signal(syscall.SIGTERM)
			// Даём время на graceful shutdown, потом SIGKILL
			go func(p *os.Process) {
				time.Sleep(500 * time.Millisecond)
				_ = p.Signal(syscall.SIGKILL)
			}(proc)
		}
	}
}

// runGStreamerPipeline запускает gst-launch. pipeReader != nil → fdsrc (ExtraFiles[0]=fd 3).
func (gs *GStreamerService) runGStreamerPipeline(pipelineArgs []string, pipeReader *os.File) error {
	logrus.Infof("🔧 macOS: GStreamer pipeline: gst-launch-1.0 %v", pipelineArgs)

	gstLaunchPath, err := exec.LookPath("gst-launch-1.0")
	if err != nil {
		for _, p := range []string{"/opt/homebrew/bin/gst-launch-1.0", "/usr/local/bin/gst-launch-1.0"} {
			if info, err := os.Stat(p); err == nil && !info.IsDir() && info.Mode()&0111 != 0 {
				gstLaunchPath = p
				break
			}
		}
	}
	if gstLaunchPath == "" {
		gs.mutex.Lock()
		gs.isConnecting = false
		gs.frameProcessorRunning = false
		gs.mutex.Unlock()
		if gs.stop != nil {
			gs.stop.signal()
		}
		return fmt.Errorf("gst-launch-1.0 не найден. Установите: brew install gstreamer gst-plugins-base gst-plugins-good gst-plugins-bad")
	}

	// Убиваем осиротевшие gst-launch на нашем порту (зомби от предыдущих запусков перехватывают UDP)
	for _, arg := range pipelineArgs {
		if strings.HasPrefix(arg, "port=") {
			if p, err := strconv.Atoi(strings.TrimPrefix(arg, "port=")); err == nil && p > 0 {
				gs.killStaleGStreamerProcesses(p)
				time.Sleep(200 * time.Millisecond) // даём ОС время освободить сокет
				break
			}
		}
	}

	gs.mutex.Lock()
	gs.cmd = exec.Command(gstLaunchPath, pipelineArgs...)
	gs.cmd.Env = gs.getGStreamerEnv()
	if pipeReader != nil {
		gs.cmd.ExtraFiles = []*os.File{pipeReader}
	}
	gs.mutex.Unlock()

	stdout, err := gs.cmd.StdoutPipe()
	if err != nil {
		gs.mutex.Lock()
		gs.isConnecting = false
		gs.frameProcessorRunning = false
		gs.mutex.Unlock()
		if gs.stop != nil {
			gs.stop.signal()
		}
		logrus.Errorf("❌ macOS: Ошибка создания stdout pipe: %v", err)
		return fmt.Errorf("ошибка создания stdout pipe: %v", err)
	}

	stderr, err := gs.cmd.StderrPipe()
	if err != nil {
		gs.mutex.Lock()
		gs.isConnecting = false
		gs.frameProcessorRunning = false
		gs.mutex.Unlock()
		if gs.stop != nil {
			gs.stop.signal()
		}
		logrus.Errorf("❌ macOS: Ошибка создания stderr pipe: %v", err)
		return fmt.Errorf("ошибка создания stderr pipe: %v", err)
	}

	gs.mutex.Lock()
	gs.stdout = stdout
	gs.mutex.Unlock()

	// Запускаем горутину для чтения stderr (для диагностики)
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text()
			// Пропускаем безвредные предупреждения GTK (дубликаты классов при gtk3+gtk4)
			if strings.Contains(line, "implemented in both") && strings.Contains(line, "libgtk") {
				continue
			}
			logrus.Warnf("📺 gst-launch stderr: %s", line)
		}
	}()


	if err := gs.cmd.Start(); err != nil {
		gs.mutex.Lock()
		gs.isConnecting = false
		gs.frameProcessorRunning = false
		gs.mutex.Unlock()
		if gs.stop != nil {
			gs.stop.signal()
		}
		logrus.Errorf("❌ macOS: Ошибка запуска gst-launch-1.0: %v", err)
		return fmt.Errorf("ошибка запуска gst-launch-1.0: %v", err)
	}

	logrus.Infof("✅ macOS: gst-launch-1.0 процесс запущен (PID: %d)", gs.cmd.Process.Pid)

	// Запускаем чтение кадров
	gs.mutex.Lock()
	gs.frameReaderRunning = true
	gs.mutex.Unlock()
	go gs.readFrames()

	// Запускаем мониторинг процесса
	gs.mutex.Lock()
	gs.monitorRunning = true
	gs.mutex.Unlock()
	go gs.monitorProcess()

	// НЕ устанавливаем isConnected = true здесь!
	// Это будет сделано в readFrames() после получения первого кадра
	gs.mutex.Lock()
	gs.isConnecting = false
	// isConnected останется false до получения первого кадра
	gs.mutex.Unlock()

	logrus.Info("🔗 [VIDEO] Шаг 3: GStreamer процесс запущен, ожидание RTP→rtph264depay→h264parse→vtdec→fdsink...")
	return nil
}

// firstByteLogReader оборачивает io.Reader и логирует при первом получении байтов
type firstByteLogReader struct {
	io.Reader
	port int
	once sync.Once
}

func (r *firstByteLogReader) Read(p []byte) (n int, err error) {
	n, err = r.Reader.Read(p)
	r.once.Do(func() {
		if n > 0 {
			logrus.Infof("📥 [VIDEO] Шаг 5: Первые байты от GStreamer (%d байт) — udpsrc RTP получает данные", n)
		}
	})
	return n, err
}

// readFrames читает RGBA кадры из stdout gst-launch
func (gs *GStreamerService) readFrames() {
	defer func() {
		gs.mutex.Lock()
		gs.frameReaderRunning = false
		gs.isConnected = false // Важно! Сбрасываем флаг при завершении
		gs.mutex.Unlock()
		logrus.Info("🛑 macOS: readFrames завершен")
	}()

	gs.mutex.RLock()
	width := gs.width
	height := gs.height
	stdout := gs.stdout
	done := gs.stop.done()
	udpPort := 0
	if gs.config != nil {
		udpPort = gs.config.VideoUDPPort
	}
	gs.mutex.RUnlock()

	logrus.Infof("🎬 [VIDEO] Шаг 1: readFrames запущен (udpsrc RTP port=%d, кадр=%dx%d RGBA)", udpPort, width, height)

	frameSize := width * height * 4 // RGBA
	buffer := make([]byte, frameSize)
	reader := bufio.NewReader(&firstByteLogReader{Reader: stdout, port: udpPort})
	firstFrameReceived := false
	waitStart := time.Now()

	// Периодический лог ожидания первого кадра (ReadFull блокируется, поэтому отдельная горутина)
	waitDone := make(chan struct{})
	var waitDoneOnce sync.Once
	closeWaitDone := func() { waitDoneOnce.Do(func() { close(waitDone) }) }
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-waitDone:
				return
			case <-done:
				return
			case <-ticker.C:
				logrus.Warnf("⚠️ [VIDEO] Шаг 4: Ожидание первого кадра (%.0f сек) — udpsrc RTP port=%d, проверьте FRP visitor→GStreamer", time.Since(waitStart).Seconds(), udpPort)
			}
		}
	}()

	for {
		select {
		case <-done:
			closeWaitDone()
			logrus.Info("🛑 macOS: Остановка чтения кадров по сигналу stop")
			return
		default:
		}

		// Читаем ровно один кадр
		n, err := io.ReadFull(reader, buffer)
		if err != nil {
			closeWaitDone()
			if err == io.EOF {
				logrus.Warn("⚠️ [VIDEO] EOF - поток завершён (GStreamer не получил данных?)")
				gs.mutex.RLock()
				shouldReconnect := !gs.manualDisconnect && gs.autoReconnect
				gs.mutex.RUnlock()
				if shouldReconnect {
					go gs.attemptReconnect()
				}
				return
			}
			logrus.Errorf("❌ [VIDEO] Ошибка чтения кадра: %v", err)
			gs.mutex.RLock()
			shouldReconnect := !gs.manualDisconnect && gs.autoReconnect
			gs.mutex.RUnlock()
			if shouldReconnect {
				go gs.attemptReconnect()
			}
			return
		}

		if n != frameSize {
			logrus.Warnf("⚠️ macOS: Неполный кадр: %d байт (ожидается %d)", n, frameSize)
			continue
		}

		img := rgbaToImage(buffer, width, height)
		if img != nil {
			gs.mutex.Lock()
			gs.frameCount++
			frameNum := gs.frameCount
			
			// Устанавливаем isConnected = true при получении первого кадра
			if !firstFrameReceived {
				gs.isConnected = true
				firstFrameReceived = true
				closeWaitDone()
				gs.mutex.Unlock()
				logrus.Info("✅ [VIDEO] Шаг 6: Первый кадр получен! Соединение установлено.")
				
				// Вызываем callback если есть
				gs.mutex.RLock()
				stateCallback := gs.onStateChanged
				gs.mutex.RUnlock()
				if stateCallback != nil {
					stateCallback("connected")
				}
				gs.mutex.Lock()
			}
			gs.mutex.Unlock()

			// Логируем каждый 300-й кадр (~10 сек при 30fps)
			if frameNum%300 == 0 {
				gs.mutex.Lock()
				dropped := gs.framesDropped
				gs.mutex.Unlock()
				chanLen := len(gs.frameChan)
				chanCap := cap(gs.frameChan)
				logrus.Infof("🎬 macOS GStreamer: %d кадров | Пропущено: %d | Канал: %d/%d", frameNum, dropped, chanLen, chanCap)
			}

			// Отправляем кадр в канал НЕБЛОКИРУЮЩИМ способом
			select {
			case gs.frameChan <- img:
				// Кадр отправлен успешно
			default:
				// Канал полон - пропускаем кадр (критично для реалтайма!)
				gs.mutex.Lock()
				gs.framesDropped++
				dropped := gs.framesDropped
				gs.mutex.Unlock()
				// Логируем каждый 30-й пропущенный кадр
				if dropped%30 == 1 {
					chanLen := len(gs.frameChan)
					logrus.Warnf("⏭️ macOS GStreamer: пропущен кадр #%d (всего пропущено: %d, канал: %d/%d)", frameNum, dropped, chanLen, cap(gs.frameChan))
				}
			}
		}
	}
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
		logrus.Info("🛑 macOS: frameProcessor завершен и очищен")
	}()

	processedCount := int64(0)
	lastLogTime := time.Now()

	done := gs.stop.done()
	for {
		select {
		case <-done:
			logrus.Info("🛑 macOS: Остановка обработчика кадров по сигналу stop")
			return
		case frame, ok := <-gs.frameChan:
			if !ok {
				logrus.Info("🛑 macOS: frameChan закрыт, остановка обработчика")
				return
			}

			processedCount++

			if time.Since(lastLogTime) > time.Second {
				chanLen := len(gs.frameChan)
				logrus.Infof("📤 macOS frameProcessor: обработано %d кадров, канал: %d/%d", processedCount, chanLen, cap(gs.frameChan))
				lastLogTime = time.Now()
			}

			gs.mutex.RLock()
			callback := gs.onFrameReceived
			gs.mutex.RUnlock()

			if callback != nil {
				gs.mutex.Lock()
				gs.lastFrameTime = time.Now()
				gs.mutex.Unlock()

				callback(frame)
			}
		}
	}
}

// monitorProcess мониторит состояние процесса GStreamer
func (gs *GStreamerService) monitorProcess() {
	defer func() {
		if r := recover(); r != nil {
			logrus.Errorf("❌ macOS: Паника в monitorProcess: %v", r)
		}

		gs.mutex.Lock()
		gs.monitorRunning = false
		gs.mutex.Unlock()
		logrus.Info("🛑 macOS: monitorProcess завершен")
	}()

	logrus.Info("📊 macOS: Запуск мониторинга gst-launch процесса")

	gs.mutex.RLock()
	cmd := gs.cmd
	done := gs.stop.done()
	gs.mutex.RUnlock()

	if cmd == nil {
		logrus.Warn("⚠️ macOS: cmd is nil, cannot monitor")
		return
	}

	err := cmd.Wait()

	gs.mutex.RLock()
	manualDisc := gs.manualDisconnect
	shouldReconnect := !gs.manualDisconnect && gs.autoReconnect
	gs.mutex.RUnlock()

	if err != nil && !manualDisc {
		logrus.Errorf("❌ macOS: gst-launch процесс завершился с ошибкой: %v", err)
		gs.mutex.RLock()
		errCallback := gs.onError
		gs.mutex.RUnlock()
		if errCallback != nil {
			errCallback(fmt.Errorf("процесс gst-launch завершился: %v", err))
		}
	} else if !manualDisc {
		logrus.Warn("⚠️ macOS: gst-launch процесс завершился")
	}

	select {
	case <-done:
		logrus.Info("🛑 macOS: Остановка мониторинга по stop")
		return
	default:
	}

	// Запускаем переподключение если нужно
	if shouldReconnect {
		logrus.Info("🔄 macOS: Запуск автоматического переподключения...")
		go gs.attemptReconnect()
	}
}

// Disconnect отключается от RTSP потока
func (gs *GStreamerService) Disconnect() error {
	gs.mutex.Lock()

	if !gs.isConnected && !gs.isConnecting {
		gs.mutex.Unlock()
		logrus.Info("🔌 macOS: Disconnect: уже отключен")
		return nil
	}

	logrus.Info("🔌 macOS: Отключение от RTSP потока...")

	gs.manualDisconnect = true
	gs.isConnected = false
	gs.isConnecting = false

	cmd := gs.cmd
	stop := gs.stop
	gs.mutex.Unlock()

	if stop != nil {
		stop.signal()
	}

	// Останавливаем процесс gst-launch
	if cmd != nil && cmd.Process != nil {
		logrus.Info("🛑 macOS: Остановка gst-launch процесса...")
		cmd.Process.Kill()
		cmd.Wait()
		logrus.Info("✅ macOS: gst-launch процесс остановлен")
	}

	// Ждем завершения горутин (не более 2 секунд)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		gs.mutex.RLock()
		running := gs.frameProcessorRunning || gs.frameReaderRunning || gs.monitorRunning
		gs.mutex.RUnlock()

		if !running {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Проверяем, завершились ли горутины
	gs.mutex.RLock()
	stillRunning := gs.frameProcessorRunning || gs.frameReaderRunning || gs.monitorRunning
	gs.mutex.RUnlock()

	if stillRunning {
		logrus.Warn("⚠️ macOS: Некоторые горутины не завершились в течение 2 секунд, продолжаем...")
	}

	// Очищаем
	gs.mutex.Lock()
	gs.cmd = nil
	gs.stdout = nil

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
		logrus.Info("✅ macOS: Канал кадров очищен")
	}
	gs.mutex.Unlock()

	logrus.Info("✅ macOS: GStreamer соединение закрыто")
	return nil
}

// attemptReconnect пытается переподключиться к RTSP потоку
func (gs *GStreamerService) attemptReconnect() {
	gs.mutex.Lock()

	// Проверяем условия для переподключения
	if !gs.autoReconnect || gs.manualDisconnect || gs.isConnecting || gs.isReconnecting {
		gs.mutex.Unlock()
		logrus.Infof("🔄 macOS: Переподключение пропущено: autoReconnect=%v, manualDisconnect=%v, isConnecting=%v, isReconnecting=%v",
			gs.autoReconnect, gs.manualDisconnect, gs.isConnecting, gs.isReconnecting)
		return
	}

	if gs.reconnectAttempts >= gs.maxReconnectAttempts {
		logrus.Errorf("❌ macOS: Превышено максимальное количество попыток переподключения (%d)", gs.maxReconnectAttempts)
		gs.autoReconnect = false
		gs.mutex.Unlock()
		return
	}

	// Устанавливаем флаг переподключения
	gs.isReconnecting = true
	gs.isConnected = false
	gs.isConnecting = false

	oldCmd := gs.cmd
	stop := gs.stop
	gs.cmd = nil
	gs.mutex.Unlock()

	logrus.Info("🧹 macOS: Очистка старого процесса перед переподключением...")

	if stop != nil {
		stop.signal()
	}

	// Останавливаем старый процесс
	if oldCmd != nil && oldCmd.Process != nil {
		logrus.Info("🛑 macOS: Остановка старого процесса...")
		oldCmd.Process.Kill()
		oldCmd.Wait()
		time.Sleep(200 * time.Millisecond)
		logrus.Info("✅ macOS: Старый процесс остановлен")
	}

	// Ждем завершения горутин
	logrus.Info("⏳ macOS: Ожидание завершения горутин...")
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		gs.mutex.RLock()
		running := gs.frameProcessorRunning || gs.frameReaderRunning || gs.monitorRunning
		gs.mutex.RUnlock()

		if !running {
			logrus.Info("✅ macOS: Все горутины завершены")
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	gs.mutex.Lock()
	gs.reconnectAttempts++
	attempt := gs.reconnectAttempts
	maxAttempts := gs.maxReconnectAttempts
	gs.mutex.Unlock()

	logrus.Infof("🔄 macOS: Попытка переподключения GStreamer #%d/%d...", attempt, maxAttempts)

	// Задержка перед переподключением
	delay := time.Duration(attempt) * 2 * time.Second
	if delay > 10*time.Second {
		delay = 10 * time.Second
	}
	logrus.Infof("⏳ macOS: Задержка перед переподключением: %v", delay)
	time.Sleep(delay)

	if err := gs.ConnectToRTSP(); err != nil {
		logrus.Errorf("❌ macOS: Ошибка переподключения GStreamer #%d: %v", attempt, err)
		gs.mutex.Lock()
		gs.isReconnecting = false
		gs.mutex.Unlock()

		// Пробуем еще раз если не достигли лимита
		if attempt < maxAttempts {
			logrus.Info("🔄 macOS: Запланирована следующая попытка переподключения...")
			go gs.attemptReconnect()
		}
	} else {
		logrus.Info("✅ macOS: Успешное переподключение GStreamer!")
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

// UpdateHost обновляет хост MediaMTX
func (gs *GStreamerService) UpdateHost(host string) {
	gs.mutex.Lock()
	defer gs.mutex.Unlock()

	gs.config.MediaMTXHost = host
	logrus.Infof("🔧 macOS: GStreamer сервис: хост обновлен на %s", host)
}

// UpdateRTSPPort обновляет RTSP порт (совместимость, теперь используется VideoUDPPort)
func (gs *GStreamerService) UpdateRTSPPort(port int) {
	gs.mutex.Lock()
	defer gs.mutex.Unlock()

	gs.config.MediaMTXRTSP = port
	gs.config.VideoUDPPort = port
	logrus.Infof("🔧 macOS: GStreamer сервис: видео UDP порт обновлен на %d", port)
}

// UpdateVideoUDPPort обновляет порт приёма UDP видео
func (gs *GStreamerService) UpdateVideoUDPPort(port int) {
	gs.mutex.Lock()
	defer gs.mutex.Unlock()

	gs.config.VideoUDPPort = port
	logrus.Infof("🔧 macOS: GStreamer сервис: видео UDP порт обновлен на %d", port)
}

// GetConfig возвращает конфигурацию
func (gs *GStreamerService) GetConfig() *models.AppConfig {
	gs.mutex.RLock()
	defer gs.mutex.RUnlock()
	return gs.config
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
	gs.reconnectAttempts = 0
}

// ConnectToRTSP подключается к UDP H.264 (через QUIC/SUDP туннель или напрямую)
func (gs *GStreamerService) ConnectToRTSP() error {
	port := gs.config.VideoUDPPort
	if port <= 0 {
		port = models.DefaultVideoUDPPort
	}
	return gs.ConnectToUDP(port)
}

// Reconnect принудительно переподключается к UDP потоку (для смены устройств)
func (gs *GStreamerService) Reconnect() error {
	logrus.Info("🔄 macOS: Принудительное переподключение (смена устройства)...")

	// Сначала отключаемся
	if err := gs.Disconnect(); err != nil {
		logrus.Warnf("⚠️ macOS: Ошибка при отключении перед переподключением: %v", err)
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
	logrus.Info("🔗 macOS: Подключаемся к новому устройству...")
	return gs.ConnectToRTSP()
}

