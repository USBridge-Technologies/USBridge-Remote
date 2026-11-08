// gl_video_impl_linux.c — OpenGL overlay for video rendering on Linux (the
// fallback when Vulkan is unavailable).
//   • X11 / XWayland: GLX on a child X11 Window over the video widget area.
//   • Native Wayland: EGL on a wl_egl_window over the same desync
//     wl_subsurface host the Vulkan overlay uses (vk_video_wl_linux.c).
// RGBA frames are drawn as a GL texture on a dedicated render thread.

#if defined(__linux__) && !defined(__ANDROID__)

struct wl_display; struct wl_surface;
extern int  phase_probe_init(struct wl_display *dpy, struct wl_surface *surface);
extern void phase_probe_note_submit(const unsigned char *y, int stride, int w, int h);
extern void phase_probe_before_commit(void);
extern void phase_probe_after_commit(void);
#include <X11/Xlib.h>
#include <X11/extensions/shape.h>
#define GL_GLEXT_PROTOTYPES
#include <GL/gl.h>
#include <GL/glext.h>
#include <GL/glx.h>
#include <EGL/egl.h>
#include <EGL/eglext.h>
#include <wayland-client.h>
#include <wayland-egl.h>
#include <math.h>
#include "vk_video_wl_linux.h"
#include <pthread.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include <stdio.h>
#include <stdatomic.h>
#include <time.h>
#include <unistd.h>

// Shape the picture is shown in: the target screen's width/height when the
// client's video source setting names it (gl_video_set_display_aspect), so an
// anamorphic capture mode (the MS2130 stretching a 16:10 monitor into
// 1024x768) still looks right; otherwise the frame's own. The Go side fits
// the absolute-mouse rect with the same aspect (service.VideoDisplayAspect).
static volatile float g_display_aspect = 0.0f;
void gl_video_set_display_aspect(float a) { g_display_aspect = a > 0.0f ? a : 0.0f; }
static float display_aspect(int fw, int fh) {
    float a = g_display_aspect;
    return a > 0.0f ? a : (float)fw / (float)(fh > 0 ? fh : 1);
}

// GLX vsync extensions (optional, loaded at runtime).
typedef void (*PFNGLXSWAPINTERVALEXTPROC)(Display*, GLXDrawable, int);
typedef int  (*PFNGLXSWAPINTERVALMESAPROC)(unsigned);
static PFNGLXSWAPINTERVALEXTPROC  glx_swap_ext  = NULL;
static PFNGLXSWAPINTERVALMESAPROC glx_swap_mesa = NULL;
static int g_vsync = 0;

// goGLLog is called only from gl_video_create/destroy (CGO context — safe).
// NEVER called from the render thread (native pthread — would deadlock Go GC).
extern void goGLLog(char *msg, int level);

// g_pipe_{r,w} is a self-pipe used purely to wake select() below (or to break
// the render thread out of it on teardown) -- the byte's value and the read
// byte count are both irrelevant, only "something arrived" matters. write()/
// read() are declared warn_unused_result; routing the return value through a
// variable (rather than discarding it directly) silences that without
// pretending the result is checked for anything meaningful.
static void pipe_wake(int fd, char c) {
    ssize_t n = write(fd, &c, 1);
    (void)n;
}
static void pipe_drain(int fd) {
    char buf[64];
    ssize_t n = read(fd, buf, sizeof(buf));
    (void)n;
}

// ─────────────────────────────────────────────────────────────────────────────

static Display   *g_dpy  = NULL;
static Window     g_win  = 0;
static GLXContext g_ctx  = NULL;
static GLuint     g_tex  = 0;
static int        g_tex_w = 0, g_tex_h = 0;

static volatile atomic_int g_active = 0;

// Native Wayland (EGL) state. The EGLDisplay is GLFW's own (EGL hands out one
// per wl_display), so it is never terminated here.
static int             g_is_wl    = 0;
static EGLDisplay      g_egl_dpy  = EGL_NO_DISPLAY;
static EGLContext      g_egl_ctx  = EGL_NO_CONTEXT;
static EGLSurface      g_egl_surf = EGL_NO_SURFACE;
static struct wl_egl_window *g_egl_win = NULL;
static atomic_int      g_want_w, g_want_h; // buffer pixels, set by update_frame
static int             g_cur_w = 0, g_cur_h = 0; // render thread only
static float           g_wl_scale = 1.0f;

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
    float fa = display_aspect(fw, fh);
    float wa = (float)ww / (float)(wh ? wh : 1);
    if (fa > wa) { float s=wa/fa; *x0=-1;*x1=1;*y0=-s;*y1=s; }
    else         { float s=fa/wa; *x0=-s;*x1=s;*y0=-1;*y1=1; }
}

