package models

import (
	"fmt"
	"strings"
	"time"
)

// DefaultVideoUDPPort — порт приёма RTP H.264 по UDP. 55000 в динамическом диапазоне (49152–65535),
// свободен на Windows (nidmsrv/UPnP часто занимают 5000), macOS (AirPlay), Linux, Android.
const DefaultVideoUDPPort = 55000

const (
	ConnectionProtocolAuto      = "auto"
	ConnectionProtocolQUIC      = "quic"
	ConnectionProtocolWireGuard = "wireguard"
	ConnectionProtocolTailscale = "tailscale"
)

// AppConfig конфигурация приложения
type AppConfig struct {
	// USBridge 2 подключение (как клиент)
	USBPort    int `json:"usb_port" mapstructure:"usb_port"`       // Порт USBridge 2 (8080)
	APITimeout int `json:"api_timeout" mapstructure:"api_timeout"` // Таймаут API запросов

	// FRP настройки (QUIC туннель)
	FRPServerPort      int    `json:"frp_server_port" mapstructure:"frp_server_port"` // Порт FRP сервера (443)
	FRPAuthToken       string `json:"frp_auth_token" mapstructure:"frp_auth_token"`   // Токен аутентификации FRP
	FRPEnabled         bool   `json:"frp_enabled" mapstructure:"frp_enabled"`         // Включить FRP туннель
	FRPTLSCert         string `json:"frp_tls_cert" mapstructure:"frp_tls_cert"`       // Путь к TLS сертификату
	FRPTLSKey          string `json:"frp_tls_key" mapstructure:"frp_tls_key"`         // Путь к TLS ключу
	FRPTLSCa           string `json:"frp_tls_ca" mapstructure:"frp_tls_ca"`           // Путь к CA сертификату
	ConnectionProtocol string `json:"connection_protocol" mapstructure:"connection_protocol"`

	// WireGuard настройки
	WireGuardEnabled       bool   `json:"wireguard_enabled" mapstructure:"wireguard_enabled"`
	WireGuardInterfaceName string `json:"wireguard_interface_name" mapstructure:"wireguard_interface_name"`
	WireGuardServerPort    int    `json:"wireguard_server_port" mapstructure:"wireguard_server_port"`
	WireGuardListenPort    int    `json:"wireguard_listen_port" mapstructure:"wireguard_listen_port"`

	// Tailscale настройки
	TailscaleEnabled bool `json:"tailscale_enabled" mapstructure:"tailscale_enabled"`

	// NBD сервер (как сервер)
	NBDPort           int      `json:"nbd_port" mapstructure:"nbd_port"` // Порт NBD сервера (10809)
	NBDBindHost       string   `json:"nbd_bind_host" mapstructure:"nbd_bind_host"`
	MaxClients        int      `json:"max_clients" mapstructure:"max_clients"`                   // Максимум NBD клиентов
	ScanPaths         []string `json:"scan_paths" mapstructure:"scan_paths"`                     // Пути для сканирования устройств
	SupportedTypes    []string `json:"supported_types" mapstructure:"supported_types"`           // Поддерживаемые типы файлов
	NBDExportReadOnly bool     `json:"nbd_export_read_only" mapstructure:"nbd_export_read_only"` // true = экспорт только для чтения (безопасный дефолт), false = RW через overlay

	// Хост видеопотока (задаётся из адресной строки; в конфиге не хранится)
	VideoHost     string `json:"-"`
	VideoBindHost string `json:"video_bind_host" mapstructure:"video_bind_host"`

	// Видео UDP (новый протокол)
	VideoUDPPort int `json:"video_udp_port" mapstructure:"video_udp_port"` // Порт приёма RTP видео по UDP (55000 — свободен на Win/Mac/Linux/Android)

	// Видео настройки
	VideoBitrate   int  `json:"video_bitrate" mapstructure:"video_bitrate"` // kbps
	VideoWidth     int  `json:"video_width" mapstructure:"video_width"`
	VideoHeight    int  `json:"video_height" mapstructure:"video_height"`
	VideoFPS       int  `json:"video_fps" mapstructure:"video_fps"`
	LowLatencyMode bool `json:"low_latency_mode" mapstructure:"low_latency_mode"` // Режим низкой задержки
	BufferSize     int  `json:"buffer_size" mapstructure:"buffer_size"`           // Размер буфера кадров
	SkipFrameDelay bool `json:"skip_frame_delay" mapstructure:"skip_frame_delay"` // Пропускать задержки между кадрами

	// Аудио настройки
	AudioCodec      string `json:"audio_codec" mapstructure:"audio_codec"`     // Opus, G.711
	AudioBitrate    int    `json:"audio_bitrate" mapstructure:"audio_bitrate"` // kbps
	AudioSampleRate int    `json:"audio_sample_rate" mapstructure:"audio_sample_rate"`
	AudioChannels   int    `json:"audio_channels" mapstructure:"audio_channels"`

	// UI настройки
	WindowWidth  int    `json:"window_width" mapstructure:"window_width"`
	WindowHeight int    `json:"window_height" mapstructure:"window_height"`
	Theme        string `json:"theme" mapstructure:"theme"` // light, dark
	LogLevel     string `json:"log_level" mapstructure:"log_level"`
}

