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

// ─── JNI bridge (cached class / JVM) ─────────────────────────────────────────

static JavaVM *g_jvm           = NULL;
static jclass  g_cls_vob       = NULL; // com/usbridge/client/VulkanOverlayBridge
static jclass  g_cls_haptic    = NULL; // com/usbridge/client/HapticBridge

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

static int vk_create_instance(void) {
    const char *exts[] = {
        VK_KHR_SURFACE_EXTENSION_NAME,
        VK_KHR_ANDROID_SURFACE_EXTENSION_NAME,
    };
    VkInstanceCreateInfo ci = { VK_STRUCTURE_TYPE_INSTANCE_CREATE_INFO };
    ci.enabledExtensionCount   = 2;
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
    const char *dev_exts[] = { VK_KHR_SWAPCHAIN_EXTENSION_NAME };
    VkDeviceCreateInfo dci = { VK_STRUCTURE_TYPE_DEVICE_CREATE_INFO };
    dci.queueCreateInfoCount    = 1;
    dci.pQueueCreateInfos       = &qci;
    dci.enabledExtensionCount   = 1;
    dci.ppEnabledExtensionNames = dev_exts;
    if (vkCreateDevice(g_pdev, &dci, NULL, &g_dev) != VK_SUCCESS) return 0;
    vkGetDeviceQueue(g_dev, g_qfam, 0, &g_queue);
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

// ─── render one frame ─────────────────────────────────────────────────────────

static int vk_render_frame(uint8_t *pixels, int fw, int fh, int fs) {
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

    size_t row = (size_t)fw * 4;
    if ((size_t)fs == row) {
        memcpy(g_stage_ptr, pixels, frame_sz);
    } else {
        uint8_t *dst = (uint8_t *)g_stage_ptr;
        for (int y = 0; y < fh; y++)
            memcpy(dst + (size_t)y * row, pixels + (size_t)y * (size_t)fs, row);
    }

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

static double mono_sec_vk(void) {
    struct timespec ts;
    clock_gettime(CLOCK_MONOTONIC, &ts);
    return (double)ts.tv_sec + (double)ts.tv_nsec * 1e-9;
}

// ─── render thread ────────────────────────────────────────────────────────────

static void *vk_render_thread(void *unused) {
    (void)unused;
    VLOGI("render thread started");
    uint8_t *frame_buf = NULL;
    size_t frame_buf_sz = 0;
    int last_fw = 0, last_fh = 0, last_fs = 0;
    int has_last_frame = 0;

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
        pthread_mutex_lock(&g_mu);
        if (g_ready && g_buf) {
            fw = g_fw; fh = g_fh; fs = g_fs;
            size_t sz = (size_t)fh * (size_t)fs;
            if (sz > frame_buf_sz) {
                free(frame_buf);
                frame_buf = malloc(sz);
                frame_buf_sz = frame_buf ? sz : 0;
            }
            if (frame_buf) {
                memcpy(frame_buf, g_buf, sz);
                got_frame = 1;
            }
            g_ready = 0;
        }
        pthread_mutex_unlock(&g_mu);

        int cursor_dirty = atomic_exchange(&g_cursor_dirty, 0);

        int is_video_frame = 0;
        if (got_frame) {
            // Remember last frame dimensions for cursor-only redraws.
            last_fw = fw; last_fh = fh; last_fs = fs;
            has_last_frame = 1;
            is_video_frame = 1;
        } else if (cursor_dirty && has_last_frame) {
            // Cursor moved but no new video frame — redraw last frame with updated cursor.
            fw = last_fw; fh = last_fh; fs = last_fs;
            got_frame = 1;
        }

        if (!got_frame) continue;

        if (vk_render_frame(frame_buf, fw, fh, fs)) {
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
                VLOGI("rendered %lld frames, submitted %lld, fps=%.1f",
                      (long long)g_rendered, (long long)g_submitted, (double)g_stat_fps);
            }
        }
    }
    free(frame_buf);
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
        if (g_tex)     { vkDestroyImage(g_dev, g_tex, NULL);   g_tex     = VK_NULL_HANDLE; }
        if (g_tex_mem) { vkFreeMemory(g_dev, g_tex_mem, NULL); g_tex_mem = VK_NULL_HANDLE; }
        g_tex_w = 0; g_tex_h = 0;
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
    pthread_mutex_unlock(&g_mu);
    g_fps_n = 0; g_fps_t0 = 0.0; g_stat_fps = 0.0f; g_stat_ready = 0;
}

// ─── Public C API ─────────────────────────────────────────────────────────────

void android_vk_set_jvm(JavaVM *jvm, jobject ctx) {
    if (g_jvm != NULL && g_cls_vob != NULL) return;
    g_jvm = jvm;
    int nd; JNIEnv *env = get_env(&nd);
    if (!env) return;

    jclass cls = (*env)->FindClass(env, "com/usbridge/client/VulkanOverlayBridge");
    if (!cls || (*env)->ExceptionCheck(env)) {
        (*env)->ExceptionClear(env);
        VLOGI("android_vk_set_jvm: fallback to ClassLoader for VulkanOverlayBridge");
        if (ctx) {
            jclass ctxCls = (*env)->GetObjectClass(env, ctx);
            jmethodID getCL = (*env)->GetMethodID(env, ctxCls, "getClassLoader", "()Ljava/lang/ClassLoader;");
            jobject cl = (*env)->CallObjectMethod(env, ctx, getCL);
            jclass clCls = (*env)->FindClass(env, "java/lang/ClassLoader");
            jmethodID loadCls = (*env)->GetMethodID(env, clCls, "loadClass", "(Ljava/lang/String;)Ljava/lang/Class;");
            jstring name = (*env)->NewStringUTF(env, "com.usbridge.client.VulkanOverlayBridge");
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
    jclass hcls = (*env)->FindClass(env, "com/usbridge/client/HapticBridge");
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

#endif // __ANDROID__
