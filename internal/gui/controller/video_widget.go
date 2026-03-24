package controller

import (
	"image"
	"sync"
	"time"

	"usbridge-client/internal/api"
	"usbridge-client/internal/gui/graphics"
	"usbridge-client/internal/gui/view"
	"usbridge-client/internal/media"
	"usbridge-client/internal/service"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/widget"
)

// VideoWidget виджет управления видео захватом
type VideoWidget struct {
	container        *fyne.Container
	videoCanvas      *canvas.Image
	touchpadWrapper  *TouchpadWrapper
	statusLabel      *widget.Label
	infoLabel        *widget.Label
	statsLabel       *widget.Label
	contentContainer *fyne.Container // Контейнер для видео и клавиатуры
	ui               *view.VideoWidgetUI
	statsTickerStop  chan struct{}

	// Состояние
	isStreaming          bool
	streamURL            string
	isGStreamerConnected bool
	isMouseConnected     bool // Флаг подключенной мыши

	// Сервисы
	usbClient        *api.USBClient
	gstreamerService *service.GStreamerService
	frpService       *service.FRPService // для проверки режима FRP
	updateStatus     func()
	onFPSChanged     func(float64)
	videoOpMu        sync.Mutex
	videoOpRunning   bool

	// Видео поток
	currentFrame  image.Image
	frameMutex    sync.RWMutex
	lastFrameTime time.Time
	frameCount    int64
	frameDecoder  *media.FrameDecoder

	// Диалоги
	fullscreenDialog *FullscreenDialog
	startDialog      *view.VideoStartDialog
	parentWindow     fyne.Window
	virtualKeyboard  *graphics.VirtualKeyboard

	// Мышь/тачпад
	lastMouseX         float32
	lastMouseY         float32
	currentMouseX      float32 // Текущая позиция мыши (для polling)
	currentMouseY      float32 // Текущая позиция мыши (для polling)
	relativeRemainderX float32 // накопленный дробный остаток относительного перемещения по X
	relativeRemainderY float32 // накопленный дробный остаток относительного перемещения по Y
	isDragging         bool
	dragButton         int
	touchStartX        float32
	touchStartY        float32
	touchStartTime     time.Time
	mousePollingQuit   chan bool // Канал для остановки polling горутины
	mouseInputMode     string    // "mouse" (по умолчанию), "touchscreen" или "absolute"
	touchpadSizeW      float32   // Ширина области ввода (для перевода в абсолютные координаты)
	touchpadSizeH      float32   // Высота области ввода
	// Прямоугольник видео внутри области ввода (ImageFillContain): для корректного перевода координат в 0..4095
	contentRectX      float32
	contentRectY      float32
	contentRectW      float32
	contentRectH      float32
	baseContentRectW  float32
	baseContentRectH  float32
	zoomScale         float32
	panOffsetX        float32
	panOffsetY        float32
	multiTouchActive  bool
	lastMultiTouchAt  time.Time
	lastTouchX        int // последние отправленные координаты touch (чтобы не дублировать в MouseMoved)
	lastTouchY        int
	lastAbsX          int // последние отправленные координаты absolute (touch_position) чтобы не спамить
	lastAbsY          int
	lastAbsSentTime   time.Time // время последней отправки absolute (для дебаунса)
	absButtons        uint8     // битмаска кнопок для absolute режима
	lastTouchDownTime time.Time // время последнего SendTouch(_, _, true) — для дедупликации
	touchDedupMu      sync.Mutex
	// Задержка touch(down) при MouseDown: если за ~120ms не пришёл Tapped — считаем драг, шлём touch(true).
	// Tapped приходит при полном клике на виджет; MouseUp в Fyne приходит только виджету под курсором при отпускании.
	touchDownDelayTimer *time.Timer
	touchDownDelayMu    sync.Mutex
	touchActive         bool // touch(true) уже отправлен и ещё не отправлен touch(false); MouseMoved шлёт только при true
}
