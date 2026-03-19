package service

import (
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"usbridge-client/internal/models"
	"usbridge-client/internal/platform"

	"fyne.io/fyne/v2"
	"github.com/pojntfx/go-nbd/pkg/server"
	"github.com/sirupsen/logrus"
)

// NBDServer NBD сервер для раздачи ISO образов
type NBDServer struct {
	config       *models.AppConfig
	exports      map[string]*DiskExport
	listener     net.Listener
	isRunning    bool
	mutex        sync.RWMutex
	clients      map[string]*NBDClient
	clientMutex  sync.RWMutex
	healthTicker *time.Ticker
	stopHealth   chan bool
	app          fyne.App      // Для доступа к SAF на Android
	readyChan    chan struct{} // Канал для сигнала готовности сервера
}

// DiskExport экспорт устройства для NBD сервера
type DiskExport struct {
	Name        string
	FilePath    string
	Size        int64
	ReadOnly    bool
	Description string
	IsActive    bool
	ExportName  string
	Backend     *FileBackend
	OverlayPath string // путь к overlay (не удаляется при снятии экспорта — переиспользуется)
}

// NBDClient информация о подключенном клиенте
type NBDClient struct {
	Conn         net.Conn
	Export       *DiskExport
	ConnectedAt  time.Time
	LastActivity time.Time
}

// FileBackend бэкенд для работы с файлами
type FileBackend struct {
	file        *os.File
	lock        sync.RWMutex
	virtualSize int64 // если > 0, Size() возвращает его (для qcow2 overlay)
}

// NewFileBackend создает новый файловый бэкенд (размер берётся из file.Stat()).
func NewFileBackend(file *os.File) *FileBackend {
	return &FileBackend{file: file, virtualSize: 0}
}

// NewFileBackendWithSize создаёт бэкенд с явным размером (для overlay: виртуальный размер).
func NewFileBackendWithSize(file *os.File, virtualSize int64) *FileBackend {
	return &FileBackend{file: file, virtualSize: virtualSize}
}

// ReadAt читает данные из файла
func (b *FileBackend) ReadAt(p []byte, off int64) (n int, err error) {
	b.lock.RLock()
	defer b.lock.RUnlock()
	return b.file.ReadAt(p, off)
}

// WriteAt записывает данные в файл
func (b *FileBackend) WriteAt(p []byte, off int64) (n int, err error) {
	b.lock.Lock()
	defer b.lock.Unlock()
	return b.file.WriteAt(p, off)
}

// Size возвращает размер: virtualSize если задан, иначе file.Stat().
func (b *FileBackend) Size() (int64, error) {
	if b.virtualSize > 0 {
		return b.virtualSize, nil
	}
	stat, err := b.file.Stat()
	if err != nil {
		return -1, err
	}
	return stat.Size(), nil
}

// Sync синхронизирует файл
func (b *FileBackend) Sync() error {
	return b.file.Sync()
}

// NewNBDServer создает новый NBD сервер
func NewNBDServer(config *models.AppConfig) *NBDServer {
	return &NBDServer{
		config:     config,
		exports:    make(map[string]*DiskExport),
		clients:    make(map[string]*NBDClient),
		stopHealth: make(chan bool),
		app:        nil, // Будет установлен через SetApp
		readyChan:  make(chan struct{}),
	}
}

// NewNBDServerWithApp создает новый NBD сервер с поддержкой SAF
func NewNBDServerWithApp(config *models.AppConfig, app fyne.App) *NBDServer {
	logrus.Infof("📍 [NBD-INIT] Создание NBD сервера с поддержкой SAF (app: %T)", app)
	return &NBDServer{
		config:     config,
		exports:    make(map[string]*DiskExport),
		clients:    make(map[string]*NBDClient),
		stopHealth: make(chan bool),
		app:        app,
		readyChan:  make(chan struct{}),
	}
}

// SetApp устанавливает app для поддержки SAF
func (ns *NBDServer) SetApp(app fyne.App) {
	ns.mutex.Lock()
	defer ns.mutex.Unlock()
	logrus.Infof("📍 [NBD-SETAPP] Установка app для NBD сервера (app: %T)", app)
	ns.app = app
}

// Start запускает NBD сервер
func (ns *NBDServer) Start(port int) error {
	ns.mutex.Lock()
	defer ns.mutex.Unlock()

	if ns.isRunning {
		return fmt.Errorf("NBD сервер уже запущен")
	}

	// Создаем ListenConfig с SO_REUSEADDR для быстрого переиспользования порта
	lc := net.ListenConfig{
		Control: func(network, address string, c syscall.RawConn) error {
			// Устанавливаем SO_REUSEADDR для немедленного переиспользования порта
			// Платформо-специфичная реализация в nbd_sockopt_*.go
			return setSocketReuseAddr(c)
		},
	}

	bindHost := strings.TrimSpace(ns.config.NBDBindHost)
	if bindHost == "" {
		bindHost = "127.0.0.1"
	}
	addr := fmt.Sprintf("%s:%d", bindHost, port)
	logrus.Infof("📍 [NBD-START-1] Создание TCP listener на %s (порт %d)", addr, port)
	listener, err := lc.Listen(context.Background(), "tcp", addr)
	if err != nil {
		logrus.Errorf("❌ [NBD-START-1-ERROR] Ошибка создания listener на %s: %v", addr, err)
		return fmt.Errorf("ошибка запуска NBD сервера: %v", err)
	}
	// Подтверждаем фактический адрес listener (может отличаться при порте 0)
	actualAddr := listener.Addr().String()
	logrus.Infof("✅ [NBD-START-1-SUCCESS] TCP listener создан: запрошен=%s, фактический=%s", addr, actualAddr)

	ns.listener = listener
	ns.isRunning = true

	logrus.Infof("📍 [NBD-START-2] Запуск горутины acceptConnections для приема соединений")
	go ns.acceptConnections()

	logrus.Infof("📍 [NBD-START-3] Запуск мониторинга здоровья")
	ns.startHealthMonitoring()

	logrus.Infof("🚀 [NBD-START-4-SUCCESS] NBD сервер запущен на %s, ожидает добавления экспортов (FRP туннель: nbd_srv%d)", actualAddr, port-10808)

	// НЕ закрываем readyChan здесь - сервер еще не готов к приему соединений
	// readyChan будет закрыт после добавления экспортов через SignalReady()

	return nil
}

