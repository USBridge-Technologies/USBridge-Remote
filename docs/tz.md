# Техническое задание: Нативный фронтенд на Go (Fyne)

## Выбранный стек

### Backend + UI (Go)
- **Fyne** - кросс-платформенный GUI фреймворк
- **Gin** - HTTP API сервер (для проксирования к USB Bridge 2)
- **WebSocket** - real-time обновления
- **Viper** - конфигурация
- **Logrus** - логирование
- **NBD Protocol** - реализация NBD сервера
- **File System** - работа с дисками и образами

### Видео и медиа
- **Fyne Canvas** - для отображения видео
- **WebRTC** - получение видео+аудио потока
- **Pion WebRTC** - Go WebRTC библиотека
- **FFmpeg bindings** - обработка видео потоков
- **OpenGL** - аппаратное ускорение (через Fyne)

## Архитектура приложения

```
┌───────────────────────────────────────────────────────────────────────┐
│                    Fyne App (Гибридное приложение)                    │
│                                                                       │
│  ┌─────────────────┐    ┌─────────────────┐    ┌──────────────┐       │
│  │   UI Клиент     │    │   NBD Сервер    │    │   WebRTC     │       │
│  │                 │    │                 │    │   Клиент     │       │
│  │ - Video Widget  │◄───┤ - Disk Manager  │    │              │       │
│  │ - Keyboard UI   │    │ - Export List   │    │ - MediaMTX   │       │
│  │ - Settings      │    │ - Client Mgmt   │    │ - Pion WebRTC│       │
│  │                 │    │ - Stats         │    │              │       │
│  └─────────────────┘    └─────────────────┘    └──────────────┘       │
│           │                       │                       │           │
│           ▼                       ▼                       ▼           │
│  ┌─────────────────────────────────────────────────────────────────┐  │
│  │                    USB Bridge 2 (Целевое устройство)            │  │
│  │                                                                 │  │
│  │  ┌─────────────┐    ┌─────────────┐    ┌─────────────────────┐  │  │
│  │  │ NBD Клиент  │    │ HTTP API    │    │   MediaMTX Server   │  │  │
│  │  │             │    │             │    │                     │  │  │
│  │  │ - Connect   │◄───┤ - Service   │    │ - WebRTC :8889      │  │  │
│  │  │ - Read ISO  │    │ - Keyboard  │    │ - RTSP :8554        │  │  │
│  │  │ - Mount     │    │ - Video API │    │ - HLS :8888         │  │  │
│  │  └─────────────┘    └─────────────┘    └─────────────────────┘  │  │
│  │                                    │                            │  │
│  │                                    ▼                            │  │
│  │  ┌───────────────────────────────────────────────────────────┐  │  │
│  │  │                    FFmpeg Video Capture                   │  │  │
│  │  │                                                           │  │  │
│  │  │ - /dev/video0 (UVC Camera)                                │  │  │
│  │  │ - H.264 Encoding                                          │  │  │
│  │  │ - RTSP Stream to MediaMTX                                 │  │  │
│  │  └───────────────────────────────────────────────────────────┘  │  │
│  └─────────────────────────────────────────────────────────────────┘  │
└───────────────────────────────────────────────────────────────────────┘
```

### Роль гибридного приложения
Приложение одновременно является:

**🎮 Клиентом USB Bridge 2:**
- **Получает видео+аудио** через WebRTC от MediaMTX на USB Bridge 2
- **Отправляет команды клавиатуры** через HTTP API на USB Bridge 2
- **Управляет USB Bridge 2** (через HTTP API старт/стоп сервиса, видео стриминг)

**💾 NBD сервером:**
- **Раздает ISO образы** через NBD протокол
- **USB Bridge 2 подключается** к нашему NBD серверу как клиент
- **Управляет дисками** и экспортами через UI
- **Предоставляет UI** для выбора и стриминга дисков

### Последовательность работы
1. **Пользователь выбирает ISO** в UI приложения
2. **Приложение запускает NBD сервер** с выбранным ISO
3. **Приложение отправляет команды** на USB Bridge 2:
   - `POST /api/service/start` - запуск сервиса (подключение NBD клиента)
   - `POST /api/video/start` - запуск видео стриминга (FFmpeg + MediaMTX)
