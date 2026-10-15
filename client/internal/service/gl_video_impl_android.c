// gl_video_impl_android.c — GPU video path for Android.
// Uses SurfaceTexture + AMediaCodec hardware decoding.
// EGL/GL ES 2.0 is used to render the OES external texture to an FBO,
// then glReadPixels delivers RGBA pixels to goVTFrame (CPU readback).
//
// Threading: all EGL/GL calls happen in the decoder thread (dr_submit).
// We make our EGL context current at the start of each render call.

#ifdef __ANDROID__

#include <EGL/egl.h>
#include <EGL/eglext.h>
#include <GLES3/gl3.h>
#include <GLES3/gl3ext.h>
#include <GLES2/gl2ext.h>   // GL_TEXTURE_EXTERNAL_OES, GL_OES_EGL_image_external
#include <android/native_window_jni.h>
#include <android/hardware_buffer.h>
#include <stdatomic.h>
#include <time.h>
#include <jni.h>
#include <android/log.h>
#include <stdlib.h>
#include <string.h>

#define VTAG "USBridge/VideoGL"
#define VLOGI(...) __android_log_print(ANDROID_LOG_INFO,  VTAG, __VA_ARGS__)
#define VLOGE(...) __android_log_print(ANDROID_LOG_ERROR, VTAG, __VA_ARGS__)

// ── State ──────────────────────────────────────────────────────────────────────

static JavaVM    *g_jvm      = NULL;
static jclass     g_cls_vsb  = NULL; // cached io/usbridge/client/VideoSurfaceBridge
static EGLDisplay g_display  = EGL_NO_DISPLAY;
static EGLContext g_context  = EGL_NO_CONTEXT;
static EGLSurface g_surface  = EGL_NO_SURFACE; // tiny pbuffer (just to hold the context)
static GLuint     g_oes_tex  = 0;               // GL_TEXTURE_EXTERNAL_OES
static GLuint     g_fbo      = 0;
static GLuint     g_fbo_tex  = 0;               // RGBA GL_TEXTURE_2D
static GLuint     g_prog     = 0;
static GLint      g_loc_tex  = -1;
static GLint      g_loc_mtx  = -1;
static int        g_fbo_w    = 0;
static int        g_fbo_h    = 0;
static uint8_t   *g_readback = NULL;
static size_t     g_readback_sz = 0;
static jobject    g_surf_ref = NULL; // global ref to android.view.Surface

// GLES 3.0 double-buffered PBO for async glReadPixels (zero GPU stall).
static int     g_gles3      = 0;     // 1 = GLES 3.0 context
static GLuint  g_pbo[2]     = {0,0};
static size_t  g_pbo_sz     = 0;

// EGL access lock: prevents android_gl_release from destroying the context
// while render_and_readback holds it on the decoder thread.
// 0 = idle, 1 = render in progress.
static atomic_int g_egl_busy = 0;
static int     g_pbo_wr     = 0;     // which PBO we just wrote into
static int     g_pbo_primed = 0;     // 1 after first frame (other PBO ready)

// Low-latency path (moonlight_cgo_android.go's g_amc_lowlat): frame-available
// is delivered on its own HandlerThread and waited for with a blocking call
// instead of 0.5 ms sleep polling. Set before android_gl_init.
static int     g_low_latency = 0;

// ── JNI helpers ───────────────────────────────────────────────────────────────

// get_env/detach_env used to attach-then-detach the calling native thread on
// every single call. That's fine for the rare one-off callers (set_jvm,
// release), but android_gl_get_frame's per-frame path (wait_for_frame_available's
// poll loop alone can call this up to 40 times, plus java_update_tex) runs on
// the SAME decoder thread every frame — and on Android's ART, re-attaching a
// thread right after DetachCurrentThread registers a brand-new
// java.lang.Thread wrapper each time (visible as an ever-growing count of
// anonymous "Thread-N" objects in `top`), which is expensive enough to be a
// measurable chunk of CPU/battery at 60fps. Attach the calling thread once
// and leave it attached; detach_env is now a no-op and the one real detach
// happens in android_gl_release() when this render thread is done for good.
static JNIEnv *get_env(int *need_detach) {
    *need_detach = 0;
    if (!g_jvm) return NULL;
    JNIEnv *env = NULL;
    jint rc = (*g_jvm)->GetEnv(g_jvm, (void **)&env, JNI_VERSION_1_6);
    if (rc == JNI_EDETACHED) {
        (*g_jvm)->AttachCurrentThread(g_jvm, &env, NULL);
    }
    return env;
}

static void detach_env(int need_detach) {
    (void)need_detach; // kept for call-site compatibility; see get_env() above
}

// ── GLSL shaders ──────────────────────────────────────────────────────────────

static const char *kVertSrc =
    "attribute vec2 aPos;\n"
    "attribute vec2 aTC;\n"
    "varying   vec2 vTC;\n"
    "uniform   mat4 uMtx;\n"
    "void main() {\n"
    "  gl_Position = vec4(aPos, 0.0, 1.0);\n"
    "  vTC = (uMtx * vec4(aTC, 0.0, 1.0)).xy;\n"
    "}\n";

static const char *kFragSrc =
    "#extension GL_OES_EGL_image_external : require\n"
    "precision mediump float;\n"
    "uniform samplerExternalOES sTexture;\n"
    "varying vec2 vTC;\n"
    "void main() {\n"
    "  gl_FragColor = texture2D(sTexture, vTC);\n"
    "}\n";

static GLuint compile_shader(GLenum type, const char *src) {
    GLuint s = glCreateShader(type);
    glShaderSource(s, 1, &src, NULL);
    glCompileShader(s);
    GLint ok = 0;
    glGetShaderiv(s, GL_COMPILE_STATUS, &ok);
    if (!ok) {
        char buf[512];
        glGetShaderInfoLog(s, sizeof(buf), NULL, buf);
        VLOGE("shader error: %s", buf);
        glDeleteShader(s);
        return 0;
    }
    return s;
}

// ── EGL init ──────────────────────────────────────────────────────────────────

