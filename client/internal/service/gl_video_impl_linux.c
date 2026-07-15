// gl_video_impl_linux.c — GLX/X11 OpenGL overlay for video rendering on Linux.
// Creates a child X11 Window over the video widget area and renders RGBA
// frames as a GL texture on a dedicated render thread.
// Handles both X11 native windows and XWayland.

#if defined(__linux__) && !defined(__ANDROID__)

#include <X11/Xlib.h>
#include <X11/extensions/shape.h>
#include <GL/gl.h>
#include <GL/glx.h>
#include <pthread.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include <stdio.h>
#include <stdatomic.h>
#include <time.h>
#include <unistd.h>

// GLX vsync extensions (optional, loaded at runtime).
typedef void (*PFNGLXSWAPINTERVALEXTPROC)(Display*, GLXDrawable, int);
typedef int  (*PFNGLXSWAPINTERVALMESAPROC)(unsigned);
static PFNGLXSWAPINTERVALEXTPROC  glx_swap_ext  = NULL;
static PFNGLXSWAPINTERVALMESAPROC glx_swap_mesa = NULL;
static int g_vsync = 0;

// goGLLog is called only from gl_video_create/destroy (CGO context — safe).
// NEVER called from the render thread (native pthread — would deadlock Go GC).
extern void goGLLog(char *msg, int level);

// ─────────────────────────────────────────────────────────────────────────────

static Display   *g_dpy  = NULL;
static Window     g_win  = 0;
static GLXContext g_ctx  = NULL;
static GLuint     g_tex  = 0;
static int        g_tex_w = 0, g_tex_h = 0;

static volatile atomic_int g_active = 0;

static uint8_t *g_buf    = NULL;
static size_t   g_buf_sz = 0;
static int      g_fw=0, g_fh=0, g_fs=0;
static volatile int g_ready = 0;

static pthread_mutex_t g_mu     = PTHREAD_MUTEX_INITIALIZER;
static pthread_t       g_thread = 0;
static int             g_pipe_r = -1, g_pipe_w = -1;

static long long g_submitted=0, g_rendered=0, g_fps_n=0;
static double    g_fps_t0=0.0;

// Stats accessible from Go via gl_video_get_stats() — no CGO from render thread.
static volatile long long g_stat_rendered  = 0;
static volatile long long g_stat_submitted = 0;
static volatile float     g_stat_fps       = 0.0f;
static volatile int       g_stat_fps_ready = 0;
static volatile int       g_stat_first     = 0;
static volatile int       g_stat_fw = 0, g_stat_fh = 0;

static double mono_sec(void) {
    struct timespec ts;
    clock_gettime(CLOCK_MONOTONIC, &ts);
    return (double)ts.tv_sec + (double)ts.tv_nsec * 1e-9;
}

static void letterbox(int fw, int fh, int ww, int wh,
                      float *x0, float *y0, float *x1, float *y1) {
    float fa = (float)fw / (float)(fh ? fh : 1);
    float wa = (float)ww / (float)(wh ? wh : 1);
    if (fa > wa) { float s=wa/fa; *x0=-1;*x1=1;*y0=-s;*y1=s; }
    else         { float s=fa/wa; *x0=-s;*x1=s;*y0=-1;*y1=1; }
}