4. **USB Bridge 2 подключается** к нашему NBD серверу
5. **Приложение подключается** к WebRTC потоку через MediaMTX (`http://USB_BRIDGE_IP:8889/stream/`)
6. **Получаем видео+аудио** и можем управлять клавиатурой

## Структура проекта

```
usbridge-client/
├── cmd/
│   └── main.go                    # Точка входа
├── internal/
│   ├── ui/                        # Presentation Layer
│   │   ├── main_window.go         # Главное окно
│   │   ├── video_widget.go        # Видео виджет
│   │   ├── keyboard_widget.go     # Клавиатура
│   │   └── disk_widget.go         # Управление дисками
│   ├── service/                   # Business Logic Layer
│   │   ├── nbd_service.go         # NBD сервер + ISO управление
│   │   ├── webrtc_service.go      # WebRTC клиент + видео/аудио
│   │   └── usb_service.go         # USB Bridge 2 клиент
│   ├── api/                       # API Layer
│   │   ├── usb_client.go          # HTTP клиент для USB Bridge 2
│   │   ├── webrtc_client.go       # WebRTC клиент
│   │   └── nbd_client_manager.go  # Управление NBD клиентами
│   └── models/                    # Модели данных
│       ├── disk.go                # DiskInfo, Export
│       ├── config.go              # Config
│       └── usb.go                 # USB Bridge 2 модели
├── go.mod
└── README.md
```

### Описание архитектуры

**Presentation Layer (ui/)**
- **main_window.go** - главное окно приложения
- **video_widget.go** - отображение видео потока
- **keyboard_widget.go** - виртуальная клавиатура
- **disk_widget.go** - управление дисками и ISO образами

**Business Logic Layer (service/)**
- **nbd_service.go** - NBD сервер + управление ISO экспортами
- **webrtc_service.go** - WebRTC клиент + обработка видео/аудио
- **usb_service.go** - координация работы с USB Bridge 2

**API Layer (api/)**
- **usb_client.go** - HTTP клиент для USB Bridge 2 API
- **webrtc_client.go** - WebRTC клиент для подключения к потоку
- **nbd_client_manager.go** - управление NBD клиентами (подключениями)

**Models (models/)**
- **disk.go** - структуры для дисков и экспортов
- **config.go** - конфигурация приложения
- **usb.go** - модели для USB Bridge 2 API

## Go модули и зависимости
# Просто установи нужные модули вручную:

go get fyne.io/fyne/v2
go get github.com/gin-gonic/gin
go get github.com/gorilla/websocket
go get github.com/spf13/viper
go get github.com/sirupsen/logrus
go get github.com/google/uuid
go get github.com/fsnotify/fsnotify
go get github.com/mitchellh/mapstructure
go get github.com/pelletier/go-toml/v2
go get github.com/spf13/afero
go get github.com/spf13/cast
go get github.com/spf13/jwalterweatherman
go get github.com/spf13/pflag

# MediaMTX WebRTC клиент
go get github.com/pion/webrtc/v3
go get github.com/pion/webrtc/v3/pkg/media
go get github.com/subosito/gotenv
go get github.com/pion/webrtc/v3
go get github.com/pion/ice/v2
go get github.com/pion/stun
go get github.com/pion/turn/v2
go get github.com/pion/dtls/v2
go get github.com/pion/srtp/v2
go get github.com/pion/rtcp
go get github.com/pion/rtp
go get golang.org/x/sys
go get golang.org/x/text
go get gopkg.in/ini.v1
go get gopkg.in/yaml.v3

# Без зависимостей и версий, просто ссылки на нужные модули.

## Основные компоненты Fyne

### 1. Главное окно
```go
type MainWindow struct {
    app    fyne.App
    window fyne.Window
    
    // Виджеты
    videoWidget    *VideoWidget
    keyboardWidget *KeyboardWidget
    diskWidget     *DiskWidget
    
    // Состояние
    isConnected bool
    isStreaming bool
}
```

