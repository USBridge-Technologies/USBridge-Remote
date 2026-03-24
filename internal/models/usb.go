package models

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// USBStatus статус USBridge 2
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
	Enabled           bool                 `json:"enabled"`
	Device            string               `json:"device"`
	Width             int                  `json:"width"`
	Height            int                  `json:"height"`
	FPS               int                  `json:"fps"`
	Quality           int                  `json:"quality"`
	Bitrate           string               `json:"bitrate"`
	BufferSize        int                  `json:"buffer_size"`
	Mode              string               `json:"mode"`
	Transport         string               `json:"transport"`
	Encoding          string               `json:"encoding"`
	SourceFormat      string               `json:"source_format"`
	ServerDecodesJPEG bool                 `json:"server_decodes_jpeg"`
	CaptureModes      []VideoCaptureMode   `json:"capture_modes,omitempty"`
	SupportedModes    []VideoTransportMode `json:"supported_modes,omitempty"`
	ClientsCount      int                  `json:"clients_count"`
	Streaming         bool                 `json:"streaming"`
}

const (
	VideoModeH264    = "h264"
	VideoModeJPEGRTP = "jpeg_rtp"
)

type VideoTransportMode struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	Description       string `json:"description"`
	Transport         string `json:"transport"`
	Encoding          string `json:"encoding"`
	ServerDecodesJPEG bool   `json:"server_decodes_jpeg"`
}

type VideoCaptureMode struct {
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	FPS         []int  `json:"fps"`
	PixelFormat string `json:"pixel_format"`
}

type VideoInfoData struct {
	VideoStatus
	UDPPort          int            `json:"udp_port"`
	StreamURL        string         `json:"stream_url"`
	UDPListenerReady bool           `json:"udp_listener_ready"`
	AvailableDevices []SystemDevice `json:"available_devices,omitempty"`
}

func ParseVideoInfoData(data interface{}) (*VideoInfoData, error) {
	raw, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal video info data: %w", err)
	}

	var parsed VideoInfoData
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("failed to parse video info data: %w", err)
	}

	if parsed.Mode == "" {
		parsed.Mode = VideoModeH264
	}
	if parsed.Encoding == "" {
		parsed.Encoding = "h264"
	}
	if parsed.Transport == "" {
		parsed.Transport = "rtp"
	}
	return &parsed, nil
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
	Action      string `json:"action"`                 // move, click, scroll, action (мышь) или touch (тачскрин)
	DX          int    `json:"dx,omitempty"`           // Смещение по X (от -128 до 127)
	DY          int    `json:"dy,omitempty"`           // Смещение по Y (от -128 до 127)
	X           *int   `json:"x,omitempty"`            // X для тачскрина (0..4095); указатель нужен, чтобы 0 не выпадал из JSON
	Y           *int   `json:"y,omitempty"`            // Y для тачскрина (0..4095); указатель нужен, чтобы 0 не выпадал из JSON
	Tip         bool   `json:"tip"`                    // для action "touch": true = касание, false = отпускание (обязательно передавать; без omitempty чтобы false не опускался в JSON)
	Button      int    `json:"button,omitempty"`       // Кнопка мыши (1=левая, 2=правая, 3=средняя)
	Scroll      int    `json:"scroll,omitempty"`       // Прокрутка колесика (от -127 до 127)
	ButtonState int    `json:"button_state,omitempty"` // Битмаска кнопок (bit0=L, bit1=R, bit2=M) для absolute_event
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
	VideoDevice  string `json:"video_device,omitempty"`
	VideoWidth   int    `json:"video_width"`
	VideoHeight  int    `json:"video_height"`
	VideoFPS     int    `json:"video_fps"`
	VideoQuality int    `json:"video_quality"`
	VideoBitrate string `json:"video_bitrate"`
	VideoMode    string `json:"video_mode,omitempty"`
	// ClientPort — порт клиента для приёма UDP потока (сервер возьмёт IP из HTTP)
	ClientPort int `json:"client_port,omitempty"`
}

// SystemDevice устройство, видимое на стороне bridge через /api/devices.
type SystemDevice struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	Connected   bool   `json:"connected"`
	Description string `json:"description"`
}

