// gl_video_impl_android.c — GPU video path for Android.
// Uses SurfaceTexture + AMediaCodec hardware decoding.
// EGL/GL ES 2.0 is used to render the OES external texture to an FBO,
// then glReadPixels delivers RGBA pixels to goVTFrame (CPU readback).
//
// Threading: all EGL/GL calls happen in the decoder thread (dr_submit).
// We make our EGL context current at the start of each render call.

#ifdef __ANDROID__

#include <EGL/egl.h>
#include <GLES2/gl2.h>
#include <GLES2/gl2ext.h>
#include <android/native_window_jni.h>
#include <jni.h>
#include <android/log.h>
#include <stdlib.h>
#include <string.h>

#define VTAG "USBridge/VideoGL"
#define VLOGI(...) __android_log_print(ANDROID_LOG_INFO,  VTAG, __VA_ARGS__)
#define VLOGE(...) __android_log_print(ANDROID_LOG_ERROR, VTAG, __VA_ARGS__)

// ── State ──────────────────────────────────────────────────────────────────────

static JavaVM    *g_jvm      = NULL;
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

// ── JNI helpers ───────────────────────────────────────────────────────────────

static JNIEnv *get_env(int *need_detach) {
    *need_detach = 0;
    if (!g_jvm) return NULL;
    JNIEnv *env = NULL;
    jint rc = (*g_jvm)->GetEnv(g_jvm, (void **)&env, JNI_VERSION_1_6);
    if (rc == JNI_EDETACHED) {
        (*g_jvm)->AttachCurrentThread(g_jvm, &env, NULL);
        *need_detach = 1;
    }
    return env;
}

static void detach_env(int need_detach) {
    if (need_detach && g_jvm) (*g_jvm)->DetachCurrentThread(g_jvm);
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
    g_display = eglGetDisplay(EGL_DEFAULT_DISPLAY);
    if (g_display == EGL_NO_DISPLAY) { VLOGE("eglGetDisplay failed"); return 0; }
    eglInitialize(g_display, NULL, NULL);

    EGLint attribs[] = {
        EGL_RENDERABLE_TYPE, EGL_OPENGL_ES2_BIT,
        EGL_SURFACE_TYPE,    EGL_PBUFFER_BIT,
        EGL_RED_SIZE,   8,
        EGL_GREEN_SIZE, 8,
        EGL_BLUE_SIZE,  8,
        EGL_ALPHA_SIZE, 8,
        EGL_NONE
    };
    EGLConfig cfg;
    EGLint n = 0;
    if (!eglChooseConfig(g_display, attribs, &cfg, 1, &n) || n == 0) {
        VLOGE("eglChooseConfig failed");
        return 0;
    }

    EGLint ctx_attribs[] = { EGL_CONTEXT_CLIENT_VERSION, 2, EGL_NONE };
    g_context = eglCreateContext(g_display, cfg, EGL_NO_CONTEXT, ctx_attribs);
    if (g_context == EGL_NO_CONTEXT) { VLOGE("eglCreateContext failed"); return 0; }

    EGLint pbuf[] = { EGL_WIDTH, 16, EGL_HEIGHT, 16, EGL_NONE };
    g_surface = eglCreatePbufferSurface(g_display, cfg, pbuf);
    if (g_surface == EGL_NO_SURFACE) { VLOGE("eglCreatePbufferSurface failed"); return 0; }

    if (!eglMakeCurrent(g_display, g_surface, g_surface, g_context)) {
        VLOGE("eglMakeCurrent failed");
        return 0;
    }
    VLOGI("EGL GL ES 2.0 context ready");
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
    VLOGI("GL resources ready (oes=%u fbo=%u prog=%u)", g_oes_tex, g_fbo, g_prog);
    return 1;
}

// ── Java bridge calls ─────────────────────────────────────────────────────────

static ANativeWindow *java_create_surface(int width, int height) {
    int detach;
    JNIEnv *env = get_env(&detach);
    if (!env) { VLOGE("no JNI env"); return NULL; }

    jclass cls = (*env)->FindClass(env, "com/usbridge/client/VideoSurfaceBridge");
    if (!cls || (*env)->ExceptionCheck(env)) {
        (*env)->ExceptionClear(env);
        VLOGE("VideoSurfaceBridge class not found");
        detach_env(detach);
        return NULL;
    }

    jmethodID mid_create = (*env)->GetStaticMethodID(env, cls,
        "createSurface", "(I)Landroid/view/Surface;");
    if (!mid_create || (*env)->ExceptionCheck(env)) {
        (*env)->ExceptionClear(env);
        VLOGE("createSurface method not found");
        detach_env(detach);
        return NULL;
    }

    jobject surf = (*env)->CallStaticObjectMethod(env, cls, mid_create, (jint)g_oes_tex);
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
    int detach;
    JNIEnv *env = get_env(&detach);
    if (!env) return;
    jclass cls = (*env)->FindClass(env, "com/usbridge/client/VideoSurfaceBridge");
    if (!cls || (*env)->ExceptionCheck(env)) { (*env)->ExceptionClear(env); detach_env(detach); return; }
    jmethodID mid = (*env)->GetStaticMethodID(env, cls, "updateTexImage", "()V");
    if (!mid || (*env)->ExceptionCheck(env)) { (*env)->ExceptionClear(env); detach_env(detach); return; }
    (*env)->CallStaticVoidMethod(env, cls, mid);
    if ((*env)->ExceptionCheck(env)) (*env)->ExceptionClear(env);
    detach_env(detach);
}

