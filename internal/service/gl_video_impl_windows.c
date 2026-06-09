// gl_video_impl_windows.c — WGL OpenGL overlay for video rendering on Windows.
// Creates a child HWND that covers the video widget area and renders RGBA
// frames as an OpenGL texture on a dedicated render thread.
// Uses only OpenGL 1.1 (fixed-function pipeline) — no extension loading needed.

#ifdef _WIN32

#define WIN32_LEAN_AND_MEAN
#include <windows.h>
#include <GL/gl.h>
#ifndef GL_CLAMP_TO_EDGE
#define GL_CLAMP_TO_EDGE 0x812F
#endif
#include <stdint.h>
#include <string.h>
#include <stdlib.h>
#include <stdio.h>
#include <stdatomic.h>

// WGL vsync extension (optional, loaded at runtime).
typedef BOOL (WINAPI *PFNWGLSWAPINTERVALEXTPROC)(int);
static PFNWGLSWAPINTERVALEXTPROC wgl_swap_interval = NULL;
static int g_vsync = 0; // 1 = vsync on, 0 = vsync off (set by gl_video_create)

// Forward declaration — defined by CGO export in gl_video_windows.go.
extern void goGLLog(char *msg, int level);

// ─────────────────────────────────────────────────────────────────────────────

static HWND    g_hwnd  = NULL;
static HDC     g_hdc   = NULL;
static HGLRC   g_ctx   = NULL;
static GLuint  g_tex   = 0;
static int     g_tex_w = 0, g_tex_h = 0;

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

// Stats (written / read on render thread only).
static long long g_submitted = 0, g_rendered = 0, g_fps_n = 0;
static double    g_fps_t0 = 0.0;

static double mono_sec(void) {
    LARGE_INTEGER f, c;
    QueryPerformanceFrequency(&f);
    QueryPerformanceCounter(&c);
    return (double)c.QuadPart / (double)f.QuadPart;
}

// Letterbox: compute NDC quad that preserves frame aspect ratio.
static void letterbox(int fw, int fh, int ww, int wh,
                      float *x0, float *y0, float *x1, float *y1) {
    float fa = (float)fw / (float)(fh ? fh : 1);
    float wa = (float)ww / (float)(wh ? wh : 1);
    if (fa > wa) { float s = wa/fa; *x0=-1;*x1=1;*y0=-s;*y1=s; }
    else         { float s = fa/wa; *x0=-s;*x1=s;*y0=-1;*y1=1; }
}