### 2. Видео виджет
```go
type VideoWidget struct {
    container *fyne.Container
    canvas    *canvas.Image
    controls  *fyne.Container
    
    // Видео поток
    streamURL string
    isPlaying bool
}
```

### 3. Виртуальная клавиатура
```go
type KeyboardWidget struct {
    container *fyne.Container
    buttons   map[string]*widget.Button
    
    // Состояние
    modifiers uint8
    history   []string
}
```

### 4. Диск виджет
```go
type DiskWidget struct {
    container     *fyne.Container
    diskList      *widget.List
    exportList    *widget.List
    addDiskBtn    *widget.Button
    removeDiskBtn *widget.Button
    connectBtn    *widget.Button
    
    // Состояние
    disks         []DiskInfo
    exports       []DiskExport
    serverRunning bool
}

type DiskInfo struct {
    Name        string
    Path        string
    Size        int64
    Type        string // iso, img, device
    Description string
}
```

## Преимущества Fyne

### ✅ Кросс-платформенность
- **Linux** (GTK, Wayland)
- **Windows** (Win32)
- **macOS** (Cocoa)
- **Android** (через Go Mobile)
- **iOS** (через Go Mobile)

### ✅ Нативный вид
- Использует системные виджеты
- Автоматическая поддержка тем
- Нативные шрифты и иконки
- Системные диалоги

### ✅ Простота разработки
- Чистый Go код без JS/HTML/CSS
- Интуитивный API
- Встроенные виджеты
- Material Design из коробки

### ✅ Производительность
- OpenGL рендеринг
- Аппаратное ускорение
- Минимальное потребление памяти
- Быстрый запуск

## Функциональность

### Видео плеер
- **Canvas.Image** для отображения кадров
- **WebRTC** для получения видео потока
- **FFmpeg** для декодирования H.264
- **OpenGL** для аппаратного ускорения
- Полноэкранный режим
- Контроль качества

### Виртуальная клавиатура
- **widget.Button** для клавиш
- Поддержка модификаторов (Ctrl, Alt, Shift)
- Быстрые команды (Ctrl+Alt+Del)
- История команд
- Макросы

### Управление дисками
- **Сканирование** доступных ISO образов
- **Создание экспортов** из файлов
- **Управление NBD сервером** и клиентами
- **Мониторинг** активности чтения/записи
- **Статистика** использования

### Поддерживаемые форматы
- **ISO образы** (.iso файлы)
- **Физические диски** (/dev/sdX, /dev/nvmeX)
- **Образы дисков** (.img, .vmdk, .vdi)

### NBD протокол
- **Handshake** с клиентами
- **Экспорт списка** доступных дисков
- **Обработка запросов** чтения/записи
- **Управление сессиями** и разрывом соединений

## WebRTC функциональность

### Видео поток
- **Pion WebRTC** для подключения к USB Bridge 2
- **H.264 декодирование** видео потока
- **Canvas.Image** для отображения кадров
- **Real-time** обработка видео

### Signaling
- **WebSocket** для обмена SDP/ICE
- **STUN/TURN** серверы для NAT traversal
- **Автоматическое переподключение**
- **Статус соединения** мониторинг

## Пример кода

### Основное окно
```go
func NewMainWindow(app fyne.App) *MainWindow {
    window := app.NewWindow("USB Bridge Client")
    window.Resize(fyne.NewSize(1200, 800))
    
    mw := &MainWindow{
        app:    app,
        window: window,
    }
    
    // Создаем виджеты
    mw.videoWidget = NewVideoWidget()
    mw.keyboardWidget = NewKeyboardWidget()
    mw.diskWidget = NewDiskWidget()
    
    // Размещаем в layout
    content := container.NewBorder(
        mw.createToolbar(),    // Верх
        mw.createStatusBar(),   // Низ
        mw.createSidebar(),     // Лево
        nil,                    // Право
        mw.createMainContent(), // Центр
    )
    
    window.SetContent(content)
    return mw
}
```

### Видео виджет
```go
func NewVideoWidget() *VideoWidget {
    canvas := canvas.NewImageFromResource(nil)
    canvas.FillMode = canvas.ImageFillContain
    
    playBtn := widget.NewButton("▶️", nil)
    stopBtn := widget.NewButton("⏹️", nil)
    
    controls := container.NewHBox(playBtn, stopBtn)
    
    return &VideoWidget{
        container: container.NewBorder(nil, controls, nil, nil, canvas),
        canvas:    canvas,
        controls:  controls,
    }
}
```