// DefaultConfig возвращает конфигурацию по умолчанию
func DefaultConfig() *AppConfig {
	return &AppConfig{
		// USBridge 2
		USBPort:    8080,
		APITimeout: 30,

		// FRP
		FRPServerPort:          443,
		FRPAuthToken:           "usbridge-secret-token",
		FRPEnabled:             true,
		FRPTLSCert:             "./certs/server.crt",
		FRPTLSKey:              "./certs/server.key",
		FRPTLSCa:               "./certs/ca.crt",
		ConnectionProtocol:     modelsafeProtocol(ConnectionProtocolAuto),
		WireGuardEnabled:       true,
		WireGuardInterfaceName: "usbwg0",
		WireGuardServerPort:    51820,
		WireGuardListenPort:    51821,
		TailscaleEnabled:       true,

		// NBD сервер
		NBDPort:           10809,
		NBDBindHost:       "127.0.0.1",
		MaxClients:        5,
		ScanPaths:         []string{"./isos", "/home/user/isos", "/mnt/isos"},
		SupportedTypes:    []string{".iso", ".img", ".vmdk", ".vdi", ".qcow", ".qcow2", ".raw", ".vmi"},
		NBDExportReadOnly: true, // безопасный дефолт: RO; false = экспорт RW через overlay (база не портится)

		// Видео UDP (55000 — динамический диапазон, не конфликтует с nidmsrv/UPnP/AirPlay и т.д.)
		VideoUDPPort:  DefaultVideoUDPPort,
		VideoBindHost: "127.0.0.1",

		// Видео
		VideoBitrate:   2000,
		VideoWidth:     640,
		VideoHeight:    480,
		VideoFPS:       30,
		LowLatencyMode: true, // Включаем режим низкой задержки по умолчанию
		BufferSize:     2,    // Минимальный буфер кадров
		SkipFrameDelay: true, // Пропускаем задержки между кадрами

		// Аудио
		AudioCodec:      "Opus",
		AudioBitrate:    128,
		AudioSampleRate: 48000,
		AudioChannels:   2,

		// UI — размер по умолчанию удобен для современных DPI и масштабирования
		WindowWidth:  960,
		WindowHeight: 640,
		Theme:        "light",
		LogLevel:     "info",
	}
}

func modelsafeProtocol(protocol string) string {
	switch strings.ToLower(strings.TrimSpace(protocol)) {
	case ConnectionProtocolWireGuard:
		return ConnectionProtocolTailscale
	case ConnectionProtocolQUIC, ConnectionProtocolTailscale:
		return strings.ToLower(strings.TrimSpace(protocol))
	default:
		return ConnectionProtocolAuto
	}
}

// AppState состояние приложения
type AppState struct {
	IsConnected      bool      `json:"is_connected"`
	IsStreaming      bool      `json:"is_streaming"`
	IsNBDRunning     bool      `json:"is_nbd_running"`
	CurrentISO       string    `json:"current_iso"`
	LastConnected    time.Time `json:"last_connected"`
	LastDisconnected time.Time `json:"last_disconnected"`
}

// SnapshotInfo информация о снапшоте
type SnapshotInfo struct {
	Name        string    `json:"name"`
	Size        int64     `json:"size"`
	SizeHuman   string    `json:"size_human"` // Размер в читаемом формате от API (например "0 B / 40.0 GB")
	Changelog   string    `json:"changelog"`  // Changelog снапшота (сырой от btrfs)
	CreatedAt   time.Time `json:"created_at"`
	Description string    `json:"description,omitempty"`
	Path        string    `json:"path"`
	Connected   bool      `json:"connected"`
}