static int egl_init(void) {
    if (g_display != EGL_NO_DISPLAY && g_context != EGL_NO_CONTEXT) {
        return 1; // Already initialized
    }
    g_display = eglGetDisplay(EGL_DEFAULT_DISPLAY);
    if (g_display == EGL_NO_DISPLAY) { VLOGE("eglGetDisplay failed"); return 0; }
    eglInitialize(g_display, NULL, NULL);

    // Try GLES 3.0 first — enables PBO async readback.
    EGLConfig cfg = 0;
    EGLint n = 0;
#ifdef EGL_OPENGL_ES3_BIT_KHR
    EGLint attribs3[] = {
        EGL_RENDERABLE_TYPE, EGL_OPENGL_ES3_BIT_KHR,
        EGL_SURFACE_TYPE,    EGL_PBUFFER_BIT,
        EGL_RED_SIZE, 8, EGL_GREEN_SIZE, 8, EGL_BLUE_SIZE, 8, EGL_ALPHA_SIZE, 8,
        EGL_NONE
    };
    if (eglChooseConfig(g_display, attribs3, &cfg, 1, &n) && n > 0) {
        EGLint ctx3[] = { EGL_CONTEXT_CLIENT_VERSION, 3, EGL_NONE };
        g_context = eglCreateContext(g_display, cfg, EGL_NO_CONTEXT, ctx3);
        if (g_context != EGL_NO_CONTEXT) {
            g_gles3 = 1;
        }
    }
#endif
    // Fall back to GLES 2.0 if 3.0 unavailable.
    if (g_context == EGL_NO_CONTEXT) {
        EGLint attribs2[] = {
            EGL_RENDERABLE_TYPE, EGL_OPENGL_ES2_BIT,
            EGL_SURFACE_TYPE,    EGL_PBUFFER_BIT,
            EGL_RED_SIZE, 8, EGL_GREEN_SIZE, 8, EGL_BLUE_SIZE, 8, EGL_ALPHA_SIZE, 8,
            EGL_NONE
        };
        n = 0;
        if (!eglChooseConfig(g_display, attribs2, &cfg, 1, &n) || n == 0) {
            VLOGE("eglChooseConfig failed");
            return 0;
        }
        EGLint ctx2[] = { EGL_CONTEXT_CLIENT_VERSION, 2, EGL_NONE };
        g_context = eglCreateContext(g_display, cfg, EGL_NO_CONTEXT, ctx2);
        if (g_context == EGL_NO_CONTEXT) { VLOGE("eglCreateContext failed"); return 0; }
    }

    EGLint pbuf[] = { EGL_WIDTH, 16, EGL_HEIGHT, 16, EGL_NONE };
    g_surface = eglCreatePbufferSurface(g_display, cfg, pbuf);
    if (g_surface == EGL_NO_SURFACE) { VLOGE("eglCreatePbufferSurface failed"); return 0; }

    if (!eglMakeCurrent(g_display, g_surface, g_surface, g_context)) {
        VLOGE("eglMakeCurrent failed");
        return 0;
    }
    VLOGI("EGL GL ES %s context ready%s", g_gles3 ? "3.0" : "2.0",
          g_gles3 ? " (PBO async readback)" : "");
    return 1;
}

// ── GL resource setup ─────────────────────────────────────────────────────────

static int gl_setup(void) {
    glGenTextures(1, &g_oes_tex);
    glBindTexture(GL_TEXTURE_EXTERNAL_OES, g_oes_tex);
    glTexParameteri(GL_TEXTURE_EXTERNAL_OES, GL_TEXTURE_MIN_FILTER, GL_LINEAR);
    glTexParameteri(GL_TEXTURE_EXTERNAL_OES, GL_TEXTURE_MAG_FILTER, GL_LINEAR);
    glTexParameteri(GL_TEXTURE_EXTERNAL_OES, GL_TEXTURE_WRAP_S, GL_CLAMP_TO_EDGE);
    glTexParameteri(GL_TEXTURE_EXTERNAL_OES, GL_TEXTURE_WRAP_T, GL_CLAMP_TO_EDGE);

    glGenFramebuffers(1, &g_fbo);
    glGenTextures(1, &g_fbo_tex);

    GLuint vs = compile_shader(GL_VERTEX_SHADER,   kVertSrc);
    GLuint fs = compile_shader(GL_FRAGMENT_SHADER, kFragSrc);
    if (!vs || !fs) { glDeleteShader(vs); glDeleteShader(fs); return 0; }

    g_prog = glCreateProgram();
    glAttachShader(g_prog, vs);
    glAttachShader(g_prog, fs);
    glBindAttribLocation(g_prog, 0, "aPos");
    glBindAttribLocation(g_prog, 1, "aTC");
    glLinkProgram(g_prog);
    glDeleteShader(vs);
    glDeleteShader(fs);

    GLint ok = 0;
    glGetProgramiv(g_prog, GL_LINK_STATUS, &ok);
    if (!ok) {
        char buf[512];
        glGetProgramInfoLog(g_prog, sizeof(buf), NULL, buf);
        VLOGE("program link error: %s", buf);
        glDeleteProgram(g_prog);
        g_prog = 0;
        return 0;
    }
    g_loc_tex = glGetUniformLocation(g_prog, "sTexture");
    g_loc_mtx = glGetUniformLocation(g_prog, "uMtx");

    if (g_gles3) {
        glGenBuffers(2, g_pbo);
        VLOGI("PBO double-buffered async readback enabled");
    }
    VLOGI("GL resources ready (oes=%u fbo=%u prog=%u gles3=%d)", g_oes_tex, g_fbo, g_prog, g_gles3);
    return 1;
}

// ── Java bridge calls ─────────────────────────────────────────────────────────

static ANativeWindow *java_create_surface(int width, int height) {
    int detach;
    JNIEnv *env = get_env(&detach);
    if (!env) { VLOGE("no JNI env"); return NULL; }

    if (!g_cls_vsb) {
        VLOGE("VideoSurfaceBridge class not cached");
        detach_env(detach);
        return NULL;
    }
    jclass cls = g_cls_vsb;

    jmethodID mid_create = (*env)->GetStaticMethodID(env, cls,
        "createSurface", "(IZ)Landroid/view/Surface;");
    if (!mid_create || (*env)->ExceptionCheck(env)) {
        (*env)->ExceptionClear(env);
        VLOGE("createSurface method not found");
        detach_env(detach);
        return NULL;
    }

    jobject surf = (*env)->CallStaticObjectMethod(env, cls, mid_create, (jint)g_oes_tex,
                                                  (jboolean)(g_low_latency ? JNI_TRUE : JNI_FALSE));
    if ((*env)->ExceptionCheck(env)) { (*env)->ExceptionClear(env); surf = NULL; }
    if (!surf) {
        VLOGE("createSurface returned null");
        detach_env(detach);
        return NULL;
    }

    jmethodID mid_size = (*env)->GetStaticMethodID(env, cls, "setDefaultBufferSize", "(II)V");
    if (mid_size) (*env)->CallStaticVoidMethod(env, cls, mid_size, (jint)width, (jint)height);
    if ((*env)->ExceptionCheck(env)) (*env)->ExceptionClear(env);

    if (g_surf_ref) (*env)->DeleteGlobalRef(env, g_surf_ref);
    g_surf_ref = (*env)->NewGlobalRef(env, surf);

    ANativeWindow *win = ANativeWindow_fromSurface(env, surf);
    detach_env(detach);
    VLOGI("ANativeWindow=%p for %dx%d", (void*)win, width, height);
    return win;
}