### NBD сервер
```go
type NBDServer struct {
    config     *NBDServerConfig
    exports    map[string]*DiskExport
    listener   net.Listener
    isRunning  bool
    mutex      sync.RWMutex
}

func (ns *NBDServer) Start(port int) error {
    listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
    if err != nil {
        return err
    }
    
    ns.listener = listener
    ns.isRunning = true
    
    go ns.acceptConnections()
    
    log.Printf("🚀 NBD сервер запущен на порту %d", port)
    return nil
}
```

## Сборка и развертывание

### Сборка для разных платформ
```bash
# Linux
fyne package -os linux -icon assets/icon.png

# Windows  
fyne package -os windows -icon assets/icon.png

# macOS
fyne package -os darwin -icon assets/icon.png

# Android
fyne package -os android -icon assets/icon.png
```

### Docker (для Linux)
```dockerfile
FROM golang:1.21-alpine AS builder
RUN apk add --no-cache gcc musl-dev
WORKDIR /app
COPY . .
RUN go mod download
RUN go build -o usbridge2-frontend cmd/main.go

FROM alpine:latest
RUN apk add --no-cache gtk+3.0-dev
COPY --from=builder /app/usbridge2-frontend /usr/local/bin/
CMD ["usbridge2-frontend"]
```

## API клиент для USB Bridge 2

### HTTP клиент для управления USB Bridge 2
```go
type USBClient struct {
    baseURL    string
    httpClient *http.Client
    apiKey     string
}

// Системные запросы
func (c *USBClient) GetStatus() (*StatusResponse, error)
func (c *USBClient) StartService() error
func (c *USBClient) StopService() error
func (c *USBClient) RestartService() error

// Управление устройствами
func (c *USBClient) GetDevices() ([]Device, error)
func (c *USBClient) GetConfig() (*Config, error)
func (c *USBClient) UpdateConfig(config *Config) error

// Управление клавиатурой
func (c *USBClient) SendKey(keyCode int) error
func (c *USBClient) SendCombo(modifiers int, keyCode int) error
func (c *USBClient) SendText(text string) error

// Видео управление
func (c *USBClient) GetVideoInfo() (*VideoInfo, error)
func (c *USBClient) StartVideo() error
func (c *USBClient) StopVideo() error
```

### WebRTC клиент для MediaMTX
```go
type WebRTCClient struct {
    peerConnection *webrtc.PeerConnection
    videoTrack     *webrtc.TrackRemote
    audioTrack     *webrtc.TrackRemote
    mediaMTXURL    string  // "http://192.168.1.109:8889/stream/"
    rtspURL        string  // "rtsp://192.168.1.109:8554/stream"
    isConnected    bool
}

// Подключение к WebRTC потоку через MediaMTX
func (c *WebRTCClient) ConnectToMediaMTX(usbHost string, port int) error
func (c *WebRTCClient) ConnectToRTSP(usbHost string, port int) error
func (c *WebRTCClient) Disconnect() error
func (c *WebRTCClient) GetConnectionState() webrtc.ICEConnectionState
```

## Конфигурация гибридного приложения