// ─── NV12 upload path ────────────────────────────────────────────────────────
// Hardware-decoded frames arrive as NV12 (a quick download for NVDEC/VAAPI)
// and are converted to RGB by a fragment shader, like the Vulkan path's
// YCbCr sampler. The RGBA path's CPU sws_scale (~13 ms per 2560x1440 frame)
// held the decoder to ~43 fps; frames then queued up for seconds.
static uint8_t *g_nv12_buf = NULL;     // Y plane then interleaved UV, tightly packed
static size_t   g_nv12_cap = 0;
static int      g_nv12_w = 0, g_nv12_h = 0, g_nv12_bt709 = 0;
static volatile int g_nv12_ready = 0;
static atomic_int g_nv12_submit_bt709;
static atomic_int g_nv12_failed;       // shader unavailable: decoder keeps RGBA
static GLuint   g_ytex = 0, g_uvtex = 0, g_prog = 0;
static int      g_prog_state = 0;      // 0 untried, 1 ready, -1 failed (render thread)
static int      g_yuv_w = 0, g_yuv_h = 0;
static GLint    g_u_y = -1, g_u_uv = -1, g_u_m = -1, g_u_off = -1;
static int      g_last_kind = 0;       // 1 RGBA, 2 NV12 -- what the textures hold
static int      g_last_fw = 0, g_last_fh = 0;
static volatile int g_redraw = 0;      // holes changed: draw the last frame again

// Menu holes, in overlay pixels (top-left origin). X11 cuts them out of the
// child window's shape; Wayland clears them to alpha 0 in the EGL buffer.
#define GL_MAX_HOLES 1024
static pthread_mutex_t g_holes_mu = PTHREAD_MUTEX_INITIALIZER;
static int  g_holes[GL_MAX_HOLES * 4];
static int  g_hole_n = 0;
static volatile atomic_int g_hidden;
static int  g_win_mapped = 1;
static int  g_egl_alpha = 0;           // Wayland EGL config has alpha (holes drawable)

static GLuint gl_compile(GLenum type, const char *src) {
    GLuint sh = glCreateShader(type);
    glShaderSource(sh, 1, &src, NULL);
    glCompileShader(sh);
    GLint ok = 0;
    glGetShaderiv(sh, GL_COMPILE_STATUS, &ok);
    if (!ok) { glDeleteShader(sh); return 0; }
    return sh;
}

static int gl_nv12_ensure_prog(void) {
    if (g_prog_state) return g_prog_state > 0;
    static const char *vs =
        "#version 110\n"
        "varying vec2 uv;\n"
        "void main() { gl_Position = gl_Vertex; uv = gl_MultiTexCoord0.xy; }\n";
    static const char *fs =
        "#version 110\n"
        "uniform sampler2D ytex;\n"
        "uniform sampler2D uvtex;\n"
        "uniform mat3 m;\n"
        "uniform vec3 off;\n"
        "varying vec2 uv;\n"
        "void main() {\n"
        "  vec3 yuv = vec3(texture2D(ytex, uv).r, texture2D(uvtex, uv).ra) - off;\n"
        "  gl_FragColor = vec4(clamp(m * yuv, 0.0, 1.0), 1.0);\n"
        "}\n";
    GLuint v = gl_compile(GL_VERTEX_SHADER, vs), f = gl_compile(GL_FRAGMENT_SHADER, fs);
    if (v && f) {
        g_prog = glCreateProgram();
        glAttachShader(g_prog, v);
        glAttachShader(g_prog, f);
        glLinkProgram(g_prog);
        GLint ok = 0;
        glGetProgramiv(g_prog, GL_LINK_STATUS, &ok);
        if (!ok) { glDeleteProgram(g_prog); g_prog = 0; }
    }
    if (v) glDeleteShader(v);
    if (f) glDeleteShader(f);
    if (!g_prog) {
        g_prog_state = -1;
        atomic_store(&g_nv12_failed, 1);
        return 0;
    }
    g_u_y = glGetUniformLocation(g_prog, "ytex");
    g_u_uv = glGetUniformLocation(g_prog, "uvtex");
    g_u_m = glGetUniformLocation(g_prog, "m");
    g_u_off = glGetUniformLocation(g_prog, "off");
    GLuint t[2];
    glGenTextures(2, t);
    g_ytex = t[0]; g_uvtex = t[1];
    for (int i = 0; i < 2; i++) {
        glBindTexture(GL_TEXTURE_2D, t[i]);
        glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_MIN_FILTER, GL_LINEAR);
        glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_MAG_FILTER, GL_LINEAR);
        glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_WRAP_S, GL_CLAMP_TO_EDGE);
        glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_WRAP_T, GL_CLAMP_TO_EDGE);
    }
    g_prog_state = 1;
    return 1;
}