static void gl_render_frame(void) {
    // Take a local copy of the pending frame.
    int fw, fh, fs;
    uint8_t *tmp = NULL;
    EnterCriticalSection(&g_cs);
    if (g_ready && g_buf) {
        fw = g_fw; fh = g_fh; fs = g_fs;
        size_t sz = (size_t)fh * (size_t)fs;
        tmp = (uint8_t*)malloc(sz);
        if (tmp) { memcpy(tmp, g_buf, sz); }
        g_ready = 0;
    }
    LeaveCriticalSection(&g_cs);
    if (!tmp) return;

    // Upload to GL texture.
    glBindTexture(GL_TEXTURE_2D, g_tex);
    if (fw != g_tex_w || fh != g_tex_h) {
        glTexImage2D(GL_TEXTURE_2D, 0, GL_RGBA, fw, fh, 0, GL_RGBA, GL_UNSIGNED_BYTE, tmp);
        g_tex_w = fw; g_tex_h = fh;
    } else {
        glTexSubImage2D(GL_TEXTURE_2D, 0, 0, 0, fw, fh, GL_RGBA, GL_UNSIGNED_BYTE, tmp);
    }
    free(tmp);

    // Get current child-window size.
    RECT rc; GetClientRect(g_hwnd, &rc);
    int ww = rc.right - rc.left, wh = rc.bottom - rc.top;
    if (ww <= 0 || wh <= 0) return;
    glViewport(0, 0, ww, wh);

    glClearColor(0, 0, 0, 1);
    glClear(GL_COLOR_BUFFER_BIT);

    float x0, y0, x1, y1;
    letterbox(fw, fh, ww, wh, &x0, &y0, &x1, &y1);

    glMatrixMode(GL_PROJECTION); glLoadIdentity();
    glMatrixMode(GL_MODELVIEW);  glLoadIdentity();
    glEnable(GL_TEXTURE_2D);
    glBindTexture(GL_TEXTURE_2D, g_tex);
    glColor4f(1, 1, 1, 1);
    glBegin(GL_QUADS);
        glTexCoord2f(0,1); glVertex2f(x0,y0);
        glTexCoord2f(1,1); glVertex2f(x1,y0);
        glTexCoord2f(1,0); glVertex2f(x1,y1);
        glTexCoord2f(0,0); glVertex2f(x0,y1);
    glEnd();
    glDisable(GL_TEXTURE_2D);
    SwapBuffers(g_hdc);

    // Stats (not spam — first frame + every 5 s).
    g_rendered++;
    g_fps_n++;
    double now = mono_sec();
    if (g_rendered == 1) {
        char m[128];
        snprintf(m, sizeof(m), "first frame rendered — %dx%d", fw, fh);
        goGLLog(m, 0);
        g_fps_t0 = now; g_fps_n = 0;
    }
    double el = now - g_fps_t0;
    if (el >= 5.0 && g_fps_n > 0) {
        char m[192];
        snprintf(m, sizeof(m), "fps=%.1f  rendered=%lld  submitted=%lld  size=%dx%d",
                 (double)g_fps_n / el, g_rendered, g_submitted, fw, fh);
        goGLLog(m, 0);
        g_fps_t0 = now; g_fps_n = 0;
    }
}

static DWORD WINAPI render_thread_fn(LPVOID unused) {
    (void)unused;
    if (!wglMakeCurrent(g_hdc, g_ctx)) {
        goGLLog("render_thread: wglMakeCurrent failed", 2);
        return 1;
    }
    // Load vsync extension here (context is current on this thread only).
    wgl_swap_interval = (PFNWGLSWAPINTERVALEXTPROC)wglGetProcAddress("wglSwapIntervalEXT");
    if (wgl_swap_interval) wgl_swap_interval(g_vsync ? 1 : 0);
    // Initialise texture on this thread (GL context is current here).
    glGenTextures(1, &g_tex);
    glBindTexture(GL_TEXTURE_2D, g_tex);
    glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_MIN_FILTER, GL_LINEAR);
    glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_MAG_FILTER, GL_LINEAR);
    glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_WRAP_S, GL_CLAMP_TO_EDGE);
    glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_WRAP_T, GL_CLAMP_TO_EDGE);
    goGLLog(g_vsync ? "render thread started (vsync on)" : "render thread started (vsync off)", 0);

    while (atomic_load(&g_active)) {
        DWORD r = WaitForSingleObject(g_event, 16);
        if (!atomic_load(&g_active)) break;
        if ((r == WAIT_OBJECT_0 || r == WAIT_TIMEOUT) && g_ready)
            gl_render_frame();
    }
    if (g_tex) { glDeleteTextures(1, &g_tex); g_tex = 0; }
    wglMakeCurrent(NULL, NULL);
    return 0;
}

static LRESULT CALLBACK gl_wndproc(HWND h, UINT m, WPARAM w, LPARAM l) {
    if (m == WM_ERASEBKGND) return 1;
    if (m == WM_PAINT) { PAINTSTRUCT ps; BeginPaint(h,&ps); EndPaint(h,&ps); return 0; }
    return DefWindowProcW(h, m, w, l);
}