// Stop останавливает NBD сервер
func (ns *NBDServer) Stop() error {
	ns.mutex.Lock()
	defer ns.mutex.Unlock()

	if !ns.isRunning {
		return fmt.Errorf("NBD сервер не запущен")
	}

	// Пересоздаем канал готовности для следующего запуска
	ns.readyChan = make(chan struct{})

	// Останавливаем мониторинг здоровья
	ns.stopHealthMonitoring()

	// Закрываем все клиентские соединения
	ns.clientMutex.Lock()
	for _, client := range ns.clients {
		client.Conn.Close()
	}
	ns.clients = make(map[string]*NBDClient)
	ns.clientMutex.Unlock()

	// Закрываем все файловые бэкенды и удаляем overlay-файлы
	for _, export := range ns.exports {
		if export.Backend != nil && export.Backend.file != nil {
			export.Backend.file.Close()

			// Если это Android content:// URI, очищаем кэш SAF
			if runtime.GOOS == "android" && strings.HasPrefix(export.FilePath, "content://") && ns.app != nil {
				safHelper := platform.GetSAFHelper(ns.app)
				if err := safHelper.CloseFD(export.FilePath); err != nil {
					logrus.Warnf("⚠️ Ошибка очистки кэша SAF для %s: %v", export.FilePath, err)
				}
			}
		}
		// Overlay не удаляем — при следующем монтировании тот же overlay будет использован
	}

	// Сначала устанавливаем флаг остановки
	ns.isRunning = false

	// Закрываем слушатель
	if ns.listener != nil {
		// Принудительно закрываем listener
		if err := ns.listener.Close(); err != nil {
			logrus.Warnf("⚠️ Ошибка закрытия listener: %v", err)
		}
		ns.listener = nil

		// Даем время горутине acceptConnections завершиться и ОС освободить порт
		time.Sleep(200 * time.Millisecond)
	}

	logrus.Info("🛑 NBD сервер остановлен")
	return nil
}

// IsRunning проверяет, запущен ли сервер
func (ns *NBDServer) IsRunning() bool {
	ns.mutex.RLock()
	defer ns.mutex.RUnlock()
	return ns.isRunning
}

// WaitReady возвращает канал, который закрывается когда сервер готов принимать соединения
func (ns *NBDServer) WaitReady() <-chan struct{} {
	ns.mutex.RLock()
	defer ns.mutex.RUnlock()
	return ns.readyChan
}

// SignalReady сигнализирует что сервер готов к приему соединений (экспорты добавлены)
// Запускает асинхронную проверку готовности NBD протокола
func (ns *NBDServer) SignalReady() {
	ns.mutex.Lock()

	logrus.Infof("📍 [NBD-SIGNAL-READY-1] SignalReady вызван, проверяем состояние readyChan")

	// Проверяем что канал еще не закрыт
	select {
	case <-ns.readyChan:
		// Канал уже закрыт
		ns.mutex.Unlock()
		logrus.Warn("⚠️ [NBD-SIGNAL-READY-ALREADY] readyChan уже закрыт, пропускаем")
		return
	default:
		// Канал открыт, запускаем проверку готовности в горутине
	}

	// Логируем текущее состояние сервера
	logrus.Infof("📍 [NBD-SIGNAL-READY-2] Текущее состояние:")
	logrus.Infof("   - isRunning: %v", ns.isRunning)
	logrus.Infof("   - listener != nil: %v", ns.listener != nil)
	logrus.Infof("   - exports count: %d", len(ns.exports))
	for exportName, export := range ns.exports {
		logrus.Infof("     • %s: %s (size: %d bytes)", exportName, export.FilePath, export.Size)
	}

	port := ns.config.NBDPort
	hasExports := len(ns.exports) > 0
	hasListener := ns.listener != nil
	ns.mutex.Unlock()

	// Запускаем проверку готовности в отдельной горутине, чтобы не блокировать вызывающий код
	go ns.verifyReadiness(port, hasListener, hasExports)
}

// verifyReadiness проверяет готовность NBD протокола и закрывает readyChan когда готов
func (ns *NBDServer) verifyReadiness(port int, hasListener bool, hasExports bool) {
	checkAddr := fmt.Sprintf("127.0.0.1:%d", port)
	logrus.Infof("📍 [NBD-VERIFY-1] Запуск проверки готовности: подключаемся к %s (hasListener=%v, hasExports=%v)", checkAddr, hasListener, hasExports)

	// Проверяем, что сервер действительно слушает порт и готов обрабатывать NBD протокол
	if hasListener && hasExports {
		logrus.Infof("📍 [NBD-VERIFY-2] Проверка NBD handshake: TCP connect -> ожидаем 8 байт NBDMAGIC (0x4e42444d41474943) от сервера")

		// Пытаемся выполнить пробное подключение и проверить NBD handshake
		maxAttempts := 20
		success := false
		for attempt := 1; attempt <= maxAttempts; attempt++ {
			logrus.Infof("📍 [NBD-VERIFY-ATTEMPT] Попытка %d/%d: DialTimeout(%s, 200ms)...", attempt, maxAttempts, checkAddr)
			conn, err := net.DialTimeout("tcp", checkAddr, 200*time.Millisecond)
			if err != nil {
				if attempt < maxAttempts {
					logrus.Warnf("⏳ [NBD-VERIFY-RETRY] Порт %d не принимает соединения (попытка %d/%d): %v", port, attempt, maxAttempts, err)
					time.Sleep(100 * time.Millisecond)
					continue
				} else {
					logrus.Errorf("❌ [NBD-VERIFY-FAIL] Порт %d не ответил после %d попыток: %v", port, maxAttempts, err)
					break
				}
			}

			// Порт принял соединение, теперь проверяем NBD handshake
			// NBD сервер должен отправить магическое число при подключении
			conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))

			// NBD протокол начинается с "NBDMAGIC" (0x4e42444d41474943)
			magic := make([]byte, 8)
			n, err := conn.Read(magic)
			conn.Close()

			magicHex := hex.EncodeToString(magic)
			if err != nil {
				logrus.Warnf("⏳ [NBD-VERIFY-RETRY] Порт %d: соединение установлено, но Read вернул err=%v, n=%d, получено hex=%s", port, err, n, magicHex)
			} else if n != 8 {
				logrus.Warnf("⏳ [NBD-VERIFY-RETRY] Порт %d: прочитано %d байт (ожидалось 8), hex=%s", port, n, magicHex)
			} else {
				// Проверяем магическое число NBD
				expectedMagic := []byte{0x4e, 0x42, 0x44, 0x4d, 0x41, 0x47, 0x49, 0x43} // "NBDMAGIC"
				isValid := true
				for i := 0; i < 8; i++ {
					if magic[i] != expectedMagic[i] {
						isValid = false
						break
					}
				}

				if isValid {
					logrus.Infof("✅ [NBD-VERIFY-SUCCESS] Порт %d: сервер ответил NBDMAGIC (hex=%s), NBD протокол готов (попытка %d/%d)", port, magicHex, attempt, maxAttempts)
					success = true
					break
				} else {
					logrus.Warnf("⏳ [NBD-VERIFY-RETRY] Порт %d: получен неверный magic (ожидался NBDMAGIC), hex=%s", port, magicHex)
				}
			}

			if attempt < maxAttempts {
				time.Sleep(100 * time.Millisecond)
			}
		}

		if !success {
			logrus.Warnf("⚠️ [NBD-VERIFY-WARN] NBD протокол не подтвердил готовность после %d попыток, но продолжаем", maxAttempts)
		}
	} else {
		logrus.Warnf("⚠️ [NBD-VERIFY-SKIP] Пропускаем проверку: hasListener=%v, hasExports=%v", hasListener, hasExports)
	}

	// Закрываем канал готовности
	ns.mutex.Lock()
	defer ns.mutex.Unlock()

	select {
	case <-ns.readyChan:
		// Канал уже закрыт
		logrus.Warn("⚠️ [NBD-VERIFY-ALREADY-CLOSED] readyChan уже закрыт другой горутиной")
	default:
		close(ns.readyChan)
		logrus.Info("✅ [NBD-VERIFY-READY] NBD сервер готов к приему соединений (readyChan закрыт)")
	}
}

