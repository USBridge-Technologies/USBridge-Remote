// gl_video_impl_windows.c — GDI parent-DC video renderer for Windows.
//
// Architecture:
//   • No child HWND — draw directly into the parent window's GDI DC.
//   • Render thread paints at ~60 fps CONTINUOUSLY (even without new frames),
//     so any DWM surface clear is recovered within one frame (~16 ms).
//   • videoCanvas is Hidden on the Go side → Fyne never paints over the video area.
//   • Atomic video-rect updated by Go via gl_video_update_frame (atomic stores only,
//     no Win32 API calls — safe to call from fyne.Do / CGO context).
//   • No PostMessage, no child HWND, no WGL — zero deadlock risk.

#ifdef _WIN32

#define WIN32_LEAN_AND_MEAN
#include <windows.h>
#include <stdint.h>
#include <string.h>
#include <stdlib.h>
#include <stdio.h>
#include <stdatomic.h>

// goGLLog — safe only from CGO context (gl_video_create/destroy).
// Never call from the render thread (native CreateThread).
extern void goGLLog(char *msg, int level);

// ─────────────────────────────────────────────────────────────────────────────
// State

static HWND g_hwnd = NULL;   // parent Fyne/GLFW HWND; we hold GetDC() on it
static HDC  g_hdc  = NULL;   // parent window DC — released on destroy

// Video rect in parent client pixels (set by gl_video_update_frame, read by render thread).
static atomic_int g_dst_x, g_dst_y, g_dst_w, g_dst_h;

// DIB section — BGRA pixels (AV_PIX_FMT_BGRA from sws_scale, matches BI_RGB/32-bit).
static HBITMAP  g_bmp      = NULL;
static uint8_t *g_bmp_bits = NULL;
static int      g_bmp_w    = 0, g_bmp_h = 0;

// Cached memory DC (avoids CreateCompatibleDC per frame).
static HDC     g_memDC     = NULL;
static HBITMAP g_memDC_old = NULL;

static volatile atomic_int g_active;

// Set to 1 once the first frame bitmap is ready; render thread skips blit before that.
static volatile int g_has_frame = 0;

// Pending frame — protected by g_cs.
static uint8_t *g_buf    = NULL;
static size_t   g_buf_sz = 0;
static int      g_fw = 0, g_fh = 0, g_fs = 0;
static volatile int g_ready = 0;

static CRITICAL_SECTION g_cs;
static int              g_cs_init = 0;
static HANDLE           g_thread  = NULL;
static HANDLE           g_event   = NULL;

// Stats (written by render thread, read by Go via polling).
static volatile long long g_submitted = 0, g_rendered = 0;
static volatile long long g_fps_n = 0;
static volatile double    g_fps_t0 = 0.0;
static volatile long long g_stat_rendered  = 0;
static volatile long long g_stat_submitted = 0;
static volatile float     g_stat_fps       = 0.0f;
static volatile int       g_stat_fps_ready = 0;
static volatile int       g_stat_first     = 0;
static volatile int       g_stat_fw = 0, g_stat_fh = 0;

// ─────────────────────────────────────────────────────────────────────────────

static double mono_sec(void) {
    LARGE_INTEGER f, c;
    QueryPerformanceFrequency(&f);
    QueryPerformanceCounter(&c);
    return (double)c.QuadPart / (double)f.QuadPart;
}

static void gdi_ensure_bmp(int w, int h) {
    if (g_bmp && g_bmp_w == w && g_bmp_h == h) return;
    if (g_memDC && g_memDC_old) { SelectObject(g_memDC, g_memDC_old); g_memDC_old = NULL; }
    if (g_bmp) { DeleteObject(g_bmp); g_bmp = NULL; g_bmp_bits = NULL; }

    BITMAPINFO bmi = {0};
    bmi.bmiHeader.biSize        = sizeof(BITMAPINFOHEADER);
    bmi.bmiHeader.biWidth       = w;
    bmi.bmiHeader.biHeight      = -h; // top-down
    bmi.bmiHeader.biPlanes      = 1;
    bmi.bmiHeader.biBitCount    = 32;
    bmi.bmiHeader.biCompression = BI_RGB; // 32-bit BGRA, A ignored by GDI

    HDC screen = GetDC(NULL);
    g_bmp = CreateDIBSection(screen, &bmi, DIB_RGB_COLORS, (void**)&g_bmp_bits, NULL, 0);
    ReleaseDC(NULL, screen);

    if (g_bmp) {
        g_bmp_w = w; g_bmp_h = h;
        if (!g_memDC) g_memDC = CreateCompatibleDC(g_hdc);
        if (g_memDC)  g_memDC_old = (HBITMAP)SelectObject(g_memDC, g_bmp);
    }
}

