// Package service: экспорт виртуального диска через qemu-nbd (VMDK/QCOW2/VDI — клиент видит MBR/GPT, а не контейнер).

package service

import (
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// NBDRunner — общий интерфейс для нашего NBD-сервера (go-nbd) и процесса qemu-nbd.
// Позволяет подменять экспорт "файла как есть" на экспорт расшифрованного диска.
type NBDRunner interface {
	Start(port int) error
	Stop() error
	IsRunning() bool
	GetServerStatus() map[string]interface{}
	WaitReady() <-chan struct{}
	SignalReady()
	GetClients() []*NBDClient
	// NBDExportNameForConnection — имя экспорта для NBD handshake. У qemu-nbd один экспорт с пустым именем.
	NBDExportNameForConnection() string
	// NBDExportNameForAPI — имя для API (export_name в запросе). Непустое, уникальное (у qemu-nbd — nbd_<port>, у go-nbd — имя экспорта).
	NBDExportNameForAPI() string
	// NBDHandshakeEmptyExport — true = Bridge должен в NBD handshake использовать пустое имя экспорта (qemu-nbd отдаёт один диск без имени).
	NBDHandshakeEmptyExport() bool
}

// QemuNBDRunner запускает qemu-nbd для образа (vmdk/qcow2/vdi/qcow), чтобы по NBD
// отдавался виртуальный диск (первые байты — MBR/GPT), а не контейнер.
type QemuNBDRunner struct {
	filePath    string
	format      string
	readOnly    bool
	bindHost    string
	port        int
	cmd         *exec.Cmd
	overlayPath string
	readyChan   chan struct{}
	running     bool
	mu          sync.RWMutex
}

// qemuNbdFormat по расширению возвращает формат для qemu-nbd -f.
func qemuNbdFormat(ext string) string {
	switch strings.ToLower(ext) {
	case ".qcow2", ".qcow":
		return "qcow2"
	case ".vmdk":
		return "vmdk"
	case ".vdi":
		return "vdi"
	case ".raw", ".img", ".vmi":
		return "raw"
	default:
		return "raw"
	}
}

// IsQemuNbdFormatForPath возвращает true, если по пути образ нужно экспортировать через qemu-nbd
// (виртуальный диск — MBR/GPT), а не как сырой файл (контейнер VMDK/QCOW2).
func IsQemuNbdFormatForPath(path string) bool {
	ext := path
	if i := strings.LastIndex(ext, "."); i >= 0 {
		ext = strings.ToLower(ext[i:])
	} else {
		return false
	}
	switch ext {
	case ".vmdk", ".vdi", ".qcow2", ".qcow":
		return true
	default:
		return false
	}
}

// NewQemuNBDRunner создаёт раннер для qemu-nbd. filePath — путь к образу (на десктопе).
// Для RW при необходимости создаётся overlay; тогда экспортируется overlay.
func NewQemuNBDRunner(filePath string, readOnly bool, bindHost string) *QemuNBDRunner {
	ext := strings.ToLower(filePath)
	if i := strings.LastIndex(ext, "."); i >= 0 {
		ext = ext[i:]
	} else {
		ext = ""
	}
	format := qemuNbdFormat(ext)
	return &QemuNBDRunner{
		filePath:  filePath,
		format:    format,
		readOnly:  readOnly,
		bindHost:  strings.TrimSpace(bindHost),
		readyChan: make(chan struct{}),
	}
}

// Start запускает qemu-nbd на порту. Экспортируется виртуальный диск (внутреннее содержимое образа).
func (q *QemuNBDRunner) Start(port int) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.cmd != nil {
		return fmt.Errorf("qemu-nbd уже запущен")
	}

	path := q.filePath
	if q.overlayPath != "" {
		path = q.overlayPath
	}

	qemuNbd, err := exec.LookPath("qemu-nbd")
	if err != nil {
		return fmt.Errorf("qemu-nbd не найден в PATH. Установите QEMU (например: apt install qemu-utils, brew install qemu)")
	}

	args := []string{
		"-f", q.format,
		"-p", fmt.Sprintf("%d", port),
		"-b", q.getBindHost(),
	}
	if q.readOnly {
		args = append(args, "-r")
	}
	args = append(args, path)

	q.cmd = exec.Command(qemuNbd, args...)
	maybeHideWindow(q.cmd)
	q.cmd.Stdout = nil
	q.cmd.Stderr = nil
	if err := q.cmd.Start(); err != nil {
		q.cmd = nil
		return fmt.Errorf("запуск qemu-nbd: %w", err)
	}

	q.port = port
	q.running = true
	logrus.Infof("✅ [NBD-QEMU] qemu-nbd запущен: bind=%s порт=%d, формат=%s, readOnly=%v, путь=%s", q.getBindHost(), port, q.format, q.readOnly, path)

	// Готовность: даём qemu-nbd время открыть порт
	go func() {
		time.Sleep(500 * time.Millisecond)
		q.mu.Lock()
		ch := q.readyChan
		q.mu.Unlock()
		select {
		case <-ch:
		default:
			close(ch)
		}
	}()

	return nil
}