static void java_get_transform(float mtx[16]) {
    // identity fallback
    memset(mtx, 0, 16 * sizeof(float));
    mtx[0] = mtx[5] = mtx[10] = mtx[15] = 1.0f;

    int detach;
    JNIEnv *env = get_env(&detach);
    if (!env) return;
    jclass cls = (*env)->FindClass(env, "com/usbridge/client/VideoSurfaceBridge");
    if (!cls || (*env)->ExceptionCheck(env)) { (*env)->ExceptionClear(env); detach_env(detach); return; }
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
    VLOGI("FBO resized to %dx%d", w, h);
}

// ── Render OES → FBO → readback ───────────────────────────────────────────────

// Full-screen quad: pos(x,y) + texcoord(u,v), triangle-strip order
static const float kVerts[] = {
    -1.0f, -1.0f,  0.0f, 0.0f,
     1.0f, -1.0f,  1.0f, 0.0f,
    -1.0f,  1.0f,  0.0f, 1.0f,
     1.0f,  1.0f,  1.0f, 1.0f,
};

static uint8_t *render_and_readback(int w, int h) {
    // Bind our EGL context to the current thread before any GL/SurfaceTexture call.
    if (!eglMakeCurrent(g_display, g_surface, g_surface, g_context)) {
        VLOGE("eglMakeCurrent failed in render");
        return NULL;
    }

    // Latch the latest decoded frame onto the OES texture.
    java_update_tex();

    float mtx[16];
    java_get_transform(mtx);

    resize_fbo(w, h);
    if (!g_readback) return NULL;

    // Render OES texture into RGBA FBO.
    glBindFramebuffer(GL_FRAMEBUFFER, g_fbo);
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

    // CPU readback (required for Fyne canvas path).
    glReadPixels(0, 0, w, h, GL_RGBA, GL_UNSIGNED_BYTE, g_readback);
    glBindFramebuffer(GL_FRAMEBUFFER, 0);

    return g_readback;
}

// ── Public C API (called from moonlight_cgo_android.go) ───────────────────────

void android_gl_set_jvm(JavaVM *jvm) {
    g_jvm = jvm;
    VLOGI("JavaVM set");
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
    return java_create_surface(width, height);
}

// Render the last decoded frame and return RGBA pixels.
// Must be called after AMediaCodec_releaseOutputBuffer(..., true).
// Returns a pointer to width*height*4 bytes (valid until next call), or NULL.
uint8_t *android_gl_get_frame(int width, int height) {
    if (g_context == EGL_NO_CONTEXT || !g_prog) return NULL;
    return render_and_readback(width, height);
}

void android_gl_release(void) {
    if (g_context == EGL_NO_CONTEXT) return;

    eglMakeCurrent(g_display, g_surface, g_surface, g_context);

    if (g_prog)    { glDeleteProgram(g_prog);        g_prog = 0; }
    if (g_fbo)     { glDeleteFramebuffers(1, &g_fbo); g_fbo = 0; }
    if (g_fbo_tex) { glDeleteTextures(1, &g_fbo_tex); g_fbo_tex = 0; }
    if (g_oes_tex) { glDeleteTextures(1, &g_oes_tex); g_oes_tex = 0; }

    eglMakeCurrent(g_display, EGL_NO_SURFACE, EGL_NO_SURFACE, EGL_NO_CONTEXT);
    eglDestroyContext(g_display, g_context); g_context = EGL_NO_CONTEXT;
    eglDestroySurface(g_display, g_surface); g_surface = EGL_NO_SURFACE;
    eglTerminate(g_display);                 g_display = EGL_NO_DISPLAY;

    free(g_readback); g_readback = NULL; g_readback_sz = 0;
    g_fbo_w = 0; g_fbo_h = 0;

    if (g_surf_ref) {
        int detach; JNIEnv *env = get_env(&detach);
        if (env) {
            jclass cls = (*env)->FindClass(env, "com/usbridge/client/VideoSurfaceBridge");
            if (cls) {
                jmethodID mid = (*env)->GetStaticMethodID(env, cls, "release", "()V");
                if (mid) (*env)->CallStaticVoidMethod(env, cls, mid);
                if ((*env)->ExceptionCheck(env)) (*env)->ExceptionClear(env);
            }
            (*env)->DeleteGlobalRef(env, g_surf_ref);
            detach_env(detach);
        }
        g_surf_ref = NULL;
    }
    VLOGI("android_gl_release done");
}

#endif // __ANDROID__
