// vk_video_impl_android.c — Vulkan overlay video renderer for Android.
//
// Architecture:
//   • SurfaceView overlay created by VulkanOverlayBridge.kt (Kotlin).
//   • VkAndroidSurfaceKHR on the SurfaceView ANativeWindow.
//   • VkSwapchainKHR with MAILBOX (or FIFO) present mode.
//   • Render thread: staging buffer → VkImage → blit to swapchain.
//   • Frame source: RGBA pixels from android_gl_get_frame() (existing EGL decode path).
//   • Eliminates the Fyne canvas CPU path for video — present goes direct to SurfaceFlinger.
//
// Thread safety:
//   • android_vk_create / android_vk_destroy — called from CGO goroutines.
//   • android_vk_try_submit — called from decoder C thread; protected by g_mu.
//   • Render thread: pure Vulkan, no JNI calls.

#ifdef __ANDROID__

#define VK_USE_PLATFORM_ANDROID_KHR
#include <vulkan/vulkan.h>
#include <vulkan/vulkan_android.h>
#include <android/native_window_jni.h>
#include <android/hardware_buffer.h>
#include <android/log.h>
#include <media/NdkImage.h>
#include <dlfcn.h>
#include <sys/system_properties.h>
#include <jni.h>
#include <pthread.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include <stdio.h>
#include <stdatomic.h>
#include <time.h>
#include <unistd.h>

#include "shader_arrays.h" // g_ycbcr_rect_vert_spv / g_ycbcr_frag_spv
#include "gles_overlay_android.h"
#include "FramePacer.h"

// Shape the picture is shown in: the target screen's width/height when the
// client's video source setting names it (vk_video_set_display_aspect), so an
// anamorphic capture mode (the MS2130 stretching a 16:10 monitor into
// 1024x768) still looks right; otherwise the frame's own. The Go side fits
// the absolute-mouse rect with the same aspect (service.VideoDisplayAspect).
static volatile float g_display_aspect = 0.0f;
void vk_video_set_display_aspect(float a) { g_display_aspect = a > 0.0f ? a : 0.0f; }
static float display_aspect(int fw, int fh) {
    float a = g_display_aspect;
    return a > 0.0f ? a : (float)fw / (float)(fh > 0 ? fh : 1);
}

#define VTAG "USBridge/VulkanVideo"
#define VLOGI(...) __android_log_print(ANDROID_LOG_INFO,  VTAG, __VA_ARGS__)
#define VLOGE(...) __android_log_print(ANDROID_LOG_ERROR, VTAG, __VA_ARGS__)

// ─── JNI bridge (cached class / JVM) ─────────────────────────────────────────

static JavaVM *g_jvm           = NULL;
static jclass  g_cls_vob       = NULL; // io/usbridge/client/VulkanOverlayBridge
static jclass  g_cls_haptic    = NULL; // io/usbridge/client/HapticBridge

static JNIEnv *get_env(int *need_detach) {
    *need_detach = 0;
    if (!g_jvm) return NULL;
    JNIEnv *env = NULL;
    jint rc = (*g_jvm)->GetEnv(g_jvm, (void **)&env, JNI_VERSION_1_6);
    if (rc == JNI_EDETACHED) { (*g_jvm)->AttachCurrentThread(g_jvm, &env, NULL); *need_detach = 1; }
    return env;
}
static void detach_env(int nd) { if (nd && g_jvm) (*g_jvm)->DetachCurrentThread(g_jvm); }

static int java_create_overlay(int x, int y, int w, int h) {
    if (!g_cls_vob) { VLOGE("VulkanOverlayBridge class not cached"); return 0; }
    int nd; JNIEnv *env = get_env(&nd);
    if (!env) return 0;
    jmethodID mid = (*env)->GetStaticMethodID(env, g_cls_vob, "createOverlay", "(IIII)V");
    if (!mid || (*env)->ExceptionCheck(env)) { (*env)->ExceptionClear(env); detach_env(nd); return 0; }
    (*env)->CallStaticVoidMethod(env, g_cls_vob, mid, (jint)x, (jint)y, (jint)w, (jint)h);
    if ((*env)->ExceptionCheck(env)) (*env)->ExceptionClear(env);
    detach_env(nd);
    return 1;
}

static ANativeWindow *java_get_pending_window(void) {
    if (!g_cls_vob) return NULL;
    int nd; JNIEnv *env = get_env(&nd);
    if (!env) return NULL;
    jmethodID mid = (*env)->GetStaticMethodID(env, g_cls_vob,
        "getPendingSurface", "()Landroid/view/Surface;");
    if (!mid || (*env)->ExceptionCheck(env)) { (*env)->ExceptionClear(env); detach_env(nd); return NULL; }
    jobject surf = (*env)->CallStaticObjectMethod(env, g_cls_vob, mid);
    if ((*env)->ExceptionCheck(env)) { (*env)->ExceptionClear(env); surf = NULL; }
    if (!surf) { detach_env(nd); return NULL; }

    ANativeWindow *win = ANativeWindow_fromSurface(env, surf);

    // Clear pending surface so we don't re-acquire it.
    jmethodID mid_clear = (*env)->GetStaticMethodID(env, g_cls_vob, "clearPendingSurface", "()V");
    if (mid_clear) (*env)->CallStaticVoidMethod(env, g_cls_vob, mid_clear);
    if ((*env)->ExceptionCheck(env)) (*env)->ExceptionClear(env);

    detach_env(nd);
    VLOGI("ANativeWindow=%p", (void*)win);
    return win;
}

static void java_set_rect(int x, int y, int w, int h) {
    if (!g_cls_vob) return;
    int nd; JNIEnv *env = get_env(&nd);
    if (!env) return;
    jmethodID mid = (*env)->GetStaticMethodID(env, g_cls_vob, "setRect", "(IIII)V");
    if (mid) (*env)->CallStaticVoidMethod(env, g_cls_vob, mid, (jint)x, (jint)y, (jint)w, (jint)h);
    if ((*env)->ExceptionCheck(env)) (*env)->ExceptionClear(env);
    detach_env(nd);
}

static void java_destroy_overlay(void) {
    if (!g_cls_vob) return;
    int nd; JNIEnv *env = get_env(&nd);
    if (!env) return;
    jmethodID mid = (*env)->GetStaticMethodID(env, g_cls_vob, "destroy", "()V");
    if (mid) (*env)->CallStaticVoidMethod(env, g_cls_vob, mid);
    if ((*env)->ExceptionCheck(env)) (*env)->ExceptionClear(env);
    detach_env(nd);
}

static void java_set_visible(int visible) {
    if (!g_cls_vob) return;
    int nd; JNIEnv *env = get_env(&nd);
    if (!env) return;
    jmethodID mid = (*env)->GetStaticMethodID(env, g_cls_vob, "setVisible", "(Z)V");
    if (mid) (*env)->CallStaticVoidMethod(env, g_cls_vob, mid, (jboolean)(visible ? 1 : 0));
    if ((*env)->ExceptionCheck(env)) (*env)->ExceptionClear(env);
    detach_env(nd);
}

// ─── Vulkan state ─────────────────────────────────────────────────────────────

static VkInstance               g_inst       = VK_NULL_HANDLE;
static VkPhysicalDevice         g_pdev       = VK_NULL_HANDLE;
static VkDevice                 g_dev        = VK_NULL_HANDLE;
static VkQueue                  g_queue      = VK_NULL_HANDLE;
static uint32_t                 g_qfam       = 0;
static VkSurfaceKHR             g_surf       = VK_NULL_HANDLE;
static VkSwapchainKHR           g_swap       = VK_NULL_HANDLE;
static uint32_t                 g_swap_count = 0;
static VkImage                 *g_swap_imgs  = NULL;
static VkImageView             *g_swap_views = NULL;
static VkFormat                 g_swap_fmt   = VK_FORMAT_UNDEFINED;
static VkExtent2D               g_swap_ext   = {0, 0};

static VkBuffer                 g_stage_buf  = VK_NULL_HANDLE;
static VkDeviceMemory           g_stage_mem  = VK_NULL_HANDLE;
static void                    *g_stage_ptr  = NULL;
static VkDeviceSize             g_stage_sz   = 0;

static VkImage                  g_tex        = VK_NULL_HANDLE;
static VkDeviceMemory           g_tex_mem    = VK_NULL_HANDLE;
static int                      g_tex_w      = 0, g_tex_h = 0;

// ── AHardwareBuffer zero-copy import cache ─────────────────────────────────────
// gl_video_impl_android.c's render_to_hwbuffer hands back one of two
// AHardwareBuffer* it owns and reuses every frame (double-buffered). Rather
// than import (allocate a VkImage + bind memory) on every single frame, cache
// one imported VkImage per distinct AHardwareBuffer* we've seen -- keyed by
// pointer identity, which is stable across frames for each of GL's two slots.
#define HWIMPORT_COUNT 2
static PFN_vkGetAndroidHardwareBufferPropertiesANDROID p_vkGetAndroidHardwareBufferPropertiesANDROID = NULL;
static void        *g_hwimport_src[HWIMPORT_COUNT] = {NULL, NULL}; // AHardwareBuffer* this slot was imported from
static VkImage       g_hwimport_img[HWIMPORT_COUNT] = {VK_NULL_HANDLE, VK_NULL_HANDLE};
static VkDeviceMemory g_hwimport_mem[HWIMPORT_COUNT] = {VK_NULL_HANDLE, VK_NULL_HANDLE};
static int            g_hwimport_w = 0, g_hwimport_h = 0;

static VkCommandPool            g_cmdpool    = VK_NULL_HANDLE;
static VkCommandBuffer          g_cmdbuf     = VK_NULL_HANDLE;
static VkFence                  g_fence      = VK_NULL_HANDLE;
static VkSemaphore              g_img_sem    = VK_NULL_HANDLE;
static VkSemaphore              g_rnd_sem    = VK_NULL_HANDLE;

// Overlay rect atomics (set from Go, read by render thread for swapchain recreation).
static atomic_int g_dst_w, g_dst_h;
static atomic_int g_video_dx, g_video_dy, g_video_dw, g_video_dh;
static atomic_int g_video_sw, g_video_sh;

static void vk_store_video_dest(int dx, int dy, int dw, int dh, int sw, int sh) {
    atomic_store(&g_video_dx, dx);
    atomic_store(&g_video_dy, dy);
    atomic_store(&g_video_dw, dw);
    atomic_store(&g_video_dh, dh);
    atomic_store(&g_video_sw, sw);
    atomic_store(&g_video_sh, sh);
}

// Set to 1 by android_vk_force_recreate_swapchain() to request an explicit
// swapchain recreation on the next render-thread iteration (e.g. after fullscreen).
static atomic_int g_force_recreate;

// ─── Render thread state ──────────────────────────────────────────────────────

static volatile atomic_int g_active;
static volatile atomic_int g_hidden;

static uint8_t        *g_buf    = NULL;
static size_t          g_buf_sz = 0;
static int             g_fw = 0, g_fh = 0, g_fs = 0;
static volatile int    g_ready  = 0;

// AHardwareBuffer zero-copy submission (android_vk_try_submit_hwbuffer):
// nothing to copy, just the pointer GL rendered into and its dimensions.
// Mutually exclusive with g_buf/g_ready above for a given frame; the
// Net Graph HUD switches to the CPU path mid-session (see
// moonlight_cgo_android.go's dr_submit), so each submit must clear the
// other path's pending flag or the render thread keeps presenting the
// leftover AHB frame (HUD appears to flicker).
static void            *g_pend_ahb = NULL;
static int              g_pend_ahb_w = 0, g_pend_ahb_h = 0;
static volatile int     g_ahb_ready  = 0;
// Render-thread "forget last frame of the other path" -- cursor-only
// redraws otherwise re-blit the stale AHB (no HUD) or CPU (HUD burned in)
// after a path switch.
static atomic_int       g_drop_last_ahb;
static atomic_int       g_drop_last_cpu;

// Decoder images (android_vk_try_submit_aimage). The render thread owns
// every AImage it is handed and deletes it once no frame uses it:
// g_pend_aimg waits for the render thread (under g_mu), g_cur_aimg is the one
// on screen, redrawn for cursor moves (render thread only).
static AImage          *g_pend_aimg = NULL;
static AImage          *g_cur_aimg  = NULL;
static atomic_int       g_drop_last_aimg;
// android_vk_release_aimages asks the render thread to give back every
// AImage before the decoder's AImageReader goes away; it acks by clearing it.
static atomic_int       g_aimg_release_req;

// Jitter buffer at the display (android_vk_set_pacing): a decoder image waits
// in g_pace_img until FramePacer.h's release time for its host capture time
// (the AImage timestamp, see dr_submit) before becoming g_pend_aimg. Pacing
// here rather than before decode also evens out the decoder's own spread.
// All under g_mu.
#define PACE_QUEUE_MAX 3
#define PACE_RENDER_LEAD_US 1000 // released this early: the render itself takes ~1 ms
static int              g_pace_on = 0;
static FRAME_PACER      g_pacer;
static AImage          *g_pace_img[PACE_QUEUE_MAX];
static int64_t          g_pace_at[PACE_QUEUE_MAX];
static int              g_pace_n = 0;
// Average hold the jitter buffer adds (planned wait, 1/32 EMA), for the Net
// Graph's BUF (android_vk_pace_hold_us).
static atomic_llong     g_pace_hold_us;
// Video frames whose interval to the previous one was not ~2 refreshes of a
// 120 Hz panel (under 12.5 ms or over 20.8 ms) -- logged with the render
// stats so pacing on/off can be compared by number, not by eye.
static int              g_cad_uneven = 0, g_cad_n = 0;
static double           g_cad_last = 0;

// No Vulkan device on this phone (e.g. PowerVR GE8320): the overlay draws
// through OpenGL ES instead (gles_overlay_android.c), on the same
// SurfaceView, with the same state and render thread. Set by
// android_vk_create.
static int              g_use_gles = 0;
// GLES mode keeps its CPU frame here (Vulkan copies into g_stage_ptr).
static uint8_t         *g_gles_cpu = NULL;
static size_t           g_gles_cpu_sz = 0;
// Cursor image for GLES mode, uploaded on the render thread.
static uint8_t         *g_gles_cursor = NULL;
static int              g_gles_cursor_w = 0, g_gles_cursor_h = 0;
static atomic_int       g_gles_cursor_dirty;

static pthread_mutex_t g_mu     = PTHREAD_MUTEX_INITIALIZER;
static pthread_t       g_thread = 0;
static int             g_pipe_r = -1, g_pipe_w = -1;

static volatile long long g_submitted = 0, g_rendered = 0;
// FPS tracking (same pattern as vk_video_impl_linux.c).
static volatile long long g_fps_n      = 0;
static volatile double    g_fps_t0     = 0.0;
static volatile float     g_stat_fps   = 0.0f;
static volatile int       g_stat_ready = 0;

// ─── Viewport (zoom / pan) + cursor state ─────────────────────────────────────
// UV range of the video frame that is currently visible.
// Fixed-point: 0..65536 = 0.0..1.0.  Default = full frame.
static atomic_int g_vp_u0_fp;
static atomic_int g_vp_v0_fp;
static atomic_int g_vp_u1_fp = ATOMIC_VAR_INIT(65536);
static atomic_int g_vp_v1_fp = ATOMIC_VAR_INIT(65536);

// Mutex that must be held while writing OR reading the six viewport+cursor
// atomics as a group.  Without this, the render thread can read a viewport
// written by one Go call and a cursor written by the next, producing a frame
// where the cursor flies to a completely wrong screen position.
static pthread_mutex_t g_state_mu = PTHREAD_MUTEX_INITIALIZER;

// When non-zero: 1 = bottom-align fitted video, 2 = top-align (under special
// keys). 0 = center. Bottom while IME-only; top while special keys own the
// header so letterbox does not pool under the keys.
static atomic_int g_v_align;

// Extra destination blit offset in physical pixels (letterbox / zoom pan).
// Applied after aspect-fit and uniform zoomScale. Zero when centered.
static atomic_int g_blit_pan_x;
static atomic_int g_blit_pan_y;
// Uniform zoom of the fitted destination (fixed-point 65536 = 1.0). RustDesk-
// style: scale the blit dest, keep source = full frame — never stretch a UV
// crop to the swapchain (that deformed the picture horizontally).
static atomic_int g_blit_zoom_fp = ATOMIC_VAR_INIT(65536);

// ─── Virtual cursor ───────────────────────────────────────────────────────────
static atomic_int g_cursor_visible;      // 0 = hidden
static atomic_int g_cursor_uc_fp;       // cursor U in frame UV, 0..65536
static atomic_int g_cursor_vc_fp;       // cursor V in frame UV, 0..65536
// Set to 1 when cursor position changed; render thread re-renders last frame
// immediately so cursor movement is decoupled from video frame arrival rate.
static atomic_int g_cursor_dirty;

// Arrow cursor bitmap (9×12). Tip at top-left (0,0).
// Shaft (rows 7-11) is 3px wide (cols 5-7) so the middle pixel (col 6)
// is interior (white) and the two edges are border (black).
static const char *CURSOR_ROWS[12] = {
    "100000000", "110000000",
    "111000000", "111100000",
    "111110000", "111111000",
    "111111100", "111101110",
    "110001110", "100001110",
    "000001110", "000001110",
};
#define CURSOR_BASE_W 9
#define CURSOR_BASE_H 12

static int cursor_is_opaque(int x, int y) {
    if (x < 0 || x >= CURSOR_BASE_W || y < 0 || y >= CURSOR_BASE_H) return 0;
    return CURSOR_ROWS[y][x] == '1';
}
static int cursor_is_border(int x, int y) {
    return !cursor_is_opaque(x-1,y) || !cursor_is_opaque(x+1,y) ||
           !cursor_is_opaque(x,y-1) || !cursor_is_opaque(x,y+1);
}

typedef struct { int rel_x, rel_y, width; uint32_t buf_off; } CursorSpan;
#define MAX_CURSOR_SPANS 256
static CursorSpan     g_cursor_spans[MAX_CURSOR_SPANS];
static int            g_cursor_nspans = 0;
static int            g_cursor_px_w   = 0, g_cursor_px_h = 0;
static VkBuffer       g_cursor_vk_buf = VK_NULL_HANDLE;
static VkDeviceMemory g_cursor_vk_mem = VK_NULL_HANDLE;
static void          *g_cursor_vk_ptr = NULL;

// video_latency_android.c
extern void vlat_note_present(void);
extern void vlat_note_dropped(void);

// ─── helpers ──────────────────────────────────────────────────────────────────

static uint32_t vk_find_mem(VkPhysicalDeviceMemoryProperties *mp,
                             uint32_t type_bits, VkMemoryPropertyFlags props) {
    for (uint32_t i = 0; i < mp->memoryTypeCount; i++)
        if ((type_bits & (1u << i)) && (mp->memoryTypes[i].propertyFlags & props) == props)
            return i;
    return UINT32_MAX;
}

// vk_surface_extent returns the swapchain extent we should use.
// When caps.currentExtent is 0xFFFFFFFF (flexible), falls back to g_dst_w/g_dst_h set by Go.
// We always use VK_SURFACE_TRANSFORM_IDENTITY_BIT_KHR as preTransform (see vk_create_swapchain),
// so caps.currentExtent is already in the app/display coordinate system — no axis swap needed.
static VkExtent2D vk_surface_extent(const VkSurfaceCapabilitiesKHR *caps) {
    if (caps->currentExtent.width != 0xFFFFFFFFu)
        return caps->currentExtent;
    VkExtent2D ext;
    int dw = atomic_load(&g_dst_w), dh = atomic_load(&g_dst_h);
    ext.width  = dw > 0 ? (uint32_t)dw : 1u;
    ext.height = dh > 0 ? (uint32_t)dh : 1u;
    return ext;
}

// ─── Cursor buffer management ─────────────────────────────────────────────────

static void cursor_destroy(void) {
    g_cursor_nspans = 0; g_cursor_px_w = 0; g_cursor_px_h = 0;
    if (!g_dev) return;
    if (g_cursor_vk_ptr) { vkUnmapMemory(g_dev, g_cursor_vk_mem); g_cursor_vk_ptr = NULL; }
    if (g_cursor_vk_buf != VK_NULL_HANDLE) {
        vkDestroyBuffer(g_dev, g_cursor_vk_buf, NULL); g_cursor_vk_buf = VK_NULL_HANDLE;
    }
    if (g_cursor_vk_mem != VK_NULL_HANDLE) {
        vkFreeMemory(g_dev, g_cursor_vk_mem, NULL); g_cursor_vk_mem = VK_NULL_HANDLE;
    }
}

static void cursor_init(int scale) {
    if (!g_dev || !g_pdev || scale < 1 || scale > 4) return;
    cursor_destroy();

    int pw = CURSOR_BASE_W * scale;
    int ph = CURSOR_BASE_H * scale;

    // Count total opaque pixels to size the Vulkan buffer.
    size_t total_pix = 0;
    for (int oy = 0; oy < CURSOR_BASE_H; oy++)
        for (int ox = 0; ox < CURSOR_BASE_W; ox++)
            if (cursor_is_opaque(ox, oy)) total_pix += (size_t)(scale * scale);
    if (!total_pix) return;

    size_t buf_sz = total_pix * 4;
    VkBufferCreateInfo bci = {VK_STRUCTURE_TYPE_BUFFER_CREATE_INFO};
    bci.size = buf_sz;
    bci.usage = VK_BUFFER_USAGE_TRANSFER_SRC_BIT;
    bci.sharingMode = VK_SHARING_MODE_EXCLUSIVE;
    VkBuffer buf = VK_NULL_HANDLE;
    if (vkCreateBuffer(g_dev, &bci, NULL, &buf) != VK_SUCCESS) return;

    VkMemoryRequirements mr;
    vkGetBufferMemoryRequirements(g_dev, buf, &mr);
    VkPhysicalDeviceMemoryProperties mp;
    vkGetPhysicalDeviceMemoryProperties(g_pdev, &mp);
    uint32_t mi = vk_find_mem(&mp, mr.memoryTypeBits,
        VK_MEMORY_PROPERTY_HOST_VISIBLE_BIT | VK_MEMORY_PROPERTY_HOST_COHERENT_BIT);
    if (mi == UINT32_MAX) { vkDestroyBuffer(g_dev, buf, NULL); return; }

    VkMemoryAllocateInfo mai = {VK_STRUCTURE_TYPE_MEMORY_ALLOCATE_INFO};
    mai.allocationSize = mr.size;
    mai.memoryTypeIndex = mi;
    VkDeviceMemory mem = VK_NULL_HANDLE;
    if (vkAllocateMemory(g_dev, &mai, NULL, &mem) != VK_SUCCESS) {
        vkDestroyBuffer(g_dev, buf, NULL); return;
    }
    vkBindBufferMemory(g_dev, buf, mem, 0);
    void *ptr = NULL;
    vkMapMemory(g_dev, mem, 0, VK_WHOLE_SIZE, 0, &ptr);
    if (!ptr) { vkFreeMemory(g_dev, mem, NULL); vkDestroyBuffer(g_dev, buf, NULL); return; }

    uint8_t *dst = (uint8_t *)ptr;
    uint32_t buf_off = 0;
    g_cursor_nspans = 0;

    for (int sy = 0; sy < ph; sy++) {
        int oy = sy / scale;
        int run_start = -1;
        for (int sx = 0; sx <= pw; sx++) {
            int ox = sx / scale;
            int op = (sx < pw) && cursor_is_opaque(ox < CURSOR_BASE_W ? ox : CURSOR_BASE_W-1, oy);
            if (!op && run_start >= 0) {
                int rw = sx - run_start;
                if (g_cursor_nspans < MAX_CURSOR_SPANS) {
                    g_cursor_spans[g_cursor_nspans].rel_x  = run_start;
                    g_cursor_spans[g_cursor_nspans].rel_y  = sy;
                    g_cursor_spans[g_cursor_nspans].width  = rw;
                    g_cursor_spans[g_cursor_nspans].buf_off = buf_off;
                    // Determine byte order from swapchain format (RGBA vs BGRA).
                    // Brand accent: R=0x93 G=0xc5 B=0x72.
                    int is_bgra = (g_swap_fmt == VK_FORMAT_B8G8R8A8_UNORM ||
                                   g_swap_fmt == VK_FORMAT_B8G8R8A8_SRGB  ||
                                   g_swap_fmt == VK_FORMAT_B8G8R8A8_SNORM);
                    for (int px = run_start; px < sx; px++) {
                        int pox = px / scale;
                        if (pox >= CURSOR_BASE_W) pox = CURSOR_BASE_W - 1;
                        if (cursor_is_border(pox, oy)) {
                            dst[buf_off++] = 0;
                            dst[buf_off++] = 0;
                            dst[buf_off++] = 0;
                            dst[buf_off++] = 255;
                        } else {
                            dst[buf_off++] = is_bgra ? 0x72 : 0x93; // ch0: B or R
                            dst[buf_off++] = 0xc5;                   // ch1: G
                            dst[buf_off++] = is_bgra ? 0x93 : 0x72; // ch2: R or B
                            dst[buf_off++] = 0xff;
                        }
                    }
                    g_cursor_nspans++;
                }
                run_start = -1;
            } else if (op && run_start < 0) {
                run_start = sx;
            }
        }
    }

    g_cursor_vk_buf = buf;
    g_cursor_vk_mem = mem;
    g_cursor_vk_ptr = ptr;
    g_cursor_px_w   = pw;
    g_cursor_px_h   = ph;
    VLOGI("cursor init: scale=%d size=%dx%d spans=%d buf=%u bytes", scale, pw, ph, g_cursor_nspans, buf_off);
}

