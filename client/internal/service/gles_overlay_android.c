// gles_overlay_android.c — OpenGL ES fallback for the Android video overlay,
// for devices without Vulkan (e.g. Redmi A2: Helio G36 / PowerVR GE8320).
//
// vk_video_impl_android.c keeps owning the overlay's state (frames, cursor,
// zoom/pan layout, menu holes) and its render thread; when no Vulkan device
// exists it draws each frame through here instead, onto the same
// SurfaceView. Decoder images (AImageReader buffers) are sampled zero-copy
// through an EGLImage on a GL_TEXTURE_EXTERNAL_OES texture, so the GPU does
// the YUV->RGB conversion; CPU RGBA frames (Net Graph burn-in) go through a
// plain texture upload. Menu holes are scissored clears to alpha 0 (the
// SurfaceView is TRANSLUCENT), everything else is opaque.
//
// All entry points except gles_ov_init run on the render thread, with the
// context current (gles_ov_make_current).
#ifdef __ANDROID__

#include <EGL/egl.h>
#include <EGL/eglext.h>
#include <GLES2/gl2.h>
#include <GLES2/gl2ext.h>
#include <android/hardware_buffer.h>
#include <android/log.h>
#include <android/native_window.h>
#include <dlfcn.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "gles_overlay_android.h"

#define GTAG "USBridge/GLESVideo"
#define GLOGI(...) __android_log_print(ANDROID_LOG_INFO,  GTAG, __VA_ARGS__)
#define GLOGE(...) __android_log_print(ANDROID_LOG_ERROR, GTAG, __VA_ARGS__)

static EGLDisplay     g_dpy  = EGL_NO_DISPLAY;
static EGLContext     g_ctx  = EGL_NO_CONTEXT;
static EGLSurface     g_surf = EGL_NO_SURFACE;
static ANativeWindow *g_win  = NULL;

static GLuint g_prog_ext = 0, g_prog_2d = 0;  // video (OES) / RGBA (2D) quads
static GLint  g_ext_loc_uv = -1, g_2d_loc_uv = -1;
static GLuint g_rgba_tex = 0;
static int    g_rgba_w = 0, g_rgba_h = 0;
static GLuint g_cursor_tex = 0;
static int    g_cursor_w = 0, g_cursor_h = 0;
static GLuint g_hud_tex = 0;
static int    g_hud_w = 0, g_hud_h = 0;
static char   g_renderer[128];

static PFNEGLGETNATIVECLIENTBUFFERANDROIDPROC p_getNativeClientBuffer = NULL;
static PFNEGLCREATEIMAGEKHRPROC               p_createImage = NULL;
static PFNEGLDESTROYIMAGEKHRPROC              p_destroyImage = NULL;
static PFNGLEGLIMAGETARGETTEXTURE2DOESPROC    p_imageTargetTexture = NULL;
static PFNEGLCREATESYNCKHRPROC                p_createSync = NULL;
static PFNEGLDESTROYSYNCKHRPROC               p_destroySync = NULL;
static PFNEGLCLIENTWAITSYNCKHRPROC            p_clientWaitSync = NULL;
static EGLSyncKHR                             g_last_sync = EGL_NO_SYNC_KHR;

// Decoder buffers come from a small pool: EGLImages are cached per buffer
// (AHardwareBuffer_getId; same reasoning as the Vulkan import cache), each
// entry holding a reference so the buffer and its id stay alive.
#define GLES_CACHE 24
typedef struct {
    AHardwareBuffer *ahb;
    uint64_t         key;
    EGLImageKHR      img;
    GLuint           tex;
    uint64_t         used;
} gles_entry;
static gles_entry g_cache[GLES_CACHE];
static uint64_t   g_tick = 0;

typedef int (*pfn_ahb_get_id)(const AHardwareBuffer *, uint64_t *);
static pfn_ahb_get_id p_ahb_get_id = NULL;

static const char *kVert =
    "attribute vec2 aPos;\n"
    "varying vec2 vUV;\n"
    "uniform vec4 uUV;\n" // u0, v0, u1, v1
    "void main() {\n"
    "  vec2 t = aPos * 0.5 + 0.5;\n"
    "  vUV = vec2(mix(uUV.x, uUV.z, t.x), mix(uUV.w, uUV.y, t.y));\n"
    "  gl_Position = vec4(aPos, 0.0, 1.0);\n"
    "}\n";
