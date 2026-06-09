// gl_video_impl_windows.c — GDI video overlay for Windows.
//
// Previously used WGL/OpenGL which caused a DWM compositor deadlock on Win10+:
//   render-thread SwapBuffers → DWM → SendMessage(parent) blocked on GLFW loop
//   GLFW loop → blocked in its own SwapBuffers waiting for DWM
//
// Now uses CreateDIBSection + StretchBlt. GDI draws directly via WDDM without
// any DWM swap-chain synchronisation, eliminating the deadlock entirely.

#ifdef _WIN32

#define WIN32_LEAN_AND_MEAN
#include <windows.h>
#include <stdint.h>
#include <string.h>
#include <stdlib.h>
#include <stdio.h>
#include <stdatomic.h>

// goGLLog — safe only from CGO context (gl_video_create/destroy calls).
// Never call from the render thread (native CreateThread).
extern void goGLLog(char *msg, int level);

// ─────────────────────────────────────────────────────────────────────────────
// Window / GDI state

static HWND    g_hwnd    = NULL;  // parent Fyne/GLFW HWND; no child HWND is created
static HDC     g_hdc     = NULL;  // parent window DC acquired with GetDC(parent)
static atomic_int g_dst_x = 0, g_dst_y = 0, g_dst_w = 0, g_dst_h = 0;

// DIB section used as source bitmap for StretchBlt.
// Pixel format: BGRA (matches AV_PIX_FMT_BGRA from sws_scale — pure memcpy, no swap).
static HBITMAP  g_bmp      = NULL;
static uint8_t *g_bmp_bits = NULL;
static int      g_bmp_w    = 0, g_bmp_h = 0;

// Cached memory DC for StretchBlt (avoids CreateCompatibleDC per frame).
static HDC     g_memDC    = NULL;
static HBITMAP g_memDC_old = NULL;

static volatile atomic_int g_active = 0;

// Pending frame buffer — protected by g_cs.
static uint8_t *g_buf    = NULL;
static size_t   g_buf_sz = 0;
static int      g_fw = 0, g_fh = 0, g_fs = 0;
static volatile int g_ready = 0;

static CRITICAL_SECTION g_cs;
static int              g_cs_init = 0;
static HANDLE           g_thread  = NULL;
static HANDLE           g_event   = NULL;

// ── Stats — written by render thread (volatile), read by Go via polling ───────
static volatile long long g_submitted = 0, g_rendered = 0;
static volatile long long g_fps_n = 0;
static volatile double    g_fps_t0 = 0.0;
static volatile long long g_stat_rendered  = 0;
static volatile long long g_stat_submitted = 0;
static volatile float     g_stat_fps       = 0.0f;
static volatile int       g_stat_fps_ready = 0;
static volatile int       g_stat_first     = 0;
static volatile int       g_stat_fw = 0, g_stat_fh = 0;

// ── Diagnostics exported to a Go watchdog goroutine ─────────────────────────
enum {
    GDI_STAGE_IDLE = 0,
    GDI_STAGE_WAIT = 1,
    GDI_STAGE_COPY_PENDING = 2,
    GDI_STAGE_ENSURE_DIB = 3,
    GDI_STAGE_COPY_DIB = 4,
    GDI_STAGE_GET_CLIENT = 5,
    GDI_STAGE_FILL_BARS = 6,
    GDI_STAGE_STRETCH_BLT = 7,
    GDI_STAGE_STATS = 8,
};

static volatile int       g_diag_stage = GDI_STAGE_IDLE;
static volatile double    g_diag_stage_t = 0.0;
static volatile double    g_diag_last_submit_t = 0.0;
static volatile double    g_diag_last_render_begin_t = 0.0;
static volatile double    g_diag_last_render_end_t = 0.0;
static volatile double    g_diag_last_blt_begin_t = 0.0;
static volatile double    g_diag_last_blt_end_t = 0.0;
static volatile double    g_diag_last_render_ms = 0.0;
static volatile double    g_diag_last_blt_ms = 0.0;
static volatile int       g_diag_last_blt_ok = 0;
static volatile unsigned  g_diag_last_winerr = 0;
static volatile long long g_diag_update_posted = 0;
static volatile long long g_diag_update_applied = 0;
static volatile double    g_diag_last_update_req_t = 0.0;
static volatile double    g_diag_last_update_apply_t = 0.0;

// ─────────────────────────────────────────────────────────────────────────────