// ─── Vulkan init ──────────────────────────────────────────────────────────────

// g_hw_import_supported is set once vk_create_device has confirmed every
// extension AHardwareBuffer import needs was actually enabled. android_vk_try_submit
// checks this and falls back to the old CPU staging-buffer path (still fully
// functional, just not zero-copy) on any device/driver that's missing one of
// these -- never crashes or blanks the screen over it.
static int g_hw_import_supported = 0;
// Decoder-image path (AImageReader buffers sampled through a
// VkSamplerYcbcrConversion): needs the hwbuffer extensions above plus the
// samplerYcbcrConversion feature.
static int g_ycbcr_supported = 0;
static PFN_vkCreateSamplerYcbcrConversionKHR  p_vkCreateSamplerYcbcrConversionKHR  = NULL;
static PFN_vkDestroySamplerYcbcrConversionKHR p_vkDestroySamplerYcbcrConversionKHR = NULL;
static int g_inst_has_ext_mem_caps = 0; // both instance prereqs enabled successfully

static int has_ext(VkExtensionProperties *avail, uint32_t n, const char *name) {
    for (uint32_t i = 0; i < n; i++)
        if (strcmp(avail[i].extensionName, name) == 0) return 1;
    return 0;
}

static int vk_create_instance(void) {
    uint32_t availN = 0;
    vkEnumerateInstanceExtensionProperties(NULL, &availN, NULL);
    VkExtensionProperties *avail = availN ? malloc(availN * sizeof(*avail)) : NULL;
    if (avail) vkEnumerateInstanceExtensionProperties(NULL, &availN, avail);

    const char *exts[8];
    uint32_t n = 0;
    exts[n++] = VK_KHR_SURFACE_EXTENSION_NAME;
    exts[n++] = VK_KHR_ANDROID_SURFACE_EXTENSION_NAME;
    // Prerequisites for AHardwareBuffer zero-copy import (see vk_create_device).
    // Both have been core-promoted since Vulkan 1.1 but Android's Vulkan is
    // often exposed at 1.0 via extensions, so request them explicitly.
    int hasGpdp2 = avail && has_ext(avail, availN, VK_KHR_GET_PHYSICAL_DEVICE_PROPERTIES_2_EXTENSION_NAME);
    int hasExtMemCap = avail && has_ext(avail, availN, VK_KHR_EXTERNAL_MEMORY_CAPABILITIES_EXTENSION_NAME);
    if (hasGpdp2)     exts[n++] = VK_KHR_GET_PHYSICAL_DEVICE_PROPERTIES_2_EXTENSION_NAME;
    if (hasExtMemCap) exts[n++] = VK_KHR_EXTERNAL_MEMORY_CAPABILITIES_EXTENSION_NAME;
    g_inst_has_ext_mem_caps = hasGpdp2 && hasExtMemCap;
    free(avail);

    VkInstanceCreateInfo ci = { VK_STRUCTURE_TYPE_INSTANCE_CREATE_INFO };
    ci.enabledExtensionCount   = n;
    ci.ppEnabledExtensionNames = exts;
    VkResult r = vkCreateInstance(&ci, NULL, &g_inst);
    if (r != VK_SUCCESS) { VLOGE("vkCreateInstance failed: %d", (int)r); return 0; }
    return 1;
}

static int vk_select_device(void) {
    uint32_t n = 0;
    vkEnumeratePhysicalDevices(g_inst, &n, NULL);
    if (!n) { VLOGE("no Vulkan physical devices"); return 0; }
    VkPhysicalDevice *devs = malloc(n * sizeof(VkPhysicalDevice));
    vkEnumeratePhysicalDevices(g_inst, &n, devs);
    g_pdev = devs[0]; // On Android, typically one GPU.
    free(devs);

    uint32_t qn = 0;
    vkGetPhysicalDeviceQueueFamilyProperties(g_pdev, &qn, NULL);
    VkQueueFamilyProperties *qp = malloc(qn * sizeof(*qp));
    vkGetPhysicalDeviceQueueFamilyProperties(g_pdev, &qn, qp);
    for (uint32_t j = 0; j < qn; j++)
        if (qp[j].queueFlags & VK_QUEUE_GRAPHICS_BIT) { g_qfam = j; break; }
    free(qp);
    return 1;
}

static int vk_create_device(void) {
    float pri = 1.0f;
    VkDeviceQueueCreateInfo qci = { VK_STRUCTURE_TYPE_DEVICE_QUEUE_CREATE_INFO };
    qci.queueFamilyIndex = g_qfam;
    qci.queueCount       = 1;
    qci.pQueuePriorities = &pri;

    uint32_t availN = 0;
    vkEnumerateDeviceExtensionProperties(g_pdev, NULL, &availN, NULL);
    VkExtensionProperties *avail = availN ? malloc(availN * sizeof(*avail)) : NULL;
    if (avail) vkEnumerateDeviceExtensionProperties(g_pdev, NULL, &availN, avail);

    const char *dev_exts[16];
    uint32_t n = 0;
    dev_exts[n++] = VK_KHR_SWAPCHAIN_EXTENSION_NAME;

    // Full prerequisite chain the Vulkan spec requires for
    // VK_ANDROID_external_memory_android_hardware_buffer. If the instance
    // didn't get its two prerequisites (vk_create_instance) or the device is
    // missing any single one of these, zero-copy AHardwareBuffer import is
    // simply unavailable here -- g_hw_import_supported stays 0 and
    // android_vk_try_submit falls back to the CPU staging-buffer path that
    // already worked before this, on every device.
    static const char *hwbuf_exts[] = {
        VK_KHR_MAINTENANCE1_EXTENSION_NAME,
        VK_KHR_BIND_MEMORY_2_EXTENSION_NAME,
        VK_KHR_GET_MEMORY_REQUIREMENTS_2_EXTENSION_NAME,
        VK_KHR_SAMPLER_YCBCR_CONVERSION_EXTENSION_NAME,
        VK_KHR_EXTERNAL_MEMORY_EXTENSION_NAME,
        VK_KHR_DEDICATED_ALLOCATION_EXTENSION_NAME,
        VK_EXT_QUEUE_FAMILY_FOREIGN_EXTENSION_NAME,
        VK_ANDROID_EXTERNAL_MEMORY_ANDROID_HARDWARE_BUFFER_EXTENSION_NAME,
    };
    int hwbuf_ok = g_inst_has_ext_mem_caps && avail;
    if (hwbuf_ok) {
        for (size_t i = 0; i < sizeof(hwbuf_exts) / sizeof(hwbuf_exts[0]); i++) {
            if (!has_ext(avail, availN, hwbuf_exts[i])) { hwbuf_ok = 0; break; }
        }
    }
    if (hwbuf_ok) {
        for (size_t i = 0; i < sizeof(hwbuf_exts) / sizeof(hwbuf_exts[0]); i++) {
            dev_exts[n++] = hwbuf_exts[i];
        }
    }
    free(avail);

    VkPhysicalDeviceSamplerYcbcrConversionFeatures ycf = {
        VK_STRUCTURE_TYPE_PHYSICAL_DEVICE_SAMPLER_YCBCR_CONVERSION_FEATURES };
    if (hwbuf_ok) {
        PFN_vkGetPhysicalDeviceFeatures2KHR gpf2 = (PFN_vkGetPhysicalDeviceFeatures2KHR)
            vkGetInstanceProcAddr(g_inst, "vkGetPhysicalDeviceFeatures2KHR");
        if (gpf2) {
            VkPhysicalDeviceFeatures2 f2 = { VK_STRUCTURE_TYPE_PHYSICAL_DEVICE_FEATURES_2, &ycf };
            gpf2(g_pdev, &f2);
        }
    }
    int ycbcr_ok = hwbuf_ok && ycf.samplerYcbcrConversion;
    ycf.pNext = NULL;

    VkDeviceCreateInfo dci = { VK_STRUCTURE_TYPE_DEVICE_CREATE_INFO };
    dci.pNext                   = ycbcr_ok ? &ycf : NULL;
    dci.queueCreateInfoCount    = 1;
    dci.pQueueCreateInfos       = &qci;
    dci.enabledExtensionCount   = n;
    dci.ppEnabledExtensionNames = dev_exts;
    if (vkCreateDevice(g_pdev, &dci, NULL, &g_dev) != VK_SUCCESS) {
        if (hwbuf_ok) {
            // Retry with just the required extension in case one of the
            // "optional" ones we detected as available still fails device
            // creation for some other reason -- never let this new code
            // path stop the swapchain (and therefore all video) from
            // coming up at all.
            VLOGE("vkCreateDevice with hwbuffer extensions failed, retrying without them");
            dci.enabledExtensionCount = 1;
            dci.pNext = NULL;
            hwbuf_ok = 0;
            ycbcr_ok = 0;
            if (vkCreateDevice(g_pdev, &dci, NULL, &g_dev) != VK_SUCCESS) return 0;
        } else {
            return 0;
        }
    }
    vkGetDeviceQueue(g_dev, g_qfam, 0, &g_queue);

    if (hwbuf_ok) {
        p_vkGetAndroidHardwareBufferPropertiesANDROID =
            (PFN_vkGetAndroidHardwareBufferPropertiesANDROID)
            vkGetDeviceProcAddr(g_dev, "vkGetAndroidHardwareBufferPropertiesANDROID");
        g_hw_import_supported = p_vkGetAndroidHardwareBufferPropertiesANDROID != NULL;
        VLOGI("AHardwareBuffer zero-copy import: %s", g_hw_import_supported ? "available" : "unavailable (proc addr)");
        if (ycbcr_ok) {
            p_vkCreateSamplerYcbcrConversionKHR = (PFN_vkCreateSamplerYcbcrConversionKHR)
                vkGetDeviceProcAddr(g_dev, "vkCreateSamplerYcbcrConversionKHR");
            p_vkDestroySamplerYcbcrConversionKHR = (PFN_vkDestroySamplerYcbcrConversionKHR)
                vkGetDeviceProcAddr(g_dev, "vkDestroySamplerYcbcrConversionKHR");
        }
        g_ycbcr_supported = g_hw_import_supported && ycbcr_ok &&
            p_vkCreateSamplerYcbcrConversionKHR && p_vkDestroySamplerYcbcrConversionKHR;
        VLOGI("decoder-image YCbCr sampling: %s", g_ycbcr_supported ? "available" : "unavailable");
    } else {
        VLOGI("AHardwareBuffer zero-copy import: unavailable (missing extensions)");
    }
    return 1;
}

static int vk_create_swapchain(int w, int h) {
    VkSurfaceCapabilitiesKHR caps;
    vkGetPhysicalDeviceSurfaceCapabilitiesKHR(g_pdev, g_surf, &caps);

    uint32_t nfmt = 0;
    vkGetPhysicalDeviceSurfaceFormatsKHR(g_pdev, g_surf, &nfmt, NULL);
    VkSurfaceFormatKHR *fmts = malloc(nfmt * sizeof(*fmts));
    vkGetPhysicalDeviceSurfaceFormatsKHR(g_pdev, g_surf, &nfmt, fmts);
    g_swap_fmt = fmts[0].format;
    VkColorSpaceKHR csp = fmts[0].colorSpace;
    for (uint32_t i = 0; i < nfmt; i++) {
        if (fmts[i].format == VK_FORMAT_R8G8B8A8_UNORM ||
            fmts[i].format == VK_FORMAT_B8G8R8A8_UNORM) {
            g_swap_fmt = fmts[i].format; csp = fmts[i].colorSpace; break;
        }
    }
    free(fmts);

    // Android: prefer MAILBOX (low-latency, no tearing); fallback FIFO.
    uint32_t npm = 0;
    vkGetPhysicalDeviceSurfacePresentModesKHR(g_pdev, g_surf, &npm, NULL);
    VkPresentModeKHR *pms = malloc(npm * sizeof(*pms));
    vkGetPhysicalDeviceSurfacePresentModesKHR(g_pdev, g_surf, &npm, pms);
    VkPresentModeKHR pm = VK_PRESENT_MODE_FIFO_KHR;
    for (uint32_t i = 0; i < npm; i++)
        if (pms[i] == VK_PRESENT_MODE_MAILBOX_KHR) { pm = pms[i]; break; }
    free(pms);

    g_swap_ext = vk_surface_extent(&caps);
    // Explicit override (only when caller passes non-zero w/h).
    if (w > 0) g_swap_ext.width  = (uint32_t)w;
    if (h > 0) g_swap_ext.height = (uint32_t)h;
    if (g_swap_ext.width  == 0) g_swap_ext.width  = 1;
    if (g_swap_ext.height == 0) g_swap_ext.height = 1;
    // Clamp to surface capability limits.
    if (g_swap_ext.width  > caps.maxImageExtent.width)  g_swap_ext.width  = caps.maxImageExtent.width;
    if (g_swap_ext.height > caps.maxImageExtent.height) g_swap_ext.height = caps.maxImageExtent.height;
    if (g_swap_ext.width  < caps.minImageExtent.width)  g_swap_ext.width  = caps.minImageExtent.width;
    if (g_swap_ext.height < caps.minImageExtent.height) g_swap_ext.height = caps.minImageExtent.height;

    uint32_t imgCount = caps.minImageCount + 1;
    if (caps.maxImageCount && imgCount > caps.maxImageCount) imgCount = caps.maxImageCount;

    VkSwapchainCreateInfoKHR sci = { VK_STRUCTURE_TYPE_SWAPCHAIN_CREATE_INFO_KHR };
    sci.surface          = g_surf;
    sci.minImageCount    = imgCount;
    sci.imageFormat      = g_swap_fmt;
    sci.imageColorSpace  = csp;
    sci.imageExtent      = g_swap_ext;
    sci.imageArrayLayers = 1;
    sci.imageUsage       = VK_IMAGE_USAGE_COLOR_ATTACHMENT_BIT | VK_IMAGE_USAGE_TRANSFER_DST_BIT;
    sci.imageSharingMode = VK_SHARING_MODE_EXCLUSIVE;
    // Always use IDENTITY: we do NOT pre-rotate video pixels, so we let
    // SurfaceFlinger (the compositor) apply the device rotation itself.
    // Using caps.currentTransform here would tell SurfaceFlinger "already
    // rotated, don't touch it" — but we haven't rotated, so the video would
    // appear as a vertical column in landscape mode.
    sci.preTransform     = VK_SURFACE_TRANSFORM_IDENTITY_BIT_KHR;
    // INHERIT: the SurfaceView's TRANSLUCENT format decides, so menu holes
    // (alpha 0, see vk_cmd_punch_holes) show the Fyne window underneath.
    sci.compositeAlpha   = (caps.supportedCompositeAlpha & VK_COMPOSITE_ALPHA_INHERIT_BIT_KHR)
                               ? VK_COMPOSITE_ALPHA_INHERIT_BIT_KHR
                               : VK_COMPOSITE_ALPHA_OPAQUE_BIT_KHR;
    sci.presentMode      = pm;
    sci.clipped          = VK_TRUE;

    VLOGI("swapchain %dx%d %s (%d images)",
          (int)g_swap_ext.width, (int)g_swap_ext.height,
          (pm == VK_PRESENT_MODE_MAILBOX_KHR) ? "MAILBOX" : "FIFO",
          (int)imgCount);

    if (vkCreateSwapchainKHR(g_dev, &sci, NULL, &g_swap) != VK_SUCCESS) return 0;

    vkGetSwapchainImagesKHR(g_dev, g_swap, &g_swap_count, NULL);
    g_swap_imgs  = malloc(g_swap_count * sizeof(VkImage));
    g_swap_views = malloc(g_swap_count * sizeof(VkImageView));
    vkGetSwapchainImagesKHR(g_dev, g_swap, &g_swap_count, g_swap_imgs);
    for (uint32_t i = 0; i < g_swap_count; i++) {
        VkImageViewCreateInfo vci = { VK_STRUCTURE_TYPE_IMAGE_VIEW_CREATE_INFO };
        vci.image    = g_swap_imgs[i];
        vci.viewType = VK_IMAGE_VIEW_TYPE_2D;
        vci.format   = g_swap_fmt;
        vci.subresourceRange.aspectMask = VK_IMAGE_ASPECT_COLOR_BIT;
        vci.subresourceRange.levelCount = 1;
        vci.subresourceRange.layerCount = 1;
        vkCreateImageView(g_dev, &vci, NULL, &g_swap_views[i]);
    }
    return 1;
}

static void vk_yc_destroy_framebuffers(void);

static void vk_hud_destroy_framebuffers(void);

static void vk_destroy_swapchain(void) {
    vk_yc_destroy_framebuffers();
    vk_hud_destroy_framebuffers();
    if (g_swap_views) {
        for (uint32_t i = 0; i < g_swap_count; i++)
            if (g_swap_views[i]) vkDestroyImageView(g_dev, g_swap_views[i], NULL);
        free(g_swap_views); g_swap_views = NULL;
    }
    if (g_swap_imgs) { free(g_swap_imgs); g_swap_imgs = NULL; }
    if (g_swap != VK_NULL_HANDLE) { vkDestroySwapchainKHR(g_dev, g_swap, NULL); g_swap = VK_NULL_HANDLE; }
    g_swap_count = 0;
}

static int vk_recreate_swapchain(void) {
    if (!g_dev) return 0;
    VLOGI("recreating swapchain and checking for new surface");
    vkDeviceWaitIdle(g_dev);

    // Check if JNI has a new surface for us (e.g. after a destroy/create cycle).
    ANativeWindow *win = java_get_pending_window();
    if (win) {
        VLOGI("found new surface, recreating VkSurfaceKHR");
        vk_destroy_swapchain();
        if (g_surf != VK_NULL_HANDLE) {
            vkDestroySurfaceKHR(g_inst, g_surf, NULL);
            g_surf = VK_NULL_HANDLE;
        }
        VkAndroidSurfaceCreateInfoKHR sci = { VK_STRUCTURE_TYPE_ANDROID_SURFACE_CREATE_INFO_KHR };
        sci.window = win;
        if (vkCreateAndroidSurfaceKHR(g_inst, &sci, NULL, &g_surf) != VK_SUCCESS) {
            VLOGE("failed to recreate VkSurfaceKHR");
            ANativeWindow_release(win); return 0;
        }
        ANativeWindow_release(win);
    }

    if (!g_surf) { VLOGE("no surface for swapchain recreation"); return 0; }

    vk_destroy_swapchain();
    return vk_create_swapchain(0, 0);
}

static int vk_ensure_tex(int w, int h) {
    if (g_tex != VK_NULL_HANDLE && g_tex_w == w && g_tex_h == h) return 1;
    if (g_tex != VK_NULL_HANDLE) {
        vkDeviceWaitIdle(g_dev);
        vkFreeMemory(g_dev, g_tex_mem, NULL); g_tex_mem = VK_NULL_HANDLE;
        vkDestroyImage(g_dev, g_tex, NULL);   g_tex     = VK_NULL_HANDLE;
    }
    VkImageCreateInfo ici = { VK_STRUCTURE_TYPE_IMAGE_CREATE_INFO };
    ici.imageType   = VK_IMAGE_TYPE_2D;
    ici.format      = VK_FORMAT_R8G8B8A8_UNORM;
    ici.extent      = (VkExtent3D){(uint32_t)w, (uint32_t)h, 1};
    ici.mipLevels   = 1;
    ici.arrayLayers = 1;
    ici.samples     = VK_SAMPLE_COUNT_1_BIT;
    ici.tiling      = VK_IMAGE_TILING_OPTIMAL;
    ici.usage       = VK_IMAGE_USAGE_TRANSFER_DST_BIT | VK_IMAGE_USAGE_TRANSFER_SRC_BIT;
    ici.initialLayout = VK_IMAGE_LAYOUT_UNDEFINED;
    if (vkCreateImage(g_dev, &ici, NULL, &g_tex) != VK_SUCCESS) return 0;
    VkMemoryRequirements mr;
    vkGetImageMemoryRequirements(g_dev, g_tex, &mr);
    VkPhysicalDeviceMemoryProperties mp;
    vkGetPhysicalDeviceMemoryProperties(g_pdev, &mp);
    uint32_t mi = vk_find_mem(&mp, mr.memoryTypeBits, VK_MEMORY_PROPERTY_DEVICE_LOCAL_BIT);
    if (mi == UINT32_MAX) return 0;
    VkMemoryAllocateInfo mai = { VK_STRUCTURE_TYPE_MEMORY_ALLOCATE_INFO };
    mai.allocationSize  = mr.size;
    mai.memoryTypeIndex = mi;
    if (vkAllocateMemory(g_dev, &mai, NULL, &g_tex_mem) != VK_SUCCESS) return 0;
    vkBindImageMemory(g_dev, g_tex, g_tex_mem, 0);
    g_tex_w = w; g_tex_h = h;
    return 1;
}

static int vk_ensure_staging(size_t sz) {
    if (g_stage_buf != VK_NULL_HANDLE && g_stage_sz >= sz) return 1;
    if (g_stage_buf != VK_NULL_HANDLE) {
        vkUnmapMemory(g_dev, g_stage_mem);
        vkFreeMemory(g_dev, g_stage_mem, NULL); g_stage_mem = VK_NULL_HANDLE;
        vkDestroyBuffer(g_dev, g_stage_buf, NULL); g_stage_buf = VK_NULL_HANDLE;
        g_stage_ptr = NULL; g_stage_sz = 0;
    }
    VkBufferCreateInfo bci = { VK_STRUCTURE_TYPE_BUFFER_CREATE_INFO };
    bci.size        = sz;
    bci.usage       = VK_BUFFER_USAGE_TRANSFER_SRC_BIT;
    bci.sharingMode = VK_SHARING_MODE_EXCLUSIVE;
    if (vkCreateBuffer(g_dev, &bci, NULL, &g_stage_buf) != VK_SUCCESS) return 0;
    VkMemoryRequirements mr;
    vkGetBufferMemoryRequirements(g_dev, g_stage_buf, &mr);
    VkPhysicalDeviceMemoryProperties mp;
    vkGetPhysicalDeviceMemoryProperties(g_pdev, &mp);
    uint32_t mi = vk_find_mem(&mp, mr.memoryTypeBits,
        VK_MEMORY_PROPERTY_HOST_VISIBLE_BIT | VK_MEMORY_PROPERTY_HOST_COHERENT_BIT);
    if (mi == UINT32_MAX) return 0;
    VkMemoryAllocateInfo mai = { VK_STRUCTURE_TYPE_MEMORY_ALLOCATE_INFO };
    mai.allocationSize  = mr.size;
    mai.memoryTypeIndex = mi;
    if (vkAllocateMemory(g_dev, &mai, NULL, &g_stage_mem) != VK_SUCCESS) return 0;
    vkBindBufferMemory(g_dev, g_stage_buf, g_stage_mem, 0);
    vkMapMemory(g_dev, g_stage_mem, 0, VK_WHOLE_SIZE, 0, &g_stage_ptr);
    g_stage_sz = sz;
    return 1;
}