static void gl_nv12_destroy(void) {
    if (g_prog) { glDeleteProgram(g_prog); g_prog = 0; }
    if (g_ytex) { GLuint t[2] = { g_ytex, g_uvtex }; glDeleteTextures(2, t); g_ytex = g_uvtex = 0; }
    g_prog_state = 0;
    g_yuv_w = g_yuv_h = 0;
}

static void gl_nv12_upload(const uint8_t *buf, int w, int h) {
    int cw = (w + 1) / 2, ch = (h + 1) / 2;
    glPixelStorei(GL_UNPACK_ALIGNMENT, 1);
    glBindTexture(GL_TEXTURE_2D, g_ytex);
    if (w != g_yuv_w || h != g_yuv_h)
        glTexImage2D(GL_TEXTURE_2D, 0, GL_LUMINANCE, w, h, 0, GL_LUMINANCE, GL_UNSIGNED_BYTE, buf);
    else
        glTexSubImage2D(GL_TEXTURE_2D, 0, 0, 0, w, h, GL_LUMINANCE, GL_UNSIGNED_BYTE, buf);
    glBindTexture(GL_TEXTURE_2D, g_uvtex);
    const uint8_t *uv = buf + (size_t)w * (size_t)h;
    if (w != g_yuv_w || h != g_yuv_h)
        glTexImage2D(GL_TEXTURE_2D, 0, GL_LUMINANCE_ALPHA, cw, ch, 0, GL_LUMINANCE_ALPHA, GL_UNSIGNED_BYTE, uv);
    else
        glTexSubImage2D(GL_TEXTURE_2D, 0, 0, 0, cw, ch, GL_LUMINANCE_ALPHA, GL_UNSIGNED_BYTE, uv);
    glPixelStorei(GL_UNPACK_ALIGNMENT, 4);
    g_yuv_w = w; g_yuv_h = h;
}

static void gl_quad(float x0, float y0, float x1, float y1) {
    glBegin(GL_QUADS);
        glTexCoord2f(0,1); glVertex2f(x0,y0);
        glTexCoord2f(1,1); glVertex2f(x1,y0);
        glTexCoord2f(1,0); glVertex2f(x1,y1);
        glTexCoord2f(0,0); glVertex2f(x0,y1);
    glEnd();
}