static void java_update_tex(void) {
    if (!g_cls_vsb) return;
    int detach;
    JNIEnv *env = get_env(&detach);
    if (!env) return;
    jclass cls = g_cls_vsb;
    jmethodID mid = (*env)->GetStaticMethodID(env, cls, "updateTexImage", "()V");
    if (!mid || (*env)->ExceptionCheck(env)) { (*env)->ExceptionClear(env); detach_env(detach); return; }
    (*env)->CallStaticVoidMethod(env, cls, mid);
    if ((*env)->ExceptionCheck(env)) (*env)->ExceptionClear(env);
    detach_env(detach);
}

// Returns 1 if a fresh decoded frame has actually reached the SurfaceTexture's
// BufferQueue (VideoSurfaceBridge's onFrameAvailable fired), 0 otherwise.
static int java_consume_frame_available(void) {
    if (!g_cls_vsb) return 0;
    int detach;
    JNIEnv *env = get_env(&detach);
    if (!env) return 0;
    jclass cls = g_cls_vsb;
    jmethodID mid = (*env)->GetStaticMethodID(env, cls, "consumeFrameAvailable", "()Z");
    if (!mid || (*env)->ExceptionCheck(env)) { (*env)->ExceptionClear(env); detach_env(detach); return 0; }
    jboolean ret = (*env)->CallStaticBooleanMethod(env, cls, mid);
    if ((*env)->ExceptionCheck(env)) { (*env)->ExceptionClear(env); ret = 0; }
    detach_env(detach);
    return ret ? 1 : 0;
}

// Wait (briefly) until the decoded buffer we just released has actually
// reached the SurfaceTexture consumer side. Calling updateTexImage() before
// that happens re-latches the PREVIOUS frame instead of the new one — a
// documented SurfaceTexture race that shows up as ghosting/smearing, worse
// against encoders with jittery per-frame completion timing (e.g. Allwinner
// VE2 h264_omx). Bounded to avoid stalling the decoder thread indefinitely if
// something upstream never signals.
// java_wait_frame_available blocks until VideoSurfaceBridge's
// onFrameAvailable fires (or timeout_ms passes) and consumes it.
static int java_wait_frame_available(int timeout_ms) {
    if (!g_cls_vsb) return 0;
    int detach;
    JNIEnv *env = get_env(&detach);
    if (!env) return 0;
    static jmethodID mid = NULL;
    if (!mid) mid = (*env)->GetStaticMethodID(env, g_cls_vsb, "waitFrameAvailable", "(I)Z");
    if (!mid || (*env)->ExceptionCheck(env)) { (*env)->ExceptionClear(env); mid = NULL; return 0; }
    jboolean ret = (*env)->CallStaticBooleanMethod(env, g_cls_vsb, mid, (jint)timeout_ms);
    if ((*env)->ExceptionCheck(env)) { (*env)->ExceptionClear(env); ret = 0; }
    detach_env(detach);
    return ret ? 1 : 0;
}

static void wait_for_frame_available(void) {
    if (g_low_latency) {
        if (!java_wait_frame_available(20))
            VLOGE("wait_for_frame_available: timed out — latching whatever is current");
        return;
    }
    for (int i = 0; i < 40; i++) { // up to ~20ms
        if (java_consume_frame_available()) return;
        struct timespec ts = {0, 500000}; // 0.5ms
        nanosleep(&ts, NULL);
    }
    VLOGE("wait_for_frame_available: timed out — latching whatever is current");
}

static void java_get_transform(float mtx[16]) {
    // identity fallback
    memset(mtx, 0, 16 * sizeof(float));
    mtx[0] = mtx[5] = mtx[10] = mtx[15] = 1.0f;

    if (!g_cls_vsb) return;
    int detach;
    JNIEnv *env = get_env(&detach);
    if (!env) return;
    jclass cls = g_cls_vsb;
    jmethodID mid = (*env)->GetStaticMethodID(env, cls, "getTransformMatrix", "([F)V");
    if (!mid || (*env)->ExceptionCheck(env)) { (*env)->ExceptionClear(env); detach_env(detach); return; }
    jfloatArray arr = (*env)->NewFloatArray(env, 16);
    if (!arr) { detach_env(detach); return; }
    (*env)->CallStaticVoidMethod(env, cls, mid, arr);
    if (!(*env)->ExceptionCheck(env)) {
        (*env)->GetFloatArrayRegion(env, arr, 0, 16, mtx);
    } else {
        (*env)->ExceptionClear(env);
    }
    (*env)->DeleteLocalRef(env, arr);
    detach_env(detach);
}

// ── FBO resize ────────────────────────────────────────────────────────────────

static void resize_fbo(int w, int h) {
    if (g_fbo_w == w && g_fbo_h == h) return;
    g_fbo_w = w; g_fbo_h = h;

    glBindTexture(GL_TEXTURE_2D, g_fbo_tex);
    glTexImage2D(GL_TEXTURE_2D, 0, GL_RGBA, w, h, 0, GL_RGBA, GL_UNSIGNED_BYTE, NULL);
    glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_MIN_FILTER, GL_NEAREST);
    glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_MAG_FILTER, GL_NEAREST);
    glBindFramebuffer(GL_FRAMEBUFFER, g_fbo);
    glFramebufferTexture2D(GL_FRAMEBUFFER, GL_COLOR_ATTACHMENT0, GL_TEXTURE_2D, g_fbo_tex, 0);

    size_t sz = (size_t)w * (size_t)h * 4;
    if (sz > g_readback_sz) {
        free(g_readback);
        g_readback    = (uint8_t *)malloc(sz);
        g_readback_sz = g_readback ? sz : 0;
    }
    // Resize PBOs to match new frame size.
    if (g_gles3 && g_pbo[0] && sz != g_pbo_sz) {
        for (int i = 0; i < 2; i++) {
            glBindBuffer(GL_PIXEL_PACK_BUFFER, g_pbo[i]);
            glBufferData(GL_PIXEL_PACK_BUFFER, (GLsizeiptr)sz, NULL, GL_STREAM_READ);
        }
        glBindBuffer(GL_PIXEL_PACK_BUFFER, 0);
        g_pbo_sz     = sz;
        g_pbo_primed = 0;  // reset pipeline — first frame will sync
    }
    VLOGI("FBO resized to %dx%d", w, h);
}