static double mono_sec(void) {
    LARGE_INTEGER f, c;
    QueryPerformanceFrequency(&f);
    QueryPerformanceCounter(&c);
    return (double)c.QuadPart / (double)f.QuadPart;
}

static void diag_stage(int stage) {
    g_diag_stage = stage;
    g_diag_stage_t = mono_sec();
}

// Recreate the DIB section (and memory DC) only when frame dimensions change.
static void gdi_ensure_bmp(int w, int h) {
    if (g_bmp && g_bmp_w == w && g_bmp_h == h) return;

    // Detach old bitmap from memory DC before deleting it.
    if (g_memDC && g_memDC_old) { SelectObject(g_memDC, g_memDC_old); g_memDC_old = NULL; }
    if (g_bmp) { DeleteObject(g_bmp); g_bmp = NULL; g_bmp_bits = NULL; }

    BITMAPINFO bmi = {0};
    bmi.bmiHeader.biSize        = sizeof(BITMAPINFOHEADER);
    bmi.bmiHeader.biWidth       = w;
    bmi.bmiHeader.biHeight      = -h; // negative = top-down (matches FFmpeg row order)
    bmi.bmiHeader.biPlanes      = 1;
    bmi.bmiHeader.biBitCount    = 32;
    bmi.bmiHeader.biCompression = BI_RGB; // 32-bit: B G R A (GDI ignores A)

    HDC screen = GetDC(NULL);
    g_bmp = CreateDIBSection(screen, &bmi, DIB_RGB_COLORS, (void**)&g_bmp_bits, NULL, 0);
    ReleaseDC(NULL, screen);

    if (g_bmp) {
        g_bmp_w = w; g_bmp_h = h;
        // Create/reuse memory DC and select the new bitmap.
        if (!g_memDC) g_memDC = CreateCompatibleDC(g_hdc);
        if (g_memDC)  g_memDC_old = (HBITMAP)SelectObject(g_memDC, g_bmp);
    }
}

