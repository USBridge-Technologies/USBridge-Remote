//go:build windows && cgo

package service

/*
#cgo LDFLAGS: -lgdi32 -luser32

#include <stdint.h>

// Implemented in gl_video_impl_windows.c.
extern int  gl_video_is_active(void);
extern int  gl_video_try_submit(uint8_t *rgba, int width, int height, int stride);
extern int  gl_video_create(uintptr_t parent_hwnd, int x, int y, int w, int h, int vsync);
extern void gl_video_update_frame(int x, int y, int w, int h);
extern void gl_video_destroy(void);
extern void gl_video_get_stats(long long *rendered, long long *submitted,
                               float *fps, int *fps_ready,
                               int *first_frame, int *fw, int *fh,
                               int *stage, int *stage_ms,
                               int *last_submit_ms, int *last_render_begin_ms, int *last_render_end_ms,
                               int *last_blt_begin_ms, int *last_blt_end_ms,
                               int *last_render_ms, int *last_blt_ms,
                               int *last_blt_ok, unsigned *last_winerr,
                               long long *update_posted, long long *update_applied,
                               int *last_update_req_ms, int *last_update_apply_ms);
extern void gl_video_clear_pending_stats(void);

// goGLLog is called only from gl_video_create/destroy (CGO context — safe).
// It is NOT called from the render thread to avoid Go-GC deadlock.
extern void goGLLog(char *msg, int level);
*/
import "C"

import (
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/sirupsen/logrus"
)

var glDiagGeneration uint64

//export goGLLog
func goGLLog(msg *C.char, level C.int) {
	text := C.GoString(msg)
	switch int(level) {
	case 1:
		logrus.Warnf("[GDI/Win] %s", text)
	case 2:
		logrus.Errorf("[GDI/Win] %s", text)
	default:
		logrus.Infof("[GDI/Win] %s", text)
	}
}

func GLVideoIsActive() bool {
	return C.gl_video_is_active() != 0
}

// GLVideoTrySubmit offers a BGRA frame to the native Windows render thread.
// Returns true if the GDI overlay is active and the frame was queued.
func GLVideoTrySubmit(rgba []byte, width, height, stride int) bool {
	if len(rgba) == 0 {
		return false
	}
	return C.gl_video_try_submit(
		(*C.uint8_t)(unsafe.Pointer(&rgba[0])),
		C.int(width), C.int(height), C.int(stride),
	) != 0
}

// GLVideoCreate creates (or replaces) the GDI overlay on the given HWND.
// x,y,w,h are in Windows client pixels (DPI-scaled by caller).
// Pass w=0,h=0 to cover the entire parent client area.
func GLVideoCreate(hwnd uintptr, x, y, w, h int, vsync bool) bool {
	v := C.int(0)
	if vsync {
		v = 1
	}
	ok := C.gl_video_create(C.uintptr_t(hwnd), C.int(x), C.int(y), C.int(w), C.int(h), v) != 0
	if ok {
		startGLVideoDiagnostics()
	}
	return ok
}

// Last overlay geometry sent to C — used to suppress no-op repositions.
// Written and read only from fyne.Do (single-threaded), no mutex needed.
var glOverlayLastX, glOverlayLastY, glOverlayLastW, glOverlayLastH int

// GLVideoUpdateFrame repositions the GDI overlay (pixels).
// Skips the call if the geometry hasn't changed to avoid redundant PostMessage spam.
func GLVideoUpdateFrame(x, y, w, h int) {
	if x == glOverlayLastX && y == glOverlayLastY && w == glOverlayLastW && h == glOverlayLastH {
		return
	}
	glOverlayLastX, glOverlayLastY, glOverlayLastW, glOverlayLastH = x, y, w, h
	C.gl_video_update_frame(C.int(x), C.int(y), C.int(w), C.int(h))
}

// GLVideoResetLastFrame resets the cached overlay geometry so the next
// GLVideoUpdateFrame call unconditionally repositions the window.
func GLVideoResetLastFrame() {
	glOverlayLastX, glOverlayLastY, glOverlayLastW, glOverlayLastH = 0, 0, 0, 0
}

// GLVideoDestroy removes the overlay and stops the render thread.
func GLVideoDestroy() {
	atomic.AddUint64(&glDiagGeneration, 1)
	C.gl_video_destroy()
}

// GLVideoStats holds render-thread statistics read via atomic polling.
type GLVideoStats struct {
	Rendered          int64
	Submitted         int64
	FPS               float32
	FPSReady          bool
	FirstFrame        bool
	FW, FH            int
	Stage             int
	StageMS           int
	LastSubmitMS      int
	LastRenderBeginMS int
	LastRenderEndMS   int
	LastBltBeginMS    int
	LastBltEndMS      int
	LastRenderMS      int
	LastBltMS         int
	LastBltOK         bool
	LastWinErr        uint32
	UpdatePosted      int64
	UpdateApplied     int64
	LastUpdateReqMS   int
	LastUpdateApplyMS int
}

