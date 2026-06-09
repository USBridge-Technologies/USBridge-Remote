// gl_video_impl_windows.c — GDI child-window video renderer for Windows.
//
// Architecture:
//   • Child HWND (WS_CHILD, CS_OWNDC, no WGL) created on the Fyne main thread.
//   • Fyne's GLFW event loop pumps the child's messages (same OS thread) —
//     no custom PostMessage, no unprocessed queue.
//   • Render thread: StretchBlt to permanent child DC (CS_OWNDC).
//   • gl_video_update_frame: SetWindowPos from main thread (CGO from fyne.Do).
//   • Zero deadlock risk: child has no WGL context, no DWM swap-chain conflict.

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

static HWND g_hwnd     = NULL;  // parent Fyne/GLFW HWND
static HWND g_child    = NULL;  // our video child window
static HDC  g_child_dc = NULL;  // permanent DC (CS_OWNDC — never released until destroy)

// Child size in client pixels — set by main thread, read by render thread for letterboxing.
static atomic_int g_dst_w, g_dst_h;

// DIB section — BGRA pixels (AV_PIX_FMT_BGRA from sws_scale).
static HBITMAP  g_bmp      = NULL;
static uint8_t *g_bmp_bits = NULL;
static int      g_bmp_w    = 0, g_bmp_h = 0;

// Cached memory DC (avoids CreateCompatibleDC per frame).
static HDC     g_memDC     = NULL;
static HBITMAP g_memDC_old = NULL;

static volatile atomic_int g_active;

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

// Custom message: reposition child window.
// PostMessage'd by gl_video_update_frame (non-blocking); processed by GLFW's PeekMessage loop.
// WPARAM = MAKEWPARAM(x, y),  LPARAM = MAKELPARAM(w, h)
#define WM_VIDEO_SETPOS (WM_APP + 1)

// WndProc for child window.  Suppresses background erase (render thread paints).
static LRESULT CALLBACK ChildWndProc(HWND hwnd, UINT msg, WPARAM wp, LPARAM lp) {
    if (msg == WM_ERASEBKGND) return 1;
    if (msg == WM_PAINT) {
        PAINTSTRUCT ps;
        BeginPaint(hwnd, &ps);
        EndPaint(hwnd, &ps);
        return 0;
    }
    if (msg == WM_VIDEO_SETPOS) {
        int x = (int)LOWORD(wp), y = (int)HIWORD(wp);
        int w = (int)LOWORD(lp), h = (int)HIWORD(lp);
        if (w < 1) w = 1; if (h < 1) h = 1;
        MoveWindow(hwnd, x, y, w, h, FALSE);
        return 0;
    }
    return DefWindowProc(hwnd, msg, wp, lp);
}

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
        if (!g_memDC) g_memDC = CreateCompatibleDC(g_child_dc);
        if (g_memDC)  g_memDC_old = (HBITMAP)SelectObject(g_memDC, g_bmp);
    }
}

static void gdi_render_frame(void) {
    int fw, fh, fs;
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
    if (!tmp) return;

    gdi_ensure_bmp(fw, fh);
    if (g_bmp && g_bmp_bits) {
        size_t row = (size_t)fw * 4;
        for (int y = 0; y < fh; y++)
            memcpy(g_bmp_bits + (size_t)y * row, tmp + (size_t)y * (size_t)fs, row);
    }
    free(tmp);

    if (!g_bmp || !g_memDC || !g_child_dc) return;

    // Child window size — this IS the video rect; coords are child-relative (origin 0,0).
    int ww = atomic_load(&g_dst_w);
    int wh = atomic_load(&g_dst_h);
    if (ww <= 0 || wh <= 0) {
        RECT rc; GetClientRect(g_child, &rc);
        ww = rc.right; wh = rc.bottom;
    }
    if (ww <= 0 || wh <= 0) return;

    // Letterbox: keep frame aspect ratio within the child area.
    float fa = (float)fw / (float)(fh ? fh : 1);
    float wa = (float)ww / (float)(wh ? wh : 1);
    int dx = 0, dy = 0, dw = ww, dh = wh;
    if (fa > wa) { dh = (int)(ww / fa + 0.5f); dy = (wh - dh) / 2; }
    else         { dw = (int)(wh * fa + 0.5f); dx = (ww - dw) / 2; }

    // Fill black bars (child-relative coordinates).
    HBRUSH blk = (HBRUSH)GetStockObject(BLACK_BRUSH);
    if (dy > 0) {
        RECT top = {0, 0, ww, dy};       FillRect(g_child_dc, &top, blk);
        RECT bot = {0, dy+dh, ww, wh};   FillRect(g_child_dc, &bot, blk);
    }
    if (dx > 0) {
        RECT lft = {0, dy, dx, dy+dh};       FillRect(g_child_dc, &lft, blk);
        RECT rgt = {dx+dw, dy, ww, dy+dh};   FillRect(g_child_dc, &rgt, blk);
    }

    SetStretchBltMode(g_child_dc, HALFTONE);
    SetBrushOrgEx(g_child_dc, 0, 0, NULL);
    StretchBlt(g_child_dc, dx, dy, dw, dh, g_memDC, 0, 0, fw, fh, SRCCOPY);

    // Validate dirty region so Windows doesn't keep requesting WM_PAINT.
    ValidateRect(g_child, NULL);

    // Stats (no goGLLog — render thread is not a CGO thread).
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
}