// gdi_render_frame — always called at ~60fps by the render thread.
// If a new frame is pending, the bitmap is updated first.
// The blit runs regardless, so the GDI surface is refreshed every ~16ms —
// any DWM surface clear is recovered within one frame.
static void gdi_render_frame(void) {
    // Consume new frame if available.
    int fw = 0, fh = 0, fs = 0;
    uint8_t *tmp = NULL;

    EnterCriticalSection(&g_cs);
    if (g_ready && g_buf) {
        fw = g_fw; fh = g_fh; fs = g_fs;
        size_t sz = (size_t)fh * (size_t)fs;
        tmp = (uint8_t*)malloc(sz);
        if (tmp) memcpy(tmp, g_buf, sz);
        g_ready = 0;
    }
    LeaveCriticalSection(&g_cs);

    if (tmp) {
        // Update cached bitmap from the new frame.
        gdi_ensure_bmp(fw, fh);
        if (g_bmp && g_bmp_bits) {
            size_t row = (size_t)fw * 4;
            for (int y = 0; y < fh; y++)
                memcpy(g_bmp_bits + (size_t)y * row, tmp + (size_t)y * (size_t)fs, row);
            g_has_frame = 1;
        }
        free(tmp);
    }

    // Skip blit until we have at least one valid frame.
    if (!g_has_frame || !g_bmp || !g_memDC || !g_hdc) return;

    // Source dimensions are always the last decoded frame size.
    int sw = g_bmp_w, sh = g_bmp_h;

    // Read the current video rect (set by gl_video_update_frame).
    int vx = atomic_load(&g_dst_x);
    int vy = atomic_load(&g_dst_y);
    int ww = atomic_load(&g_dst_w);
    int wh = atomic_load(&g_dst_h);
    if (ww <= 0 || wh <= 0) {
        // Fallback: full parent client area.
        RECT rc; GetClientRect(g_hwnd, &rc);
        vx = 0; vy = 0;
        ww = rc.right - rc.left; wh = rc.bottom - rc.top;
    }
    if (ww <= 0 || wh <= 0) return;

    // Letterbox within the video rect: keep frame aspect ratio.
    float fa = (float)sw / (float)(sh ? sh : 1);
    float wa = (float)ww / (float)(wh ? wh : 1);
    int dx = 0, dy = 0, dw = ww, dh = wh;
    if (fa > wa) { dh = (int)(ww / fa + 0.5f); dy = (wh - dh) / 2; }
    else         { dw = (int)(wh * fa + 0.5f); dx = (ww - dw) / 2; }
    dx += vx; dy += vy;

    // Fill black bars around the video within the video rect.
    HBRUSH blk = (HBRUSH)GetStockObject(BLACK_BRUSH);
    if (dy > vy) {
        RECT top = {vx, vy, vx+ww, dy};          FillRect(g_hdc, &top, blk);
        RECT bot = {vx, dy+dh, vx+ww, vy+wh};    FillRect(g_hdc, &bot, blk);
    }
    if (dx > vx) {
        RECT lft = {vx, dy, dx, dy+dh};           FillRect(g_hdc, &lft, blk);
        RECT rgt = {dx+dw, dy, vx+ww, dy+dh};     FillRect(g_hdc, &rgt, blk);
    }

    SetStretchBltMode(g_hdc, HALFTONE);
    SetBrushOrgEx(g_hdc, 0, 0, NULL);
    StretchBlt(g_hdc, dx, dy, dw, dh, g_memDC, 0, 0, sw, sh, SRCCOPY);

    // Count stats for new frames only (not for cached repaints).
    if (tmp) {
        g_rendered++;
        g_fps_n++;
        g_stat_rendered  = g_rendered;
        g_stat_submitted = g_submitted;
        double now = mono_sec();
        if (g_rendered == 1) {
            g_stat_first = 1; g_stat_fw = sw; g_stat_fh = sh;
            g_fps_t0 = now; g_fps_n = 0;
        }
        double el = now - g_fps_t0;
        if (el >= 5.0 && g_fps_n > 0) {
            g_stat_fps       = (float)((double)g_fps_n / el);
            g_stat_fps_ready = 1;
            g_fps_t0 = now; g_fps_n = 0;
        }
    }
}

