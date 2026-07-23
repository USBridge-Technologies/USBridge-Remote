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
#include <jni.h>
#include <pthread.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include <stdio.h>
#include <stdatomic.h>
#include <time.h>
#include <unistd.h>

#define VTAG "USBridge/VulkanVideo"
#define VLOGI(...) __android_log_print(ANDROID_LOG_INFO,  VTAG, __VA_ARGS__)
#define VLOGE(...) __android_log_print(ANDROID_LOG_ERROR, VTAG, __VA_ARGS__)

#include "vk_postprocess_spv.h"

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
// Mutually exclusive with g_buf/g_ready above in practice (dr_submit picks
// one path or the other for a whole session), guarded by the same g_mu.
static void            *g_pend_ahb = NULL;
static int              g_pend_ahb_w = 0, g_pend_ahb_h = 0;
static volatile int     g_ahb_ready  = 0;

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

// When 1, the fitted video rect is bottom-aligned in the swapchain (dy = sh - dh)
// instead of centered (dy = (sh - dh) / 2). Set while the system IME is open so
// the video sits flush against the keyboard panel with no black gap below.
static atomic_int g_align_bottom;

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

// ─── Post-processing (Vulkan compute: denoise → sharpen → temporal → grade) ──
// Off by default; enabled and tuned from the "Vulkan" popup in the Fyne UI
// (see android_vk_set_postprocess). When disabled the renderer takes the
// exact original blit-only path below — zero risk, zero extra cost.
typedef struct {
    float sharpen;
    float denoise;
    float temporal;
    float gamma;
    float contrast;
    float saturation;
} PPParams;

typedef struct {
    float   sharpen;
    float   denoise;
    float   temporal;
    float   gamma;
    float   contrast;
    float   saturation;
    int32_t enabled;
    int32_t width;
    int32_t height;
    int32_t radius;
} PPPushConstants;

static atomic_int       g_pp_enabled;
static atomic_int       g_pp_primed; // 0 = force temporal=0 next dispatch (history invalid/stale)
static pthread_mutex_t  g_pp_mu = PTHREAD_MUTEX_INITIALIZER;
static PPParams         g_pp_params = {0.35f, 0.35f, 0.5f, 1.0f, 1.0f, 1.0f};

static int              g_pp_compute_capable = 0; // graphics queue family also supports VK_QUEUE_COMPUTE_BIT

static VkSampler             g_pp_sampler  = VK_NULL_HANDLE;
static VkShaderModule         g_pp_shader   = VK_NULL_HANDLE;
static VkDescriptorSetLayout  g_pp_dsl      = VK_NULL_HANDLE;
static VkPipelineLayout       g_pp_playout  = VK_NULL_HANDLE;
static VkPipeline             g_pp_pipeline = VK_NULL_HANDLE;
static VkDescriptorPool       g_pp_dpool    = VK_NULL_HANDLE;
static VkDescriptorSet        g_pp_dset     = VK_NULL_HANDLE;
static VkImageView            g_pp_dset_bound_view = VK_NULL_HANDLE;
static long long              g_pp_dispatch_count = 0; // diagnostic: proves the compute pass actually ran

// History image: doubles as this frame's post-processed output (blit source)
// and the temporal reference for next frame. g_pp_hist_layout tracks its true
// current layout across frames (unlike g_tex/hwimport source images, its
// contents are deliberately persisted, so we can't use the VK_IMAGE_LAYOUT_
// UNDEFINED discard trick used elsewhere in this file).
static VkImage        g_pp_hist_img = VK_NULL_HANDLE;
static VkDeviceMemory g_pp_hist_mem = VK_NULL_HANDLE;
static VkImageView    g_pp_hist_view = VK_NULL_HANDLE;
static int            g_pp_hist_w = 0, g_pp_hist_h = 0;
static VkImageLayout  g_pp_hist_layout = VK_IMAGE_LAYOUT_UNDEFINED;

// Persistent sampled VkImageView for g_tex (CPU staging path). Recreated
// alongside g_tex in vk_ensure_tex.
static VkImageView g_tex_view = VK_NULL_HANDLE;

// Persistent sampled VkImageViews for the two AHardwareBuffer-backed images
// (zero-copy path), one per hwimport_get_or_create slot.
static VkImageView g_hwimport_view[HWIMPORT_COUNT] = {VK_NULL_HANDLE, VK_NULL_HANDLE};

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
    g_pp_compute_capable = 0;
    for (uint32_t j = 0; j < qn; j++)
        if (qp[j].queueFlags & VK_QUEUE_GRAPHICS_BIT) {
            g_qfam = j;
            g_pp_compute_capable = (qp[j].queueFlags & VK_QUEUE_COMPUTE_BIT) != 0;
            break;
        }
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

    VkDeviceCreateInfo dci = { VK_STRUCTURE_TYPE_DEVICE_CREATE_INFO };
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
            hwbuf_ok = 0;
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
    sci.compositeAlpha   = VK_COMPOSITE_ALPHA_OPAQUE_BIT_KHR;
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