// AddExport добавляет экспорт в сервер
func (ns *NBDServer) AddExport(export *models.DiskExport) error {
	ns.mutex.Lock()
	defer ns.mutex.Unlock()

	logrus.Infof("📍 [NBD-ADDEXPORT-1] AddExport вызван для: %s (FilePath: %s, ExportName: %s)",
		export.Name, export.FilePath, export.ExportName)

	// Проверяем, не существует ли уже экспорт с таким именем
	if _, exists := ns.exports[export.ExportName]; exists {
		err := fmt.Errorf("экспорт с именем '%s' уже существует", export.ExportName)
		logrus.Errorf("❌ [NBD-ADDEXPORT-1-ERROR] %v", err)
		return err
	}

	var file *os.File
	var fileSize int64
	var err error
	var overlayPathForCleanup string // путь к overlay (только десктоп; не удаляется при снятии)

	// На Android с content:// URI используем SAF
	if runtime.GOOS == "android" && strings.HasPrefix(export.FilePath, "content://") {
		logrus.Infof("📍 [NBD-ADDEXPORT-ANDROID-2] Обнаружен Android content:// URI")

		if ns.app == nil {
			err := fmt.Errorf("NBDServer: app не установлен, невозможно использовать SAF")
			logrus.Errorf("❌ [NBD-ADDEXPORT-ANDROID-2-ERROR] %v", err)
			return err
		}

		logrus.Infof("📍 [NBD-ADDEXPORT-ANDROID-3] Использование SAF для открытия файла")

		// Получаем SAF helper
		safHelper := platform.GetSAFHelper(ns.app)

		// Открываем файл через SAF
		logrus.Infof("📍 [NBD-ADDEXPORT-ANDROID-4] Вызов OpenFileDescriptor для URI: %s", export.FilePath)

		// Определяем режим открытия на основе источника файла
		var mode string
		if strings.Contains(export.FilePath, "com.google.android.apps.docs.storage") {
			// Google Drive - только read-only
			mode = "r"
			export.ReadOnly = true
			logrus.Infof("📍 [NBD-ADDEXPORT-ANDROID-4] Обнаружен Google Drive, используем режим 'r' (read-only)")
		} else {
			// Локальные файлы - пробуем read-write
			mode = "rw"
			logrus.Infof("📍 [NBD-ADDEXPORT-ANDROID-4] Локальный файл, используем режим 'rw'")
		}

		file, err = safHelper.OpenFileDescriptor(export.FilePath, mode)
		if err != nil {
			// Если не удалось открыть с выбранным режимом, пробуем fallback на read-only
			if mode == "rw" {
				logrus.Warnf("⚠️ [NBD-ADDEXPORT-ANDROID-4-FALLBACK] Не удалось открыть в режиме 'rw': %v, пробуем 'r'", err)
				file, err = safHelper.OpenFileDescriptor(export.FilePath, "r")
				if err != nil {
					logrus.Errorf("❌ [NBD-ADDEXPORT-ANDROID-4-ERROR] Не удалось открыть файл: %v", err)
					return fmt.Errorf("ошибка открытия файла через SAF %s: %v", export.FilePath, err)
				}
				export.ReadOnly = true
				logrus.Infof("✅ [NBD-ADDEXPORT-ANDROID-4-SUCCESS] Файл открыт через SAF в режиме read-only (fallback)")
			} else {
				logrus.Errorf("❌ [NBD-ADDEXPORT-ANDROID-4-ERROR] Не удалось открыть файл: %v", err)
				return fmt.Errorf("ошибка открытия файла через SAF %s: %v", export.FilePath, err)
			}
		} else {
			if mode == "r" {
				logrus.Infof("✅ [NBD-ADDEXPORT-ANDROID-4-SUCCESS] Файл открыт через SAF в режиме read-only")
			} else {
				logrus.Infof("✅ [NBD-ADDEXPORT-ANDROID-4-SUCCESS] Файл открыт через SAF в режиме read-write")
			}
		}

		// Получаем размер через file.Stat()
		logrus.Infof("📍 [NBD-ADDEXPORT-ANDROID-5] Получение размера файла")
		stat, err := file.Stat()
		if err != nil {
			file.Close()
			logrus.Errorf("❌ [NBD-ADDEXPORT-ANDROID-5-ERROR] Не удалось получить размер файла: %v", err)
			return fmt.Errorf("ошибка получения размера файла через SAF %s: %v", export.FilePath, err)
		}
		fileSize = stat.Size()
		logrus.Infof("✅ [NBD-ADDEXPORT-ANDROID-5-SUCCESS] Размер файла: %d байт", fileSize)

		// Google Drive НЕ ПОДДЕРЖИВАЕТ случайное чтение (random access/seek) через SAF!
		// NBD протокол требует ReadAt (случайное чтение)
		// Стратегия: читаем файл полностью для кэширования, затем закрываем и переоткрываем
		if strings.Contains(export.FilePath, "com.google.android.apps.docs.storage") {
			logrus.Warnf("⚠️ [NBD-ADDEXPORT-ANDROID-6-GDRIVE] Google Drive файл обнаружен!")
			logrus.Infof("📍 [NBD-ADDEXPORT-ANDROID-6-GDRIVE] Google Drive не поддерживает random access через SAF")
			logrus.Infof("📍 [NBD-ADDEXPORT-ANDROID-6-GDRIVE] Читаем файл полностью для кэширования Android...")

			// Читаем весь файл последовательно, чтобы Android закэшировал его
			startTime := time.Now()
			logrus.Infof("📥 [NBD-ADDEXPORT-ANDROID-6-GDRIVE] Начало чтения %d байт из Google Drive для кэширования...", fileSize)

			// Перематываем файл в начало
			if _, err := file.Seek(0, 0); err != nil {
				file.Close()
				logrus.Errorf("❌ [NBD-ADDEXPORT-ANDROID-6-GDRIVE-ERROR] Не удалось перемотать файл: %v", err)
				return fmt.Errorf("ошибка чтения файла из Google Drive %s: %v", export.FilePath, err)
			}

			// Читаем весь файл последовательно, используя io.Discard
			// Android автоматически закэширует данные при чтении
			written, err := io.Copy(io.Discard, file)
			duration := time.Since(startTime)

			if err != nil {
				file.Close()
				logrus.Errorf("❌ [NBD-ADDEXPORT-ANDROID-6-GDRIVE-ERROR] Ошибка чтения данных для кэширования: %v (прочитано %d из %d байт за %v)", err, written, fileSize, duration)
				return fmt.Errorf("ошибка кэширования файла из Google Drive %s: %v", export.FilePath, err)
			}

			logrus.Infof("✅ [NBD-ADDEXPORT-ANDROID-6-GDRIVE-SUCCESS] Прочитано %d байт за %v (%.2f MB/s)",
				written, duration, float64(written)/1024/1024/duration.Seconds())

			// КРИТИЧНО: io.Copy может вернуться ДО того как Android полностью скачал файл в кэш!
			// Используем комплексную проверку готовности кэша (размер стабилен + ReadAt быстрый)
			logrus.Infof("📍 [NBD-ADDEXPORT-ANDROID-6-GDRIVE] Проверяем готовность кэша (размер + скорость ReadAt)...")
			if err := WaitForGDriveCache(file, fileSize, 3*time.Minute); err != nil {
				file.Close()
				logrus.Errorf("❌ [NBD-ADDEXPORT-ANDROID-6-GDRIVE-CACHE-TIMEOUT] %v", err)
				return fmt.Errorf("Google Drive файл %s не закэшировался: %v", export.FilePath, err)
			}
			logrus.Infof("✅ [NBD-ADDEXPORT-ANDROID-6-GDRIVE-CACHE-VERIFIED] Кэш полностью готов (проверен размер и скорость)")

			// КРИТИЧНО: Закрываем и переоткрываем файл!
			// Android кэширует файл, но ParcelFileDescriptor может не переключиться на локальный кэш
			// Переоткрытие заставит Android вернуть дескриптор к закэшированному файлу с поддержкой seek
			logrus.Infof("📍 [NBD-ADDEXPORT-ANDROID-6-GDRIVE] Закрываем и переоткрываем файл для использования кэша...")

			file.Close()
			safHelper.CloseFD(export.FilePath)

			// Переоткрываем файл через SAF - теперь Android должен вернуть кэшированную версию
			logrus.Infof("📍 [NBD-ADDEXPORT-ANDROID-6-GDRIVE-REOPEN] Переоткрытие файла через SAF...")
			file, err = safHelper.OpenFileDescriptor(export.FilePath, "r")
			if err != nil {
				logrus.Errorf("❌ [NBD-ADDEXPORT-ANDROID-6-GDRIVE-REOPEN-ERROR] Не удалось переоткрыть файл: %v", err)
				return fmt.Errorf("ошибка переоткрытия файла из Google Drive %s: %v", export.FilePath, err)
			}

			logrus.Infof("✅ [NBD-ADDEXPORT-ANDROID-6-GDRIVE-REOPEN-SUCCESS] Файл переоткрыт, теперь должен работать seek")

			// Проверяем, что seek теперь работает
			seekOffset, seekErr := file.Seek(0, 0)
			if seekErr != nil {
				file.Close()
				logrus.Errorf("❌ [NBD-ADDEXPORT-ANDROID-6-GDRIVE-SEEK-TEST-ERROR] Seek все еще не работает после переоткрытия: %v", seekErr)
				return fmt.Errorf("файл из Google Drive %s не поддерживает seek даже после кэширования: %v", export.FilePath, seekErr)
			}
			logrus.Infof("✅ [NBD-ADDEXPORT-ANDROID-6-GDRIVE-SEEK-TEST] Seek работает! (offset=%d)", seekOffset)

			// Тестируем ReadAt с разными офсетами (критично для NBD!)
			// NBD клиент будет читать с произвольных офсетов, поэтому проверяем несколько
			verifyBuf := make([]byte, 512)
			testOffsets := []int64{0, 512, 1024, fileSize / 2, fileSize - 512}

			logrus.Infof("📍 [NBD-ADDEXPORT-ANDROID-6-GDRIVE-READAT-TEST] Тестируем ReadAt с %d разными офсетами...", len(testOffsets))
			for i, offset := range testOffsets {
				if offset < 0 || offset >= fileSize {
					continue // Пропускаем невалидные офсеты
				}

				n, testErr := file.ReadAt(verifyBuf, offset)
				if testErr != nil && testErr != io.EOF {
					file.Close()
					logrus.Errorf("❌ [NBD-ADDEXPORT-ANDROID-6-GDRIVE-READAT-TEST-ERROR] ReadAt не работает на офсете %d (тест %d/%d): %v", offset, i+1, len(testOffsets), testErr)
					return fmt.Errorf("файл из Google Drive %s не поддерживает ReadAt на офсете %d: %v", export.FilePath, offset, testErr)
				}
				logrus.Infof("✅ [NBD-ADDEXPORT-ANDROID-6-GDRIVE-READAT-TEST] ReadAt #%d: offset=%d, прочитано=%d байт", i+1, offset, n)
			}

			logrus.Infof("📍 [NBD-ADDEXPORT-ANDROID-6-GDRIVE] Файл готов для NBD (закэширован Android, seek и ReadAt работают на всех офсетах)")
		}

	} else {
		// Десктоп или обычный путь
		logrus.Infof("📍 [NBD-ADDEXPORT-DESKTOP-2] Десктоп режим, обычное открытие файла")

		// Проверяем, что файл существует
		if _, err := os.Stat(export.FilePath); os.IsNotExist(err) {
			logrus.Errorf("❌ [NBD-ADDEXPORT-DESKTOP-2-ERROR] Файл не существует: %s", export.FilePath)
			return fmt.Errorf("файл %s не существует", export.FilePath)
		}

		ext := strings.ToLower(filepath.Ext(export.FilePath))
		useOverlay := !export.ReadOnly && IsOverlayCapableExtension(ext)
		openPath := export.FilePath
		overlayPath := ""

		if useOverlay {
			// RW без порчи базы: создаём qcow2 overlay рядом с образом
			created, vs, createErr := createOverlay(export.FilePath)
			if createErr != nil {
				logrus.Warnf("⚠️ [NBD-ADDEXPORT-DESKTOP-OVERLAY] Не удалось создать overlay (экспортируем базу RO): %v", createErr)
				useOverlay = false
				export.ReadOnly = true
			} else {
				overlayPath = created
				overlayPathForCleanup = created
				openPath = created
				fileSize = vs
				logrus.Infof("📍 [NBD-ADDEXPORT-DESKTOP-OVERLAY] Экспорт через overlay: %s (virtual size: %d)", overlayPath, fileSize)
			}
		}

		if !useOverlay {
			// Открываем файл (RO или прямой RW)
			flags := os.O_RDONLY
			if !export.ReadOnly {
				flags = os.O_RDWR
			}
			logrus.Infof("📍 [NBD-ADDEXPORT-DESKTOP-3] Открытие файла: %s (readOnly=%v)", openPath, export.ReadOnly)
			file, err = os.OpenFile(openPath, flags, 0)
			if err != nil {
				logrus.Errorf("❌ [NBD-ADDEXPORT-DESKTOP-3-ERROR] Ошибка открытия файла: %v", err)
				return fmt.Errorf("ошибка открытия файла %s: %v", openPath, err)
			}
			stat, err := file.Stat()
			if err != nil {
				file.Close()
				return fmt.Errorf("ошибка получения размера файла %s: %v", openPath, err)
			}
			fileSize = stat.Size()
			logrus.Infof("✅ [NBD-ADDEXPORT-DESKTOP-4-SUCCESS] Размер файла: %d байт", fileSize)
		} else {
			// Открываем созданный overlay для RW
			logrus.Infof("📍 [NBD-ADDEXPORT-DESKTOP-3] Открытие overlay: %s", openPath)
			file, err = os.OpenFile(openPath, os.O_RDWR, 0)
			if err != nil {
				logrus.Errorf("❌ [NBD-ADDEXPORT-DESKTOP-3-ERROR] Ошибка открытия overlay: %v", err)
				return fmt.Errorf("ошибка открытия overlay %s: %v", openPath, err)
			}
		}
	}

	// Генерируем имя экспорта если не задано
	if export.ExportName == "" {
		export.ExportName = filepath.Base(export.FilePath)
		logrus.Infof("📍 [NBD-ADDEXPORT-6] Сгенерировано имя экспорта: %s", export.ExportName)
	}

	// Создаем файловый бэкенд (для overlay — с виртуальным размером)
	logrus.Infof("📍 [NBD-ADDEXPORT-7] Создание файлового бэкенда")
	var backend *FileBackend
	if overlayPathForCleanup != "" {
		backend = NewFileBackendWithSize(file, fileSize)
	} else {
		backend = NewFileBackend(file)
	}

	// Создаем экспорт
	diskExport := &DiskExport{
		Name:        export.Name,
		FilePath:    export.FilePath,
		Size:        fileSize,
		ReadOnly:    export.ReadOnly,
		Description: export.Description,
		IsActive:    true,
		ExportName:  export.ExportName,
		Backend:     backend,
		OverlayPath: overlayPathForCleanup,
	}

	ns.exports[export.ExportName] = diskExport
	logrus.Infof("✅ [NBD-ADDEXPORT-8-SUCCESS] Экспорт %s добавлен: %s (размер: %d байт, всего экспортов: %d)",
		export.ExportName, export.FilePath, fileSize, len(ns.exports))

	// КРИТИЧНО: Даем библиотеке go-nbd время инициализироваться ПОСЛЕ добавления экспорта
	// Без этой задержки NBD протокол может быть не готов к handshake
	logrus.Infof("⏳ [NBD-ADDEXPORT-9] Ожидание инициализации go-nbd протокола (500ms)...")
	time.Sleep(500 * time.Millisecond)
	logrus.Infof("✅ [NBD-ADDEXPORT-9-SUCCESS] go-nbd протокол инициализирован для экспорта %s", export.ExportName)

	return nil
}

