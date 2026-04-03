//go:build android
// +build android

package service

/*
#cgo CFLAGS: -I${SRCDIR}/../../gstreamer-android-dynamic/include/gstreamer-1.0 -I${SRCDIR}/../../gstreamer-android-dynamic/include/glib-2.0 -I${SRCDIR}/../../gstreamer-android-dynamic/lib/glib-2.0/include -I${SRCDIR}/../../gstreamer/build-android-arm64-shared/subprojects/gst-plugins-base/gst-libs
#cgo LDFLAGS: -L${SRCDIR}/../../gstreamer-android-dynamic/lib -L${SRCDIR}/../../android/jniLibs/arm64-v8a -lgstreamer-full-1.0 -llog -lm -ldl -landroid -lEGL -lGLESv2
#include <gst/gst.h>
#include <gst/app/gstappsink.h>
#include <gst/gl/gl.h>
#include <gst/gl/egl/gstgldisplay_egl.h>
#include <EGL/egl.h>
#include <stdlib.h>
#include <string.h>
#include <android/log.h>
#include <jni.h>

#define LOG_TAG "GStreamer"

// Мост JNI для androidmedia (amcvideodec) — плагин ищет эти символы при инициализации
static JavaVM *g_gst_java_vm = NULL;
static jobject g_gst_class_loader = NULL;  // Global ref

// Вызывается из Go через driver.RunNative ДО создания pipeline
void gst_android_set_java_vm_and_context(JavaVM *vm, JNIEnv *env, jobject context) {
    if (!vm || !env || !context) return;
    g_gst_java_vm = vm;
    if (g_gst_class_loader) {
        (*env)->DeleteGlobalRef(env, g_gst_class_loader);
        g_gst_class_loader = NULL;
    }
    jclass ctx_class = (*env)->GetObjectClass(env, context);
    if (!ctx_class) return;
    jmethodID get_class = (*env)->GetMethodID(env, ctx_class, "getClass", "()Ljava/lang/Class;");
    if (!get_class) return;
    jobject cls = (*env)->CallObjectMethod(env, context, get_class);
    if (!cls) return;
    jclass class_class = (*env)->GetObjectClass(env, cls);
    if (!class_class) return;
    jmethodID get_class_loader = (*env)->GetMethodID(env, class_class, "getClassLoader", "()Ljava/lang/ClassLoader;");
    if (!get_class_loader) return;
    jobject loader = (*env)->CallObjectMethod(env, cls, get_class_loader);
    if (loader) {
        g_gst_class_loader = (*env)->NewGlobalRef(env, loader);
    }
    __android_log_print(ANDROID_LOG_INFO, "GStreamer", "gst_android: VM=%p, class_loader=%p", (void*)vm, (void*)g_gst_class_loader);
}

JavaVM* gst_android_get_java_vm(void) {
    return g_gst_java_vm;
}

jobject gst_android_get_application_class_loader(void) {
    return g_gst_class_loader;
}
#define LOGI(...) __android_log_print(ANDROID_LOG_INFO, LOG_TAG, __VA_ARGS__)
#define LOGE(...) __android_log_print(ANDROID_LOG_ERROR, LOG_TAG, __VA_ARGS__)

// Объявление функции регистрации статических плагинов
extern void gst_init_static_plugins(void);

// ═══════════════════════════════════════════════════════════════════════
// Полноценный EGL контекст для amcviddec (аппаратный H.264 → SurfaceTexture)
//
// amcviddec декодирует через MediaCodec → SurfaceTexture → GL external-oes текстура.
// updateTexImage() ТРЕБУЕТ:
//   1) Инициализированный EGLDisplay (eglInitialize)
//   2) EGLContext с корректной конфигурацией
//   3) EGLSurface (хотя бы 1×1 pbuffer)
// Без этого gst_buffer_map на GL-памяти будет падать.
// ═══════════════════════════════════════════════════════════════════════

static EGLDisplay g_egl_display = EGL_NO_DISPLAY;
static EGLContext g_egl_context = EGL_NO_CONTEXT;
static EGLSurface g_egl_surface = EGL_NO_SURFACE;

// Fallback на SW декодер при провале HW — объявление до create_pipeline
static volatile int g_hw_map_fail_count;
static volatile gboolean g_force_sw_decoder;
static volatile gboolean g_reconnect_requested;  // TRUE = Go должен сделать Reconnect; сбрасывается в hw_frame_store_init
static volatile gint64 g_last_rtp_packet_us;
static volatile gint64 g_last_appsink_frame_us;
static gchar *g_cached_h264_hw_decoder = NULL;
static gchar *g_cached_jpeg_hw_decoder = NULL;

#define PIPELINE_MODE_H264 0
#define PIPELINE_MODE_JPEG_RTP 1

static gboolean ensure_egl_context(void) {
    if (g_egl_context != EGL_NO_CONTEXT) return TRUE;

    g_egl_display = eglGetDisplay(EGL_DEFAULT_DISPLAY);
    if (g_egl_display == EGL_NO_DISPLAY) {
        LOGE("❌ eglGetDisplay failed");
        return FALSE;
    }

    EGLint major, minor;
    if (!eglInitialize(g_egl_display, &major, &minor)) {
        LOGE("❌ eglInitialize failed: 0x%x", eglGetError());
        return FALSE;
    }
    LOGI("✅ EGL %d.%d initialized", major, minor);

    EGLint config_attribs[] = {
        EGL_SURFACE_TYPE, EGL_PBUFFER_BIT,
        EGL_RENDERABLE_TYPE, EGL_OPENGL_ES2_BIT,
        EGL_RED_SIZE, 8,
        EGL_GREEN_SIZE, 8,
        EGL_BLUE_SIZE, 8,
        EGL_ALPHA_SIZE, 8,
        EGL_NONE
    };

    EGLConfig config;
    EGLint num_configs;
    if (!eglChooseConfig(g_egl_display, config_attribs, &config, 1, &num_configs) || num_configs == 0) {
        LOGE("❌ eglChooseConfig failed: 0x%x", eglGetError());
        return FALSE;
    }

    EGLint ctx_attribs[] = { EGL_CONTEXT_CLIENT_VERSION, 2, EGL_NONE };
    g_egl_context = eglCreateContext(g_egl_display, config, EGL_NO_CONTEXT, ctx_attribs);
    if (g_egl_context == EGL_NO_CONTEXT) {
        LOGE("❌ eglCreateContext failed: 0x%x", eglGetError());
        return FALSE;
    }

    EGLint pbuf_attribs[] = { EGL_WIDTH, 1, EGL_HEIGHT, 1, EGL_NONE };
    g_egl_surface = eglCreatePbufferSurface(g_egl_display, config, pbuf_attribs);
    if (g_egl_surface == EGL_NO_SURFACE) {
        LOGE("⚠️ eglCreatePbufferSurface failed: 0x%x", eglGetError());
    }

    // Тест: проверяем что контекст можно активировать
    if (eglMakeCurrent(g_egl_display, g_egl_surface, g_egl_surface, g_egl_context)) {
        LOGI("✅ EGL context created & tested: display=%p ctx=%p surface=%p",
             (void*)(intptr_t)g_egl_display, (void*)(intptr_t)g_egl_context, (void*)(intptr_t)g_egl_surface);
        eglMakeCurrent(g_egl_display, EGL_NO_SURFACE, EGL_NO_SURFACE, EGL_NO_CONTEXT);
    } else {
        LOGE("⚠️ eglMakeCurrent test failed: 0x%x", eglGetError());
    }

    return TRUE;
}

static void init_gstreamer() {
    LOGI("Initializing GStreamer (dynamic build)...");

    gst_init(NULL, NULL);
    gst_init_static_plugins();  // gst-full: плагины в одной .so

    // Выводим список доступных плагинов
    GList *plugins = gst_registry_get_plugin_list(gst_registry_get());
    GList *l;
    LOGI("Available static plugins:");
    int count = 0;
    for (l = plugins; l != NULL; l = l->next) {
        GstPlugin *plugin = (GstPlugin *)l->data;
        LOGI("  - %s", gst_plugin_get_name(plugin));
        count++;
    }
    LOGI("Total plugins loaded: %d", count);
    gst_plugin_list_free(plugins);
}

static jmethodID get_optional_method_id(JNIEnv *env, jclass cls, const char *name, const char *sig) {
    jmethodID mid = (*env)->GetMethodID(env, cls, name, sig);
    if (!mid && (*env)->ExceptionCheck(env)) {
        (*env)->ExceptionClear(env);
    }
    return mid;
}

static gchar* find_android_decoder_by_name_fragments(const char *required1, const char *required2);

static JNIEnv* gst_android_get_attached_env(void) {
    if (!g_gst_java_vm) return NULL;

    JNIEnv *env = NULL;
    jint rc = (*g_gst_java_vm)->GetEnv(g_gst_java_vm, (void **)&env, JNI_VERSION_1_6);
    if (rc == JNI_OK) return env;
    if (rc != JNI_EDETACHED) return NULL;
    if ((*g_gst_java_vm)->AttachCurrentThread(g_gst_java_vm, &env, NULL) != JNI_OK) {
        return NULL;
    }
    return env;
}

static gchar* normalize_codec_token(const gchar *value) {
    if (!value) return NULL;

    GString *out = g_string_new(NULL);
    for (const gchar *p = value; *p; p++) {
        if (g_ascii_isalnum(*p)) {
            g_string_append_c(out, g_ascii_tolower(*p));
        }
    }
    return g_string_free(out, FALSE);
}

static gboolean codec_name_looks_software(const gchar *name) {
    if (!name) return TRUE;

    gchar *lower = g_ascii_strdown(name, -1);
    gboolean software =
        strstr(lower, "google") != NULL ||
        strstr(lower, "android") != NULL ||
        strstr(lower, "ffmpeg") != NULL ||
        strstr(lower, ".sw.") != NULL ||
        strstr(lower, "sw-decoder") != NULL ||
        strstr(lower, "software") != NULL;
    g_free(lower);
    return software;
}

static gboolean codec_supports_mime(JNIEnv *env, jobject codec_info, jmethodID get_supported_types, const char *mime) {
    jobjectArray types = (jobjectArray)(*env)->CallObjectMethod(env, codec_info, get_supported_types);
    if ((*env)->ExceptionCheck(env)) {
        (*env)->ExceptionClear(env);
        return FALSE;
    }
    if (!types) return FALSE;

    jsize count = (*env)->GetArrayLength(env, types);
    gboolean supported = FALSE;
    for (jsize i = 0; i < count; i++) {
        jstring jtype = (jstring)(*env)->GetObjectArrayElement(env, types, i);
        if (!jtype) continue;
        const char *ctype = (*env)->GetStringUTFChars(env, jtype, NULL);
        if (ctype) {
            if (g_ascii_strcasecmp(ctype, mime) == 0) {
                supported = TRUE;
            }
            (*env)->ReleaseStringUTFChars(env, jtype, ctype);
        }
        (*env)->DeleteLocalRef(env, jtype);
        if (supported) break;
    }

    (*env)->DeleteLocalRef(env, types);
    return supported;
}

static gchar* choose_preferred_mediacodec_name(const char *mime) {
    JNIEnv *env = gst_android_get_attached_env();
    if (!env) {
        LOGI("⚠️ Android decoder selection: JNIEnv unavailable, using registry fallback");
        return NULL;
    }

    jclass codec_list_cls = (*env)->FindClass(env, "android/media/MediaCodecList");
    jclass codec_info_cls = (*env)->FindClass(env, "android/media/MediaCodecInfo");
    if (!codec_list_cls || !codec_info_cls) {
        if ((*env)->ExceptionCheck(env)) (*env)->ExceptionClear(env);
        LOGI("⚠️ Android decoder selection: MediaCodecList unavailable");
        return NULL;
    }

    jmethodID ctor = (*env)->GetMethodID(env, codec_list_cls, "<init>", "(I)V");
    jmethodID get_codec_infos = (*env)->GetMethodID(env, codec_list_cls, "getCodecInfos", "()[Landroid/media/MediaCodecInfo;");
    jmethodID is_encoder = (*env)->GetMethodID(env, codec_info_cls, "isEncoder", "()Z");
    jmethodID get_name = (*env)->GetMethodID(env, codec_info_cls, "getName", "()Ljava/lang/String;");
    jmethodID get_supported_types = (*env)->GetMethodID(env, codec_info_cls, "getSupportedTypes", "()[Ljava/lang/String;");
    jmethodID is_hardware = get_optional_method_id(env, codec_info_cls, "isHardwareAccelerated", "()Z");
    jmethodID is_software = get_optional_method_id(env, codec_info_cls, "isSoftwareOnly", "()Z");
    jmethodID is_vendor = get_optional_method_id(env, codec_info_cls, "isVendor", "()Z");
    jmethodID is_alias = get_optional_method_id(env, codec_info_cls, "isAlias", "()Z");

    if (!ctor || !get_codec_infos || !is_encoder || !get_name || !get_supported_types) {
        if ((*env)->ExceptionCheck(env)) (*env)->ExceptionClear(env);
        LOGI("⚠️ Android decoder selection: codec reflection incomplete");
        return NULL;
    }

    jobject codec_list = (*env)->NewObject(env, codec_list_cls, ctor, 1);
    if (!codec_list || (*env)->ExceptionCheck(env)) {
        if ((*env)->ExceptionCheck(env)) (*env)->ExceptionClear(env);
        LOGI("⚠️ Android decoder selection: failed to instantiate MediaCodecList");
        return NULL;
    }

    jobjectArray codec_infos = (jobjectArray)(*env)->CallObjectMethod(env, codec_list, get_codec_infos);
    if (!codec_infos || (*env)->ExceptionCheck(env)) {
        if ((*env)->ExceptionCheck(env)) (*env)->ExceptionClear(env);
        (*env)->DeleteLocalRef(env, codec_list);
        LOGI("⚠️ Android decoder selection: getCodecInfos failed");
        return NULL;
    }

    gchar *best_hw = NULL;
    gchar *best_vendor = NULL;
    gchar *best_non_sw = NULL;
    gchar *best_any = NULL;

    jsize count = (*env)->GetArrayLength(env, codec_infos);
    for (jsize i = 0; i < count; i++) {
        jobject codec_info = (*env)->GetObjectArrayElement(env, codec_infos, i);
        if (!codec_info) continue;

        jboolean encoder = (*env)->CallBooleanMethod(env, codec_info, is_encoder);
        if ((*env)->ExceptionCheck(env)) {
            (*env)->ExceptionClear(env);
            (*env)->DeleteLocalRef(env, codec_info);
            continue;
        }
        if (encoder) {
            (*env)->DeleteLocalRef(env, codec_info);
            continue;
        }

        if (!codec_supports_mime(env, codec_info, get_supported_types, mime)) {
            (*env)->DeleteLocalRef(env, codec_info);
            continue;
        }

        if (is_alias) {
            jboolean alias = (*env)->CallBooleanMethod(env, codec_info, is_alias);
            if (!(*env)->ExceptionCheck(env) && alias) {
                (*env)->DeleteLocalRef(env, codec_info);
                continue;
            }
            if ((*env)->ExceptionCheck(env)) (*env)->ExceptionClear(env);
        }

        jstring jname = (jstring)(*env)->CallObjectMethod(env, codec_info, get_name);
        if ((*env)->ExceptionCheck(env)) {
            (*env)->ExceptionClear(env);
            (*env)->DeleteLocalRef(env, codec_info);
            continue;
        }
        if (!jname) {
            (*env)->DeleteLocalRef(env, codec_info);
            continue;
        }

        const char *codec_name = (*env)->GetStringUTFChars(env, jname, NULL);
        if (!codec_name) {
            (*env)->DeleteLocalRef(env, jname);
            (*env)->DeleteLocalRef(env, codec_info);
            continue;
        }

        gboolean software = codec_name_looks_software(codec_name);
        gboolean hardware = !software;
        gboolean vendor = FALSE;

        if (is_software) {
            software = (*env)->CallBooleanMethod(env, codec_info, is_software);
            if ((*env)->ExceptionCheck(env)) {
                (*env)->ExceptionClear(env);
                software = codec_name_looks_software(codec_name);
            }
        }
        if (is_hardware) {
            hardware = (*env)->CallBooleanMethod(env, codec_info, is_hardware);
            if ((*env)->ExceptionCheck(env)) {
                (*env)->ExceptionClear(env);
                hardware = !software;
            }
        }
        if (is_vendor) {
            vendor = (*env)->CallBooleanMethod(env, codec_info, is_vendor);
            if ((*env)->ExceptionCheck(env)) {
                (*env)->ExceptionClear(env);
                vendor = FALSE;
            }
        }

        if (strstr(codec_name, ".secure") == NULL) {
            if (!best_any) best_any = g_strdup(codec_name);
            if (!software && hardware && !best_hw) best_hw = g_strdup(codec_name);
            if (!software && vendor && !best_vendor) best_vendor = g_strdup(codec_name);
            if (!software && !best_non_sw) best_non_sw = g_strdup(codec_name);
        }

        (*env)->ReleaseStringUTFChars(env, jname, codec_name);
        (*env)->DeleteLocalRef(env, jname);
        (*env)->DeleteLocalRef(env, codec_info);
    }

    (*env)->DeleteLocalRef(env, codec_infos);
    (*env)->DeleteLocalRef(env, codec_list);

    gchar *selected = NULL;
    if (best_hw) {
        selected = g_strdup(best_hw);
    } else if (best_vendor) {
        selected = g_strdup(best_vendor);
    } else if (best_non_sw) {
        selected = g_strdup(best_non_sw);
    } else if (best_any) {
        selected = g_strdup(best_any);
    }

    if (selected) {
        LOGI("✅ Android decoder selection for %s: %s", mime, selected);
    } else {
        LOGI("⚠️ Android decoder selection for %s: no preferred MediaCodec candidate", mime);
    }

    g_free(best_hw);
    g_free(best_vendor);
    g_free(best_non_sw);
    g_free(best_any);
    return selected;
}

static gchar* find_android_decoder_for_codec_name(const char *factory_prefix, const char *codec_name) {
    if (!factory_prefix || !codec_name) return NULL;

    gchar *normalized_codec = normalize_codec_token(codec_name);
    if (!normalized_codec || normalized_codec[0] == '\0') {
        g_free(normalized_codec);
        return NULL;
    }

    GList *factories = gst_element_factory_list_get_elements(
        GST_ELEMENT_FACTORY_TYPE_DECODER | GST_ELEMENT_FACTORY_TYPE_MEDIA_VIDEO,
        GST_RANK_NONE);

    gchar *decoder_name = NULL;
    for (GList *l = factories; l; l = l->next) {
        GstElementFactory *f = (GstElementFactory *)l->data;
        const gchar *fname = gst_plugin_feature_get_name(GST_PLUGIN_FEATURE(f));
        if (!fname || !strstr(fname, factory_prefix)) continue;

        gchar *normalized_factory = normalize_codec_token(fname);
        gboolean matches = normalized_factory && strstr(normalized_factory, normalized_codec) != NULL;
        g_free(normalized_factory);
        if (!matches) continue;

        decoder_name = g_strdup(fname);
        LOGI("✅ GStreamer decoder matched: codec=%s -> factory=%s", codec_name, fname);
        break;
    }

    gst_plugin_feature_list_free(factories);
    g_free(normalized_codec);
    return decoder_name;
}

static gchar* resolve_preferred_android_decoder(const char *mime, const char *factory_prefix, gchar **cache_slot) {
    if (cache_slot && *cache_slot) {
        return g_strdup(*cache_slot);
    }

    gchar *codec_name = choose_preferred_mediacodec_name(mime);
    if (codec_name) {
        gchar *factory_name = find_android_decoder_for_codec_name(factory_prefix, codec_name);
        if (factory_name) {
            if (cache_slot) {
                *cache_slot = g_strdup(factory_name);
            }
            g_free(codec_name);
            return factory_name;
        }
        LOGI("⚠️ Preferred MediaCodec %s not found in GStreamer registry, falling back to registry scan", codec_name);
        g_free(codec_name);
    }

    gchar *fallback = find_android_decoder_by_name_fragments(factory_prefix, NULL);
    if (fallback && cache_slot) {
        *cache_slot = g_strdup(fallback);
    }
    return fallback;
}

void gst_android_reset_runtime_decoder_fallback(void) {
    g_hw_map_fail_count = 0;
    g_force_sw_decoder = FALSE;
    g_reconnect_requested = FALSE;
    LOGI("✅ Android decoder runtime fallback reset");
}

// udpsrc_probe_cb — вызывается при получении RTP пакета (диагностика)
static GstPadProbeReturn udpsrc_probe_cb(GstPad *pad, GstPadProbeInfo *info, gpointer user_data) {
    (void)pad;
    (void)user_data;
    static int first = 0;
    g_last_rtp_packet_us = g_get_monotonic_time();
    if (first == 0) {
        first = 1;
        GstBuffer *buf = GST_PAD_PROBE_INFO_BUFFER(info);
        LOGI("📨 RTP: первый пакет получен! size=%zu", buf ? gst_buffer_get_size(buf) : 0);
    }
    return GST_PAD_PROBE_OK;
}

// log_element_src_caps — выводит src pad template caps элемента (диагностика)
static void log_element_src_caps(GstElement *element, const char *name) {
    GstPad *srcpad = gst_element_get_static_pad(element, "src");
    if (srcpad) {
        GstCaps *tmpl_caps = gst_pad_get_pad_template_caps(srcpad);
        if (tmpl_caps) {
            gchar *caps_str = gst_caps_to_string(tmpl_caps);
            LOGI("📋 %s src template caps: %s", name, caps_str);
            g_free(caps_str);
            gst_caps_unref(tmpl_caps);
        }
        gst_object_unref(srcpad);
    }
}

// Forward declarations (определены ниже)
static GstBusSyncReply bus_sync_handler(GstBus *bus, GstMessage *msg, gpointer user_data);
static void apply_gl_context_to_pipeline(GstElement *pipeline);
static void drain_bus_messages(GstElement* pipeline);

static gchar* find_android_decoder_by_name_fragments(const char *required1, const char *required2) {
    GList *factories = gst_element_factory_list_get_elements(
        GST_ELEMENT_FACTORY_TYPE_DECODER | GST_ELEMENT_FACTORY_TYPE_MEDIA_VIDEO,
        GST_RANK_NONE);
    gchar *decoder_name = NULL;
    for (GList *l = factories; l; l = l->next) {
        GstElementFactory *f = (GstElementFactory *)l->data;
        const gchar *fname = gst_plugin_feature_get_name(GST_PLUGIN_FEATURE(f));
        if (!fname) continue;
        if (required1 && !strstr(fname, required1)) continue;
        if (required2 && !strstr(fname, required2)) continue;
        if (strstr(fname, "secure") != NULL) continue;
        decoder_name = g_strdup(fname);
        LOGI("✅ Android decoder found: %s", fname);
        break;
    }
    gst_plugin_feature_list_free(factories);
    return decoder_name;
}

// create_hw_pipeline — создаёт HW pipeline через gst_parse_launch (надёжнее ручной линковки)
// Пробует несколько вариантов цепочки GL→CPU для amcviddec (external-oes текстуры)
// Возвращает pipeline или NULL если не удалось
static GstElement* create_hw_pipeline(int port, const char *decoder_name) {
    LOGI("🔧 Сборка HW pipeline (gst_parse_launch): декодер=%s", decoder_name);

    // Диагностика: src caps декодера
    GstElement *tmp_dec = gst_element_factory_make(decoder_name, NULL);
    if (tmp_dec) {
        log_element_src_caps(tmp_dec, decoder_name);
        gst_object_unref(tmp_dec);
    }

    // Варианты GL→CPU цепочки (от лучшего к простому).
    // На ряде Qualcomm/Pixel/Samsung устройств одного "gldownload ! video/x-raw"
    // недостаточно: appsink всё равно получает GLMemory, а gst_buffer_map падает.
    // Поэтому сначала жёстко переводим texture-target в 2D, потом явно требуем
    // SystemMemory после gldownload.
    const char *gl_chains[] = {
        "glcolorconvert ! video/x-raw(memory:GLMemory),texture-target=2D,format=RGBA ! gldownload ! video/x-raw(memory:SystemMemory),format=RGBA",
        "glcolorconvert ! video/x-raw(memory:GLMemory),texture-target=2D ! gldownload ! video/x-raw(memory:SystemMemory),format=RGBA",
        "gldownload ! video/x-raw(memory:SystemMemory),format=RGBA",
        NULL  // прямой videoconvert
    };
    const char *gl_chain_names[] = {
        "glcolorconvert+2d+download+sysmem-rgba",
        "glcolorconvert+2d+download+sysmem",
        "gldownload+sysmem-rgba",
        "direct"
    };
    const gboolean chain_needs_videoconvert[] = {
        FALSE,
        FALSE,
        FALSE,
        TRUE,
    };

    GstElement *pipeline = NULL;
    for (guint i = 0; i < G_N_ELEMENTS(gl_chains); i++) {
        gchar *pipeline_str;
        if (gl_chains[i] && !chain_needs_videoconvert[i]) {
            pipeline_str = g_strdup_printf(
                "udpsrc name=udpsrc0 port=%d buffer-size=131072 timeout=0 "
                "caps=\"application/x-rtp,media=video,encoding-name=H264,payload=96\" ! "
                "rtpjitterbuffer latency=15 faststart-min-packets=1 drop-on-latency=true ! "
                "rtph264depay ! "
                "h264parse config-interval=-1 ! "
                "%s ! "
                "%s ! "
                "appsink name=sink emit-signals=false max-buffers=1 drop=true sync=false",
                port, decoder_name, gl_chains[i]
            );
        } else {
            pipeline_str = g_strdup_printf(
                "udpsrc name=udpsrc0 port=%d buffer-size=131072 timeout=0 "
                "caps=\"application/x-rtp,media=video,encoding-name=H264,payload=96\" ! "
                "rtpjitterbuffer latency=15 faststart-min-packets=1 drop-on-latency=true ! "
                "rtph264depay ! "
                "h264parse config-interval=-1 ! "
                "%s ! "
                "videoconvert ! "
                "video/x-raw,format=RGBA ! "
                "appsink name=sink emit-signals=false max-buffers=1 drop=true sync=false",
                port, decoder_name
            );
        }

        LOGI("📝 HW pipeline вариант %d (%s): %s", i+1, gl_chain_names[i], pipeline_str);

        GError *error = NULL;
        pipeline = gst_parse_launch(pipeline_str, &error);
        g_free(pipeline_str);

        if (error) {
            LOGI("⚠️ HW вариант %d (%s): %s", i+1, gl_chain_names[i], error->message);
            g_error_free(error);
            if (pipeline) { gst_object_unref(pipeline); pipeline = NULL; }
            continue;
        }

        if (pipeline) {
            // Устанавливаем GL context и bus sync ДО set_state
            apply_gl_context_to_pipeline(pipeline);
            GstBus *bus = gst_element_get_bus(pipeline);
            if (bus) {
                gst_bus_set_sync_handler(bus, bus_sync_handler, pipeline, NULL);
                gst_object_unref(bus);
            }

            // Пробуем перевести в PAUSED — проверяет реальную negotiation
            GstStateChangeReturn ret = gst_element_set_state(pipeline, GST_STATE_READY);
            if (ret == GST_STATE_CHANGE_FAILURE) {
                LOGI("⚠️ HW вариант %d (%s): set_state(READY) FAILURE — пробуем следующий", i+1, gl_chain_names[i]);
                drain_bus_messages(pipeline);
                gst_element_set_state(pipeline, GST_STATE_NULL);
                gst_object_unref(pipeline);
                pipeline = NULL;
                continue;
            }

            // Pad probe для диагностики RTP
            GstElement *udpsrc = gst_bin_get_by_name(GST_BIN(pipeline), "udpsrc0");
            if (udpsrc) {
                GstPad *pad = gst_element_get_static_pad(udpsrc, "src");
                if (pad) {
                    gst_pad_add_probe(pad, GST_PAD_PROBE_TYPE_BUFFER, udpsrc_probe_cb, NULL, NULL);
                    gst_object_unref(pad);
                }
                gst_object_unref(udpsrc);
            }

            // Возвращаем в NULL — вызывающий код сам сделает set_state(PLAYING)
            gst_element_set_state(pipeline, GST_STATE_NULL);

            LOGI("✅ HW pipeline с %s (%s) готов!", decoder_name, gl_chain_names[i]);
            return pipeline;
        }
    }

    LOGI("⚠️ Все HW варианты для %s не сработали", decoder_name);
    return NULL;
}

static GstElement* create_hw_jpeg_pipeline(int port, const char *decoder_name) {
    LOGI("🔧 Сборка JPEG HW pipeline (gst_parse_launch): декодер=%s", decoder_name);

    GstElement *tmp_dec = gst_element_factory_make(decoder_name, NULL);
    if (tmp_dec) {
        log_element_src_caps(tmp_dec, decoder_name);
        gst_object_unref(tmp_dec);
    }

    const char *gl_chains[] = {
        "glcolorconvert ! video/x-raw(memory:GLMemory),texture-target=2D,format=RGBA ! gldownload ! video/x-raw(memory:SystemMemory),format=RGBA",
        "glcolorconvert ! video/x-raw(memory:GLMemory),texture-target=2D ! gldownload ! video/x-raw(memory:SystemMemory),format=RGBA",
        "gldownload ! video/x-raw(memory:SystemMemory),format=RGBA",
        NULL
    };
    const char *gl_chain_names[] = {
        "glcolorconvert+2d+download+sysmem-rgba",
        "glcolorconvert+2d+download+sysmem",
        "gldownload+sysmem-rgba",
        "direct"
    };
    const gboolean chain_needs_videoconvert[] = {
        FALSE,
        FALSE,
        FALSE,
        TRUE,
    };

    GstElement *pipeline = NULL;
    for (guint i = 0; i < G_N_ELEMENTS(gl_chains); i++) {
        gchar *pipeline_str;
        if (gl_chains[i] && !chain_needs_videoconvert[i]) {
            pipeline_str = g_strdup_printf(
                "udpsrc name=udpsrc0 port=%d buffer-size=65536 timeout=0 "
                "caps=\"application/x-rtp,media=video,encoding-name=JPEG,clock-rate=90000,payload=26\" ! "
                "rtpjitterbuffer latency=15 faststart-min-packets=1 drop-on-latency=true ! "
                "rtpjpegdepay ! "
                "jpegparse ! "
                "%s ! "
                "%s ! "
                "appsink name=sink emit-signals=false max-buffers=2 drop=true sync=false",
                port, decoder_name, gl_chains[i]
            );
        } else {
            pipeline_str = g_strdup_printf(
                "udpsrc name=udpsrc0 port=%d buffer-size=65536 timeout=0 "
                "caps=\"application/x-rtp,media=video,encoding-name=JPEG,clock-rate=90000,payload=26\" ! "
                "rtpjitterbuffer latency=15 faststart-min-packets=1 drop-on-latency=true ! "
                "rtpjpegdepay ! "
                "jpegparse ! "
                "%s ! "
                "videoconvert ! "
                "video/x-raw,format=RGBA ! "
                "appsink name=sink emit-signals=false max-buffers=2 drop=true sync=false",
                port, decoder_name
            );
        }

        LOGI("📝 JPEG HW pipeline вариант %d (%s): %s", i+1, gl_chain_names[i], pipeline_str);

        GError *error = NULL;
        pipeline = gst_parse_launch(pipeline_str, &error);
        g_free(pipeline_str);

        if (error) {
            LOGI("⚠️ JPEG HW вариант %d (%s): %s", i+1, gl_chain_names[i], error->message);
            g_error_free(error);
            if (pipeline) { gst_object_unref(pipeline); pipeline = NULL; }
            continue;
        }

        if (pipeline) {
            apply_gl_context_to_pipeline(pipeline);
            GstBus *bus = gst_element_get_bus(pipeline);
            if (bus) {
                gst_bus_set_sync_handler(bus, bus_sync_handler, pipeline, NULL);
                gst_object_unref(bus);
            }

            GstStateChangeReturn ret = gst_element_set_state(pipeline, GST_STATE_READY);
            if (ret == GST_STATE_CHANGE_FAILURE) {
                LOGI("⚠️ JPEG HW вариант %d (%s): set_state(READY) FAILURE — пробуем следующий", i+1, gl_chain_names[i]);
                drain_bus_messages(pipeline);
                gst_element_set_state(pipeline, GST_STATE_NULL);
                gst_object_unref(pipeline);
                pipeline = NULL;
                continue;
            }

            GstElement *udpsrc = gst_bin_get_by_name(GST_BIN(pipeline), "udpsrc0");
            if (udpsrc) {
                GstPad *pad = gst_element_get_static_pad(udpsrc, "src");
                if (pad) {
                    gst_pad_add_probe(pad, GST_PAD_PROBE_TYPE_BUFFER, udpsrc_probe_cb, NULL, NULL);
                    gst_object_unref(pad);
                }
                gst_object_unref(udpsrc);
            }

            gst_element_set_state(pipeline, GST_STATE_NULL);
            LOGI("✅ JPEG HW pipeline с %s (%s) готов!", decoder_name, gl_chain_names[i]);
            return pipeline;
        }
    }

    LOGI("⚠️ Все JPEG HW варианты для %s не сработали", decoder_name);
    return NULL;
}

static GstElement* create_jpeg_pipeline(int port) {
    LOGI("📝 Creating RTP MJPEG pipeline for port: %d", port);

    GstElement *pipeline = NULL;
    gchar *jpeg_hw_name = NULL;
    if (g_force_sw_decoder) {
        LOGI("⚠️ g_force_sw_decoder=TRUE — пропускаем Android JPEG HW decoder");
    } else {
        jpeg_hw_name = resolve_preferred_android_decoder("image/jpeg", "amc", &g_cached_jpeg_hw_decoder);
        if (!jpeg_hw_name) {
            jpeg_hw_name = find_android_decoder_by_name_fragments("amc", "jpeg");
        }
    }

    if (jpeg_hw_name) {
        pipeline = create_hw_jpeg_pipeline(port, jpeg_hw_name);
        if (pipeline) {
            LOGI("✅ Pipeline с JPEG HW декодером %s готов", jpeg_hw_name);
            g_free(jpeg_hw_name);
            return pipeline;
        }
        LOGI("⚠️ JPEG HW декодер %s не удалось использовать — пробуем software fallback", jpeg_hw_name);
        g_free(jpeg_hw_name);
    }

    const char *candidates[] = {
        "udpsrc name=udpsrc0 port=%d buffer-size=65536 timeout=0 "
        "caps=\"application/x-rtp,media=video,encoding-name=JPEG,clock-rate=90000,payload=26\" ! "
        "rtpjitterbuffer latency=15 faststart-min-packets=1 drop-on-latency=true ! "
        "rtpjpegdepay ! jpegdec ! videoconvert ! video/x-raw,format=RGBA ! "
        "appsink name=sink emit-signals=false max-buffers=2 drop=true sync=false",

        "udpsrc name=udpsrc0 port=%d buffer-size=65536 timeout=0 "
        "caps=\"application/x-rtp,media=video,encoding-name=JPEG,clock-rate=90000,payload=26\" ! "
        "rtpjitterbuffer latency=15 faststart-min-packets=1 drop-on-latency=true ! "
        "rtpjpegdepay ! jpegparse ! avdec_mjpeg max-threads=0 ! videoconvert ! video/x-raw,format=RGBA ! "
        "appsink name=sink emit-signals=false max-buffers=2 drop=true sync=false",

        "udpsrc name=udpsrc0 port=%d buffer-size=65536 timeout=0 "
        "caps=\"application/x-rtp,media=video,encoding-name=JPEG,clock-rate=90000,payload=26\" ! "
        "rtpjitterbuffer latency=15 faststart-min-packets=1 drop-on-latency=true ! "
        "rtpjpegdepay ! jpegparse ! decodebin ! videoconvert ! video/x-raw,format=RGBA ! "
        "appsink name=sink emit-signals=false max-buffers=2 drop=true sync=false",
    };

    for (guint i = 0; i < G_N_ELEMENTS(candidates); i++) {
        gchar *pipeline_str = g_strdup_printf(candidates[i], port);
        LOGI("📝 JPEG fallback pipeline вариант %u: %s", i + 1, pipeline_str);

        GError *error = NULL;
        pipeline = gst_parse_launch(pipeline_str, &error);
        g_free(pipeline_str);

        if (error) {
            LOGI("⚠️ JPEG fallback вариант %u: %s", i + 1, error->message);
            g_error_free(error);
            if (pipeline) { gst_object_unref(pipeline); pipeline = NULL; }
            continue;
        }

        if (pipeline) {
            GstElement *udpsrc = gst_bin_get_by_name(GST_BIN(pipeline), "udpsrc0");
            if (udpsrc) {
                GstPad *pad = gst_element_get_static_pad(udpsrc, "src");
                if (pad) {
                    gst_pad_add_probe(pad, GST_PAD_PROBE_TYPE_BUFFER, udpsrc_probe_cb, NULL, NULL);
                    gst_object_unref(pad);
                }
                gst_object_unref(udpsrc);
            }
            LOGI("✅ JPEG fallback pipeline готов (вариант %u)", i + 1);
            return pipeline;
        }
    }

    LOGE("❌ Failed to create any MJPEG pipeline!");
    return NULL;
}

// bus_sync_handler — обрабатывает NEED_CONTEXT для GL элементов.
// Предоставляет полноценный EGL display + app context (не пустой gst_gl_display_egl_new).
static GstBusSyncReply bus_sync_handler(GstBus *bus, GstMessage *msg, gpointer user_data) {
    (void)bus;
    if (GST_MESSAGE_TYPE(msg) != GST_MESSAGE_NEED_CONTEXT)
        return GST_BUS_PASS;

    GstElement *pipeline = (GstElement *)user_data;
    if (!pipeline) return GST_BUS_PASS;

    const gchar *context_type = NULL;
    gst_message_parse_context_type(msg, &context_type);
    if (!context_type) return GST_BUS_PASS;

    LOGI("🔧 NEED_CONTEXT: %s (от %s)", context_type, GST_OBJECT_NAME(GST_MESSAGE_SRC(msg)));

    if (!ensure_egl_context()) {
        LOGE("❌ EGL context не создан, не можем ответить на NEED_CONTEXT");
        return GST_BUS_PASS;
    }

    if (strcmp(context_type, GST_GL_DISPLAY_CONTEXT_TYPE) == 0) {
        // Запрос GL display → даём наш инициализированный EGL display
        GstGLDisplay *display = GST_GL_DISPLAY(gst_gl_display_egl_new_with_egl_display((gpointer)g_egl_display));
        if (display) {
            GstContext *ctx = gst_context_new(context_type, TRUE);
            gst_context_set_gl_display(ctx, display);
            gst_element_set_context(pipeline, ctx);
            gst_context_unref(ctx);
            gst_object_unref(display);
            LOGI("✅ GL display установлен (bus sync)");
            return GST_BUS_DROP;
        }
    } else if (strcmp(context_type, "gst.gl.app_context") == 0) {
        // Запрос app GL context → даём wrapped EGLContext (amcviddec создаст shared от него)
        GstGLDisplay *display = GST_GL_DISPLAY(gst_gl_display_egl_new_with_egl_display((gpointer)g_egl_display));
        if (display) {
            GstGLContext *gl_ctx = gst_gl_context_new_wrapped(display,
                (guintptr)g_egl_context, GST_GL_PLATFORM_EGL, GST_GL_API_GLES2);
            if (gl_ctx) {
                GstContext *ctx = gst_context_new(context_type, TRUE);
                GstStructure *s = gst_context_writable_structure(ctx);
                gst_structure_set(s, "context", GST_TYPE_GL_CONTEXT, gl_ctx, NULL);
                gst_element_set_context(pipeline, ctx);
                gst_context_unref(ctx);
                gst_object_unref(gl_ctx);
                LOGI("✅ GL app context установлен (bus sync)");
            }
            gst_object_unref(display);
            return GST_BUS_DROP;
        }
    }

    return GST_BUS_PASS;
}

// apply_gl_context_to_pipeline — устанавливает полноценный EGL контекст на pipeline.
// Предоставляет и GstGLDisplay (EGL display), и gst.gl.app_context (wrapped EGLContext).
// Это позволяет amcviddec создать shared GL context для SurfaceTexture.updateTexImage().
static void apply_gl_context_to_pipeline(GstElement *pipeline) {
    if (!ensure_egl_context()) {
        LOGE("⚠️ EGL context не создан — HW декодер может не работать");
        return;
    }

    // 1. Оборачиваем наш EGLDisplay в GstGLDisplayEGL
    GstGLDisplay *display = GST_GL_DISPLAY(gst_gl_display_egl_new_with_egl_display((gpointer)g_egl_display));
    if (!display) {
        LOGE("⚠️ gst_gl_display_egl_new_with_egl_display failed");
        return;
    }

    // 2. Оборачиваем наш EGLContext в GstGLContext
    GstGLContext *gl_ctx = gst_gl_context_new_wrapped(display,
        (guintptr)g_egl_context, GST_GL_PLATFORM_EGL, GST_GL_API_GLES2);
    if (!gl_ctx) {
        LOGE("⚠️ gst_gl_context_new_wrapped failed");
        gst_object_unref(display);
        return;
    }

    // 3. Заполняем GL info.
    // Для wrapped GstGLContext недостаточно одного eglMakeCurrent():
    // нужно также уведомить сам GStreamer, что этот GstGLContext активен на текущем потоке.
    gboolean egl_current = eglMakeCurrent(g_egl_display, g_egl_surface, g_egl_surface, g_egl_context);
    gboolean gst_ctx_active = FALSE;
    if (!egl_current) {
        LOGE("⚠️ eglMakeCurrent before gst_gl_context_fill_info failed: 0x%x", eglGetError());
    } else {
        gst_ctx_active = gst_gl_context_activate(gl_ctx, TRUE);
        if (!gst_ctx_active) {
            LOGE("⚠️ gst_gl_context_activate(TRUE) failed for wrapped EGL context");
        }
    }

    GError *err = NULL;
    if (!gst_gl_context_fill_info(gl_ctx, &err)) {
        LOGI("⚠️ gst_gl_context_fill_info: %s (не критично)", err ? err->message : "?");
        if (err) g_error_free(err);
    }
    if (gst_ctx_active) {
        gst_gl_context_activate(gl_ctx, FALSE);
    }
    if (egl_current) {
        eglMakeCurrent(g_egl_display, EGL_NO_SURFACE, EGL_NO_SURFACE, EGL_NO_CONTEXT);
    }

    // 4. Устанавливаем GL display на pipeline
    GstContext *display_ctx = gst_context_new(GST_GL_DISPLAY_CONTEXT_TYPE, TRUE);
    gst_context_set_gl_display(display_ctx, display);
    gst_element_set_context(pipeline, display_ctx);
    gst_context_unref(display_ctx);

    // 5. Устанавливаем app GL context на pipeline (amcviddec создаст shared context от него)
    GstContext *app_ctx = gst_context_new("gst.gl.app_context", TRUE);
    GstStructure *s = gst_context_writable_structure(app_ctx);
    gst_structure_set(s, "context", GST_TYPE_GL_CONTEXT, gl_ctx, NULL);
    gst_element_set_context(pipeline, app_ctx);
    gst_context_unref(app_ctx);

    LOGI("✅ GL context предустановлен на pipeline: EGL display=%p, ctx=%p, surface=%p",
         (void*)(intptr_t)g_egl_display, (void*)(intptr_t)g_egl_context, (void*)(intptr_t)g_egl_surface);

    gst_object_unref(gl_ctx);
    gst_object_unref(display);
}

// gst_android_prepare_hw_pipeline — bus sync + GL context перед PLAYING (вызывается из Go)
static gboolean pipeline_needs_gl_context(GstElement *pipeline) {
    if (!pipeline || !GST_IS_BIN(pipeline)) return FALSE;

    GstIterator *it = gst_bin_iterate_elements(GST_BIN(pipeline));
    if (!it) return FALSE;

    gboolean needs_gl = FALSE;
    GValue item = G_VALUE_INIT;

    while (gst_iterator_next(it, &item) == GST_ITERATOR_OK) {
        GstElement *element = GST_ELEMENT(g_value_get_object(&item));
        if (element) {
            GstElementFactory *factory = gst_element_get_factory(element);
            if (factory) {
                const gchar *fname = gst_plugin_feature_get_name(GST_PLUGIN_FEATURE(factory));
                if (fname &&
                    (strstr(fname, "amcviddec") != NULL ||
                        strstr(fname, "glcolorconvert") != NULL ||
                        strstr(fname, "gldownload") != NULL ||
                        strstr(fname, "glupload") != NULL ||
                        strstr(fname, "gl") == fname)) {
                    needs_gl = TRUE;
                }
            }
        }
        g_value_reset(&item);
        if (needs_gl) break;
    }

    g_value_unset(&item);
    gst_iterator_free(it);
    return needs_gl;
}

void gst_android_prepare_hw_pipeline(GstElement *pipeline) {
    if (!pipeline) return;
    GstBus *bus = gst_element_get_bus(pipeline);
    if (bus) {
        gst_bus_set_sync_handler(bus, bus_sync_handler, pipeline, NULL);
        gst_object_unref(bus);
    }
    if (pipeline_needs_gl_context(pipeline)) {
        apply_gl_context_to_pipeline(pipeline);
    } else {
        LOGI("ℹ️ Pipeline does not require GL context preconfiguration");
    }
}

// create_pipeline создаёт pipeline для Android (HW H264/MJPEG, SW fallback)
static GstElement* create_pipeline(int port, int width, int height, int mode) {
    (void)width;
    (void)height;
    if (mode == PIPELINE_MODE_JPEG_RTP) {
        return create_jpeg_pipeline(port);
    }

    LOGI("📝 Creating RTP H.264 pipeline for port: %d", port);

    GstElement *pipeline = NULL;
    gchar *pipeline_str;

    // 1) Ищем amcviddec (аппаратный декодер) — пропускаем если ранее GL провалился
    gchar *amc_name = NULL;
    if (g_force_sw_decoder) {
        LOGI("⚠️ g_force_sw_decoder=TRUE — пропускаем amcviddec, сразу avdec_h264");
    } else {
        amc_name = resolve_preferred_android_decoder("video/avc", "amcviddec", &g_cached_h264_hw_decoder);
        if (!amc_name) {
            amc_name = find_android_decoder_by_name_fragments("amcviddec", NULL);
        }
    }

    // 2) Пробуем HW pipeline через gst_parse_launch (amcviddec + GL chain)
    if (amc_name) {
        pipeline = create_hw_pipeline(port, amc_name);
        if (pipeline) {
            LOGI("✅ Pipeline с HW декодером %s готов", amc_name);
            g_free(amc_name);
            return pipeline;
        }
        LOGI("⚠️ HW декодер %s не удалось использовать — пробуем avdec_h264", amc_name);
        g_free(amc_name);
    }

    // 3) Fallback: avdec_h264 (software, multi-threaded)
    GstElementFactory *avdec_factory = gst_element_factory_find("avdec_h264");
    if (!avdec_factory) {
        LOGE("❌ Нет ни amcviddec ни avdec_h264!");
        return NULL;
    }
    gst_object_unref(avdec_factory);

    // avdec_h264 max-threads=0 — авто-выбор числа потоков (NEON на ARM64)
    pipeline_str = g_strdup_printf(
        "udpsrc name=udpsrc0 port=%d buffer-size=131072 timeout=0 caps=\"application/x-rtp,media=video,encoding-name=H264,payload=96\" ! "
        "rtpjitterbuffer latency=15 faststart-min-packets=1 drop-on-latency=true ! "
        "rtph264depay ! "
        "h264parse config-interval=-1 ! "
        "avdec_h264 max-threads=0 ! "
        "videoconvert ! "
        "video/x-raw,format=RGBA ! "
        "appsink name=sink emit-signals=false max-buffers=1 drop=true sync=false",
        port
    );
    LOGI("📝 Pipeline (avdec_h264 multi-threaded): %s", pipeline_str);

    GError *error = NULL;
    pipeline = gst_parse_launch(pipeline_str, &error);
    g_free(pipeline_str);

    if (error) {
        LOGE("❌ gst_parse_launch error: %s", error->message);
        g_error_free(error);
        if (pipeline) { gst_object_unref(pipeline); pipeline = NULL; }
    }

    if (pipeline) {
        LOGI("✅ Fallback: pipeline с avdec_h264 (multi-threaded)");
        // Pad probe
        GstElement *udpsrc = gst_bin_get_by_name(GST_BIN(pipeline), "udpsrc0");
        if (udpsrc) {
            GstPad *pad = gst_element_get_static_pad(udpsrc, "src");
            if (pad) {
                gst_pad_add_probe(pad, GST_PAD_PROBE_TYPE_BUFFER, udpsrc_probe_cb, NULL, NULL);
                gst_object_unref(pad);
            }
            gst_object_unref(udpsrc);
        }
    } else {
        LOGE("❌ Failed to create any pipeline!");
    }

    return pipeline;
}

// wait_for_playing_or_error: ждёт перехода в PLAYING или ошибки на bus (timeout в секундах)
// Возвращает 0=OK (PLAYING), 1=ошибка, 2=таймаут
static int wait_for_playing_or_error(GstElement* pipeline, int timeout_sec) {
    GstBus *bus = gst_element_get_bus(pipeline);
    if (!bus) return 1;

    gint64 timeout_ns = (gint64)timeout_sec * GST_SECOND;
    GstMessage *msg = gst_bus_timed_pop_filtered(bus, timeout_ns,
        (GstMessageType)(GST_MESSAGE_ERROR | GST_MESSAGE_STATE_CHANGED | GST_MESSAGE_EOS));

    int result = 2; // таймаут
    if (msg) {
        switch (GST_MESSAGE_TYPE(msg)) {
            case GST_MESSAGE_ERROR: {
                GError *err = NULL;
                gchar *debug = NULL;
                gst_message_parse_error(msg, &err, &debug);
                LOGE("🔴 wait: ERROR: %s", err ? err->message : "?");
                if (debug) { LOGE("🔴 wait: debug: %s", debug); g_free(debug); }
                if (err) g_error_free(err);
                result = 1;
                break;
            }
            case GST_MESSAGE_STATE_CHANGED: {
                // Ищем переход pipeline в PLAYING
                if (GST_MESSAGE_SRC(msg) == GST_OBJECT(pipeline)) {
                    GstState old_s, new_s, pending_s;
                    gst_message_parse_state_changed(msg, &old_s, &new_s, &pending_s);
                    LOGI("🔄 wait: %s -> %s (pending: %s)",
                        gst_element_state_get_name(old_s),
                        gst_element_state_get_name(new_s),
                        gst_element_state_get_name(pending_s));
                    if (new_s == GST_STATE_PLAYING) {
                        result = 0;
                    } else {
                        // Не PLAYING — ждём ещё
                        gst_message_unref(msg);
                        msg = gst_bus_timed_pop_filtered(bus, timeout_ns,
                            (GstMessageType)(GST_MESSAGE_ERROR | GST_MESSAGE_STATE_CHANGED));
                        if (msg && GST_MESSAGE_TYPE(msg) == GST_MESSAGE_ERROR) {
                            GError *err2 = NULL;
                            gst_message_parse_error(msg, &err2, NULL);
                            LOGE("🔴 wait2: ERROR: %s", err2 ? err2->message : "?");
                            if (err2) g_error_free(err2);
                            result = 1;
                        } else if (msg && GST_MESSAGE_TYPE(msg) == GST_MESSAGE_STATE_CHANGED) {
                            GstState o2, n2, p2;
                            gst_message_parse_state_changed(msg, &o2, &n2, &p2);
                            if (n2 == GST_STATE_PLAYING) result = 0;
                        }
                    }
                }
                break;
            }
            default:
                result = 2;
        }
        if (msg) gst_message_unref(msg);
    }

    gst_object_unref(bus);
    LOGI("📊 wait_for_playing_or_error: result=%d", result);
    return result;
}

// Функция для проверки состояния pipeline
static void check_pipeline_state(GstElement* pipeline) {
    GstState state, pending;
    GstStateChangeReturn ret = gst_element_get_state(pipeline, &state, &pending, 0);

    const char* state_name = gst_element_state_get_name(state);
    const char* pending_name = gst_element_state_get_name(pending);

    LOGI("📊 Pipeline state: %s (pending: %s)", state_name, pending_name);
}

// Функция для проверки ошибок на bus
static char* check_pipeline_errors(GstElement* pipeline) {
    GstBus *bus = gst_element_get_bus(pipeline);
    if (!bus) {
        return NULL;
    }

    GstMessage *msg = gst_bus_pop_filtered(bus, GST_MESSAGE_ERROR | GST_MESSAGE_WARNING);
    char *error_str = NULL;

    if (msg) {
        GError *err = NULL;
        gchar *debug_info = NULL;

        if (GST_MESSAGE_TYPE(msg) == GST_MESSAGE_ERROR) {
            gst_message_parse_error(msg, &err, &debug_info);
            LOGE("❌ GStreamer ERROR: %s", err->message);
            if (debug_info) {
                LOGE("❌ Debug info: %s", debug_info);
            }
            error_str = g_strdup(err->message);
        } else if (GST_MESSAGE_TYPE(msg) == GST_MESSAGE_WARNING) {
            gst_message_parse_warning(msg, &err, &debug_info);
            LOGI("⚠️ GStreamer WARNING: %s", err->message);
            if (debug_info) {
                LOGI("⚠️ Debug info: %s", debug_info);
            }
        }

        if (err) g_error_free(err);
        if (debug_info) g_free(debug_info);
        gst_message_unref(msg);
    }

    gst_object_unref(bus);
    return error_str;
}

// drain_bus_messages логирует ВСЕ сообщения с bus (для отладки)
static void drain_bus_messages(GstElement* pipeline) {
    GstBus *bus = gst_element_get_bus(pipeline);
    if (!bus) return;

    GstMessage *msg;
    while ((msg = gst_bus_pop(bus)) != NULL) {
        GError *err = NULL;
        gchar *debug_info = NULL;
        GstObject *src = GST_MESSAGE_SRC(msg);
        const char *src_name = src ? GST_OBJECT_NAME(src) : "unknown";

        switch (GST_MESSAGE_TYPE(msg)) {
            case GST_MESSAGE_ERROR:
                gst_message_parse_error(msg, &err, &debug_info);
                LOGE("🔴 BUS ERROR [%s]: %s", src_name, err ? err->message : "?");
                if (debug_info) LOGE("🔴 Debug: %s", debug_info);
                if (err) g_error_free(err);
                if (debug_info) g_free(debug_info);
                break;
            case GST_MESSAGE_WARNING:
                gst_message_parse_warning(msg, &err, &debug_info);
                LOGI("🟡 BUS WARNING [%s]: %s", src_name, err ? err->message : "?");
                if (debug_info) LOGI("🟡 Debug: %s", debug_info);
                if (err) g_error_free(err);
                if (debug_info) g_free(debug_info);
                break;
            case GST_MESSAGE_STATE_CHANGED:
                if (GST_MESSAGE_SRC(msg) == GST_OBJECT(pipeline)) {
                    GstState old_s, new_s, pending_s;
                    gst_message_parse_state_changed(msg, &old_s, &new_s, &pending_s);
                    LOGI("🔄 BUS STATE_CHANGED: %s -> %s (pending: %s)",
                        gst_element_state_get_name(old_s),
                        gst_element_state_get_name(new_s),
                        gst_element_state_get_name(pending_s));
                }
                break;
            case GST_MESSAGE_ELEMENT:
                {
                    const GstStructure *s = gst_message_get_structure(msg);
                    if (s && gst_structure_has_name(s, "GstUDPSrcTimeout")) {
                        LOGI("🟠 BUS: udpsrc timeout - no data received!");
                    }
                }
                break;
            case GST_MESSAGE_EOS:
                LOGI("🟣 BUS: EOS (end of stream)");
                break;
            default:
                LOGI("📨 BUS: %s from %s", gst_message_type_get_name(GST_MESSAGE_TYPE(msg)), src_name);
        }
        gst_message_unref(msg);
    }
    gst_object_unref(bus);
}

// Функция для получения кадра из appsink
static GstSample* get_sample_from_appsink(GstElement* pipeline) {
    static int call_count = 0;
    call_count++;

    GstElement *appsink = gst_bin_get_by_name(GST_BIN(pipeline), "sink");
    if (!appsink) {
        if (call_count == 1) {
            LOGE("❌ Appsink 'sink' not found in pipeline!");
        }
        return NULL;
    }

    if (call_count == 1) {
        // Логируем детали только при первом вызове
        gboolean is_eos = gst_app_sink_is_eos(GST_APP_SINK(appsink));
        LOGI("📊 Appsink status: EOS=%d", is_eos);
        check_pipeline_state(pipeline);
    }

    GstSample *sample = gst_app_sink_try_pull_sample(GST_APP_SINK(appsink), 16000000); // 16ms — не блокировать надолго

    if (call_count <= 3) {
        if (sample) {
            LOGI("✅ Sample #%d pulled successfully", call_count);
        } else {
            LOGI("⚠️ No sample #%d available", call_count);
        }
    }

    gst_object_unref(appsink);
    return sample;
}

// Функция для получения данных кадра
static void extract_frame_data(GstSample* sample, guint8** data, gint* width, gint* height, gsize* size) {
    static int extract_count = 0;
    extract_count++;

    if (!sample) {
        *data = NULL;
        return;
    }

    GstCaps *caps = gst_sample_get_caps(sample);
    if (!caps) {
        *data = NULL;
        return;
    }

    GstStructure *structure = gst_caps_get_structure(caps, 0);
    if (!structure) {
        *data = NULL;
        return;
    }

    gst_structure_get_int(structure, "width", width);
    gst_structure_get_int(structure, "height", height);

    if (extract_count == 1) {
        gchar *caps_str = gst_caps_to_string(caps);
        LOGI("📐 Frame #1: %dx%d, caps: %s", *width, *height, caps_str);
        g_free(caps_str);
    }

    GstBuffer *buffer = gst_sample_get_buffer(sample);
    if (!buffer) {
        *data = NULL;
        return;
    }

    if (extract_count == 1) {
        guint n_mem = gst_buffer_n_memory(buffer);
        LOGI("📊 Buffer: %u memory blocks, size=%zu", n_mem, gst_buffer_get_size(buffer));
        if (n_mem > 0) {
            GstMemory *mem = gst_buffer_peek_memory(buffer, 0);
            if (mem && mem->allocator) {
                LOGI("📊 Memory allocator: %s", GST_OBJECT_NAME(mem->allocator));
            }
        }
    }

    GstMapInfo map;
    if (extract_count <= 3) {
        LOGI("📊 Вызываем gst_buffer_map #%d...", extract_count);
    }
    if (gst_buffer_map(buffer, &map, GST_MAP_READ)) {
        *size = map.size;
        if (extract_count == 1) {
            LOGI("📊 Buffer size: %zu bytes (RGBA format)", *size);
        }
        *data = (guint8*)g_memdup2(map.data, map.size);
        gst_buffer_unmap(buffer, &map);
    } else {
        *data = NULL;
    }
}

// ═══════════════════════════════════════════════════════════════════════
// HWFrameStore — потокобезопасное хранилище кадра.
// appsink callback вызывается на streaming thread (где доступен GL контекст),
// маппит буфер, копирует данные в HWFrameStore.
// Go-горутина забирает данные через hw_frame_poll().
// ═══════════════════════════════════════════════════════════════════════

typedef struct {
    guint8 *data;
    gint    width;
    gint    height;
    gsize   size;
    GMutex  mutex;
    GCond   cond;
    gboolean has_frame;
    gboolean active;
} HWFrameStore;

static HWFrameStore hw_frame_store;

// ═══════════════════════════════════════════════════════════════════════
// Автоматический fallback на SW декодер при провале HW (amcviddec + GL)
// ═══════════════════════════════════════════════════════════════════════

// is_hw_decoder_failed — Go проверяет этот флаг и триггерит переподключение с SW.
// Возвращает 1 только когда нужен reconnect (сбрасывается в hw_frame_store_init при создании нового pipeline).
int is_hw_decoder_failed() {
    return g_reconnect_requested ? 1 : 0;
}

void hw_frame_store_init() {
    memset(&hw_frame_store, 0, sizeof(hw_frame_store));
    g_mutex_init(&hw_frame_store.mutex);
    g_cond_init(&hw_frame_store.cond);
    hw_frame_store.active = TRUE;
    g_hw_map_fail_count = 0;
    g_reconnect_requested = FALSE;  // новый pipeline создан — больше не триггерить Reconnect
    g_last_rtp_packet_us = 0;
    g_last_appsink_frame_us = 0;
    LOGI("✅ hw_frame_store инициализирован (force_sw=%d)", g_force_sw_decoder);
}

void hw_frame_store_stop() {
    g_mutex_lock(&hw_frame_store.mutex);
    hw_frame_store.active = FALSE;
    g_cond_broadcast(&hw_frame_store.cond);
    g_mutex_unlock(&hw_frame_store.mutex);
    LOGI("🛑 hw_frame_store остановлен");
}

void hw_frame_store_destroy() {
    g_mutex_lock(&hw_frame_store.mutex);
    if (hw_frame_store.data) {
        g_free(hw_frame_store.data);
        hw_frame_store.data = NULL;
    }
    hw_frame_store.has_frame = FALSE;
    g_mutex_unlock(&hw_frame_store.mutex);
    g_mutex_clear(&hw_frame_store.mutex);
    g_cond_clear(&hw_frame_store.cond);
    LOGI("🗑️ hw_frame_store уничтожен");
}

// appsink_new_sample_cb — вызывается на streaming thread GStreamer.
// На этом потоке GL контекст доступен, поэтому gst_buffer_map работает корректно
// даже для буферов в GL-памяти (amcviddec → glcolorconvert → gldownload).
static GstFlowReturn appsink_new_sample_cb(GstAppSink *appsink, gpointer user_data) {
    (void)user_data;
    static int cb_count = 0;
    cb_count++;

    GstSample *sample = gst_app_sink_pull_sample(appsink);
    if (!sample) return GST_FLOW_OK;

    GstCaps *caps = gst_sample_get_caps(sample);
    GstBuffer *buffer = gst_sample_get_buffer(sample);
    if (!caps || !buffer) {
        gst_sample_unref(sample);
        return GST_FLOW_OK;
    }

    GstStructure *s = gst_caps_get_structure(caps, 0);
    gint w = 0, h = 0;
    gst_structure_get_int(s, "width", &w);
    gst_structure_get_int(s, "height", &h);

    if (cb_count == 1) {
        gchar *cs = gst_caps_to_string(caps);
        LOGI("📐 [CB] Frame #1: %dx%d, caps: %s", w, h, cs);
        g_free(cs);
        guint nm = gst_buffer_n_memory(buffer);
        LOGI("📊 [CB] Buffer: %u memory blocks, size=%zu", nm, gst_buffer_get_size(buffer));
        if (nm > 0) {
            GstMemory *mem = gst_buffer_peek_memory(buffer, 0);
            if (mem && mem->allocator)
                LOGI("📊 [CB] Allocator: %s", GST_OBJECT_NAME(mem->allocator));
        }
    }

    GstMapInfo map;
    if (cb_count <= 5) LOGI("📊 [CB] gst_buffer_map #%d (streaming thread)...", cb_count);

    // ═══ Safety net: активируем наш EGL контекст на streaming thread ═══
    // GL-память (amcviddec → glcolorconvert → gldownload) требует
    // активного GL контекста для маппинга (glReadPixels / transfer).
    // Сохраняем предыдущий контекст (GStreamer может установить свой).
    EGLContext prev_egl_ctx = eglGetCurrentContext();
    EGLDisplay prev_egl_dpy = eglGetCurrentDisplay();
    EGLSurface prev_egl_draw = eglGetCurrentSurface(EGL_DRAW);
    EGLSurface prev_egl_read = eglGetCurrentSurface(EGL_READ);
    gboolean egl_made_current = FALSE;

    if (g_egl_context != EGL_NO_CONTEXT && prev_egl_ctx == EGL_NO_CONTEXT) {
        // GL контекст не активен на этом потоке — активируем наш
        egl_made_current = eglMakeCurrent(g_egl_display, g_egl_surface, g_egl_surface, g_egl_context);
        if (cb_count <= 3) LOGI("📊 [CB] eglMakeCurrent(ours): %s", egl_made_current ? "OK" : "FAIL");
    } else if (cb_count <= 3) {
        LOGI("📊 [CB] GL context already active: %p", (void*)prev_egl_ctx);
    }

    if (!gst_buffer_map(buffer, &map, GST_MAP_READ)) {
        // Первая попытка не удалась — если EGL ещё не наш, пробуем принудительно
        if (!egl_made_current && g_egl_context != EGL_NO_CONTEXT) {
            egl_made_current = eglMakeCurrent(g_egl_display, g_egl_surface, g_egl_surface, g_egl_context);
            if (cb_count <= 5) LOGI("📊 [CB] retry eglMakeCurrent: %s", egl_made_current ? "OK" : "FAIL");
            if (egl_made_current && gst_buffer_map(buffer, &map, GST_MAP_READ)) {
                if (cb_count <= 3) LOGI("✅ [CB] gst_buffer_map OK after eglMakeCurrent retry #%d", cb_count);
                goto map_success;
            }
        }
        g_hw_map_fail_count++;
        if (cb_count <= 10) LOGE("❌ [CB] gst_buffer_map FAILED #%d (consecutive=%d)", cb_count, g_hw_map_fail_count);
        if (g_hw_map_fail_count >= 50 && !g_force_sw_decoder) {
            LOGE("❌ [CB] %d подряд провалов gst_buffer_map — GL декодер не работает, переключаемся на avdec_h264", g_hw_map_fail_count);
            g_force_sw_decoder = TRUE;
            g_reconnect_requested = TRUE;  // Go триггернет Reconnect
        }
        // Восстанавливаем предыдущий контекст
        if (egl_made_current) {
            if (prev_egl_ctx != EGL_NO_CONTEXT)
                eglMakeCurrent(prev_egl_dpy, prev_egl_draw, prev_egl_read, prev_egl_ctx);
            else
                eglMakeCurrent(g_egl_display, EGL_NO_SURFACE, EGL_NO_SURFACE, EGL_NO_CONTEXT);
        }
        gst_sample_unref(sample);
        return GST_FLOW_OK;
    }
map_success:
    (void)0; // label requires statement

    // Успех — сбрасываем счётчик провалов
    g_hw_map_fail_count = 0;
    g_last_appsink_frame_us = g_get_monotonic_time();
    gsize frame_size = map.size;
    if (cb_count <= 5) LOGI("✅ [CB] gst_buffer_map OK #%d, size=%zu", cb_count, frame_size);

    // Копируем данные в HWFrameStore (потокобезопасно)
    g_mutex_lock(&hw_frame_store.mutex);
    if (hw_frame_store.active) {
        if (hw_frame_store.data) g_free(hw_frame_store.data);
        hw_frame_store.data = (guint8 *)g_memdup2(map.data, frame_size);
        hw_frame_store.width = w;
        hw_frame_store.height = h;
        hw_frame_store.size = frame_size;
        hw_frame_store.has_frame = TRUE;
        g_cond_signal(&hw_frame_store.cond);
    }
    g_mutex_unlock(&hw_frame_store.mutex);

    gst_buffer_unmap(buffer, &map);

    // Восстанавливаем предыдущий EGL контекст (если мы его подменяли)
    if (egl_made_current) {
        if (prev_egl_ctx != EGL_NO_CONTEXT)
            eglMakeCurrent(prev_egl_dpy, prev_egl_draw, prev_egl_read, prev_egl_ctx);
        else
            eglMakeCurrent(g_egl_display, EGL_NO_SURFACE, EGL_NO_SURFACE, EGL_NO_CONTEXT);
    }

    gst_sample_unref(sample);

    if (cb_count <= 3 || cb_count % 300 == 0) {
        LOGI("📊 [CB] Frame #%d OK: %dx%d, %zu bytes", cb_count, w, h, frame_size);
    }
    return GST_FLOW_OK;
}

// hw_frame_poll — вызывается из Go для получения последнего кадра.
// Ждёт до timeout_ms миллисекунд, если кадра пока нет.
// Возвращает 1 если кадр получен, 0 если нет.
// Данные (*data) нужно освободить через g_free из Go.
int hw_frame_poll(guint8 **data, gint *width, gint *height, gsize *size, int timeout_ms) {
    g_mutex_lock(&hw_frame_store.mutex);

    if (!hw_frame_store.has_frame && hw_frame_store.active) {
        gint64 deadline = g_get_monotonic_time() + (gint64)timeout_ms * 1000;
        g_cond_wait_until(&hw_frame_store.cond, &hw_frame_store.mutex, deadline);
    }

    if (!hw_frame_store.has_frame || !hw_frame_store.active || !hw_frame_store.data) {
        g_mutex_unlock(&hw_frame_store.mutex);
        *data = NULL;
        return 0;
    }

    // Забираем данные (ownership transfer → Go вызовет g_free)
    *data = hw_frame_store.data;
    *width = hw_frame_store.width;
    *height = hw_frame_store.height;
    *size = hw_frame_store.size;
    hw_frame_store.data = NULL;
    hw_frame_store.has_frame = FALSE;
    g_mutex_unlock(&hw_frame_store.mutex);
    return 1;
}

long long video_latency_rtp_age_us() {
    gint64 ts = g_last_rtp_packet_us;
    if (ts <= 0) return -1;
    return (long long)(g_get_monotonic_time() - ts);
}

long long video_latency_appsink_age_us() {
    gint64 ts = g_last_appsink_frame_us;
    if (ts <= 0) return -1;
    return (long long)(g_get_monotonic_time() - ts);
}

// setup_appsink_callbacks — настраивает appsink на callback-режим.
// Callback вызывается на streaming thread GStreamer, где GL контекст доступен.
void setup_appsink_callbacks(GstElement *pipeline) {
    if (!pipeline) return;
    GstElement *appsink = gst_bin_get_by_name(GST_BIN(pipeline), "sink");
    if (!appsink) {
        LOGE("❌ setup_appsink_callbacks: sink не найден в pipeline");
        return;
    }
    GstAppSinkCallbacks callbacks;
    memset(&callbacks, 0, sizeof(callbacks));
    callbacks.new_sample = appsink_new_sample_cb;
    gst_app_sink_set_callbacks(GST_APP_SINK(appsink), &callbacks, NULL, NULL);
    gst_object_unref(appsink);
    LOGI("✅ Appsink callbacks установлены (gst_buffer_map на streaming thread)");
}
*/
import "C"