// vk_apply_fit_pan pans the aspect-fit destination, then clips both dest and
// source so the blit stays inside the swapchain. This lets the fitted video
// slide partially off-screen (letterbox / free pan) without Vulkan rejecting
// out-of-bounds dest offsets — the old "keep full rect on screen" clamp was
// fighting Go's pan and snapping the picture back toward center.
static void vk_apply_fit_pan(int *dx, int *dy, int *dw, int *dh,
                             int *sx0, int *sy0, int *sx1, int *sy1,
                             int sw, int sh, int pan_x, int pan_y) {
    if (*dw <= 0 || *dh <= 0) {
        return;
    }
    int dest_x0 = *dx + pan_x;
    int dest_y0 = *dy + pan_y;
    int dest_x1 = dest_x0 + *dw;
    int dest_y1 = dest_y0 + *dh;

    int clip_x0 = dest_x0 < 0 ? 0 : dest_x0;
    int clip_y0 = dest_y0 < 0 ? 0 : dest_y0;
    int clip_x1 = dest_x1 > sw ? sw : dest_x1;
    int clip_y1 = dest_y1 > sh ? sh : dest_y1;
    if (clip_x1 <= clip_x0 || clip_y1 <= clip_y0) {
        *dw = 0;
        *dh = 0;
        return;
    }

    int src0x = *sx0, src0y = *sy0, src1x = *sx1, src1y = *sy1;
    float src_w = (float)(src1x - src0x);
    float src_h = (float)(src1y - src0y);
    float ndw = (float)(*dw);
    float ndh = (float)(*dh);

    float nx0 = (clip_x0 - dest_x0) / ndw;
    float ny0 = (clip_y0 - dest_y0) / ndh;
    float nx1 = (clip_x1 - dest_x0) / ndw;
    float ny1 = (clip_y1 - dest_y0) / ndh;

    *sx0 = src0x + (int)(src_w * nx0 + 0.5f);
    *sy0 = src0y + (int)(src_h * ny0 + 0.5f);
    *sx1 = src0x + (int)(src_w * nx1 + 0.5f);
    *sy1 = src0y + (int)(src_h * ny1 + 0.5f);
    *dx = clip_x0;
    *dy = clip_y0;
    *dw = clip_x1 - clip_x0;
    *dh = clip_y1 - clip_y0;
}

// vk_layout_zoomed_dest aspect-fits the FULL frame into the swapchain, scales
// that dest by zoom (pinch), applies pan, then clips. Source stays the full
// frame UV; clipping dest proportionally crops src — uniform digital zoom
// without aspect distortion.
static void vk_layout_zoomed_dest(int fw, int fh, int sw, int sh,
                                  float zoom, int pan_x, int pan_y,
                                  int *dx, int *dy, int *dw, int *dh,
                                  int *sx0, int *sy0, int *sx1, int *sy1) {
    float fa = display_aspect(fw, fh);
    float wa = (float)sw / (float)(sh > 0 ? sh : 1);
    *dx = 0;
    *dy = 0;
    *dw = sw;
    *dh = sh;
    if (fa > wa) {
        *dh = (int)(sw / fa + 0.5f);
        switch (atomic_load(&g_v_align)) {
            case 1: *dy = sh - *dh; break; // bottom
            case 2: *dy = 0; break;         // top
            default: *dy = (sh - *dh) / 2; break; // center
        }
    } else {
        *dw = (int)(sh * fa + 0.5f);
        *dx = (sw - *dw) / 2;
    }
    if (zoom < 1.0f) {
        zoom = 1.0f;
    }
    if (zoom > 1.001f) {
        int cx = *dx + *dw / 2;
        int cy = *dy + *dh / 2;
        *dw = (int)(*dw * zoom + 0.5f);
        *dh = (int)(*dh * zoom + 0.5f);
        *dx = cx - *dw / 2;
        *dy = cy - *dh / 2;
    }
    // Full-frame source before clip; pan/zoom clip adjusts src with dest.
    *sx0 = 0;
    *sy0 = 0;
    *sx1 = fw;
    *sy1 = fh;
    vk_apply_fit_pan(dx, dy, dw, dh, sx0, sy0, sx1, sy1, sw, sh, pan_x, pan_y);
}

static void vk_image_barrier(VkCommandBuffer cb, VkImage img,
                              VkImageLayout old_l, VkImageLayout new_l,
                              VkAccessFlags src_acc, VkAccessFlags dst_acc,
                              VkPipelineStageFlags src_st, VkPipelineStageFlags dst_st) {
    VkImageMemoryBarrier b = { VK_STRUCTURE_TYPE_IMAGE_MEMORY_BARRIER };
    b.oldLayout = old_l; b.newLayout = new_l;
    b.srcQueueFamilyIndex = VK_QUEUE_FAMILY_IGNORED;
    b.dstQueueFamilyIndex = VK_QUEUE_FAMILY_IGNORED;
    b.image = img;
    b.subresourceRange.aspectMask = VK_IMAGE_ASPECT_COLOR_BIT;
    b.subresourceRange.levelCount = 1;
    b.subresourceRange.layerCount = 1;
    b.srcAccessMask = src_acc;
    b.dstAccessMask = dst_acc;
    vkCmdPipelineBarrier(cb, src_st, dst_st, 0, 0, NULL, 0, NULL, 1, &b);
}

// ─── Net Graph HUD ────────────────────────────────────────────────────────────
//
// Same native layer as Linux (vk_overlay_common.h): net_graph.go pushes its
// 640x400 canvas ~10 times a second and the render thread draws it over the
// frame, so the decoder image never has to come back to the CPU for a burn-in.
// Shared by both backends: Vulkan draws it in its own small render pass (no
// dynamic rendering on most Android drivers), GLES as one more blended quad.
// Drawn before the menu holes so a menu still cuts through it.

static VkRenderPass   g_hud_rp       = VK_NULL_HANDLE;
static VkFormat       g_hud_rp_fmt   = VK_FORMAT_UNDEFINED;
static VkFramebuffer *g_hud_fbs      = NULL;
static uint32_t       g_hud_fb_count = 0;

static VkShaderModule vk_shader_from_spv(const uint32_t *code, size_t size) {
    VkShaderModuleCreateInfo ci = { VK_STRUCTURE_TYPE_SHADER_MODULE_CREATE_INFO };
    ci.codeSize = size;
    ci.pCode    = code;
    VkShaderModule m = VK_NULL_HANDLE;
    if (vkCreateShaderModule(g_dev, &ci, NULL, &m) != VK_SUCCESS) return VK_NULL_HANDLE;
    return m;
}

#define goVKLog(msg, lvl) do { if ((lvl) >= 2) VLOGE("%s", msg); else VLOGI("%s", msg); } while (0)
#define VKOV_RENDER_PASS g_hud_rp
#define VKOV_MUTEX_DECL static pthread_mutex_t g_hud_mu = PTHREAD_MUTEX_INITIALIZER;
#define VKOV_LOCK()     pthread_mutex_lock(&g_hud_mu)
#define VKOV_UNLOCK()   pthread_mutex_unlock(&g_hud_mu)
#include "vk_overlay_common.h"

// hud_place: where the HUD goes on the surface (top-left origin, pixels):
// the bottom-right corner of the visible picture, sized like the old
// burn-in at the unzoomed fit scale so zooming the video doesn't zoom it.
static int hud_place(int fw, int fh, int dx, int dy, int dw, int dh, int sw, int sh, int out[4]) {
    if (fw <= 0 || fh <= 0 || sw <= 0 || sh <= 0) return 0;
    int vx0 = dx < 0 ? 0 : dx, vy0 = dy < 0 ? 0 : dy;
    int vx1 = dx + dw > sw ? sw : dx + dw, vy1 = dy + dh > sh ? sh : dy + dh;
    if (vx1 <= vx0 || vy1 <= vy0) return 0;
    float s = (float)sw / (float)fw;
    if ((float)sh / (float)fh < s) s = (float)sh / (float)fh;
    float k = s * (float)atomic_load(&g_hud_scale_pct) / 100.0f;
    int w = (int)(VK_HUD_W * k + 0.5f), h = (int)(VK_HUD_H * k + 0.5f);
    int m = (int)(VK_HUD_MARGIN * s + 0.5f);
    int x = vx1 - m - w, y = vy1 - m - h;
    if (x < vx0 + m) x = vx0 + m;
    if (y < vy0 + m) y = vy0 + m;
    if (w <= 0 || h <= 0) return 0;
    out[0] = x; out[1] = y; out[2] = w; out[3] = h;
    return 1;
}

static void vk_hud_destroy_framebuffers(void) {
    if (g_hud_fbs) {
        for (uint32_t i = 0; i < g_hud_fb_count; i++)
            if (g_hud_fbs[i]) vkDestroyFramebuffer(g_dev, g_hud_fbs[i], NULL);
        free(g_hud_fbs);
    }
    g_hud_fbs = NULL;
    g_hud_fb_count = 0;
}

// vk_hud_destroy_all: device teardown (the GPU is idle).
static void vk_hud_destroy_all(void) {
    vk_hud_destroy_framebuffers();
    vk_hud_destroy();
    if (g_hud_rp) { vkDestroyRenderPass(g_dev, g_hud_rp, NULL); g_hud_rp = VK_NULL_HANDLE; }
    g_hud_rp_fmt = VK_FORMAT_UNDEFINED;
}

// vk_hud_ensure_rp: a pass that loads the finished frame (cursor included)
// and leaves it in TRANSFER_DST for the holes blit that follows.
static int vk_hud_ensure_rp(void) {
    if (g_hud_rp && g_hud_rp_fmt == g_swap_fmt) return 1;
    if (g_hud_rp) {
        vkDeviceWaitIdle(g_dev);
        vk_hud_destroy_all();
    }
    VkAttachmentDescription att = {0};
    att.format         = g_swap_fmt;
    att.samples        = VK_SAMPLE_COUNT_1_BIT;
    att.loadOp         = VK_ATTACHMENT_LOAD_OP_LOAD;
    att.storeOp        = VK_ATTACHMENT_STORE_OP_STORE;
    att.stencilLoadOp  = VK_ATTACHMENT_LOAD_OP_DONT_CARE;
    att.stencilStoreOp = VK_ATTACHMENT_STORE_OP_DONT_CARE;
    att.initialLayout  = VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL;
    att.finalLayout    = VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL;
    VkAttachmentReference ref = { 0, VK_IMAGE_LAYOUT_COLOR_ATTACHMENT_OPTIMAL };
    VkSubpassDescription sub = {0};
    sub.pipelineBindPoint    = VK_PIPELINE_BIND_POINT_GRAPHICS;
    sub.colorAttachmentCount = 1;
    sub.pColorAttachments    = &ref;
    VkSubpassDependency deps[2] = {0};
    deps[0].srcSubpass    = VK_SUBPASS_EXTERNAL;
    deps[0].dstSubpass    = 0;
    deps[0].srcStageMask  = VK_PIPELINE_STAGE_TRANSFER_BIT | VK_PIPELINE_STAGE_COLOR_ATTACHMENT_OUTPUT_BIT;
    deps[0].srcAccessMask = VK_ACCESS_TRANSFER_WRITE_BIT | VK_ACCESS_COLOR_ATTACHMENT_WRITE_BIT;
    deps[0].dstStageMask  = VK_PIPELINE_STAGE_COLOR_ATTACHMENT_OUTPUT_BIT;
    deps[0].dstAccessMask = VK_ACCESS_COLOR_ATTACHMENT_READ_BIT | VK_ACCESS_COLOR_ATTACHMENT_WRITE_BIT;
    deps[1].srcSubpass    = 0;
    deps[1].dstSubpass    = VK_SUBPASS_EXTERNAL;
    deps[1].srcStageMask  = VK_PIPELINE_STAGE_COLOR_ATTACHMENT_OUTPUT_BIT;
    deps[1].srcAccessMask = VK_ACCESS_COLOR_ATTACHMENT_WRITE_BIT;
    deps[1].dstStageMask  = VK_PIPELINE_STAGE_TRANSFER_BIT;
    deps[1].dstAccessMask = VK_ACCESS_TRANSFER_WRITE_BIT | VK_ACCESS_TRANSFER_READ_BIT;
    VkRenderPassCreateInfo rpci = { VK_STRUCTURE_TYPE_RENDER_PASS_CREATE_INFO };
    rpci.attachmentCount = 1; rpci.pAttachments  = &att;
    rpci.subpassCount    = 1; rpci.pSubpasses    = &sub;
    rpci.dependencyCount = 2; rpci.pDependencies = deps;
    if (vkCreateRenderPass(g_dev, &rpci, NULL, &g_hud_rp) != VK_SUCCESS) {
        g_hud_rp = VK_NULL_HANDLE;
        return 0;
    }
    g_hud_rp_fmt = g_swap_fmt;
    return 1;
}

static int vk_hud_ensure_framebuffers(void) {
    if (g_hud_fbs && g_hud_fb_count == g_swap_count) return 1;
    vk_hud_destroy_framebuffers();
    g_hud_fbs = calloc(g_swap_count, sizeof(VkFramebuffer));
    if (!g_hud_fbs) return 0;
    g_hud_fb_count = g_swap_count;
    for (uint32_t i = 0; i < g_swap_count; i++) {
        VkFramebufferCreateInfo fci = { VK_STRUCTURE_TYPE_FRAMEBUFFER_CREATE_INFO };
        fci.renderPass      = g_hud_rp;
        fci.attachmentCount = 1;
        fci.pAttachments    = &g_swap_views[i];
        fci.width           = g_swap_ext.width;
        fci.height          = g_swap_ext.height;
        fci.layers          = 1;
        if (vkCreateFramebuffer(g_dev, &fci, NULL, &g_hud_fbs[i]) != VK_SUCCESS) {
            vk_hud_destroy_framebuffers();
            return 0;
        }
    }
    return 1;
}

// vk_cmd_draw_hud records the HUD over swapchain image img_idx (in
// TRANSFER_DST, left there). fw x fh is the frame, d* where it was drawn.
// Costs nothing while Net Graph is off.
static void vk_cmd_draw_hud(uint32_t img_idx, int fw, int fh, int dx, int dy, int dw, int dh) {
    VKOV_LOCK();
    int active = g_hud_active;
    VKOV_UNLOCK();
    if (!active) return;
    if (!vk_hud_ensure_rp()) return;
    if (!vk_hud_maybe_upload_cmds(g_cmdbuf)) return;
    if (!vk_hud_ensure_framebuffers()) return;
    int sw = (int)g_swap_ext.width, sh = (int)g_swap_ext.height;
    int r[4];
    if (!hud_place(fw, fh, dx, dy, dw, dh, sw, sh, r)) return;

    VkRenderPassBeginInfo rbi = { VK_STRUCTURE_TYPE_RENDER_PASS_BEGIN_INFO };
    rbi.renderPass  = g_hud_rp;
    rbi.framebuffer = g_hud_fbs[img_idx];
    rbi.renderArea.extent = g_swap_ext;
    vkCmdBeginRenderPass(g_cmdbuf, &rbi, VK_SUBPASS_CONTENTS_INLINE);
    int x0 = r[0] < 0 ? 0 : r[0], y0 = r[1] < 0 ? 0 : r[1];
    int x1 = r[0] + r[2] > sw ? sw : r[0] + r[2], y1 = r[1] + r[3] > sh ? sh : r[1] + r[3];
    if (x1 > x0 && y1 > y0) {
        VkViewport vpt = { (float)r[0], (float)r[1], (float)r[2], (float)r[3], 0.0f, 1.0f };
        VkRect2D sc = { { x0, y0 }, { (uint32_t)(x1 - x0), (uint32_t)(y1 - y0) } };
        float rect[4] = { -1.0f, -1.0f, 1.0f, 1.0f };
        vkCmdBindPipeline(g_cmdbuf, VK_PIPELINE_BIND_POINT_GRAPHICS, g_hud_pipeline);
        vkCmdSetViewport(g_cmdbuf, 0, 1, &vpt);
        vkCmdSetScissor(g_cmdbuf, 0, 1, &sc);
        vkCmdBindDescriptorSets(g_cmdbuf, VK_PIPELINE_BIND_POINT_GRAPHICS, g_hud_playout, 0, 1, &g_hud_dset, 0, NULL);
        vkCmdPushConstants(g_cmdbuf, g_hud_playout, VK_SHADER_STAGE_VERTEX_BIT, 0, sizeof(rect), rect);
        vkCmdDraw(g_cmdbuf, 6, 1, 0, 0);
    }
    vkCmdEndRenderPass(g_cmdbuf);
}

// hud_gles_sync hands the GLES backend a newer canvas, if any. Render
// thread, current context. Returns whether to draw the HUD.
static int hud_gles_sync(void) {
    int active;
    VKOV_LOCK();
    active = g_hud_active;
    if (active && (g_hud_dirty || !gles_ov_hud_ready())) {
        gles_ov_set_hud(g_hud_pixels, VK_HUD_W, VK_HUD_H);
        g_hud_dirty = 0;
    }
    VKOV_UNLOCK();
    return active && gles_ov_hud_ready();
}

// ─── menu holes ───────────────────────────────────────────────────────────────
//
// The SurfaceView sits above the Fyne window (setZOrderOnTop), so a Fyne menu
// or popup opened over the video would be covered by it. Instead of hiding the
// video, every presented frame gets the open menus' rects punched to alpha 0;
// the surface is TRANSLUCENT (VulkanOverlayBridge), so SurfaceFlinger shows
// the Fyne window through them while the rest of the video keeps playing.
// Everything else stays alpha 1 (video, letterbox clear, cursor).
//
// g_holes is in swapchain pixels, written by android_vk_set_holes under
// g_state_mu; a rounded card arrives as several plain rects (Go splits it).
#define VK_MAX_HOLES 512
static VkRect2D g_holes[VK_MAX_HOLES];
static int      g_hole_count = 0;

// 1x1 transparent-black image that vkCmdBlitImage stretches over each hole.
// Render thread only.
static VkImage        g_clear_img   = VK_NULL_HANDLE;
static VkDeviceMemory g_clear_mem   = VK_NULL_HANDLE;
static int            g_clear_ready = 0; // cleared and in TRANSFER_SRC_OPTIMAL
static VkImageBlit    g_hole_blits[VK_MAX_HOLES];

static int vk_ensure_clear_img(void) {
    if (g_clear_img != VK_NULL_HANDLE) return 1;
    VkImageCreateInfo ici = { VK_STRUCTURE_TYPE_IMAGE_CREATE_INFO };
    ici.imageType     = VK_IMAGE_TYPE_2D;
    ici.format        = VK_FORMAT_R8G8B8A8_UNORM;
    ici.extent        = (VkExtent3D){1, 1, 1};
    ici.mipLevels     = 1;
    ici.arrayLayers   = 1;
    ici.samples       = VK_SAMPLE_COUNT_1_BIT;
    ici.tiling        = VK_IMAGE_TILING_OPTIMAL;
    ici.usage         = VK_IMAGE_USAGE_TRANSFER_SRC_BIT | VK_IMAGE_USAGE_TRANSFER_DST_BIT;
    ici.sharingMode   = VK_SHARING_MODE_EXCLUSIVE;
    ici.initialLayout = VK_IMAGE_LAYOUT_UNDEFINED;
    if (vkCreateImage(g_dev, &ici, NULL, &g_clear_img) != VK_SUCCESS) {
        g_clear_img = VK_NULL_HANDLE;
        return 0;
    }
    VkMemoryRequirements mr;
    vkGetImageMemoryRequirements(g_dev, g_clear_img, &mr);
    VkPhysicalDeviceMemoryProperties mp;
    vkGetPhysicalDeviceMemoryProperties(g_pdev, &mp);
    uint32_t mi = vk_find_mem(&mp, mr.memoryTypeBits, VK_MEMORY_PROPERTY_DEVICE_LOCAL_BIT);
    if (mi == UINT32_MAX) mi = vk_find_mem(&mp, mr.memoryTypeBits, 0);
    VkMemoryAllocateInfo mai = { VK_STRUCTURE_TYPE_MEMORY_ALLOCATE_INFO };
    mai.allocationSize  = mr.size;
    mai.memoryTypeIndex = mi;
    if (mi == UINT32_MAX || vkAllocateMemory(g_dev, &mai, NULL, &g_clear_mem) != VK_SUCCESS) {
        vkDestroyImage(g_dev, g_clear_img, NULL);
        g_clear_img = VK_NULL_HANDLE;
        g_clear_mem = VK_NULL_HANDLE;
        return 0;
    }
    vkBindImageMemory(g_dev, g_clear_img, g_clear_mem, 0);
    g_clear_ready = 0;
    return 1;
}

static void vk_destroy_clear_img(void) {
    if (g_clear_img) { vkDestroyImage(g_dev, g_clear_img, NULL); g_clear_img = VK_NULL_HANDLE; }
    if (g_clear_mem) { vkFreeMemory(g_dev, g_clear_mem, NULL);   g_clear_mem = VK_NULL_HANDLE; }
    g_clear_ready = 0;
}

// vk_cmd_punch_holes records the hole blits into g_cmdbuf over dst (in
// TRANSFER_DST_OPTIMAL, after the video and cursor writes). No-op without holes.
static void vk_cmd_punch_holes(VkImage dst) {
    int sw = (int)g_swap_ext.width, sh = (int)g_swap_ext.height;
    int n = 0;
    pthread_mutex_lock(&g_state_mu);
    for (int i = 0; i < g_hole_count; i++) {
        int x0 = g_holes[i].offset.x, y0 = g_holes[i].offset.y;
        int x1 = x0 + (int)g_holes[i].extent.width, y1 = y0 + (int)g_holes[i].extent.height;
        if (x0 < 0) x0 = 0;
        if (y0 < 0) y0 = 0;
        if (x1 > sw) x1 = sw;
        if (y1 > sh) y1 = sh;
        if (x1 <= x0 || y1 <= y0) continue;
        VkImageBlit *b = &g_hole_blits[n++];
        memset(b, 0, sizeof(*b));
        b->srcSubresource.aspectMask = VK_IMAGE_ASPECT_COLOR_BIT;
        b->srcSubresource.layerCount = 1;
        b->srcOffsets[1] = (VkOffset3D){1, 1, 1};
        b->dstSubresource.aspectMask = VK_IMAGE_ASPECT_COLOR_BIT;
        b->dstSubresource.layerCount = 1;
        b->dstOffsets[0] = (VkOffset3D){x0, y0, 0};
        b->dstOffsets[1] = (VkOffset3D){x1, y1, 1};
    }
    pthread_mutex_unlock(&g_state_mu);
    if (n == 0 || !vk_ensure_clear_img()) return;

    if (!g_clear_ready) {
        vk_image_barrier(g_cmdbuf, g_clear_img,
            VK_IMAGE_LAYOUT_UNDEFINED, VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL,
            0, VK_ACCESS_TRANSFER_WRITE_BIT,
            VK_PIPELINE_STAGE_TOP_OF_PIPE_BIT, VK_PIPELINE_STAGE_TRANSFER_BIT);
        VkClearColorValue clear = {{0.0f, 0.0f, 0.0f, 0.0f}};
        VkImageSubresourceRange full = { VK_IMAGE_ASPECT_COLOR_BIT, 0, 1, 0, 1 };
        vkCmdClearColorImage(g_cmdbuf, g_clear_img,
                             VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL, &clear, 1, &full);
        vk_image_barrier(g_cmdbuf, g_clear_img,
            VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL, VK_IMAGE_LAYOUT_TRANSFER_SRC_OPTIMAL,
            VK_ACCESS_TRANSFER_WRITE_BIT, VK_ACCESS_TRANSFER_READ_BIT,
            VK_PIPELINE_STAGE_TRANSFER_BIT, VK_PIPELINE_STAGE_TRANSFER_BIT);
        g_clear_ready = 1;
    }

    // Serialise the video/cursor writes before the hole writes.
    VkMemoryBarrier mb = { VK_STRUCTURE_TYPE_MEMORY_BARRIER };
    mb.srcAccessMask = VK_ACCESS_TRANSFER_WRITE_BIT;
    mb.dstAccessMask = VK_ACCESS_TRANSFER_WRITE_BIT;
    vkCmdPipelineBarrier(g_cmdbuf,
        VK_PIPELINE_STAGE_TRANSFER_BIT, VK_PIPELINE_STAGE_TRANSFER_BIT,
        0, 1, &mb, 0, NULL, 0, NULL);

    vkCmdBlitImage(g_cmdbuf,
        g_clear_img, VK_IMAGE_LAYOUT_TRANSFER_SRC_OPTIMAL,
        dst,         VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL,
        (uint32_t)n, g_hole_blits, VK_FILTER_NEAREST);
}

