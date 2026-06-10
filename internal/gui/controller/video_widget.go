package controller

import (
	"fmt"
	"image"
	"sync"
	"sync/atomic"
	"time"

	"usbridge-client/internal/api"
	"usbridge-client/internal/gui/graphics"
	"usbridge-client/internal/gui/view"
	"usbridge-client/internal/media"
	"usbridge-client/internal/service"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/widget"
	"github.com/sirupsen/logrus"
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
	enableVSync          bool // mirrors VideoStartRequest.EnableVSync for the GL overlay

	// Сервисы
	usbClient           *api.USBClient
	videoClient    service.VideoClient
	frpService              *service.FRPService // для проверки режима FRP
	tailscaleService        *service.TailscaleService
	tailscaleVideoEnabled   bool   // false when connected via direct/LAN (disables Tailscale UDP routing for video)
	bridgeInternalHost      string // LAN/internal IP of bridge; used to detect same-subnet direct path
	updateStatus        func()
	onFPSChanged        func(float64)
	videoOpMu           sync.Mutex
	videoOpRunning      bool
	desiredStreaming    bool
	videoRestartPending bool
	inputQueue          chan inputCommand
	moveQueueMu         sync.Mutex
	bottomInset         float32 // Отступ снизу (например, для клавиатуры), который выталкивает видео вверх

	pendingMoveX          int
	pendingMoveY          int
	moveWorkerStarted     bool
	videoOps              chan videoOperation
	videoReconcilePending atomic.Bool

	// Видео поток
	currentFrame         image.Image
	pendingFrame         atomic.Pointer[image.RGBA] // latest decoded frame, not yet displayed
	frameMutex           sync.RWMutex
	lastFrameTime        time.Time
	frameCount           int64
	frameDecoder         *media.FrameDecoder
	frameRenderScheduled atomic.Bool
	lastUIFrameRenderAt  atomic.Int64
	forceCanvasRefresh   atomic.Bool
	renderTickerStop     chan struct{}
	videoTraceSeq        atomic.Uint64
	videoTraceID         atomic.Uint64
	videoTraceStartedAt  atomic.Int64
	videoTraceFirstFrame atomic.Int64
	videoTraceFirstPaint atomic.Int64
	videoTraceLabel      atomic.Value
	fpsWindowStart       atomic.Int64 // for Go-level frame arrival FPS logging
	metalFPSWarned       atomic.Bool  // gates the one-shot Metal FPS mismatch warning
	onNativeReady        func()       // one-shot: called on main thread when native overlay (Metal/GL) is first created
	lastVideoImgW        float32      // pixel width of the last decoded video frame (for resize recalc when frame=nil)
	lastVideoImgH        float32      // pixel height of the last decoded video frame
	frameContentX        float32 // нормализованная активная область кадра по X без black bars
	frameContentY        float32 // нормализованная активная область кадра по Y без black bars
	frameContentW        float32 // нормализованная ширина активной области кадра
	frameContentH        float32 // нормализованная высота активной области кадра

	// Диалоги
	fullscreenDialog      *FullscreenDialog
	startDialog           *view.VideoStartDialog
	parentWindow          fyne.Window
	virtualKeyboard       *graphics.VirtualKeyboard
	desktopKeyCapture     *desktopKeyboardCapture
	keyboardModifierState atomic.Int32
	suppressRuneUntilNS   atomic.Int64

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
	mouseInputMode     string    // desired mode: "mouse" (touchpad), "touchscreen" или "absolute"
	observedMouseMode  string    // фактический transport, который сообщил сервер через /api/device/info
	showMouseCursor    bool      // показывать курсор мыши в захваченном видео
	agentOS            string
	agentDisplay       string
	cursorOverlayX     float32
	cursorOverlayY     float32
	cursorOverlayShown bool
	lastMouseModeDiag  string  // последняя диагностическая строка режима мыши, чтобы не спамить лог
	touchpadSizeW      float32 // Ширина области ввода (для перевода в абсолютные координаты)
	touchpadSizeH      float32 // Высота области ввода
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
	scrollDragAxis    string
	scrollDragLastX   float32
	scrollDragLastY   float32
	lastTouchX        int // последние отправленные координаты touch (чтобы не дублировать в MouseMoved)
	lastTouchY        int
	lastAbsX          int // последние отправленные координаты absolute (touch_position) чтобы не спамить
	lastAbsY          int
	lastAbsSentTime   time.Time // время последней отправки absolute (для дебаунса)
	absSendMu         sync.Mutex
	absButtons        uint8     // битмаска кнопок для absolute режима
	// Stats for periodic log (atomics — written from capture goroutine, read from log timer).
	statAbsMoonlight  atomic.Int64 // absolute events sent via Moonlight LiSendMousePositionEvent
	statAbsWS         atomic.Int64 // absolute events sent via WebSocket
	statRelMoonlight  atomic.Int64 // relative events sent via Moonlight SendMoonlightMouseMove
	statRelWS         atomic.Int64 // relative events sent via WebSocket SendMouseMove
	lastTouchDownTime time.Time // время последнего SendTouch(_, _, true) — для дедупликации
	touchDedupMu      sync.Mutex
	// Задержка touch(down) при MouseDown: если за ~120ms не пришёл Tapped — считаем драг, шлём touch(true).
	// Tapped приходит при полном клике на виджет; MouseUp в Fyne приходит только виджету под курсором при отпускании.
	touchDownDelayTimer *time.Timer
	touchDownDelayMu    sync.Mutex
	touchActive         bool // touch(true) уже отправлен и ещё не отправлен touch(false); MouseMoved шлёт только при true
	isClosing           atomic.Bool
}

