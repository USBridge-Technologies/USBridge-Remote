// gl_video_impl_windows.c — GDI child-window video overlay for Windows.
//
// Architecture:
//   Parent HWND  — Fyne's GLFW window (OpenGL rendering, owned by Fyne).
//   Child HWND   — our overlay, WS_CHILD | WS_VISIBLE, positioned over the
//                  video canvas area (CS_OWNDC for a dedicated GDI device context).
//
// Why child HWND + GDI (not the parent DC, not WGL):
//   • Parent DC + GDI conflicts with Fyne's OpenGL SwapBuffers — the DWM
//     composites both layers but Fyne's SwapBuffers can overwrite GDI pixels,
//     causing the double-image ("наложенная трансляция") artefact.
//   • WGL on a child HWND + Fyne's parent-window SwapBuffers deadlock on
//     Win10+ via DWM cross-thread SendMessage.
//   • GDI on a child HWND is independent of Fyne's OpenGL surface.
//     StretchBlt goes through WDDM without any DWM swap-chain interaction,
//     and SetWindowPos on a GDI child is safe from the message-loop thread.

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
// Child window / GDI state

static HWND    g_hwnd    = NULL;   // child HWND, CS_OWNDC
static HDC     g_hdc     = NULL;   // child window DC, never released until DestroyWindow

// DIB section — BGRA pixels (matches AV_PIX_FMT_BGRA → BI_RGB/32-bit).
static HBITMAP  g_bmp      = NULL;
static uint8_t *g_bmp_bits = NULL;
static int      g_bmp_w    = 0, g_bmp_h = 0;

// Cached memory DC to avoid CreateCompatibleDC per frame.
static HDC      g_memDC     = NULL;
static HBITMAP  g_memDC_old = NULL;

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

// ── Stats (written by render thread, read by Go via polling) ─────────────────
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
    bmi.bmiHeader.biCompression = BI_RGB; // 32-bit BGRA, GDI ignores A byte

    HDC screen = GetDC(NULL);
    g_bmp = CreateDIBSection(screen, &bmi, DIB_RGB_COLORS, (void**)&g_bmp_bits, NULL, 0);
    ReleaseDC(NULL, screen);

    if (g_bmp) {
        g_bmp_w = w; g_bmp_h = h;
        if (!g_memDC) g_memDC = CreateCompatibleDC(g_hdc);
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

    // BGRA from sws_scale matches GDI BI_RGB/32-bit — straight memcpy per row.
    gdi_ensure_bmp(fw, fh);
    if (g_bmp && g_bmp_bits) {
        size_t row = (size_t)fw * 4;
        for (int y = 0; y < fh; y++)
            memcpy(g_bmp_bits + (size_t)y * row, tmp + (size_t)y * (size_t)fs, row);
    }
    free(tmp);

    if (!g_bmp || !g_memDC || !g_hdc) return;

    // Child window size = video canvas area; letterbox within it.
    RECT rc; GetClientRect(g_hwnd, &rc);
    int ww = rc.right - rc.left, wh = rc.bottom - rc.top;
    if (ww <= 0 || wh <= 0) return;

    float fa = (float)fw / (float)(fh ? fh : 1);
    float wa = (float)ww / (float)(wh ? wh : 1);
    int dx = 0, dy = 0, dw = ww, dh = wh;
    if (fa > wa) { dh = (int)(ww / fa + 0.5f); dy = (wh - dh) / 2; }
    else         { dw = (int)(wh * fa + 0.5f); dx = (ww - dw) / 2; }

    HBRUSH blk = (HBRUSH)GetStockObject(BLACK_BRUSH);
    if (dy > 0) {
        RECT top = {0, 0, ww, dy};       FillRect(g_hdc, &top, blk);
        RECT bot = {0, dy+dh, ww, wh};   FillRect(g_hdc, &bot, blk);
    }
    if (dx > 0) {
        RECT lft = {0, dy, dx, dy+dh};        FillRect(g_hdc, &lft, blk);
        RECT rgt = {dx+dw, dy, ww, dy+dh};    FillRect(g_hdc, &rgt, blk);
    }

    SetStretchBltMode(g_hdc, HALFTONE);
    SetBrushOrgEx(g_hdc, 0, 0, NULL);
    StretchBlt(g_hdc, dx, dy, dw, dh, g_memDC, 0, 0, fw, fh, SRCCOPY);

    // Stats (render thread — no goGLLog allowed here).
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
    // Pure GDI — no wglMakeCurrent, no SwapBuffers, no DWM swap-chain interaction.
    while (atomic_load(&g_active)) {
        DWORD r = WaitForSingleObject(g_event, 16);
        if (!atomic_load(&g_active)) break;
        if ((r == WAIT_OBJECT_0 || r == WAIT_TIMEOUT) && g_ready)
            gdi_render_frame();
    }
    return 0;
}

static LRESULT CALLBACK gl_wndproc(HWND h, UINT m, WPARAM w, LPARAM l) {
    if (m == WM_ERASEBKGND) return 1;
    if (m == WM_PAINT) { PAINTSTRUCT ps; BeginPaint(h,&ps); EndPaint(h,&ps); return 0; }
    return DefWindowProcW(h, m, w, l);
}