static const char *kFragExt =
    "#extension GL_OES_EGL_image_external : require\n"
    "precision mediump float;\n"
    "varying vec2 vUV;\n"
    "uniform samplerExternalOES uTex;\n"
    "void main() { gl_FragColor = vec4(texture2D(uTex, vUV).rgb, 1.0); }\n";
static const char *kFrag2d =
    "precision mediump float;\n"
    "varying vec2 vUV;\n"
    "uniform sampler2D uTex;\n"
    "void main() { gl_FragColor = texture2D(uTex, vUV); }\n";
static const GLfloat kQuad[] = { -1, -1, 1, -1, -1, 1, 1, 1 };

static GLuint compile(GLenum type, const char *src) {
    GLuint s = glCreateShader(type);
    glShaderSource(s, 1, &src, NULL);
    glCompileShader(s);
    GLint ok = 0;
    glGetShaderiv(s, GL_COMPILE_STATUS, &ok);
    if (!ok) {
        char log[256] = {0};
        glGetShaderInfoLog(s, sizeof(log), NULL, log);
        GLOGE("shader compile failed: %s", log);
        glDeleteShader(s);
        return 0;
    }
    return s;
}

static GLuint link_program(const char *frag, GLint *loc_uv) {
    GLuint vs = compile(GL_VERTEX_SHADER, kVert), fs = compile(GL_FRAGMENT_SHADER, frag);
    if (!vs || !fs) return 0;
    GLuint p = glCreateProgram();
    glAttachShader(p, vs);
    glAttachShader(p, fs);
    glBindAttribLocation(p, 0, "aPos");
    glLinkProgram(p);
    glDeleteShader(vs);
    glDeleteShader(fs);
    GLint ok = 0;
    glGetProgramiv(p, GL_LINK_STATUS, &ok);
    if (!ok) { glDeleteProgram(p); return 0; }
    *loc_uv = glGetUniformLocation(p, "uUV");
    glUseProgram(p);
    glUniform1i(glGetUniformLocation(p, "uTex"), 0);
    return p;
}