// SnapshotJSON структура для парсинга JSON ответа от сервера
type SnapshotJSON struct {
	Name       string `json:"name"`
	Date       string `json:"date"`
	Timestamp  int64  `json:"timestamp"`
	Size       int64  `json:"size"`
	SizeHuman  string `json:"size_human"`
	Changelog  string `json:"changelog"`
	FilesCount int    `json:"files_count"`
	IsLatest   bool   `json:"is_latest"`
	Connected  bool   `json:"connected"`
}

// ToSnapshotInfo преобразует SnapshotJSON в SnapshotInfo
func (sj *SnapshotJSON) ToSnapshotInfo() *SnapshotInfo {
	// Пытаемся парсить дату из строки, если не получается - используем timestamp
	var createdAt time.Time
	var err error

	if sj.Date != "" {
		// Пробуем разные форматы даты
		formats := []string{
			"2006-01-02 15:04:05",
			"2006-01-02T15:04:05",
			"2006-01-02 15:04:05Z",
			"2006-01-02T15:04:05Z",
			"2006-01-02T15:04:05.000Z",
		}

		for _, format := range formats {
			if createdAt, err = time.Parse(format, sj.Date); err == nil {
				break
			}
		}
	}

	// Если не удалось парсить дату из строки, используем timestamp
	if err != nil {
		createdAt = time.Unix(sj.Timestamp, 0)
	}

	return &SnapshotInfo{
		Name:        sj.Name,
		Size:        sj.Size,
		SizeHuman:   sj.SizeHuman,
		Changelog:   sj.Changelog,
		CreatedAt:   createdAt,
		Description: "",
		Path:        "",
		Connected:   sj.Connected,
	}
}

// FormatSize форматирует размер в человекочитаемый вид (из байтов)
func (s *SnapshotInfo) FormatSize() string {
	const unit = 1024
	if s.Size < unit {
		return fmt.Sprintf("%d B", s.Size)
	}
	div, exp := int64(unit), 0
	for n := s.Size / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(s.Size)/float64(div), "KMGTPE"[exp])
}

// DisplaySize возвращает размер для отображения: приоритет у size_human от API
func (s *SnapshotInfo) DisplaySize() string {
	if s.SizeHuman != "" {
		return s.SizeHuman
	}
	return s.FormatSize()
}

// splitChangelogLine разбивает строку changelog, сохраняя экранированные пробелы (\ )
func splitChangelogLine(line string) []string {
	// Заменяем \ (backslash-space) на placeholder, чтобы не разбивать пути с пробелами
	const placeholder = "\u200B" // zero-width space
	line = strings.ReplaceAll(line, "\\ ", placeholder)
	parts := strings.Fields(line)
	for i := range parts {
		parts[i] = strings.ReplaceAll(parts[i], placeholder, " ")
	}
	return parts
}

// isParamPart проверяет, является ли part параметром key=value (dest=, from=, uuid= и т.д.)
func isParamPart(part string) bool {
	idx := strings.Index(part, "=")
	if idx <= 0 {
		return false
	}
	key := part[:idx]
	for _, c := range key {
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_') {
			return false
		}
	}
	return len(key) > 0
}

// extractPathFromParts извлекает путь, который может занимать несколько parts (при пробелах в имени)
func extractPathFromParts(parts []string) (path string, params []string) {
	if len(parts) < 2 {
		return "", parts
	}
	var pathParts []string
	for i := 1; i < len(parts); i++ {
		if isParamPart(parts[i]) {
			return strings.Join(pathParts, " "), parts[i:]
		}
		pathParts = append(pathParts, parts[i])
	}
	return strings.Join(pathParts, " "), nil
}

// extractPathParam извлекает значение параметра key= из parts (from=, dest=).
// Значение может занимать несколько parts, если путь содержит неэкранированные пробелы.
func extractPathParam(parts []string, prefix string) string {
	for i, p := range parts {
		if !strings.HasPrefix(p, prefix) {
			continue
		}
		val := strings.TrimPrefix(p, prefix)
		// Собираем последующие parts до следующего key=value (если значение разбито)
		for j := i + 1; j < len(parts); j++ {
			next := parts[j]
			if strings.Contains(next, "=") && !strings.HasPrefix(next, prefix) {
				break // Следующий параметр key=value
			}
			val += " " + next
		}
		val = simplifyPath(val)
		return val
	}
	return ""
}