static void vk_destroy_swapchain(void) {
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
        if (g_tex_view) { vkDestroyImageView(g_dev, g_tex_view, NULL); g_tex_view = VK_NULL_HANDLE; }
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
    // SAMPLED_BIT is only needed by the post-processing compute pass, but it's
    // always cheap to request so vk_pp_process can read g_tex as-is whenever
    // the user turns the Vulkan popup's post-processing toggle on mid-session.
    ici.usage       = VK_IMAGE_USAGE_TRANSFER_DST_BIT | VK_IMAGE_USAGE_TRANSFER_SRC_BIT | VK_IMAGE_USAGE_SAMPLED_BIT;
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

    VkImageViewCreateInfo vci = { VK_STRUCTURE_TYPE_IMAGE_VIEW_CREATE_INFO };
    vci.image    = g_tex;
    vci.viewType = VK_IMAGE_VIEW_TYPE_2D;
    vci.format   = VK_FORMAT_R8G8B8A8_UNORM;
    vci.subresourceRange.aspectMask = VK_IMAGE_ASPECT_COLOR_BIT;
    vci.subresourceRange.levelCount = 1;
    vci.subresourceRange.layerCount = 1;
    if (vkCreateImageView(g_dev, &vci, NULL, &g_tex_view) != VK_SUCCESS) return 0;

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

// ─── Post-processing pipeline setup ──────────────────────────────────────────

// vk_pp_ensure_pipeline lazily creates the compute pipeline the first time
// post-processing is used, and is a no-op on every call after that. Never
// touches a command buffer -- safe to call before deciding which render path
// (processed vs. plain blit) a frame will take.
static int vk_pp_ensure_pipeline(void) {
    if (g_pp_pipeline != VK_NULL_HANDLE) return 1;
    if (!g_dev || !g_pp_compute_capable) return 0;

    VkSamplerCreateInfo sci = { VK_STRUCTURE_TYPE_SAMPLER_CREATE_INFO };
    sci.magFilter = VK_FILTER_NEAREST;
    sci.minFilter = VK_FILTER_NEAREST;
    sci.addressModeU = VK_SAMPLER_ADDRESS_MODE_CLAMP_TO_EDGE;
    sci.addressModeV = VK_SAMPLER_ADDRESS_MODE_CLAMP_TO_EDGE;
    sci.addressModeW = VK_SAMPLER_ADDRESS_MODE_CLAMP_TO_EDGE;
    sci.mipmapMode = VK_SAMPLER_MIPMAP_MODE_NEAREST;
    if (vkCreateSampler(g_dev, &sci, NULL, &g_pp_sampler) != VK_SUCCESS) {
        VLOGE("pp: vkCreateSampler failed");
        goto fail;
    }

    VkShaderModuleCreateInfo smci = { VK_STRUCTURE_TYPE_SHADER_MODULE_CREATE_INFO };
    smci.codeSize = vk_postprocess_spv_len;
    smci.pCode    = vk_postprocess_spv;
    if (vkCreateShaderModule(g_dev, &smci, NULL, &g_pp_shader) != VK_SUCCESS) {
        VLOGE("pp: vkCreateShaderModule failed");
        goto fail;
    }

    VkDescriptorSetLayoutBinding bindings[2] = {0};
    bindings[0].binding         = 0;
    bindings[0].descriptorType  = VK_DESCRIPTOR_TYPE_COMBINED_IMAGE_SAMPLER;
    bindings[0].descriptorCount = 1;
    bindings[0].stageFlags      = VK_SHADER_STAGE_COMPUTE_BIT;
    bindings[1].binding         = 1;
    bindings[1].descriptorType  = VK_DESCRIPTOR_TYPE_STORAGE_IMAGE;
    bindings[1].descriptorCount = 1;
    bindings[1].stageFlags      = VK_SHADER_STAGE_COMPUTE_BIT;

    VkDescriptorSetLayoutCreateInfo dslci = { VK_STRUCTURE_TYPE_DESCRIPTOR_SET_LAYOUT_CREATE_INFO };
    dslci.bindingCount = 2;
    dslci.pBindings    = bindings;
    if (vkCreateDescriptorSetLayout(g_dev, &dslci, NULL, &g_pp_dsl) != VK_SUCCESS) {
        VLOGE("pp: vkCreateDescriptorSetLayout failed");
        goto fail;
    }

    VkPushConstantRange pcRange = {0};
    pcRange.stageFlags = VK_SHADER_STAGE_COMPUTE_BIT;
    pcRange.offset     = 0;
    pcRange.size       = sizeof(PPPushConstants);

    VkPipelineLayoutCreateInfo plci = { VK_STRUCTURE_TYPE_PIPELINE_LAYOUT_CREATE_INFO };
    plci.setLayoutCount         = 1;
    plci.pSetLayouts            = &g_pp_dsl;
    plci.pushConstantRangeCount = 1;
    plci.pPushConstantRanges    = &pcRange;
    if (vkCreatePipelineLayout(g_dev, &plci, NULL, &g_pp_playout) != VK_SUCCESS) {
        VLOGE("pp: vkCreatePipelineLayout failed");
        goto fail;
    }

    VkPipelineShaderStageCreateInfo stage = { VK_STRUCTURE_TYPE_PIPELINE_SHADER_STAGE_CREATE_INFO };
    stage.stage  = VK_SHADER_STAGE_COMPUTE_BIT;
    stage.module = g_pp_shader;
    stage.pName  = "main";

    VkComputePipelineCreateInfo cpci = { VK_STRUCTURE_TYPE_COMPUTE_PIPELINE_CREATE_INFO };
    cpci.stage  = stage;
    cpci.layout = g_pp_playout;
    if (vkCreateComputePipelines(g_dev, VK_NULL_HANDLE, 1, &cpci, NULL, &g_pp_pipeline) != VK_SUCCESS) {
        VLOGE("pp: vkCreateComputePipelines failed");
        goto fail;
    }

    VkDescriptorPoolSize poolSizes[2] = {0};
    poolSizes[0].type            = VK_DESCRIPTOR_TYPE_COMBINED_IMAGE_SAMPLER;
    poolSizes[0].descriptorCount = 1;
    poolSizes[1].type            = VK_DESCRIPTOR_TYPE_STORAGE_IMAGE;
    poolSizes[1].descriptorCount = 1;

    VkDescriptorPoolCreateInfo dpci = { VK_STRUCTURE_TYPE_DESCRIPTOR_POOL_CREATE_INFO };
    dpci.maxSets       = 1;
    dpci.poolSizeCount = 2;
    dpci.pPoolSizes    = poolSizes;
    if (vkCreateDescriptorPool(g_dev, &dpci, NULL, &g_pp_dpool) != VK_SUCCESS) {
        VLOGE("pp: vkCreateDescriptorPool failed");
        goto fail;
    }

    VkDescriptorSetAllocateInfo dsai = { VK_STRUCTURE_TYPE_DESCRIPTOR_SET_ALLOCATE_INFO };
    dsai.descriptorPool     = g_pp_dpool;
    dsai.descriptorSetCount = 1;
    dsai.pSetLayouts        = &g_pp_dsl;
    if (vkAllocateDescriptorSets(g_dev, &dsai, &g_pp_dset) != VK_SUCCESS) {
        VLOGE("pp: vkAllocateDescriptorSets failed");
        goto fail;
    }

    VLOGI("pp: compute pipeline ready");
    return 1;

fail:
    // Leave whatever partially-created objects behind; vk_full_cleanup tears
    // them all down unconditionally (destroying VK_NULL_HANDLE is a no-op).
    // Reset g_pp_pipeline to NULL so the "already created" check above
    // continues retrying on future frames instead of wedging in a broken half
    // state forever.
    g_pp_pipeline = VK_NULL_HANDLE;
    return 0;
}

// vk_pp_ensure_history (re)creates the history/output image when the frame
// size changes. Also runs before any command buffer recording.
static int vk_pp_ensure_history(int w, int h) {
    if (g_pp_hist_img != VK_NULL_HANDLE && g_pp_hist_w == w && g_pp_hist_h == h) return 1;
    if (g_pp_hist_img != VK_NULL_HANDLE) {
        vkDeviceWaitIdle(g_dev);
        if (g_pp_hist_view) { vkDestroyImageView(g_dev, g_pp_hist_view, NULL); g_pp_hist_view = VK_NULL_HANDLE; }
        if (g_pp_hist_mem) { vkFreeMemory(g_dev, g_pp_hist_mem, NULL); g_pp_hist_mem = VK_NULL_HANDLE; }
        if (g_pp_hist_img) { vkDestroyImage(g_dev, g_pp_hist_img, NULL); g_pp_hist_img = VK_NULL_HANDLE; }
    }

    VkImageCreateInfo ici = { VK_STRUCTURE_TYPE_IMAGE_CREATE_INFO };
    ici.imageType   = VK_IMAGE_TYPE_2D;
    ici.format      = VK_FORMAT_R8G8B8A8_UNORM;
    ici.extent      = (VkExtent3D){(uint32_t)w, (uint32_t)h, 1};
    ici.mipLevels   = 1;
    ici.arrayLayers = 1;
    ici.samples     = VK_SAMPLE_COUNT_1_BIT;
    ici.tiling      = VK_IMAGE_TILING_OPTIMAL;
    ici.usage       = VK_IMAGE_USAGE_STORAGE_BIT | VK_IMAGE_USAGE_TRANSFER_SRC_BIT;
    ici.initialLayout = VK_IMAGE_LAYOUT_UNDEFINED;
    VkResult hist_res = vkCreateImage(g_dev, &ici, NULL, &g_pp_hist_img);
    if (hist_res != VK_SUCCESS) {
        VLOGE("pp: history vkCreateImage(%dx%d) failed: %d", w, h, (int)hist_res);
        return 0;
    }

    VkMemoryRequirements mr;
    vkGetImageMemoryRequirements(g_dev, g_pp_hist_img, &mr);
    VkPhysicalDeviceMemoryProperties mp;
    vkGetPhysicalDeviceMemoryProperties(g_pdev, &mp);
    uint32_t mi = vk_find_mem(&mp, mr.memoryTypeBits, VK_MEMORY_PROPERTY_DEVICE_LOCAL_BIT);
    if (mi == UINT32_MAX) {
        VLOGE("pp: history no matching memory type");
        vkDestroyImage(g_dev, g_pp_hist_img, NULL); g_pp_hist_img = VK_NULL_HANDLE; return 0;
    }
    VkMemoryAllocateInfo mai = { VK_STRUCTURE_TYPE_MEMORY_ALLOCATE_INFO };
    mai.allocationSize  = mr.size;
    mai.memoryTypeIndex = mi;
    hist_res = vkAllocateMemory(g_dev, &mai, NULL, &g_pp_hist_mem);
    if (hist_res != VK_SUCCESS) {
        VLOGE("pp: history vkAllocateMemory failed: %d", (int)hist_res);
        vkDestroyImage(g_dev, g_pp_hist_img, NULL); g_pp_hist_img = VK_NULL_HANDLE; return 0;
    }
    vkBindImageMemory(g_dev, g_pp_hist_img, g_pp_hist_mem, 0);

    VkImageViewCreateInfo vci = { VK_STRUCTURE_TYPE_IMAGE_VIEW_CREATE_INFO };
    vci.image    = g_pp_hist_img;
    vci.viewType = VK_IMAGE_VIEW_TYPE_2D;
    vci.format   = VK_FORMAT_R8G8B8A8_UNORM;
    vci.subresourceRange.aspectMask = VK_IMAGE_ASPECT_COLOR_BIT;
    vci.subresourceRange.levelCount = 1;
    vci.subresourceRange.layerCount = 1;
    if (vkCreateImageView(g_dev, &vci, NULL, &g_pp_hist_view) != VK_SUCCESS) {
        VLOGE("pp: history vkCreateImageView failed");
        vkFreeMemory(g_dev, g_pp_hist_mem, NULL); g_pp_hist_mem = VK_NULL_HANDLE;
        vkDestroyImage(g_dev, g_pp_hist_img, NULL); g_pp_hist_img = VK_NULL_HANDLE;
        return 0;
    }

    g_pp_hist_w = w; g_pp_hist_h = h;
    g_pp_hist_layout = VK_IMAGE_LAYOUT_UNDEFINED;
    // Fresh (or resized) history has no valid prior frame to blend against.
    atomic_store(&g_pp_primed, 0);
    VLOGI("pp: history image ready (%dx%d)", w, h);
    return 1;
}

// vk_pp_process records the compute dispatch that turns srcImg (the frame
// just decoded, fw x fh, R8G8B8A8) into the post-processed g_pp_hist_img and
// returns that image, already transitioned to TRANSFER_SRC_OPTIMAL and ready
// to use as a vkCmdBlitImage source exactly like the raw frame would have
// been. srcLayout/srcAccess/srcStage describe srcImg's true current state so
// the read barrier is correct for both the CPU staging path (coming from a
// real TRANSFER_DST_OPTIMAL write) and the AHardwareBuffer zero-copy path
// (which -- like the rest of this file's handling of that image -- uses the
// VK_IMAGE_LAYOUT_UNDEFINED discard trick since GL owns it across frames).
static VkImage vk_pp_process(VkCommandBuffer cb, VkImage srcImg, VkImageView srcView,
                              VkImageLayout srcLayout, VkAccessFlags srcAccess,
                              VkPipelineStageFlags srcStage, int fw, int fh) {
    vk_image_barrier(cb, srcImg, srcLayout, VK_IMAGE_LAYOUT_SHADER_READ_ONLY_OPTIMAL,
        srcAccess, VK_ACCESS_SHADER_READ_BIT,
        srcStage, VK_PIPELINE_STAGE_COMPUTE_SHADER_BIT);

    VkAccessFlags histSrcAccess = 0;
    VkPipelineStageFlags histSrcStage = VK_PIPELINE_STAGE_TOP_OF_PIPE_BIT;
    if (g_pp_hist_layout == VK_IMAGE_LAYOUT_TRANSFER_SRC_OPTIMAL) {
        histSrcAccess = VK_ACCESS_TRANSFER_READ_BIT;
        histSrcStage  = VK_PIPELINE_STAGE_TRANSFER_BIT;
    }
    vk_image_barrier(cb, g_pp_hist_img, g_pp_hist_layout, VK_IMAGE_LAYOUT_GENERAL,
        histSrcAccess, VK_ACCESS_SHADER_READ_BIT | VK_ACCESS_SHADER_WRITE_BIT,
        histSrcStage, VK_PIPELINE_STAGE_COMPUTE_SHADER_BIT);
    g_pp_hist_layout = VK_IMAGE_LAYOUT_GENERAL;

    if (g_pp_dset_bound_view != srcView) {
        VkDescriptorImageInfo imgInfo0 = { g_pp_sampler, srcView, VK_IMAGE_LAYOUT_SHADER_READ_ONLY_OPTIMAL };
        VkDescriptorImageInfo imgInfo1 = { VK_NULL_HANDLE, g_pp_hist_view, VK_IMAGE_LAYOUT_GENERAL };
        VkWriteDescriptorSet writes[2] = {0};
        writes[0].sType           = VK_STRUCTURE_TYPE_WRITE_DESCRIPTOR_SET;
        writes[0].dstSet          = g_pp_dset;
        writes[0].dstBinding      = 0;
        writes[0].descriptorCount = 1;
        writes[0].descriptorType  = VK_DESCRIPTOR_TYPE_COMBINED_IMAGE_SAMPLER;
        writes[0].pImageInfo      = &imgInfo0;
        writes[1].sType           = VK_STRUCTURE_TYPE_WRITE_DESCRIPTOR_SET;
        writes[1].dstSet          = g_pp_dset;
        writes[1].dstBinding      = 1;
        writes[1].descriptorCount = 1;
        writes[1].descriptorType  = VK_DESCRIPTOR_TYPE_STORAGE_IMAGE;
        writes[1].pImageInfo      = &imgInfo1;
        vkUpdateDescriptorSets(g_dev, 2, writes, 0, NULL);
        g_pp_dset_bound_view = srcView;
    }

    PPPushConstants pc;
    pthread_mutex_lock(&g_pp_mu);
    pc.sharpen    = g_pp_params.sharpen;
    pc.denoise    = g_pp_params.denoise;
    pc.temporal   = g_pp_params.temporal;
    pc.gamma      = g_pp_params.gamma;
    pc.contrast   = g_pp_params.contrast;
    pc.saturation = g_pp_params.saturation;
    pthread_mutex_unlock(&g_pp_mu);
    pc.enabled = 1;
    pc.width   = fw;
    pc.height  = fh;
    if (!atomic_exchange(&g_pp_primed, 1)) {
        pc.temporal = 0.0f; // history is stale/uninitialised for this one frame
    }

    // The final blit shrinks this frame (native capture/decode resolution)
    // down to the on-screen video rect -- often by 2x or more -- and that
    // blit's VK_FILTER_LINEAR resampling re-blurs anything a 1-texel-radius
    // filter changed. Widen the shader's neighbor taps by roughly the same
    // ratio so denoise/sharpen survive the downscale instead of washing out
    // to invisible. g_swap_ext is the whole window, not just the fitted
    // video rect, so this slightly under-estimates the true downscale when
    // the video is letterboxed -- fine, it only needs to be in the right
    // ballpark, not exact.
    int radius = 1;
    if (g_swap_ext.width > 0 && fw > (int)g_swap_ext.width) {
        radius = (fw + (int)g_swap_ext.width / 2) / (int)g_swap_ext.width;
        if (radius < 1) radius = 1;
        if (radius > 4) radius = 4;
    }
    pc.radius = radius;

    vkCmdBindPipeline(cb, VK_PIPELINE_BIND_POINT_COMPUTE, g_pp_pipeline);
    vkCmdBindDescriptorSets(cb, VK_PIPELINE_BIND_POINT_COMPUTE, g_pp_playout, 0, 1, &g_pp_dset, 0, NULL);
    vkCmdPushConstants(cb, g_pp_playout, VK_SHADER_STAGE_COMPUTE_BIT, 0, sizeof(pc), &pc);
    uint32_t gx = (uint32_t)((fw + 7) / 8);
    uint32_t gy = (uint32_t)((fh + 7) / 8);
    vkCmdDispatch(cb, gx, gy, 1);

    vk_image_barrier(cb, g_pp_hist_img, VK_IMAGE_LAYOUT_GENERAL, VK_IMAGE_LAYOUT_TRANSFER_SRC_OPTIMAL,
        VK_ACCESS_SHADER_WRITE_BIT, VK_ACCESS_TRANSFER_READ_BIT,
        VK_PIPELINE_STAGE_COMPUTE_SHADER_BIT, VK_PIPELINE_STAGE_TRANSFER_BIT);
    g_pp_hist_layout = VK_IMAGE_LAYOUT_TRANSFER_SRC_OPTIMAL;

    g_pp_dispatch_count++;
    if (g_pp_dispatch_count == 1 || g_pp_dispatch_count % 300 == 0) {
        VLOGI("pp: dispatch #%lld gx=%u gy=%u radius=%d swap=%ux%u src=%p srcView=%p hist=%p sharpen=%.2f denoise=%.2f temporal=%.2f",
              (long long)g_pp_dispatch_count, gx, gy, pc.radius, g_swap_ext.width, g_swap_ext.height,
              (void*)srcImg, (void*)srcView, (void*)g_pp_hist_img,
              (double)pc.sharpen, (double)pc.denoise, (double)pc.temporal);
    }

    return g_pp_hist_img;
}

// vk_pp_want_process reports whether post-processing should run this frame:
// user-enabled, compute-capable device, and both the pipeline and the
// history image (sized for this frame) were created successfully. Never
// touches a command buffer.
static int vk_pp_want_process(int fw, int fh) {
    if (!atomic_load(&g_pp_enabled)) return 0;
    if (!vk_pp_ensure_pipeline()) return 0;
    if (!vk_pp_ensure_history(fw, fh)) return 0;
    return 1;
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

    // When Vulkan post-processing (denoise/sharpen/temporal/grade) is enabled
    // from the popup, run the compute pass and blit from its output instead
    // of the raw decoded frame. Disabled (the default) takes the exact
    // original path -- g_tex straight to the swapchain.
    VkImage blitSrc = g_tex;
    if (vk_pp_want_process(fw, fh)) {
        blitSrc = vk_pp_process(g_cmdbuf, g_tex, g_tex_view,
            VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL, VK_ACCESS_TRANSFER_WRITE_BIT,
            VK_PIPELINE_STAGE_TRANSFER_BIT, fw, fh);
    } else {
        vk_image_barrier(g_cmdbuf, g_tex,
            VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL, VK_IMAGE_LAYOUT_TRANSFER_SRC_OPTIMAL,
            VK_ACCESS_TRANSFER_WRITE_BIT, VK_ACCESS_TRANSFER_READ_BIT,
            VK_PIPELINE_STAGE_TRANSFER_BIT, VK_PIPELINE_STAGE_TRANSFER_BIT);
    }

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
    pthread_mutex_unlock(&g_state_mu);
    if (u0 < 0.0f) u0 = 0.0f; if (u1 > 1.0f) u1 = 1.0f;
    if (v0 < 0.0f) v0 = 0.0f; if (v1 > 1.0f) v1 = 1.0f;
    if (u1 <= u0 + 0.001f) { u0 = 0.0f; u1 = 1.0f; }
    if (v1 <= v0 + 0.001f) { v0 = 0.0f; v1 = 1.0f; }

    int src_x0 = (int)(u0 * fw);
    int src_y0 = (int)(v0 * fh);
    int src_x1 = (int)(u1 * fw + 0.5f);
    int src_y1 = (int)(v1 * fh + 0.5f);
    if (src_x0 < 0) src_x0 = 0;
    if (src_y0 < 0) src_y0 = 0;
    if (src_x1 > fw) src_x1 = fw;
    if (src_y1 > fh) src_y1 = fh;
    if (src_x1 <= src_x0 || src_y1 <= src_y0) return 0;

    int sw = (int)g_swap_ext.width, sh = (int)g_swap_ext.height;
    float fa = (float)(src_x1 - src_x0) / (float)(src_y1 - src_y0);
    float wa = (float)sw / (float)(sh ? sh : 1);
    int dx = 0, dy = 0, dw = sw, dh = sh;
    if (fa > wa) { dh = (int)(sw / fa + 0.5f); dy = atomic_load(&g_align_bottom) ? (sh - dh) : (sh - dh) / 2; }
    else         { dw = (int)(sh * fa + 0.5f); dx = (sw - dw) / 2; }

    VkClearColorValue black = {0};
    VkImageSubresourceRange full = { VK_IMAGE_ASPECT_COLOR_BIT, 0, 1, 0, 1 };
    vkCmdClearColorImage(g_cmdbuf, g_swap_imgs[img_idx],
                         VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL, &black, 1, &full);

    // Sync clear before blit (Write-After-Write hazard in TRANSFER stage)
    vk_image_barrier(g_cmdbuf, g_swap_imgs[img_idx],
        VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL, VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL,
        VK_ACCESS_TRANSFER_WRITE_BIT, VK_ACCESS_TRANSFER_WRITE_BIT,
        VK_PIPELINE_STAGE_TRANSFER_BIT, VK_PIPELINE_STAGE_TRANSFER_BIT);

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
        blitSrc,               VK_IMAGE_LAYOUT_TRANSFER_SRC_OPTIMAL,
        g_swap_imgs[img_idx], VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL,
        1, &blt, VK_FILTER_LINEAR);

    // Draw virtual cursor (if visible) on top of the video.
    // Use the state snapshot taken at the start of this function so that cursor
    // and viewport are always from the same Go update — never a stale mix.
    if (snap_vis && g_cursor_nspans > 0 &&
        g_cursor_vk_buf != VK_NULL_HANDLE) {
        float uc = snap_uc;
        float vc = snap_vc;
        float span_u = u1 - u0, span_v = v1 - v0;
        float tu = (span_u > 0.001f) ? (uc - u0) / span_u : 0.5f;
        float tv = (span_v > 0.001f) ? (vc - v0) / span_v : 0.5f;
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

    vk_image_barrier(g_cmdbuf, g_swap_imgs[img_idx],
        VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL, VK_IMAGE_LAYOUT_PRESENT_SRC_KHR,
        VK_ACCESS_TRANSFER_WRITE_BIT, 0,
        VK_PIPELINE_STAGE_TRANSFER_BIT, VK_PIPELINE_STAGE_BOTTOM_OF_PIPE_BIT);

    vkEndCommandBuffer(g_cmdbuf);

    VkPipelineStageFlags wait_stage = VK_PIPELINE_STAGE_TRANSFER_BIT;
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
            if (g_hwimport_view[i] != VK_NULL_HANDLE) { vkDestroyImageView(g_dev, g_hwimport_view[i], NULL); g_hwimport_view[i] = VK_NULL_HANDLE; }
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

    if (g_hwimport_view[slot] != VK_NULL_HANDLE) {
        vkDestroyImageView(g_dev, g_hwimport_view[slot], NULL); g_hwimport_view[slot] = VK_NULL_HANDLE;
    }
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

    // Sampled view for the post-processing compute pass (vk_pp_process). Not
    // required for the plain blit path, so a failure here is logged but not
    // fatal -- vk_pp_process checks for VK_NULL_HANDLE and simply falls back
    // to the unprocessed blit for frames sourced from this slot.
    VkImageViewCreateInfo vci = { VK_STRUCTURE_TYPE_IMAGE_VIEW_CREATE_INFO };
    vci.image    = img;
    vci.viewType = VK_IMAGE_VIEW_TYPE_2D;
    vci.format   = fmtProps.format;
    vci.subresourceRange.aspectMask = VK_IMAGE_ASPECT_COLOR_BIT;
    vci.subresourceRange.levelCount = 1;
    vci.subresourceRange.layerCount = 1;
    if (vkCreateImageView(g_dev, &vci, NULL, &g_hwimport_view[slot]) != VK_SUCCESS) {
        VLOGE("hwimport: vkCreateImageView failed (slot %d), postprocessing unavailable for this slot", slot);
        g_hwimport_view[slot] = VK_NULL_HANDLE;
    }

    VLOGI("hwimport: imported AHardwareBuffer %p as %dx%d VkImage (slot %d, fmt=%d)",
          ahb_void, fw, fh, slot, (int)fmtProps.format);
    return img;
}

// hwimport_get_view returns the cached sampled VkImageView for the VkImage
// hwimport_get_or_create most recently returned for this ahb_void, or
// VK_NULL_HANDLE if none (import failed or the view failed to create).
static VkImageView hwimport_get_view(void *ahb_void) {
    for (int i = 0; i < HWIMPORT_COUNT; i++)
        if (g_hwimport_src[i] == ahb_void) return g_hwimport_view[i];
    return VK_NULL_HANDLE;
}

static int vk_render_frame_hw(void *ahb_void, int fw, int fh) {
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

    VkImage srcImg = hwimport_get_or_create(ahb_void, fw, fh);
    if (srcImg == VK_NULL_HANDLE) return 0;
    VkImageView srcView = hwimport_get_view(ahb_void);

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

    // GL already wrote (and glFinish()'d) this frame's pixels straight into
    // srcImg's backing memory -- there's nothing for us to copy in, just a
    // layout transition before reading it. Treated as UNDEFINED->TRANSFER_SRC
    // (or UNDEFINED->SHADER_READ_ONLY when post-processing) every frame
    // rather than tracking Vulkan-side layout across GL/Vulkan API
    // boundaries: we never write to this image via Vulkan ourselves, only
    // ever read it, so there's no prior Vulkan-tracked content to lose.
    VkImage blitSrc = srcImg;
    if (srcView != VK_NULL_HANDLE && vk_pp_want_process(fw, fh)) {
        blitSrc = vk_pp_process(g_cmdbuf, srcImg, srcView,
            VK_IMAGE_LAYOUT_UNDEFINED, 0,
            VK_PIPELINE_STAGE_TOP_OF_PIPE_BIT, fw, fh);
    } else {
        vk_image_barrier(g_cmdbuf, srcImg,
            VK_IMAGE_LAYOUT_UNDEFINED, VK_IMAGE_LAYOUT_TRANSFER_SRC_OPTIMAL,
            0, VK_ACCESS_TRANSFER_READ_BIT,
            VK_PIPELINE_STAGE_TOP_OF_PIPE_BIT, VK_PIPELINE_STAGE_TRANSFER_BIT);
    }

    vk_image_barrier(g_cmdbuf, g_swap_imgs[img_idx],
        VK_IMAGE_LAYOUT_UNDEFINED, VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL,
        0, VK_ACCESS_TRANSFER_WRITE_BIT,
        VK_PIPELINE_STAGE_TOP_OF_PIPE_BIT, VK_PIPELINE_STAGE_TRANSFER_BIT);

    // Snapshot viewport + cursor state atomically, identical to vk_render_frame.
    pthread_mutex_lock(&g_state_mu);
    float u0 = atomic_load(&g_vp_u0_fp) / 65536.0f;
    float v0 = atomic_load(&g_vp_v0_fp) / 65536.0f;
    float u1 = atomic_load(&g_vp_u1_fp) / 65536.0f;
    float v1 = atomic_load(&g_vp_v1_fp) / 65536.0f;
    float snap_uc  = atomic_load(&g_cursor_uc_fp)  / 65536.0f;
    float snap_vc  = atomic_load(&g_cursor_vc_fp)  / 65536.0f;
    int   snap_vis = atomic_load(&g_cursor_visible);
    pthread_mutex_unlock(&g_state_mu);
    if (u0 < 0.0f) u0 = 0.0f; if (u1 > 1.0f) u1 = 1.0f;
    if (v0 < 0.0f) v0 = 0.0f; if (v1 > 1.0f) v1 = 1.0f;
    if (u1 <= u0 + 0.001f) { u0 = 0.0f; u1 = 1.0f; }
    if (v1 <= v0 + 0.001f) { v0 = 0.0f; v1 = 1.0f; }

    int src_x0 = (int)(u0 * fw);
    int src_y0 = (int)(v0 * fh);
    int src_x1 = (int)(u1 * fw + 0.5f);
    int src_y1 = (int)(v1 * fh + 0.5f);
    if (src_x0 < 0) src_x0 = 0;
    if (src_y0 < 0) src_y0 = 0;
    if (src_x1 > fw) src_x1 = fw;
    if (src_y1 > fh) src_y1 = fh;
    if (src_x1 <= src_x0 || src_y1 <= src_y0) return 0;

    int sw = (int)g_swap_ext.width, sh = (int)g_swap_ext.height;
    float fa = (float)(src_x1 - src_x0) / (float)(src_y1 - src_y0);
    float wa = (float)sw / (float)(sh ? sh : 1);
    int dx = 0, dy = 0, dw = sw, dh = sh;
    if (fa > wa) { dh = (int)(sw / fa + 0.5f); dy = atomic_load(&g_align_bottom) ? (sh - dh) : (sh - dh) / 2; }
    else         { dw = (int)(sh * fa + 0.5f); dx = (sw - dw) / 2; }

    VkClearColorValue black = {0};
    VkImageSubresourceRange full = { VK_IMAGE_ASPECT_COLOR_BIT, 0, 1, 0, 1 };
    vkCmdClearColorImage(g_cmdbuf, g_swap_imgs[img_idx],
                         VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL, &black, 1, &full);

    vk_image_barrier(g_cmdbuf, g_swap_imgs[img_idx],
        VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL, VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL,
        VK_ACCESS_TRANSFER_WRITE_BIT, VK_ACCESS_TRANSFER_WRITE_BIT,
        VK_PIPELINE_STAGE_TRANSFER_BIT, VK_PIPELINE_STAGE_TRANSFER_BIT);

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
        blitSrc,               VK_IMAGE_LAYOUT_TRANSFER_SRC_OPTIMAL,
        g_swap_imgs[img_idx], VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL,
        1, &blt, VK_FILTER_LINEAR);

    if (snap_vis && g_cursor_nspans > 0 &&
        g_cursor_vk_buf != VK_NULL_HANDLE) {
        float uc = snap_uc;
        float vc = snap_vc;
        float span_u = u1 - u0, span_v = v1 - v0;
        float tu = (span_u > 0.001f) ? (uc - u0) / span_u : 0.5f;
        float tv = (span_v > 0.001f) ? (vc - v0) / span_v : 0.5f;
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

    vk_image_barrier(g_cmdbuf, g_swap_imgs[img_idx],
        VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL, VK_IMAGE_LAYOUT_PRESENT_SRC_KHR,
        VK_ACCESS_TRANSFER_WRITE_BIT, 0,
        VK_PIPELINE_STAGE_TRANSFER_BIT, VK_PIPELINE_STAGE_BOTTOM_OF_PIPE_BIT);

    vkEndCommandBuffer(g_cmdbuf);

    VkPipelineStageFlags wait_stage = VK_PIPELINE_STAGE_TRANSFER_BIT;
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

static void *vk_render_thread(void *unused) {
    (void)unused;
    VLOGI("render thread started");
    int last_fw = 0, last_fh = 0, last_fs = 0;
    int has_last_frame = 0;
    void *last_ahb = NULL;
    int last_ahb_w = 0, last_ahb_h = 0;
    int has_last_ahb = 0;

    while (atomic_load(&g_active)) {
        struct timeval tv = {0, 8000};
        fd_set fds; FD_ZERO(&fds); FD_SET(g_pipe_r, &fds);
        select(g_pipe_r + 1, &fds, NULL, NULL, &tv);
        if (FD_ISSET(g_pipe_r, &fds)) {
            char tmp[64]; read(g_pipe_r, tmp, sizeof(tmp));
        }
        if (!atomic_load(&g_active)) break;
        if (atomic_load(&g_hidden))  continue;

        // Explicit swapchain recreation requested (e.g. after fullscreen entry/exit).
        if (atomic_exchange(&g_force_recreate, 0)) {
            VLOGI("force swapchain recreate requested");
            vk_recreate_swapchain();
            continue;
        }

        int fw = 0, fh = 0, fs = 0;
        int got_frame = 0;
        void *ahb = NULL;
        int ahb_w = 0, ahb_h = 0;
        int got_ahb = 0;

        pthread_mutex_lock(&g_mu);
        if (g_ahb_ready && g_pend_ahb) {
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
            if (g_dev && vk_ensure_staging(sz) && g_stage_ptr) {
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

        int ok = got_ahb ? vk_render_frame_hw(ahb, ahb_w, ahb_h) : vk_render_frame(fw, fh, fs);
        if (ok) {
            g_rendered++;
            // Only count real video frames toward FPS — cursor-only redraws skew the counter.
            if (is_video_frame) {
                g_fps_n++;
                double now = mono_sec_vk();
                if (g_rendered == 1) { g_fps_t0 = now; g_fps_n = 0; }
                if (now - g_fps_t0 >= 2.0 && g_fps_n > 0) {
                    g_stat_fps   = (float)((double)g_fps_n / (now - g_fps_t0));
                    g_stat_ready = 1;
                    g_fps_t0 = now; g_fps_n = 0;
                }
            }
            if (g_rendered % 300 == 0) {
                int pp_on = atomic_load(&g_pp_enabled);
                PPParams pp_snapshot;
                pthread_mutex_lock(&g_pp_mu);
                pp_snapshot = g_pp_params;
                pthread_mutex_unlock(&g_pp_mu);
                VLOGI("rendered %lld frames, submitted %lld, fps=%.1f, pp=%s (pipeline=%s sharpen=%.2f denoise=%.2f temporal=%.2f gamma=%.2f contrast=%.2f saturation=%.2f)",
                      (long long)g_rendered, (long long)g_submitted, (double)g_stat_fps,
                      pp_on ? "on" : "off", g_pp_pipeline != VK_NULL_HANDLE ? "ready" : "not-created",
                      (double)pp_snapshot.sharpen, (double)pp_snapshot.denoise, (double)pp_snapshot.temporal,
                      (double)pp_snapshot.gamma, (double)pp_snapshot.contrast, (double)pp_snapshot.saturation);
            }
        }
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

    if (g_dev) {
        vkDeviceWaitIdle(g_dev);
        cursor_destroy();
        if (g_stage_ptr && g_stage_mem) { vkUnmapMemory(g_dev, g_stage_mem); g_stage_ptr = NULL; }
        if (g_stage_buf) { vkDestroyBuffer(g_dev, g_stage_buf, NULL); g_stage_buf = VK_NULL_HANDLE; }
        if (g_stage_mem) { vkFreeMemory(g_dev, g_stage_mem, NULL);   g_stage_mem = VK_NULL_HANDLE; }
        g_stage_sz = 0;
        if (g_tex_view) { vkDestroyImageView(g_dev, g_tex_view, NULL); g_tex_view = VK_NULL_HANDLE; }
        if (g_tex)     { vkDestroyImage(g_dev, g_tex, NULL);   g_tex     = VK_NULL_HANDLE; }
        if (g_tex_mem) { vkFreeMemory(g_dev, g_tex_mem, NULL); g_tex_mem = VK_NULL_HANDLE; }
        g_tex_w = 0; g_tex_h = 0;
        for (int i = 0; i < HWIMPORT_COUNT; i++) {
            if (g_hwimport_view[i]) { vkDestroyImageView(g_dev, g_hwimport_view[i], NULL); g_hwimport_view[i] = VK_NULL_HANDLE; }
            if (g_hwimport_img[i]) { vkDestroyImage(g_dev, g_hwimport_img[i], NULL); g_hwimport_img[i] = VK_NULL_HANDLE; }
            if (g_hwimport_mem[i]) { vkFreeMemory(g_dev, g_hwimport_mem[i], NULL);   g_hwimport_mem[i] = VK_NULL_HANDLE; }
            g_hwimport_src[i] = NULL;
        }
        g_hwimport_w = g_hwimport_h = 0;
        g_hw_import_supported = 0;

        // Post-processing objects (compute pipeline + history image). User
        // settings in g_pp_params/g_pp_enabled deliberately survive this --
        // reapplied automatically the next time postprocessing runs, so a
        // reconnect doesn't silently reset the popup's sliders.
        if (g_pp_hist_view) { vkDestroyImageView(g_dev, g_pp_hist_view, NULL); g_pp_hist_view = VK_NULL_HANDLE; }
        if (g_pp_hist_img)  { vkDestroyImage(g_dev, g_pp_hist_img, NULL);      g_pp_hist_img  = VK_NULL_HANDLE; }
        if (g_pp_hist_mem)  { vkFreeMemory(g_dev, g_pp_hist_mem, NULL);        g_pp_hist_mem  = VK_NULL_HANDLE; }
        g_pp_hist_w = 0; g_pp_hist_h = 0;
        g_pp_hist_layout = VK_IMAGE_LAYOUT_UNDEFINED;
        g_pp_dset_bound_view = VK_NULL_HANDLE;
        atomic_store(&g_pp_primed, 0);
        if (g_pp_dpool)   { vkDestroyDescriptorPool(g_dev, g_pp_dpool, NULL);        g_pp_dpool   = VK_NULL_HANDLE; g_pp_dset = VK_NULL_HANDLE; }
        if (g_pp_pipeline){ vkDestroyPipeline(g_dev, g_pp_pipeline, NULL);           g_pp_pipeline= VK_NULL_HANDLE; }
        if (g_pp_playout) { vkDestroyPipelineLayout(g_dev, g_pp_playout, NULL);      g_pp_playout = VK_NULL_HANDLE; }
        if (g_pp_dsl)     { vkDestroyDescriptorSetLayout(g_dev, g_pp_dsl, NULL);     g_pp_dsl     = VK_NULL_HANDLE; }
        if (g_pp_shader)  { vkDestroyShaderModule(g_dev, g_pp_shader, NULL);         g_pp_shader  = VK_NULL_HANDLE; }
        if (g_pp_sampler) { vkDestroySampler(g_dev, g_pp_sampler, NULL);             g_pp_sampler = VK_NULL_HANDLE; }
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
    atomic_store(&g_cursor_visible, 0);
    atomic_store(&g_cursor_uc_fp, 32768); atomic_store(&g_cursor_vc_fp, 32768);
    atomic_store(&g_cursor_dirty, 0);

    if (!vk_create_instance())  { VLOGE("vkCreateInstance failed");  goto fail; }
    if (!vk_select_device())    { VLOGE("no suitable GPU");          goto fail; }
    if (!vk_create_device())    { VLOGE("vkCreateDevice failed");    goto fail; }
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
    pthread_mutex_unlock(&g_mu);
    if (g_pipe_w >= 0) { char c = 1; write(g_pipe_w, &c, 1); }
    return 1;
}

// android_vk_hwbuffer_supported reports whether this device/driver has every
// extension AHardwareBuffer zero-copy import needs (see vk_create_device).
// Callers should check this once after android_vk_create succeeds and pick
// android_vk_try_submit_hwbuffer vs android_vk_try_submit for the whole
// session accordingly -- switching mid-session isn't supported (the render
// thread doesn't clear the other path's pending frame).
int android_vk_hwbuffer_supported(void) {
    return g_hw_import_supported;
}

void android_vk_update_rect(int x, int y, int w, int h) {
    if (!atomic_load(&g_active)) return;
    atomic_store(&g_dst_w, w);
    atomic_store(&g_dst_h, h);
    java_set_rect(x, y, w, h);
}

void android_vk_set_hidden(int hidden) {
    if (!atomic_load(&g_active)) return;
    atomic_store(&g_hidden, hidden ? 1 : 0);
    java_set_visible(!hidden);
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
    atomic_store(&g_align_bottom, bottom ? 1 : 0);
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

// android_vk_set_viewport_and_cursor updates viewport UV and cursor position in
// a single mutex-protected write.  The render thread always reads both under the
// same mutex, so it will never see viewport from update N paired with cursor from
// update N+1 (which causes the cursor to flash at a wrong screen position).
void android_vk_set_viewport_and_cursor(float u0, float v0, float u1, float v1,
                                         float uc, float vc, int visible) {
    pthread_mutex_lock(&g_state_mu);
    atomic_store(&g_vp_u0_fp, (int)(u0 * 65536));
    atomic_store(&g_vp_v0_fp, (int)(v0 * 65536));
    atomic_store(&g_vp_u1_fp, (int)(u1 * 65536));
    atomic_store(&g_vp_v1_fp, (int)(v1 * 65536));
    atomic_store(&g_cursor_uc_fp, (int)(uc * 65536));
    atomic_store(&g_cursor_vc_fp, (int)(vc * 65536));
    atomic_store(&g_cursor_visible, visible ? 1 : 0);
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

// android_vk_set_postprocess updates the Vulkan post-processing pipeline
// (denoise → sharpen → temporal accumulation → gamma/contrast/saturation)
// live from the Fyne "Vulkan" popup. Safe to call from any thread at any
// time, including before the renderer is created (settings are just cached
// and applied to the next frame that's actually rendered). When enabled is
// 0, the render thread takes the exact original blit-only path -- this
// function has zero effect on latency or output until the user opts in.
void android_vk_set_postprocess(int enabled, float sharpen, float denoise, float temporal,
                                 float gamma, float contrast, float saturation) {
    if (sharpen < 0.0f) sharpen = 0.0f; if (sharpen > 1.0f) sharpen = 1.0f;
    if (denoise < 0.0f) denoise = 0.0f; if (denoise > 1.0f) denoise = 1.0f;
    if (temporal < 0.0f) temporal = 0.0f; if (temporal > 1.0f) temporal = 1.0f;
    if (gamma < 0.2f) gamma = 0.2f; if (gamma > 3.0f) gamma = 3.0f;
    if (contrast < 0.0f) contrast = 0.0f; if (contrast > 2.0f) contrast = 2.0f;
    if (saturation < 0.0f) saturation = 0.0f; if (saturation > 2.0f) saturation = 2.0f;

    pthread_mutex_lock(&g_pp_mu);
    g_pp_params.sharpen    = sharpen;
    g_pp_params.denoise    = denoise;
    g_pp_params.temporal   = temporal;
    g_pp_params.gamma      = gamma;
    g_pp_params.contrast   = contrast;
    g_pp_params.saturation = saturation;
    pthread_mutex_unlock(&g_pp_mu);

    int wasEnabled = atomic_exchange(&g_pp_enabled, enabled ? 1 : 0);
    if (enabled && !wasEnabled) {
        // Fresh enable: any history image left over from a previous session
        // at the same resolution holds a stale frame -- skip temporal blend
        // for one frame so it can't ghost in stale content.
        atomic_store(&g_pp_primed, 0);
    }
    // Nudge the render thread awake so a paused/cursor-only stream picks up
    // the new settings immediately instead of waiting for the next real frame.
    if (g_pipe_w >= 0) { char c = 1; write(g_pipe_w, &c, 1); }
}

#endif // __ANDROID__