static void gl_render_frame(void) {
    int fw, fh, fs;
    uint8_t *tmp = NULL;
    pthread_mutex_lock(&g_mu);
    if (g_ready && g_buf) {
        fw=g_fw; fh=g_fh; fs=g_fs;
        size_t sz = (size_t)fh*(size_t)fs;
        tmp = (uint8_t*)malloc(sz);
        if (tmp) memcpy(tmp, g_buf, sz);
        g_ready = 0;
    }
    pthread_mutex_unlock(&g_mu);
    if (!tmp) return;

    glBindTexture(GL_TEXTURE_2D, g_tex);
    if (fw!=g_tex_w || fh!=g_tex_h) {
        glTexImage2D(GL_TEXTURE_2D,0,GL_RGBA,fw,fh,0,GL_RGBA,GL_UNSIGNED_BYTE,tmp);
        g_tex_w=fw; g_tex_h=fh;
    } else {
        glTexSubImage2D(GL_TEXTURE_2D,0,0,0,fw,fh,GL_RGBA,GL_UNSIGNED_BYTE,tmp);
    }
    free(tmp);

    // Get current window geometry.
    Window root; int rx,ry; unsigned ww,wh,bw,depth;
    XGetGeometry(g_dpy, g_win, &root, &rx, &ry, &ww, &wh, &bw, &depth);
    if (!ww || !wh) return;
    glViewport(0,0,(int)ww,(int)wh);

    glClearColor(0,0,0,1); glClear(GL_COLOR_BUFFER_BIT);

    float x0,y0,x1,y1;
    letterbox(fw,fh,(int)ww,(int)wh,&x0,&y0,&x1,&y1);

    glMatrixMode(GL_PROJECTION); glLoadIdentity();
    glMatrixMode(GL_MODELVIEW);  glLoadIdentity();
    glEnable(GL_TEXTURE_2D);
    glBindTexture(GL_TEXTURE_2D, g_tex);
    glColor4f(1,1,1,1);
    glBegin(GL_QUADS);
        glTexCoord2f(0,1); glVertex2f(x0,y0);
        glTexCoord2f(1,1); glVertex2f(x1,y0);
        glTexCoord2f(1,0); glVertex2f(x1,y1);
        glTexCoord2f(0,0); glVertex2f(x0,y1);
    glEnd();
    glDisable(GL_TEXTURE_2D);
    glXSwapBuffers(g_dpy, g_win);

    // Update stats atomics — Go polls these, no CGO from render thread.
    g_rendered++; g_fps_n++;
    g_stat_rendered  = g_rendered;
    g_stat_submitted = g_submitted;
    double now = mono_sec();
    if (g_rendered == 1) {
        g_stat_first = 1; g_stat_fw = fw; g_stat_fh = fh;
        g_fps_t0=now; g_fps_n=0;
    }
    double el = now - g_fps_t0;
    if (el >= 5.0 && g_fps_n > 0) {
        g_stat_fps       = (float)((double)g_fps_n/el);
        g_stat_fps_ready = 1;
        g_fps_t0=now; g_fps_n=0;
    }
}

static void vsync_apply(void) {
    const char *ext = glXQueryExtensionsString(g_dpy, DefaultScreen(g_dpy));
    if (ext) {
        if (strstr(ext, "GLX_EXT_swap_control"))
            glx_swap_ext = (PFNGLXSWAPINTERVALEXTPROC)glXGetProcAddressARB(
                               (const GLubyte*)"glXSwapIntervalEXT");
        if (strstr(ext, "GLX_MESA_swap_control"))
            glx_swap_mesa = (PFNGLXSWAPINTERVALMESAPROC)glXGetProcAddressARB(
                                (const GLubyte*)"glXSwapIntervalMESA");
    }
    int v = g_vsync ? 1 : 0;
    if (glx_swap_ext)  glx_swap_ext(g_dpy, g_win, v);
    else if (glx_swap_mesa) glx_swap_mesa(v);
}