// RemoveExport удаляет экспорт из сервера
func (ns *NBDServer) RemoveExport(exportName string) error {
	ns.mutex.Lock()
	defer ns.mutex.Unlock()

	// Логируем стек-трейс для понимания, откуда вызывается удаление
	logrus.Infof("🔍 RemoveExport вызвана для экспорта %s", exportName)
	logrus.Debugf("📍 Стек-трейс вызова RemoveExport:")

	// Получаем стек-трейс
	buf := make([]byte, 1024)
	n := runtime.Stack(buf, false)
	logrus.Debugf("📍 %s", string(buf[:n]))

	export, exists := ns.exports[exportName]
	if !exists {
		logrus.Warnf("⚠️ Экспорт %s не найден при попытке удаления", exportName)
		return fmt.Errorf("экспорт %s не найден", exportName)
	}

	// Закрываем файл
	if export.Backend != nil && export.Backend.file != nil {
		export.Backend.file.Close()

		// Если это Android content:// URI, очищаем кэш SAF
		if runtime.GOOS == "android" && strings.HasPrefix(export.FilePath, "content://") && ns.app != nil {
			safHelper := platform.GetSAFHelper(ns.app)
			if err := safHelper.CloseFD(export.FilePath); err != nil {
				logrus.Warnf("⚠️ Ошибка очистки кэша SAF для %s: %v", export.FilePath, err)
			}
		}
	}

	// Overlay не удаляем — при следующем монтировании тот же overlay будет использован

	delete(ns.exports, exportName)
	logrus.Infof("🗑️ Экспорт %s удален", exportName)
	return nil
}

