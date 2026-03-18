package models

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// USBStatus статус USB Bridge 2
type USBStatus struct {
	Success bool        `json:"success"`
	Message string      `json:"message"`
	Data    *StatusData `json:"data"`
}

// StatusData данные статуса
type StatusData struct {
	Service *ServiceStatus `json:"service"`
	NBD     *NBDStatus     `json:"nbd"`
	USB     *USBDeviceInfo `json:"usb"`
	Kernel  *KernelInfo    `json:"kernel"`
	Video   *VideoStatus   `json:"video"`
}

// ServiceStatus статус сервиса
type ServiceStatus struct {
	Status    string    `json:"status"`
	Timestamp time.Time `json:"timestamp"`
	Uptime    string    `json:"uptime"`
}

// NBDStatus статус NBD подключения
type NBDStatus struct {
	Connected bool   `json:"connected"`
	Device    string `json:"device"`
	Server    string `json:"server"`
	Port      int    `json:"port"`
	Export    string `json:"export"`
}

// USBDeviceInfo информация об USB устройстве
type USBDeviceInfo struct {
	Connected       bool   `json:"connected"`
	GadgetName      string `json:"gadget_name"`
	UDCName         string `json:"udc_name"`
	VendorID        string `json:"vendor_id"`
	ProductID       string `json:"product_id"`
	KeyboardEnabled bool   `json:"keyboard_enabled"`
}

// KernelInfo информация о ядре
type KernelInfo struct {
	ModulesLoaded bool     `json:"modules_loaded"`
	Modules       []string `json:"modules"`
}

// VideoStatus статус видео
type VideoStatus struct {
	Enabled         bool   `json:"enabled"`
	Device          string `json:"device"`
	Width           int    `json:"width"`
	Height          int    `json:"height"`
	FPS             int    `json:"fps"`
	Quality         int    `json:"quality"`
	Codec           string `json:"codec"`
	Bitrate         string `json:"bitrate"`
	PixelFormat     string `json:"pixel_format"`
	BufferSize      int    `json:"buffer_size"`
	StreamFormat    string `json:"stream_format"`
	LowLatency      bool   `json:"low_latency"`
	ClientsCount    int    `json:"clients_count"`
	Streaming       bool   `json:"streaming"`
}

// PCPanelLedsData данные о состоянии светодиодов PC Panel
type PCPanelLedsData struct {
	Power bool `json:"power"`
	HDD   bool `json:"hdd"`
}

// PCPanelLedsResponse ответ API /api/pcpanel/leds
type PCPanelLedsResponse struct {
	Success bool            `json:"success"`
	Message string          `json:"message"`
	Data    PCPanelLedsData `json:"data"`
}

// PCPanelButtonRequest запрос на нажатие кнопки Power/Reset
type PCPanelButtonRequest struct {
	Button string `json:"button"` // "power" или "reset"
}