int gles_ov_init(ANativeWindow *win) {
    g_dpy = eglGetDisplay(EGL_DEFAULT_DISPLAY);
    if (g_dpy == EGL_NO_DISPLAY || !eglInitialize(g_dpy, NULL, NULL)) {
        GLOGE("eglInitialize failed");
        return 0;
    }
    const EGLint cfg_attrs[] = {
        EGL_SURFACE_TYPE, EGL_WINDOW_BIT,
        EGL_RENDERABLE_TYPE, EGL_OPENGL_ES2_BIT,
        EGL_RED_SIZE, 8, EGL_GREEN_SIZE, 8, EGL_BLUE_SIZE, 8,
        EGL_ALPHA_SIZE, 8, // alpha 0 = menu hole
        EGL_NONE,
    };
    EGLConfig cfg;
    EGLint n = 0;
    if (!eglChooseConfig(g_dpy, cfg_attrs, &cfg, 1, &n) || n < 1) {
        GLOGE("eglChooseConfig failed");
        gles_ov_destroy();
        return 0;
    }
    const EGLint ctx_attrs[] = { EGL_CONTEXT_CLIENT_VERSION, 2, EGL_NONE };
    g_ctx = eglCreateContext(g_dpy, cfg, EGL_NO_CONTEXT, ctx_attrs);
    ANativeWindow_acquire(win);
    g_win = win;
    g_surf = g_ctx != EGL_NO_CONTEXT ? eglCreateWindowSurface(g_dpy, cfg, win, NULL) : EGL_NO_SURFACE;
    if (g_surf == EGL_NO_SURFACE || !eglMakeCurrent(g_dpy, g_surf, g_surf, g_ctx)) {
        GLOGE("EGL window surface/context failed (0x%x)", eglGetError());
        gles_ov_destroy();
        return 0;
    }
    eglSwapInterval(g_dpy, 0); // present as soon as drawn, like MAILBOX

    p_getNativeClientBuffer = (PFNEGLGETNATIVECLIENTBUFFERANDROIDPROC)eglGetProcAddress("eglGetNativeClientBufferANDROID");
    p_createImage = (PFNEGLCREATEIMAGEKHRPROC)eglGetProcAddress("eglCreateImageKHR");
    p_destroyImage = (PFNEGLDESTROYIMAGEKHRPROC)eglGetProcAddress("eglDestroyImageKHR");
    p_imageTargetTexture = (PFNGLEGLIMAGETARGETTEXTURE2DOESPROC)eglGetProcAddress("glEGLImageTargetTexture2DOES");
    p_createSync = (PFNEGLCREATESYNCKHRPROC)eglGetProcAddress("eglCreateSyncKHR");
    p_destroySync = (PFNEGLDESTROYSYNCKHRPROC)eglGetProcAddress("eglDestroySyncKHR");
    p_clientWaitSync = (PFNEGLCLIENTWAITSYNCKHRPROC)eglGetProcAddress("eglClientWaitSyncKHR");
    void *lib = dlopen("libandroid.so", RTLD_NOW);
    if (lib) p_ahb_get_id = (pfn_ahb_get_id)dlsym(lib, "AHardwareBuffer_getId");

    g_prog_ext = link_program(kFragExt, &g_ext_loc_uv);
    g_prog_2d = link_program(kFrag2d, &g_2d_loc_uv);
    if (!g_prog_ext || !g_prog_2d) {
        GLOGE("GL programs failed");
        gles_ov_destroy();
        return 0;
    }
    snprintf(g_renderer, sizeof(g_renderer), "%s", (const char *)glGetString(GL_RENDERER));
    GLOGI("GLES video overlay ready: %s (decoder images %s)", g_renderer,
          gles_ov_images_supported() ? "zero-copy" : "unavailable");
    eglMakeCurrent(g_dpy, EGL_NO_SURFACE, EGL_NO_SURFACE, EGL_NO_CONTEXT);
    return 1;
}

int gles_ov_make_current(void) {
    return g_ctx != EGL_NO_CONTEXT && eglMakeCurrent(g_dpy, g_surf, g_surf, g_ctx);
}

int gles_ov_images_supported(void) {
    return p_getNativeClientBuffer && p_createImage && p_destroyImage && p_imageTargetTexture;
}

void gles_ov_gpu_name(char *out, int out_len) {
    snprintf(out, (size_t)out_len, "%s", g_renderer);
}

int gles_ov_surface_size(int *w, int *h) {
    EGLint sw = 0, sh = 0;
    eglQuerySurface(g_dpy, g_surf, EGL_WIDTH, &sw);
    eglQuerySurface(g_dpy, g_surf, EGL_HEIGHT, &sh);
    *w = sw; *h = sh;
    return sw > 0 && sh > 0;
}

static void entry_free(gles_entry *e) {
    if (e->tex) glDeleteTextures(1, &e->tex);
    if (e->img != EGL_NO_IMAGE_KHR && e->img) p_destroyImage(g_dpy, e->img);
    if (e->ahb) AHardwareBuffer_release(e->ahb);
    memset(e, 0, sizeof(*e));
}

void gles_ov_drop_images(void) {
    gles_ov_wait_idle();
    for (int i = 0; i < GLES_CACHE; i++) entry_free(&g_cache[i]);
}