// ── Render OES → FBO → readback ───────────────────────────────────────────────

// Full-screen quad: pos(x,y) + texcoord(u,v), triangle-strip order.
// Y positions are flipped so glReadPixels (bottom-up) produces a top-down image
// without any CPU row-reversal on the Go side.
// Decoder-image source (android_gl_get_frame_from_ahb): when set,
// render_and_readback samples this AHardwareBuffer through a per-frame
// EGLImage instead of latching the SurfaceTexture, with g_src_mtx standing in
// for SurfaceTexture's transform (crop + the same vertical flip).
static AHardwareBuffer *g_src_ahb = NULL;
static float            g_src_mtx[16];
static GLuint           g_src_tex = 0;

static EGLImageKHR src_ahb_bind(AHardwareBuffer *ahb);
static void        src_ahb_unbind(EGLImageKHR img);

static const float kVerts[] = {
    -1.0f, +1.0f,  0.0f, 0.0f,
     1.0f, +1.0f,  1.0f, 0.0f,
    -1.0f, -1.0f,  0.0f, 1.0f,
     1.0f, -1.0f,  1.0f, 1.0f,
};

static uint8_t *render_and_readback(int w, int h) {
    // Mark EGL as busy so android_gl_release waits before destroying the context.
    int expected = 0;
    if (!atomic_compare_exchange_strong(&g_egl_busy, &expected, 1)) {
        return NULL; // release already in progress
    }

    // Bind our EGL context to the current thread before any GL/SurfaceTexture call.
    if (!eglMakeCurrent(g_display, g_surface, g_surface, g_context)) {
        VLOGE("eglMakeCurrent failed in render");
        atomic_store(&g_egl_busy, 0);
        return NULL;
    }

    float mtx[16];
    GLuint tex = g_oes_tex;
    EGLImageKHR src_img = EGL_NO_IMAGE_KHR;
    if (g_src_ahb) {
        src_img = src_ahb_bind(g_src_ahb);
        if (src_img == EGL_NO_IMAGE_KHR) {
            eglMakeCurrent(g_display, EGL_NO_SURFACE, EGL_NO_SURFACE, EGL_NO_CONTEXT);
            atomic_store(&g_egl_busy, 0);
            return NULL;
        }
        memcpy(mtx, g_src_mtx, sizeof(mtx));
        tex = g_src_tex;
    } else {
        // Latch the latest decoded frame onto the OES texture — but only after
        // confirming the buffer we released actually arrived at the consumer.
        wait_for_frame_available();
        java_update_tex();
        java_get_transform(mtx);
    }

    resize_fbo(w, h);
    if (!g_readback) {
        src_ahb_unbind(src_img);
        eglMakeCurrent(g_display, EGL_NO_SURFACE, EGL_NO_SURFACE, EGL_NO_CONTEXT);
        atomic_store(&g_egl_busy, 0);
        return NULL;
    }

    // Render OES texture into RGBA FBO.
    glBindFramebuffer(GL_FRAMEBUFFER, g_fbo);
    glViewport(0, 0, w, h);
    glUseProgram(g_prog);

    glActiveTexture(GL_TEXTURE0);
    glBindTexture(GL_TEXTURE_EXTERNAL_OES, tex);
    glUniform1i(g_loc_tex, 0);
    glUniformMatrix4fv(g_loc_mtx, 1, GL_FALSE, mtx);

    glEnableVertexAttribArray(0);
    glEnableVertexAttribArray(1);
    glVertexAttribPointer(0, 2, GL_FLOAT, GL_FALSE, 4 * sizeof(float), kVerts);
    glVertexAttribPointer(1, 2, GL_FLOAT, GL_FALSE, 4 * sizeof(float), kVerts + 2);
    glDrawArrays(GL_TRIANGLE_STRIP, 0, 4);
    glDisableVertexAttribArray(0);
    glDisableVertexAttribArray(1);

    if (g_gles3 && g_pbo[0] && g_pbo_sz == (size_t)w * (size_t)h * 4) {
        // Async PBO path: submit readback for THIS frame into PBO[g_pbo_wr].
        // The GPU schedules async DMA — returns immediately without stalling.
        glBindBuffer(GL_PIXEL_PACK_BUFFER, g_pbo[g_pbo_wr]);
        glReadPixels(0, 0, w, h, GL_RGBA, GL_UNSIGNED_BYTE, 0); // 0 = offset into PBO
        glBindBuffer(GL_PIXEL_PACK_BUFFER, 0);
        glBindFramebuffer(GL_FRAMEBUFFER, 0);

        if (!g_pbo_primed) {
            // First frame: the other PBO has no data yet; do a sync readback to fill g_readback.
            glBindFramebuffer(GL_FRAMEBUFFER, g_fbo);
            glReadPixels(0, 0, w, h, GL_RGBA, GL_UNSIGNED_BYTE, g_readback);
            glBindFramebuffer(GL_FRAMEBUFFER, 0);
            g_pbo_primed = 1;
        } else {
            // Map the PREVIOUS PBO — GPU DMA completed a full frame ago, no stall.
            int prev = 1 - g_pbo_wr;
            glBindBuffer(GL_PIXEL_PACK_BUFFER, g_pbo[prev]);
            void *ptr = glMapBufferRange(GL_PIXEL_PACK_BUFFER, 0,
                                         (GLsizeiptr)g_pbo_sz, GL_MAP_READ_BIT);
            if (ptr) {
                memcpy(g_readback, ptr, g_pbo_sz);
                glUnmapBuffer(GL_PIXEL_PACK_BUFFER);
            }
            glBindBuffer(GL_PIXEL_PACK_BUFFER, 0);
        }
        g_pbo_wr = 1 - g_pbo_wr;
    } else {
        // GLES 2.0 fallback: synchronous readback (stalls until GPU is done).
        glReadPixels(0, 0, w, h, GL_RGBA, GL_UNSIGNED_BYTE, g_readback);
        glBindFramebuffer(GL_FRAMEBUFFER, 0);
    }
    src_ahb_unbind(src_img);

    // Unbind and release the EGL lock so android_gl_release can proceed.
    eglMakeCurrent(g_display, EGL_NO_SURFACE, EGL_NO_SURFACE, EGL_NO_CONTEXT);
    atomic_store(&g_egl_busy, 0);

    return g_readback;
}