static void gdi_render_frame(void) {
    int fw, fh, fs;
    uint8_t *tmp = NULL;
    double render_t0 = mono_sec();
    g_diag_last_render_begin_t = render_t0;

    diag_stage(GDI_STAGE_COPY_PENDING);
    EnterCriticalSection(&g_cs);
    if (g_ready && g_buf) {
        fw = g_fw; fh = g_fh; fs = g_fs;
        size_t sz = (size_t)fh * (size_t)fs;
        tmp = (uint8_t*)malloc(sz);
        if (tmp) memcpy(tmp, g_buf, sz);
        g_ready = 0;
    }
    LeaveCriticalSection(&g_cs);
    if (!tmp) return;

    // Copy BGRA rows into DIB (AV_PIX_FMT_BGRA matches GDI's BI_RGB/32-bit layout exactly).
    diag_stage(GDI_STAGE_ENSURE_DIB);
    gdi_ensure_bmp(fw, fh);
    diag_stage(GDI_STAGE_COPY_DIB);
    if (g_bmp && g_bmp_bits) {
        size_t row = (size_t)fw * 4;
        for (int y = 0; y < fh; y++)
            memcpy(g_bmp_bits + (size_t)y * row, tmp + (size_t)y * (size_t)fs, row);
    }
    free(tmp);

    if (!g_bmp || !g_memDC || !g_hdc) return;

    diag_stage(GDI_STAGE_GET_CLIENT);
    RECT rc; GetClientRect(g_hwnd, &rc);
    int parent_w = rc.right - rc.left, parent_h = rc.bottom - rc.top;
    if (parent_w <= 0 || parent_h <= 0) return;

    int vx = atomic_load(&g_dst_x);
    int vy = atomic_load(&g_dst_y);
    int ww = atomic_load(&g_dst_w);
    int wh = atomic_load(&g_dst_h);
    if (ww <= 0 || wh <= 0) {
        vx = 0; vy = 0; ww = parent_w; wh = parent_h;
    }
    if (vx < 0) { ww += vx; vx = 0; }
    if (vy < 0) { wh += vy; vy = 0; }
    if (vx + ww > parent_w) ww = parent_w - vx;
    if (vy + wh > parent_h) wh = parent_h - vy;
    if (ww <= 0 || wh <= 0) return;

    // Letterbox: keep frame aspect ratio, fill remainder with black.
    float fa = (float)fw / (float)(fh ? fh : 1);
    float wa = (float)ww / (float)(wh ? wh : 1);
    int dx = 0, dy = 0, dw = ww, dh = wh;
    if (fa > wa) { dh = (int)(ww / fa + 0.5f); dy = (wh - dh) / 2; }
    else         { dw = (int)(wh * fa + 0.5f); dx = (ww - dw) / 2; }
    dx += vx;
    dy += vy;

    diag_stage(GDI_STAGE_FILL_BARS);
    HBRUSH blk = (HBRUSH)GetStockObject(BLACK_BRUSH);
    if (dy > vy) {
        RECT top = {vx, vy, vx + ww, dy};       FillRect(g_hdc, &top, blk);
        RECT bot = {vx, dy+dh, vx + ww, vy + wh};   FillRect(g_hdc, &bot, blk);
    }
    if (dx > vx) {
        RECT lft = {vx, dy, dx, dy+dh};          FillRect(g_hdc, &lft, blk);
        RECT rgt = {dx+dw, dy, vx + ww, dy+dh};      FillRect(g_hdc, &rgt, blk);
    }

    SetStretchBltMode(g_hdc, HALFTONE);
    SetBrushOrgEx(g_hdc, 0, 0, NULL); // required after HALFTONE
    diag_stage(GDI_STAGE_STRETCH_BLT);
    double blt_t0 = mono_sec();
    g_diag_last_blt_begin_t = blt_t0;
    BOOL blt_ok = StretchBlt(g_hdc, dx, dy, dw, dh, g_memDC, 0, 0, fw, fh, SRCCOPY);
    double blt_t1 = mono_sec();
    g_diag_last_blt_end_t = blt_t1;
    g_diag_last_blt_ms = (blt_t1 - blt_t0) * 1000.0;
    g_diag_last_blt_ok = blt_ok ? 1 : 0;
    g_diag_last_winerr = blt_ok ? 0 : GetLastError();

    // Update stats atomics (render thread — no goGLLog allowed here).
    diag_stage(GDI_STAGE_STATS);
    g_rendered++;
    g_fps_n++;
    g_stat_rendered  = g_rendered;
    g_stat_submitted = g_submitted;
    double now = mono_sec();
    if (g_rendered == 1) {
        g_stat_first = 1; g_stat_fw = fw; g_stat_fh = fh;
        g_fps_t0 = now; g_fps_n = 0;
    }
    double el = now - g_fps_t0;
    if (el >= 5.0 && g_fps_n > 0) {
        g_stat_fps       = (float)((double)g_fps_n / el);
        g_stat_fps_ready = 1;
        g_fps_t0 = now; g_fps_n = 0;
    }
    double render_t1 = mono_sec();
    g_diag_last_render_end_t = render_t1;
    g_diag_last_render_ms = (render_t1 - render_t0) * 1000.0;
    diag_stage(GDI_STAGE_IDLE);
}

static DWORD WINAPI render_thread_fn(LPVOID unused) {
    (void)unused;
    // Pure GDI — no wglMakeCurrent, no SwapBuffers, no DWM interaction.
    while (atomic_load(&g_active)) {
        diag_stage(GDI_STAGE_WAIT);
        DWORD r = WaitForSingleObject(g_event, 16);
        if (!atomic_load(&g_active)) break;
        if ((r == WAIT_OBJECT_0 || r == WAIT_TIMEOUT) && g_ready)
            gdi_render_frame();
    }
    return 0;
}

// ─────────────────────────────────────────────────────────────────────────────
// Public C API (called from gl_video_windows.go via CGO)

int gl_video_is_active(void) { return atomic_load(&g_active); }

// gl_video_try_submit — accepts BGRA frames (AV_PIX_FMT_BGRA from sws_scale).
// Returns 1 if overlay is active and the frame was queued.
int gl_video_try_submit(uint8_t *rgba, int width, int height, int stride) {
    if (!atomic_load(&g_active)) return 0;
    size_t sz = (size_t)height * (size_t)stride;
    if (!g_cs_init) return 0;
    EnterCriticalSection(&g_cs);
    if (!g_buf || g_buf_sz < sz) {
        free(g_buf);
        g_buf = (uint8_t*)malloc(sz);
        g_buf_sz = (g_buf ? sz : 0);
    }
    if (g_buf) {
        memcpy(g_buf, rgba, sz);
        g_fw = width; g_fh = height; g_fs = stride;
        g_ready = 1; g_submitted++;
        g_diag_last_submit_t = mono_sec();
    }
    LeaveCriticalSection(&g_cs);
    SetEvent(g_event);
    return 1;
}