import (
	"fmt"
	"image"
	"os"
	"sync"
	"time"
	"unsafe"

	"usbridge-client/internal/models"

	"fyne.io/fyne/v2/driver"
	"github.com/sirupsen/logrus"
)

// GStreamerService сервис для работы с GStreamer на Android
type GStreamerService struct {
	config *models.AppConfig

	// GStreamer pipeline
	pipeline  unsafe.Pointer
	videoMode string

	// Размеры кадра (как на Mac: 1920x1080)
	width  int
	height int

	// Состояние
	isConnected  bool
	isConnecting bool

	// Автоматическое переподключение
	autoReconnect        bool
	reconnectAttempts    int
	maxReconnectAttempts int
	manualDisconnect     bool

	// Каналы для видео кадров
	videoFrameChan chan image.Image
	stopChan       chan struct{}

	// Последний кадр для throttled UI (30fps, не тормозит main thread)
	latestFrame     image.Image
	latestFrameMeta videoLatencyFrameMeta
	latestFrameMu   sync.RWMutex

	// Статистика
	frameDropCount int64
	lastFrameTime  time.Time
	frameCount     int64
	latencyProfile videoLatencyProfile

	// Мьютексы
	mutex sync.RWMutex

	// Callbacks
	onFrameReceived func(image.Image)
	onStateChanged  func(string)
	onError         func(error)
}

