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
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
	"github.com/sirupsen/logrus"
)

// VideoWidget is the widget that controls video capture
type VideoWidget struct {
	container        *fyne.Container
	videoCanvas      *canvas.Image
	touchpadWrapper  *TouchpadWrapper
	statusLabel      *widget.Label
	infoLabel        *widget.Label
	statsLabel       *widget.Label
	contentContainer *fyne.Container // Container for video and keyboard
	ui               *view.VideoWidgetUI
	statsTickerStop  chan struct{}

	// clearVideoMu serializes clearVideo() (and therefore stopMetalVideo() /
	// the native overlay teardown). Needed because the native Android destroy
	// path (android_vk_destroy in vk_video_impl_android.c) only guards against
	// being called again *after* a prior call finished — it checks g_active
	// then acts, non-atomically — so two concurrent callers can both pass the
	// check and both run the teardown, double-freeing Vulkan/EGL resources.
	// This was hit in practice: the unexpected-termination handler (video_widget_ctor.go)
	// spawns clearVideo() in its own goroutine, which can race with a
	// concurrent explicit disconnect's synchronous clearVideo() call in
	// stopVideoInternal(), producing a SIGSEGV.
	clearVideoMu sync.Mutex

	// State
	isStreaming      bool
	isVideoConnected bool
	isMouseConnected bool // Flag for whether the mouse is connected
	enableVSync      bool // mirrors VideoStartRequest.EnableVSync for the GL overlay

	// Services
	usbClient             *api.USBClient
	videoClient           service.VideoClient
	tailscaleService      *service.TailscaleService
	tailscaleVideoEnabled bool   // false when connected via direct/LAN (disables Tailscale UDP routing for video)
	bridgeInternalHost    string // LAN/internal IP of bridge; used to detect same-subnet direct path
	updateStatus          func()
	onFPSChanged          func(float64)
	videoOpMu             sync.Mutex
	videoOpRunning        bool
	desiredStreaming      bool
	videoRestartPending   bool
	moveQueueMu           sync.Mutex
	bottomInset           float32 // Bottom inset (e.g. for the keyboard) that pushes the video upward

	pendingMoveX          int
	pendingMoveY          int
	moveWorkerStarted     bool
	videoOps              chan videoOperation
	videoReconcilePending atomic.Bool

	// sendQueueMu/sendQueue/sendQueueWake/sendWorkerStarted back a small FIFO
	// worker (see video_widget_input_queue.go's enqueueSend) that moves every
	// synchronous cgo call into moonlight-common-c (mouse position/button/
	// scroll — an ENet reliable send that can block on socket/lock state) off
	// whichever goroutine calls it. Fyne invokes Dragged/Touch callbacks
	// synchronously on the UI goroutine, so without this a stalled ENet send
	// freezes the entire UI, including tab switches, until it returns.
	sendQueueMu       sync.Mutex
	sendQueue         []func()
	sendQueueWake     chan struct{}
	sendWorkerStarted bool

	// Video stream
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
	// consecutiveStuckReconnects counts how many beginVideoTrace attempts in
	// a row have hit the no-frame timeout (see beginVideoTrace's own doc
	// comment on why this shrinks the timeout on retries). Reset to 0 the
	// instant a trace actually receives a frame.
	consecutiveStuckReconnects atomic.Int32
	// videoSilenceReconnectFired latches once checkVideoSilence has already
	// forced a reconnect for the current stall, so the 1s stats-loop tick
	// doesn't re-trigger (and re-log) it every second while the reconcile is
	// still in flight. beginVideoTrace clears it when the next connection
	// attempt starts.
	videoSilenceReconnectFired atomic.Bool
	fpsWindowStart             atomic.Int64 // for Go-level frame arrival FPS logging
	metalFPSWarned             atomic.Bool  // gates the one-shot Metal FPS mismatch warning
	isMetalFullscreen          atomic.Bool  // true while Metal overlay covers the full fullscreen window
	onNativeReady              func()       // one-shot: called on main thread when native overlay (Metal/GL) is first created
	lastVideoImgW              float32      // pixel width of the last decoded video frame (for resize recalc when frame=nil)
	lastVideoImgH              float32      // pixel height of the last decoded video frame
	frameContentX              float32      // normalized active frame area on X without black bars
	frameContentY              float32      // normalized active frame area on Y without black bars
	frameContentW              float32      // normalized width of the active frame area
	frameContentH              float32      // normalized height of the active frame area

	// Dialogs
	fullscreenDialog      *FullscreenDialog
	startDialog           *view.VideoStartDialog
	pairingPINDialog      dialog.Dialog // shown by SetOnPairingPINRequired, dismissed by SetOnPairingPINResolved
	parentWindow          fyne.Window
	virtualKeyboard       *graphics.VirtualKeyboard
	keyboardModifierState atomic.Int32
	suppressRuneUntilNS   atomic.Int64
	moonlightKeyMu        sync.Mutex
	moonlightHeldVKs      map[int16]bool // tracks VK codes currently held in Moonlight session

	// Mouse/touchpad
	lastMouseX         float32
	lastMouseY         float32
	currentMouseX      float32 // Current mouse position (for polling)
	currentMouseY      float32 // Current mouse position (for polling)
	relativeRemainderX float32 // accumulated fractional remainder of relative movement on X
	relativeRemainderY float32 // accumulated fractional remainder of relative movement on Y
	isDragging         bool
	dragButton         int
	touchStartX        float32
	touchStartY        float32
	touchStartTime     time.Time
	mousePollingQuit   chan bool // Channel for stopping the polling goroutine
	mouseInputMode     string    // desired mode: "mouse" (touchpad), "touchscreen" or "absolute"
	observedMouseMode  string    // actual transport reported by the server via /api/device/info
	showMouseCursor    bool      // show the mouse cursor in the captured video
	agentOS            string
	agentDisplay       string
	cursorOverlayX     float32
	cursorOverlayY     float32
	cursorOverlayShown bool
	lastMouseModeDiag  string  // last mouse-mode diagnostic string, to avoid spamming the log
	touchpadSizeW      float32 // Width of the input area (for conversion to absolute coordinates)
	touchpadSizeH      float32 // Height of the input area
	// When > 0: standalone VK fullscreen is active and the touchpad logical size is
	// fixed to the screen dp dimensions. updateFrameContentRect uses these instead of
	// the (wrong) main-window widget size so absolute mouse mapping covers the full screen.
	standaloneVKScreenDpW float32
	standaloneVKScreenDpH float32
	// Video rectangle within the input area (ImageFillContain): for correct coordinate translation into 0..4095
	contentRectX     float32
	contentRectY     float32
	contentRectW     float32
	contentRectH     float32
	baseContentRectW float32
	baseContentRectH float32
	zoomScale        float32
	panOffsetX       float32
	panOffsetY       float32
	// bottomAnchorContentVertically switches recalculateViewport's "content
	// shorter than available area" branch from vertically centering the
	// video to anchoring it flush against the bottom of the available
	// area. False (preserving the original centering behavior) on every
	// platform except wasm, which sets this once the real IME's own
	// window-content swap logic starts running (see
	// syncKeyboardWindowContent in video_widget_web.go) -- without it,
	// centering meant the video visibly jumped/re-centered every time the
	// available height changed (IME open/close swapping window content in
	// or out), since a fixed-size video centered in a small box sits in a
	// very different screen position than the same video centered in a
	// much taller one. Bottom-anchoring keeps the video's position stable
	// relative to the keyboard panel that's always docked right below it,
	// so opening/closing the IME only ever reveals or hides space above
	// the video, never moves the video itself.
	bottomAnchorContentVertically bool
	multiTouchActive              bool
	lastMultiTouchAt              time.Time
	scrollDragAxis                string
	scrollDragLastX               float32
	scrollDragLastY               float32
	lastTouchX                    int // last sent touch coordinates (to avoid duplicating in MouseMoved)
	lastTouchY                    int
	lastAbsX                      int // last sent absolute (touch_position) coordinates, to avoid spamming
	lastAbsY                      int
	lastAbsSentTime               time.Time // time of the last absolute send (for debounce)
	absSendMu                     sync.Mutex
	absButtons                    uint8 // bitmask of buttons for absolute mode
	// Stats for periodic log (atomics — written from capture goroutine, read from log timer).
	statAbsMoonlight  atomic.Int64 // absolute events sent via Moonlight LiSendMousePositionEvent
	statAbsWS         atomic.Int64 // absolute events sent via WebSocket
	statRelMoonlight  atomic.Int64 // relative events sent via Moonlight SendMoonlightMouseMove
	statRelWS         atomic.Int64 // relative events sent via WebSocket SendMouseMove
	lastTouchDownTime time.Time    // time of the last SendTouch(_, _, true) — for deduplication
	touchDedupMu      sync.Mutex
	// Virtual cursor (Android "cursor" mouse mode): position in frame UV space (0..1).
	vcMu                      sync.Mutex
	virtualCursorU            float32
	virtualCursorV            float32
	lastVirtualCursorSentTime time.Time
	// touch(down) delay on MouseDown: if Tapped hasn't arrived within ~120ms — we treat it as a drag and send touch(true).
	// Tapped arrives on a full click on the widget; Fyne delivers MouseUp only to the widget under the cursor on release.
	touchDownDelayTimer *time.Timer
	touchDownDelayMu    sync.Mutex
	touchActive         bool // touch(true) has already been sent and touch(false) not yet; MouseMoved only sends while true
	// Tap-then-hold LMB drag for virtual cursor.
	// lastVirtualTapAt is set when a quick tap completes; second TouchDown within 600ms holds LMB.
	lastVirtualTapAt time.Time
	lmbHeld          bool
	// Android LMB-hold gesture helpers:
	// lmbTapAt records when Down1 was sent (used to compute guard from Down1).
	// lmbUpTimer is non-nil while Up1 is pending (100ms cancel window after Down1).
	// lmbPendingHold is set when second finger is down but we haven't decided hold vs double-click yet.
	// lmbHoldTimer fires after 200ms of the second finger being held, committing to the hold.
	// lmbPendingDoubleClick is set when second finger lifted quickly while lmbUpTimer still runs.
	lmbTapAt              time.Time
	lmbUp1Sent            bool // true once Up1 has actually been sent
	lmbUpTimer            *time.Timer
	lmbPendingHold        bool
	lmbHoldTimer          *time.Timer
	lmbPendingDoubleClick bool
	rmbHapticTimer        *time.Timer // fires at 1s hold to signal RMB long-press readiness
	isClosing             atomic.Bool

	// userStoppedVideo is set when the user explicitly presses stop/disconnect
	// and cleared on a fresh host connection (UpdateClient) or an explicit
	// start request. scheduleControlBootstrap's timers (main_window_lifecycle.go)
	// fire on a schedule tied to which tab is showing, not to user intent, and
	// used to unconditionally call setDesiredStreaming(true) — silently
	// reconnecting video/audio moments after the user asked to stop, since one
	// of those timers could still be pending when the stop happened.
	userStoppedVideo atomic.Bool
}