static void *render_thread_fn(void *unused) {
    (void)unused;
    // NOTE: Do NOT call goGLLog here — this is a native pthread, and CGO
    // callbacks from non-Go threads can deadlock the Go GC.
    if (!glXMakeCurrent(g_dpy, g_win, g_ctx)) {
        atomic_store(&g_active, 0);
        return NULL;
    }
    glGenTextures(1, &g_tex);
    glBindTexture(GL_TEXTURE_2D, g_tex);
    glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_MIN_FILTER, GL_LINEAR);
    glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_MAG_FILTER, GL_LINEAR);
    glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_WRAP_S, GL_CLAMP_TO_EDGE);
    glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_WRAP_T, GL_CLAMP_TO_EDGE);
    vsync_apply();

    while (atomic_load(&g_active)) {
        // Poll with short timeout so we can check g_active.
        struct timeval tv = {0, 16000}; // 16 ms
        fd_set fds; FD_ZERO(&fds); FD_SET(g_pipe_r, &fds);
        select(g_pipe_r+1, &fds, NULL, NULL, &tv);
        if (FD_ISSET(g_pipe_r, &fds)) {
            char buf[64]; read(g_pipe_r, buf, sizeof(buf));
        }
        if (!atomic_load(&g_active)) break;
        if (g_ready) gl_render_frame();
    }
    if (g_tex) { glDeleteTextures(1, &g_tex); g_tex=0; }
    glXMakeCurrent(g_dpy, None, NULL);
    return NULL;
}

// ─────────────────────────────────────────────────────────────────────────────
// Public C API
// ─────────────────────────────────────────────────────────────────────────────

int gl_video_is_active(void) { return atomic_load(&g_active); }

int gl_video_try_submit(uint8_t *rgba, int width, int height, int stride) {
    if (!atomic_load(&g_active)) return 0;
    size_t sz = (size_t)height*(size_t)stride;
    pthread_mutex_lock(&g_mu);
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
    pthread_mutex_unlock(&g_mu);
    if (g_pipe_w >= 0) { char c=1; write(g_pipe_w, &c, 1); }
    return 1;
}

// x,y,w,h are in X11 pixels (DPI-scaled by caller).
// Pass w<=0 or h<=0 to cover the full parent window.
int gl_video_create(uintptr_t parent_xwin, int x, int y, int w, int h, int vsync) {
    g_vsync = vsync;
    // Tear down previous.
    if (atomic_load(&g_active)) {
        atomic_store(&g_active, 0);
        if (g_pipe_w>=0) { char c=0; write(g_pipe_w,&c,1); }
        if (g_thread) { pthread_join(g_thread, NULL); g_thread=0; }
        if (g_pipe_r>=0) { close(g_pipe_r); g_pipe_r=-1; }
        if (g_pipe_w>=0) { close(g_pipe_w); g_pipe_w=-1; }
        if (g_ctx && g_dpy) { glXDestroyContext(g_dpy, g_ctx); g_ctx=NULL; }
        if (g_win && g_dpy) { XDestroyWindow(g_dpy, g_win); g_win=0; }
        if (g_dpy)  { XCloseDisplay(g_dpy); g_dpy=NULL; }
        if (g_buf)  { free(g_buf); g_buf=NULL; g_buf_sz=0; }
    }

    Display *dpy = XOpenDisplay(NULL);
    if (!dpy) { goGLLog("gl_video_create: XOpenDisplay failed", 2); return 0; }

    int screen = DefaultScreen(dpy);
    int attribs[] = { GLX_RGBA, GLX_DOUBLEBUFFER,
                      GLX_RED_SIZE,8, GLX_GREEN_SIZE,8, GLX_BLUE_SIZE,8,
                      GLX_DEPTH_SIZE,16, None };
    XVisualInfo *vi = glXChooseVisual(dpy, screen, attribs);
    if (!vi) { goGLLog("gl_video_create: glXChooseVisual failed", 2); XCloseDisplay(dpy); return 0; }

    Window parent = (Window)parent_xwin;

    int fullwin = (w<=0 || h<=0);
    if (fullwin) {
        Window root; int rx,ry; unsigned uw,uh,bw,depth;
        XGetGeometry(dpy, parent, &root, &rx, &ry, &uw, &uh, &bw, &depth);
        x=0; y=0; w=(int)uw; h=(int)uh;
    }

    XSetWindowAttributes wa = {0};
    wa.colormap   = XCreateColormap(dpy, RootWindow(dpy,screen), vi->visual, AllocNone);
    wa.background_pixel = BlackPixel(dpy, screen);
    wa.border_pixel     = 0;
    unsigned long mask = CWColormap|CWBackPixel|CWBorderPixel;
    Window child = XCreateWindow(dpy, parent, x, y, (unsigned)w, (unsigned)h,
                                 0, vi->depth, InputOutput, vi->visual, mask, &wa);
    XMapWindow(dpy, child);
    // Make the overlay window input-transparent so the X server routes pointer
    // events to the Fyne/GLFW parent instead of consuming them here.
    XShapeCombineRectangles(dpy, child, ShapeInput, 0, 0, NULL, 0, ShapeSet, Unsorted);
    XFlush(dpy);
    XFree(vi);

    GLXContext ctx = glXCreateContext(dpy, vi, NULL, GL_TRUE);
    if (!ctx) {
        goGLLog("gl_video_create: glXCreateContext failed", 2);
        XDestroyWindow(dpy, child); XCloseDisplay(dpy); return 0;
    }

    int fds[2];
    if (pipe(fds) != 0) {
        goGLLog("gl_video_create: pipe failed", 2);
        glXDestroyContext(dpy, ctx); XDestroyWindow(dpy, child); XCloseDisplay(dpy); return 0;
    }

    g_dpy=dpy; g_win=child; g_ctx=ctx;
    g_pipe_r=fds[0]; g_pipe_w=fds[1];
    g_submitted=0; g_rendered=0; g_fps_n=0; g_fps_t0=0;
    g_ready=0; g_tex=0; g_tex_w=0; g_tex_h=0;
    atomic_store(&g_active, 1);

    pthread_attr_t attr; pthread_attr_init(&attr);
    if (pthread_create(&g_thread, &attr, render_thread_fn, NULL) != 0) {
        goGLLog("gl_video_create: pthread_create failed", 2);
        atomic_store(&g_active, 0);
        close(g_pipe_r); g_pipe_r=-1; close(g_pipe_w); g_pipe_w=-1;
        glXDestroyContext(dpy, ctx); g_ctx=NULL;
        XDestroyWindow(dpy, child); g_win=0;
        XCloseDisplay(dpy); g_dpy=NULL; return 0;
    }
    pthread_attr_destroy(&attr);

    char m[192];
    if (fullwin)
        snprintf(m,sizeof(m),"overlay created (full-window %dx%d) XWin=0x%lx", w, h, (unsigned long)parent_xwin);
    else
        snprintf(m,sizeof(m),"overlay created %dx%d at (%d,%d) XWin=0x%lx", w, h, x, y, (unsigned long)parent_xwin);
    goGLLog(m, 0);
    return 1;
}