// ─── render one frame ─────────────────────────────────────────────────────────

// vk_render_frame presents the frame whose pixels are already sitting in
// g_stage_ptr (written directly by the render thread before calling this —
// see vk_render_thread) at dimensions fw x fh, row stride fs bytes.
static int vk_render_frame(int fw, int fh, int fs) {
    if (!g_dev) return 0;
    // Retry swapchain creation if a previous attempt failed (g_swap may be NULL
    // after a failed vk_recreate_swapchain call or after explicit force-recreate).
    if (!g_swap) {
        vk_recreate_swapchain();
        return 0;
    }

    // Proactively detect surface resize: query caps and compare to our swapchain.
    // On Android, Vulkan may not always signal VK_SUBOPTIMAL/OUT_OF_DATE when the
    // surface changes (driver-dependent), so we check explicitly each frame.
    {
        VkSurfaceCapabilitiesKHR caps;
        if (vkGetPhysicalDeviceSurfaceCapabilitiesKHR(g_pdev, g_surf, &caps) == VK_SUCCESS) {
            VkExtent2D cur = vk_surface_extent(&caps);
            if (cur.width != 0 && cur.height != 0 &&
                (cur.width != g_swap_ext.width || cur.height != g_swap_ext.height)) {
                VLOGI("surface resized %ux%u → %ux%u, recreating swapchain",
                      g_swap_ext.width, g_swap_ext.height, cur.width, cur.height);
                vk_recreate_swapchain();
                return 0;
            }
        } else {
            VLOGI("failed to query surface caps, attempting to find new surface");
            vk_recreate_swapchain();
            return 0;
        }
    }

    size_t frame_sz = (size_t)fh * (size_t)fs;
    if (!vk_ensure_staging(frame_sz)) return 0;
    if (!vk_ensure_tex(fw, fh))       return 0;

    uint32_t img_idx = 0;
    VkResult res = vkAcquireNextImageKHR(g_dev, g_swap, 2000000000ULL,
                                          g_img_sem, VK_NULL_HANDLE, &img_idx);
    if (res == VK_ERROR_OUT_OF_DATE_KHR) {
        vk_recreate_swapchain(); return 0;
    }
    if (res != VK_SUCCESS && res != VK_SUBOPTIMAL_KHR) return 0;

    if (vkWaitForFences(g_dev, 1, &g_fence, VK_TRUE, 2000000000ULL) == VK_TIMEOUT) {
        VLOGE("vkWaitForFences timeout (2s)");
        vkResetFences(g_dev, 1, &g_fence); return 0;
    }
    vkResetFences(g_dev, 1, &g_fence);

    vkResetCommandBuffer(g_cmdbuf, 0);
    VkCommandBufferBeginInfo bi = { VK_STRUCTURE_TYPE_COMMAND_BUFFER_BEGIN_INFO };
    bi.flags = VK_COMMAND_BUFFER_USAGE_ONE_TIME_SUBMIT_BIT;
    vkBeginCommandBuffer(g_cmdbuf, &bi);

    vk_image_barrier(g_cmdbuf, g_tex,
        VK_IMAGE_LAYOUT_UNDEFINED, VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL,
        0, VK_ACCESS_TRANSFER_WRITE_BIT,
        VK_PIPELINE_STAGE_TOP_OF_PIPE_BIT, VK_PIPELINE_STAGE_TRANSFER_BIT);

    VkBufferImageCopy bic = {0};
    bic.bufferRowLength         = (uint32_t)fw;
    bic.bufferImageHeight       = (uint32_t)fh;
    bic.imageSubresource.aspectMask = VK_IMAGE_ASPECT_COLOR_BIT;
    bic.imageSubresource.layerCount = 1;
    bic.imageExtent = (VkExtent3D){(uint32_t)fw, (uint32_t)fh, 1};
    vkCmdCopyBufferToImage(g_cmdbuf, g_stage_buf, g_tex,
                           VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL, 1, &bic);

    vk_image_barrier(g_cmdbuf, g_tex,
        VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL, VK_IMAGE_LAYOUT_TRANSFER_SRC_OPTIMAL,
        VK_ACCESS_TRANSFER_WRITE_BIT, VK_ACCESS_TRANSFER_READ_BIT,
        VK_PIPELINE_STAGE_TRANSFER_BIT, VK_PIPELINE_STAGE_TRANSFER_BIT);

    vk_image_barrier(g_cmdbuf, g_swap_imgs[img_idx],
        VK_IMAGE_LAYOUT_UNDEFINED, VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL,
        0, VK_ACCESS_TRANSFER_WRITE_BIT,
        VK_PIPELINE_STAGE_TOP_OF_PIPE_BIT, VK_PIPELINE_STAGE_TRANSFER_BIT);

    // Snapshot viewport + cursor state atomically so the render always uses a
    // consistent pair: viewport from call N with cursor from call N (not N+1).
    pthread_mutex_lock(&g_state_mu);
    float u0 = atomic_load(&g_vp_u0_fp) / 65536.0f;
    float v0 = atomic_load(&g_vp_v0_fp) / 65536.0f;
    float u1 = atomic_load(&g_vp_u1_fp) / 65536.0f;
    float v1 = atomic_load(&g_vp_v1_fp) / 65536.0f;
    float snap_uc  = atomic_load(&g_cursor_uc_fp)  / 65536.0f;
    float snap_vc  = atomic_load(&g_cursor_vc_fp)  / 65536.0f;
    int   snap_vis = atomic_load(&g_cursor_visible);
    int   snap_pan_x = atomic_load(&g_blit_pan_x);
    int   snap_pan_y = atomic_load(&g_blit_pan_y);
    float snap_zoom  = atomic_load(&g_blit_zoom_fp) / 65536.0f;
    pthread_mutex_unlock(&g_state_mu);
    if (u0 < 0.0f) u0 = 0.0f; if (u1 > 1.0f) u1 = 1.0f;
    if (v0 < 0.0f) v0 = 0.0f; if (v1 > 1.0f) v1 = 1.0f;
    if (u1 <= u0 + 0.001f) { u0 = 0.0f; u1 = 1.0f; }
    if (v1 <= v0 + 0.001f) { v0 = 0.0f; v1 = 1.0f; }
    (void)u0; (void)v0; (void)u1; (void)v1;

    int src_x0 = 0, src_y0 = 0, src_x1 = fw, src_y1 = fh;
    int sw = (int)g_swap_ext.width, sh = (int)g_swap_ext.height;
    int dx = 0, dy = 0, dw = sw, dh = sh;
    vk_layout_zoomed_dest(fw, fh, sw, sh, snap_zoom, snap_pan_x, snap_pan_y,
                          &dx, &dy, &dw, &dh, &src_x0, &src_y0, &src_x1, &src_y1);
    vk_store_video_dest(dx, dy, dw, dh, sw, sh);

    // Opaque black: the surface is translucent, alpha 0 is reserved for holes.
    VkClearColorValue black = {{0.0f, 0.0f, 0.0f, 1.0f}};
    VkImageSubresourceRange full = { VK_IMAGE_ASPECT_COLOR_BIT, 0, 1, 0, 1 };
    vkCmdClearColorImage(g_cmdbuf, g_swap_imgs[img_idx],
                         VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL, &black, 1, &full);

    // Sync clear before blit (Write-After-Write hazard in TRANSFER stage)
    vk_image_barrier(g_cmdbuf, g_swap_imgs[img_idx],
        VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL, VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL,
        VK_ACCESS_TRANSFER_WRITE_BIT, VK_ACCESS_TRANSFER_WRITE_BIT,
        VK_PIPELINE_STAGE_TRANSFER_BIT, VK_PIPELINE_STAGE_TRANSFER_BIT);

    if (dw > 0 && dh > 0 && src_x1 > src_x0 && src_y1 > src_y0) {
    VkImageBlit blt = {0};
    blt.srcSubresource.aspectMask = VK_IMAGE_ASPECT_COLOR_BIT;
    blt.srcSubresource.layerCount = 1;
    blt.srcOffsets[0] = (VkOffset3D){src_x0, src_y0, 0};
    blt.srcOffsets[1] = (VkOffset3D){src_x1, src_y1, 1};
    blt.dstSubresource.aspectMask = VK_IMAGE_ASPECT_COLOR_BIT;
    blt.dstSubresource.layerCount = 1;
    blt.dstOffsets[0] = (VkOffset3D){dx,      dy,      0};
    blt.dstOffsets[1] = (VkOffset3D){dx + dw, dy + dh, 1};
    vkCmdBlitImage(g_cmdbuf,
        g_tex,                VK_IMAGE_LAYOUT_TRANSFER_SRC_OPTIMAL,
        g_swap_imgs[img_idx], VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL,
        1, &blt, VK_FILTER_LINEAR);
    }

    // Draw virtual cursor (if visible) on top of the video.
    // Use the state snapshot taken at the start of this function so that cursor
    // and viewport are always from the same Go update — never a stale mix.
    if (snap_vis && g_cursor_nspans > 0 &&
        g_cursor_vk_buf != VK_NULL_HANDLE) {
        float uc = snap_uc;
        float vc = snap_vc;
        // Map cursor through the visible source crop (after zoom/pan clip).
        float src_u0 = (fw > 0) ? (float)src_x0 / (float)fw : 0.0f;
        float src_v0 = (fh > 0) ? (float)src_y0 / (float)fh : 0.0f;
        float src_u1 = (fw > 0) ? (float)src_x1 / (float)fw : 1.0f;
        float src_v1 = (fh > 0) ? (float)src_y1 / (float)fh : 1.0f;
        float span_u = src_u1 - src_u0, span_v = src_v1 - src_v0;
        float tu = (span_u > 0.001f) ? (uc - src_u0) / span_u : 0.5f;
        float tv = (span_v > 0.001f) ? (vc - src_v0) / span_v : 0.5f;
        int csx = dx + (int)(tu * dw + 0.5f);
        int csy = dy + (int)(tv * dh + 0.5f);

        // Barrier: serialise blit write before cursor copy writes.
        VkMemoryBarrier mb = {VK_STRUCTURE_TYPE_MEMORY_BARRIER};
        mb.srcAccessMask = VK_ACCESS_TRANSFER_WRITE_BIT;
        mb.dstAccessMask = VK_ACCESS_TRANSFER_WRITE_BIT;
        vkCmdPipelineBarrier(g_cmdbuf,
            VK_PIPELINE_STAGE_TRANSFER_BIT, VK_PIPELINE_STAGE_TRANSFER_BIT,
            0, 1, &mb, 0, NULL, 0, NULL);

        VkBufferImageCopy regs[MAX_CURSOR_SPANS];
        int nreg = 0;
        for (int i = 0; i < g_cursor_nspans; i++) {
            int ax0 = csx + g_cursor_spans[i].rel_x;
            int ax1 = ax0 + g_cursor_spans[i].width;
            int ay  = csy + g_cursor_spans[i].rel_y;
            if (ay < 0 || ay >= sh || ax1 <= 0 || ax0 >= sw) continue;
            int cx0 = ax0 < 0 ? 0 : ax0;
            int cx1 = ax1 > sw ? sw : ax1;
            if (cx1 <= cx0) continue;
            regs[nreg].bufferOffset      = g_cursor_spans[i].buf_off + (uint32_t)(cx0-ax0)*4;
            regs[nreg].bufferRowLength   = 0;
            regs[nreg].bufferImageHeight = 0;
            regs[nreg].imageSubresource.aspectMask     = VK_IMAGE_ASPECT_COLOR_BIT;
            regs[nreg].imageSubresource.mipLevel       = 0;
            regs[nreg].imageSubresource.baseArrayLayer = 0;
            regs[nreg].imageSubresource.layerCount     = 1;
            regs[nreg].imageOffset = (VkOffset3D){cx0, ay, 0};
            regs[nreg].imageExtent = (VkExtent3D){(uint32_t)(cx1-cx0), 1, 1};
            nreg++;
        }
        if (nreg > 0)
            vkCmdCopyBufferToImage(g_cmdbuf, g_cursor_vk_buf,
                g_swap_imgs[img_idx], VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL,
                (uint32_t)nreg, regs);
    }

    vk_cmd_draw_hud(img_idx, fw, fh, dx, dy, dw, dh);
    vk_cmd_punch_holes(g_swap_imgs[img_idx]);

    vk_image_barrier(g_cmdbuf, g_swap_imgs[img_idx],
        VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL, VK_IMAGE_LAYOUT_PRESENT_SRC_KHR,
        VK_ACCESS_TRANSFER_WRITE_BIT, 0,
        VK_PIPELINE_STAGE_TRANSFER_BIT, VK_PIPELINE_STAGE_BOTTOM_OF_PIPE_BIT);

    vkEndCommandBuffer(g_cmdbuf);

    VkPipelineStageFlags wait_stage = VK_PIPELINE_STAGE_TRANSFER_BIT |
                                      VK_PIPELINE_STAGE_COLOR_ATTACHMENT_OUTPUT_BIT;
    VkSubmitInfo si = { VK_STRUCTURE_TYPE_SUBMIT_INFO };
    si.waitSemaphoreCount   = 1; si.pWaitSemaphores   = &g_img_sem;
    si.pWaitDstStageMask    = &wait_stage;
    si.commandBufferCount   = 1; si.pCommandBuffers   = &g_cmdbuf;
    si.signalSemaphoreCount = 1; si.pSignalSemaphores = &g_rnd_sem;
    vkQueueSubmit(g_queue, 1, &si, g_fence);

    VkPresentInfoKHR pi = { VK_STRUCTURE_TYPE_PRESENT_INFO_KHR };
    pi.waitSemaphoreCount = 1; pi.pWaitSemaphores = &g_rnd_sem;
    pi.swapchainCount     = 1; pi.pSwapchains     = &g_swap;
    pi.pImageIndices      = &img_idx;
    res = vkQueuePresentKHR(g_queue, &pi);
    if (res == VK_ERROR_OUT_OF_DATE_KHR || res == VK_SUBOPTIMAL_KHR) {
        VLOGI("vkQueuePresentKHR out of date or suboptimal (%d), recreating swapchain", (int)res);
        vk_recreate_swapchain(); return 1;
    }
    if (res != VK_SUCCESS) {
        VLOGE("vkQueuePresentKHR failed: %d", (int)res);
        return 0;
    }
    return 1;
}

// ── AHardwareBuffer zero-copy path ─────────────────────────────────────────────
// See gl_video_impl_android.c's render_to_hwbuffer for the GL side. Imports
// the same AHardwareBuffer GL just rendered into directly as a VkImage
// (VK_ANDROID_external_memory_android_hardware_buffer) and blits straight
// from it -- no staging buffer, no vkCmdCopyBufferToImage, no CPU touches
// the pixels at all between the GPU shader write and the swapchain present.

static VkImage hwimport_get_or_create(void *ahb_void, int fw, int fh) {
    AHardwareBuffer *ahb = (AHardwareBuffer *)ahb_void;

    if (g_hwimport_w != fw || g_hwimport_h != fh) {
        // GL only reallocates both of its AHardwareBuffers together on
        // resize, so any cached import from the old size is stale for both
        // slots at once -- drop everything rather than risk matching a
        // pointer that happens to have been reused at the new size.
        if (g_hwimport_img[0] != VK_NULL_HANDLE || g_hwimport_img[1] != VK_NULL_HANDLE) {
            vkDeviceWaitIdle(g_dev);
        }
        for (int i = 0; i < HWIMPORT_COUNT; i++) {
            if (g_hwimport_img[i] != VK_NULL_HANDLE) { vkDestroyImage(g_dev, g_hwimport_img[i], NULL); g_hwimport_img[i] = VK_NULL_HANDLE; }
            if (g_hwimport_mem[i] != VK_NULL_HANDLE) { vkFreeMemory(g_dev, g_hwimport_mem[i], NULL);  g_hwimport_mem[i] = VK_NULL_HANDLE; }
            g_hwimport_src[i] = NULL;
        }
        g_hwimport_w = fw; g_hwimport_h = fh;
    }

    for (int i = 0; i < HWIMPORT_COUNT; i++) {
        if (g_hwimport_src[i] == ahb_void && g_hwimport_img[i] != VK_NULL_HANDLE) {
            return g_hwimport_img[i];
        }
    }

    int slot = -1;
    for (int i = 0; i < HWIMPORT_COUNT; i++) if (g_hwimport_src[i] == NULL) { slot = i; break; }
    if (slot < 0) slot = 0; // shouldn't happen (GL only ever hands back one of 2 pointers)

    if (g_hwimport_img[slot] != VK_NULL_HANDLE) {
        vkDeviceWaitIdle(g_dev);
        vkDestroyImage(g_dev, g_hwimport_img[slot], NULL); g_hwimport_img[slot] = VK_NULL_HANDLE;
    }
    if (g_hwimport_mem[slot] != VK_NULL_HANDLE) {
        vkFreeMemory(g_dev, g_hwimport_mem[slot], NULL); g_hwimport_mem[slot] = VK_NULL_HANDLE;
    }

    VkAndroidHardwareBufferFormatPropertiesANDROID fmtProps = { VK_STRUCTURE_TYPE_ANDROID_HARDWARE_BUFFER_FORMAT_PROPERTIES_ANDROID };
    VkAndroidHardwareBufferPropertiesANDROID props = { VK_STRUCTURE_TYPE_ANDROID_HARDWARE_BUFFER_PROPERTIES_ANDROID, &fmtProps };
    if (p_vkGetAndroidHardwareBufferPropertiesANDROID(g_dev, ahb, &props) != VK_SUCCESS) {
        VLOGE("hwimport: vkGetAndroidHardwareBufferPropertiesANDROID failed");
        return VK_NULL_HANDLE;
    }
    if (fmtProps.format == VK_FORMAT_UNDEFINED) {
        // Would need VkExternalFormatANDROID + a sampler Ycbcr conversion to
        // use at all (the path for opaque/YUV buffer formats) -- our buffers
        // are always allocated as plain AHARDWAREBUFFER_FORMAT_R8G8B8A8_UNORM
        // (gl_video_impl_android.c), which should always report a direct
        // format here. Treat anything else as "can't import".
        VLOGE("hwimport: AHardwareBuffer has no direct VkFormat (external format unsupported here)");
        return VK_NULL_HANDLE;
    }

    VkExternalMemoryImageCreateInfo extImgCi = { VK_STRUCTURE_TYPE_EXTERNAL_MEMORY_IMAGE_CREATE_INFO };
    extImgCi.handleTypes = VK_EXTERNAL_MEMORY_HANDLE_TYPE_ANDROID_HARDWARE_BUFFER_BIT_ANDROID;

    VkImageCreateInfo ici = { VK_STRUCTURE_TYPE_IMAGE_CREATE_INFO, &extImgCi };
    ici.imageType     = VK_IMAGE_TYPE_2D;
    ici.format        = fmtProps.format;
    ici.extent        = (VkExtent3D){(uint32_t)fw, (uint32_t)fh, 1};
    ici.mipLevels     = 1;
    ici.arrayLayers   = 1;
    ici.samples       = VK_SAMPLE_COUNT_1_BIT;
    ici.tiling        = VK_IMAGE_TILING_OPTIMAL;
    ici.usage         = VK_IMAGE_USAGE_TRANSFER_SRC_BIT | VK_IMAGE_USAGE_SAMPLED_BIT;
    ici.sharingMode   = VK_SHARING_MODE_EXCLUSIVE;
    ici.initialLayout = VK_IMAGE_LAYOUT_UNDEFINED;

    VkImage img = VK_NULL_HANDLE;
    if (vkCreateImage(g_dev, &ici, NULL, &img) != VK_SUCCESS) {
        VLOGE("hwimport: vkCreateImage failed");
        return VK_NULL_HANDLE;
    }

    VkImportAndroidHardwareBufferInfoANDROID importInfo = { VK_STRUCTURE_TYPE_IMPORT_ANDROID_HARDWARE_BUFFER_INFO_ANDROID };
    importInfo.buffer = ahb;

    // Dedicated allocation is required by the Vulkan spec when importing an
    // AHardwareBuffer that (like ours) has GPU_COLOR_OUTPUT usage.
    VkMemoryDedicatedAllocateInfo dedicated = { VK_STRUCTURE_TYPE_MEMORY_DEDICATED_ALLOCATE_INFO, &importInfo };
    dedicated.image = img;

    VkPhysicalDeviceMemoryProperties mp;
    vkGetPhysicalDeviceMemoryProperties(g_pdev, &mp);
    uint32_t mi = vk_find_mem(&mp, props.memoryTypeBits, 0);
    if (mi == UINT32_MAX) {
        VLOGE("hwimport: no matching memory type for imported buffer");
        vkDestroyImage(g_dev, img, NULL);
        return VK_NULL_HANDLE;
    }

    VkMemoryAllocateInfo mai = { VK_STRUCTURE_TYPE_MEMORY_ALLOCATE_INFO, &dedicated };
    mai.allocationSize  = props.allocationSize;
    mai.memoryTypeIndex = mi;

    VkDeviceMemory mem = VK_NULL_HANDLE;
    if (vkAllocateMemory(g_dev, &mai, NULL, &mem) != VK_SUCCESS) {
        VLOGE("hwimport: vkAllocateMemory (import) failed");
        vkDestroyImage(g_dev, img, NULL);
        return VK_NULL_HANDLE;
    }
    if (vkBindImageMemory(g_dev, img, mem, 0) != VK_SUCCESS) {
        VLOGE("hwimport: vkBindImageMemory failed");
        vkFreeMemory(g_dev, mem, NULL);
        vkDestroyImage(g_dev, img, NULL);
        return VK_NULL_HANDLE;
    }

    g_hwimport_img[slot] = img;
    g_hwimport_mem[slot] = mem;
    g_hwimport_src[slot] = ahb_void;
    VLOGI("hwimport: imported AHardwareBuffer %p as %dx%d VkImage (slot %d, fmt=%d)",
          ahb_void, fw, fh, slot, (int)fmtProps.format);
    return img;
}

// ── Decoder-image path (AImageReader -> YCbCr sampling) ───────────────────────
//
// MediaCodec decodes into AImageReader buffers (moonlight_cgo_android.go);
// each buffer is imported here as a VkImage of the decoder's own (usually
// opaque, "external") YUV format and drawn to the swapchain through a
// VkSamplerYcbcrConversion, so the hardware converts to RGB while sampling.
// No GL pass, no glFinish, no intermediate RGBA copy. The conversion uses the
// model/range/chroma siting the driver suggests for the buffer, which follows
// the colour info the decoder attached to it.
//
// Buffers come from a small fixed pool, so imports are cached per buffer.
// AImage hands out a new AHardwareBuffer* wrapper for every image, so the key
// is the buffer's system-wide id (AHardwareBuffer_getId, Android 12+; older
// systems fall back to the pointer and so import every frame). Each entry
// holds a reference (AHardwareBuffer_acquire) so the buffer, and with it the
// id, stays alive while cached. Render thread only, except
// vk_yc_destroy_all at cleanup.

#define YC_CACHE 24
typedef struct {
    AHardwareBuffer *ahb;
    uint64_t         key;
    VkImage          img;
    VkDeviceMemory   mem;
    VkImageView      view;
    VkDescriptorSet  ds;   // bound to view once, at import
    uint64_t         used;
} yc_entry;
static yc_entry g_yc[YC_CACHE];
static uint64_t g_yc_tick = 0;

typedef int (*pfn_ahb_get_id)(const AHardwareBuffer *, uint64_t *);
static pfn_ahb_get_id p_ahb_get_id = NULL;
static int            g_ahb_get_id_looked = 0;