func (vw *VideoWidget) Close() {
	vw.isClosing.Store(true)
}

// MarkUserStopped records that the user explicitly asked to stop/disconnect,
// suppressing any pending scheduleControlBootstrap timer (main_window_lifecycle.go)
// that would otherwise silently restart video/audio moments later. Callers
// that disconnect from outside this widget (e.g. the main window's full
// host-disconnect flow) must call this synchronously, as early as possible —
// setting the same flag later from inside a background cleanup goroutine
// races against an already-pending bootstrap timer and can lose.
func (vw *VideoWidget) MarkUserStopped() {
	vw.userStoppedVideo.Store(true)
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
	// The user requested to avoid restarting the video stream when the USB gadget is rebuilt
	// (e.g. when changing mouse modes or mounting/unmounting devices), because it is a heavy operation.
	// The video stream will now stay alive; Moonlight will keep sending input packets which
	// will be routed to the new HID device once the gadget is back online.
	logrus.Infof("Control device rebuilt; intentionally keeping video stream alive")
}

// videoTraceFirstAttemptTimeout is used for a trace that isn't already part
// of a stuck-reconnect streak (see consecutiveStuckReconnects) -- kept
// conservative so a *normal*, first-time connect (Sunshine's own launch/
// negotiate can itself take a couple of seconds) is never mistaken for the
// stuck-with-no-video case this watchdog exists to catch.
//
// This is the value every platform except wasm uses (see
// videoTraceFirstAttemptTimeout method's own per-platform override files,
// video_widget_trace_timeout_default.go / _wasm.go) -- unchanged from
// before that split existed.
const videoTraceFirstAttemptTimeout = 4 * time.Second