static DWORD WINAPI render_thread_fn(LPVOID unused) {
    (void)unused;
    while (atomic_load(&g_active)) {
        DWORD r = WaitForSingleObject(g_event, 16);
        if (!atomic_load(&g_active)) break;
        if ((r == WAIT_OBJECT_0 || r == WAIT_TIMEOUT) && g_ready)
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
    SetEvent(g_event);
    return 1;
}

// gl_video_create — MUST be called from the Fyne main OS thread (via RunNative).
// The child HWND is created on the calling thread; GLFW's event loop then pumps its messages.
int gl_video_create(uintptr_t parent_hwnd, int x, int y, int w, int h, int vsync) {
    (void)vsync;
    if (atomic_load(&g_active)) {
        atomic_store(&g_active, 0);
        if (g_thread)   { SetEvent(g_event); WaitForSingleObject(g_thread, 2000); CloseHandle(g_thread); g_thread=NULL; }
        if (g_event)    { CloseHandle(g_event); g_event=NULL; }
        if (g_memDC)    { if (g_memDC_old) SelectObject(g_memDC, g_memDC_old); DeleteDC(g_memDC); g_memDC=NULL; g_memDC_old=NULL; }
        if (g_bmp)      { DeleteObject(g_bmp); g_bmp=NULL; g_bmp_bits=NULL; }
        if (g_child_dc) { ReleaseDC(g_child, g_child_dc); g_child_dc=NULL; }
        if (g_child)    { DestroyWindow(g_child); g_child=NULL; }
        g_hwnd = NULL;
        if (g_cs_init)  { DeleteCriticalSection(&g_cs); g_cs_init=0; }
        if (g_buf)      { free(g_buf); g_buf=NULL; g_buf_sz=0; }
        g_bmp_w=0; g_bmp_h=0;
    }

    HWND parent = (HWND)(uintptr_t)parent_hwnd;
    if (!parent) { goGLLog("gl_video_create: parent HWND is null", 2); return 0; }
    g_hwnd = parent;

    // Register window class (ignore "already registered" on repeated create/destroy).
    WNDCLASSEXW wc = {0};
    wc.cbSize        = sizeof(wc);
    wc.style         = CS_OWNDC | CS_HREDRAW | CS_VREDRAW;
    wc.lpfnWndProc   = ChildWndProc;
    wc.hInstance     = GetModuleHandleW(NULL);
    wc.hbrBackground = (HBRUSH)GetStockObject(BLACK_BRUSH);
    wc.lpszClassName = L"USBridgeVideoChild";
    RegisterClassExW(&wc);

    int cw = w > 0 ? w : 16, ch = h > 0 ? h : 16;
    g_child = CreateWindowExW(
        0, L"USBridgeVideoChild", L"",
        WS_CHILD | WS_VISIBLE | WS_CLIPSIBLINGS,
        x, y, cw, ch,
        parent, NULL, GetModuleHandleW(NULL), NULL
    );
    if (!g_child) {
        char m[256];
        snprintf(m, sizeof(m), "gl_video_create: CreateWindowEx failed err=%lu", GetLastError());
        goGLLog(m, 2); g_hwnd=NULL; return 0;
    }

    // CS_OWNDC: GetDC returns the window's permanent DC — valid for the window's lifetime.
    g_child_dc = GetDC(g_child);
    if (!g_child_dc) {
        goGLLog("gl_video_create: GetDC(child) failed", 2);
        DestroyWindow(g_child); g_child=NULL; g_hwnd=NULL; return 0;
    }

    atomic_store(&g_dst_w, w);
    atomic_store(&g_dst_h, h);

    InitializeCriticalSection(&g_cs); g_cs_init = 1;
    g_event = CreateEventW(NULL, FALSE, FALSE, NULL);
    if (!g_event) {
        goGLLog("gl_video_create: CreateEvent failed", 2);
        ReleaseDC(g_child, g_child_dc); g_child_dc=NULL;
        DestroyWindow(g_child); g_child=NULL; g_hwnd=NULL;
        DeleteCriticalSection(&g_cs); g_cs_init=0; return 0;
    }

    g_submitted=0; g_rendered=0; g_fps_n=0; g_fps_t0=0;
    g_ready=0; g_stat_first=0; g_stat_fw=0; g_stat_fh=0;
    atomic_store(&g_active, 1);

    g_thread = CreateThread(NULL, 0, render_thread_fn, NULL, 0, NULL);
    if (!g_thread) {
        goGLLog("gl_video_create: CreateThread failed", 2);
        atomic_store(&g_active, 0);
        CloseHandle(g_event); g_event=NULL;
        ReleaseDC(g_child, g_child_dc); g_child_dc=NULL;
        DestroyWindow(g_child); g_child=NULL; g_hwnd=NULL;
        DeleteCriticalSection(&g_cs); g_cs_init=0; return 0;
    }

    char m[256];
    snprintf(m, sizeof(m),
             "GDI child-window renderer created — rect=(%d,%d,%dx%d) parent=%p child=%p",
             x, y, w, h, (void*)parent_hwnd, (void*)g_child);
    goGLLog(m, 0);
    return 1;
}

// gl_video_update_frame — safe to call from fyne.Do (main goroutine).
// Updates letterbox size via atomics, then posts WM_VIDEO_SETPOS to the child.
// PostMessage is non-blocking — MoveWindow is deferred to GLFW's PeekMessage loop,
// so no synchronous Win32 window management runs inside a GL rendering callback.
void gl_video_update_frame(int x, int y, int w, int h) {
    if (!atomic_load(&g_active)) return;
    atomic_store(&g_dst_w, w > 0 ? w : 0);
    atomic_store(&g_dst_h, h > 0 ? h : 0);
    if (g_child) {
        PostMessageW(g_child, WM_VIDEO_SETPOS,
                     MAKEWPARAM((WORD)x, (WORD)y),
                     MAKELPARAM((WORD)(w > 0 ? w : 1), (WORD)(h > 0 ? h : 1)));
    }
}

// gl_video_destroy — MUST be called from the Fyne main OS thread (DestroyWindow requirement).
void gl_video_destroy(void) {
    if (!atomic_load(&g_active)) return;
    atomic_store(&g_active, 0);
    if (g_thread)   { SetEvent(g_event); WaitForSingleObject(g_thread, 2000); CloseHandle(g_thread); g_thread=NULL; }
    if (g_event)    { CloseHandle(g_event); g_event=NULL; }
    if (g_memDC)    { if (g_memDC_old) SelectObject(g_memDC, g_memDC_old); DeleteDC(g_memDC); g_memDC=NULL; g_memDC_old=NULL; }
    if (g_bmp)      { DeleteObject(g_bmp); g_bmp=NULL; g_bmp_bits=NULL; }
    if (g_child_dc) { ReleaseDC(g_child, g_child_dc); g_child_dc=NULL; }
    if (g_child)    { DestroyWindow(g_child); g_child=NULL; }
    g_hwnd = NULL;
    if (g_cs_init)  { DeleteCriticalSection(&g_cs); g_cs_init=0; }
    if (g_buf)      { free(g_buf); g_buf=NULL; g_buf_sz=0; }
    g_bmp_w=0; g_bmp_h=0;
    char m[192];
    snprintf(m, sizeof(m), "GDI child-window renderer destroyed — rendered=%lld submitted=%lld",
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