static void gl_render_frame(void) {
    int fw = 0, fh = 0, fs = 0;
    uint8_t *tmp = NULL;
    int kind = 0; // what arrived: 1 RGBA, 2 NV12, 0 nothing (redraw)
    pthread_mutex_lock(&g_mu);
    if (g_nv12_ready && g_nv12_buf) {
        fw = g_nv12_w; fh = g_nv12_h;
        size_t sz = (size_t)fw * (size_t)fh + (size_t)((fw + 1) / 2) * 2 * (size_t)((fh + 1) / 2);
        tmp = (uint8_t*)malloc(sz);
        if (tmp) { memcpy(tmp, g_nv12_buf, sz); kind = 2; }
        g_nv12_ready = 0;
        g_ready = 0;
    } else if (g_ready && g_buf) {
        fw=g_fw; fh=g_fh; fs=g_fs;
        size_t sz = (size_t)fh*(size_t)fs;
        tmp = (uint8_t*)malloc(sz);
        if (tmp) { memcpy(tmp, g_buf, sz); kind = 1; }
        g_ready = 0;
    }
    int bt709 = g_nv12_bt709;
    pthread_mutex_unlock(&g_mu);
    int redraw = g_redraw;
    g_redraw = 0;
    if (!kind && !(redraw && g_last_kind)) return;

    if (kind == 2 && !gl_nv12_ensure_prog()) { free(tmp); return; }
    if (kind == 2) {
        gl_nv12_upload(tmp, fw, fh);
    } else if (kind == 1) {
        glBindTexture(GL_TEXTURE_2D, g_tex);
        if (fw!=g_tex_w || fh!=g_tex_h) {
            glTexImage2D(GL_TEXTURE_2D,0,GL_RGBA,fw,fh,0,GL_RGBA,GL_UNSIGNED_BYTE,tmp);
            g_tex_w=fw; g_tex_h=fh;
        } else {
            glTexSubImage2D(GL_TEXTURE_2D,0,0,0,fw,fh,GL_RGBA,GL_UNSIGNED_BYTE,tmp);
        }
    }
    free(tmp);
    if (kind) { g_last_kind = kind; g_last_fw = fw; g_last_fh = fh; }
    else { fw = g_last_fw; fh = g_last_fh; }

    // Get current window geometry.
    unsigned ww, wh;
    if (g_is_wl) {
        int want_w = atomic_load(&g_want_w), want_h = atomic_load(&g_want_h);
        if (want_w > 0 && want_h > 0 && (want_w != g_cur_w || want_h != g_cur_h)) {
            // Takes effect with the next eglSwapBuffers.
            wl_egl_window_resize(g_egl_win, want_w, want_h, 0, 0);
            g_cur_w = want_w; g_cur_h = want_h;
        }
        ww = (unsigned)g_cur_w; wh = (unsigned)g_cur_h;
    } else {
        Window root; int rx,ry; unsigned bw,depth;
        XGetGeometry(g_dpy, g_win, &root, &rx, &ry, &ww, &wh, &bw, &depth);
    }
    if (!ww || !wh) return;
    glViewport(0,0,(int)ww,(int)wh);

    glClearColor(0,0,0,1); glClear(GL_COLOR_BUFFER_BIT);

    float x0,y0,x1,y1;
    letterbox(fw,fh,(int)ww,(int)wh,&x0,&y0,&x1,&y1);

    glMatrixMode(GL_PROJECTION); glLoadIdentity();
    glMatrixMode(GL_MODELVIEW);  glLoadIdentity();
    glColor4f(1,1,1,1);
    if (g_last_kind == 2) {
        // Limited-range BT.601 / BT.709, columns = Y, U, V coefficients.
        static const float m601[9] = { 1.164f, 1.164f, 1.164f,  0.0f, -0.392f, 2.017f,  1.596f, -0.813f, 0.0f };
        static const float m709[9] = { 1.164f, 1.164f, 1.164f,  0.0f, -0.213f, 2.112f,  1.793f, -0.533f, 0.0f };
        glUseProgram(g_prog);
        glUniform1i(g_u_y, 0);
        glUniform1i(g_u_uv, 1);
        glUniformMatrix3fv(g_u_m, 1, GL_FALSE, bt709 ? m709 : m601);
        glUniform3f(g_u_off, 16.0f / 255.0f, 128.0f / 255.0f, 128.0f / 255.0f);
        glActiveTexture(GL_TEXTURE1); glBindTexture(GL_TEXTURE_2D, g_uvtex);
        glActiveTexture(GL_TEXTURE0); glBindTexture(GL_TEXTURE_2D, g_ytex);
        gl_quad(x0, y0, x1, y1);
        glUseProgram(0);
    } else {
        glEnable(GL_TEXTURE_2D);
        glBindTexture(GL_TEXTURE_2D, g_tex);
        gl_quad(x0, y0, x1, y1);
        glDisable(GL_TEXTURE_2D);
    }

    if (g_is_wl && g_egl_alpha) {
        // The picture is opaque except the menu holes, cleared to alpha 0 so
        // the Fyne menu on the parent surface shows through.
        glColorMask(GL_FALSE, GL_FALSE, GL_FALSE, GL_TRUE);
        glClearColor(0, 0, 0, 1);
        glClear(GL_COLOR_BUFFER_BIT);
        glColorMask(GL_TRUE, GL_TRUE, GL_TRUE, GL_TRUE);
        pthread_mutex_lock(&g_holes_mu);
        if (g_hole_n > 0) {
            glEnable(GL_SCISSOR_TEST);
            glClearColor(0, 0, 0, 0);
            for (int i = 0; i < g_hole_n; i++) {
                int hx = g_holes[i*4], hy = g_holes[i*4+1], hw = g_holes[i*4+2], hh = g_holes[i*4+3];
                glScissor(hx, (int)wh - hy - hh, hw, hh); // GL origin is bottom-left
                glClear(GL_COLOR_BUFFER_BIT);
            }
            glDisable(GL_SCISSOR_TEST);
        }
        pthread_mutex_unlock(&g_holes_mu);
    }

    if (g_is_wl) {
        if (kind) phase_probe_before_commit();
        eglSwapBuffers(g_egl_dpy, g_egl_surf);
        phase_probe_after_commit();
    } else {
        glXSwapBuffers(g_dpy, g_win);
    }
    if (!kind) return; // a redraw is not a frame

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
    if (g_is_wl) {
        if (!eglMakeCurrent(g_egl_dpy, g_egl_surf, g_egl_surf, g_egl_ctx)) {
            atomic_store(&g_active, 0);
            return NULL;
        }
    } else if (!glXMakeCurrent(g_dpy, g_win, g_ctx)) {
        atomic_store(&g_active, 0);
        return NULL;
    }
    glGenTextures(1, &g_tex);
    glBindTexture(GL_TEXTURE_2D, g_tex);
    glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_MIN_FILTER, GL_LINEAR);
    glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_MAG_FILTER, GL_LINEAR);
    glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_WRAP_S, GL_CLAMP_TO_EDGE);
    glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_WRAP_T, GL_CLAMP_TO_EDGE);
    if (g_is_wl) {
        // Always interval 0 on Wayland: the compositor already presents
        // without tearing, and interval 1 makes eglSwapBuffers wait for the
        // frame callback of a desync subsurface, which queued frames up and
        // added a frame or more of latency (same choice as the Vulkan path's
        // IMMEDIATE/MAILBOX present mode).
        eglSwapInterval(g_egl_dpy, 0);
    } else {
        vsync_apply();
    }

    while (atomic_load(&g_active)) {
        // Poll with short timeout so we can check g_active.
        struct timeval tv = {0, 16000}; // 16 ms
        fd_set fds; FD_ZERO(&fds); FD_SET(g_pipe_r, &fds);
        select(g_pipe_r+1, &fds, NULL, NULL, &tv);
        if (FD_ISSET(g_pipe_r, &fds)) {
            pipe_drain(g_pipe_r);
        }
        if (!atomic_load(&g_active)) break;
        if (atomic_load(&g_hidden)) continue;
        if (g_ready || g_nv12_ready || g_redraw) gl_render_frame();
    }
    if (g_tex) { glDeleteTextures(1, &g_tex); g_tex=0; }
    gl_nv12_destroy();
    if (g_is_wl) eglMakeCurrent(g_egl_dpy, EGL_NO_SURFACE, EGL_NO_SURFACE, EGL_NO_CONTEXT);
    else glXMakeCurrent(g_dpy, None, NULL);
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
    if (g_pipe_w >= 0) { pipe_wake(g_pipe_w, 1); }
    return 1;
}

