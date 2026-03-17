// Package service: создание qcow2-overlay для NBD экспорта в режиме RW без порчи базового образа.
// Только десктоп (не Android); требует qemu-img в PATH.

package service

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/sirupsen/logrus"
)

const overlaySuffix = ".overlay.qcow2"

// backingFormat возвращает формат backing file для qemu-img по расширению.
func backingFormat(ext string) string {
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

// IsOverlayCapableExtension возвращает true, если для формата можно создать overlay (qcow2 с backing).
// Используется в UI для отображения переключателя RO/RW у образов vdi, vmdk, qcow2 и т.д.
func IsOverlayCapableExtension(ext string) bool {
	switch strings.ToLower(ext) {
	case ".qcow2", ".qcow", ".vmdk", ".vdi", ".raw", ".img", ".vmi":
		return true
	default:
		return false
	}
}

// qemuImgPath ищет qemu-img в PATH.
func qemuImgPath() string {
	path, _ := exec.LookPath("qemu-img")
	return path
}

// qemuImgVirtualSize возвращает виртуальный размер образа в байтах (qemu-img info --output=json).
func qemuImgVirtualSize(imagePath string) (int64, error) {
	qemuImg := qemuImgPath()
	if qemuImg == "" {
		return 0, fmt.Errorf("qemu-img не найден в PATH")
	}
	cmd := exec.Command(qemuImg, "info", "--output=json", imagePath)
	cmd.Env = os.Environ()
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return 0, fmt.Errorf("qemu-img info: %s: %w", string(ee.Stderr), err)
		}
		return 0, fmt.Errorf("qemu-img info: %w", err)
	}
	var info struct {
		VirtualSize int64 `json:"virtual-size"`
	}
	if err := json.Unmarshal(out, &info); err != nil {
		return 0, fmt.Errorf("парсинг qemu-img info: %w", err)
	}
	if info.VirtualSize <= 0 {
		return 0, fmt.Errorf("qemu-img info: virtual-size не найден или 0")
	}
	return info.VirtualSize, nil
}

// createOverlay создаёт qcow2 overlay с backing на basePath (рядом с базой: base.overlay.qcow2).
// Возвращает путь к overlay и виртуальный размер. Только для десктопа; basePath должен быть абсолютным.
func createOverlay(basePath string) (overlayPath string, virtualSize int64, err error) {
	qemuImg := qemuImgPath()
	if qemuImg == "" {
		return "", 0, fmt.Errorf("qemu-img не найден в PATH: для RW-экспорта через overlay установите QEMU")
	}
	baseAbs, err := filepath.Abs(basePath)
	if err != nil {
		return "", 0, fmt.Errorf("абсолютный путь базы: %w", err)
	}
	ext := strings.ToLower(filepath.Ext(baseAbs))
	if !IsOverlayCapableExtension(ext) {
		return "", 0, fmt.Errorf("формат %s не поддерживает overlay", ext)
	}
	dir := filepath.Dir(baseAbs)
	baseName := strings.TrimSuffix(filepath.Base(baseAbs), filepath.Ext(baseAbs))
	// Один overlay на образ: baseName.overlay.qcow2 — при повторном запуске используем уже созданный
	overlayPath = filepath.Join(dir, baseName+overlaySuffix)
	if _, err := os.Stat(overlayPath); err == nil {
		virtualSize, err = qemuImgVirtualSize(overlayPath)
		if err != nil {
			return "", 0, fmt.Errorf("получение размера существующего overlay: %w", err)
		}
		logrus.Infof("✅ [NBD-OVERLAY] Используется существующий overlay %s (backing %s)", overlayPath, baseAbs)
		return overlayPath, virtualSize, nil
	}
	format := backingFormat(ext)
	// qemu-img create -f qcow2 -b <base> -F <format> <overlay>
	cmd := exec.Command(qemuImg, "create", "-f", "qcow2", "-b", baseAbs, "-F", format, overlayPath)
	cmd.Env = os.Environ()
	if out, err := cmd.CombinedOutput(); err != nil {
		// Overlay не удаляем даже при ошибке — не трогаем существующие файлы
		return "", 0, fmt.Errorf("qemu-img create overlay: %s: %w", string(out), err)
	}
	logrus.Infof("✅ [NBD-OVERLAY] Создан overlay %s (backing %s, format %s)", overlayPath, baseAbs, format)
	virtualSize, err = qemuImgVirtualSize(overlayPath)
	if err != nil {
		// Overlay не удаляем — оставляем созданный файл
		return "", 0, fmt.Errorf("получение размера overlay: %w", err)
	}
	return overlayPath, virtualSize, nil
}