// simplifyPath оставляет только имя файла, убирает ./ и trailing /
func simplifyPath(p string) string {
	p = strings.TrimPrefix(p, "./")
	p = strings.TrimSuffix(p, "/")
	if idx := strings.LastIndex(p, "/"); idx >= 0 && idx < len(p)-1 {
		p = p[idx+1:]
	}
	return p
}

// ChangelogFormatOptions опции форматирования changelog (для локализации)
type ChangelogFormatOptions struct {
	OpNames       map[string]string // переводы операций btrfs: "snapshot" -> "создание снапшота"
	TempFileLabel string            // не используется, оставлено для совместимости
}

// formatPathForDisplay возвращает путь для отображения (BTRFS-имена показываются как есть)
func formatPathForDisplay(path, _ string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return path
	}
	return path
}

// FormatChangelog форматирует сырой changelog btrfs в удобочитаемый вид.
// opts — локализованные подписи (если nil, используются значения по умолчанию).
func (s *SnapshotInfo) FormatChangelog(opts *ChangelogFormatOptions) string {
	raw := strings.TrimSpace(s.Changelog)
	if raw == "" {
		return ""
	}
	opNames := map[string]string{
		"snapshot": "snapshot creation",
		"utimes":   "time update",
		"mkfile":   "file creation",
		"rename":   "rename",
		"truncate": "file truncation",
		"clone":    "clone",
		"chown":    "owner change",
		"chmod":    "permissions change",
	}
	tempFileLabel := "temporary file"
	if opts != nil {
		if opts.OpNames != nil {
			opNames = opts.OpNames
		}
		if opts.TempFileLabel != "" {
			tempFileLabel = opts.TempFileLabel
		}
	}
	var lines []string
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := splitChangelogLine(line)
		if len(parts) < 2 {
			lines = append(lines, "• "+line)
			continue
		}
		op := parts[0]
		path, params := extractPathFromParts(parts)
		path = formatPathForDisplay(simplifyPath(path), tempFileLabel)
		if name, ok := opNames[op]; ok {
			op = name
		}
		if len(params) > 0 {
			dest := formatPathForDisplay(extractPathParam(params, "dest="), tempFileLabel)
			from := formatPathForDisplay(extractPathParam(params, "from="), tempFileLabel)
			if dest != "" {
				// rename: source → dest
				lines = append(lines, fmt.Sprintf("• %s: %s → %s", op, path, dest))
			} else if from != "" {
				// clone: from (источник) → path (назначение)
				lines = append(lines, fmt.Sprintf("• %s: %s → %s", op, from, path))
			} else {
				lines = append(lines, fmt.Sprintf("• %s: %s", op, path))
			}
		} else {
			lines = append(lines, fmt.Sprintf("• %s: %s", op, path))
		}
	}
	return strings.Join(lines, "\n")
}

// SnapshotsResponse ответ с списком снапшотов
type SnapshotsResponse struct {
	Count     int            `json:"count"`
	Snapshots []SnapshotInfo `json:"snapshots"`
}

// SnapshotsJSONResponse структура для парсинга JSON ответа от сервера
type SnapshotsJSONResponse struct {
	Count     int            `json:"count"`
	Total     int            `json:"total"`
	Snapshots []SnapshotJSON `json:"snapshots"`
}

// ToSnapshotsResponse преобразует SnapshotsJSONResponse в SnapshotsResponse
func (sjr *SnapshotsJSONResponse) ToSnapshotsResponse() *SnapshotsResponse {
	snapshots := make([]SnapshotInfo, len(sjr.Snapshots))
	for i, snapshotJSON := range sjr.Snapshots {
		snapshots[i] = *snapshotJSON.ToSnapshotInfo()
	}

	return &SnapshotsResponse{
		Count:     sjr.Count,
		Snapshots: snapshots,
	}
}

// ISOSpaceInfo информация о месте на SD-карте (btrfs раздел iso/data/backup)
type ISOSpaceInfo struct {
	TotalSpace     int64   `json:"total_space"`
	UsedSpace      int64   `json:"used_space"`
	AvailableSpace int64   `json:"available_space"`
	TotalSpaceGB   string  `json:"total_space_gb"`
	UsedSpaceGB    string  `json:"used_space_gb"`
	AvailableGB    string  `json:"available_gb"`
	UsedPercent    float64 `json:"used_percent"`
	ISODirectory   string  `json:"iso_directory"`
}