// Stops the render thread and frees the host (X11 or Wayland). CGO thread.
static void gl_teardown(void) {
    atomic_store(&g_active, 0);
    if (g_pipe_w>=0) { pipe_wake(g_pipe_w, 0); }
    if (g_thread) { pthread_join(g_thread, NULL); g_thread=0; }
    if (g_pipe_r>=0) { close(g_pipe_r); g_pipe_r=-1; }
    if (g_pipe_w>=0) { close(g_pipe_w); g_pipe_w=-1; }
    if (g_ctx && g_dpy) { glXDestroyContext(g_dpy, g_ctx); g_ctx=NULL; }
    if (g_win && g_dpy) { XDestroyWindow(g_dpy, g_win); g_win=0; }
    if (g_dpy)  { XCloseDisplay(g_dpy); g_dpy=NULL; }
    if (g_egl_surf != EGL_NO_SURFACE) { eglDestroySurface(g_egl_dpy, g_egl_surf); g_egl_surf = EGL_NO_SURFACE; }
    if (g_egl_ctx != EGL_NO_CONTEXT)  { eglDestroyContext(g_egl_dpy, g_egl_ctx); g_egl_ctx = EGL_NO_CONTEXT; }
    if (g_egl_win) { wl_egl_window_destroy(g_egl_win); g_egl_win = NULL; }
    if (g_is_wl) vk_wl_destroy();
    g_egl_dpy = EGL_NO_DISPLAY;
    g_is_wl = 0;
    g_egl_alpha = 0;
    if (g_buf)  { free(g_buf); g_buf=NULL; g_buf_sz=0; }
    if (g_nv12_buf) { free(g_nv12_buf); g_nv12_buf = NULL; g_nv12_cap = 0; }
    g_nv12_ready = 0;
    atomic_store(&g_nv12_failed, 0);
    g_last_kind = 0; g_redraw = 0;
    pthread_mutex_lock(&g_holes_mu); g_hole_n = 0; pthread_mutex_unlock(&g_holes_mu);
    atomic_store(&g_hidden, 0);
    g_win_mapped = 1;
}