// ── AHardwareBuffer zero-copy render target (Vulkan-active path) ──────────────
//
// render_and_readback above exists for the Fyne-canvas fallback (no Vulkan
// overlay): render OES -> FBO -> glReadPixels -> CPU RGBA -> handed to Go,
// which is unavoidable there since Fyne needs actual pixels on the CPU side.
// When the Vulkan overlay IS active, that CPU round-trip is pure overhead:
// Vulkan only wants the pixels back on the GPU anyway. render_to_hwbuffer
// renders directly into an AHardwareBuffer-backed texture instead of a plain
// one, so Vulkan can import that same buffer as a VkImage
// (VK_ANDROID_external_memory_android_hardware_buffer) and blit straight
// from it — no glReadPixels, no staging-buffer upload, no CPU memcpy at all.
//
// Double-buffered (like the PBO path above) so Vulkan can still be reading
// slot A while GL starts rendering the next frame into slot B; there's no
// cross-API fence between the two GPU work queues here, so glFinish() after
// each render is what actually guarantees Vulkan never observes a
// partially-written buffer -- a deliberate simplicity/latency trade instead
// of wiring up EGL_ANDROID_native_fence_sync <-> VK_KHR_external_semaphore_fd.

#define HWBUF_COUNT 2
static AHardwareBuffer *g_hwbuf[HWBUF_COUNT]     = {NULL, NULL};
static EGLImageKHR      g_hwbuf_img[HWBUF_COUNT] = {EGL_NO_IMAGE_KHR, EGL_NO_IMAGE_KHR};
static GLuint           g_hwbuf_tex[HWBUF_COUNT] = {0, 0};
static GLuint           g_hwbuf_fbo[HWBUF_COUNT] = {0, 0};
static int              g_hwbuf_w = 0, g_hwbuf_h = 0;
static int              g_hwbuf_wr = 0; // slot GL will render into next

typedef EGLImageKHR (EGLAPIENTRYP PFNEGLCREATEIMAGEKHRPROC_L)(EGLDisplay, EGLContext, EGLenum, EGLClientBuffer, const EGLint*);
typedef EGLBoolean  (EGLAPIENTRYP PFNEGLDESTROYIMAGEKHRPROC_L)(EGLDisplay, EGLImageKHR);
typedef void        (GL_APIENTRYP PFNGLEGLIMAGETARGETTEXTURE2DOESPROC_L)(GLenum, GLeglImageOES);
typedef EGLClientBuffer (EGLAPIENTRYP PFNEGLGETNATIVECLIENTBUFFERANDROIDPROC_L)(const AHardwareBuffer*);

static PFNEGLCREATEIMAGEKHRPROC_L            p_eglCreateImageKHR  = NULL;
static PFNEGLDESTROYIMAGEKHRPROC_L           p_eglDestroyImageKHR = NULL;
static PFNGLEGLIMAGETARGETTEXTURE2DOESPROC_L p_glEGLImageTargetTexture2DOES = NULL;
static PFNEGLGETNATIVECLIENTBUFFERANDROIDPROC_L p_eglGetNativeClientBufferANDROID = NULL;

static int hwbuf_load_ext_procs(void) {
    if (!p_eglCreateImageKHR)
        p_eglCreateImageKHR = (PFNEGLCREATEIMAGEKHRPROC_L)eglGetProcAddress("eglCreateImageKHR");
    if (!p_eglDestroyImageKHR)
        p_eglDestroyImageKHR = (PFNEGLDESTROYIMAGEKHRPROC_L)eglGetProcAddress("eglDestroyImageKHR");
    if (!p_glEGLImageTargetTexture2DOES)
        p_glEGLImageTargetTexture2DOES = (PFNGLEGLIMAGETARGETTEXTURE2DOESPROC_L)eglGetProcAddress("glEGLImageTargetTexture2DOES");
    if (!p_eglGetNativeClientBufferANDROID)
        p_eglGetNativeClientBufferANDROID = (PFNEGLGETNATIVECLIENTBUFFERANDROIDPROC_L)eglGetProcAddress("eglGetNativeClientBufferANDROID");
    return p_eglCreateImageKHR && p_eglDestroyImageKHR && p_glEGLImageTargetTexture2DOES && p_eglGetNativeClientBufferANDROID;
}

static void hwbuf_destroy_slot(int i) {
    if (g_hwbuf_fbo[i]) { glDeleteFramebuffers(1, &g_hwbuf_fbo[i]); g_hwbuf_fbo[i] = 0; }
    if (g_hwbuf_tex[i]) { glDeleteTextures(1, &g_hwbuf_tex[i]); g_hwbuf_tex[i] = 0; }
    if (g_hwbuf_img[i] != EGL_NO_IMAGE_KHR && p_eglDestroyImageKHR) {
        p_eglDestroyImageKHR(g_display, g_hwbuf_img[i]);
        g_hwbuf_img[i] = EGL_NO_IMAGE_KHR;
    }
    if (g_hwbuf[i]) { AHardwareBuffer_release(g_hwbuf[i]); g_hwbuf[i] = NULL; }
}

