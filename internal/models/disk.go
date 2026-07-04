package models

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DiskInfo информация об устройстве/ISO образе
type DiskInfo struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	URI         string `json:"uri"` // Android content:// URI (если применимо)
	Size        int64  `json:"size"`
	Type        string `json:"type"` // iso, img, device
	Description string `json:"description"`
	IsActive    bool   `json:"is_active"`
	LastUsed    string `json:"last_used"`
}

// DiskExport экспорт для NBD сервера или локальный файл
type DiskExport struct {
	Name        string `json:"name"`
	FilePath    string `json:"file_path"`
	Size        int64  `json:"size"`
	ReadOnly    bool   `json:"read_only"`
	Description string `json:"description"`
	IsActive    bool   `json:"is_active"`
	ExportName  string `json:"export_name"` // Имя экспорта в NBD
	DeviceID    int    `json:"device_id"`   // ID устройства для отключения
	SourceType  string `json:"source_type"` // "nbd" или "local"
	NBDHost     string `json:"nbd_host"`    // IP адрес NBD сервера (только для NBD)
	NBDPort     int    `json:"nbd_port"`    // Порт NBD сервера (только для NBD)
}

// NewDiskInfo создает новую информацию об устройстве
func NewDiskInfo(path string) (*DiskInfo, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	diskType := getDiskType(path)

	return &DiskInfo{
		Name:        filepath.Base(path),
		Path:        path,
		Size:        info.Size(),
		Type:        diskType,
		Description: getDescription(diskType),
		IsActive:    false,
		LastUsed:    "",
	}, nil
}

// getDiskType определяет тип устройства по расширению файла.
// Поддерживаются: ISO, IMG, VMDK, VDI, QCOW/QCOW2, RAW, VMI (образы для NBD).
func getDiskType(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".iso":
		return "iso"
	case ".img", ".vmdk", ".vdi", ".qcow", ".qcow2", ".raw", ".vmi":
		return "img"
	default:
		return "unknown"
	}
}

// getDescription возвращает описание типа устройства
func getDescription(diskType string) string {
	switch diskType {
	case "iso":
		return "ISO образ устройства"
	case "img":
		return "Образ устройства"
	case "device":
		return "Физическое устройство"
	default:
		return "Неизвестный тип"
	}
}

// FormatSize форматирует размер в читаемый вид
func (d *DiskInfo) FormatSize() string {
	const unit = 1024
	if d.Size < unit {
		return fmt.Sprintf("%d B", d.Size)
	}
	div, exp := int64(unit), 0
	for n := d.Size / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(d.Size)/float64(div), "KMGTPE"[exp])
}

// IsSupported проверяет, поддерживается ли тип файла
func (d *DiskInfo) IsSupported(supportedTypes []string) bool {
	ext := strings.ToLower(filepath.Ext(d.Path))
	for _, supportedType := range supportedTypes {
		if strings.ToLower(supportedType) == ext {
			return true
		}
	}
	return false
}