// Render thread: ticks every 16 ms (~60 fps) and always repaints.
// The continuous repaint ensures GDI surface is never stale for more than ~16 ms,
// masking any DWM composition glitches that could briefly clear it.
static DWORD WINAPI render_thread_fn(LPVOID unused) {
    (void)unused;
    while (atomic_load(&g_active)) {
        WaitForSingleObject(g_event, 16); // woken early by new frame, or timeout at 16 ms
        if (!atomic_load(&g_active)) break;
        gdi_render_frame();
    }
    return 0;
}

// ─────────────────────────────────────────────────────────────────────────────
// Public C API

int gl_video_is_active(void) { return atomic_load(&g_active); }

int gl_video_try_submit(uint8_t *rgba, int width, int height, int stride) {
    if (!atomic_load(&g_active)) return 0;
    size_t sz = (size_t)height * (size_t)stride;
    if (!g_cs_init) return 0;
    EnterCriticalSection(&g_cs);
    if (!g_buf || g_buf_sz < sz) {
        free(g_buf);
        g_buf    = (uint8_t*)malloc(sz);
        g_buf_sz = g_buf ? sz : 0;
    }
    if (g_buf) {
        memcpy(g_buf, rgba, sz);
        g_fw = width; g_fh = height; g_fs = stride;
        g_ready = 1; g_submitted++;
    }
    LeaveCriticalSection(&g_cs);
    SetEvent(g_event); // wake render thread immediately
    return 1;
}