static int hwbuf_ensure_slots(int w, int h) {
    if (g_hwbuf_w == w && g_hwbuf_h == h && g_hwbuf[0] && g_hwbuf[1]) return 1;
    if (!hwbuf_load_ext_procs()) {
        VLOGE("hwbuffer: required EGL/GL extensions unavailable");
        return 0;
    }

    for (int i = 0; i < HWBUF_COUNT; i++) hwbuf_destroy_slot(i);

    AHardwareBuffer_Desc desc;
    memset(&desc, 0, sizeof(desc));
    desc.width  = (uint32_t)w;
    desc.height = (uint32_t)h;
    desc.layers = 1;
    desc.format = AHARDWAREBUFFER_FORMAT_R8G8B8A8_UNORM;
    desc.usage  = AHARDWAREBUFFER_USAGE_GPU_COLOR_OUTPUT | AHARDWAREBUFFER_USAGE_GPU_SAMPLED_IMAGE;

    for (int i = 0; i < HWBUF_COUNT; i++) {
        if (AHardwareBuffer_allocate(&desc, &g_hwbuf[i]) != 0 || !g_hwbuf[i]) {
            VLOGE("hwbuffer: AHardwareBuffer_allocate failed for slot %d", i);
            for (int j = 0; j <= i; j++) hwbuf_destroy_slot(j);
            g_hwbuf_w = g_hwbuf_h = 0;
            return 0;
        }
        EGLClientBuffer clientBuf = p_eglGetNativeClientBufferANDROID(g_hwbuf[i]);
        if (!clientBuf) {
            VLOGE("hwbuffer: eglGetNativeClientBufferANDROID failed (slot %d)", i);
            hwbuf_destroy_slot(i);
            g_hwbuf_w = g_hwbuf_h = 0;
            return 0;
        }
        const EGLint imgAttrs[] = { EGL_IMAGE_PRESERVED_KHR, EGL_TRUE, EGL_NONE };
        g_hwbuf_img[i] = p_eglCreateImageKHR(g_display, EGL_NO_CONTEXT,
            EGL_NATIVE_BUFFER_ANDROID, clientBuf, imgAttrs);
        if (g_hwbuf_img[i] == EGL_NO_IMAGE_KHR) {
            VLOGE("hwbuffer: eglCreateImageKHR failed (slot %d)", i);
            hwbuf_destroy_slot(i);
            g_hwbuf_w = g_hwbuf_h = 0;
            return 0;
        }

        glGenTextures(1, &g_hwbuf_tex[i]);
        glBindTexture(GL_TEXTURE_2D, g_hwbuf_tex[i]);
        glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_MIN_FILTER, GL_NEAREST);
        glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_MAG_FILTER, GL_NEAREST);
        glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_WRAP_S, GL_CLAMP_TO_EDGE);
        glTexParameteri(GL_TEXTURE_2D, GL_TEXTURE_WRAP_T, GL_CLAMP_TO_EDGE);
        p_glEGLImageTargetTexture2DOES(GL_TEXTURE_2D, (GLeglImageOES)g_hwbuf_img[i]);

        glGenFramebuffers(1, &g_hwbuf_fbo[i]);
        glBindFramebuffer(GL_FRAMEBUFFER, g_hwbuf_fbo[i]);
        glFramebufferTexture2D(GL_FRAMEBUFFER, GL_COLOR_ATTACHMENT0, GL_TEXTURE_2D, g_hwbuf_tex[i], 0);
        GLenum status = glCheckFramebufferStatus(GL_FRAMEBUFFER);
        if (status != GL_FRAMEBUFFER_COMPLETE) {
            VLOGE("hwbuffer: FBO incomplete for slot %d: 0x%x", i, status);
            glBindFramebuffer(GL_FRAMEBUFFER, 0);
            hwbuf_destroy_slot(i);
            g_hwbuf_w = g_hwbuf_h = 0;
            return 0;
        }
    }
    glBindFramebuffer(GL_FRAMEBUFFER, 0);
    g_hwbuf_w = w; g_hwbuf_h = h;
    VLOGI("hwbuffer: allocated %dx%d double-buffered zero-copy render targets", w, h);
    return 1;
}

static AHardwareBuffer *render_to_hwbuffer(int w, int h) {
    int expected = 0;
    if (!atomic_compare_exchange_strong(&g_egl_busy, &expected, 1)) {
        return NULL; // release already in progress
    }

    if (!eglMakeCurrent(g_display, g_surface, g_surface, g_context)) {
        VLOGE("eglMakeCurrent failed in render_to_hwbuffer");
        atomic_store(&g_egl_busy, 0);
        return NULL;
    }

    wait_for_frame_available();
    java_update_tex();

    float mtx[16];
    java_get_transform(mtx);

    if (!hwbuf_ensure_slots(w, h)) {
        eglMakeCurrent(g_display, EGL_NO_SURFACE, EGL_NO_SURFACE, EGL_NO_CONTEXT);
        atomic_store(&g_egl_busy, 0);
        return NULL;
    }

    int slot = g_hwbuf_wr;
    glBindFramebuffer(GL_FRAMEBUFFER, g_hwbuf_fbo[slot]);
    glViewport(0, 0, w, h);
    glUseProgram(g_prog);

    glActiveTexture(GL_TEXTURE0);
    glBindTexture(GL_TEXTURE_EXTERNAL_OES, g_oes_tex);
    glUniform1i(g_loc_tex, 0);
    glUniformMatrix4fv(g_loc_mtx, 1, GL_FALSE, mtx);

    glEnableVertexAttribArray(0);
    glEnableVertexAttribArray(1);
    glVertexAttribPointer(0, 2, GL_FLOAT, GL_FALSE, 4 * sizeof(float), kVerts);
    glVertexAttribPointer(1, 2, GL_FLOAT, GL_FALSE, 4 * sizeof(float), kVerts + 2);
    glDrawArrays(GL_TRIANGLE_STRIP, 0, 4);
    glDisableVertexAttribArray(0);
    glDisableVertexAttribArray(1);

    glBindFramebuffer(GL_FRAMEBUFFER, 0);
    glFinish(); // see file comment above: stands in for a cross-API GPU fence

    eglMakeCurrent(g_display, EGL_NO_SURFACE, EGL_NO_SURFACE, EGL_NO_CONTEXT);
    atomic_store(&g_egl_busy, 0);

    g_hwbuf_wr = 1 - g_hwbuf_wr;
    return g_hwbuf[slot];
}

// src_ahb_bind wraps g_src_ahb in an EGLImage on g_src_tex (EGL context
// current). The image only lives for one render_and_readback.
static EGLImageKHR src_ahb_bind(AHardwareBuffer *ahb) {
    if (!hwbuf_load_ext_procs()) return EGL_NO_IMAGE_KHR;
    EGLClientBuffer cb = p_eglGetNativeClientBufferANDROID(ahb);
    if (!cb) return EGL_NO_IMAGE_KHR;
    const EGLint attrs[] = { EGL_IMAGE_PRESERVED_KHR, EGL_TRUE, EGL_NONE };
    EGLImageKHR img = p_eglCreateImageKHR(g_display, EGL_NO_CONTEXT, EGL_NATIVE_BUFFER_ANDROID, cb, attrs);
    if (img == EGL_NO_IMAGE_KHR) { VLOGE("src_ahb_bind: eglCreateImageKHR failed"); return img; }
    if (!g_src_tex) {
        glGenTextures(1, &g_src_tex);
        glBindTexture(GL_TEXTURE_EXTERNAL_OES, g_src_tex);
        glTexParameteri(GL_TEXTURE_EXTERNAL_OES, GL_TEXTURE_MIN_FILTER, GL_LINEAR);
        glTexParameteri(GL_TEXTURE_EXTERNAL_OES, GL_TEXTURE_MAG_FILTER, GL_LINEAR);
        glTexParameteri(GL_TEXTURE_EXTERNAL_OES, GL_TEXTURE_WRAP_S, GL_CLAMP_TO_EDGE);
        glTexParameteri(GL_TEXTURE_EXTERNAL_OES, GL_TEXTURE_WRAP_T, GL_CLAMP_TO_EDGE);
    }
    glBindTexture(GL_TEXTURE_EXTERNAL_OES, g_src_tex);
    p_glEGLImageTargetTexture2DOES(GL_TEXTURE_EXTERNAL_OES, (GLeglImageOES)img);
    return img;
}