// image_texture returns the OES texture showing ahb, importing it once.
static GLuint image_texture(AHardwareBuffer *ahb) {
    uint64_t key = 0;
    if (!p_ahb_get_id || p_ahb_get_id(ahb, &key) != 0 || !key) key = (uint64_t)(uintptr_t)ahb;
    g_tick++;
    for (int i = 0; i < GLES_CACHE; i++)
        if (g_cache[i].ahb && g_cache[i].key == key) { g_cache[i].used = g_tick; return g_cache[i].tex; }

    int slot = 0;
    for (int i = 0; i < GLES_CACHE; i++) {
        if (!g_cache[i].ahb) { slot = i; break; }
        if (g_cache[i].used < g_cache[slot].used) slot = i;
    }
    gles_entry *e = &g_cache[slot];
    if (e->ahb) {
        gles_ov_wait_idle();
        entry_free(e);
    }
    EGLClientBuffer cb = p_getNativeClientBuffer(ahb);
    const EGLint attrs[] = { EGL_IMAGE_PRESERVED_KHR, EGL_TRUE, EGL_NONE };
    EGLImageKHR img = cb ? p_createImage(g_dpy, EGL_NO_CONTEXT, EGL_NATIVE_BUFFER_ANDROID, cb, attrs) : EGL_NO_IMAGE_KHR;
    if (img == EGL_NO_IMAGE_KHR) {
        GLOGE("eglCreateImageKHR failed for decoder buffer (0x%x)", eglGetError());
        return 0;
    }
    GLuint tex = 0;
    glGenTextures(1, &tex);
    glBindTexture(GL_TEXTURE_EXTERNAL_OES, tex);
    glTexParameteri(GL_TEXTURE_EXTERNAL_OES, GL_TEXTURE_MIN_FILTER, GL_LINEAR);
    glTexParameteri(GL_TEXTURE_EXTERNAL_OES, GL_TEXTURE_MAG_FILTER, GL_LINEAR);
    glTexParameteri(GL_TEXTURE_EXTERNAL_OES, GL_TEXTURE_WRAP_S, GL_CLAMP_TO_EDGE);
    glTexParameteri(GL_TEXTURE_EXTERNAL_OES, GL_TEXTURE_WRAP_T, GL_CLAMP_TO_EDGE);
    p_imageTargetTexture(GL_TEXTURE_EXTERNAL_OES, (GLeglImageOES)img);
    AHardwareBuffer_acquire(ahb);
    e->ahb = ahb; e->key = key; e->img = img; e->tex = tex; e->used = g_tick;
    GLOGI("imported decoder buffer %p (slot %d)", (void *)ahb, slot);
    return tex;
}

void gles_ov_set_cursor(const uint8_t *rgba, int w, int h) {
    if (!rgba || w <= 0 || h <= 0) return;
    if (!g_cursor_tex) {
        glGenTextures(1, &g_cursor_tex);
        glBindTexture(GL_TEXTURE_2D, g_cursor_tex);
        glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_MIN_FILTER, GL_NEAREST);
        glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_MAG_FILTER, GL_NEAREST);
        glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_WRAP_S, GL_CLAMP_TO_EDGE);
        glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_WRAP_T, GL_CLAMP_TO_EDGE);
    }
    glBindTexture(GL_TEXTURE_2D, g_cursor_tex);
    glPixelStorei(GL_UNPACK_ALIGNMENT, 1);
    glTexImage2D(GL_TEXTURE_2D, 0, GL_RGBA, w, h, 0, GL_RGBA, GL_UNSIGNED_BYTE, rgba);
    g_cursor_w = w; g_cursor_h = h;
}

void gles_ov_set_hud(const uint8_t *rgba, int w, int h) {
    if (!rgba || w <= 0 || h <= 0) return;
    if (!g_hud_tex) {
        glGenTextures(1, &g_hud_tex);
        glBindTexture(GL_TEXTURE_2D, g_hud_tex);
        glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_MIN_FILTER, GL_LINEAR);
        glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_MAG_FILTER, GL_LINEAR);
        glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_WRAP_S, GL_CLAMP_TO_EDGE);
        glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_WRAP_T, GL_CLAMP_TO_EDGE);
    }
    glBindTexture(GL_TEXTURE_2D, g_hud_tex);
    glPixelStorei(GL_UNPACK_ALIGNMENT, 4);
    if (g_hud_w != w || g_hud_h != h) {
        glTexImage2D(GL_TEXTURE_2D, 0, GL_RGBA, w, h, 0, GL_RGBA, GL_UNSIGNED_BYTE, rgba);
        g_hud_w = w; g_hud_h = h;
    } else {
        glTexSubImage2D(GL_TEXTURE_2D, 0, 0, 0, w, h, GL_RGBA, GL_UNSIGNED_BYTE, rgba);
    }
}

int gles_ov_hud_ready(void) { return g_hud_tex != 0 && g_hud_w > 0; }