// NewGStreamerService создает новый GStreamer сервис для Android
func NewGStreamerService(config *models.AppConfig) *GStreamerService {
	// ВАЖНО: Устанавливаем JNI ДО gst_init — androidmedia регистрирует amcvideodec при загрузке
	driver.RunNative(func(ctx any) error {
		if androidCtx, ok := ctx.(*driver.AndroidContext); ok && androidCtx.VM != 0 && androidCtx.Env != 0 && androidCtx.Ctx != 0 {
			C.gst_android_set_java_vm_and_context(
				(*C.JavaVM)(unsafe.Pointer(androidCtx.VM)),
				(*C.JNIEnv)(unsafe.Pointer(androidCtx.Env)),
				(C.jobject)(unsafe.Pointer(androidCtx.Ctx)),
			)
			logrus.Info("✅ Android: JNI VM установлен для amcvideodec (до gst_init)")
		}
		return nil
	})

	// Инициализируем GStreamer (androidmedia загрузится с уже установленным VM)
	C.init_gstreamer()

	gs := &GStreamerService{
		config:               config,
		videoFrameChan:       make(chan image.Image, config.BufferSize),
		stopChan:             make(chan struct{}),
		autoReconnect:        true,
		reconnectAttempts:    0,
		maxReconnectAttempts: 5,
		width:                1920,
		height:               1080,
		videoMode:            models.VideoModeH264,
	}

	logrus.Info("✅ GStreamer сервис для Android инициализирован")
	return gs
}