// GetExports возвращает список экспортов
func (ns *NBDServer) GetExports() []*models.DiskExport {
	ns.mutex.RLock()
	defer ns.mutex.RUnlock()

	exports := make([]*models.DiskExport, 0, len(ns.exports))
	for _, export := range ns.exports {
		modelsExport := &models.DiskExport{
			Name:        export.Name,
			FilePath:    export.FilePath,
			Size:        export.Size,
			ReadOnly:    export.ReadOnly,
			Description: export.Description,
			IsActive:    export.IsActive,
			ExportName:  export.ExportName,
		}
		exports = append(exports, modelsExport)
	}
	return exports
}

// GetClients возвращает список подключенных клиентов
func (ns *NBDServer) GetClients() []*NBDClient {
	ns.clientMutex.RLock()
	defer ns.clientMutex.RUnlock()

	clients := make([]*NBDClient, 0, len(ns.clients))
	for _, client := range ns.clients {
		clients = append(clients, client)
	}
	return clients
}

// NBDExportNameForConnection возвращает имя экспорта для NBD handshake (у go-nbd — имя добавленного экспорта).
func (ns *NBDServer) NBDExportNameForConnection() string {
	ns.mutex.RLock()
	defer ns.mutex.RUnlock()
	for name := range ns.exports {
		return name
	}
	return ""
}