static int gl_start_thread(void) {
    int fds[2];
    if (pipe(fds) != 0) { goGLLog("gl_video: pipe failed", 2); return 0; }
    g_pipe_r=fds[0]; g_pipe_w=fds[1];
    g_submitted=0; g_rendered=0; g_fps_n=0; g_fps_t0=0;
    g_ready=0; g_tex=0; g_tex_w=0; g_tex_h=0;
    atomic_store(&g_active, 1);
    if (pthread_create(&g_thread, NULL, render_thread_fn, NULL) != 0) {
        goGLLog("gl_video: pthread_create failed", 2);
        atomic_store(&g_active, 0);
        g_thread = 0;
        return 0;
    }
    return 1;
}

// gl_video_create_wayland — native-Wayland host: an EGL window surface on a
// desync wl_subsurface of the Fyne window's wl_surface. x,y,w,h are buffer
// pixels, scale = buffer pixels per logical unit.
int gl_video_create_wayland(uintptr_t display, uintptr_t parent_surface,
                            int x, int y, int w, int h, float scale, int vsync) {
    if (atomic_load(&g_active) || g_is_wl) gl_teardown();
    g_vsync = vsync;
    if (w <= 0 || h <= 0) { goGLLog("gl_video_create_wayland: empty rect", 2); return 0; }
    if (scale <= 0) scale = 1;
    g_wl_scale = scale;
    g_is_wl = 1;
    if (!vk_wl_create((void *)display, (void *)parent_surface,
                      (int)lroundf(x / scale), (int)lroundf(y / scale),
                      (int)lroundf(w / scale), (int)lroundf(h / scale))) {
        g_is_wl = 0;
        return 0;
    }

    PFNEGLGETPLATFORMDISPLAYEXTPROC get_platform_display =
        (PFNEGLGETPLATFORMDISPLAYEXTPROC)eglGetProcAddress("eglGetPlatformDisplayEXT");
    g_egl_dpy = get_platform_display
        ? get_platform_display(EGL_PLATFORM_WAYLAND_KHR, (void *)display, NULL)
        : eglGetDisplay((EGLNativeDisplayType)display);
    EGLint major = 0, minor = 0;
    if (g_egl_dpy == EGL_NO_DISPLAY || !eglInitialize(g_egl_dpy, &major, &minor)) {
        goGLLog("gl_video_create_wayland: EGL display init failed", 2); goto fail;
    }
    if (!eglBindAPI(EGL_OPENGL_API)) { goGLLog("gl_video_create_wayland: no desktop GL on EGL", 2); goto fail; }
    {
        const EGLint cfg_attr[] = {
            EGL_SURFACE_TYPE, EGL_WINDOW_BIT, EGL_RENDERABLE_TYPE, EGL_OPENGL_BIT,
            EGL_RED_SIZE, 8, EGL_GREEN_SIZE, 8, EGL_BLUE_SIZE, 8, EGL_ALPHA_SIZE, 8,
            EGL_NONE };
        EGLConfig cfg; EGLint n = 0;
        // Alpha lets menu holes show the parent surface (premultiplied, as
        // Wayland EGL presents ARGB buffers); without one, open menus hide
        // the video instead.
        if (eglChooseConfig(g_egl_dpy, cfg_attr, &cfg, 1, &n) && n >= 1) {
            g_egl_alpha = 1;
        } else {
            const EGLint opaque_attr[] = {
                EGL_SURFACE_TYPE, EGL_WINDOW_BIT, EGL_RENDERABLE_TYPE, EGL_OPENGL_BIT,
                EGL_RED_SIZE, 8, EGL_GREEN_SIZE, 8, EGL_BLUE_SIZE, 8, EGL_NONE };
            if (!eglChooseConfig(g_egl_dpy, opaque_attr, &cfg, 1, &n) || n < 1) {
                goGLLog("gl_video_create_wayland: eglChooseConfig failed", 2); goto fail;
            }
        }
        // Compatibility context: the renderer uses fixed-function GL, like GLX.
        g_egl_ctx = eglCreateContext(g_egl_dpy, cfg, EGL_NO_CONTEXT, NULL);
        if (g_egl_ctx == EGL_NO_CONTEXT) { goGLLog("gl_video_create_wayland: eglCreateContext failed", 2); goto fail; }
        g_egl_win = wl_egl_window_create(vk_wl_surface(), w, h);
        if (!g_egl_win) { goGLLog("gl_video_create_wayland: wl_egl_window_create failed", 2); goto fail; }
        g_egl_surf = eglCreateWindowSurface(g_egl_dpy, cfg, (EGLNativeWindowType)g_egl_win, NULL);
        if (g_egl_surf == EGL_NO_SURFACE) { goGLLog("gl_video_create_wayland: eglCreateWindowSurface failed", 2); goto fail; }
    }
    atomic_store(&g_want_w, w); atomic_store(&g_want_h, h);
    g_cur_w = w; g_cur_h = h;
    if (!gl_start_thread()) goto fail;
    phase_probe_init((struct wl_display *)display, vk_wl_surface());
    {
        char m[160];
        snprintf(m, sizeof(m), "overlay created (Wayland EGL %d.%d) %dx%d at (%d,%d) scale %.2f",
                 major, minor, w, h, x, y, scale);
        goGLLog(m, 0);
    }
    return 1;

fail:
    gl_teardown();
    return 0;
}