func durationFromMicroseconds(us C.longlong) time.Duration {
	if us < 0 {
		return 0
	}
	return time.Duration(int64(us)) * time.Microsecond
}

// ConnectToUDP подключается к UDP H.264 потоку (новый протокол)
func (gs *GStreamerService) ConnectToUDP(udpPort int) error {
	gs.mutex.Lock()
	defer gs.mutex.Unlock()

	if gs.isConnecting || gs.isConnected {
		return fmt.Errorf("уже подключен или подключается")
	}

	gs.manualDisconnect = false
	gs.frameDropCount = 0
	gs.lastFrameTime = time.Time{}

	// Пересоздаем stopChan для нового подключения (старый может быть закрыт)
	gs.stopChan = make(chan struct{})

	gs.isConnecting = true
	logrus.Infof("🔗 Android: Подключение к UDP видеопотоку mode=%s...", gs.videoMode)

	if udpPort <= 0 {
		udpPort = models.DefaultVideoUDPPort
	}

	// Инициализируем хранилище кадров для callback-режима
	C.hw_frame_store_init()

	pipelineMode := C.int(C.PIPELINE_MODE_H264)
	if gs.videoMode == models.VideoModeJPEGRTP {
		pipelineMode = C.int(C.PIPELINE_MODE_JPEG_RTP)
	}

	// Создаем pipeline под выбранный видеорежим
	pipeline := C.create_pipeline(C.int(udpPort), C.int(gs.width), C.int(gs.height), pipelineMode)
	if pipeline == nil {
		gs.isConnecting = false
		return fmt.Errorf("ошибка создания GStreamer pipeline")
	}

	gs.pipeline = unsafe.Pointer(pipeline)

	// GL-контекст и bus sync для аппаратного декодера (amcviddec→gldownload)
	C.gst_android_prepare_hw_pipeline((*C.GstElement)(gs.pipeline))

	// Настраиваем appsink callbacks — gst_buffer_map вызывается на streaming thread (с GL контекстом)
	C.setup_appsink_callbacks((*C.GstElement)(gs.pipeline))

	// Запускаем pipeline
	logrus.Infof("▶️ Android: Установка pipeline в PLAYING (udpsrc port=%d, ожидаем RTP)...", udpPort)
	ret := C.gst_element_set_state((*C.GstElement)(gs.pipeline), C.GST_STATE_PLAYING)
	retStr := "?"
	switch ret {
	case C.GST_STATE_CHANGE_FAILURE:
		retStr = "FAILURE"
	case C.GST_STATE_CHANGE_SUCCESS:
		retStr = "SUCCESS"
	case C.GST_STATE_CHANGE_ASYNC:
		retStr = "ASYNC"
	case C.GST_STATE_CHANGE_NO_PREROLL:
		retStr = "NO_PREROLL"
	}
	logrus.Infof("▶️ Android: gst_element_set_state вернул: %s", retStr)
	if ret == C.GST_STATE_CHANGE_FAILURE {
		C.drain_bus_messages((*C.GstElement)(gs.pipeline))
		gs.isConnecting = false
		return fmt.Errorf("ошибка запуска GStreamer pipeline")
	}

	logrus.Info("✅ Android: Pipeline запущен (async) — возвращаемся, ожидаем RTP после StartVideo")
	go func() { C.drain_bus_messages((*C.GstElement)(gs.pipeline)) }()

	gs.isConnected = true
	gs.isConnecting = false

	if gs.onStateChanged != nil {
		gs.onStateChanged("connected")
	}

	logrus.Infof("✅ Android: GStreamer подключен к UDP видеопотоку mode=%s", gs.videoMode)

	// Запускаем UI refresh (30fps, не тормозит main thread)
	go gs.uiRefreshLoop()
	// Запускаем обработку кадров (получение из appsink)
	go gs.processFrames()

	return nil
}