static uint64_t vk_yc_key(AHardwareBuffer *ahb) {
    if (!g_ahb_get_id_looked) {
        g_ahb_get_id_looked = 1;
        void *lib = dlopen("libandroid.so", RTLD_NOW);
        if (lib) p_ahb_get_id = (pfn_ahb_get_id)dlsym(lib, "AHardwareBuffer_getId");
        VLOGI("ycbcr: AHardwareBuffer_getId %s", p_ahb_get_id ? "available" : "missing (import per frame)");
    }
    uint64_t id = 0;
    if (p_ahb_get_id && p_ahb_get_id(ahb, &id) == 0 && id) return id;
    return (uint64_t)(uintptr_t)ahb;
}

// Conversion + sampler, rebuilt when the decoder's format changes.
static VkFormat                 g_yc_fmt      = VK_FORMAT_UNDEFINED;
static uint64_t                 g_yc_extfmt   = 0;
static int                      g_yc_have_fmt = 0;
static VkSamplerYcbcrConversion g_yc_conv     = VK_NULL_HANDLE;
static VkSampler                g_yc_sampler  = VK_NULL_HANDLE;
// Pipeline objects, rebuilt with the conversion (immutable sampler) or when
// the swapchain format changes (render pass).
static VkDescriptorSetLayout    g_yc_dsl      = VK_NULL_HANDLE;
static VkPipelineLayout         g_yc_pl       = VK_NULL_HANDLE;
static VkRenderPass             g_yc_rp       = VK_NULL_HANDLE;
static VkFormat                 g_yc_rp_fmt   = VK_FORMAT_UNDEFINED;
static VkPipeline               g_yc_pipe     = VK_NULL_HANDLE;
static VkDescriptorPool         g_yc_dpool    = VK_NULL_HANDLE;
// One framebuffer per swapchain image, dropped with the swapchain.
static VkFramebuffer           *g_yc_fbs      = NULL;
static uint32_t                 g_yc_fb_count = 0;

static void vk_yc_destroy_framebuffers(void) {
    if (!g_yc_fbs) return;
    for (uint32_t i = 0; i < g_yc_fb_count; i++)
        if (g_yc_fbs[i]) vkDestroyFramebuffer(g_dev, g_yc_fbs[i], NULL);
    free(g_yc_fbs);
    g_yc_fbs = NULL;
    g_yc_fb_count = 0;
}

static void vk_yc_entry_free(yc_entry *e) {
    if (e->ds && g_yc_dpool) vkFreeDescriptorSets(g_dev, g_yc_dpool, 1, &e->ds);
    if (e->view) vkDestroyImageView(g_dev, e->view, NULL);
    if (e->img)  vkDestroyImage(g_dev, e->img, NULL);
    if (e->mem)  vkFreeMemory(g_dev, e->mem, NULL);
    if (e->ahb)  AHardwareBuffer_release(e->ahb);
    memset(e, 0, sizeof(*e));
}

static void vk_yc_cache_clear(void) {
    for (int i = 0; i < YC_CACHE; i++) vk_yc_entry_free(&g_yc[i]);
}

static void vk_yc_destroy_pipeline(void) {
    // Descriptor sets live in g_yc_dpool: imports go with it.
    vk_yc_cache_clear();
    vk_yc_destroy_framebuffers();
    if (g_yc_pipe)  { vkDestroyPipeline(g_dev, g_yc_pipe, NULL);             g_yc_pipe  = VK_NULL_HANDLE; }
    if (g_yc_dpool) { vkDestroyDescriptorPool(g_dev, g_yc_dpool, NULL);      g_yc_dpool = VK_NULL_HANDLE; }
    if (g_yc_pl)    { vkDestroyPipelineLayout(g_dev, g_yc_pl, NULL);         g_yc_pl    = VK_NULL_HANDLE; }
    if (g_yc_dsl)   { vkDestroyDescriptorSetLayout(g_dev, g_yc_dsl, NULL);   g_yc_dsl   = VK_NULL_HANDLE; }
    if (g_yc_rp)    { vkDestroyRenderPass(g_dev, g_yc_rp, NULL);             g_yc_rp    = VK_NULL_HANDLE; }
    g_yc_rp_fmt = VK_FORMAT_UNDEFINED;
}

// vk_yc_destroy_all frees every decoder-image object. Device must be idle.
static void vk_yc_destroy_all(void) {
    if (!g_dev) return;
    vk_yc_cache_clear();
    vk_yc_destroy_pipeline();
    if (g_yc_sampler) { vkDestroySampler(g_dev, g_yc_sampler, NULL); g_yc_sampler = VK_NULL_HANDLE; }
    if (g_yc_conv)    { p_vkDestroySamplerYcbcrConversionKHR(g_dev, g_yc_conv, NULL); g_yc_conv = VK_NULL_HANDLE; }
    g_yc_have_fmt = 0;
}

static VkShaderModule vk_yc_shader(const uint32_t *code, size_t size) {
    VkShaderModuleCreateInfo ci = { VK_STRUCTURE_TYPE_SHADER_MODULE_CREATE_INFO };
    ci.codeSize = size;
    ci.pCode    = code;
    VkShaderModule m = VK_NULL_HANDLE;
    if (vkCreateShaderModule(g_dev, &ci, NULL, &m) != VK_SUCCESS) return VK_NULL_HANDLE;
    return m;
}

// vk_yc_build_pipeline builds the render pass (for g_swap_fmt) and the
// pipeline around the current immutable sampler.
static int vk_yc_build_pipeline(void) {
    VkAttachmentDescription att = {0};
    att.format         = g_swap_fmt;
    att.samples        = VK_SAMPLE_COUNT_1_BIT;
    att.loadOp         = VK_ATTACHMENT_LOAD_OP_CLEAR;
    att.storeOp        = VK_ATTACHMENT_STORE_OP_STORE;
    att.stencilLoadOp  = VK_ATTACHMENT_LOAD_OP_DONT_CARE;
    att.stencilStoreOp = VK_ATTACHMENT_STORE_OP_DONT_CARE;
    att.initialLayout  = VK_IMAGE_LAYOUT_UNDEFINED;
    // Cursor and menu holes are transfer writes after the pass.
    att.finalLayout    = VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL;
    VkAttachmentReference ref = { 0, VK_IMAGE_LAYOUT_COLOR_ATTACHMENT_OPTIMAL };
    VkSubpassDescription sub = {0};
    sub.pipelineBindPoint    = VK_PIPELINE_BIND_POINT_GRAPHICS;
    sub.colorAttachmentCount = 1;
    sub.pColorAttachments    = &ref;
    VkSubpassDependency deps[2] = {0};
    deps[0].srcSubpass    = VK_SUBPASS_EXTERNAL;
    deps[0].dstSubpass    = 0;
    deps[0].srcStageMask  = VK_PIPELINE_STAGE_COLOR_ATTACHMENT_OUTPUT_BIT;
    deps[0].dstStageMask  = VK_PIPELINE_STAGE_COLOR_ATTACHMENT_OUTPUT_BIT;
    deps[0].dstAccessMask = VK_ACCESS_COLOR_ATTACHMENT_WRITE_BIT;
    deps[1].srcSubpass    = 0;
    deps[1].dstSubpass    = VK_SUBPASS_EXTERNAL;
    deps[1].srcStageMask  = VK_PIPELINE_STAGE_COLOR_ATTACHMENT_OUTPUT_BIT;
    deps[1].srcAccessMask = VK_ACCESS_COLOR_ATTACHMENT_WRITE_BIT;
    deps[1].dstStageMask  = VK_PIPELINE_STAGE_TRANSFER_BIT;
    deps[1].dstAccessMask = VK_ACCESS_TRANSFER_WRITE_BIT | VK_ACCESS_TRANSFER_READ_BIT;
    VkRenderPassCreateInfo rpci = { VK_STRUCTURE_TYPE_RENDER_PASS_CREATE_INFO };
    rpci.attachmentCount = 1; rpci.pAttachments  = &att;
    rpci.subpassCount    = 1; rpci.pSubpasses    = &sub;
    rpci.dependencyCount = 2; rpci.pDependencies = deps;
    if (vkCreateRenderPass(g_dev, &rpci, NULL, &g_yc_rp) != VK_SUCCESS) return 0;
    g_yc_rp_fmt = g_swap_fmt;

    VkDescriptorSetLayoutBinding b = {0};
    b.binding            = 0;
    b.descriptorType     = VK_DESCRIPTOR_TYPE_COMBINED_IMAGE_SAMPLER;
    b.descriptorCount    = 1;
    b.stageFlags         = VK_SHADER_STAGE_FRAGMENT_BIT;
    b.pImmutableSamplers = &g_yc_sampler; // required with a YCbCr conversion
    VkDescriptorSetLayoutCreateInfo dslci = { VK_STRUCTURE_TYPE_DESCRIPTOR_SET_LAYOUT_CREATE_INFO };
    dslci.bindingCount = 1; dslci.pBindings = &b;
    if (vkCreateDescriptorSetLayout(g_dev, &dslci, NULL, &g_yc_dsl) != VK_SUCCESS) return 0;

    VkPushConstantRange pcr = { VK_SHADER_STAGE_VERTEX_BIT, 0, 4 * sizeof(float) };
    VkPipelineLayoutCreateInfo plci = { VK_STRUCTURE_TYPE_PIPELINE_LAYOUT_CREATE_INFO };
    plci.setLayoutCount = 1; plci.pSetLayouts = &g_yc_dsl;
    plci.pushConstantRangeCount = 1; plci.pPushConstantRanges = &pcr;
    if (vkCreatePipelineLayout(g_dev, &plci, NULL, &g_yc_pl) != VK_SUCCESS) return 0;

    // One set per cached import; a multi-planar format can take several
    // descriptors per sampler.
    VkDescriptorPoolSize ps = { VK_DESCRIPTOR_TYPE_COMBINED_IMAGE_SAMPLER, 4 * YC_CACHE };
    VkDescriptorPoolCreateInfo dpci = { VK_STRUCTURE_TYPE_DESCRIPTOR_POOL_CREATE_INFO };
    dpci.flags = VK_DESCRIPTOR_POOL_CREATE_FREE_DESCRIPTOR_SET_BIT;
    dpci.maxSets = YC_CACHE; dpci.poolSizeCount = 1; dpci.pPoolSizes = &ps;
    if (vkCreateDescriptorPool(g_dev, &dpci, NULL, &g_yc_dpool) != VK_SUCCESS) return 0;

    VkShaderModule vs = vk_yc_shader(g_ycbcr_rect_vert_spv, sizeof(g_ycbcr_rect_vert_spv));
    VkShaderModule fs = vk_yc_shader(g_ycbcr_frag_spv, sizeof(g_ycbcr_frag_spv));
    int ok = 0;
    if (vs && fs) {
        VkPipelineShaderStageCreateInfo st[2] = {
            { VK_STRUCTURE_TYPE_PIPELINE_SHADER_STAGE_CREATE_INFO, NULL, 0, VK_SHADER_STAGE_VERTEX_BIT,   vs, "main", NULL },
            { VK_STRUCTURE_TYPE_PIPELINE_SHADER_STAGE_CREATE_INFO, NULL, 0, VK_SHADER_STAGE_FRAGMENT_BIT, fs, "main", NULL },
        };
        VkPipelineVertexInputStateCreateInfo vi = { VK_STRUCTURE_TYPE_PIPELINE_VERTEX_INPUT_STATE_CREATE_INFO };
        VkPipelineInputAssemblyStateCreateInfo ia = { VK_STRUCTURE_TYPE_PIPELINE_INPUT_ASSEMBLY_STATE_CREATE_INFO };
        ia.topology = VK_PRIMITIVE_TOPOLOGY_TRIANGLE_LIST;
        VkPipelineViewportStateCreateInfo vp = { VK_STRUCTURE_TYPE_PIPELINE_VIEWPORT_STATE_CREATE_INFO };
        vp.viewportCount = 1; vp.scissorCount = 1;
        VkPipelineRasterizationStateCreateInfo rs = { VK_STRUCTURE_TYPE_PIPELINE_RASTERIZATION_STATE_CREATE_INFO };
        rs.polygonMode = VK_POLYGON_MODE_FILL;
        rs.cullMode    = VK_CULL_MODE_NONE;
        rs.frontFace   = VK_FRONT_FACE_COUNTER_CLOCKWISE;
        rs.lineWidth   = 1.0f;
        VkPipelineMultisampleStateCreateInfo ms = { VK_STRUCTURE_TYPE_PIPELINE_MULTISAMPLE_STATE_CREATE_INFO };
        ms.rasterizationSamples = VK_SAMPLE_COUNT_1_BIT;
        VkPipelineColorBlendAttachmentState cba = {0};
        cba.colorWriteMask = VK_COLOR_COMPONENT_R_BIT | VK_COLOR_COMPONENT_G_BIT |
                             VK_COLOR_COMPONENT_B_BIT | VK_COLOR_COMPONENT_A_BIT;
        VkPipelineColorBlendStateCreateInfo cb = { VK_STRUCTURE_TYPE_PIPELINE_COLOR_BLEND_STATE_CREATE_INFO };
        cb.attachmentCount = 1; cb.pAttachments = &cba;
        VkDynamicState dyn[2] = { VK_DYNAMIC_STATE_VIEWPORT, VK_DYNAMIC_STATE_SCISSOR };
        VkPipelineDynamicStateCreateInfo ds = { VK_STRUCTURE_TYPE_PIPELINE_DYNAMIC_STATE_CREATE_INFO };
        ds.dynamicStateCount = 2; ds.pDynamicStates = dyn;
        VkGraphicsPipelineCreateInfo gpci = { VK_STRUCTURE_TYPE_GRAPHICS_PIPELINE_CREATE_INFO };
        gpci.stageCount = 2; gpci.pStages = st;
        gpci.pVertexInputState   = &vi;
        gpci.pInputAssemblyState = &ia;
        gpci.pViewportState      = &vp;
        gpci.pRasterizationState = &rs;
        gpci.pMultisampleState   = &ms;
        gpci.pColorBlendState    = &cb;
        gpci.pDynamicState       = &ds;
        gpci.layout     = g_yc_pl;
        gpci.renderPass = g_yc_rp;
        ok = vkCreateGraphicsPipelines(g_dev, VK_NULL_HANDLE, 1, &gpci, NULL, &g_yc_pipe) == VK_SUCCESS;
    }
    if (vs) vkDestroyShaderModule(g_dev, vs, NULL);
    if (fs) vkDestroyShaderModule(g_dev, fs, NULL);
    return ok;
}

// vk_yc_ensure_format makes the conversion, sampler and pipeline match the
// buffer format described by fp (and the current swapchain format).
static int vk_yc_ensure_format(const VkAndroidHardwareBufferFormatPropertiesANDROID *fp) {
    uint64_t ext = fp->format == VK_FORMAT_UNDEFINED ? fp->externalFormat : 0;
    if (!g_yc_have_fmt || g_yc_fmt != fp->format || g_yc_extfmt != ext) {
        vkDeviceWaitIdle(g_dev);
        vk_yc_destroy_all();

        VkExternalFormatANDROID ef = { VK_STRUCTURE_TYPE_EXTERNAL_FORMAT_ANDROID };
        ef.externalFormat = ext;
        VkSamplerYcbcrConversionCreateInfo cci = { VK_STRUCTURE_TYPE_SAMPLER_YCBCR_CONVERSION_CREATE_INFO };
        cci.pNext         = ext ? &ef : NULL;
        cci.format        = fp->format;
        cci.ycbcrModel    = fp->suggestedYcbcrModel;
        cci.ycbcrRange    = fp->suggestedYcbcrRange;
        cci.components    = fp->samplerYcbcrConversionComponents;
        cci.xChromaOffset = fp->suggestedXChromaOffset;
        cci.yChromaOffset = fp->suggestedYChromaOffset;
        VkFilter filter = (fp->formatFeatures & VK_FORMAT_FEATURE_SAMPLED_IMAGE_YCBCR_CONVERSION_LINEAR_FILTER_BIT)
                              ? VK_FILTER_LINEAR : VK_FILTER_NEAREST;
        cci.chromaFilter  = filter;
        if (p_vkCreateSamplerYcbcrConversionKHR(g_dev, &cci, NULL, &g_yc_conv) != VK_SUCCESS) {
            VLOGE("ycbcr: conversion create failed (fmt=%d ext=0x%llx)", (int)fp->format, (unsigned long long)ext);
            g_yc_conv = VK_NULL_HANDLE;
            return 0;
        }
        VkSamplerYcbcrConversionInfo ci = { VK_STRUCTURE_TYPE_SAMPLER_YCBCR_CONVERSION_INFO };
        ci.conversion = g_yc_conv;
        VkSamplerCreateInfo sci = { VK_STRUCTURE_TYPE_SAMPLER_CREATE_INFO, &ci };
        sci.magFilter    = filter;
        sci.minFilter    = filter;
        sci.mipmapMode   = VK_SAMPLER_MIPMAP_MODE_NEAREST;
        sci.addressModeU = VK_SAMPLER_ADDRESS_MODE_CLAMP_TO_EDGE;
        sci.addressModeV = VK_SAMPLER_ADDRESS_MODE_CLAMP_TO_EDGE;
        sci.addressModeW = VK_SAMPLER_ADDRESS_MODE_CLAMP_TO_EDGE;
        sci.maxLod       = 0.0f;
        if (vkCreateSampler(g_dev, &sci, NULL, &g_yc_sampler) != VK_SUCCESS) {
            VLOGE("ycbcr: sampler create failed");
            g_yc_sampler = VK_NULL_HANDLE;
            return 0;
        }
        g_yc_fmt = fp->format; g_yc_extfmt = ext; g_yc_have_fmt = 1;
        VLOGI("ycbcr: conversion for fmt=%d ext=0x%llx model=%d range=%d chroma=(%d,%d) filter=%s",
              (int)fp->format, (unsigned long long)ext, (int)fp->suggestedYcbcrModel,
              (int)fp->suggestedYcbcrRange, (int)fp->suggestedXChromaOffset,
              (int)fp->suggestedYChromaOffset, filter == VK_FILTER_LINEAR ? "linear" : "nearest");
    }
    if (g_yc_pipe && g_yc_rp_fmt != g_swap_fmt) {
        vkDeviceWaitIdle(g_dev);
        vk_yc_destroy_pipeline();
    }
    if (!g_yc_pipe && !vk_yc_build_pipeline()) {
        VLOGE("ycbcr: pipeline build failed");
        vk_yc_destroy_pipeline();
        return 0;
    }
    return 1;
}

// vk_yc_get returns the cached (or freshly imported) view of ahb.
static yc_entry *vk_yc_get(AHardwareBuffer *ahb) {
    g_yc_tick++;
    uint64_t key = vk_yc_key(ahb);
    if (g_yc_have_fmt && g_yc_pipe && g_yc_rp_fmt == g_swap_fmt) {
        for (int i = 0; i < YC_CACHE; i++)
            if (g_yc[i].ahb && g_yc[i].key == key) { g_yc[i].used = g_yc_tick; return &g_yc[i]; }
    }

    VkAndroidHardwareBufferFormatPropertiesANDROID fp = { VK_STRUCTURE_TYPE_ANDROID_HARDWARE_BUFFER_FORMAT_PROPERTIES_ANDROID };
    VkAndroidHardwareBufferPropertiesANDROID props = { VK_STRUCTURE_TYPE_ANDROID_HARDWARE_BUFFER_PROPERTIES_ANDROID, &fp };
    if (p_vkGetAndroidHardwareBufferPropertiesANDROID(g_dev, ahb, &props) != VK_SUCCESS) {
        VLOGE("ycbcr: vkGetAndroidHardwareBufferPropertiesANDROID failed");
        return NULL;
    }
    if (!vk_yc_ensure_format(&fp)) return NULL;
    // ensure_format may have flushed the cache; look again.
    for (int i = 0; i < YC_CACHE; i++)
        if (g_yc[i].ahb && g_yc[i].key == key) { g_yc[i].used = g_yc_tick; return &g_yc[i]; }

    // Evict the least recently used entry. Its last use is a finished
    // frame: the caller waited g_fence before getting here.
    int slot = 0;
    for (int i = 0; i < YC_CACHE; i++) {
        if (!g_yc[i].ahb) { slot = i; break; }
        if (g_yc[i].used < g_yc[slot].used) slot = i;
    }
    yc_entry *e = &g_yc[slot];
    if (e->ahb) vk_yc_entry_free(e);

    AHardwareBuffer_Desc desc;
    AHardwareBuffer_describe(ahb, &desc);

    VkExternalFormatANDROID ef = { VK_STRUCTURE_TYPE_EXTERNAL_FORMAT_ANDROID };
    ef.externalFormat = g_yc_extfmt;
    VkExternalMemoryImageCreateInfo emi = { VK_STRUCTURE_TYPE_EXTERNAL_MEMORY_IMAGE_CREATE_INFO, &ef };
    emi.handleTypes = VK_EXTERNAL_MEMORY_HANDLE_TYPE_ANDROID_HARDWARE_BUFFER_BIT_ANDROID;
    VkImageCreateInfo ici = { VK_STRUCTURE_TYPE_IMAGE_CREATE_INFO, &emi };
    ici.imageType     = VK_IMAGE_TYPE_2D;
    ici.format        = g_yc_fmt;
    ici.extent        = (VkExtent3D){desc.width, desc.height, 1};
    ici.mipLevels     = 1;
    ici.arrayLayers   = 1;
    ici.samples       = VK_SAMPLE_COUNT_1_BIT;
    ici.tiling        = VK_IMAGE_TILING_OPTIMAL;
    ici.usage         = VK_IMAGE_USAGE_SAMPLED_BIT;
    ici.sharingMode   = VK_SHARING_MODE_EXCLUSIVE;
    ici.initialLayout = VK_IMAGE_LAYOUT_UNDEFINED;
    VkImage img = VK_NULL_HANDLE;
    if (vkCreateImage(g_dev, &ici, NULL, &img) != VK_SUCCESS) {
        VLOGE("ycbcr: vkCreateImage failed (%ux%u fmt=%d)", desc.width, desc.height, (int)g_yc_fmt);
        return NULL;
    }

    VkImportAndroidHardwareBufferInfoANDROID imp = { VK_STRUCTURE_TYPE_IMPORT_ANDROID_HARDWARE_BUFFER_INFO_ANDROID };
    imp.buffer = ahb;
    VkMemoryDedicatedAllocateInfo ded = { VK_STRUCTURE_TYPE_MEMORY_DEDICATED_ALLOCATE_INFO, &imp };
    ded.image = img;
    VkPhysicalDeviceMemoryProperties mp;
    vkGetPhysicalDeviceMemoryProperties(g_pdev, &mp);
    uint32_t mi = vk_find_mem(&mp, props.memoryTypeBits, 0);
    VkMemoryAllocateInfo mai = { VK_STRUCTURE_TYPE_MEMORY_ALLOCATE_INFO, &ded };
    mai.allocationSize  = props.allocationSize;
    mai.memoryTypeIndex = mi;
    VkDeviceMemory mem = VK_NULL_HANDLE;
    if (mi == UINT32_MAX || vkAllocateMemory(g_dev, &mai, NULL, &mem) != VK_SUCCESS) {
        VLOGE("ycbcr: import allocation failed");
        vkDestroyImage(g_dev, img, NULL);
        return NULL;
    }
    if (vkBindImageMemory(g_dev, img, mem, 0) != VK_SUCCESS) {
        VLOGE("ycbcr: vkBindImageMemory failed");
        vkFreeMemory(g_dev, mem, NULL);
        vkDestroyImage(g_dev, img, NULL);
        return NULL;
    }

    VkSamplerYcbcrConversionInfo ci = { VK_STRUCTURE_TYPE_SAMPLER_YCBCR_CONVERSION_INFO };
    ci.conversion = g_yc_conv;
    VkImageViewCreateInfo vci = { VK_STRUCTURE_TYPE_IMAGE_VIEW_CREATE_INFO, &ci };
    vci.image    = img;
    vci.viewType = VK_IMAGE_VIEW_TYPE_2D;
    vci.format   = g_yc_fmt;
    vci.subresourceRange.aspectMask = VK_IMAGE_ASPECT_COLOR_BIT;
    vci.subresourceRange.levelCount = 1;
    vci.subresourceRange.layerCount = 1;
    VkImageView view = VK_NULL_HANDLE;
    if (vkCreateImageView(g_dev, &vci, NULL, &view) != VK_SUCCESS) {
        VLOGE("ycbcr: vkCreateImageView failed");
        vkFreeMemory(g_dev, mem, NULL);
        vkDestroyImage(g_dev, img, NULL);
        return NULL;
    }

    VkDescriptorSet ds = VK_NULL_HANDLE;
    VkDescriptorSetAllocateInfo dsai = { VK_STRUCTURE_TYPE_DESCRIPTOR_SET_ALLOCATE_INFO };
    dsai.descriptorPool = g_yc_dpool; dsai.descriptorSetCount = 1; dsai.pSetLayouts = &g_yc_dsl;
    if (vkAllocateDescriptorSets(g_dev, &dsai, &ds) != VK_SUCCESS) {
        VLOGE("ycbcr: vkAllocateDescriptorSets failed");
        vkDestroyImageView(g_dev, view, NULL);
        vkFreeMemory(g_dev, mem, NULL);
        vkDestroyImage(g_dev, img, NULL);
        return NULL;
    }
    VkDescriptorImageInfo dii = { VK_NULL_HANDLE, view, VK_IMAGE_LAYOUT_SHADER_READ_ONLY_OPTIMAL };
    VkWriteDescriptorSet w = { VK_STRUCTURE_TYPE_WRITE_DESCRIPTOR_SET };
    w.dstSet = ds; w.dstBinding = 0; w.descriptorCount = 1;
    w.descriptorType = VK_DESCRIPTOR_TYPE_COMBINED_IMAGE_SAMPLER;
    w.pImageInfo = &dii;
    vkUpdateDescriptorSets(g_dev, 1, &w, 0, NULL);

    AHardwareBuffer_acquire(ahb);
    e->ahb = ahb; e->key = key; e->img = img; e->mem = mem; e->view = view; e->ds = ds; e->used = g_yc_tick;
    VLOGI("ycbcr: imported decoder buffer %p %ux%u (slot %d)", (void *)ahb, desc.width, desc.height, slot);
    return e;
}