// NBDExportNameForAPI возвращает имя экспорта для API (у go-nbd — имя файла/экспорта).
func (ns *NBDServer) NBDExportNameForAPI() string {
	return ns.NBDExportNameForConnection()
}

// NBDHandshakeEmptyExport — у go-nbd используем имя экспорта в handshake.
func (ns *NBDServer) NBDHandshakeEmptyExport() bool {
	return false
}

// acceptConnections принимает входящие соединения
func (ns *NBDServer) acceptConnections() {
	logrus.Infof("🎧 [NBD-ACCEPT-START] Горутина acceptConnections запущена, начинаем слушать входящие соединения")

	for {
		// Проверяем что сервер еще работает и listener не nil
		ns.mutex.RLock()
		if !ns.isRunning || ns.listener == nil {
			ns.mutex.RUnlock()
			logrus.Infof("🛑 [NBD-ACCEPT-STOP] acceptConnections завершается (isRunning=%v, listener=%v)", ns.isRunning, ns.listener != nil)
			return
		}
		listener := ns.listener
		ns.mutex.RUnlock()

		ns.mutex.RLock()
		listenAddr := "unknown"
		if ns.listener != nil {
			listenAddr = ns.listener.Addr().String()
		}
		ns.mutex.RUnlock()
		logrus.Infof("⏳ [NBD-ACCEPT-WAITING] Ожидаем входящее соединение на %s (Accept)...", listenAddr)

		conn, err := listener.Accept()
		if err != nil {
			// Проверяем снова после ошибки - возможно сервер был остановлен
			ns.mutex.RLock()
			stillRunning := ns.isRunning
			ns.mutex.RUnlock()

			if stillRunning {
				logrus.Errorf("❌ [NBD-ACCEPT-ERROR] Ошибка принятия соединения: %v", err)
			} else {
				logrus.Infof("ℹ️ [NBD-ACCEPT-STOPPED] Сервер остановлен, игнорируем ошибку Accept")
			}

			// Если сервер остановлен, выходим из цикла
			if !stillRunning {
				return
			}
			continue
		}

		remoteAddr := conn.RemoteAddr().String()
		logrus.Infof("✅ [NBD-ACCEPT-SUCCESS] Принято входящее соединение: remote=%s -> local=%s, запускаем handleClient", remoteAddr, listenAddr)
		go ns.handleClient(conn)
	}
}

// handleClient обрабатывает клиентское соединение
func (ns *NBDServer) handleClient(conn net.Conn) {
	defer conn.Close()

	clientAddr := conn.RemoteAddr().String()
	connStartTime := time.Now()
	logrus.Infof("📥 [NBD-CLIENT-CONNECT] Новый NBD клиент подключен: %s", clientAddr)

	// Включаем TCP KeepAlive — обнаруживаем разрыв быстрее, чем дефолтные 2 часа.
	// Важно для FRP STCP: если QUIC-туннель рвётся, localhost TCP может зависнуть без keepalive.
	if tcpConn, ok := conn.(*net.TCPConn); ok {
		tcpConn.SetKeepAlive(true)
		tcpConn.SetKeepAlivePeriod(30 * time.Second)
		logrus.Debugf("📡 [NBD-CLIENT-KEEPALIVE] TCP KeepAlive включён для %s (период: 30 с)", clientAddr)
	}

	// Получаем список экспортов и ReadOnly первого в детерминированном порядке (по имени)
	// Важно: NBD handshake передаёт один флаг ReadOnly — при одном экспорте на порт это флаг этого экспорта
	ns.mutex.RLock()
	names := make([]string, 0, len(ns.exports))
	for name := range ns.exports {
		names = append(names, name)
	}
	sort.Strings(names)
	exports := make([]*server.Export, 0, len(names)+1)
	firstExportRO := true
	for _, name := range names {
		export := ns.exports[name]
		exports = append(exports, &server.Export{
			Name:        export.ExportName,
			Description: export.Description,
			Backend:     export.Backend,
		})
		if firstExportRO {
			firstExportRO = export.ReadOnly
		}
	}
	// Гаджет (usbridge) при nbd_handshake_empty_export строит nbd://host:port/ и nbd-client подключается без -N (пустое имя).
	// go-nbd ищет candidate.Name == string(exportName); без экспорта с Name "" handshake даёт NBD_REP_ERR_UNKNOWN и чтение с nbd0 даёт I/O error.
	// При одном экспорте добавляем дубликат с пустым именем, чтобы оба варианта (пустой и непустой) работали.
	if len(exports) == 1 {
		exports = append(exports, &server.Export{
			Name:        "",
			Description: exports[0].Description,
			Backend:     exports[0].Backend,
		})
	}
	ns.mutex.RUnlock()

	if len(exports) == 0 {
		logrus.Warnf("⚠️ [NBD-CLIENT-NO-EXPORTS] Нет доступных экспортов для клиента %s, закрываем соединение", clientAddr)
		return
	}

	// Логируем доступные экспорты для клиента
	logrus.Infof("📋 [NBD-CLIENT-EXPORTS] Доступные экспорты для клиента %s (всего: %d):", clientAddr, len(exports))
	for i, export := range exports {
		logrus.Infof("  %d. %s: %s", i+1, export.Name, export.Description)
	}

	// Добавляем клиента в список
	client := &NBDClient{
		Conn:         conn,
		ConnectedAt:  connStartTime,
		LastActivity: connStartTime,
	}

	ns.clientMutex.Lock()
	ns.clients[clientAddr] = client
	ns.clientMutex.Unlock()
	logrus.Infof("✅ [NBD-CLIENT-REGISTERED] Клиент %s зарегистрирован, всего активных клиентов: %d", clientAddr, len(ns.clients))

	defer func() {
		sessionDuration := time.Since(connStartTime).Round(time.Second)
		ns.clientMutex.Lock()
		delete(ns.clients, clientAddr)
		remainingClients := len(ns.clients)
		ns.clientMutex.Unlock()
		logrus.Infof("👋 [NBD-CLIENT-DISCONNECT] NBD клиент отключен: %s, длительность сессии: %v, осталось активных: %d", clientAddr, sessionDuration, remainingClients)
	}()

	// Обрабатываем соединение с помощью go-nbd
	// Передаем все экспорты, чтобы клиент мог выбрать нужный
	// ВАЖНО: не устанавливаем Deadline на conn — go-nbd управляет соединением
	options := &server.Options{
		ReadOnly:           firstExportRO,
		MinimumBlockSize:   512,
		PreferredBlockSize: 4096,
		MaximumBlockSize:   65536,
	}

	logrus.Infof("🤝 [NBD-CLIENT-HANDSHAKE] Начало NBD handshake с клиентом %s (опции: ReadOnly=%v, BlockSize=%d-%d)",
		clientAddr, options.ReadOnly, options.MinimumBlockSize, options.MaximumBlockSize)

	if err := server.Handle(conn, exports, options); err != nil {
		sessionDuration := time.Since(connStartTime).Round(time.Second)
		logrus.Errorf("❌ [NBD-CLIENT-HANDLE-ERROR] Ошибка NBD соединения с %s после %v: %v", clientAddr, sessionDuration, err)
	} else {
		sessionDuration := time.Since(connStartTime).Round(time.Second)
		logrus.Infof("✅ [NBD-CLIENT-HANDLE-SUCCESS] NBD сессия с %s завершена нормально после %v", clientAddr, sessionDuration)
	}
}