func (gs *GStreamerService) ResetRuntimeDecoderFallback() {
	C.gst_android_reset_runtime_decoder_fallback()
}

// uiRefreshLoop обновляет UI с последним кадром с фиксированной частотой (30fps)
// Не блокирует main thread — только один fyne.Do в 33ms
func (gs *GStreamerService) uiRefreshLoop() {
	ticker := time.NewTicker(33 * time.Millisecond) // ~30 fps
	defer ticker.Stop()

	for {
		select {
		case <-gs.stopChan:
			return
		case <-ticker.C:
			gs.latestFrameMu.Lock()
			img := gs.latestFrame
			meta := gs.latestFrameMeta
			gs.latestFrame = nil
			gs.latestFrameMeta = videoLatencyFrameMeta{}
			gs.latestFrameMu.Unlock()

			if img == nil {
				continue
			}

			gs.mutex.RLock()
			callback := gs.onFrameReceived
			gs.mutex.RUnlock()

			if callback != nil {
				gs.recordUIDelay(time.Since(meta.producedAt), meta, "Android")
				callback(img)
			}
		}
	}
}

// processFrames обрабатывает видео кадры из GStreamer (callback mode).
// Кадры маппятся на streaming thread (appsink callback), а здесь забираются через hw_frame_poll.
// Это решает проблему зависания gst_buffer_map на GL-памяти из Go-горутины.
func (gs *GStreamerService) processFrames() {
	logrus.Info("🎬 Android: Запуск обработки GStreamer кадров (callback mode)...")

	gs.mutex.RLock()
	stopChan := gs.stopChan
	gs.mutex.RUnlock()

	logrus.Infof("🎬 Android: Pipeline pointer: %p", gs.pipeline)
	logrus.Infof("🎬 Android: Callback установлен: %v", gs.onFrameReceived != nil)

	frameNum := 0
	noFrameCount := 0
	lastErrorCheck := time.Now()

	for {
		select {
		case <-stopChan:
			logrus.Info("🛑 Android: Остановка обработки GStreamer кадров по сигналу stopChan")
			return
		default:
		}

		// Получаем кадр из HWFrameStore (замаплен на streaming thread через callback)
		var data *C.guint8
		var width, height C.gint
		var frameSize C.gsize

		got := C.hw_frame_poll(&data, &width, &height, &frameSize, 30) // 30ms таймаут
		if got == 0 || data == nil {
			noFrameCount++

			// Быстрая проверка: HW декодер провалился → автоматический fallback на SW
			if C.is_hw_decoder_failed() != 0 {
				logrus.Warn("⚠️ Android: HW декодер (amcviddec) провалился (GL context), переключаемся на avdec_h264...")
				go gs.Reconnect() // Reconnect вызовет create_pipeline, который теперь пропустит amcviddec
				return
			}

			// Периодически проверяем ошибки и EOS
			if noFrameCount%100 == 0 && time.Since(lastErrorCheck) > 5*time.Second {
				gs.mutex.RLock()
				currentPipeline := gs.pipeline
				shouldReconnect := gs.autoReconnect && !gs.manualDisconnect
				gs.mutex.RUnlock()

				if currentPipeline == nil {
					logrus.Warn("⚠️ Android: Pipeline is nil, завершаем обработку")
					return
				}

				// Проверяем bus на ошибки
				errStr := C.check_pipeline_errors((*C.GstElement)(currentPipeline))
				if errStr != nil {
					errMsg := C.GoString(errStr)
					C.g_free(C.gpointer(errStr))
					logrus.Errorf("❌ Android: Ошибка pipeline: %s", errMsg)
					if gs.onError != nil {
						gs.onError(fmt.Errorf("GStreamer: %s", errMsg))
					}
					return
				}

				// Проверяем EOS
				sinkName := C.CString("sink")
				appsinkElement := C.gst_bin_get_by_name((*C.GstBin)(currentPipeline), sinkName)
				C.free(unsafe.Pointer(sinkName))

				if appsinkElement != nil {
					isEOS := C.gst_app_sink_is_eos((*C.GstAppSink)(unsafe.Pointer(appsinkElement)))
					C.gst_object_unref(C.gpointer(appsinkElement))

					if isEOS != 0 {
						logrus.Warn("⚠️ Android: Обнаружен EOS (конец потока)")
						if shouldReconnect {
							logrus.Info("🔄 Android: Запуск автоматического переподключения...")
							go gs.attemptReconnect()
						}
						return
					}
				}

				lastErrorCheck = time.Now()
				logrus.Warnf("⚠️ Android: Нет кадров уже %d итераций (ожидаем callback)", noFrameCount)
				go func(p unsafe.Pointer) { C.drain_bus_messages((*C.GstElement)(p)) }(currentPipeline)
			}
			continue
		}

		// Кадр получен
		noFrameCount = 0
		frameNum++

		copyStarted := time.Now()
		w := int(width)
		h := int(height)
		frameData := C.GoBytes(unsafe.Pointer(data), C.int(frameSize))
		C.g_free(C.gpointer(data))

		if frameNum <= 3 {
			logrus.Infof("🖼️ Android: Кадр #%d: %dx%d, %d bytes (callback mode)", frameNum, w, h, len(frameData))
		}

		// Создаем RGBA image — данные уже в формате RGBA из GStreamer
		img := image.NewRGBA(image.Rect(0, 0, w, h))
		copy(img.Pix, frameData)
		producedAt := time.Now()
		meta := videoLatencyFrameMeta{
			producedAt:  producedAt,
			rtpAge:      durationFromMicroseconds(C.video_latency_rtp_age_us()),
			appsinkAge:  durationFromMicroseconds(C.video_latency_appsink_age_us()),
			copyTime:    producedAt.Sub(copyStarted),
			frameWidth:  w,
			frameHeight: h,
		}

		// Обновляем статистику
		gs.mutex.Lock()
		gs.frameCount++
		currentCount := gs.frameCount
		gs.lastFrameTime = producedAt
		gs.mutex.Unlock()

		if currentCount <= 3 || currentCount%100 == 0 {
			logrus.Infof("📊 Android: Кадр #%d обработан (callback mode)", currentCount)
		}

		// Сохраняем последний кадр — uiRefreshLoop заберёт и отправит в UI
		gs.latestFrameMu.Lock()
		gs.latestFrame = img
		gs.latestFrameMeta = meta
		gs.latestFrameMu.Unlock()
		gs.recordIngressLatency(meta)
	}
}