static int vk_yc_ensure_framebuffers(void) {
    if (g_yc_fbs && g_yc_fb_count == g_swap_count) return 1;
    vk_yc_destroy_framebuffers();
    g_yc_fbs = calloc(g_swap_count, sizeof(VkFramebuffer));
    if (!g_yc_fbs) return 0;
    g_yc_fb_count = g_swap_count;
    for (uint32_t i = 0; i < g_swap_count; i++) {
        VkFramebufferCreateInfo fci = { VK_STRUCTURE_TYPE_FRAMEBUFFER_CREATE_INFO };
        fci.renderPass      = g_yc_rp;
        fci.attachmentCount = 1;
        fci.pAttachments    = &g_swap_views[i];
        fci.width           = g_swap_ext.width;
        fci.height          = g_swap_ext.height;
        fci.layers          = 1;
        if (vkCreateFramebuffer(g_dev, &fci, NULL, &g_yc_fbs[i]) != VK_SUCCESS) {
            vk_yc_destroy_framebuffers();
            return 0;
        }
    }
    return 1;
}

// A decoder image to draw instead of an RGBA AHardwareBuffer: the buffer and
// the visible crop of it (decoders pad to their block size).
typedef struct {
    AHardwareBuffer *ahb;
    int buf_w, buf_h;
    int crop_x, crop_y;
} vk_yc_frame;

// vk_render_frame_hw presents one frame from an AHardwareBuffer: an RGBA
// buffer GL rendered (ahb_void, blitted), or, with ycf, a decoder buffer
// sampled through the YCbCr pipeline. fw x fh is the visible frame size.
static int vk_render_frame_hw(void *ahb_void, int fw, int fh, const vk_yc_frame *ycf) {
    if (!g_dev) return 0;
    if (!g_swap) { vk_recreate_swapchain(); return 0; }

    {
        VkSurfaceCapabilitiesKHR caps;
        if (vkGetPhysicalDeviceSurfaceCapabilitiesKHR(g_pdev, g_surf, &caps) == VK_SUCCESS) {
            VkExtent2D cur = vk_surface_extent(&caps);
            if (cur.width != 0 && cur.height != 0 &&
                (cur.width != g_swap_ext.width || cur.height != g_swap_ext.height)) {
                VLOGI("surface resized %ux%u → %ux%u, recreating swapchain",
                      g_swap_ext.width, g_swap_ext.height, cur.width, cur.height);
                vk_recreate_swapchain();
                return 0;
            }
        } else {
            vk_recreate_swapchain();
            return 0;
        }
    }

    VkImage srcImg = VK_NULL_HANDLE;
    yc_entry *yce = NULL;
    if (ycf) {
        // Importing may evict a cached image: the previous frame must be
        // finished first.
        // (Wait only; the reset happens after acquire as usual.)
        if (vkWaitForFences(g_dev, 1, &g_fence, VK_TRUE, 2000000000ULL) == VK_TIMEOUT) {
            VLOGE("vkWaitForFences timeout (2s)");
            return 0;
        }
        yce = vk_yc_get(ycf->ahb);
        if (!yce || !vk_yc_ensure_framebuffers()) return 0;
    } else {
        srcImg = hwimport_get_or_create(ahb_void, fw, fh);
        if (srcImg == VK_NULL_HANDLE) return 0;
    }

    uint32_t img_idx = 0;
    VkResult res = vkAcquireNextImageKHR(g_dev, g_swap, 2000000000ULL,
                                          g_img_sem, VK_NULL_HANDLE, &img_idx);
    if (res == VK_ERROR_OUT_OF_DATE_KHR) { vk_recreate_swapchain(); return 0; }
    if (res != VK_SUCCESS && res != VK_SUBOPTIMAL_KHR) return 0;

    if (vkWaitForFences(g_dev, 1, &g_fence, VK_TRUE, 2000000000ULL) == VK_TIMEOUT) {
        VLOGE("vkWaitForFences timeout (2s)");
        vkResetFences(g_dev, 1, &g_fence); return 0;
    }
    vkResetFences(g_dev, 1, &g_fence);

    vkResetCommandBuffer(g_cmdbuf, 0);
    VkCommandBufferBeginInfo bi = { VK_STRUCTURE_TYPE_COMMAND_BUFFER_BEGIN_INFO };
    bi.flags = VK_COMMAND_BUFFER_USAGE_ONE_TIME_SUBMIT_BIT;
    vkBeginCommandBuffer(g_cmdbuf, &bi);

    if (yce) {
        // The decoder wrote this buffer (AImageReader waited its fence);
        // take it from the foreign queue for sampling.
        VkImageMemoryBarrier b = { VK_STRUCTURE_TYPE_IMAGE_MEMORY_BARRIER };
        b.oldLayout = VK_IMAGE_LAYOUT_UNDEFINED;
        b.newLayout = VK_IMAGE_LAYOUT_SHADER_READ_ONLY_OPTIMAL;
        b.srcQueueFamilyIndex = VK_QUEUE_FAMILY_FOREIGN_EXT;
        b.dstQueueFamilyIndex = g_qfam;
        b.image = yce->img;
        b.subresourceRange.aspectMask = VK_IMAGE_ASPECT_COLOR_BIT;
        b.subresourceRange.levelCount = 1;
        b.subresourceRange.layerCount = 1;
        b.dstAccessMask = VK_ACCESS_SHADER_READ_BIT;
        vkCmdPipelineBarrier(g_cmdbuf, VK_PIPELINE_STAGE_TOP_OF_PIPE_BIT,
            VK_PIPELINE_STAGE_FRAGMENT_SHADER_BIT, 0, 0, NULL, 0, NULL, 1, &b);
    } else {
        // GL already wrote (and glFinish()'d) this frame's pixels straight into
        // srcImg's backing memory -- there's nothing for us to copy in, just a
        // layout transition before reading it. Treated as UNDEFINED->TRANSFER_SRC
        // every frame rather than tracking Vulkan-side layout across GL/Vulkan API
        // boundaries: we never write to this image via Vulkan ourselves, only
        // ever blit-read it, so there's no prior Vulkan-tracked content to lose.
        vk_image_barrier(g_cmdbuf, srcImg,
            VK_IMAGE_LAYOUT_UNDEFINED, VK_IMAGE_LAYOUT_TRANSFER_SRC_OPTIMAL,
            0, VK_ACCESS_TRANSFER_READ_BIT,
            VK_PIPELINE_STAGE_TOP_OF_PIPE_BIT, VK_PIPELINE_STAGE_TRANSFER_BIT);

        vk_image_barrier(g_cmdbuf, g_swap_imgs[img_idx],
            VK_IMAGE_LAYOUT_UNDEFINED, VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL,
            0, VK_ACCESS_TRANSFER_WRITE_BIT,
            VK_PIPELINE_STAGE_TOP_OF_PIPE_BIT, VK_PIPELINE_STAGE_TRANSFER_BIT);
    }

    // Snapshot viewport + cursor state atomically, identical to vk_render_frame.
    pthread_mutex_lock(&g_state_mu);
    float u0 = atomic_load(&g_vp_u0_fp) / 65536.0f;
    float v0 = atomic_load(&g_vp_v0_fp) / 65536.0f;
    float u1 = atomic_load(&g_vp_u1_fp) / 65536.0f;
    float v1 = atomic_load(&g_vp_v1_fp) / 65536.0f;
    float snap_uc  = atomic_load(&g_cursor_uc_fp)  / 65536.0f;
    float snap_vc  = atomic_load(&g_cursor_vc_fp)  / 65536.0f;
    int   snap_vis = atomic_load(&g_cursor_visible);
    int   snap_pan_x = atomic_load(&g_blit_pan_x);
    int   snap_pan_y = atomic_load(&g_blit_pan_y);
    float snap_zoom  = atomic_load(&g_blit_zoom_fp) / 65536.0f;
    pthread_mutex_unlock(&g_state_mu);
    if (u0 < 0.0f) u0 = 0.0f; if (u1 > 1.0f) u1 = 1.0f;
    if (v0 < 0.0f) v0 = 0.0f; if (v1 > 1.0f) v1 = 1.0f;
    if (u1 <= u0 + 0.001f) { u0 = 0.0f; u1 = 1.0f; }
    if (v1 <= v0 + 0.001f) { v0 = 0.0f; v1 = 1.0f; }

    int src_x0 = 0, src_y0 = 0, src_x1 = fw, src_y1 = fh;
    int sw = (int)g_swap_ext.width, sh = (int)g_swap_ext.height;
    int dx = 0, dy = 0, dw = sw, dh = sh;
    vk_layout_zoomed_dest(fw, fh, sw, sh, snap_zoom, snap_pan_x, snap_pan_y,
                          &dx, &dy, &dw, &dh, &src_x0, &src_y0, &src_x1, &src_y1);
    vk_store_video_dest(dx, dy, dw, dh, sw, sh);

    if (yce) {
        // Render pass clears to opaque black (alpha 0 is reserved for menu
        // holes) and leaves the image in TRANSFER_DST for cursor and holes.
        VkClearValue clear = { .color = {{0.0f, 0.0f, 0.0f, 1.0f}} };
        VkRenderPassBeginInfo rbi = { VK_STRUCTURE_TYPE_RENDER_PASS_BEGIN_INFO };
        rbi.renderPass  = g_yc_rp;
        rbi.framebuffer = g_yc_fbs[img_idx];
        rbi.renderArea.extent = g_swap_ext;
        rbi.clearValueCount = 1; rbi.pClearValues = &clear;
        vkCmdBeginRenderPass(g_cmdbuf, &rbi, VK_SUBPASS_CONTENTS_INLINE);
        int sx0 = dx < 0 ? 0 : dx, sy0 = dy < 0 ? 0 : dy;
        int sx1 = dx + dw > sw ? sw : dx + dw, sy1 = dy + dh > sh ? sh : dy + dh;
        if (sx1 > sx0 && sy1 > sy0 && src_x1 > src_x0 && src_y1 > src_y0) {
            VkViewport vpt = { (float)dx, (float)dy, (float)dw, (float)dh, 0.0f, 1.0f };
            VkRect2D sc = { { sx0, sy0 }, { (uint32_t)(sx1 - sx0), (uint32_t)(sy1 - sy0) } };
            float pc[4] = {
                (float)(ycf->crop_x + src_x0) / (float)ycf->buf_w,
                (float)(ycf->crop_y + src_y0) / (float)ycf->buf_h,
                (float)(src_x1 - src_x0) / (float)ycf->buf_w,
                (float)(src_y1 - src_y0) / (float)ycf->buf_h,
            };
            vkCmdBindPipeline(g_cmdbuf, VK_PIPELINE_BIND_POINT_GRAPHICS, g_yc_pipe);
            vkCmdSetViewport(g_cmdbuf, 0, 1, &vpt);
            vkCmdSetScissor(g_cmdbuf, 0, 1, &sc);
            vkCmdPushConstants(g_cmdbuf, g_yc_pl, VK_SHADER_STAGE_VERTEX_BIT, 0, sizeof(pc), pc);
            vkCmdBindDescriptorSets(g_cmdbuf, VK_PIPELINE_BIND_POINT_GRAPHICS, g_yc_pl,
                                    0, 1, &yce->ds, 0, NULL);
            vkCmdDraw(g_cmdbuf, 3, 1, 0, 0);
        }
        vkCmdEndRenderPass(g_cmdbuf);
    } else {
        // Opaque black: the surface is translucent, alpha 0 is reserved for holes.
        VkClearColorValue black = {{0.0f, 0.0f, 0.0f, 1.0f}};
        VkImageSubresourceRange full = { VK_IMAGE_ASPECT_COLOR_BIT, 0, 1, 0, 1 };
        vkCmdClearColorImage(g_cmdbuf, g_swap_imgs[img_idx],
                             VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL, &black, 1, &full);

        vk_image_barrier(g_cmdbuf, g_swap_imgs[img_idx],
            VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL, VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL,
            VK_ACCESS_TRANSFER_WRITE_BIT, VK_ACCESS_TRANSFER_WRITE_BIT,
            VK_PIPELINE_STAGE_TRANSFER_BIT, VK_PIPELINE_STAGE_TRANSFER_BIT);

        if (dw > 0 && dh > 0 && src_x1 > src_x0 && src_y1 > src_y0) {
        VkImageBlit blt = {0};
        blt.srcSubresource.aspectMask = VK_IMAGE_ASPECT_COLOR_BIT;
        blt.srcSubresource.layerCount = 1;
        blt.srcOffsets[0] = (VkOffset3D){src_x0, src_y0, 0};
        blt.srcOffsets[1] = (VkOffset3D){src_x1, src_y1, 1};
        blt.dstSubresource.aspectMask = VK_IMAGE_ASPECT_COLOR_BIT;
        blt.dstSubresource.layerCount = 1;
        blt.dstOffsets[0] = (VkOffset3D){dx,      dy,      0};
        blt.dstOffsets[1] = (VkOffset3D){dx + dw, dy + dh, 1};
        vkCmdBlitImage(g_cmdbuf,
            srcImg,                VK_IMAGE_LAYOUT_TRANSFER_SRC_OPTIMAL,
            g_swap_imgs[img_idx], VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL,
            1, &blt, VK_FILTER_LINEAR);
        }
    }

    if (snap_vis && g_cursor_nspans > 0 &&
        g_cursor_vk_buf != VK_NULL_HANDLE) {
        float uc = snap_uc;
        float vc = snap_vc;
        // Map cursor through the visible source crop (after zoom/pan clip).
        float src_u0 = (fw > 0) ? (float)src_x0 / (float)fw : 0.0f;
        float src_v0 = (fh > 0) ? (float)src_y0 / (float)fh : 0.0f;
        float src_u1 = (fw > 0) ? (float)src_x1 / (float)fw : 1.0f;
        float src_v1 = (fh > 0) ? (float)src_y1 / (float)fh : 1.0f;
        float span_u = src_u1 - src_u0, span_v = src_v1 - src_v0;
        float tu = (span_u > 0.001f) ? (uc - src_u0) / span_u : 0.5f;
        float tv = (span_v > 0.001f) ? (vc - src_v0) / span_v : 0.5f;
        (void)u0; (void)v0; (void)u1; (void)v1;
        int csx = dx + (int)(tu * dw + 0.5f);
        int csy = dy + (int)(tv * dh + 0.5f);

        VkMemoryBarrier mb = {VK_STRUCTURE_TYPE_MEMORY_BARRIER};
        mb.srcAccessMask = VK_ACCESS_TRANSFER_WRITE_BIT;
        mb.dstAccessMask = VK_ACCESS_TRANSFER_WRITE_BIT;
        vkCmdPipelineBarrier(g_cmdbuf,
            VK_PIPELINE_STAGE_TRANSFER_BIT, VK_PIPELINE_STAGE_TRANSFER_BIT,
            0, 1, &mb, 0, NULL, 0, NULL);

        VkBufferImageCopy regs[MAX_CURSOR_SPANS];
        int nreg = 0;
        for (int i = 0; i < g_cursor_nspans; i++) {
            int ax0 = csx + g_cursor_spans[i].rel_x;
            int ax1 = ax0 + g_cursor_spans[i].width;
            int ay  = csy + g_cursor_spans[i].rel_y;
            if (ay < 0 || ay >= sh || ax1 <= 0 || ax0 >= sw) continue;
            int cx0 = ax0 < 0 ? 0 : ax0;
            int cx1 = ax1 > sw ? sw : ax1;
            if (cx1 <= cx0) continue;
            regs[nreg].bufferOffset      = g_cursor_spans[i].buf_off + (uint32_t)(cx0-ax0)*4;
            regs[nreg].bufferRowLength   = 0;
            regs[nreg].bufferImageHeight = 0;
            regs[nreg].imageSubresource.aspectMask     = VK_IMAGE_ASPECT_COLOR_BIT;
            regs[nreg].imageSubresource.mipLevel       = 0;
            regs[nreg].imageSubresource.baseArrayLayer = 0;
            regs[nreg].imageSubresource.layerCount     = 1;
            regs[nreg].imageOffset = (VkOffset3D){cx0, ay, 0};
            regs[nreg].imageExtent = (VkExtent3D){(uint32_t)(cx1-cx0), 1, 1};
            nreg++;
        }
        if (nreg > 0)
            vkCmdCopyBufferToImage(g_cmdbuf, g_cursor_vk_buf,
                g_swap_imgs[img_idx], VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL,
                (uint32_t)nreg, regs);
    }

    vk_cmd_draw_hud(img_idx, fw, fh, dx, dy, dw, dh);
    vk_cmd_punch_holes(g_swap_imgs[img_idx]);

    vk_image_barrier(g_cmdbuf, g_swap_imgs[img_idx],
        VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL, VK_IMAGE_LAYOUT_PRESENT_SRC_KHR,
        VK_ACCESS_TRANSFER_WRITE_BIT, 0,
        VK_PIPELINE_STAGE_TRANSFER_BIT, VK_PIPELINE_STAGE_BOTTOM_OF_PIPE_BIT);

    vkEndCommandBuffer(g_cmdbuf);

    VkPipelineStageFlags wait_stage = VK_PIPELINE_STAGE_TRANSFER_BIT |
                                      VK_PIPELINE_STAGE_COLOR_ATTACHMENT_OUTPUT_BIT;
    VkSubmitInfo si = { VK_STRUCTURE_TYPE_SUBMIT_INFO };
    si.waitSemaphoreCount   = 1; si.pWaitSemaphores   = &g_img_sem;
    si.pWaitDstStageMask    = &wait_stage;
    si.commandBufferCount   = 1; si.pCommandBuffers   = &g_cmdbuf;
    si.signalSemaphoreCount = 1; si.pSignalSemaphores = &g_rnd_sem;
    vkQueueSubmit(g_queue, 1, &si, g_fence);

    VkPresentInfoKHR pi = { VK_STRUCTURE_TYPE_PRESENT_INFO_KHR };
    pi.waitSemaphoreCount = 1; pi.pWaitSemaphores = &g_rnd_sem;
    pi.swapchainCount     = 1; pi.pSwapchains     = &g_swap;
    pi.pImageIndices      = &img_idx;
    res = vkQueuePresentKHR(g_queue, &pi);
    if (res == VK_ERROR_OUT_OF_DATE_KHR || res == VK_SUBOPTIMAL_KHR) {
        VLOGI("vkQueuePresentKHR out of date or suboptimal (%d), recreating swapchain", (int)res);
        vk_recreate_swapchain(); return 1;
    }
    if (res != VK_SUCCESS) {
        VLOGE("vkQueuePresentKHR failed: %d", (int)res);
        return 0;
    }
    return 1;
}

static double mono_sec_vk(void) {
    struct timespec ts;
    clock_gettime(CLOCK_MONOTONIC, &ts);
    return (double)ts.tv_sec + (double)ts.tv_nsec * 1e-9;
}

// ─── render thread ────────────────────────────────────────────────────────────

// vk_note_video_frame_rendered counts a newly presented video frame (not a
// cursor-only redraw) toward FPS and latency stats.
static void vk_note_video_frame_rendered(void) {
    g_rendered++;
    vlat_note_present();
    g_fps_n++;
    double now = mono_sec_vk();
    if (g_cad_last > 0) {
        double d = now - g_cad_last;
        if (d < 0.0125 || d > 0.0208) g_cad_uneven++;
        g_cad_n++;
    }
    g_cad_last = now;
    if (g_rendered == 1) { g_fps_t0 = now; g_fps_n = 0; }
    if (now - g_fps_t0 >= 2.0 && g_fps_n > 0) {
        g_stat_fps   = (float)((double)g_fps_n / (now - g_fps_t0));
        g_stat_ready = 1;
        g_fps_t0 = now; g_fps_n = 0;
    }
    if (g_rendered % 300 == 0) {
        VLOGI("rendered %lld frames, submitted %lld, fps=%.1f uneven=%d/%d%s pace_margin=%.1fms hold=%.1fms",
              (long long)g_rendered, (long long)g_submitted, (double)g_stat_fps, g_cad_uneven, g_cad_n,
              g_pace_on ? "" : " (pacing off)", g_pace_on ? FramePacerMarginUs(&g_pacer) / 1000.0 : 0.0,
              (double)atomic_load(&g_pace_hold_us) / 1000.0);
        g_cad_uneven = 0;
        g_cad_n = 0;
    }
}

static int64_t pace_now_us(void) {
    struct timespec ts;
    clock_gettime(CLOCK_MONOTONIC, &ts);
    return (int64_t)ts.tv_sec * 1000000 + ts.tv_nsec / 1000;
}

// Caller holds g_mu.
static void pace_drop_queued(void) {
    for (int i = 0; i < g_pace_n; i++) AImage_delete(g_pace_img[i]);
    g_pace_n = 0;
}

// pace_take_due moves queued images whose time has come to g_pend_aimg (the
// newest due one wins) and returns how long until the next one is due, or -1
// with none queued. Caller holds g_mu.
static int64_t pace_take_due(void) {
    int64_t now = pace_now_us();
    while (g_pace_n > 0 && g_pace_at[0] - PACE_RENDER_LEAD_US <= now) {
        if (g_pend_aimg) {
            AImage_delete(g_pend_aimg);
            vlat_note_dropped();
        }
        g_pend_aimg = g_pace_img[0];
        g_pace_n--;
        memmove(g_pace_img, g_pace_img + 1, (size_t)g_pace_n * sizeof(g_pace_img[0]));
        memmove(g_pace_at, g_pace_at + 1, (size_t)g_pace_n * sizeof(g_pace_at[0]));
    }
    return g_pace_n > 0 ? g_pace_at[0] - PACE_RENDER_LEAD_US - now : -1;
}

// vk_aimg_set_current puts img on screen and deletes the previous one once
// the GPU is done with it.
static void vk_aimg_set_current(AImage *img) {
    if (g_cur_aimg && g_cur_aimg != img) {
        if (g_use_gles) gles_ov_wait_idle();
        else if (g_dev && g_fence) vkWaitForFences(g_dev, 1, &g_fence, VK_TRUE, 2000000000ULL);
        AImage_delete(g_cur_aimg);
    }
    g_cur_aimg = img;
}

// vk_aimg_drop_all gives every decoder image back, and with them the cached
// imports (which hold references to the reader's buffers).
static void vk_aimg_drop_all(void) {
    vk_aimg_set_current(NULL);
    pthread_mutex_lock(&g_mu);
    AImage *pend = g_pend_aimg;
    g_pend_aimg = NULL;
    pace_drop_queued();
    pthread_mutex_unlock(&g_mu);
    if (pend) AImage_delete(pend);
    if (g_use_gles) {
        gles_ov_drop_images();
    } else if (g_dev) {
        vkDeviceWaitIdle(g_dev);
        vk_yc_cache_clear();
    }
}