// x,y,w,h — video canvas rect in parent client pixels.
// Safe to call from fyne.Do (CGO context) — only stores atomics, no Win32 calls.
int gl_video_create(uintptr_t parent_hwnd, int x, int y, int w, int h, int vsync) {
    (void)vsync; // no swap interval in GDI path
    if (atomic_load(&g_active)) {
        atomic_store(&g_active, 0);
        if (g_thread)  { SetEvent(g_event); WaitForSingleObject(g_thread, 2000); CloseHandle(g_thread); g_thread=NULL; }
        if (g_event)   { CloseHandle(g_event); g_event=NULL; }
        if (g_memDC)   { if (g_memDC_old) SelectObject(g_memDC, g_memDC_old); DeleteDC(g_memDC); g_memDC=NULL; g_memDC_old=NULL; }
        if (g_bmp)     { DeleteObject(g_bmp); g_bmp=NULL; g_bmp_bits=NULL; }
        if (g_hdc)     { ReleaseDC(g_hwnd, g_hdc); g_hdc=NULL; }
        g_hwnd = NULL;
        if (g_cs_init) { DeleteCriticalSection(&g_cs); g_cs_init=0; }
        if (g_buf)     { free(g_buf); g_buf=NULL; g_buf_sz=0; }
        g_bmp_w=0; g_bmp_h=0; g_has_frame=0;
    }

    HWND parent = (HWND)(uintptr_t)parent_hwnd;
    if (!parent) { goGLLog("gl_video_create: parent HWND is null", 2); return 0; }

    g_hwnd = parent;
    g_hdc  = GetDC(parent);
    if (!g_hdc) { goGLLog("gl_video_create: GetDC(parent) failed", 2); g_hwnd=NULL; return 0; }

    // Store initial video rect.
    atomic_store(&g_dst_x, x);
    atomic_store(&g_dst_y, y);
    atomic_store(&g_dst_w, w);
    atomic_store(&g_dst_h, h);

    InitializeCriticalSection(&g_cs); g_cs_init = 1;
    g_event = CreateEventW(NULL, FALSE, FALSE, NULL);
    if (!g_event) {
        goGLLog("gl_video_create: CreateEvent failed", 2);
        ReleaseDC(g_hwnd, g_hdc); g_hdc=NULL; g_hwnd=NULL;
        DeleteCriticalSection(&g_cs); g_cs_init=0; return 0;
    }

    g_submitted=0; g_rendered=0; g_fps_n=0; g_fps_t0=0;
    g_ready=0; g_has_frame=0; g_stat_first=0; g_stat_fw=0; g_stat_fh=0;
    atomic_store(&g_active, 1);

    g_thread = CreateThread(NULL, 0, render_thread_fn, NULL, 0, NULL);
    if (!g_thread) {
        goGLLog("gl_video_create: CreateThread failed", 2);
        atomic_store(&g_active, 0);
        CloseHandle(g_event); g_event=NULL;
        ReleaseDC(g_hwnd, g_hdc); g_hdc=NULL; g_hwnd=NULL;
        DeleteCriticalSection(&g_cs); g_cs_init=0; return 0;
    }

    char m[256];
    snprintf(m, sizeof(m),
             "GDI parent-DC renderer created — rect=(%d,%d,%dx%d) parent=%p",
             x, y, w, h, (void*)parent_hwnd);
    goGLLog(m, 0);
    return 1;
}

// gl_video_update_frame — safe to call from fyne.Do (CGO context).
// Only stores atomics; no Win32 API calls, no risk of deadlock.
void gl_video_update_frame(int x, int y, int w, int h) {
    if (!atomic_load(&g_active)) return;
    atomic_store(&g_dst_x, x);
    atomic_store(&g_dst_y, y);
    atomic_store(&g_dst_w, w);
    atomic_store(&g_dst_h, h);
}

void gl_video_destroy(void) {
    if (!atomic_load(&g_active)) return;
    atomic_store(&g_active, 0);
    if (g_thread)  { SetEvent(g_event); WaitForSingleObject(g_thread, 2000); CloseHandle(g_thread); g_thread=NULL; }
    if (g_event)   { CloseHandle(g_event); g_event=NULL; }
    if (g_memDC)   { if (g_memDC_old) SelectObject(g_memDC, g_memDC_old); DeleteDC(g_memDC); g_memDC=NULL; g_memDC_old=NULL; }
    if (g_bmp)     { DeleteObject(g_bmp); g_bmp=NULL; g_bmp_bits=NULL; }
    if (g_hdc)     { ReleaseDC(g_hwnd, g_hdc); g_hdc=NULL; }
    g_hwnd = NULL;
    if (g_cs_init) { DeleteCriticalSection(&g_cs); g_cs_init=0; }
    if (g_buf)     { free(g_buf); g_buf=NULL; g_buf_sz=0; }
    g_bmp_w=0; g_bmp_h=0; g_has_frame=0;
    char m[192];
    snprintf(m, sizeof(m), "GDI parent-DC renderer destroyed — rendered=%lld submitted=%lld",
             g_rendered, g_submitted);
    goGLLog(m, 0);
}

void gl_video_get_stats(long long *rendered, long long *submitted,
                        float *fps, int *fps_ready,
                        int *first_frame, int *fw, int *fh) {
    *rendered    = g_stat_rendered;
    *submitted   = g_stat_submitted;
    *fps         = g_stat_fps;
    *fps_ready   = g_stat_fps_ready;
    *first_frame = g_stat_first;
    *fw          = g_stat_fw;
    *fh          = g_stat_fh;
}

void gl_video_clear_pending_stats(void) {
    g_stat_fps_ready = 0;
    g_stat_first     = 0;
}

#endif // _WIN32