```go
type AppConfig struct {
    // USB Bridge 2 подключение (как клиент)
    USBHost        string `json:"usb_host"`        // IP адрес USB Bridge 2
    USBPort        int    `json:"usb_port"`        // Порт USB Bridge 2 (8080)
    APITimeout     int    `json:"api_timeout"`    // Таймаут API запросов
    
    // NBD сервер (как сервер)
    NBDPort        int    `json:"nbd_port"`        // Порт NBD сервера (10809)
    MaxClients     int    `json:"max_clients"`    // Максимум NBD клиентов
    ScanPaths      []string `json:"scan_paths"`  // Пути для сканирования дисков
    SupportedTypes []string `json:"supported_types"` // Поддерживаемые типы файлов
    
    // WebRTC настройки (через MediaMTX)
    MediaMTXHost     string `json:"mediamtx_host"`    // IP MediaMTX (обычно тот же USBHost)
    MediaMTXWebRTC   int    `json:"mediamtx_webrtc"` // Порт WebRTC (8889)
    MediaMTXRTSP     int    `json:"mediamtx_rtsp"`   // Порт RTSP (8554)
    MediaMTXHLS      int    `json:"mediamtx_hls"`    // Порт HLS (8888)
    STUNServers      []string `json:"stun_servers"`  // STUN серверы
    TURNServers      []string `json:"turn_servers"`  // TURN серверы
    
    // Видео настройки
    VideoCodec     string `json:"video_codec"`    // H.264, VP8, VP9
    VideoBitrate   int    `json:"video_bitrate"`  // kbps
    VideoWidth     int    `json:"video_width"`
    VideoHeight    int    `json:"video_height"`
    VideoFPS       int    `json:"video_fps"`
    
    // Аудио настройки
    AudioCodec     string `json:"audio_codec"`    // Opus, G.711
    AudioBitrate   int    `json:"audio_bitrate"`  // kbps
    AudioSampleRate int   `json:"audio_sample_rate"`
    AudioChannels  int    `json:"audio_channels"`
    
    // UI настройки
    WindowWidth    int    `json:"window_width"`
    WindowHeight   int    `json:"window_height"`
    Theme          string `json:"theme"`          // light, dark
    LogLevel       string `json:"log_level"`
}
```

## Интеграция с USB Bridge 2

### Правильная последовательность подключения
```go
func (app *MainApp) ConnectToUSB(isoPath string) error {
    // 1. Запускаем NBD сервер с выбранным ISO
    err := app.startNBDServerWithISO(isoPath)
    if err != nil {
        return fmt.Errorf("ошибка запуска NBD сервера: %v", err)
    }
    
    // 2. Отправляем API запрос на запуск сервиса USB Bridge 2
    err = app.usbClient.StartService()
    if err != nil {
        return fmt.Errorf("ошибка запуска сервиса USB Bridge 2: %v", err)
    }
    log.Println("✅ Сервис USB Bridge 2 запущен")
    
    // 3. Запускаем видео стриминг на USB Bridge 2
    err = app.usbClient.StartVideo()
    if err != nil {
        return fmt.Errorf("ошибка запуска видео стриминга: %v", err)
    }
    log.Println("✅ Видео стриминг запущен на USB Bridge 2")
    
    // 4. Подключаемся к WebRTC потоку через MediaMTX
    err = app.webrtcClient.ConnectToMediaMTX(app.config.MediaMTXHost, app.config.MediaMTXWebRTC)
    if err != nil {
        return fmt.Errorf("ошибка подключения WebRTC через MediaMTX: %v", err)
    }
    log.Println("✅ WebRTC подключение через MediaMTX установлено")
    
    return nil
}

func (app *MainApp) startNBDServerWithISO(isoPath string) error {
    // Запускаем NBD сервер
    err := app.nbdServer.Start(app.config.NBDPort)
    if err != nil {
        return err
    }
    
    // Добавляем ISO в экспорты
    export := &DiskExport{
        Name:        filepath.Base(isoPath),
        FilePath:    isoPath,
        Size:        getFileSize(isoPath),
        ReadOnly:    true,
        Description: "ISO образ для USB Bridge 2",
        IsActive:    true,
    }
    
    err = app.nbdServer.AddExport(export)
    if err != nil {
        return err
    }
    
    log.Printf("🚀 NBD сервер запущен с ISO: %s", export.Name)
    log.Printf("📡 USB Bridge 2 должен подключиться к порту: %d", app.config.NBDPort)
    
    return nil
}
```

### Workflow подключения
1. **Выбираем ISO образ** в UI
2. **Запускаем NBD сервер** с этим ISO
3. **Отправляем POST /api/service/start** на USB Bridge 2
4. **Отправляем POST /api/video/start** на USB Bridge 2  
5. **Подключаемся к WebRTC** потоку от USB Bridge 2
6. **Получаем видео+аудио** и можем управлять клавиатурой