// APIResponse стандартный ответ API
type APIResponse struct {
	Success bool        `json:"success"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
	Error   string      `json:"error,omitempty"`
}

// KeyboardRequest запрос на отправку клавиши
type KeyboardRequest struct {
	Action    string `json:"action"` // key, combo, text
	KeyCode   int    `json:"key_code,omitempty"`
	Modifiers int    `json:"modifiers,omitempty"`
	Text      string `json:"text,omitempty"`
}

// MouseRequest запрос на управление мышью или тачскрином
type MouseRequest struct {
	Action string `json:"action"`           // move, click, scroll, action (мышь) или touch (тачскрин)
	DX     int    `json:"dx,omitempty"`     // Смещение по X (от -128 до 127)
	DY     int    `json:"dy,omitempty"`     // Смещение по Y (от -128 до 127)
	X      int    `json:"x,omitempty"`      // X для тачскрина (0..4095)
	Y      int    `json:"y,omitempty"`      // Y для тачскрина (0..4095)
	Tip    bool   `json:"tip"`              // для action "touch": true = касание, false = отпускание (обязательно передавать; без omitempty чтобы false не опускался в JSON)
	Button int    `json:"button,omitempty"` // Кнопка мыши (1=левая, 2=правая, 3=средняя)
	Scroll int    `json:"scroll,omitempty"` // Прокрутка колесика (от -127 до 127)
	ButtonState int `json:"button_state,omitempty"` // Битмаска кнопок (bit0=L, bit1=R, bit2=M) для absolute_event
}

// DeviceStartRequest запрос на запуск устройства (старый формат - deprecated)
type DeviceStartRequest struct {
	Device                  string `json:"device"`                               // keyboard, drive, mouse и т.д.
	Type                    string `json:"type,omitempty"`                       // Для мыши: "mouse" (по умолчанию) или "touchscreen"
	RNDISMode               string `json:"rndis_mode,omitempty"`                 // Для RNDIS: "auto", "wifirouter", "etherouter", "etherbridge"
	Server                  string `json:"server,omitempty"`                     // IP сервера для NBD
	Port                    int    `json:"port,omitempty"`                       // Порт сервера для NBD
	ExportName              string `json:"export_name,omitempty"`                // Имя экспорта для API
	NBDHandshakeEmptyExport bool   `json:"nbd_handshake_empty_export,omitempty"` // true = в NBD handshake пустое имя (qemu-nbd)
	ReadOnly                bool   `json:"read_only"`                            // true = только чтение
	VendorID                string `json:"vendor_id"`                            // ID производителя
	ProductID               string `json:"product_id"`                           // ID продукта
	ProductName             string `json:"product_name"`                         // Название продукта
	Manufacturer            string `json:"manufacturer"`                         // Производитель
	KeyboardMode            bool   `json:"keyboard_mode,omitempty"`              // Использовать параметр -k для клавиатуры
}

// DeviceStartBatchRequest запрос на запуск нескольких устройств (старый API - deprecated)
type DeviceStartBatchRequest []DeviceStartRequest

// DeviceStartRequestNew новый формат запроса на запуск устройств через sources
type DeviceStartRequestNew struct {
	Sources  []string `json:"sources"`  // Список источников (nbd://, mtp://, /path/to/file)
	Keyboard bool     `json:"keyboard"` // Включить клавиатуру
	Mouse    bool     `json:"mouse"`    // Включить мышь
	RNDIS    bool     `json:"rndis"`    // Включить сетевую карту
}

// DeviceStopRequest запрос на остановку устройства
type DeviceStopRequest struct {
	ID int `json:"id"` // ID устройства для остановки
}

// DeviceInfo информация об устройстве
type DeviceInfo struct {
	ID           int       `json:"id"`
	Device       string    `json:"device"` // disk:имя_файла, keyboard, etc.
	Status       string    `json:"status"` // connected, disconnected
	VendorID     string    `json:"vendor_id"`
	ProductID    string    `json:"product_id"`
	ProductName  string    `json:"product_name"`
	Manufacturer string    `json:"manufacturer"`
	CreatedAt    time.Time `json:"created_at"`
	Type         string    `json:"type"` // nbd, local, keyboard
	Name         string    `json:"name"` // Имя устройства/файла
}

// DeviceInfoResponse ответ с информацией об устройствах
type DeviceInfoResponse struct {
	Devices         []DeviceInfo `json:"devices"`
	Count           int          `json:"count"`
	MountInProgress bool         `json:"mount_in_progress"` // true — монтирование идёт в фоне
	LastMountError  string       `json:"last_mount_error"`  // ошибка последнего монтирования
}

// DeviceStatusResponse ответ со статусом устройств (новый API)
type DeviceStatusResponse struct {
	Available         bool     `json:"available"`
	UnmountAvailable  bool     `json:"unmount_available"`
	ConnectedDevices  []string `json:"connected_devices"`
	KeyboardAvailable bool     `json:"keyboard_available"`
}

// VideoStartRequest запрос на запуск видео стриминга
type VideoStartRequest struct {
	VideoWidth   int    `json:"video_width"`
	VideoHeight  int    `json:"video_height"`
	VideoFPS     int    `json:"video_fps"`
	VideoQuality int    `json:"video_quality"`
	VideoBitrate string `json:"video_bitrate"`
	// ClientPort — порт клиента для приёма UDP потока (сервер возьмёт IP из HTTP)
	ClientPort int `json:"client_port,omitempty"`
}

// ConfigRequest запрос на обновление конфигурации
type ConfigRequest struct {
	NBDDevice         string           `json:"nbd_device,omitempty"`
	NBDServer         string           `json:"nbd_server,omitempty"`
	NBDPort           int              `json:"nbd_port,omitempty"`
	ExportName        string           `json:"export_name,omitempty"`
	GadgetName        string           `json:"gadget_name,omitempty"`
	UDCName           string           `json:"udc_name,omitempty"`
	VendorID          string           `json:"vendor_id,omitempty"`
	ProductID         string           `json:"product_id,omitempty"`
	ProductName       string           `json:"product_name,omitempty"`
	Manufacturer      string           `json:"manufacturer,omitempty"`
	KeyboardEnabled   bool             `json:"keyboard_enabled,omitempty"`
	VideoEnabled      bool             `json:"video_enabled,omitempty"`
	VideoDevice       string           `json:"video_device,omitempty"`
	VideoWidth        int              `json:"video_width,omitempty"`
	VideoHeight       int              `json:"video_height,omitempty"`
	VideoFPS          int              `json:"video_fps,omitempty"`
	VideoQuality      int              `json:"video_quality,omitempty"`
	VideoCodec        string           `json:"video_codec,omitempty"`
	VideoBitrate      string           `json:"video_bitrate,omitempty"`
	VideoPixelFormat  string           `json:"video_pixel_format,omitempty"`
	VideoBufferSize   int              `json:"video_buffer_size,omitempty"`
	VideoStreamFormat string           `json:"video_stream_format,omitempty"`
	VideoLowLatency   bool             `json:"video_low_latency,omitempty"`
	WebServer         *WebServerConfig `json:"web_server,omitempty"`
	CheckInterval     int              `json:"check_interval,omitempty"`
}

// WebServerConfig конфигурация веб-сервера
type WebServerConfig struct {
	Enabled bool   `json:"enabled"`
	Host    string `json:"host"`
	Port    int    `json:"port"`
}

// LocalDrive информация о локальном устройстве
type LocalDrive struct {
	Name        string    `json:"name"`         // Имя файла
	Size        int64     `json:"size"`         // Размер в байтах (общий объём)
	FreeSpace   int64     `json:"free_space"`   // Свободное место в байтах (0 = неизвестно)
	SizeHuman   string    `json:"size_human"`   // Размер в читаемом формате (может быть "used / total")
	Modified    time.Time `json:"modified"`     // Дата изменения
	IsMounted   bool      `json:"is_mounted"`   // Монтировано ли устройство
	MountDevice string    `json:"mount_device"` // Устройство монтирования
	FileType    string    `json:"file_type"`    // Тип файла (ISO, IMG, MTP, etc.)
	IsValid     bool      `json:"is_valid"`     // Валидный ли файл
	SourceType  string    `json:"source_type"`  // Тип источника (images, data, mtp)
	SourcePath  string    `json:"source_path"`  // Путь к источнику
}

// LocalDrivesResponse ответ с локальными устройствами
type LocalDrivesResponse struct {
	Drives []LocalDrive `json:"drives"`
	Count  int          `json:"count"`
	Path   string       `json:"path"` // Путь к папке с устройствами
}

// FormatSize форматирует размер в читаемый вид для LocalDrive
func (ld *LocalDrive) FormatSize() string {
	const unit = 1024
	if ld.Size < unit {
		return fmt.Sprintf("%d B", ld.Size)
	}
	div, exp := int64(unit), 0
	for n := ld.Size / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(ld.Size)/float64(div), "KMGTPE"[exp])
}

// parseSizeHumanPair парсит строку формата "X GB / Y GB" или "X B / Y GB", возвращает (used, total) в байтах.
func parseSizeHumanPair(s string) (used, total int64) {
	parts := strings.SplitN(strings.TrimSpace(s), "/", 2)
	if len(parts) != 2 {
		return -1, -1
	}
	used = parseHumanSize(strings.TrimSpace(parts[0]))
	total = parseHumanSize(strings.TrimSpace(parts[1]))
	return used, total
}

func parseHumanSize(s string) int64 {
	re := regexp.MustCompile(`(?i)^([\d.]+)\s*([KMGTPE]?B?)$`)
	m := re.FindStringSubmatch(strings.TrimSpace(s))
	if len(m) < 3 {
		return -1
	}
	val, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return -1
	}
	unit := strings.ToUpper(m[2])
	mult := int64(1)
	switch {
	case strings.HasPrefix(unit, "K"):
		mult = 1024
	case strings.HasPrefix(unit, "M"):
		mult = 1024 * 1024
	case strings.HasPrefix(unit, "G"):
		mult = 1024 * 1024 * 1024
	case strings.HasPrefix(unit, "T"):
		mult = 1024 * 1024 * 1024 * 1024
	case strings.HasPrefix(unit, "P"):
		mult = 1024 * 1024 * 1024 * 1024 * 1024
	case strings.HasPrefix(unit, "E"):
		mult = 1024 * 1024 * 1024 * 1024 * 1024 * 1024
	}
	return int64(val * float64(mult))
}

// StorageDisplay возвращает (totalBytes, freeBytes, usedPercent) для отображения.
// freeBytes=0 и usedPercent=0 если данные недоступны.
func (ld *LocalDrive) StorageDisplay() (totalBytes, freeBytes int64, usedPercent float64) {
	totalBytes = ld.Size
	if totalBytes <= 0 {
		return 0, 0, 0
	}
	if ld.FreeSpace > 0 {
		freeBytes = ld.FreeSpace
		usedBytes := totalBytes - freeBytes
		if totalBytes > 0 {
			usedPercent = float64(usedBytes) / float64(totalBytes) * 100
		}
		return totalBytes, freeBytes, usedPercent
	}
	// Пробуем распарсить SizeHuman формата "used / total" (например "5.2 GB / 32 GB")
	if ld.SizeHuman != "" {
		used, total := parseSizeHumanPair(ld.SizeHuman)
		if used >= 0 && total > 0 {
			totalBytes = total
			freeBytes = total - used
			usedPercent = float64(used) / float64(total) * 100
			return totalBytes, freeBytes, usedPercent
		}
	}
	return totalBytes, 0, 0
}

// FormatSizeShort форматирует байты кратко (для отображения в заголовке).
func FormatSizeShort(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// FormatStorageCompact возвращает компактную строку для индикатора: "43% 66/119 GB" с MB/GB/TB по размеру.
func FormatStorageCompact(available, total int64, usedPct float64) string {
	const unit = 1024
	if total < unit {
		return fmt.Sprintf("%.0f%% %d/%d B", usedPct, available, total)
	}
	// Выбираем единицу по total: KB, MB, GB, TB
	div, exp := int64(unit), 0
	for n := total / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	if exp > 3 {
		exp = 3
		div = unit * unit * unit * unit
	}
	unitStr := string([]byte{'K', 'M', 'G', 'T'}[exp]) + "B"
	av := float64(available) / float64(div)
	tot := float64(total) / float64(div)
	var avStr, totStr string
	if tot >= 100 {
		avStr = fmt.Sprintf("%.0f", av)
		totStr = fmt.Sprintf("%.0f", tot)
	} else if tot >= 10 {
		avStr = fmt.Sprintf("%.1f", av)
		totStr = fmt.Sprintf("%.1f", tot)
	} else {
		avStr = fmt.Sprintf("%.1f", av)
		totStr = fmt.Sprintf("%.1f", tot)
	}
	return fmt.Sprintf("%.0f%% %s/%s %s", usedPct, avStr, totStr, unitStr)
}

// FormatStorageSizeOnly возвращает только занятость: "53/119 GB" (занято/всего, для мелкого текста под процентом).
func FormatStorageSizeOnly(used, total int64) string {
	const unit = 1024
	if total < unit {
		return fmt.Sprintf("%d/%d B", used, total)
	}
	div, exp := int64(unit), 0
	for n := total / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	if exp > 3 {
		exp = 3
		div = unit * unit * unit * unit
	}
	unitStr := string([]byte{'K', 'M', 'G', 'T'}[exp]) + "B"
	usedF := float64(used) / float64(div)
	tot := float64(total) / float64(div)
	var usedStr, totStr string
	if tot >= 100 {
		usedStr = fmt.Sprintf("%.0f", usedF)
		totStr = fmt.Sprintf("%.0f", tot)
	} else if tot >= 10 {
		usedStr = fmt.Sprintf("%.1f", usedF)
		totStr = fmt.Sprintf("%.1f", tot)
	} else {
		usedStr = fmt.Sprintf("%.1f", usedF)
		totStr = fmt.Sprintf("%.1f", tot)
	}
	return fmt.Sprintf("%s/%s %s", usedStr, totStr, unitStr)
}