// videoTraceRetryTimeout is used once beginVideoTrace already knows (via
// consecutiveStuckReconnects) that the *previous* attempt hit the no-frame
// timeout -- i.e. this reconnect is itself a recovery retry, not a fresh
// connect. Confirmed live: retrying every videoTraceFirstAttemptTimeout
// (4s) during an SDDM login/logout transition on the host meant several
// missed cycles (each one racing whatever the host side was still doing
// with its own recovery -- see rust-shine's capture-kms/gamestream-server
// fixes) before one happened to land after the host was actually ready
// again, adding up to a genuinely user-visible ~10-15s of frozen video.
// Once a streak is confirmed stuck, there's no first-connect ambiguity left
// to protect against, so polling much faster only shortens the time to
// notice the host has recovered -- it can't misfire on a legitimately slow
// *first* connect, since that path always starts a streak at 0.
//
// Was 1500ms, but on an nvidia host the KMS-probe-then-X11-greeter-fallback
// handoff (capture-kms's run_capture -> x11_fallback::try_run, since nvidia
// won't report CRTC state to a non-DRM-master client at all) routinely takes
// 1.3-1.6s on its own -- confirmed live: that landed this retry right on top
// of the host's own setup time, so every retry killed the host's session
// before it ever got a frame out, forever (0 frames delivered across 65+
// consecutive reconnects). 10s gives that handoff enough headroom to
// actually land a frame before this watchdog fires again.
const videoTraceRetryTimeout = 10 * time.Second