// x,y,w,h in Windows client pixels. Pass w<=0 or h<=0 for full-client-area mode.
int gl_video_create(uintptr_t parent_hwnd, int x, int y, int w, int h, int vsync) {
    (void)vsync; // GDI has no swap interval
    if (atomic_load(&g_active)) {
        atomic_store(&g_active, 0);
        if (g_thread)  { SetEvent(g_event); WaitForSingleObject(g_thread, 2000); CloseHandle(g_thread); g_thread=NULL; }
        if (g_event)   { CloseHandle(g_event); g_event=NULL; }
        if (g_memDC)   { if (g_memDC_old) SelectObject(g_memDC, g_memDC_old); DeleteDC(g_memDC); g_memDC=NULL; g_memDC_old=NULL; }
        if (g_bmp)     { DeleteObject(g_bmp); g_bmp=NULL; g_bmp_bits=NULL; }
        if (g_hdc && g_hwnd) { ReleaseDC(g_hwnd, g_hdc); g_hdc=NULL; }
        g_hwnd=NULL;
        if (g_cs_init) { DeleteCriticalSection(&g_cs); g_cs_init=0; }
        if (g_buf)     { free(g_buf); g_buf=NULL; g_buf_sz=0; }
        g_bmp_w=0; g_bmp_h=0;
    }

    HWND parent = (HWND)(uintptr_t)parent_hwnd;
    if (!parent) { goGLLog("gl_video_create: parent HWND is null", 2); return 0; }

    int fullwin = (w <= 0 || h <= 0);
    if (fullwin) {
        RECT rc; GetClientRect(parent, &rc);
        x=0; y=0; w=rc.right-rc.left; h=rc.bottom-rc.top;
    }

    g_hwnd = parent;
    g_hdc = GetDC(parent);
    if (!g_hdc) { goGLLog("gl_video_create: GetDC(parent) failed", 2); g_hwnd=NULL; return 0; }
    atomic_store(&g_dst_x, x);
    atomic_store(&g_dst_y, y);
    atomic_store(&g_dst_w, w);
    atomic_store(&g_dst_h, h);

    InitializeCriticalSection(&g_cs); g_cs_init = 1;
    g_event = CreateEventW(NULL, FALSE, FALSE, NULL);
    if (!g_event) {
        goGLLog("gl_video_create: CreateEvent failed", 2);
        ReleaseDC(g_hwnd, g_hdc); g_hwnd=NULL; g_hdc=NULL;
        DeleteCriticalSection(&g_cs); g_cs_init=0; return 0;
    }

    g_submitted=0; g_rendered=0; g_fps_n=0; g_fps_t0=0;
    g_ready=0; g_stat_first=0; g_stat_fw=0; g_stat_fh=0;
    g_diag_last_submit_t=0; g_diag_last_render_begin_t=0; g_diag_last_render_end_t=0;
    g_diag_last_blt_begin_t=0; g_diag_last_blt_end_t=0; g_diag_last_render_ms=0; g_diag_last_blt_ms=0;
    g_diag_last_blt_ok=0; g_diag_last_winerr=0; g_diag_update_posted=0; g_diag_update_applied=0;
    g_diag_last_update_req_t=0; g_diag_last_update_apply_t=0; diag_stage(GDI_STAGE_IDLE);
    atomic_store(&g_active, 1);

    g_thread = CreateThread(NULL, 0, render_thread_fn, NULL, 0, NULL);
    if (!g_thread) {
        goGLLog("gl_video_create: CreateThread failed", 2);
        atomic_store(&g_active, 0);
        CloseHandle(g_event); g_event=NULL;
        ReleaseDC(g_hwnd, g_hdc); g_hwnd=NULL; g_hdc=NULL;
        DeleteCriticalSection(&g_cs); g_cs_init=0; return 0;
    }

    char m[256];
    if (fullwin)
        snprintf(m, sizeof(m), "GDI parent-DC renderer created (full-window %dx%d) HWND=%p", w, h, (void*)parent_hwnd);
    else
        snprintf(m, sizeof(m), "GDI parent-DC renderer created %dx%d at (%d,%d) HWND=%p", w, h, x, y, (void*)parent_hwnd);
    goGLLog(m, 0);
    return 1;
}