// startHealthMonitoring запускает мониторинг здоровья NBD сервера
func (ns *NBDServer) startHealthMonitoring() {
	logrus.Infof("📍 [NBD-HEALTH-START-1] Создание ticker для мониторинга (интервал: 10 секунд)")
	ns.healthTicker = time.NewTicker(10 * time.Second) // Проверяем каждые 10 секунд

	logrus.Infof("📍 [NBD-HEALTH-START-2] Запуск горутины мониторинга здоровья")
	go func() {
		logrus.Infof("✅ [NBD-HEALTH-GOROUTINE] Горутина мониторинга здоровья запущена")
		for {
			select {
			case <-ns.healthTicker.C:
				ns.checkClientsHealth()
				ns.validateExports()
			case <-ns.stopHealth:
				logrus.Infof("🛑 [NBD-HEALTH-GOROUTINE-STOP] Горутина мониторинга здоровья остановлена")
				return
			}
		}
	}()

	logrus.Info("✅ [NBD-HEALTH-START-SUCCESS] Мониторинг здоровья NBD запущен")
}

// stopHealthMonitoring останавливает мониторинг здоровья
func (ns *NBDServer) stopHealthMonitoring() {
	if ns.healthTicker != nil {
		ns.healthTicker.Stop()
		close(ns.stopHealth)
		ns.stopHealth = make(chan bool) // Пересоздаем канал для следующего запуска
		logrus.Info("🔍 Мониторинг здоровья NBD остановлен")
	}
}

// checkClientsHealth проверяет здоровье подключенных клиентов.
// ВНИМАНИЕ: НЕ модифицируем соединения (SetWriteDeadline/Write) — это ломает go-nbd.
// Очистка мёртвых клиентов происходит автоматически в handleClient.defer при завершении server.Handle().
func (ns *NBDServer) checkClientsHealth() {
	ns.clientMutex.RLock()
	defer ns.clientMutex.RUnlock()

	for addr, client := range ns.clients {
		idleDuration := time.Since(client.LastActivity)
		if idleDuration > 60*time.Second {
			logrus.Debugf("📊 NBD клиент %s: неактивен %v (соединение управляется go-nbd, не трогаем)", addr, idleDuration.Round(time.Second))
		}
	}
}

// validateExports проверяет корректность экспортов
func (ns *NBDServer) validateExports() {
	ns.mutex.Lock()
	defer ns.mutex.Unlock()

	for exportName, export := range ns.exports {
		// Для Android content:// URI не проверяем через os.Stat
		// Вместо этого проверяем доступность через Backend
		if runtime.GOOS == "android" && strings.HasPrefix(export.FilePath, "content://") {
			// Для Android проверяем только доступность через бэкенд
			if export.Backend != nil && export.Backend.file != nil {
				_, err := export.Backend.Size()
				if err != nil {
					logrus.Warnf("⚠️ Экспорт %s недоступен: %v", exportName, err)
					export.IsActive = false
				}
			}
			continue
		}

		// Для обычных файлов проверяем существование
		if _, err := os.Stat(export.FilePath); os.IsNotExist(err) {
			logrus.Warnf("⚠️ Файл экспорта %s не найден: %s", exportName, export.FilePath)
			export.IsActive = false
			continue
		}

		// Проверяем, можем ли читать файл
		if export.Backend != nil && export.Backend.file != nil {
			_, err := export.Backend.Size()
			if err != nil {
				logrus.Warnf("⚠️ Экспорт %s недоступен: %v", exportName, err)
				export.IsActive = false
			}
		}
	}
}