// x,y,w,h are in X11 pixels (DPI-scaled by caller).
// Pass w<=0 or h<=0 to cover the full parent window.
int gl_video_create(uintptr_t parent_xwin, int x, int y, int w, int h, int vsync) {
    g_vsync = vsync;
    // Tear down previous.
    if (atomic_load(&g_active) || g_is_wl) gl_teardown();

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

    // vi is still needed here; freeing it first made glXCreateContext read
    // freed memory (crashed the GLX fallback on NVIDIA).
    GLXContext ctx = glXCreateContext(dpy, vi, NULL, GL_TRUE);
    XFree(vi);
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

static void gl_x11_apply_holes(void);

void gl_video_update_frame(int x, int y, int w, int h) {
    if (!atomic_load(&g_active)) return;
    if (g_is_wl) {
        if (w <= 0 || h <= 0) return;
        float sc = g_wl_scale > 0 ? g_wl_scale : 1.0f;
        vk_wl_set_rect((int)lroundf(x / sc), (int)lroundf(y / sc),
                       (int)lroundf(w / sc), (int)lroundf(h / sc));
        atomic_store(&g_want_w, w); atomic_store(&g_want_h, h);
        if (g_pipe_w >= 0) pipe_wake(g_pipe_w, 1);
        return;
    }
    if (!g_dpy || !g_win) return;
    XMoveResizeWindow(g_dpy, g_win, x, y, (unsigned)w, (unsigned)h);
    // An explicit bounding shape does not grow with the window.
    if (g_hole_n > 0) gl_x11_apply_holes();
    XFlush(g_dpy);
}

// gl_video_try_submit_nv12 — decoder thread: NV12 planes (y, then
// interleaved uv) for the shader path. 0 when GL is not active or its NV12
// shader is unavailable (the caller then takes the RGBA path).
int gl_video_try_submit_nv12(const uint8_t *y, int y_stride, const uint8_t *uv, int uv_stride, int w, int h) {
    if (!atomic_load(&g_active) || atomic_load(&g_nv12_failed) || w <= 0 || h <= 0) return 0;
    int cw = (w + 1) / 2, ch = (h + 1) / 2;
    size_t ysz = (size_t)w * (size_t)h, uvsz = (size_t)cw * 2 * (size_t)ch;
    phase_probe_note_submit(y, y_stride, w, h);
    pthread_mutex_lock(&g_mu);
    if (g_nv12_cap < ysz + uvsz) {
        free(g_nv12_buf);
        g_nv12_buf = (uint8_t*)malloc(ysz + uvsz);
        g_nv12_cap = g_nv12_buf ? ysz + uvsz : 0;
    }
    if (g_nv12_buf) {
        for (int r = 0; r < h; r++) memcpy(g_nv12_buf + (size_t)r * w, y + (size_t)r * y_stride, (size_t)w);
        uint8_t *dst = g_nv12_buf + ysz;
        for (int r = 0; r < ch; r++) memcpy(dst + (size_t)r * cw * 2, uv + (size_t)r * uv_stride, (size_t)cw * 2);
        g_nv12_w = w; g_nv12_h = h;
        g_nv12_bt709 = atomic_load(&g_nv12_submit_bt709);
        g_nv12_ready = 1; g_submitted++;
    }
    pthread_mutex_unlock(&g_mu);
    if (g_pipe_w >= 0) pipe_wake(g_pipe_w, 1);
    return g_nv12_buf != NULL;
}

int gl_video_nv12_usable(void) { return atomic_load(&g_active) && !atomic_load(&g_nv12_failed); }

void gl_video_set_nv12_bt709(int bt709) { atomic_store(&g_nv12_submit_bt709, bt709 ? 1 : 0); }

static void gl_x11_apply_holes(void) {
    if (!g_dpy || !g_win) return;
    int w = 0, h = 0;
    {
        Window root; int rx, ry; unsigned uw, uh, bw, depth;
        XGetGeometry(g_dpy, g_win, &root, &rx, &ry, &uw, &uh, &bw, &depth);
        w = (int)uw; h = (int)uh;
    }
    pthread_mutex_lock(&g_holes_mu);
    int n = g_hole_n;
    if (n == 0 || w <= 0 || h <= 0) {
        XShapeCombineMask(g_dpy, g_win, ShapeBounding, 0, 0, None, ShapeSet);
    } else {
        static XRectangle rects[GL_MAX_HOLES];
        for (int i = 0; i < n; i++) {
            int x = g_holes[i*4], y = g_holes[i*4+1], rw = g_holes[i*4+2], rh = g_holes[i*4+3];
            if (x < -32000) { rw += x + 32000; x = -32000; }
            if (y < -32000) { rh += y + 32000; y = -32000; }
            rects[i].x = (short)x; rects[i].y = (short)y;
            rects[i].width = (unsigned short)(rw > 65535 ? 65535 : rw);
            rects[i].height = (unsigned short)(rh > 65535 ? 65535 : rh);
        }
        XRectangle full = { 0, 0, (unsigned short)w, (unsigned short)h };
        XShapeCombineRectangles(g_dpy, g_win, ShapeBounding, 0, 0, &full, 1, ShapeSet, Unsorted);
        XShapeCombineRectangles(g_dpy, g_win, ShapeBounding, 0, 0, rects, n, ShapeSubtract, Unsorted);
    }
    pthread_mutex_unlock(&g_holes_mu);
    XFlush(g_dpy);
}

static void gl_apply_visibility(void) {
    int hide = atomic_load(&g_hidden);
    if (g_is_wl) {
        // No alpha config: an open menu can only be shown by hiding the video.
        int hole_hide = !g_egl_alpha && g_hole_n > 0;
        vk_wl_set_hidden(hide || hole_hide);
        return;
    }
    if (!g_dpy || !g_win) return;
    if (hide && g_win_mapped)        { XUnmapWindow(g_dpy, g_win); g_win_mapped = 0; }
    else if (!hide && !g_win_mapped) { XMapWindow(g_dpy, g_win);   g_win_mapped = 1; }
    XFlush(g_dpy);
}

// gl_video_set_holes — CGO thread: menu rects (overlay pixels, x,y,w,h
// quadruples) the video must leave uncovered.
void gl_video_set_holes(const int *xywh, int n) {
    if (!atomic_load(&g_active)) return;
    if (n > GL_MAX_HOLES) n = GL_MAX_HOLES;
    int k = 0;
    pthread_mutex_lock(&g_holes_mu);
    for (int i = 0; i < n; i++) {
        if (xywh[i*4+2] <= 0 || xywh[i*4+3] <= 0) continue;
        memcpy(&g_holes[k*4], &xywh[i*4], 4 * sizeof(int));
        k++;
    }
    int changed = k != g_hole_n || k > 0;
    g_hole_n = k;
    pthread_mutex_unlock(&g_holes_mu);
    if (!changed) return;
    if (g_is_wl) {
        vk_wl_set_holes(g_holes, k, g_wl_scale);
        gl_apply_visibility();
        g_redraw = 1; // a paused stream still has to show the hole
        if (g_pipe_w >= 0) pipe_wake(g_pipe_w, 1);
    } else {
        gl_x11_apply_holes();
    }
}

void gl_video_set_hidden(int hidden) {
    atomic_store(&g_hidden, hidden ? 1 : 0);
    if (!atomic_load(&g_active)) return;
    gl_apply_visibility();
    if (!hidden) { g_redraw = 1; if (g_pipe_w >= 0) pipe_wake(g_pipe_w, 1); }
}

void gl_video_set_wl_scale(float scale) {
    if (scale > 0) g_wl_scale = scale;
}

void gl_video_destroy(void) {
    if (!atomic_load(&g_active)) return;
    gl_teardown();
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