// blend_over: straight-alpha "over" for color, but the surface's alpha
// stays 1 -- it is translucent, and alpha below 1 would show the Fyne
// window through the video.
static void blend_over(void) {
    glEnable(GL_BLEND);
    glBlendFuncSeparate(GL_SRC_ALPHA, GL_ONE_MINUS_SRC_ALPHA, GL_ONE, GL_ONE_MINUS_SRC_ALPHA);
}

// draw_quad draws the bound texture with prog into the surface-pixel rect
// (x, y, w, h; top-left origin), sampling uv = {u0, v0, u1, v1} (v0 = top).
static void draw_quad(GLuint prog, GLint loc_uv, int sh, int x, int y, int w, int h, const float uv[4]) {
    glViewport(x, sh - y - h, w, h);
    glUseProgram(prog);
    glUniform4f(loc_uv, uv[0], uv[1], uv[2], uv[3]);
    glDrawArrays(GL_TRIANGLE_STRIP, 0, 4);
}

int gles_ov_draw(const gles_ov_frame *f) {
    int sw = f->surf_w, sh = f->surf_h;
    glDisable(GL_SCISSOR_TEST);
    glDisable(GL_BLEND);
    glViewport(0, 0, sw, sh);
    glClearColor(0.0f, 0.0f, 0.0f, 1.0f); // opaque letterbox; alpha 0 is for holes
    glClear(GL_COLOR_BUFFER_BIT);
    glEnableVertexAttribArray(0);
    glVertexAttribPointer(0, 2, GL_FLOAT, GL_FALSE, 0, kQuad);

    if (f->dw > 0 && f->dh > 0 && f->tex_w > 0 && f->tex_h > 0) {
        float uv[4] = {
            (float)f->sx0 / (float)f->tex_w, (float)f->sy0 / (float)f->tex_h,
            (float)f->sx1 / (float)f->tex_w, (float)f->sy1 / (float)f->tex_h,
        };
        glActiveTexture(GL_TEXTURE0);
        if (f->ahb) {
            GLuint tex = image_texture(f->ahb);
            if (!tex) return 0;
            glBindTexture(GL_TEXTURE_EXTERNAL_OES, tex);
            draw_quad(g_prog_ext, g_ext_loc_uv, sh, f->dx, f->dy, f->dw, f->dh, uv);
        } else if (f->rgba) {
            if (!g_rgba_tex) {
                glGenTextures(1, &g_rgba_tex);
                glBindTexture(GL_TEXTURE_2D, g_rgba_tex);
                glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_MIN_FILTER, GL_LINEAR);
                glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_MAG_FILTER, GL_LINEAR);
                glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_WRAP_S, GL_CLAMP_TO_EDGE);
                glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_WRAP_T, GL_CLAMP_TO_EDGE);
            }
            glBindTexture(GL_TEXTURE_2D, g_rgba_tex);
            glPixelStorei(GL_UNPACK_ALIGNMENT, 4);
            if (g_rgba_w != f->tex_w || g_rgba_h != f->tex_h) {
                glTexImage2D(GL_TEXTURE_2D, 0, GL_RGBA, f->tex_w, f->tex_h, 0, GL_RGBA, GL_UNSIGNED_BYTE, NULL);
                g_rgba_w = f->tex_w; g_rgba_h = f->tex_h;
            }
            // Rows are tight (vk_video_impl_android.c strips any stride).
            glTexSubImage2D(GL_TEXTURE_2D, 0, 0, 0, f->tex_w, f->tex_h, GL_RGBA, GL_UNSIGNED_BYTE, f->rgba);
            draw_quad(g_prog_2d, g_2d_loc_uv, sh, f->dx, f->dy, f->dw, f->dh, uv);
        }
    }

    if (f->cursor_visible && g_cursor_tex && g_cursor_w > 0) {
        static const float full[4] = { 0, 0, 1, 1 };
        blend_over();
        glActiveTexture(GL_TEXTURE0);
        glBindTexture(GL_TEXTURE_2D, g_cursor_tex);
        draw_quad(g_prog_2d, g_2d_loc_uv, sh, f->cursor_x, f->cursor_y, g_cursor_w, g_cursor_h, full);
        glDisable(GL_BLEND);
    }

    if (f->hud_visible && g_hud_tex) {
        static const float full[4] = { 0, 0, 1, 1 };
        blend_over();
        glActiveTexture(GL_TEXTURE0);
        glBindTexture(GL_TEXTURE_2D, g_hud_tex);
        draw_quad(g_prog_2d, g_2d_loc_uv, sh, f->hud[0], f->hud[1], f->hud[2], f->hud[3], full);
        glDisable(GL_BLEND);
    }

    // Menu holes: alpha 0 so the Fyne window under the SurfaceView shows.
    if (f->nholes > 0) {
        glEnable(GL_SCISSOR_TEST);
        glClearColor(0.0f, 0.0f, 0.0f, 0.0f);
        for (int i = 0; i < f->nholes; i++) {
            const int *h = f->holes + i * 4;
            glScissor(h[0], sh - h[1] - h[3], h[2], h[3]);
            glClear(GL_COLOR_BUFFER_BIT);
        }
        glDisable(GL_SCISSOR_TEST);
    }
    glDisableVertexAttribArray(0);

    // Fence for this frame's reads, so the previous decoder image can be
    // released once the GPU is done with it (gles_ov_wait_idle).
    if (p_createSync) {
        if (g_last_sync != EGL_NO_SYNC_KHR) p_destroySync(g_dpy, g_last_sync);
        g_last_sync = p_createSync(g_dpy, EGL_SYNC_FENCE_KHR, NULL);
    }
    if (!eglSwapBuffers(g_dpy, g_surf)) {
        GLOGE("eglSwapBuffers failed (0x%x)", eglGetError());
        return 0;
    }
    return 1;
}