// gles_render presents one frame through the GLES fallback: a decoder image
// (img) or a CPU RGBA frame (rgba, fw x fh, tightly packed). Same layout,
// cursor and holes as the Vulkan path.
static int gles_render(AImage *img, const uint8_t *rgba, int fw, int fh) {
    int sw = 0, sh = 0;
    if (!gles_ov_surface_size(&sw, &sh)) return 0;
    if (atomic_exchange(&g_gles_cursor_dirty, 0)) {
        pthread_mutex_lock(&g_mu);
        gles_ov_set_cursor(g_gles_cursor, g_gles_cursor_w, g_gles_cursor_h);
        pthread_mutex_unlock(&g_mu);
    }

    gles_ov_frame f;
    memset(&f, 0, sizeof(f));
    f.surf_w = sw; f.surf_h = sh;
    int crop_x = 0, crop_y = 0;
    if (img) {
        AHardwareBuffer *ahb = NULL;
        AImageCropRect crop;
        if (AImage_getHardwareBuffer(img, &ahb) != AMEDIA_OK || !ahb) return 0;
        AHardwareBuffer_Desc desc;
        AHardwareBuffer_describe(ahb, &desc);
        if (AImage_getCropRect(img, &crop) != AMEDIA_OK || crop.right <= crop.left || crop.bottom <= crop.top) {
            crop.left = 0; crop.top = 0; crop.right = (int32_t)desc.width; crop.bottom = (int32_t)desc.height;
        }
        f.ahb = ahb;
        f.tex_w = (int)desc.width; f.tex_h = (int)desc.height;
        crop_x = crop.left; crop_y = crop.top;
        fw = crop.right - crop.left; fh = crop.bottom - crop.top;
    } else {
        f.rgba = rgba;
        f.tex_w = fw; f.tex_h = fh;
    }

    pthread_mutex_lock(&g_state_mu);
    float snap_uc  = atomic_load(&g_cursor_uc_fp)  / 65536.0f;
    float snap_vc  = atomic_load(&g_cursor_vc_fp)  / 65536.0f;
    int   snap_vis = atomic_load(&g_cursor_visible);
    int   snap_pan_x = atomic_load(&g_blit_pan_x);
    int   snap_pan_y = atomic_load(&g_blit_pan_y);
    float snap_zoom  = atomic_load(&g_blit_zoom_fp) / 65536.0f;
    int holes[VK_MAX_HOLES * 4];
    int nholes = 0;
    for (int i = 0; i < g_hole_count; i++) {
        holes[nholes * 4 + 0] = g_holes[i].offset.x;
        holes[nholes * 4 + 1] = g_holes[i].offset.y;
        holes[nholes * 4 + 2] = (int)g_holes[i].extent.width;
        holes[nholes * 4 + 3] = (int)g_holes[i].extent.height;
        nholes++;
    }
    pthread_mutex_unlock(&g_state_mu);

    int src_x0 = 0, src_y0 = 0, src_x1 = fw, src_y1 = fh;
    int dx = 0, dy = 0, dw = sw, dh = sh;
    vk_layout_zoomed_dest(fw, fh, sw, sh, snap_zoom, snap_pan_x, snap_pan_y,
                          &dx, &dy, &dw, &dh, &src_x0, &src_y0, &src_x1, &src_y1);
    vk_store_video_dest(dx, dy, dw, dh, sw, sh);
    f.sx0 = crop_x + src_x0; f.sy0 = crop_y + src_y0;
    f.sx1 = crop_x + src_x1; f.sy1 = crop_y + src_y1;
    f.dx = dx; f.dy = dy; f.dw = dw; f.dh = dh;

    if (snap_vis) {
        // Same mapping as the Vulkan path's cursor spans: the image's
        // top-left (the arrow tip) sits on the cursor point.
        float su0 = fw > 0 ? (float)src_x0 / (float)fw : 0.0f, sv0 = fh > 0 ? (float)src_y0 / (float)fh : 0.0f;
        float su1 = fw > 0 ? (float)src_x1 / (float)fw : 1.0f, sv1 = fh > 0 ? (float)src_y1 / (float)fh : 1.0f;
        float tu = (su1 - su0 > 0.001f) ? (snap_uc - su0) / (su1 - su0) : 0.5f;
        float tv = (sv1 - sv0 > 0.001f) ? (snap_vc - sv0) / (sv1 - sv0) : 0.5f;
        f.cursor_visible = 1;
        f.cursor_x = dx + (int)(tu * dw + 0.5f);
        f.cursor_y = dy + (int)(tv * dh + 0.5f);
    }
    f.holes = holes; f.nholes = nholes;
    if (hud_gles_sync() && hud_place(fw, fh, dx, dy, dw, dh, sw, sh, f.hud))
        f.hud_visible = 1;
    return gles_ov_draw(&f);
}

static int vk_render_aimage(AImage *img) {
    if (!img) return 0;
    if (g_use_gles) return gles_render(img, NULL, 0, 0);
    AHardwareBuffer *ahb = NULL;
    AImageCropRect crop;
    if (AImage_getHardwareBuffer(img, &ahb) != AMEDIA_OK || !ahb) return 0;
    AHardwareBuffer_Desc desc;
    AHardwareBuffer_describe(ahb, &desc);
    if (AImage_getCropRect(img, &crop) != AMEDIA_OK || crop.right <= crop.left || crop.bottom <= crop.top) {
        crop.left = 0; crop.top = 0; crop.right = (int32_t)desc.width; crop.bottom = (int32_t)desc.height;
    }
    vk_yc_frame f = { ahb, (int)desc.width, (int)desc.height, crop.left, crop.top };
    return vk_render_frame_hw(NULL, crop.right - crop.left, crop.bottom - crop.top, &f);
}

static void *vk_render_thread(void *unused) {
    (void)unused;
    VLOGI("render thread started%s", g_use_gles ? " (OpenGL ES)" : "");
    if (g_use_gles && !gles_ov_make_current()) VLOGE("GLES: eglMakeCurrent failed on the render thread");
    int last_fw = 0, last_fh = 0, last_fs = 0;
    int has_last_frame = 0;
    void *last_ahb = NULL;
    int last_ahb_w = 0, last_ahb_h = 0;
    int has_last_ahb = 0;

    while (atomic_load(&g_active)) {
        struct timeval tv = {0, 8000};
        pthread_mutex_lock(&g_mu);
        int64_t pace_wait = pace_take_due();
        int pace_due = g_pend_aimg != NULL;
        pthread_mutex_unlock(&g_mu);
        if (pace_due) tv.tv_usec = 0;
        else if (pace_wait >= 0 && pace_wait < tv.tv_usec) tv.tv_usec = (suseconds_t)pace_wait;
        fd_set fds; FD_ZERO(&fds); FD_SET(g_pipe_r, &fds);
        select(g_pipe_r + 1, &fds, NULL, NULL, &tv);
        if (FD_ISSET(g_pipe_r, &fds)) {
            char tmp[64]; read(g_pipe_r, tmp, sizeof(tmp));
        }
        if (!atomic_load(&g_active)) break;
        if (atomic_load(&g_aimg_release_req)) {
            vk_aimg_drop_all();
            atomic_store(&g_aimg_release_req, 0);
        }
        if (atomic_load(&g_hidden))  continue;

        // Explicit swapchain recreation requested (e.g. after fullscreen entry/exit).
        // GLES: the EGL window surface follows the SurfaceView's size itself.
        if (atomic_exchange(&g_force_recreate, 0) && !g_use_gles) {
            VLOGI("force swapchain recreate requested");
            vk_recreate_swapchain();
            continue;
        }

        int fw = 0, fh = 0, fs = 0;
        int got_frame = 0;
        void *ahb = NULL;
        int ahb_w = 0, ahb_h = 0;
        int got_ahb = 0;
        AImage *aimg = NULL;

        pthread_mutex_lock(&g_mu);
        pace_take_due();
        if (g_pend_aimg) {
            aimg = g_pend_aimg;
            g_pend_aimg = NULL;
        } else if (g_ahb_ready && g_pend_ahb) {
            // Zero-copy path: nothing to copy, just the pointer + size GL
            // already rendered into (see android_vk_try_submit_hwbuffer).
            ahb = g_pend_ahb; ahb_w = g_pend_ahb_w; ahb_h = g_pend_ahb_h;
            got_ahb = 1;
            g_ahb_ready = 0;
        } else if (g_ready && g_buf) {
            fw = g_fw; fh = g_fh; fs = g_fs;
            size_t sz = (size_t)fh * (size_t)fs;
            // Copy straight into Vulkan's mapped staging buffer instead of
            // through an intermediate malloc'd buffer — one fewer full-frame
            // memcpy (~8MB at 1080p, every decoded frame) than the previous
            // g_buf -> frame_buf -> g_stage_ptr double-hop. vk_ensure_staging
            // is cheap (no-op) once sized; it only does real work on the
            // first frame or a resolution change.
            if (g_use_gles) {
                size_t tight = (size_t)fw * 4 * (size_t)fh;
                if (g_gles_cpu_sz < tight) {
                    free(g_gles_cpu);
                    g_gles_cpu = malloc(tight);
                    g_gles_cpu_sz = g_gles_cpu ? tight : 0;
                }
                if (g_gles_cpu) {
                    for (int y = 0; y < fh; y++)
                        memcpy(g_gles_cpu + (size_t)y * fw * 4, g_buf + (size_t)y * (size_t)fs, (size_t)fw * 4);
                    got_frame = 1;
                }
            } else if (g_dev && vk_ensure_staging(sz) && g_stage_ptr) {
                // vk_render_frame's VkBufferImageCopy always describes the
                // staging buffer as tightly packed (bufferRowLength = fw
                // texels) since that's what it uploads to a GPU image with —
                // so any row padding in g_buf (fs > fw*4, e.g. decoder
                // alignment) must be stripped here, not carried into
                // g_stage_ptr, or every row after the first renders shifted.
                size_t tight_row = (size_t)fw * 4;
                if ((size_t)fs == tight_row) {
                    memcpy(g_stage_ptr, g_buf, sz);
                } else {
                    uint8_t *dst = (uint8_t *)g_stage_ptr;
                    for (int y = 0; y < fh; y++)
                        memcpy(dst + (size_t)y * tight_row, g_buf + (size_t)y * (size_t)fs, tight_row);
                }
                got_frame = 1;
            }
            g_ready = 0;
        }
        pthread_mutex_unlock(&g_mu);

        int cursor_dirty = atomic_exchange(&g_cursor_dirty, 0);
        if (atomic_exchange(&g_drop_last_ahb, 0)) {
            has_last_ahb = 0;
            last_ahb = NULL;
        }
        if (atomic_exchange(&g_drop_last_cpu, 0)) {
            has_last_frame = 0;
        }
        if (atomic_exchange(&g_drop_last_aimg, 0)) {
            vk_aimg_set_current(NULL);
        }

        if (aimg || (cursor_dirty && g_cur_aimg)) {
            int is_new = aimg != NULL;
            if (is_new) vk_aimg_set_current(aimg);
            if (vk_render_aimage(g_cur_aimg) && is_new) vk_note_video_frame_rendered();
            continue;
        }

        int is_video_frame = 0;
        if (got_ahb) {
            last_ahb = ahb; last_ahb_w = ahb_w; last_ahb_h = ahb_h;
            has_last_ahb = 1;
            is_video_frame = 1;
        } else if (got_frame) {
            // Remember last frame dimensions for cursor-only redraws.
            last_fw = fw; last_fh = fh; last_fs = fs;
            has_last_frame = 1;
            is_video_frame = 1;
        } else if (cursor_dirty && has_last_ahb) {
            // Cursor moved but no new video frame — re-blit the same
            // imported VkImage from last time with the updated cursor.
            ahb = last_ahb; ahb_w = last_ahb_w; ahb_h = last_ahb_h;
            got_ahb = 1;
        } else if (cursor_dirty && has_last_frame) {
            // Cursor moved but no new video frame — redraw the previous
            // frame, which is still sitting in g_stage_ptr from the last
            // time we wrote it, with the updated cursor. No copy needed.
            fw = last_fw; fh = last_fh; fs = last_fs;
            got_frame = 1;
        }

        if (!got_frame && !got_ahb) continue;

        int ok = g_use_gles ? (got_frame ? gles_render(NULL, g_gles_cpu, fw, fh) : 0)
               : got_ahb ? vk_render_frame_hw(ahb, ahb_w, ahb_h, NULL) : vk_render_frame(fw, fh, fs);
        if (ok && is_video_frame) vk_note_video_frame_rendered();
        else if (ok) g_rendered++;
    }
    if (g_use_gles) {
        // The context is current here: release GL objects on this thread.
        vk_aimg_set_current(NULL);
        gles_ov_destroy();
    }
    VLOGI("render thread exiting");
    return NULL;
}

// ─── cleanup ─────────────────────────────────────────────────────────────────

static void vk_full_cleanup(void) {
    atomic_store(&g_active, 0);
    if (g_pipe_w >= 0) { char c = 0; write(g_pipe_w, &c, 1); }
    if (g_thread) { pthread_join(g_thread, NULL); g_thread = 0; }
    if (g_pipe_r >= 0) { close(g_pipe_r); g_pipe_r = -1; }
    if (g_pipe_w >= 0) { close(g_pipe_w); g_pipe_w = -1; }

    if (g_use_gles) {
        // The render thread released GL on its way out; this covers a
        // thread that never started, and the frame still waiting.
        gles_ov_destroy();
        vk_aimg_set_current(NULL);
        pthread_mutex_lock(&g_mu);
        AImage *pend = g_pend_aimg;
        g_pend_aimg = NULL;
        pace_drop_queued();
        free(g_gles_cpu); g_gles_cpu = NULL; g_gles_cpu_sz = 0;
        free(g_gles_cursor); g_gles_cursor = NULL;
        pthread_mutex_unlock(&g_mu);
        if (pend) AImage_delete(pend);
        g_use_gles = 0;
    }

    if (g_dev) {
        vkDeviceWaitIdle(g_dev);
        vk_aimg_drop_all();
        vk_yc_destroy_all();
        vk_hud_destroy_all();
        cursor_destroy();
        vk_destroy_clear_img();
        if (g_stage_ptr && g_stage_mem) { vkUnmapMemory(g_dev, g_stage_mem); g_stage_ptr = NULL; }
        if (g_stage_buf) { vkDestroyBuffer(g_dev, g_stage_buf, NULL); g_stage_buf = VK_NULL_HANDLE; }
        if (g_stage_mem) { vkFreeMemory(g_dev, g_stage_mem, NULL);   g_stage_mem = VK_NULL_HANDLE; }
        g_stage_sz = 0;
        if (g_tex)     { vkDestroyImage(g_dev, g_tex, NULL);   g_tex     = VK_NULL_HANDLE; }
        if (g_tex_mem) { vkFreeMemory(g_dev, g_tex_mem, NULL); g_tex_mem = VK_NULL_HANDLE; }
        g_tex_w = 0; g_tex_h = 0;
        for (int i = 0; i < HWIMPORT_COUNT; i++) {
            if (g_hwimport_img[i]) { vkDestroyImage(g_dev, g_hwimport_img[i], NULL); g_hwimport_img[i] = VK_NULL_HANDLE; }
            if (g_hwimport_mem[i]) { vkFreeMemory(g_dev, g_hwimport_mem[i], NULL);   g_hwimport_mem[i] = VK_NULL_HANDLE; }
            g_hwimport_src[i] = NULL;
        }
        g_hwimport_w = g_hwimport_h = 0;
        g_hw_import_supported = 0;
        if (g_img_sem) { vkDestroySemaphore(g_dev, g_img_sem, NULL); g_img_sem = VK_NULL_HANDLE; }
        if (g_rnd_sem) { vkDestroySemaphore(g_dev, g_rnd_sem, NULL); g_rnd_sem = VK_NULL_HANDLE; }
        if (g_fence)   { vkDestroyFence(g_dev, g_fence, NULL);       g_fence   = VK_NULL_HANDLE; }
        if (g_cmdbuf && g_cmdpool) {
            vkFreeCommandBuffers(g_dev, g_cmdpool, 1, &g_cmdbuf); g_cmdbuf = VK_NULL_HANDLE;
        }
        if (g_cmdpool) { vkDestroyCommandPool(g_dev, g_cmdpool, NULL); g_cmdpool = VK_NULL_HANDLE; }
        vk_destroy_swapchain();
        if (g_surf) { vkDestroySurfaceKHR(g_inst, g_surf, NULL); g_surf = VK_NULL_HANDLE; }
        vkDestroyDevice(g_dev, NULL); g_dev = VK_NULL_HANDLE;
    }
    if (g_inst) { vkDestroyInstance(g_inst, NULL); g_inst = VK_NULL_HANDLE; }
    g_pdev = VK_NULL_HANDLE;

    pthread_mutex_lock(&g_mu);
    if (g_buf) { free(g_buf); g_buf = NULL; g_buf_sz = 0; }
    g_ready = 0; g_rendered = 0; g_submitted = 0;
    g_pend_ahb = NULL; g_pend_ahb_w = 0; g_pend_ahb_h = 0; g_ahb_ready = 0;
    pthread_mutex_unlock(&g_mu);
    g_fps_n = 0; g_fps_t0 = 0.0; g_stat_fps = 0.0f; g_stat_ready = 0;
}

// ─── Public C API ─────────────────────────────────────────────────────────────

void android_vk_set_jvm(JavaVM *jvm, jobject ctx) {
    if (g_jvm != NULL && g_cls_vob != NULL) return;
    g_jvm = jvm;
    int nd; JNIEnv *env = get_env(&nd);
    if (!env) return;

    jclass cls = (*env)->FindClass(env, "io/usbridge/client/VulkanOverlayBridge");
    if (!cls || (*env)->ExceptionCheck(env)) {
        (*env)->ExceptionClear(env);
        VLOGI("android_vk_set_jvm: fallback to ClassLoader for VulkanOverlayBridge");
        if (ctx) {
            jclass ctxCls = (*env)->GetObjectClass(env, ctx);
            jmethodID getCL = (*env)->GetMethodID(env, ctxCls, "getClassLoader", "()Ljava/lang/ClassLoader;");
            jobject cl = (*env)->CallObjectMethod(env, ctx, getCL);
            jclass clCls = (*env)->FindClass(env, "java/lang/ClassLoader");
            jmethodID loadCls = (*env)->GetMethodID(env, clCls, "loadClass", "(Ljava/lang/String;)Ljava/lang/Class;");
            jstring name = (*env)->NewStringUTF(env, "io.usbridge.client.VulkanOverlayBridge");
            cls = (jclass)(*env)->CallObjectMethod(env, cl, loadCls, name);
            if ((*env)->ExceptionCheck(env)) { (*env)->ExceptionClear(env); cls = NULL; }
            (*env)->DeleteLocalRef(env, name);
        }
    }
    if (cls) {
        g_cls_vob = (jclass)(*env)->NewGlobalRef(env, cls);
        (*env)->DeleteLocalRef(env, cls);
        VLOGI("VulkanOverlayBridge class cached");
    } else {
        VLOGE("could not find VulkanOverlayBridge class");
    }

    // Cache HapticBridge for short-tap haptic feedback.
    jclass hcls = (*env)->FindClass(env, "io/usbridge/client/HapticBridge");
    if (hcls && !(*env)->ExceptionCheck(env)) {
        g_cls_haptic = (jclass)(*env)->NewGlobalRef(env, hcls);
        (*env)->DeleteLocalRef(env, hcls);
        VLOGI("HapticBridge class cached");
    } else {
        (*env)->ExceptionClear(env);
        VLOGE("could not find HapticBridge class");
    }

    detach_env(nd);
}

// android_haptic_short_tap calls HapticBridge.triggerShortTap() on the Kotlin side
// to produce a brief vibration (~30 ms). Used to confirm the RMB long-press threshold.
void android_haptic_short_tap(void) {
    if (!g_cls_haptic) return;
    int nd; JNIEnv *env = get_env(&nd);
    if (!env) return;
    jmethodID mid = (*env)->GetStaticMethodID(env, g_cls_haptic, "triggerShortTap", "()V");
    if (mid && !(*env)->ExceptionCheck(env)) {
        (*env)->CallStaticVoidMethod(env, g_cls_haptic, mid);
        if ((*env)->ExceptionCheck(env)) (*env)->ExceptionClear(env);
    } else {
        (*env)->ExceptionClear(env);
    }
    detach_env(nd);
}

int android_vk_is_active(void)  { return atomic_load(&g_active); }
int android_vk_is_hidden(void)  { return atomic_load(&g_hidden); }

// android_vk_force_recreate_swapchain asks the render thread to recreate the
// Vulkan swapchain on the next iteration. Call this after a fullscreen
// transition to pick up the new surface dimensions immediately instead of
// waiting for the proactive size-change detection in vk_render_frame.
void android_vk_force_recreate_swapchain(void) {
    if (!atomic_load(&g_active)) return;
    atomic_store(&g_force_recreate, 1);
    if (g_pipe_w >= 0) { char c = 1; write(g_pipe_w, &c, 1); }
}