// attemptReconnect пытается переподключиться к UDP потоку
func (gs *GStreamerService) attemptReconnect() {
	// Делаем несколько попыток в цикле вместо рекурсии
	for attempt := 1; attempt <= gs.maxReconnectAttempts; attempt++ {
		gs.mutex.Lock()

		// Проверяем условия для переподключения
		if !gs.autoReconnect || gs.manualDisconnect || gs.isConnecting {
			gs.mutex.Unlock()
			logrus.Infof("🔄 Android: Переподключение прервано: autoReconnect=%v, manualDisconnect=%v, isConnecting=%v",
				gs.autoReconnect, gs.manualDisconnect, gs.isConnecting)
			return
		}

		maxAttempts := gs.maxReconnectAttempts
		gs.mutex.Unlock()

		logrus.Infof("🔄 Android: Попытка переподключения #%d/%d...", attempt, maxAttempts)

		// Задержка перед переподключением (увеличивается с каждой попыткой)
		delay := time.Duration(attempt) * 2 * time.Second
		if delay > 10*time.Second {
			delay = 10 * time.Second // Максимум 10 секунд
		}

		if attempt > 1 {
			logrus.Infof("⏳ Android: Задержка перед переподключением: %v", delay)
			time.Sleep(delay)
		}

		// Переподключаемся
		if err := gs.Reconnect(); err != nil {
			logrus.Errorf("❌ Android: Ошибка переподключения #%d: %v", attempt, err)

			// Если это последняя попытка
			if attempt >= maxAttempts {
				logrus.Errorf("❌ Android: Все %d попыток переподключения исчерпаны", maxAttempts)
				gs.mutex.Lock()
				gs.autoReconnect = false
				gs.mutex.Unlock()
				return
			}
			// Продолжаем цикл для следующей попытки
			continue
		}

		// Успешное переподключение
		logrus.Info("✅ Android: Успешное переподключение!")
		gs.mutex.Lock()
		gs.reconnectAttempts = 0
		gs.mutex.Unlock()
		return
	}
}