void gles_ov_wait_idle(void) {
    if (g_last_sync != EGL_NO_SYNC_KHR && p_clientWaitSync) {
        p_clientWaitSync(g_dpy, g_last_sync, EGL_SYNC_FLUSH_COMMANDS_BIT_KHR, 2000000000ULL);
        p_destroySync(g_dpy, g_last_sync);
        g_last_sync = EGL_NO_SYNC_KHR;
    } else if (g_ctx != EGL_NO_CONTEXT && eglGetCurrentContext() == g_ctx) {
        glFinish();
    }
}

void gles_ov_destroy(void) {
    if (g_dpy != EGL_NO_DISPLAY && g_ctx != EGL_NO_CONTEXT && g_surf != EGL_NO_SURFACE)
        eglMakeCurrent(g_dpy, g_surf, g_surf, g_ctx);
    if (g_ctx != EGL_NO_CONTEXT && eglGetCurrentContext() == g_ctx) {
        gles_ov_drop_images();
        if (g_rgba_tex)   { glDeleteTextures(1, &g_rgba_tex);   g_rgba_tex = 0; }
        if (g_cursor_tex) { glDeleteTextures(1, &g_cursor_tex); g_cursor_tex = 0; }
        if (g_hud_tex)    { glDeleteTextures(1, &g_hud_tex);    g_hud_tex = 0; }
        if (g_prog_ext)   { glDeleteProgram(g_prog_ext); g_prog_ext = 0; }
        if (g_prog_2d)    { glDeleteProgram(g_prog_2d);  g_prog_2d = 0; }
    }
    g_rgba_w = g_rgba_h = 0;
    g_cursor_w = g_cursor_h = 0;
    g_hud_tex = 0; g_hud_w = g_hud_h = 0;
    if (g_dpy != EGL_NO_DISPLAY) {
        eglMakeCurrent(g_dpy, EGL_NO_SURFACE, EGL_NO_SURFACE, EGL_NO_CONTEXT);
        if (g_surf != EGL_NO_SURFACE) eglDestroySurface(g_dpy, g_surf);
        if (g_ctx != EGL_NO_CONTEXT) eglDestroyContext(g_dpy, g_ctx);
    }
    g_surf = EGL_NO_SURFACE;
    g_ctx = EGL_NO_CONTEXT;
    if (g_win) { ANativeWindow_release(g_win); g_win = NULL; }
    g_renderer[0] = 0;
}

#endif // __ANDROID__