static void src_ahb_unbind(EGLImageKHR img) {
    if (img != EGL_NO_IMAGE_KHR && p_eglDestroyImageKHR) p_eglDestroyImageKHR(g_display, img);
}

// ── Public C API (called from moonlight_cgo_android.go) ───────────────────────

// android_gl_get_frame_from_ahb reads a decoder image (AImageReader buffer)
// back as top-down RGBA of its crop rect, for the paths that need CPU pixels
// (Net Graph HUD, no Vulkan overlay). Returns NULL on failure.
uint8_t *android_gl_get_frame_from_ahb(void *ahb_void, int crop_x, int crop_y, int crop_w, int crop_h) {
    if (g_context == EGL_NO_CONTEXT || !g_prog || !ahb_void || crop_w <= 0 || crop_h <= 0) return NULL;
    AHardwareBuffer *ahb = (AHardwareBuffer *)ahb_void;
    AHardwareBuffer_Desc d;
    AHardwareBuffer_describe(ahb, &d);
    float bw = (float)d.width, bh = (float)d.height;
    // Same convention as SurfaceTexture's matrix: v = 0 is the image's
    // bottom row, so render_and_readback's flip comes out top-down.
    memset(g_src_mtx, 0, sizeof(g_src_mtx));
    g_src_mtx[0]  = (float)crop_w / bw;
    g_src_mtx[5]  = -(float)crop_h / bh;
    g_src_mtx[10] = 1.0f;
    g_src_mtx[12] = (float)crop_x / bw;
    g_src_mtx[13] = (float)(crop_y + crop_h) / bh;
    g_src_mtx[15] = 1.0f;
    g_src_ahb = ahb;
    uint8_t *out = render_and_readback(crop_w, crop_h);
    g_src_ahb = NULL;
    return out;
}

void android_gl_set_jvm(JavaVM *jvm, jobject ctx) {
    if (g_jvm != NULL && g_cls_vsb != NULL) {
        return; // Already initialized
    }
    
    g_jvm = jvm;
    int detach;
    JNIEnv *env = get_env(&detach);
    if (env) {
        // Try to find class normally first
        jclass cls = (*env)->FindClass(env, "io/usbridge/client/VideoSurfaceBridge");
        
        if (!cls) {
            if ((*env)->ExceptionCheck(env)) (*env)->ExceptionClear(env);
            VLOGI("android_gl_set_jvm: fallback to ClassLoader for VideoSurfaceBridge");
            
            if (ctx) {
                jclass contextClass = (*env)->GetObjectClass(env, ctx);
                jmethodID getClassLoaderMethod = (*env)->GetMethodID(env, contextClass, "getClassLoader", "()Ljava/lang/ClassLoader;");
                jobject classLoader = (*env)->CallObjectMethod(env, ctx, getClassLoaderMethod);
                
                jclass classLoaderClass = (*env)->FindClass(env, "java/lang/ClassLoader");
                jmethodID loadClassMethod = (*env)->GetMethodID(env, classLoaderClass, "loadClass", "(Ljava/lang/String;)Ljava/lang/Class;");
                
                jstring className = (*env)->NewStringUTF(env, "io.usbridge.client.VideoSurfaceBridge");
                cls = (jclass)(*env)->CallObjectMethod(env, classLoader, loadClassMethod, className);
                
                if ((*env)->ExceptionCheck(env)) {
                    (*env)->ExceptionClear(env);
                    cls = NULL;
                }
                
                (*env)->DeleteLocalRef(env, className);
                (*env)->DeleteLocalRef(env, classLoaderClass);
                (*env)->DeleteLocalRef(env, classLoader);
                (*env)->DeleteLocalRef(env, contextClass);
            }
        }

        if (cls) {
            g_cls_vsb = (jclass)(*env)->NewGlobalRef(env, cls);
            (*env)->DeleteLocalRef(env, cls);
        } else {
            VLOGE("android_gl_set_jvm: could not find VideoSurfaceBridge class even with ClassLoader");
        }
        detach_env(detach);
    }
    VLOGI("JavaVM set (g_cls_vsb=%p)", g_cls_vsb);
}

// Initialize EGL + GL ES + SurfaceTexture.
// Returns the ANativeWindow* to pass to AMediaCodec_configure(), or NULL.
// Must be called from the decoder thread (or any thread that will own the GL context).
ANativeWindow *android_gl_init(int width, int height) {
    if (g_context != EGL_NO_CONTEXT) {
        // Already initialized — just resize and return existing window.
        VLOGI("android_gl_init: already initialized");
        return java_create_surface(width, height);
    }
    if (!egl_init()) { VLOGE("egl_init failed"); return NULL; }
    if (!gl_setup())  { VLOGE("gl_setup failed"); return NULL; }
    
    ANativeWindow *win = java_create_surface(width, height);
    
    // Unbind context so it can be bound in the render thread (dr_submit).
    // Failing to do this causes EGL_BAD_ACCESS when render thread calls eglMakeCurrent.
    eglMakeCurrent(g_display, EGL_NO_SURFACE, EGL_NO_SURFACE, EGL_NO_CONTEXT);
    
    return win;
}

// Render the last decoded frame and return RGBA pixels.
// Must be called after AMediaCodec_releaseOutputBuffer(..., true).
// Returns a pointer to width*height*4 bytes (valid until next call), or NULL.
uint8_t *android_gl_get_frame(int width, int height) {
    if (g_context == EGL_NO_CONTEXT || !g_prog) return NULL;
    return render_and_readback(width, height);
}

// android_gl_get_frame_hwbuffer is the zero-copy counterpart of
// android_gl_get_frame for when the Vulkan overlay is active (see the
// comment above render_to_hwbuffer). Returns an AHardwareBuffer* (as void*
// so callers outside this file don't need <android/hardware_buffer.h>) that
// stays valid until this same slot is reused two calls from now, or NULL.
void *android_gl_get_frame_hwbuffer(int width, int height) {
    if (g_context == EGL_NO_CONTEXT || !g_prog) return NULL;
    return (void *)render_to_hwbuffer(width, height);
}