func (q *QemuNBDRunner) getBindHost() string {
	if strings.TrimSpace(q.bindHost) == "" {
		return "127.0.0.1"
	}
	return strings.TrimSpace(q.bindHost)
}

// Stop останавливает qemu-nbd и удаляет overlay при наличии.
func (q *QemuNBDRunner) Stop() error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.cmd == nil || q.cmd.Process == nil {
		return nil
	}

	if err := q.cmd.Process.Kill(); err != nil {
		logrus.Warnf("⚠️ Ошибка завершения qemu-nbd: %v", err)
	}
	_ = q.cmd.Wait()
	q.cmd = nil
	q.running = false

	if q.overlayPath != "" {
		// Overlay не удаляем — при следующем монтировании тот же overlay будет использован
		q.overlayPath = ""
	}

	return nil
}

// IsRunning возвращает true, если процесс qemu-nbd ещё запущен.
func (q *QemuNBDRunner) IsRunning() bool {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return q.running
}

// GetServerStatus возвращает статус в том же формате, что и NBDServer (server_port и т.д.).
func (q *QemuNBDRunner) GetServerStatus() map[string]interface{} {
	q.mu.RLock()
	defer q.mu.RUnlock()

	return map[string]interface{}{
		"is_running":   q.running,
		"export_count": 1,
		"client_count": 0,
		"server_port":  q.port,
		"exports": []map[string]interface{}{
			{
				"name":        "",
				"file_path":   q.filePath,
				"description": "qemu-nbd (виртуальный диск)",
			},
		},
	}
}

// WaitReady возвращает канал, закрываемый когда qemu-nbd готов принимать соединения.
func (q *QemuNBDRunner) WaitReady() <-chan struct{} {
	q.mu.RLock()
	defer q.mu.RUnlock()

	return q.readyChan
}

// SignalReady для qemu-nbd не меняет состояние (готовность задаётся в Start).
func (q *QemuNBDRunner) SignalReady() {
	q.mu.Lock()
	defer q.mu.Unlock()

	select {
	case <-q.readyChan:
	default:
		close(q.readyChan)
	}
}

// GetClients для qemu-nbd клиенты не отслеживаются.
func (q *QemuNBDRunner) GetClients() []*NBDClient {
	return nil
}

// NBDExportNameForConnection возвращает пустое имя — qemu-nbd экспортирует один диск без имени.
func (q *QemuNBDRunner) NBDExportNameForConnection() string {
	return ""
}

// NBDExportNameForAPI — уникальное непустое имя для API (отображение, идентификация). По порту: nbd_10809 — без путаницы при нескольких дисках.
func (q *QemuNBDRunner) NBDExportNameForAPI() string {
	q.mu.RLock()
	defer q.mu.RUnlock()
	if q.port == 0 {
		return "nbd_0"
	}
	return "nbd_" + strconv.Itoa(q.port)
}

// NBDHandshakeEmptyExport — у qemu-nbd один экспорт с пустым именем.
func (q *QemuNBDRunner) NBDHandshakeEmptyExport() bool {
	return true
}

// EnsureQemuNbdForExport при необходимости создаёт overlay и настраивает раннер на экспорт через qemu-nbd.
// Вызывать до Start. Для RW и overlay-совместимого формата создаётся overlay, path подменяется.
func (q *QemuNBDRunner) EnsureQemuNbdForExport() error {
	if runtime.GOOS == "android" {
		return fmt.Errorf("qemu-nbd на Android не поддерживается")
	}

	ext := strings.ToLower(q.filePath)
	if i := strings.LastIndex(ext, "."); i >= 0 {
		ext = ext[i:]
	} else {
		ext = ""
	}

	if !q.readOnly && IsOverlayCapableExtension(ext) {
		overlayPath, _, err := createOverlay(q.filePath)
		if err != nil {
			logrus.Warnf("⚠️ [NBD-QEMU] overlay не создан, экспорт только для чтения: %v", err)
			q.readOnly = true
			return nil
		}
		q.overlayPath = overlayPath
		q.format = "qcow2"
		logrus.Infof("✅ [NBD-QEMU] Экспорт RW через overlay: %s", overlayPath)
	}

	return nil
}