// ForceRemoveExport принудительно удаляет экспорт (идемпотентно)
func (ns *NBDServer) ForceRemoveExport(exportName string) error {
	ns.mutex.Lock()
	defer ns.mutex.Unlock()

	// Логируем стек-трейс для понимания, откуда вызывается принудительное удаление
	logrus.Infof("🔍 ForceRemoveExport вызвана для экспорта %s", exportName)
	logrus.Debugf("📍 Стек-трейс вызова ForceRemoveExport:")

	// Получаем стек-трейс
	buf := make([]byte, 1024)
	n := runtime.Stack(buf, false)
	logrus.Debugf("📍 %s", string(buf[:n]))

	export, exists := ns.exports[exportName]
	if !exists {
		logrus.Warnf("⚠️ Экспорт %s не найден, возможно уже удален", exportName)
		return nil // Идемпотентно - не ошибка если уже удален
	}

	// Принудительно закрываем файл, игнорируя ошибки
	if export.Backend != nil && export.Backend.file != nil {
		if err := export.Backend.file.Close(); err != nil {
			logrus.Warnf("⚠️ Ошибка закрытия файла для экспорта %s: %v", exportName, err)
		}

		// Если это Android content:// URI, очищаем кэш SAF
		if runtime.GOOS == "android" && strings.HasPrefix(export.FilePath, "content://") && ns.app != nil {
			safHelper := platform.GetSAFHelper(ns.app)
			if err := safHelper.CloseFD(export.FilePath); err != nil {
				logrus.Warnf("⚠️ Ошибка очистки кэша SAF для %s: %v", export.FilePath, err)
			}
		}
	}
	// Overlay не удаляем — при следующем монтировании тот же overlay будет использован

	delete(ns.exports, exportName)
	logrus.Infof("🗑️ Экспорт %s принудительно удален", exportName)
	return nil
}

// CleanupDeadExports очищает "мёртвые" экспорты
func (ns *NBDServer) CleanupDeadExports() int {
	ns.mutex.Lock()
	defer ns.mutex.Unlock()

	logrus.Debugf("🧹 CleanupDeadExports вызвана, проверяем %d экспортов", len(ns.exports))
	cleaned := 0
	for exportName, export := range ns.exports {
		logrus.Debugf("🔍 Проверка экспорта %s на \"мёртвость\"...", exportName)

		// Проверяем файл (для overlay проверяем базовый FilePath)
		if _, err := os.Stat(export.FilePath); os.IsNotExist(err) {
			logrus.Warnf("🧹 Очистка мёртвого экспорта %s: файл не найден", exportName)
			if export.Backend != nil && export.Backend.file != nil {
				export.Backend.file.Close()

				// Если это Android content:// URI, очищаем кэш SAF
				if runtime.GOOS == "android" && strings.HasPrefix(export.FilePath, "content://") && ns.app != nil {
					safHelper := platform.GetSAFHelper(ns.app)
					if err := safHelper.CloseFD(export.FilePath); err != nil {
						logrus.Warnf("⚠️ Ошибка очистки кэша SAF для %s: %v", export.FilePath, err)
					}
				}
			}
			// Overlay не удаляем
			delete(ns.exports, exportName)
			cleaned++
			continue
		}

		// Проверяем доступность файла
		if export.Backend != nil && export.Backend.file != nil {
			if _, err := export.Backend.Size(); err != nil {
				logrus.Warnf("🧹 Очистка недоступного экспорта %s: %v", exportName, err)
				export.Backend.file.Close()

				// Если это Android content:// URI, очищаем кэш SAF
				if runtime.GOOS == "android" && strings.HasPrefix(export.FilePath, "content://") && ns.app != nil {
					safHelper := platform.GetSAFHelper(ns.app)
					if err := safHelper.CloseFD(export.FilePath); err != nil {
						logrus.Warnf("⚠️ Ошибка очистки кэша SAF для %s: %v", export.FilePath, err)
					}
				}
				// Overlay не удаляем — при следующем монтировании тот же overlay будет использован

				delete(ns.exports, exportName)
				cleaned++
			} else {
				logrus.Debugf("✅ Экспорт %s здоров", exportName)
			}
		} else {
			logrus.Debugf("✅ Экспорт %s не имеет бэкенда, но это нормально", exportName)
		}
	}

	if cleaned > 0 {
		logrus.Infof("🧹 Очищено %d мёртвых экспортов", cleaned)
	}

	return cleaned
}

// IsExportHealthy проверяет здоровье конкретного экспорта
func (ns *NBDServer) IsExportHealthy(exportName string) bool {
	ns.mutex.RLock()
	defer ns.mutex.RUnlock()

	export, exists := ns.exports[exportName]
	if !exists {
		return false
	}

	// Проверяем файл
	if _, err := os.Stat(export.FilePath); os.IsNotExist(err) {
		return false
	}

	// Проверяем бэкенд
	if export.Backend == nil || export.Backend.file == nil {
		return false
	}

	// Проверяем доступность
	if _, err := export.Backend.Size(); err != nil {
		return false
	}

	return export.IsActive
}

// GetServerStatus возвращает подробный статус NBD сервера
func (ns *NBDServer) GetServerStatus() map[string]interface{} {
	ns.mutex.RLock()
	defer ns.mutex.RUnlock()

	ns.clientMutex.RLock()
	clientCount := len(ns.clients)
	ns.clientMutex.RUnlock()

	exportCount := len(ns.exports)
	exportList := make([]map[string]interface{}, 0, exportCount)

	for name, export := range ns.exports {
		exportInfo := map[string]interface{}{
			"name":        export.ExportName,
			"file_path":   export.FilePath,
			"size":        export.Size,
			"read_only":   export.ReadOnly,
			"description": export.Description,
			"is_active":   export.IsActive,
			"is_healthy":  ns.IsExportHealthy(name),
		}
		exportList = append(exportList, exportInfo)
	}

	return map[string]interface{}{
		"is_running":   ns.isRunning,
		"export_count": exportCount,
		"client_count": clientCount,
		"exports":      exportList,
		"server_port":  ns.config.NBDPort,
	}
}

// LogServerStatus логирует подробный статус сервера
func (ns *NBDServer) LogServerStatus() {
	status := ns.GetServerStatus()

	logrus.Infof("📊 Статус NBD сервера:")
	logrus.Infof("  - Запущен: %v", status["is_running"])
	logrus.Infof("  - Экспортов: %d", status["export_count"])
	logrus.Infof("  - Клиентов: %d", status["client_count"])

	if exports, ok := status["exports"].([]map[string]interface{}); ok {
		for _, export := range exports {
			logrus.Infof("  - Экспорт '%s': %v (размер: %d, активен: %v, здоров: %v)",
				export["name"], export["description"], export["size"], export["is_active"], export["is_healthy"])
		}
	}
}