// Stores the target video rect in parent client pixels. The render thread reads
// this atomically and draws directly into the parent DC; no child HWND/messages.
void gl_video_update_frame(int x, int y, int w, int h) {
    if (!atomic_load(&g_active) || !g_hwnd) return;
    atomic_store(&g_dst_x, x);
    atomic_store(&g_dst_y, y);
    atomic_store(&g_dst_w, w);
    atomic_store(&g_dst_h, h);
    g_diag_update_posted++;
    g_diag_update_applied++;
    g_diag_last_update_req_t = mono_sec();
    g_diag_last_update_apply_t = g_diag_last_update_req_t;
}

void gl_video_destroy(void) {
    if (!atomic_load(&g_active)) return;
    atomic_store(&g_active, 0);
    if (g_thread)  { SetEvent(g_event); WaitForSingleObject(g_thread, 2000); CloseHandle(g_thread); g_thread=NULL; }
    if (g_event)   { CloseHandle(g_event); g_event=NULL; }
    if (g_memDC)   { if (g_memDC_old) SelectObject(g_memDC, g_memDC_old); DeleteDC(g_memDC); g_memDC=NULL; g_memDC_old=NULL; }
    if (g_bmp)     { DeleteObject(g_bmp); g_bmp=NULL; g_bmp_bits=NULL; }
    if (g_hdc && g_hwnd) { ReleaseDC(g_hwnd, g_hdc); g_hdc=NULL; }
    g_hwnd=NULL;
    if (g_cs_init) { DeleteCriticalSection(&g_cs); g_cs_init=0; }
    if (g_buf)     { free(g_buf); g_buf=NULL; g_buf_sz=0; }
    g_bmp_w=0; g_bmp_h=0;
    char m[192];
    snprintf(m, sizeof(m), "GDI overlay destroyed — rendered=%lld submitted=%lld", g_rendered, g_submitted);
    goGLLog(m, 0);
}

void gl_video_get_stats(long long *rendered, long long *submitted,
                        float *fps, int *fps_ready,
                        int *first_frame, int *fw, int *fh,
                        int *stage, int *stage_ms,
                        int *last_submit_ms, int *last_render_begin_ms, int *last_render_end_ms,
                        int *last_blt_begin_ms, int *last_blt_end_ms,
                        int *last_render_ms, int *last_blt_ms,
                        int *last_blt_ok, unsigned *last_winerr,
                        long long *update_posted, long long *update_applied,
                        int *last_update_req_ms, int *last_update_apply_ms) {
    double now = mono_sec();
    *rendered    = g_stat_rendered;
    *submitted   = g_stat_submitted;
    *fps         = g_stat_fps;
    *fps_ready   = g_stat_fps_ready;
    *first_frame = g_stat_first;
    *fw          = g_stat_fw;
    *fh          = g_stat_fh;
    *stage       = g_diag_stage;
    *stage_ms    = g_diag_stage_t > 0 ? (int)((now - g_diag_stage_t) * 1000.0) : -1;
    *last_submit_ms = g_diag_last_submit_t > 0 ? (int)((now - g_diag_last_submit_t) * 1000.0) : -1;
    *last_render_begin_ms = g_diag_last_render_begin_t > 0 ? (int)((now - g_diag_last_render_begin_t) * 1000.0) : -1;
    *last_render_end_ms = g_diag_last_render_end_t > 0 ? (int)((now - g_diag_last_render_end_t) * 1000.0) : -1;
    *last_blt_begin_ms = g_diag_last_blt_begin_t > 0 ? (int)((now - g_diag_last_blt_begin_t) * 1000.0) : -1;
    *last_blt_end_ms = g_diag_last_blt_end_t > 0 ? (int)((now - g_diag_last_blt_end_t) * 1000.0) : -1;
    *last_render_ms = (int)g_diag_last_render_ms;
    *last_blt_ms = (int)g_diag_last_blt_ms;
    *last_blt_ok = g_diag_last_blt_ok;
    *last_winerr = g_diag_last_winerr;
    *update_posted = g_diag_update_posted;
    *update_applied = g_diag_update_applied;
    *last_update_req_ms = g_diag_last_update_req_t > 0 ? (int)((now - g_diag_last_update_req_t) * 1000.0) : -1;
    *last_update_apply_ms = g_diag_last_update_apply_t > 0 ? (int)((now - g_diag_last_update_apply_t) * 1000.0) : -1;
}

void gl_video_clear_pending_stats(void) {
    g_stat_fps_ready = 0;
    g_stat_first     = 0;
}

#endif // _WIN32