// videoMidStreamSilenceTimeout guards an *already-established* stream (one
// that already delivered at least one frame, so beginVideoTrace's own
// no-frame timeout has long since fired and gone dormant) going silent
// mid-session -- e.g. the host process dying/restarting under it. Confirmed
// live: after the host side recovers almost instantly (rust-shine's own
// capture-kms/gamestream-server crash fixes), the client still sat frozen
// for ~18-20s, because nothing client-side was watching for silence during
// an established session -- the only thing that eventually noticed was
// moonlight-common-c's own ENet control-channel peer timeout
// (enet_peer_timeout in ControlStream.c, deliberately set to 20s to tolerate
// relay/DERP jitter on the control path -- see the comment there). Checked
// via the existing 1s stats-loop tick (see startStatsLoop/checkVideoSilence),
// so worst-case detection is this timeout plus videoSilenceGracePeriod plus
// ~1s.
const videoMidStreamSilenceTimeout = 2 * time.Second

func (vw *VideoWidget) beginVideoTrace(reason string) uint64 {
	traceID := vw.videoTraceSeq.Add(1)
	startedAt := time.Now()
	vw.videoTraceID.Store(traceID)
	vw.videoTraceStartedAt.Store(startedAt.UnixNano())
	vw.videoTraceFirstFrame.Store(0)
	vw.videoTraceFirstPaint.Store(0)
	vw.videoSilenceReconnectFired.Store(false)
	// Reset saved video dimensions so a new stream with different resolution
	// doesn't inherit stale values from the previous session.
	vw.lastVideoImgW = 0
	vw.lastVideoImgH = 0
	label := fmt.Sprintf("vt-%d", traceID)
	vw.videoTraceLabel.Store(label)
	timeout := vw.videoTraceFirstAttemptTimeout()
	if vw.consecutiveStuckReconnects.Load() > 0 {
		timeout = videoTraceRetryTimeout
	}
	logrus.Infof("🎯 [VideoTrace #%d] start label=%s reason=%s timeout=%s", traceID, label, reason, timeout)

	time.AfterFunc(timeout, func() {
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
			streak := vw.consecutiveStuckReconnects.Add(1)
			logrus.Warnf("⚠️ [VideoTrace #%d] no frames reached client after %s (streak=%d) video_stats=%v relay=%s — forcing reconnect", traceID, time.Since(start).Round(time.Millisecond), streak, vw.safeVideoStats(), vw.safeRelayDebugInfo())
			vw.forceReconnectStuckStream(reason)
		case firstPaintNs == 0:
			logrus.Warnf("⚠️ [VideoTrace #%d] client receives frames but UI has not painted after %s", traceID, time.Since(start).Round(time.Millisecond))
		default:
			logrus.Infof("✅ [VideoTrace #%d] startup path complete frame=%s paint=%s", traceID, time.Unix(0, firstFrameNs).Sub(start).Round(time.Millisecond), time.Unix(0, firstPaintNs).Sub(start).Round(time.Millisecond))
		}
	})

	return traceID
}