void gl_video_update_frame(int x, int y, int w, int h) {
    if (!atomic_load(&g_active) || !g_dpy || !g_win) return;
    XMoveResizeWindow(g_dpy, g_win, x, y, (unsigned)w, (unsigned)h);
    XFlush(g_dpy);
}

void gl_video_destroy(void) {
    if (!atomic_load(&g_active)) return;
    atomic_store(&g_active, 0);
    if (g_pipe_w>=0) { char c=0; write(g_pipe_w,&c,1); }
    if (g_thread) { pthread_join(g_thread, NULL); g_thread=0; }
    if (g_pipe_r>=0) { close(g_pipe_r); g_pipe_r=-1; }
    if (g_pipe_w>=0) { close(g_pipe_w); g_pipe_w=-1; }
    if (g_ctx && g_dpy) { glXDestroyContext(g_dpy, g_ctx); g_ctx=NULL; }
    if (g_win && g_dpy) { XDestroyWindow(g_dpy, g_win); g_win=0; }
    if (g_dpy) { XCloseDisplay(g_dpy); g_dpy=NULL; }
    if (g_buf) { free(g_buf); g_buf=NULL; g_buf_sz=0; }
    // goGLLog is safe here: called from a Go CGO goroutine, not the render thread.
    char m[192];
    snprintf(m,sizeof(m),"overlay destroyed — rendered=%lld  submitted=%lld", g_rendered, g_submitted);
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

#endif // defined(__linux__) && !defined(__ANDROID__)
