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
#include <jni.h>
#include <android/log.h>
#include <stdlib.h>
#include <string.h>

#define VTAG "USBridge/VideoGL"
#define VLOGI(...) __android_log_print(ANDROID_LOG_INFO,  VTAG, __VA_ARGS__)
#define VLOGE(...) __android_log_print(ANDROID_LOG_ERROR, VTAG, __VA_ARGS__)

// ── State ──────────────────────────────────────────────────────────────────────

static JavaVM    *g_jvm      = NULL;
static jclass     g_cls_vsb  = NULL; // cached com/usbridge/client/VideoSurfaceBridge
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
static int     g_pbo_wr     = 0;     // which PBO we just wrote into
static int     g_pbo_primed = 0;     // 1 after first frame (other PBO ready)

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
static const float kVerts[] = {
    -1.0f, +1.0f,  0.0f, 0.0f,
     1.0f, +1.0f,  1.0f, 0.0f,
    -1.0f, -1.0f,  0.0f, 1.0f,
     1.0f, -1.0f,  1.0f, 1.0f,
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

    // Unbind so cleanup on another thread doesn't fail with EGL_BAD_ACCESS.
    eglMakeCurrent(g_display, EGL_NO_SURFACE, EGL_NO_SURFACE, EGL_NO_CONTEXT);

    return g_readback;
}

// ── Public C API (called from moonlight_cgo_android.go) ───────────────────────

void android_gl_set_jvm(JavaVM *jvm, jobject ctx) {
    if (g_jvm != NULL && g_cls_vsb != NULL) {
        return; // Already initialized
    }
    
    g_jvm = jvm;
    int detach;
    JNIEnv *env = get_env(&detach);
    if (env) {
        // Try to find class normally first
        jclass cls = (*env)->FindClass(env, "com/usbridge/client/VideoSurfaceBridge");
        
        if (!cls) {
            if ((*env)->ExceptionCheck(env)) (*env)->ExceptionClear(env);
            VLOGI("android_gl_set_jvm: fallback to ClassLoader for VideoSurfaceBridge");
            
            if (ctx) {
                jclass contextClass = (*env)->GetObjectClass(env, ctx);
                jmethodID getClassLoaderMethod = (*env)->GetMethodID(env, contextClass, "getClassLoader", "()Ljava/lang/ClassLoader;");
                jobject classLoader = (*env)->CallObjectMethod(env, ctx, getClassLoaderMethod);
                
                jclass classLoaderClass = (*env)->FindClass(env, "java/lang/ClassLoader");
                jmethodID loadClassMethod = (*env)->GetMethodID(env, classLoaderClass, "loadClass", "(Ljava/lang/String;)Ljava/lang/Class;");
                
                jstring className = (*env)->NewStringUTF(env, "com.usbridge.client.VideoSurfaceBridge");
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

void android_gl_release(void) {
    if (g_context == EGL_NO_CONTEXT) return;

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
    VLOGI("android_gl_release done");
}

#endif // __ANDROID__