// VideoDeviceConfig сохраненный клиентом конфиг запуска видео для конкретного /dev/video*.
type VideoDeviceConfig struct {
	DevicePath    string `json:"device_path"`
	DeviceName    string `json:"device_name,omitempty"`
	VideoWidth    int    `json:"video_width"`
	VideoHeight   int    `json:"video_height"`
	VideoFPS      int    `json:"video_fps"`
	VideoQuality  int    `json:"video_quality"`
	VideoBitrate  string `json:"video_bitrate"`
	VideoMode     string `json:"video_mode"`
	LastAppliedAt int64  `json:"last_applied_at,omitempty"`
}

func (c VideoDeviceConfig) ToVideoStartRequest() *VideoStartRequest {
	return &VideoStartRequest{
		VideoDevice:  c.DevicePath,
		VideoWidth:   c.VideoWidth,
		VideoHeight:  c.VideoHeight,
		VideoFPS:     c.VideoFPS,
		VideoQuality: c.VideoQuality,
		VideoBitrate: c.VideoBitrate,
		VideoMode:    c.VideoMode,
	}
}

type WireGuardBootstrapRequest struct {
	Token           string `json:"token"`
	ClientName      string `json:"client_name,omitempty"`
	ClientPublicKey string `json:"client_public_key"`
	EndpointHost    string `json:"endpoint_host,omitempty"`
	ServerHost      string `json:"server_host,omitempty"`
}

type WireGuardBootstrapResponse struct {
	InterfaceName       string   `json:"interface_name"`
	ServerPublicKey     string   `json:"server_public_key"`
	ServerEndpointHost  string   `json:"server_endpoint_host"`
	ServerEndpointPort  int      `json:"server_endpoint_port"`
	ServerAddress       string   `json:"server_address"`
	ServerAddressCIDR   string   `json:"server_address_cidr"`
	ClientAddress       string   `json:"client_address"`
	ClientAddressCIDR   string   `json:"client_address_cidr"`
	AllowedIPs          []string `json:"allowed_ips"`
	PersistentKeepalive int      `json:"persistent_keepalive"`
	MTU                 int      `json:"mtu"`
	ClientPrivateKey    string   `json:"client_private_key,omitempty"`
}

func DecodeWireGuardInvite(invite string) (*WireGuardBootstrapResponse, error) {
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(invite))
	if err != nil {
		return nil, fmt.Errorf("failed to decode WireGuard invite: %w", err)
	}
	var resp WireGuardBootstrapResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse WireGuard invite: %w", err)
	}
	return &resp, nil
}

// ConfigRequest запрос на обновление конфигурации
type ConfigRequest struct {
	NBDDevice       string           `json:"nbd_device,omitempty"`
	NBDServer       string           `json:"nbd_server,omitempty"`
	NBDPort         int              `json:"nbd_port,omitempty"`
	ExportName      string           `json:"export_name,omitempty"`
	GadgetName      string           `json:"gadget_name,omitempty"`
	UDCName         string           `json:"udc_name,omitempty"`
	VendorID        string           `json:"vendor_id,omitempty"`
	ProductID       string           `json:"product_id,omitempty"`
	ProductName     string           `json:"product_name,omitempty"`
	Manufacturer    string           `json:"manufacturer,omitempty"`
	KeyboardEnabled bool             `json:"keyboard_enabled,omitempty"`
	VideoEnabled    bool             `json:"video_enabled,omitempty"`
	VideoDevice     string           `json:"video_device,omitempty"`
	VideoWidth      int              `json:"video_width,omitempty"`
	VideoHeight     int              `json:"video_height,omitempty"`
	VideoFPS        int              `json:"video_fps,omitempty"`
	VideoQuality    int              `json:"video_quality,omitempty"`
	VideoBitrate    string           `json:"video_bitrate,omitempty"`
	VideoBufferSize int              `json:"video_buffer_size,omitempty"`
	WebServer       *WebServerConfig `json:"web_server,omitempty"`
	CheckInterval   int              `json:"check_interval,omitempty"`
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