// forceReconnectStuckStream recovers a session that negotiated successfully
// (Sunshine accepted the connection; RTSP/control/audio/input all reported
// success) but never actually delivered a video frame — reproduced
// intermittently after switching the captured monitor on a multi-GPU
// Windows machine, where Sunshine hands its capture/encode pipeline off to a
// *different* physical GPU (e.g. one monitor wired to an NVIDIA dGPU, the
// other to an AMD/Intel iGPU) and that handoff can silently fail with no
// error surfaced anywhere, client or server side.
//
// moonlight-common-c's own watchdogs (ML_ERROR_NO_VIDEO_TRAFFIC /
// ML_ERROR_NO_VIDEO_FRAME, both 10s — see VideoStream.c) don't reliably
// catch this: receivedDataFromPeer latches true forever after a single
// packet (even a runt/duplicate one), permanently disabling the "no
// traffic" timer, while the "no full frame yet" timer is only re-evaluated
// on receipt of the *next* packet — so if traffic stops completely after
// that one packet, neither ever fires again and the session hangs
// indefinitely. Confirmed live: a stuck session sat "connected" with zero
// frames for 3+ minutes with no self-recovery.
//
// Manually forcing a codec change or an explicit reconnect was found to
// reliably clear it, so this automates exactly that: reuse the same
// "stop, then restart with the current config" path already used for an
// explicit device switch (videoRestartPending), rather than teaching this
// watchdog its own separate recovery mechanism.
func (vw *VideoWidget) forceReconnectStuckStream(reason string) {
	vw.videoOpMu.Lock()
	if !vw.desiredStreaming || !vw.isStreaming {
		vw.videoOpMu.Unlock()
		return
	}
	vw.videoRestartPending = true
	vw.videoOpMu.Unlock()
	vw.scheduleVideoReconcile("stuck-no-frame:" + reason)
}

// videoSilenceGracePeriod is how much longer checkVideoSilence waits, once
// the stream has first crossed videoMidStreamSilenceTimeout, before actually
// forcing a reconnect. A brief network stall (wifi handoff, DERP jitter) is
// exactly the kind of gap moonlight-common-c's own loss recovery already
// handles on its own via LiRequestIdrFrame -- confirmed live, a keyframe
// landed ~150ms after the old immediate-fire version of this watchdog had
// already torn the session down, so the reconnect it forced was pure waste:
// it discarded recovery that was already in flight and paid for a brand-new
// RTSP handshake plus that reconnect's own ~500ms first-keyframe cost
// instead of just waiting a moment longer for the keyframe already on its
// way. Worst case this adds ~1.5-2.5s to detecting a genuinely dead stream,
// which is cheap compared to an unnecessary full reconnect.
const videoSilenceGracePeriod = 1500 * time.Millisecond

// videoSilenceThreshold returns how long checkVideoSilence tolerates zero
// new frames during an established stream before forcing a reconnect --
// implemented per-platform (video_widget_silence_default.go for every
// platform except wasm, video_widget_silence_wasm.go for wasm) rather than
// as a single shared constant here; see either file's own doc comment for
// why wasm needs a longer tolerance than the rest.

// checkVideoSilence is polled once a second (see startStatsLoop) while
// streaming. It only looks at *established* streams -- lastFrameTime is zero
// until the very first frame arrives, which is deliberately left to
// beginVideoTrace's own no-frame timeout so the two watchdogs don't race
// each other on a fresh/slow-starting connect.
func (vw *VideoWidget) checkVideoSilence() {
	vw.frameMutex.RLock()
	last := vw.lastFrameTime
	vw.frameMutex.RUnlock()

	if last.IsZero() {
		return
	}
	silence := time.Since(last)
	if silence < vw.videoSilenceThreshold() {
		return
	}
	if !vw.videoSilenceReconnectFired.CompareAndSwap(false, true) {
		return
	}
	logrus.Warnf("⚠️ [VideoWidget] no video frame for %s during an established stream — forcing reconnect", silence.Round(time.Millisecond))
	vw.forceReconnectStuckStream("mid-stream-silence")
}

func (vw *VideoWidget) currentVideoTraceLabel() string {
	if value, ok := vw.videoTraceLabel.Load().(string); ok {
		return value
	}
	return ""
}

func (vw *VideoWidget) safeVideoStats() map[string]interface{} {
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
	// A frame actually arrived -- whatever server-side transition this
	// client might have been retrying through (see beginVideoTrace's doc
	// comment) is over, so the *next* trace (a genuinely fresh connect)
	// should get the conservative first-attempt timeout again, not
	// inherit a short one from a streak that just ended.
	vw.consecutiveStuckReconnects.Store(0)
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

// SetBottomInset sets the bottom inset and recalculates the viewport.
func (vw *VideoWidget) SetBottomInset(inset float32) {
	logrus.Infof("📐 SetBottomInset: %.1f", inset)
	vw.bottomInset = inset
	vw.recalculateViewport()
}