// Disconnect отключается от UDP потока
func (gs *GStreamerService) Disconnect() error {
	gs.mutex.Lock()

	if !gs.isConnected && !gs.isConnecting {
		gs.mutex.Unlock()
		logrus.Info("🔌 Android: Disconnect: уже отключен")
		return nil
	}

	gs.manualDisconnect = true
	gs.isConnected = false
	gs.isConnecting = false
	logrus.Info("🔌 Android: Отключение от GStreamer...")

	// Сохраняем stopChan перед unlock
	stopChan := gs.stopChan
	pipeline := gs.pipeline
	gs.mutex.Unlock()

	// Останавливаем frame store (пробуждает заблокированный hw_frame_poll)
	C.hw_frame_store_stop()

	// Останавливаем обработку (безопасно закрываем канал)
	if stopChan != nil {
		select {
		case <-stopChan:
			// Канал уже закрыт
			logrus.Info("🔌 Android: stopChan уже закрыт")
		default:
			close(stopChan)
			logrus.Info("🔌 Android: stopChan закрыт")
		}
	}

	// Небольшая задержка для завершения горутины processFrames
	time.Sleep(200 * time.Millisecond)

	// Останавливаем pipeline
	if pipeline != nil {
		logrus.Info("🛑 Android: Остановка GStreamer pipeline...")
		C.gst_element_set_state((*C.GstElement)(pipeline), C.GST_STATE_NULL)

		// Небольшая задержка для корректного перехода в StateNull
		time.Sleep(100 * time.Millisecond)

		C.gst_object_unref(C.gpointer(pipeline))

		gs.mutex.Lock()
		gs.pipeline = nil
		gs.mutex.Unlock()

		logrus.Info("✅ Android: GStreamer pipeline остановлен")
	}

	// Уничтожаем frame store после остановки pipeline
	C.hw_frame_store_destroy()

	// Очищаем канал кадров от оставшихся данных
	gs.mutex.Lock()
	if gs.videoFrameChan != nil {
		// Неблокирующая очистка канала
		for {
			select {
			case <-gs.videoFrameChan:
				// Игнорируем оставшиеся кадры
			default:
				// Канал пуст
				goto doneClearing
			}
		}
	doneClearing:
		logrus.Info("✅ Android: Канал кадров очищен")
	}
	gs.mutex.Unlock()

	if gs.onStateChanged != nil {
		gs.onStateChanged("disconnected")
	}

	logrus.Info("✅ Android: GStreamer отключен и очищен")
	return nil
}