static const wchar_t *WCLASS = L"USBGLVideo";
static int ensure_class(void) {
    static int done = 0;
    if (done) return 1;
    WNDCLASSEXW wc = {0};
    wc.cbSize=sizeof(wc); wc.style=CS_OWNDC;
    wc.lpfnWndProc=gl_wndproc;
    wc.hInstance=GetModuleHandleW(NULL);
    wc.lpszClassName=WCLASS;
    wc.hbrBackground=(HBRUSH)GetStockObject(BLACK_BRUSH);
    if (!RegisterClassExW(&wc) && GetLastError()!=ERROR_CLASS_ALREADY_EXISTS) return 0;
    done = 1; return 1;
}

// ─────────────────────────────────────────────────────────────────────────────
// Public C API (called from gl_video_windows.go via CGO extern declarations)
// ─────────────────────────────────────────────────────────────────────────────

int gl_video_is_active(void) { return atomic_load(&g_active); }

// Returns 1 if GL accepted the frame; 0 if overlay not yet active.
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
        g_fw=width; g_fh=height; g_fs=stride;
        g_ready=1; g_submitted++;
    }
    LeaveCriticalSection(&g_cs);
    SetEvent(g_event);
    return 1;
}

// x,y,w,h are in Windows client pixels (already DPI-scaled by caller).
// Pass w<=0 or h<=0 to cover the entire parent client area (fullscreen mode).
int gl_video_create(uintptr_t parent_hwnd, int x, int y, int w, int h, int vsync) {
    // Tear down any previous overlay.
    if (atomic_load(&g_active)) {
        atomic_store(&g_active, 0);
        if (g_thread) { SetEvent(g_event); WaitForSingleObject(g_thread, 2000); CloseHandle(g_thread); g_thread=NULL; }
        if (g_event)  { CloseHandle(g_event); g_event=NULL; }
        if (g_ctx)    { wglDeleteContext(g_ctx); g_ctx=NULL; }
        if (g_hdc && g_hwnd) { ReleaseDC(g_hwnd, g_hdc); g_hdc=NULL; }
        if (g_hwnd)   { DestroyWindow(g_hwnd); g_hwnd=NULL; }
        if (g_cs_init){ DeleteCriticalSection(&g_cs); g_cs_init=0; }
        if (g_buf)    { free(g_buf); g_buf=NULL; g_buf_sz=0; }
    }

    g_vsync = vsync;
    HWND parent = (HWND)(uintptr_t)parent_hwnd;
    if (!ensure_class()) { goGLLog("gl_video_create: RegisterClass failed", 2); return 0; }

    int fullwin = (w <= 0 || h <= 0);
    if (fullwin) {
        RECT rc; GetClientRect(parent, &rc);
        x=0; y=0; w=rc.right-rc.left; h=rc.bottom-rc.top;
    }

    g_hwnd = CreateWindowExW(0, WCLASS, L"", WS_CHILD|WS_VISIBLE,
                              x, y, w, h, parent, NULL, GetModuleHandleW(NULL), NULL);
    if (!g_hwnd) { goGLLog("gl_video_create: CreateWindowEx failed", 2); return 0; }

    g_hdc = GetDC(g_hwnd);
    if (!g_hdc) { goGLLog("gl_video_create: GetDC failed", 2); DestroyWindow(g_hwnd); g_hwnd=NULL; return 0; }

    PIXELFORMATDESCRIPTOR pfd = {sizeof(pfd),1,
        PFD_DRAW_TO_WINDOW|PFD_SUPPORT_OPENGL|PFD_DOUBLEBUFFER,
        PFD_TYPE_RGBA, 24, 0,0,0,0,0,0,0,0,0,0,0,0,0, 16, 0,0, PFD_MAIN_PLANE, 0,0,0,0};
    int fmt = ChoosePixelFormat(g_hdc, &pfd);
    if (!fmt || !SetPixelFormat(g_hdc, fmt, &pfd)) {
        goGLLog("gl_video_create: SetPixelFormat failed", 2);
        ReleaseDC(g_hwnd,g_hdc); g_hdc=NULL; DestroyWindow(g_hwnd); g_hwnd=NULL; return 0;
    }

    g_ctx = wglCreateContext(g_hdc);
    if (!g_ctx) {
        goGLLog("gl_video_create: wglCreateContext failed", 2);
        ReleaseDC(g_hwnd,g_hdc); g_hdc=NULL; DestroyWindow(g_hwnd); g_hwnd=NULL; return 0;
    }
    // NOTE: Do NOT call wglMakeCurrent here. This function is invoked from
    // Fyne's RunNative callback which runs on Fyne's GL thread. Calling
    // wglMakeCurrent(NULL, NULL) would unbind Fyne's own context, causing
    // the next Fyne draw call to fail. Vsync extension is loaded in the
    // render thread instead, after it makes its own context current.
    wgl_swap_interval = NULL;

    InitializeCriticalSection(&g_cs); g_cs_init = 1;
    g_event = CreateEventW(NULL, FALSE, FALSE, NULL);
    if (!g_event) {
        goGLLog("gl_video_create: CreateEvent failed", 2);
        wglDeleteContext(g_ctx); g_ctx=NULL;
        ReleaseDC(g_hwnd,g_hdc); g_hdc=NULL; DestroyWindow(g_hwnd); g_hwnd=NULL;
        DeleteCriticalSection(&g_cs); g_cs_init=0; return 0;
    }

    g_submitted=0; g_rendered=0; g_fps_n=0; g_fps_t0=0;
    g_ready=0; g_tex=0; g_tex_w=0; g_tex_h=0;
    atomic_store(&g_active, 1);

    g_thread = CreateThread(NULL, 0, render_thread_fn, NULL, 0, NULL);
    if (!g_thread) {
        goGLLog("gl_video_create: CreateThread failed", 2);
        atomic_store(&g_active, 0);
        CloseHandle(g_event); g_event=NULL;
        wglDeleteContext(g_ctx); g_ctx=NULL;
        ReleaseDC(g_hwnd,g_hdc); g_hdc=NULL; DestroyWindow(g_hwnd); g_hwnd=NULL;
        DeleteCriticalSection(&g_cs); g_cs_init=0; return 0;
    }

    char m[192];
    if (fullwin)
        snprintf(m,sizeof(m),"overlay created (full-window %dx%d) HWND=%p", w, h, (void*)parent_hwnd);
    else
        snprintf(m,sizeof(m),"overlay created %dx%d at (%d,%d) HWND=%p", w, h, x, y, (void*)parent_hwnd);
    goGLLog(m, 0);
    return 1;
}