func (vw *VideoWidget) Close() {
	vw.isClosing.Store(true)
}

type inputCommand struct {
	dropIfBusy bool
	name       string
	run        func(*api.USBClient) error
}

func (vw *VideoWidget) setDesiredStreaming(streaming bool) {
	vw.videoOpMu.Lock()
	vw.desiredStreaming = streaming
	if !streaming {
		vw.videoRestartPending = false
	}
	vw.videoOpMu.Unlock()
}

func (vw *VideoWidget) ReconcileDesiredStreaming() {
	vw.scheduleVideoReconcile("reconcile-desired-streaming")
}

func (vw *VideoWidget) desiredStreamingState() bool {
	vw.videoOpMu.Lock()
	defer vw.videoOpMu.Unlock()
	return vw.desiredStreaming
}

func (vw *VideoWidget) RecoverAfterControlDeviceRebuildAsync() {
	go func() {
		vw.handleDeviceRebuildLocally()
		if vw.usbClient != nil {
			if err := vw.usbClient.StopVideo(); err != nil {
				logrus.Warnf("⚠️ Control rebuild restart: failed to stop server video before reconcile: %v", err)
			} else {
				logrus.Info("🛑 Control rebuild restart: server video stopped before reconcile")
			}
		}
		vw.videoOpMu.Lock()
		vw.videoRestartPending = true
		vw.videoOpMu.Unlock()
		time.Sleep(250 * time.Millisecond)
		vw.scheduleVideoReconcile("control-device-rebuild")
	}()
}