// android_decoder_best_size asks VideoSurfaceBridge.bestDecoderSize for the
// largest size up to w x h (same aspect) the hardware decoder for mime can
// be configured with. out = {width, height, max fps there}. Returns 0 when
// there is no answer (no JVM yet, no hardware decoder).
int android_decoder_best_size(const char *mime, int w, int h, int *out) {
    if (!g_cls_vsb) return 0;
    int detach;
    JNIEnv *env = get_env(&detach);
    if (!env) return 0;
    int ok = 0;
    jmethodID mid = (*env)->GetStaticMethodID(env, g_cls_vsb, "bestDecoderSize", "(Ljava/lang/String;II)[I");
    if (mid && !(*env)->ExceptionCheck(env)) {
        jstring jm = (*env)->NewStringUTF(env, mime);
        jintArray arr = (jintArray)(*env)->CallStaticObjectMethod(env, g_cls_vsb, mid, jm, (jint)w, (jint)h);
        if (!(*env)->ExceptionCheck(env) && arr && (*env)->GetArrayLength(env, arr) >= 3) {
            jint v[3];
            (*env)->GetIntArrayRegion(env, arr, 0, 3, v);
            out[0] = v[0]; out[1] = v[1]; out[2] = v[2];
            ok = 1;
        }
        if (arr) (*env)->DeleteLocalRef(env, arr);
        (*env)->DeleteLocalRef(env, jm);
    }
    if ((*env)->ExceptionCheck(env)) (*env)->ExceptionClear(env);
    detach_env(detach);
    return ok;
}

void android_gl_set_low_latency(int on) { g_low_latency = on ? 1 : 0; }

// android_display_max_refresh asks VideoSurfaceBridge.maxRefreshRate for the
// display's highest refresh rate (Hz, rounded), 0 if unknown.
int android_display_max_refresh(void) {
    if (!g_cls_vsb) return 0;
    int detach;
    JNIEnv *env = get_env(&detach);
    if (!env) return 0;
    jint hz = 0;
    jmethodID mid = (*env)->GetStaticMethodID(env, g_cls_vsb, "maxRefreshRate", "()I");
    if (mid && !(*env)->ExceptionCheck(env)) hz = (*env)->CallStaticIntMethod(env, g_cls_vsb, mid);
    if ((*env)->ExceptionCheck(env)) { (*env)->ExceptionClear(env); hz = 0; }
    detach_env(detach);
    return (int)hz;
}

// android_gl_log_vendor_params logs decoder `name`'s vendor parameters
// (VideoSurfaceBridge.logVendorParameters).
void android_gl_log_vendor_params(const char *name) {
    if (!g_cls_vsb || !name) return;
    int detach;
    JNIEnv *env = get_env(&detach);
    if (!env) return;
    jmethodID mid = (*env)->GetStaticMethodID(env, g_cls_vsb, "logVendorParameters", "(Ljava/lang/String;)V");
    if (mid && !(*env)->ExceptionCheck(env)) {
        jstring jn = (*env)->NewStringUTF(env, name);
        (*env)->CallStaticVoidMethod(env, g_cls_vsb, mid, jn);
        (*env)->DeleteLocalRef(env, jn);
    }
    if ((*env)->ExceptionCheck(env)) (*env)->ExceptionClear(env);
    detach_env(detach);
}

// android_gl_detach_thread balances get_env()'s standing attach for a thread
// that is about to exit (moonlight_cgo_android.go's output thread).
void android_gl_detach_thread(void) {
    if (!g_jvm) return;
    JNIEnv *env = NULL;
    if ((*g_jvm)->GetEnv(g_jvm, (void **)&env, JNI_VERSION_1_6) == JNI_OK)
        (*g_jvm)->DetachCurrentThread(g_jvm);
}

void android_gl_release(void) {
    if (g_context == EGL_NO_CONTEXT) return;

    // Wait for any in-progress render_and_readback to release the EGL context.
    // Avoids EGL_BAD_ACCESS when both the render thread and the cleanup thread
    // try to eglMakeCurrent simultaneously.
    int waited = 0;
    while (atomic_load(&g_egl_busy)) {
        struct timespec ts = {0, 500000}; // 0.5ms
        nanosleep(&ts, NULL);
        if (++waited > 200) { VLOGE("android_gl_release: timeout waiting for render"); break; }
    }

    eglMakeCurrent(g_display, g_surface, g_surface, g_context);

    if (g_gles3 && g_pbo[0]) {
        glDeleteBuffers(2, g_pbo);
        g_pbo[0] = g_pbo[1] = 0;
        g_pbo_sz = 0; g_pbo_primed = 0; g_pbo_wr = 0;
    }
    if (g_prog)    { glDeleteProgram(g_prog);        g_prog = 0; }
    if (g_fbo)     { glDeleteFramebuffers(1, &g_fbo); g_fbo = 0; }
    if (g_fbo_tex) { glDeleteTextures(1, &g_fbo_tex); g_fbo_tex = 0; }
    if (g_oes_tex) { glDeleteTextures(1, &g_oes_tex); g_oes_tex = 0; }
    if (g_src_tex) { glDeleteTextures(1, &g_src_tex); g_src_tex = 0; }
    for (int i = 0; i < HWBUF_COUNT; i++) hwbuf_destroy_slot(i);
    g_hwbuf_w = g_hwbuf_h = 0; g_hwbuf_wr = 0;

    eglMakeCurrent(g_display, EGL_NO_SURFACE, EGL_NO_SURFACE, EGL_NO_CONTEXT);
    eglDestroyContext(g_display, g_context); g_context = EGL_NO_CONTEXT;
    eglDestroySurface(g_display, g_surface); g_surface = EGL_NO_SURFACE;
    eglTerminate(g_display);                 g_display = EGL_NO_DISPLAY;

    free(g_readback); g_readback = NULL; g_readback_sz = 0;
    g_fbo_w = 0; g_fbo_h = 0;

    if (g_surf_ref) {
        int detach; JNIEnv *env = get_env(&detach);
        if (env) {
            if (g_cls_vsb) {
                jmethodID mid = (*env)->GetStaticMethodID(env, g_cls_vsb, "release", "()V");
                if (mid) (*env)->CallStaticVoidMethod(env, g_cls_vsb, mid);
                if ((*env)->ExceptionCheck(env)) (*env)->ExceptionClear(env);
            }
            (*env)->DeleteGlobalRef(env, g_surf_ref);
            detach_env(detach);
        }
        g_surf_ref = NULL;
    }

    // Real detach for whichever thread is calling release — the one place
    // in this file that still balances the attach get_env() left standing
    // (see get_env()'s comment). Harmless no-op if this thread was never
    // attached (e.g. the "safety cleanup" caller in moonlight_cgo_android.go
    // when dr_cleanup already handled it on the actual decoder thread).
    if (g_jvm) {
        JNIEnv *env = NULL;
        if ((*g_jvm)->GetEnv(g_jvm, (void **)&env, JNI_VERSION_1_6) == JNI_OK) {
            (*g_jvm)->DetachCurrentThread(g_jvm);
        }
    }

    VLOGI("android_gl_release done");
}

#endif // __ANDROID__