// x,y,w,h are in Windows client pixels.
void gl_video_update_frame(int x, int y, int w, int h) {
    if (!atomic_load(&g_active) || !g_hwnd) return;
    SetWindowPos(g_hwnd, NULL, x, y, w, h, SWP_NOZORDER|SWP_NOACTIVATE);
}

void gl_video_destroy(void) {
    if (!atomic_load(&g_active)) return;
    atomic_store(&g_active, 0);
    if (g_thread) { SetEvent(g_event); WaitForSingleObject(g_thread, 2000); CloseHandle(g_thread); g_thread=NULL; }
    if (g_event)  { CloseHandle(g_event); g_event=NULL; }
    if (g_ctx)    { wglDeleteContext(g_ctx); g_ctx=NULL; }
    if (g_hdc && g_hwnd) { ReleaseDC(g_hwnd,g_hdc); g_hdc=NULL; }
    if (g_hwnd)   { DestroyWindow(g_hwnd); g_hwnd=NULL; }
    if (g_cs_init){ DeleteCriticalSection(&g_cs); g_cs_init=0; }
    if (g_buf)    { free(g_buf); g_buf=NULL; g_buf_sz=0; }
    char m[192];
    snprintf(m,sizeof(m),"overlay destroyed — rendered=%lld  submitted=%lld", g_rendered, g_submitted);
    goGLLog(m, 0);
}

#endif // _WIN32