// GLVideoGetStats reads stats accumulated by the render thread.
// Safe to call from any Go goroutine (no CGO from render thread).
func GLVideoGetStats() GLVideoStats {
	var r, s C.longlong
	var fp C.float
	var fpsr, ff, fw, fh C.int
	var stage, stageMS C.int
	var lastSubmitMS, lastRenderBeginMS, lastRenderEndMS C.int
	var lastBltBeginMS, lastBltEndMS, lastRenderMS, lastBltMS C.int
	var lastBltOK C.int
	var lastWinErr C.uint
	var updatePosted, updateApplied C.longlong
	var lastUpdateReqMS, lastUpdateApplyMS C.int
	C.gl_video_get_stats(
		&r, &s, &fp, &fpsr, &ff, &fw, &fh,
		&stage, &stageMS,
		&lastSubmitMS, &lastRenderBeginMS, &lastRenderEndMS,
		&lastBltBeginMS, &lastBltEndMS,
		&lastRenderMS, &lastBltMS,
		&lastBltOK, &lastWinErr,
		&updatePosted, &updateApplied,
		&lastUpdateReqMS, &lastUpdateApplyMS,
	)
	return GLVideoStats{
		Rendered:          int64(r),
		Submitted:         int64(s),
		FPS:               float32(fp),
		FPSReady:          fpsr != 0,
		FirstFrame:        ff != 0,
		FW:                int(fw),
		FH:                int(fh),
		Stage:             int(stage),
		StageMS:           int(stageMS),
		LastSubmitMS:      int(lastSubmitMS),
		LastRenderBeginMS: int(lastRenderBeginMS),
		LastRenderEndMS:   int(lastRenderEndMS),
		LastBltBeginMS:    int(lastBltBeginMS),
		LastBltEndMS:      int(lastBltEndMS),
		LastRenderMS:      int(lastRenderMS),
		LastBltMS:         int(lastBltMS),
		LastBltOK:         lastBltOK != 0,
		LastWinErr:        uint32(lastWinErr),
		UpdatePosted:      int64(updatePosted),
		UpdateApplied:     int64(updateApplied),
		LastUpdateReqMS:   int(lastUpdateReqMS),
		LastUpdateApplyMS: int(lastUpdateApplyMS),
	}
}

// GLVideoClearPendingStats resets the "first frame" and "fps ready" flags
// after Go has consumed them.
func GLVideoClearPendingStats() {
	C.gl_video_clear_pending_stats()
}

func startGLVideoDiagnostics() {
	gen := atomic.AddUint64(&glDiagGeneration, 1)
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()

		var lastRendered, lastSubmitted int64
		for range ticker.C {
			if atomic.LoadUint64(&glDiagGeneration) != gen || !GLVideoIsActive() {
				return
			}
			st := GLVideoGetStats()
			renderDelta := st.Rendered - lastRendered
			submitDelta := st.Submitted - lastSubmitted
			lastRendered = st.Rendered
			lastSubmitted = st.Submitted

			logf := logrus.Infof
			if st.StageMS > 2000 || st.LastBltMS > 250 || (submitDelta > 0 && renderDelta == 0) {
				logf = logrus.Warnf
			}
			logf("[GDI/Win][watchdog] stage=%s(%dms) submitted=%d(+%d) rendered=%d(+%d) lastSubmitAgo=%dms renderBeginAgo=%dms renderEndAgo=%dms lastRender=%dms bltBeginAgo=%dms bltEndAgo=%dms lastBlt=%dms bltOK=%v winerr=%d wmUpdate=%d/%d updateReqAgo=%dms updateApplyAgo=%dms",
				gdiStageName(st.Stage), st.StageMS,
				st.Submitted, submitDelta, st.Rendered, renderDelta,
				st.LastSubmitMS, st.LastRenderBeginMS, st.LastRenderEndMS, st.LastRenderMS,
				st.LastBltBeginMS, st.LastBltEndMS, st.LastBltMS, st.LastBltOK, st.LastWinErr,
				st.UpdatePosted, st.UpdateApplied, st.LastUpdateReqMS, st.LastUpdateApplyMS)
		}
	}()
}

func gdiStageName(stage int) string {
	switch stage {
	case 0:
		return "idle"
	case 1:
		return "wait"
	case 2:
		return "copy-pending"
	case 3:
		return "ensure-dib"
	case 4:
		return "copy-dib"
	case 5:
		return "get-client"
	case 6:
		return "fill-bars"
	case 7:
		return "stretch-blt"
	case 8:
		return "stats"
	default:
		return "unknown"
	}
}