static const wchar_t *WCLASS = L"USBGDIVideo";
static int ensure_class(void) {
    static int done = 0;
    if (done) return 1;
    WNDCLASSEXW wc = {0};
    wc.cbSize = sizeof(wc); wc.style = CS_OWNDC;
    wc.lpfnWndProc = gl_wndproc;
    wc.hInstance = GetModuleHandleW(NULL);
    wc.lpszClassName = WCLASS;
    wc.hbrBackground = (HBRUSH)GetStockObject(BLACK_BRUSH);
    if (!RegisterClassExW(&wc) && GetLastError() != ERROR_CLASS_ALREADY_EXISTS) return 0;
    done = 1; return 1;
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
        g_buf = (uint8_t*)malloc(sz);
        g_buf_sz = (g_buf ? sz : 0);
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

// x,y,w,h in parent client pixels. Pass w<=0 or h<=0 for full-client-area mode.
int gl_video_create(uintptr_t parent_hwnd, int x, int y, int w, int h, int vsync) {
    (void)vsync;
    if (atomic_load(&g_active)) {
        atomic_store(&g_active, 0);
        if (g_thread)  { SetEvent(g_event); WaitForSingleObject(g_thread, 2000); CloseHandle(g_thread); g_thread=NULL; }
        if (g_event)   { CloseHandle(g_event); g_event=NULL; }
        if (g_memDC)   { if (g_memDC_old) SelectObject(g_memDC, g_memDC_old); DeleteDC(g_memDC); g_memDC=NULL; g_memDC_old=NULL; }
        if (g_bmp)     { DeleteObject(g_bmp); g_bmp=NULL; g_bmp_bits=NULL; }
        if (g_hwnd)    { DestroyWindow(g_hwnd); g_hwnd=NULL; g_hdc=NULL; }
        if (g_cs_init) { DeleteCriticalSection(&g_cs); g_cs_init=0; }
        if (g_buf)     { free(g_buf); g_buf=NULL; g_buf_sz=0; }
        g_bmp_w=0; g_bmp_h=0;
    }

    HWND parent = (HWND)(uintptr_t)parent_hwnd;
    if (!parent) { goGLLog("gl_video_create: parent HWND is null", 2); return 0; }
    if (!ensure_class()) { goGLLog("gl_video_create: RegisterClass failed", 2); return 0; }

    int fullwin = (w <= 0 || h <= 0);
    if (fullwin) {
        RECT rc; GetClientRect(parent, &rc);
        x=0; y=0; w=rc.right-rc.left; h=rc.bottom-rc.top;
    }

    g_hwnd = CreateWindowExW(0, WCLASS, L"", WS_CHILD|WS_VISIBLE,
                              x, y, w, h, parent, NULL, GetModuleHandleW(NULL), NULL);
    if (!g_hwnd) { goGLLog("gl_video_create: CreateWindowEx failed", 2); return 0; }
    g_hdc = GetDC(g_hwnd); // CS_OWNDC — kept for lifetime of g_hwnd

    InitializeCriticalSection(&g_cs); g_cs_init = 1;
    g_event = CreateEventW(NULL, FALSE, FALSE, NULL);
    if (!g_event) {
        goGLLog("gl_video_create: CreateEvent failed", 2);
        DestroyWindow(g_hwnd); g_hwnd=NULL; g_hdc=NULL;
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
        DestroyWindow(g_hwnd); g_hwnd=NULL; g_hdc=NULL;
        DeleteCriticalSection(&g_cs); g_cs_init=0; return 0;
    }

    char m[256];
    if (fullwin)
        snprintf(m, sizeof(m), "GDI child overlay created (full-window %dx%d) parent=%p", w, h, (void*)parent_hwnd);
    else
        snprintf(m, sizeof(m), "GDI child overlay created %dx%d at (%d,%d) parent=%p", w, h, x, y, (void*)parent_hwnd);
    goGLLog(m, 0);
    return 1;
}

// Reposition the child HWND to track the video canvas.
// Safe to call directly from the message-loop thread (fyne.Do) — GDI child
// windows do NOT interact with the DWM swap chain, so no deadlock risk.
void gl_video_update_frame(int x, int y, int w, int h) {
    if (!atomic_load(&g_active) || !g_hwnd) return;
    SetWindowPos(g_hwnd, NULL, x, y, w, h, SWP_NOZORDER|SWP_NOACTIVATE);
}

void gl_video_destroy(void) {
    if (!atomic_load(&g_active)) return;
    atomic_store(&g_active, 0);
    if (g_thread)  { SetEvent(g_event); WaitForSingleObject(g_thread, 2000); CloseHandle(g_thread); g_thread=NULL; }
    if (g_event)   { CloseHandle(g_event); g_event=NULL; }
    if (g_memDC)   { if (g_memDC_old) SelectObject(g_memDC, g_memDC_old); DeleteDC(g_memDC); g_memDC=NULL; g_memDC_old=NULL; }
    if (g_bmp)     { DeleteObject(g_bmp); g_bmp=NULL; g_bmp_bits=NULL; }
    if (g_hwnd)    { DestroyWindow(g_hwnd); g_hwnd=NULL; g_hdc=NULL; }
    if (g_cs_init) { DeleteCriticalSection(&g_cs); g_cs_init=0; }
    if (g_buf)     { free(g_buf); g_buf=NULL; g_buf_sz=0; }
    g_bmp_w=0; g_bmp_h=0;
    char m[192];
    snprintf(m, sizeof(m), "GDI child overlay destroyed — rendered=%lld submitted=%lld", g_rendered, g_submitted);
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