// android_vk_create: request SurfaceView overlay from Kotlin, wait for surface,
// then initialise Vulkan renderer. Returns 1 on success.
int android_vk_create(int x, int y, int w, int h) {
    if (atomic_load(&g_active)) vk_full_cleanup();

    // Holes belong to the surface they were cut for; Go resets its own copy
    // on create and would never push an empty list over a stale one (a
    // dialog closed while the stream restarted left its hole, black).
    pthread_mutex_lock(&g_state_mu);
    g_hole_count = 0;
    pthread_mutex_unlock(&g_state_mu);

    if (!java_create_overlay(x, y, w, h)) {
        VLOGE("android_vk_create: java_create_overlay failed"); return 0;
    }

    // Poll for the SurfaceView surface (created on Kotlin UI thread, typically <100 ms).
    ANativeWindow *win = NULL;
    for (int i = 0; i < 300; i++) {   // up to 3 s
        win = java_get_pending_window();
        if (win) break;
        struct timespec ts = {0, 10000000}; // 10 ms
        nanosleep(&ts, NULL);
    }
    if (!win) {
        VLOGE("android_vk_create: timeout waiting for SurfaceView surface");
        java_destroy_overlay();
        return 0;
    }

    // Reset viewport to full frame and hide cursor for the new session.
    atomic_store(&g_vp_u0_fp, 0); atomic_store(&g_vp_v0_fp, 0);
    atomic_store(&g_vp_u1_fp, 65536); atomic_store(&g_vp_v1_fp, 65536);
    atomic_store(&g_blit_pan_x, 0); atomic_store(&g_blit_pan_y, 0);
    atomic_store(&g_blit_zoom_fp, 65536);
    atomic_store(&g_cursor_visible, 0);
    atomic_store(&g_cursor_uc_fp, 32768); atomic_store(&g_cursor_vc_fp, 32768);
    atomic_store(&g_cursor_dirty, 0);

    g_use_gles = 0;
    // `adb shell setprop debug.usbridge.gles 1` takes the GLES path on a
    // Vulkan device too, to test it.
    char force_gles[PROP_VALUE_MAX] = {0};
    __system_property_get("debug.usbridge.gles", force_gles);
    if (force_gles[0] == '1' || !vk_create_instance() || !vk_select_device() || !vk_create_device()) {
        // No usable Vulkan (e.g. PowerVR GE8320): draw through OpenGL ES on
        // the same SurfaceView instead (gles_overlay_android.c).
        if (g_dev)  { vkDestroyDevice(g_dev, NULL);    g_dev  = VK_NULL_HANDLE; }
        if (g_inst) { vkDestroyInstance(g_inst, NULL); g_inst = VK_NULL_HANDLE; }
        g_pdev = VK_NULL_HANDLE;
        if (!gles_ov_init(win)) { VLOGE("no Vulkan and no OpenGL ES overlay"); goto fail; }
        g_use_gles = 1;
        ANativeWindow_release(win); win = NULL;
        VLOGI("no Vulkan device: using the OpenGL ES overlay");
        goto start_render_thread;
    }
    // Cursor pixels are uploaded from Go via android_vk_set_cursor_pixels()
    // after create returns, so no pre-render needed here.

    {
        VkAndroidSurfaceCreateInfoKHR sci = { VK_STRUCTURE_TYPE_ANDROID_SURFACE_CREATE_INFO_KHR };
        sci.window = win;
        if (vkCreateAndroidSurfaceKHR(g_inst, &sci, NULL, &g_surf) != VK_SUCCESS) {
            VLOGE("vkCreateAndroidSurfaceKHR failed"); goto fail;
        }
    }
    ANativeWindow_release(win); win = NULL;

    atomic_store(&g_dst_w, w); atomic_store(&g_dst_h, h);
    // Use 0,0 so vk_create_swapchain reads the actual surface size from caps.currentExtent.
    if (!vk_create_swapchain(0, 0)) { VLOGE("swapchain creation failed"); goto fail; }

    {
        VkCommandPoolCreateInfo cpci = { VK_STRUCTURE_TYPE_COMMAND_POOL_CREATE_INFO };
        cpci.queueFamilyIndex = g_qfam;
        cpci.flags            = VK_COMMAND_POOL_CREATE_RESET_COMMAND_BUFFER_BIT;
        if (vkCreateCommandPool(g_dev, &cpci, NULL, &g_cmdpool) != VK_SUCCESS) goto fail;
        VkCommandBufferAllocateInfo cbai = { VK_STRUCTURE_TYPE_COMMAND_BUFFER_ALLOCATE_INFO };
        cbai.commandPool        = g_cmdpool;
        cbai.level              = VK_COMMAND_BUFFER_LEVEL_PRIMARY;
        cbai.commandBufferCount = 1;
        if (vkAllocateCommandBuffers(g_dev, &cbai, &g_cmdbuf) != VK_SUCCESS) goto fail;
    }
    {
        VkSemaphoreCreateInfo semi = { VK_STRUCTURE_TYPE_SEMAPHORE_CREATE_INFO };
        VkFenceCreateInfo     fci  = { VK_STRUCTURE_TYPE_FENCE_CREATE_INFO };
        fci.flags = VK_FENCE_CREATE_SIGNALED_BIT;
        if (vkCreateSemaphore(g_dev, &semi, NULL, &g_img_sem) != VK_SUCCESS) goto fail;
        if (vkCreateSemaphore(g_dev, &semi, NULL, &g_rnd_sem) != VK_SUCCESS) goto fail;
        if (vkCreateFence(g_dev, &fci, NULL, &g_fence)        != VK_SUCCESS) goto fail;
    }

start_render_thread:
    {
        int fds[2];
        if (pipe(fds) != 0) { VLOGE("pipe failed"); goto fail; }
        g_pipe_r = fds[0]; g_pipe_w = fds[1];
    }

    atomic_store(&g_hidden, 0);
    atomic_store(&g_force_recreate, 0);
    g_submitted = 0; g_rendered = 0; g_ready = 0;
    atomic_store(&g_active, 1);

    if (pthread_create(&g_thread, NULL, vk_render_thread, NULL) != 0) {
        atomic_store(&g_active, 0); VLOGE("pthread_create failed"); goto fail;
    }

    VLOGI("Vulkan/Android renderer ready — rect=(%d,%d,%dx%d)", x, y, w, h);
    return 1;

fail:
    if (win) ANativeWindow_release(win);
    vk_full_cleanup();
    java_destroy_overlay();
    return 0;
}

int android_vk_try_submit(uint8_t *rgba, int width, int height, int stride) {
    if (!atomic_load(&g_active)) return 0;
    size_t sz = (size_t)height * (size_t)stride;
    pthread_mutex_lock(&g_mu);
    if (!atomic_load(&g_active)) {
        pthread_mutex_unlock(&g_mu);
        return 0;
    }
    if (!g_buf || g_buf_sz < sz) {
        free(g_buf);
        g_buf    = malloc(sz);
        g_buf_sz = g_buf ? sz : 0;
    }
    if (g_buf) {
        memcpy(g_buf, rgba, sz);
        g_fw = width; g_fh = height; g_fs = stride;
        g_ready = 1; g_submitted++;
        // Drop any leftover zero-copy frame so the render thread cannot
        // keep presenting AHB (no HUD) interleaved with this CPU frame.
        g_ahb_ready = 0;
        g_pend_ahb = NULL;
        atomic_store(&g_drop_last_ahb, 1);
        atomic_store(&g_drop_last_aimg, 1);
    }
    pthread_mutex_unlock(&g_mu);
    if (g_pipe_w >= 0) { char c = 1; write(g_pipe_w, &c, 1); }
    return 1;
}

// android_vk_try_submit_hwbuffer is the zero-copy counterpart of
// android_vk_try_submit: ahb is an AHardwareBuffer* (see
// gl_video_impl_android.c's android_gl_get_frame_hwbuffer) that GL has
// already rendered this frame into and glFinish()'d — there is nothing to
// copy, just the pointer and its dimensions to hand to the render thread.
// Returns 0 (falls back to the caller using android_vk_try_submit instead)
// if this device/driver doesn't support AHardwareBuffer import at all.
int android_vk_try_submit_hwbuffer(void *ahb, int width, int height) {
    if (!atomic_load(&g_active) || !g_hw_import_supported) return 0;
    pthread_mutex_lock(&g_mu);
    if (!atomic_load(&g_active)) {
        pthread_mutex_unlock(&g_mu);
        return 0;
    }
    g_pend_ahb = ahb; g_pend_ahb_w = width; g_pend_ahb_h = height;
    g_ahb_ready = 1; g_submitted++;
    g_ready = 0;
    atomic_store(&g_drop_last_cpu, 1);
    atomic_store(&g_drop_last_aimg, 1);
    pthread_mutex_unlock(&g_mu);
    if (g_pipe_w >= 0) { char c = 1; write(g_pipe_w, &c, 1); }
    return 1;
}

// android_vk_hwbuffer_supported reports whether this device/driver has every
// extension AHardwareBuffer zero-copy import needs (see vk_create_device).
// Callers pick android_vk_try_submit_hwbuffer vs android_vk_try_submit per
// frame (HUD-on forces the CPU path). Each submit clears the other path's
// pending frame so a mid-session switch does not flicker.
int android_vk_hwbuffer_supported(void) {
    return g_hw_import_supported;
}

// android_vk_aimage_supported reports whether decoder images can be drawn
// directly (android_vk_try_submit_aimage).
int android_vk_aimage_supported(void) {
    return atomic_load(&g_active) && (g_ycbcr_supported || (g_use_gles && gles_ov_images_supported()));
}

// android_vk_try_submit_aimage hands a decoder image (AImageReader, already
// filled) to the render thread, which takes ownership and deletes it. A
// frame still waiting is replaced (and deleted) so only the newest is shown.
// Returns 0 without taking img if the path is unavailable.
int android_vk_try_submit_aimage(void *img) {
    if (!img || !atomic_load(&g_active) || !android_vk_aimage_supported()) return 0;
    pthread_mutex_lock(&g_mu);
    if (!atomic_load(&g_active)) {
        pthread_mutex_unlock(&g_mu);
        return 0;
    }
    if (g_pace_on) {
        int64_t ts_ns = 0, now = pace_now_us();
        AImage_getTimestamp((AImage *)img, &ts_ns);
        int64_t at = FramePacerTarget(&g_pacer, ts_ns / 1000, now);
        int64_t hold = at - PACE_RENDER_LEAD_US - now, avg = atomic_load(&g_pace_hold_us);
        atomic_store(&g_pace_hold_us, avg + ((hold > 0 ? hold : 0) - avg) / 32);
        if (hold > 0) {
            AImage *dropped = NULL;
            if (g_pace_n == PACE_QUEUE_MAX) { // a burst outran the schedule: lose the oldest
                dropped = g_pace_img[0];
                g_pace_n--;
                memmove(g_pace_img, g_pace_img + 1, (size_t)g_pace_n * sizeof(g_pace_img[0]));
                memmove(g_pace_at, g_pace_at + 1, (size_t)g_pace_n * sizeof(g_pace_at[0]));
            }
            g_pace_img[g_pace_n] = (AImage *)img;
            g_pace_at[g_pace_n] = at;
            g_pace_n++;
            g_submitted++;
            pthread_mutex_unlock(&g_mu);
            if (dropped) {
                AImage_delete(dropped);
                vlat_note_dropped();
            }
            if (g_pipe_w >= 0) { char c = 1; write(g_pipe_w, &c, 1); }
            return 1;
        }
        // Late already: it goes now, and anything still queued is older.
        pace_drop_queued();
    }
    AImage *old = g_pend_aimg;
    g_pend_aimg = (AImage *)img;
    g_submitted++;
    g_ready = 0;
    g_ahb_ready = 0;
    g_pend_ahb = NULL;
    atomic_store(&g_drop_last_cpu, 1);
    atomic_store(&g_drop_last_ahb, 1);
    pthread_mutex_unlock(&g_mu);
    if (old) {
        AImage_delete(old); // replaced before it was shown
        vlat_note_dropped();
    }
    if (g_pipe_w >= 0) { char c = 1; write(g_pipe_w, &c, 1); }
    return 1;
}

// android_vk_pace_hold_us: the average hold the jitter buffer adds, 0 when off.
uint64_t android_vk_pace_hold_us(void) {
    return g_pace_on ? (uint64_t)atomic_load(&g_pace_hold_us) : 0;
}

// android_vk_set_pacing sets the display-side jitter buffer for the next
// stream (see g_pace_img): 0 off, else the latency/smoothness slider level
// (FramePacer.h's FRAME_PACER_LEVEL).
void android_vk_set_pacing(int level) {
    pthread_mutex_lock(&g_mu);
    g_pace_on = level > 0;
    atomic_store(&g_pace_hold_us, 0);
    FramePacerConfigure(&g_pacer, level);
    pace_drop_queued();
    pthread_mutex_unlock(&g_mu);
}

// android_vk_release_aimages returns every decoder image the renderer holds
// (and its imports of the reader's buffers). Call before deleting the
// AImageReader they came from.
void android_vk_release_aimages(void) {
    if (!atomic_load(&g_active)) return; // vk_full_cleanup already dropped them
    atomic_store(&g_aimg_release_req, 1);
    if (g_pipe_w >= 0) { char c = 1; write(g_pipe_w, &c, 1); }
    for (int i = 0; i < 300 && atomic_load(&g_aimg_release_req) && atomic_load(&g_active); i++) {
        struct timespec ts = {0, 5000000}; // 5 ms
        nanosleep(&ts, NULL);
    }
    if (atomic_load(&g_aimg_release_req) && atomic_load(&g_active))
        VLOGE("android_vk_release_aimages: render thread did not answer in 1.5 s");
}

void android_vk_update_rect(int x, int y, int w, int h) {
    if (!atomic_load(&g_active)) return;
    atomic_store(&g_dst_w, w);
    atomic_store(&g_dst_h, h);
    java_set_rect(x, y, w, h);
}

void android_vk_set_hidden(int hidden) {
    if (!atomic_load(&g_active)) return;
    int want = hidden ? 1 : 0;
    int prev = atomic_exchange(&g_hidden, want);
    if (prev == want) return;
    java_set_visible(!want);
    // Samsung may have dropped the buffer while "hidden"; rebuild on show.
    if (!want) atomic_store(&g_force_recreate, 1);
}

// android_vk_set_holes replaces the menu holes; xywh holds n rects as
// x,y,w,h quadruples in SurfaceView pixels. n == 0 clears every hole. Wakes
// the render thread so a paused stream re-presents its last frame with them.
void android_vk_set_holes(const int *xywh, int n) {
    if (n < 0) n = 0;
    if (n > VK_MAX_HOLES) n = VK_MAX_HOLES;
    pthread_mutex_lock(&g_state_mu);
    int k = 0;
    for (int i = 0; i < n; i++) {
        const int *h = xywh + i * 4;
        if (h[2] <= 0 || h[3] <= 0) continue;
        g_holes[k].offset = (VkOffset2D){h[0], h[1]};
        g_holes[k].extent = (VkExtent2D){(uint32_t)h[2], (uint32_t)h[3]};
        k++;
    }
    g_hole_count = k;
    pthread_mutex_unlock(&g_state_mu);
    atomic_store(&g_cursor_dirty, 1);
    if (g_pipe_w >= 0) { char c = 1; write(g_pipe_w, &c, 1); }
}

void android_vk_destroy(void) {
    if (!atomic_load(&g_active)) return;
    VLOGI("Vulkan/Android renderer destroyed — rendered=%lld submitted=%lld",
          (long long)g_rendered, (long long)g_submitted);
    vk_full_cleanup();
    java_destroy_overlay();
}

// android_vk_set_align_bottom controls vertical alignment of the fitted video rect.
// 0 = center (default); 1 = bottom-align (use while system IME is open).
void android_vk_set_align_bottom(int bottom) {
    atomic_store(&g_v_align, bottom ? 1 : 0);
}

// android_vk_set_align_top: flush-fit to the top of the SurfaceView (under
// special keys). 0 = center; non-zero = top-align.
void android_vk_set_align_top(int top) {
    atomic_store(&g_v_align, top ? 2 : 0);
}

// android_vk_set_viewport sets the visible UV sub-rect of the frame (0..1 per axis).
// u0=0,v0=0,u1=1,v1=1 shows the full frame (default).
void android_vk_set_viewport(float u0, float v0, float u1, float v1) {
    pthread_mutex_lock(&g_state_mu);
    atomic_store(&g_vp_u0_fp, (int)(u0 * 65536));
    atomic_store(&g_vp_v0_fp, (int)(v0 * 65536));
    atomic_store(&g_vp_u1_fp, (int)(u1 * 65536));
    atomic_store(&g_vp_v1_fp, (int)(v1 * 65536));
    pthread_mutex_unlock(&g_state_mu);
}

// android_vk_set_cursor sets the virtual cursor position (uc, vc in frame UV)
// and visibility. The cursor is drawn on top of the video each frame.
void android_vk_set_cursor(float uc, float vc, int visible) {
    pthread_mutex_lock(&g_state_mu);
    atomic_store(&g_cursor_uc_fp, (int)(uc * 65536));
    atomic_store(&g_cursor_vc_fp, (int)(vc * 65536));
    atomic_store(&g_cursor_visible, visible ? 1 : 0);
    pthread_mutex_unlock(&g_state_mu);
    // Wake render thread to redraw cursor immediately without waiting for next video frame.
    atomic_store(&g_cursor_dirty, 1);
    if (g_pipe_w >= 0) { char c = 1; write(g_pipe_w, &c, 1); }
}

// android_vk_set_viewport_and_cursor updates viewport, uniform zoom, letterbox
// pan and cursor in one mutex-protected write. Zoom scales the aspect-fit dest
// (RustDesk-style); UV is kept for compatibility but layout uses full frame.
void android_vk_set_viewport_and_cursor(float u0, float v0, float u1, float v1,
                                         float uc, float vc, int visible,
                                         float blit_pan_x, float blit_pan_y,
                                         float zoom_scale) {
    if (zoom_scale < 1.0f) zoom_scale = 1.0f;
    if (zoom_scale > 8.0f) zoom_scale = 8.0f;
    pthread_mutex_lock(&g_state_mu);
    atomic_store(&g_vp_u0_fp, (int)(u0 * 65536));
    atomic_store(&g_vp_v0_fp, (int)(v0 * 65536));
    atomic_store(&g_vp_u1_fp, (int)(u1 * 65536));
    atomic_store(&g_vp_v1_fp, (int)(v1 * 65536));
    atomic_store(&g_cursor_uc_fp, (int)(uc * 65536));
    atomic_store(&g_cursor_vc_fp, (int)(vc * 65536));
    atomic_store(&g_cursor_visible, visible ? 1 : 0);
    atomic_store(&g_blit_pan_x, (int)(blit_pan_x + (blit_pan_x >= 0 ? 0.5f : -0.5f)));
    atomic_store(&g_blit_pan_y, (int)(blit_pan_y + (blit_pan_y >= 0 ? 0.5f : -0.5f)));
    atomic_store(&g_blit_zoom_fp, (int)(zoom_scale * 65536.0f + 0.5f));
    pthread_mutex_unlock(&g_state_mu);
    atomic_store(&g_cursor_dirty, 1);
    if (g_pipe_w >= 0) { char c = 1; write(g_pipe_w, &c, 1); }
}

// android_vk_set_cursor_scale reinitialises the cursor pixel buffer at the given
// integer scale factor (1=18×24 px, 2=36×48 px, 3=54×72 px, 4=72×96 px).
// Safe to call while the render thread is running: waits for GPU idle first.
void android_vk_set_cursor_scale(int scale) {
    if (scale < 1) scale = 1;
    if (scale > 4) scale = 4;
    if (!g_dev || !atomic_load(&g_active)) return;
    int was_visible = atomic_exchange(&g_cursor_visible, 0);
    vkDeviceWaitIdle(g_dev);
    cursor_init(scale);
    atomic_store(&g_cursor_visible, was_visible);
}

// android_vk_set_cursor_pixels uploads a pre-rendered RGBA cursor bitmap from
// Go. Replaces the current cursor buffer. Thread-safe: waits for GPU idle.
// src_rgba: packed RGBA bytes, 4 bytes/pixel, row-major, w×h pixels.
void android_vk_set_cursor_pixels(const uint8_t *src_rgba, int w, int h) {
    if (g_use_gles && src_rgba && w > 0 && h > 0) {
        size_t n = (size_t)w * (size_t)h * 4;
        uint8_t *copy = malloc(n);
        if (!copy) return;
        memcpy(copy, src_rgba, n);
        pthread_mutex_lock(&g_mu);
        free(g_gles_cursor);
        g_gles_cursor = copy; g_gles_cursor_w = w; g_gles_cursor_h = h;
        pthread_mutex_unlock(&g_mu);
        atomic_store(&g_gles_cursor_dirty, 1);
        atomic_store(&g_cursor_dirty, 1);
        if (g_pipe_w >= 0) { char c = 1; write(g_pipe_w, &c, 1); }
        return;
    }
    if (!g_dev || !g_pdev || !src_rgba || w <= 0 || h <= 0) return;

    int was_visible = atomic_exchange(&g_cursor_visible, 0);
    vkDeviceWaitIdle(g_dev);
    cursor_destroy();

    size_t total_pix = 0;
    for (int y = 0; y < h; y++)
        for (int x = 0; x < w; x++)
            if (src_rgba[(y * w + x) * 4 + 3] > 0) total_pix++;
    if (!total_pix) { atomic_store(&g_cursor_visible, was_visible); return; }

    VkBufferCreateInfo bci = {VK_STRUCTURE_TYPE_BUFFER_CREATE_INFO};
    bci.size = total_pix * 4;
    bci.usage = VK_BUFFER_USAGE_TRANSFER_SRC_BIT;
    bci.sharingMode = VK_SHARING_MODE_EXCLUSIVE;
    VkBuffer buf = VK_NULL_HANDLE;
    if (vkCreateBuffer(g_dev, &bci, NULL, &buf) != VK_SUCCESS) {
        atomic_store(&g_cursor_visible, was_visible); return;
    }
    VkMemoryRequirements mr;
    vkGetBufferMemoryRequirements(g_dev, buf, &mr);
    VkPhysicalDeviceMemoryProperties mp;
    vkGetPhysicalDeviceMemoryProperties(g_pdev, &mp);
    uint32_t mi = vk_find_mem(&mp, mr.memoryTypeBits,
        VK_MEMORY_PROPERTY_HOST_VISIBLE_BIT | VK_MEMORY_PROPERTY_HOST_COHERENT_BIT);
    if (mi == UINT32_MAX) {
        vkDestroyBuffer(g_dev, buf, NULL);
        atomic_store(&g_cursor_visible, was_visible); return;
    }
    VkMemoryAllocateInfo mai = {VK_STRUCTURE_TYPE_MEMORY_ALLOCATE_INFO};
    mai.allocationSize = mr.size; mai.memoryTypeIndex = mi;
    VkDeviceMemory mem = VK_NULL_HANDLE;
    if (vkAllocateMemory(g_dev, &mai, NULL, &mem) != VK_SUCCESS) {
        vkDestroyBuffer(g_dev, buf, NULL);
        atomic_store(&g_cursor_visible, was_visible); return;
    }
    vkBindBufferMemory(g_dev, buf, mem, 0);
    void *ptr = NULL;
    vkMapMemory(g_dev, mem, 0, VK_WHOLE_SIZE, 0, &ptr);
    if (!ptr) {
        vkFreeMemory(g_dev, mem, NULL); vkDestroyBuffer(g_dev, buf, NULL);
        atomic_store(&g_cursor_visible, was_visible); return;
    }

    uint8_t *dst = (uint8_t *)ptr;
    uint32_t buf_off = 0;
    int nspans = 0;
    int is_bgra = (g_swap_fmt == VK_FORMAT_B8G8R8A8_UNORM ||
                   g_swap_fmt == VK_FORMAT_B8G8R8A8_SRGB  ||
                   g_swap_fmt == VK_FORMAT_B8G8R8A8_SNORM);

    for (int y = 0; y < h && nspans < MAX_CURSOR_SPANS; y++) {
        int run_start = -1;
        for (int x = 0; x <= w; x++) {
            int opaque = (x < w) && (src_rgba[(y * w + x) * 4 + 3] > 0);
            if (!opaque && run_start >= 0) {
                g_cursor_spans[nspans].rel_x   = run_start;
                g_cursor_spans[nspans].rel_y   = y;
                g_cursor_spans[nspans].width   = x - run_start;
                g_cursor_spans[nspans].buf_off = buf_off;
                for (int px = run_start; px < x; px++) {
                    const uint8_t *p = src_rgba + (y * w + px) * 4;
                    dst[buf_off++] = is_bgra ? p[2] : p[0]; // R or B
                    dst[buf_off++] = p[1];                   // G
                    dst[buf_off++] = is_bgra ? p[0] : p[2]; // B or R
                    dst[buf_off++] = p[3];                   // A
                }
                nspans++;
                run_start = -1;
            } else if (opaque && run_start < 0) {
                run_start = x;
            }
        }
    }

    g_cursor_vk_buf = buf;
    g_cursor_vk_mem = mem;
    g_cursor_vk_ptr = ptr;
    g_cursor_px_w   = w;
    g_cursor_px_h   = h;
    g_cursor_nspans = nspans;
    atomic_store(&g_cursor_visible, was_visible);
    VLOGI("cursor pixels: %dx%d spans=%d buf=%u bytes", w, h, nspans, buf_off);
}

// android_vk_get_stats returns the Vulkan render-thread FPS (2-second window),
// plus cumulative rendered/submitted frame counts. fps_ready is 1 once the
// first 2-second window completes.
void android_vk_get_stats(float *fps, int *fps_ready,
                           long long *rendered, long long *submitted) {
    if (fps)       *fps       = g_stat_fps;
    if (fps_ready) *fps_ready = g_stat_ready;
    if (rendered)  *rendered  = g_rendered;
    if (submitted) *submitted = g_submitted;
}

// android_vk_get_gpu_name: see vk_video_impl_windows.c's identical function
// for the rationale -- copies the active Vulkan physical device's name into
// out, empty when no device has been selected yet this session.
void android_vk_get_gpu_name(char *out, int out_len) {
    if (g_use_gles) { gles_ov_gpu_name(out, out_len); return; }
    if (!out || out_len <= 0) return;
    out[0] = '\0';
    if (g_pdev == VK_NULL_HANDLE) return;
    VkPhysicalDeviceProperties pr;
    vkGetPhysicalDeviceProperties(g_pdev, &pr);
    snprintf(out, out_len, "%s", pr.deviceName);
}

void android_vk_get_video_dest(int *dx, int *dy, int *dw, int *dh, int *sw, int *sh) {
    if (dx) *dx = atomic_load(&g_video_dx);
    if (dy) *dy = atomic_load(&g_video_dy);
    if (dw) *dw = atomic_load(&g_video_dw);
    if (dh) *dh = atomic_load(&g_video_dh);
    if (sw) *sw = atomic_load(&g_video_sw);
    if (sh) *sh = atomic_load(&g_video_sh);
}

#endif // __ANDROID__