### Настройка USB Bridge 2
USB Bridge 2 должен быть настроен на подключение к нашему NBD серверу:
```json
{
  "nbd_server": "IP_АДРЕС_НАШЕГО_ПРИЛОЖЕНИЯ",
  "nbd_port": 10809,
  "export_name": "имя_iso_образа"
}
```

### API запросы к USB Bridge 2
```go
// Запуск сервиса (подключает NBD клиент к нашему серверу)
POST http://USB_BRIDGE_IP:8080/api/service/start

// Запуск видео стриминга (включает FFmpeg + MediaMTX)
POST http://USB_BRIDGE_IP:8080/api/video/start

// Получение информации о видео (включает MediaMTX URLs)
GET http://USB_BRIDGE_IP:8080/api/video/info

// Получение статуса
GET http://USB_BRIDGE_IP:8080/api/status

// Отправка команд клавиатуры
POST http://USB_BRIDGE_IP:8080/api/keyboard
{
  "action": "key",
  "key_code": 40
}
```

### MediaMTX WebRTC подключение
```go
// WebRTC интерфейс MediaMTX
http://USB_BRIDGE_IP:8889/stream/

// RTSP поток MediaMTX  
rtsp://USB_BRIDGE_IP:8554/stream

// HLS поток MediaMTX
http://USB_BRIDGE_IP:8888/stream/index.m3u8

// Подключение к WebRTC через Pion WebRTC
func (c *WebRTCClient) ConnectToMediaMTX(host string, port int) error {
    c.mediaMTXURL = fmt.Sprintf("http://%s:%d/stream/", host, port)
    c.rtspURL = fmt.Sprintf("rtsp://%s:8554/stream", host)
    
    // Используем Pion WebRTC для подключения к MediaMTX
    return c.establishWebRTCConnection()
}
```

### Управление клавиатурой
```go
func (kw *KeyboardWidget) SendKey(keyCode int) error {
    return kw.usbClient.SendKey(keyCode)
}

func (kw *KeyboardWidget) SendCombo(modifiers int, keyCode int) error {
    return kw.usbClient.SendCombo(modifiers, keyCode)
}

func (kw *KeyboardWidget) SendText(text string) error {
    return kw.usbClient.SendText(text)
}
```

### Управление дисками (NBD сервер)
```go
func (dmw *DiskManagerWidget) AddDiskExport(diskPath string) error {
    // Добавляем диск в экспорты NBD сервера
    export := &DiskExport{
        Name:        filepath.Base(diskPath),
        FilePath:    diskPath,
        Size:        getFileSize(diskPath),
        ReadOnly:    true,
        Description: "ISO образ",
        IsActive:    true,
    }
    
    err := dmw.nbdServer.AddExport(export)
    if err != nil {
        return err
    }
    
    log.Printf("✅ Диск %s добавлен в экспорты NBD", export.Name)
    return nil
}
```

## Преимущества этого подхода

🎯 **Чистый Go** - никакого JavaScript/HTML/CSS
🚀 **Нативная производительность** - использует системные виджеты  
🌍 **Кросс-платформенность** - один код для всех ОС
🎨 **Современный UI** - Material Design из коробки
📱 **Мобильная поддержка** - Android/iOS через Go Mobile
⚡ **Простая разработка** - интуитивный API Fyne
🔄 **Гибридная архитектура** - клиент USB Bridge 2 + NBD сервер
💾 **NBD сервер** - раздача ISO образов через NBD протокол
📹 **WebRTC клиент** - получение видео+аудио от USB Bridge 2
🎮 **Управление клавиатурой** - отправка команд на USB Bridge 2
🔒 **Безопасность** - аутентификация и шифрование
📊 **Мониторинг** - real-time статистика и логи
🎵 **Аудио управление** - volume control и mute функциональность
🌐 **Real-time** - низкая задержка видео/аудио потока

Этот подход даст вам **гибридное приложение** на Go, которое одновременно:
- **Получает видео+аудио** от USB Bridge 2 через WebRTC
- **Отправляет команды клавиатуры** на USB Bridge 2
- **Раздает ISO образы** через NBD сервер для USB Bridge 2

Одно приложение - полный контроль! 🎉