// SetOnFrameReceived устанавливает callback для получения кадров
func (gs *GStreamerService) SetOnFrameReceived(callback func(image.Image)) {
	gs.onFrameReceived = callback
}

// SetOnStateChanged устанавливает callback для изменения состояния
func (gs *GStreamerService) SetOnStateChanged(callback func(string)) {
	gs.onStateChanged = callback
}

// SetOnError устанавливает callback для ошибок
func (gs *GStreamerService) SetOnError(callback func(error)) {
	gs.onError = callback
}

// IsConnected возвращает состояние подключения
func (gs *GStreamerService) IsConnected() bool {
	gs.mutex.RLock()
	defer gs.mutex.RUnlock()
	return gs.isConnected
}

// GetStats возвращает статистику
func (gs *GStreamerService) GetStats() map[string]interface{} {
	gs.mutex.RLock()
	defer gs.mutex.RUnlock()

	return map[string]interface{}{
		"connected":        gs.isConnected,
		"frame_count":      gs.frameCount,
		"frames_dropped":   gs.frameDropCount,
		"last_frame":       gs.lastFrameTime,
		"low_latency_mode": gs.config != nil && gs.config.LowLatencyMode,
	}
}

// UpdateHost обновляет хост
func (gs *GStreamerService) UpdateHost(host string) {
	gs.config.VideoHost = host
}

// ConnectToUDPViaPipe — pipe mode для FRP relay (Android: заглушка)
func (gs *GStreamerService) ConnectToUDPViaPipe(pipeReader *os.File) error {
	_ = pipeReader
	return fmt.Errorf("UDP relay (pipe) пока не реализован на Android, используйте прямое подключение")
}

// ConnectToRTP — алиас для ConnectToUDP (совместимость)
func (gs *GStreamerService) ConnectToRTP() error {
	port := gs.config.VideoUDPPort
	if port <= 0 {
		port = models.DefaultVideoUDPPort
	}
	logrus.Infof("🎬 Android: ConnectToRTP port=%d mode=%s (VideoUDPPort, FRP proxy шлёт сюда)", port, gs.videoMode)
	return gs.ConnectToUDP(port)
}

// UpdateVideoPort обновляет порт видеопотока (RTP/UDP)
func (gs *GStreamerService) UpdateVideoPort(port int) {
	gs.config.VideoUDPPort = port
}

// UpdateVideoUDPPort обновляет порт приёма UDP видео
func (gs *GStreamerService) UpdateVideoUDPPort(port int) {
	gs.config.VideoUDPPort = port
}

func (gs *GStreamerService) SetVideoMode(mode string) {
	gs.mutex.Lock()
	defer gs.mutex.Unlock()
	switch mode {
	case models.VideoModeJPEGRTP:
		gs.videoMode = models.VideoModeJPEGRTP
	default:
		gs.videoMode = models.VideoModeH264
	}
}

func (gs *GStreamerService) SetExpectedVideoSize(width, height int) {
	_ = width
	_ = height
}

// GetConfig возвращает конфигурацию
func (gs *GStreamerService) GetConfig() *models.AppConfig {
	return gs.config
}

func (gs *GStreamerService) SupportsNativeFullscreen() bool {
	return false
}

func (gs *GStreamerService) IsNativeFullscreenActive() bool {
	return false
}

func (gs *GStreamerService) StartNativeFullscreen() error {
	return fmt.Errorf("native fullscreen is not implemented on Android yet")
}

func (gs *GStreamerService) StopNativeFullscreen() error {
	return nil
}

// SetAutoReconnect включает/выключает автоматическое переподключение
func (gs *GStreamerService) SetAutoReconnect(enabled bool) {
	gs.autoReconnect = enabled
}

// SetMaxReconnectAttempts устанавливает максимальное количество попыток переподключения
func (gs *GStreamerService) SetMaxReconnectAttempts(max int) {
	gs.mutex.Lock()
	defer gs.mutex.Unlock()
	gs.maxReconnectAttempts = max
	gs.reconnectAttempts = 0 // Сбрасываем счетчик при изменении максимума
}

// Reconnect принудительно переподключается к UDP потоку (для смены устройств)
func (gs *GStreamerService) Reconnect() error {
	logrus.Info("🔄 Android: Принудительное переподключение (смена устройства)...")

	// Сначала отключаемся
	if err := gs.Disconnect(); err != nil {
		logrus.Warnf("⚠️ Android: Ошибка при отключении перед переподключением: %v", err)
	}

	// Ждем немного для корректного отключения
	time.Sleep(500 * time.Millisecond)

	// Сбрасываем счетчик попыток переподключения
	gs.mutex.Lock()
	gs.reconnectAttempts = 0
	gs.autoReconnect = true
	gs.manualDisconnect = false
	gs.mutex.Unlock()

	// Подключаемся заново
	logrus.Info("🔗 Android: Подключаемся к новому устройству...")
	return gs.ConnectToRTP()
}
