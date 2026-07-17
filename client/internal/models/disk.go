package models

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DiskInfo information about a device/ISO image
type DiskInfo struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	URI         string `json:"uri"` // Android content:// URI (if applicable)
	Size        int64  `json:"size"`
	Type        string `json:"type"` // iso, img, device
	Description string `json:"description"`
	IsActive    bool   `json:"is_active"`
	LastUsed    string `json:"last_used"`
}

// DiskExport export for the NBD server or a local file
type DiskExport struct {
	Name        string `json:"name"`
	FilePath    string `json:"file_path"`
	Size        int64  `json:"size"`
	ReadOnly    bool   `json:"read_only"`
	Description string `json:"description"`
	IsActive    bool   `json:"is_active"`
	ExportName  string `json:"export_name"` // Export name in NBD
	DeviceID    int    `json:"device_id"`   // Device ID for disconnecting
	SourceType  string `json:"source_type"` // "nbd" or "local"
	NBDHost     string `json:"nbd_host"`    // NBD server IP address (NBD only)
	NBDPort     int    `json:"nbd_port"`    // NBD server port (NBD only)
}

// NewDiskInfo creates new device information
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

// getDiskType determines the device type from the file extension.
// Supported: ISO, IMG, VMDK, VDI, QCOW/QCOW2, RAW, VMI (images for NBD).
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

// getDescription returns the device type description
func getDescription(diskType string) string {
	switch diskType {
	case "iso":
		return "Device ISO image"
	case "img":
		return "Device image"
	case "device":
		return "Physical device"
	default:
		return "Unknown type"
	}
}

// FormatSize formats the size into a readable form
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

// IsSupported checks whether the file type is supported
func (d *DiskInfo) IsSupported(supportedTypes []string) bool {
	ext := strings.ToLower(filepath.Ext(d.Path))
	for _, supportedType := range supportedTypes {
		if strings.ToLower(supportedType) == ext {
			return true
		}
	}
	return false
}