func (vw *VideoWidget) beginVideoTrace(reason string) uint64 {
	traceID := vw.videoTraceSeq.Add(1)
	startedAt := time.Now()
	vw.videoTraceID.Store(traceID)
	vw.videoTraceStartedAt.Store(startedAt.UnixNano())
	vw.videoTraceFirstFrame.Store(0)
	vw.videoTraceFirstPaint.Store(0)
	// Reset saved video dimensions so a new stream with different resolution
	// doesn't inherit stale values from the previous session.
	vw.lastVideoImgW = 0
	vw.lastVideoImgH = 0
	label := fmt.Sprintf("vt-%d", traceID)
	vw.videoTraceLabel.Store(label)
	logrus.Infof("🎯 [VideoTrace #%d] start label=%s reason=%s", traceID, label, reason)

	time.AfterFunc(4*time.Second, func() {
		if vw.videoTraceID.Load() != traceID {
			return
		}
		startNs := vw.videoTraceStartedAt.Load()
		if startNs == 0 {
			return
		}
		start := time.Unix(0, startNs)
		firstFrameNs := vw.videoTraceFirstFrame.Load()
		firstPaintNs := vw.videoTraceFirstPaint.Load()

		switch {
		case firstFrameNs == 0:
			logrus.Warnf("⚠️ [VideoTrace #%d] no frames reached client after %s video_stats=%v relay=%s", traceID, time.Since(start).Round(time.Millisecond), vw.safeGStreamerStats(), vw.safeRelayDebugInfo())
		case firstPaintNs == 0:
			logrus.Warnf("⚠️ [VideoTrace #%d] client receives frames but UI has not painted after %s", traceID, time.Since(start).Round(time.Millisecond))
		default:
			logrus.Infof("✅ [VideoTrace #%d] startup path complete frame=%s paint=%s", traceID, time.Unix(0, firstFrameNs).Sub(start).Round(time.Millisecond), time.Unix(0, firstPaintNs).Sub(start).Round(time.Millisecond))
		}
	})

	return traceID
}

func (vw *VideoWidget) currentVideoTraceLabel() string {
	if value, ok := vw.videoTraceLabel.Load().(string); ok {
		return value
	}
	return ""
}

func (vw *VideoWidget) safeGStreamerStats() map[string]interface{} {
	if vw.videoClient == nil {
		return nil
	}
	return vw.videoClient.GetStats()
}

func (vw *VideoWidget) safeRelayDebugInfo() string {
	if vw.tailscaleService == nil {
		return "tailscale=disabled"
	}
	return vw.tailscaleService.VideoRelayDebugInfo("")
}

func (vw *VideoWidget) noteVideoTraceFirstFrame(frameNum int64) {
	now := time.Now().UnixNano()
	if !vw.videoTraceFirstFrame.CompareAndSwap(0, now) {
		return
	}
	traceID := vw.videoTraceID.Load()
	startNs := vw.videoTraceStartedAt.Load()
	if startNs == 0 {
		logrus.Infof("🧩 [VideoTrace #%d] first client frame received frame=%d", traceID, frameNum)
		return
	}
	logrus.Infof("🧩 [VideoTrace #%d] first client frame received frame=%d after %s", traceID, frameNum, time.Unix(0, now).Sub(time.Unix(0, startNs)).Round(time.Millisecond))
}

func (vw *VideoWidget) noteVideoTraceFirstPaint(frameNum int64) {
	now := time.Now().UnixNano()
	if !vw.videoTraceFirstPaint.CompareAndSwap(0, now) {
		return
	}
	traceID := vw.videoTraceID.Load()
	startNs := vw.videoTraceStartedAt.Load()
	if startNs == 0 {
		logrus.Infof("🖼️ [VideoTrace #%d] first UI paint frame=%d", traceID, frameNum)
		return
	}
	logrus.Infof("🖼️ [VideoTrace #%d] first UI paint frame=%d after %s", traceID, frameNum, time.Unix(0, now).Sub(time.Unix(0, startNs)).Round(time.Millisecond))
}

type videoOperation struct {
	name string
	fn   func()
	done chan struct{}
}

// SetBottomInset устанавливает отступ снизу и пересчитывает вьюпорт.
func (vw *VideoWidget) SetBottomInset(inset float32) {
	logrus.Infof("📐 SetBottomInset: %.1f", inset)
	vw.bottomInset = inset
	vw.recalculateViewport()
}
