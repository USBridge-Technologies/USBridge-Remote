// vk_video_impl_linux.c — Vulkan overlay video renderer for Linux (X11 / XWayland).
//
// Architecture:
//   • Child X11 Window created directly over the parent Fyne/GLFW window.
//   • VkXlibSurfaceKHR on the child window.
//   • VkSwapchainKHR with IMMEDIATE (or MAILBOX/FIFO fallback) present mode.
//   • Render thread: staging buffer → VkImage (sampled texture) → blit to swapchain.
//   • Frame queue: capacity 1, drop-on-full (always-latest semantics).
//   • Fallback: on VK init failure Go side falls back to GLX renderer.
//
// Thread safety:
//   • vk_video_create / vk_video_destroy / vk_video_update_frame / vk_video_set_hidden
//     — called from CGO goroutines; all X11 calls confined to these (no X11 from render thread).
//   • vk_video_try_submit — called from decoder thread; protected by g_mu.
//   • Render thread: pure Vulkan only, no X11 calls.

#if defined(__linux__) && !defined(__ANDROID__)

#define VK_USE_PLATFORM_XLIB_KHR
#include <vulkan/vulkan.h>
#include <vulkan/vulkan_xlib.h>
#include <X11/Xlib.h>
#include <pthread.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include <stdio.h>
#include <stdatomic.h>
#include <time.h>
#include <unistd.h>

#include "shader_arrays.h" // g_ycbcr_crop_vert_spv / g_ycbcr_frag_spv (zero-copy dma-buf path)

extern void goVKLog(char *msg, int level);

// g_pipe_{r,w} is a self-pipe used purely to wake select() (or break the
// render thread out of it on teardown) -- see gl_video_impl_linux.c's
// identical helper for why write()/read()'s return value is routed through
// a variable instead of discarded directly (both are warn_unused_result).
static void pipe_wake(int fd, char c) {
    ssize_t n = write(fd, &c, 1);
    (void)n;
}
static void pipe_drain(int fd) {
    char buf[64];
    ssize_t n = read(fd, buf, sizeof(buf));
    (void)n;
}

// ─── X11 window state (CGO thread only) ──────────────────────────────────────

static Display *g_dpy        = NULL;
static Window   g_win        = 0;
static Window   g_parent_win = 0;

// Desired overlay rect — updated atomically; render thread uses for swapchain recreation.
static atomic_int g_dst_x, g_dst_y, g_dst_w, g_dst_h;

// Hide flag: set by vk_video_set_hidden(); applied in vk_video_update_frame() (CGO thread).
static volatile atomic_int g_hidden;
static int g_win_visible = 1; // tracks current XMapWindow / XUnmapWindow state

// Swapchain uses BGRA byte order — input RGBA pixels need R/B swap before upload.
static int g_tex_is_bgra = 0;

// ─── Vulkan state ─────────────────────────────────────────────────────────────

static VkInstance               g_inst         = VK_NULL_HANDLE;
static VkPhysicalDevice         g_pdev         = VK_NULL_HANDLE;
static VkDevice                 g_dev          = VK_NULL_HANDLE;
static VkQueue                  g_queue        = VK_NULL_HANDLE;
static uint32_t                 g_qfam         = 0;
static VkSurfaceKHR             g_surf         = VK_NULL_HANDLE;
static VkSwapchainKHR           g_swap         = VK_NULL_HANDLE;
static uint32_t                 g_swap_count   = 0;
static VkImage                 *g_swap_imgs    = NULL;
static VkImageView             *g_swap_views   = NULL;
static VkFormat                 g_swap_fmt     = VK_FORMAT_UNDEFINED;
static VkExtent2D               g_swap_ext     = {0, 0};
static int                      g_have_vk13    = 0; // set by vk_create_instance

// Staging buffer (host-visible, coherent).
static VkBuffer                 g_stage_buf    = VK_NULL_HANDLE;
static VkDeviceMemory           g_stage_mem    = VK_NULL_HANDLE;
static void                    *g_stage_ptr    = NULL;
static VkDeviceSize             g_stage_sz     = 0;

// Device-local sampled image (upload target, blit source).
static VkImage                  g_tex          = VK_NULL_HANDLE;
static VkDeviceMemory           g_tex_mem      = VK_NULL_HANDLE;
static int                      g_tex_w        = 0, g_tex_h = 0;

// Synchronisation
static VkCommandPool            g_cmdpool      = VK_NULL_HANDLE;
static VkCommandBuffer          g_cmdbuf       = VK_NULL_HANDLE;
static VkFence                  g_fence        = VK_NULL_HANDLE;
static VkSemaphore              g_img_sem      = VK_NULL_HANDLE;
static VkSemaphore              g_rnd_sem      = VK_NULL_HANDLE;

// ─── Render thread state ──────────────────────────────────────────────────────

static volatile atomic_int g_active;

// Frame slot — capacity 1, drop-on-full.
static uint8_t         *g_buf    = NULL;
static size_t           g_buf_sz = 0;
static int              g_fw = 0, g_fh = 0, g_fs = 0;
static volatile int     g_ready  = 0;

// Zero-copy dma-buf frame slot — parallel to g_buf/g_ready above, capacity 1,
// drop-on-full. Only one of {g_ready, g_dmabuf_ready} is meaningful at a
// time: a session either runs decode entirely in software/unsupported-hw
// (RGBA path, g_ready) or on VAAPI/QSV with a Vulkan-capable device
// (dma-buf path, g_dmabuf_ready) -- see moonlight_cgo_linux.go's
// deliver_frame for which one a given frame takes. fd/release_ctx/
// release_fn ownership transfers to this struct once queued (see
// vk_video_try_submit_dmabuf's doc comment).
typedef struct {
    int      fd;
    uint64_t modifier;
    int      surf_w, surf_h; // VAAPI/QSV surface's allocated (padded) extent
    int      vis_w, vis_h;   // negotiated/visible frame extent (for UV crop)
    uint32_t plane_count;
    uint32_t offset0, pitch0, offset1, pitch1;
    void     *release_ctx;
    void    (*release_fn)(void*);
} DmabufFrame;
static DmabufFrame      g_dmabuf_pending;
static volatile int     g_dmabuf_ready = 0;

static pthread_mutex_t  g_mu     = PTHREAD_MUTEX_INITIALIZER;
static pthread_t        g_thread = 0;
static int              g_pipe_r = -1, g_pipe_w = -1;

// Stats
static volatile long long g_submitted = 0, g_rendered = 0;
static volatile long long g_fps_n = 0;
static volatile double    g_fps_t0 = 0.0;
static volatile long long g_stat_rendered = 0, g_stat_submitted = 0;
static volatile float     g_stat_fps = 0.0f;
static volatile int       g_stat_fps_ready = 0, g_stat_first = 0;
static volatile int       g_stat_fw = 0, g_stat_fh = 0;
static volatile float     g_stat_max_gap_ms = 0.0f;
static volatile double    g_last_blit_ts = 0.0;

// Diagnostics
static volatile long long g_render_hb    = 0;
static volatile int       g_render_stage = 0;

// ─── helpers ──────────────────────────────────────────────────────────────────

static double mono_sec(void) {
    struct timespec ts;
    clock_gettime(CLOCK_MONOTONIC, &ts);
    return (double)ts.tv_sec + (double)ts.tv_nsec * 1e-9;
}

static uint32_t vk_find_mem(VkPhysicalDeviceMemoryProperties *mp,
                             uint32_t type_bits, VkMemoryPropertyFlags props) {
    for (uint32_t i = 0; i < mp->memoryTypeCount; i++)
        if ((type_bits & (1u << i)) &&
            (mp->memoryTypes[i].propertyFlags & props) == props)
            return i;
    return UINT32_MAX;
}

// ─── Vulkan init helpers ──────────────────────────────────────────────────────

static int vk_create_instance(void) {
    const char *exts[] = {
        VK_KHR_SURFACE_EXTENSION_NAME,
        VK_KHR_XLIB_SURFACE_EXTENSION_NAME,
    };
    // apiVersion 1.3: the zero-copy dma-buf render path needs
    // VkSamplerYcbcrConversion and dynamic rendering (vkCmdBeginRendering),
    // both core since 1.3 -- requesting a plain 1.0 instance (the previous
    // default here) leaves those core entry points/features unavailable per
    // spec even though the underlying driver (checked: Mesa ANV 1.4.x)
    // supports them.
    VkApplicationInfo appInfo = { VK_STRUCTURE_TYPE_APPLICATION_INFO };
    appInfo.apiVersion = VK_API_VERSION_1_3;
    VkInstanceCreateInfo ci = { VK_STRUCTURE_TYPE_INSTANCE_CREATE_INFO };
    ci.pApplicationInfo       = &appInfo;
    ci.enabledExtensionCount   = 2;
    ci.ppEnabledExtensionNames = exts;
    if (vkCreateInstance(&ci, NULL, &g_inst) == VK_SUCCESS) {
        g_have_vk13 = 1;
        return 1;
    }
    // Fall back to a plain 1.0 instance on drivers too old for 1.3 --
    // zero-copy dma-buf render stays disabled (vk_ycbcr_ensure_pipeline
    // checks g_have_vk13), the RGBA blit path still works unchanged.
    g_have_vk13 = 0;
    ci.pApplicationInfo = NULL;
    return vkCreateInstance(&ci, NULL, &g_inst) == VK_SUCCESS;
}

static int vk_select_device(void) {
    uint32_t n = 0;
    vkEnumeratePhysicalDevices(g_inst, &n, NULL);
    if (!n) return 0;
    VkPhysicalDevice *devs = malloc(n * sizeof(VkPhysicalDevice));
    vkEnumeratePhysicalDevices(g_inst, &n, devs);

    VkPhysicalDevice best = VK_NULL_HANDLE;
    int best_score = -1;
    for (uint32_t i = 0; i < n; i++) {
        VkPhysicalDeviceProperties pr;
        vkGetPhysicalDeviceProperties(devs[i], &pr);
        int score = (pr.deviceType == VK_PHYSICAL_DEVICE_TYPE_DISCRETE_GPU)   ? 2
                  : (pr.deviceType == VK_PHYSICAL_DEVICE_TYPE_INTEGRATED_GPU) ? 1 : 0;
        uint32_t qn = 0;
        vkGetPhysicalDeviceQueueFamilyProperties(devs[i], &qn, NULL);
        VkQueueFamilyProperties *qp = malloc(qn * sizeof(*qp));
        vkGetPhysicalDeviceQueueFamilyProperties(devs[i], &qn, qp);
        int has_gfx = 0;
        for (uint32_t j = 0; j < qn; j++)
            if (qp[j].queueFlags & VK_QUEUE_GRAPHICS_BIT) { has_gfx = 1; break; }
        free(qp);
        if (!has_gfx) continue;
        if (score > best_score) { best_score = score; best = devs[i]; }
    }
    free(devs);
    if (best == VK_NULL_HANDLE) return 0;
    g_pdev = best;

    uint32_t qn = 0;
    vkGetPhysicalDeviceQueueFamilyProperties(g_pdev, &qn, NULL);
    VkQueueFamilyProperties *qp = malloc(qn * sizeof(*qp));
    vkGetPhysicalDeviceQueueFamilyProperties(g_pdev, &qn, qp);
    for (uint32_t j = 0; j < qn; j++)
        if (qp[j].queueFlags & VK_QUEUE_GRAPHICS_BIT) { g_qfam = j; break; }
    free(qp);
    return 1;
}

// Extensions needed by the zero-copy VAAPI/QSV dma-buf render path (see
// vk_ycbcr_ensure_pipeline / vk_render_frame_dmabuf below). Enabled only if
// the device actually reports them and the instance is 1.3+ (for core
// VkSamplerYcbcrConversion + dynamic rendering) -- on a driver missing any
// of these, g_zerocopy_supported stays 0 and frames just take the existing
// RGBA blit path, same as before this feature existed.
static const char *kZeroCopyExts[] = {
    VK_EXT_IMAGE_DRM_FORMAT_MODIFIER_EXTENSION_NAME,
    VK_KHR_EXTERNAL_MEMORY_FD_EXTENSION_NAME,
    VK_EXT_EXTERNAL_MEMORY_DMA_BUF_EXTENSION_NAME,
    VK_EXT_QUEUE_FAMILY_FOREIGN_EXTENSION_NAME,
};
#define N_ZEROCOPY_EXTS (int)(sizeof(kZeroCopyExts)/sizeof(kZeroCopyExts[0]))
static int g_zerocopy_supported = 0;

static int vk_create_device(void) {
    uint32_t navail = 0;
    vkEnumerateDeviceExtensionProperties(g_pdev, NULL, &navail, NULL);
    VkExtensionProperties *avail = malloc(navail * sizeof(*avail));
    vkEnumerateDeviceExtensionProperties(g_pdev, NULL, &navail, avail);

    const char *dev_exts[1 + N_ZEROCOPY_EXTS];
    uint32_t next = 0;
    dev_exts[next++] = VK_KHR_SWAPCHAIN_EXTENSION_NAME;

    int have_all_zc = g_have_vk13 ? 1 : 0;
    for (int i = 0; i < N_ZEROCOPY_EXTS && have_all_zc; i++) {
        int found = 0;
        for (uint32_t j = 0; j < navail; j++)
            if (strcmp(avail[j].extensionName, kZeroCopyExts[i]) == 0) { found = 1; break; }
        if (!found) have_all_zc = 0;
    }
    if (have_all_zc) {
        for (int i = 0; i < N_ZEROCOPY_EXTS; i++) dev_exts[next++] = kZeroCopyExts[i];
    }
    free(avail);
    g_zerocopy_supported = have_all_zc;

    float pri = 1.0f;
    VkDeviceQueueCreateInfo qci = { VK_STRUCTURE_TYPE_DEVICE_QUEUE_CREATE_INFO };
    qci.queueFamilyIndex = g_qfam;
    qci.queueCount       = 1;
    qci.pQueuePriorities = &pri;
    VkDeviceCreateInfo dci = { VK_STRUCTURE_TYPE_DEVICE_CREATE_INFO };
    dci.queueCreateInfoCount    = 1;
    dci.pQueueCreateInfos       = &qci;
    dci.enabledExtensionCount   = next;
    dci.ppEnabledExtensionNames = dev_exts;
    if (vkCreateDevice(g_pdev, &dci, NULL, &g_dev) != VK_SUCCESS) return 0;
    vkGetDeviceQueue(g_dev, g_qfam, 0, &g_queue);

    char msg[96];
    snprintf(msg, sizeof(msg), "zero-copy dma-buf render path: %s",
             g_zerocopy_supported ? "available" : "unavailable (falling back to RGBA blit)");
    goVKLog(msg, 0);
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
    // Prefer RGBA_UNORM (matches our RGBA input — no channel swap needed).
    // Fall back to BGRA_UNORM (common on Linux/Mesa); BGRA_SRGB is last resort
    // because it applies gamma correction which washes out the image.
    int best = 0;
    for (uint32_t i = 0; i < nfmt; i++) {
        int rank = 0;
        if (fmts[i].format == VK_FORMAT_R8G8B8A8_UNORM) rank = 3;
        else if (fmts[i].format == VK_FORMAT_R8G8B8A8_SRGB) rank = 2;
        else if (fmts[i].format == VK_FORMAT_B8G8R8A8_UNORM) rank = 1;
        if (rank > best) { best = rank; g_swap_fmt = fmts[i].format; csp = fmts[i].colorSpace; }
    }
    // BGRA_SRGB (rank 0) stays as fallback only if nothing better found.
    g_tex_is_bgra = (g_swap_fmt == VK_FORMAT_B8G8R8A8_UNORM ||
                     g_swap_fmt == VK_FORMAT_B8G8R8A8_SRGB);
    {
        const char *fn = (g_swap_fmt == VK_FORMAT_R8G8B8A8_UNORM) ? "R8G8B8A8_UNORM"
                       : (g_swap_fmt == VK_FORMAT_R8G8B8A8_SRGB)  ? "R8G8B8A8_SRGB"
                       : (g_swap_fmt == VK_FORMAT_B8G8R8A8_UNORM) ? "B8G8R8A8_UNORM"
                       : (g_swap_fmt == VK_FORMAT_B8G8R8A8_SRGB)  ? "B8G8R8A8_SRGB"
                       : "other";
        char msg[96]; snprintf(msg, sizeof(msg), "swapchain format: %s bgra_swap=%d", fn, g_tex_is_bgra);
        goVKLog(msg, 0);
    }
    free(fmts);

    // Present mode: prefer IMMEDIATE (no compositor blocking) → MAILBOX → FIFO_RELAXED → FIFO.
    uint32_t npm = 0;
    vkGetPhysicalDeviceSurfacePresentModesKHR(g_pdev, g_surf, &npm, NULL);
    VkPresentModeKHR *pms = malloc(npm * sizeof(*pms));
    vkGetPhysicalDeviceSurfacePresentModesKHR(g_pdev, g_surf, &npm, pms);
    VkPresentModeKHR pm = VK_PRESENT_MODE_FIFO_KHR;
    for (uint32_t i = 0; i < npm; i++)
        if (pms[i] == VK_PRESENT_MODE_IMMEDIATE_KHR) { pm = pms[i]; break; }
    if (pm != VK_PRESENT_MODE_IMMEDIATE_KHR) {
        for (uint32_t i = 0; i < npm; i++) {
            if (pms[i] == VK_PRESENT_MODE_MAILBOX_KHR)      { pm = pms[i]; break; }
            if (pms[i] == VK_PRESENT_MODE_FIFO_RELAXED_KHR) { pm = pms[i]; }
        }
    }
    free(pms);

    g_swap_ext.width  = (uint32_t)(w > 0 ? w : (int)caps.currentExtent.width);
    g_swap_ext.height = (uint32_t)(h > 0 ? h : (int)caps.currentExtent.height);
    if (g_swap_ext.width  == 0) g_swap_ext.width  = 1;
    if (g_swap_ext.height == 0) g_swap_ext.height = 1;

    uint32_t imgCount = caps.minImageCount + 1;
    if (imgCount < 3) imgCount = 3;
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
    sci.preTransform     = caps.currentTransform;
    sci.compositeAlpha   = VK_COMPOSITE_ALPHA_OPAQUE_BIT_KHR;
    sci.presentMode      = pm;
    sci.clipped          = VK_TRUE;

    {
        const char *pm_name = (pm == VK_PRESENT_MODE_IMMEDIATE_KHR)    ? "IMMEDIATE"
                            : (pm == VK_PRESENT_MODE_MAILBOX_KHR)      ? "MAILBOX"
                            : (pm == VK_PRESENT_MODE_FIFO_RELAXED_KHR) ? "FIFO_RELAXED"
                                                                        : "FIFO";
        char msg[80];
        snprintf(msg, sizeof(msg), "swapchain present mode: %s (%d images)", pm_name, (int)imgCount);
        goVKLog(msg, 0);
    }
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
    if (!g_dev || !g_surf) return 0;
    vkDeviceWaitIdle(g_dev);
    vk_destroy_swapchain();
    int w = atomic_load(&g_dst_w);
    int h = atomic_load(&g_dst_h);
    if (w <= 0) w = 1;
    if (h <= 0) h = 1;
    char msg[80];
    snprintf(msg, sizeof(msg), "vk: recreating swapchain %dx%d", w, h);
    goVKLog(msg, 0);
    int ok = vk_create_swapchain(w, h);
    if (!ok) goVKLog("vk: swapchain recreation failed", 2);
    return ok;
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
    // Match texture format to swapchain channel order to avoid blit mis-interpretation.
    ici.format      = g_tex_is_bgra ? VK_FORMAT_B8G8R8A8_UNORM : VK_FORMAT_R8G8B8A8_UNORM;
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

// ─── zero-copy VAAPI/QSV dma-buf render path ─────────────────────────────────
// moonlight_cgo_linux.go's deliver_frame maps a decoded QSV/VAAPI frame to
// its underlying VASurfaceID and exports that surface as a dma-buf (see
// vaExportSurfaceHandle) without ever touching the pixels on the CPU. This
// path imports that dma-buf directly as a VkImage (NV12, multi-planar) and
// samples it into the swapchain through a real VkSamplerYcbcrConversion --
// the GPU does the YCbCr->RGB conversion during sampling, so decode output
// never leaves GPU memory. Falls back to the RGBA blit path above whenever
// g_zerocopy_supported is 0 (older driver/instance) or a given frame's
// surface can't be exported/imported for any reason (see deliver_frame's
// fallback branch in moonlight_cgo_linux.go).
//
// Verified feasible end-to-end on this exact Intel ADL iGPU (Mesa ANV)
// before writing this: h264_qsv decode -> av_hwframe_map to a VAAPI-derived
// frames ctx -> vaExportSurfaceHandle (NV12, Y-tiled modifier
// 0x0100000000000002) -> vkCreateImage with
// VK_IMAGE_TILING_DRM_FORMAT_MODIFIER_EXT + that exact modifier ->
// vkAllocateMemory importing the dma-buf fd -> vkBindImageMemory, all
// VK_SUCCESS. ANV's vkGetPhysicalDeviceFormatProperties2 for
// VK_FORMAT_G8_B8R8_2PLANE_420_UNORM confirmed that modifier is one of the
// ones it advertises support for, so this isn't relying on undefined
// driver behavior.
//
// Sync note: VAAPI decode and this Vulkan device are different APIs with no
// shared timeline, so instead of a semaphore we sync on the CPU --
// moonlight_cgo_linux.go calls vaSyncSurface() (which blocks until the
// VAAPI decode job has retired) before exporting the dma-buf, so by the
// time Vulkan ever touches the imported memory, decode is unconditionally
// finished. No memory is copied by this wait, only synchronized.

static VkSamplerYcbcrConversion g_yconv        = VK_NULL_HANDLE;
static VkSampler                g_ysampler     = VK_NULL_HANDLE;
static VkDescriptorSetLayout    g_ydsl         = VK_NULL_HANDLE;
static VkPipelineLayout         g_yplayout     = VK_NULL_HANDLE;
static VkPipeline               g_ypipeline    = VK_NULL_HANDLE;
static VkDescriptorPool         g_ydpool       = VK_NULL_HANDLE;
static VkDescriptorSet          g_ydset        = VK_NULL_HANDLE;
static int                      g_ypipeline_ok = 0; // 0=not tried, 1=ready, -1=failed (don't retry)

// Previous zero-copy frame's per-frame resources (fresh VkImage/VkDeviceMemory
// /VkImageView every frame, since the underlying VASurfaceID's contents
// change every frame -- unlike the RGBA path's reused g_tex, there is no
// benefit to keeping these around). Torn down (and release_fn called) once
// the NEXT frame's fence wait below confirms this GPU work has retired --
// same deferred-release timing as vk_video_impl_windows.c's
// g_vkf_prev_release_ctx/fn.
static VkImage        g_dmabuf_prev_img  = VK_NULL_HANDLE;
static VkDeviceMemory g_dmabuf_prev_mem  = VK_NULL_HANDLE;
static VkImageView    g_dmabuf_prev_view = VK_NULL_HANDLE;
static void           *g_dmabuf_prev_release_ctx = NULL;
static void          (*g_dmabuf_prev_release_fn)(void*) = NULL;

static VkShaderModule vk_shader_from_spv(const uint32_t *code, size_t code_size) {
    VkShaderModuleCreateInfo ci = { VK_STRUCTURE_TYPE_SHADER_MODULE_CREATE_INFO };
    ci.codeSize = code_size;
    ci.pCode    = code;
    VkShaderModule mod = VK_NULL_HANDLE;
    vkCreateShaderModule(g_dev, &ci, NULL, &mod);
    return mod;
}

// vk_ycbcr_ensure_pipeline lazily creates the fixed NV12 sampler-conversion
// pipeline (single format, single descriptor set reused/repointed every
// frame via vkUpdateDescriptorSets -- unlike Windows' vk_video_impl, our
// source VkImage is freshly created every frame anyway, so there is no
// image-handle cache worth keeping). Returns 1 once ready, 0 on failure
// (permanent -- doesn't retry).
static int vk_ycbcr_ensure_pipeline(void) {
    if (g_ypipeline_ok) return g_ypipeline_ok > 0;
    g_ypipeline_ok = -1; // assume failure; flipped to 1 at the end on success

    VkSamplerYcbcrConversionCreateInfo convCI = { VK_STRUCTURE_TYPE_SAMPLER_YCBCR_CONVERSION_CREATE_INFO };
    convCI.format = VK_FORMAT_G8_B8R8_2PLANE_420_UNORM;
    convCI.ycbcrModel = VK_SAMPLER_YCBCR_MODEL_CONVERSION_YCBCR_601;
    convCI.ycbcrRange = VK_SAMPLER_YCBCR_RANGE_ITU_NARROW;
    convCI.components.r = VK_COMPONENT_SWIZZLE_IDENTITY;
    convCI.components.g = VK_COMPONENT_SWIZZLE_IDENTITY;
    convCI.components.b = VK_COMPONENT_SWIZZLE_IDENTITY;
    convCI.components.a = VK_COMPONENT_SWIZZLE_IDENTITY;
    convCI.xChromaOffset = VK_CHROMA_LOCATION_COSITED_EVEN;
    convCI.yChromaOffset = VK_CHROMA_LOCATION_COSITED_EVEN;
    convCI.chromaFilter = VK_FILTER_LINEAR;
    if (vkCreateSamplerYcbcrConversion(g_dev, &convCI, NULL, &g_yconv) != VK_SUCCESS) {
        goVKLog("vk_ycbcr_ensure_pipeline: vkCreateSamplerYcbcrConversion failed", 2);
        return 0;
    }

    VkSamplerYcbcrConversionInfo convInfo = { VK_STRUCTURE_TYPE_SAMPLER_YCBCR_CONVERSION_INFO };
    convInfo.conversion = g_yconv;
    VkSamplerCreateInfo sampCI = { VK_STRUCTURE_TYPE_SAMPLER_CREATE_INFO, &convInfo };
    sampCI.magFilter = VK_FILTER_LINEAR;
    sampCI.minFilter = VK_FILTER_LINEAR;
    sampCI.addressModeU = VK_SAMPLER_ADDRESS_MODE_CLAMP_TO_EDGE;
    sampCI.addressModeV = VK_SAMPLER_ADDRESS_MODE_CLAMP_TO_EDGE;
    sampCI.addressModeW = VK_SAMPLER_ADDRESS_MODE_CLAMP_TO_EDGE;
    if (vkCreateSampler(g_dev, &sampCI, NULL, &g_ysampler) != VK_SUCCESS) {
        goVKLog("vk_ycbcr_ensure_pipeline: vkCreateSampler failed", 2);
        return 0;
    }

    VkDescriptorSetLayoutBinding binding = {0};
    binding.binding = 0;
    binding.descriptorType = VK_DESCRIPTOR_TYPE_COMBINED_IMAGE_SAMPLER;
    binding.descriptorCount = 1;
    binding.stageFlags = VK_SHADER_STAGE_FRAGMENT_BIT;
    binding.pImmutableSamplers = &g_ysampler;
    VkDescriptorSetLayoutCreateInfo dslCI = { VK_STRUCTURE_TYPE_DESCRIPTOR_SET_LAYOUT_CREATE_INFO };
    dslCI.bindingCount = 1; dslCI.pBindings = &binding;
    if (vkCreateDescriptorSetLayout(g_dev, &dslCI, NULL, &g_ydsl) != VK_SUCCESS) {
        goVKLog("vk_ycbcr_ensure_pipeline: vkCreateDescriptorSetLayout failed", 2);
        return 0;
    }

    VkPushConstantRange pcr = { VK_SHADER_STAGE_VERTEX_BIT, 0, sizeof(float) * 2 };
    VkPipelineLayoutCreateInfo plCI = { VK_STRUCTURE_TYPE_PIPELINE_LAYOUT_CREATE_INFO };
    plCI.setLayoutCount = 1; plCI.pSetLayouts = &g_ydsl;
    plCI.pushConstantRangeCount = 1; plCI.pPushConstantRanges = &pcr;
    if (vkCreatePipelineLayout(g_dev, &plCI, NULL, &g_yplayout) != VK_SUCCESS) {
        goVKLog("vk_ycbcr_ensure_pipeline: vkCreatePipelineLayout failed", 2);
        return 0;
    }

    VkDescriptorPoolSize poolSize = { VK_DESCRIPTOR_TYPE_COMBINED_IMAGE_SAMPLER, 1 };
    VkDescriptorPoolCreateInfo poolCI = { VK_STRUCTURE_TYPE_DESCRIPTOR_POOL_CREATE_INFO };
    poolCI.maxSets = 1; poolCI.poolSizeCount = 1; poolCI.pPoolSizes = &poolSize;
    if (vkCreateDescriptorPool(g_dev, &poolCI, NULL, &g_ydpool) != VK_SUCCESS) {
        goVKLog("vk_ycbcr_ensure_pipeline: vkCreateDescriptorPool failed", 2);
        return 0;
    }
    VkDescriptorSetAllocateInfo dsai = { VK_STRUCTURE_TYPE_DESCRIPTOR_SET_ALLOCATE_INFO };
    dsai.descriptorPool = g_ydpool; dsai.descriptorSetCount = 1; dsai.pSetLayouts = &g_ydsl;
    if (vkAllocateDescriptorSets(g_dev, &dsai, &g_ydset) != VK_SUCCESS) {
        goVKLog("vk_ycbcr_ensure_pipeline: vkAllocateDescriptorSets failed", 2);
        return 0;
    }

    VkShaderModule vs = vk_shader_from_spv(g_ycbcr_crop_vert_spv, sizeof(g_ycbcr_crop_vert_spv));
    VkShaderModule fs = vk_shader_from_spv(g_ycbcr_frag_spv, sizeof(g_ycbcr_frag_spv));
    if (!vs || !fs) {
        if (vs) vkDestroyShaderModule(g_dev, vs, NULL);
        if (fs) vkDestroyShaderModule(g_dev, fs, NULL);
        goVKLog("vk_ycbcr_ensure_pipeline: shader module creation failed", 2);
        return 0;
    }
    VkPipelineShaderStageCreateInfo stages[2] = {0};
    stages[0].sType = VK_STRUCTURE_TYPE_PIPELINE_SHADER_STAGE_CREATE_INFO;
    stages[0].stage = VK_SHADER_STAGE_VERTEX_BIT; stages[0].module = vs; stages[0].pName = "main";
    stages[1].sType = VK_STRUCTURE_TYPE_PIPELINE_SHADER_STAGE_CREATE_INFO;
    stages[1].stage = VK_SHADER_STAGE_FRAGMENT_BIT; stages[1].module = fs; stages[1].pName = "main";

    VkPipelineVertexInputStateCreateInfo vi = { VK_STRUCTURE_TYPE_PIPELINE_VERTEX_INPUT_STATE_CREATE_INFO };
    VkPipelineInputAssemblyStateCreateInfo ia = { VK_STRUCTURE_TYPE_PIPELINE_INPUT_ASSEMBLY_STATE_CREATE_INFO };
    ia.topology = VK_PRIMITIVE_TOPOLOGY_TRIANGLE_LIST;
    VkPipelineViewportStateCreateInfo vpState = { VK_STRUCTURE_TYPE_PIPELINE_VIEWPORT_STATE_CREATE_INFO };
    vpState.viewportCount = 1; vpState.scissorCount = 1; // dynamic
    VkDynamicState dynStates[2] = { VK_DYNAMIC_STATE_VIEWPORT, VK_DYNAMIC_STATE_SCISSOR };
    VkPipelineDynamicStateCreateInfo dynCI = { VK_STRUCTURE_TYPE_PIPELINE_DYNAMIC_STATE_CREATE_INFO };
    dynCI.dynamicStateCount = 2; dynCI.pDynamicStates = dynStates;
    VkPipelineRasterizationStateCreateInfo rs = { VK_STRUCTURE_TYPE_PIPELINE_RASTERIZATION_STATE_CREATE_INFO };
    rs.polygonMode = VK_POLYGON_MODE_FILL; rs.cullMode = VK_CULL_MODE_NONE; rs.lineWidth = 1.0f;
    VkPipelineMultisampleStateCreateInfo ms = { VK_STRUCTURE_TYPE_PIPELINE_MULTISAMPLE_STATE_CREATE_INFO };
    ms.rasterizationSamples = VK_SAMPLE_COUNT_1_BIT;
    VkPipelineColorBlendAttachmentState cba = {0};
    cba.colorWriteMask = VK_COLOR_COMPONENT_R_BIT | VK_COLOR_COMPONENT_G_BIT | VK_COLOR_COMPONENT_B_BIT | VK_COLOR_COMPONENT_A_BIT;
    VkPipelineColorBlendStateCreateInfo cb = { VK_STRUCTURE_TYPE_PIPELINE_COLOR_BLEND_STATE_CREATE_INFO };
    cb.attachmentCount = 1; cb.pAttachments = &cba;

    VkPipelineRenderingCreateInfo renderingCI = { VK_STRUCTURE_TYPE_PIPELINE_RENDERING_CREATE_INFO };
    renderingCI.colorAttachmentCount = 1; renderingCI.pColorAttachmentFormats = &g_swap_fmt;

    VkGraphicsPipelineCreateInfo pipeCI = { VK_STRUCTURE_TYPE_GRAPHICS_PIPELINE_CREATE_INFO, &renderingCI };
    pipeCI.stageCount = 2; pipeCI.pStages = stages;
    pipeCI.pVertexInputState = &vi; pipeCI.pInputAssemblyState = &ia;
    pipeCI.pViewportState = &vpState; pipeCI.pRasterizationState = &rs;
    pipeCI.pMultisampleState = &ms; pipeCI.pColorBlendState = &cb;
    pipeCI.pDynamicState = &dynCI;
    pipeCI.layout = g_yplayout;
    VkResult pr = vkCreateGraphicsPipelines(g_dev, VK_NULL_HANDLE, 1, &pipeCI, NULL, &g_ypipeline);
    vkDestroyShaderModule(g_dev, vs, NULL);
    vkDestroyShaderModule(g_dev, fs, NULL);
    if (pr != VK_SUCCESS) {
        goVKLog("vk_ycbcr_ensure_pipeline: vkCreateGraphicsPipelines failed", 2);
        return 0;
    }

    g_ypipeline_ok = 1;
    goVKLog("vk: zero-copy dma-buf NV12 pipeline ready", 0);
    return 1;
}

// vk_dmabuf_release_prev tears down the previous zero-copy frame's VkImage/
// memory/view and calls its release_fn (freeing the decoder's AVFrame ref,
// letting VAAPI reclaim that surface). Safe to call once the caller has
// confirmed (via fence wait) that no in-flight command buffer references
// these anymore -- see the call site in vk_render_frame_dmabuf.
static void vk_dmabuf_release_prev(void) {
    if (g_dmabuf_prev_view) { vkDestroyImageView(g_dev, g_dmabuf_prev_view, NULL); g_dmabuf_prev_view = VK_NULL_HANDLE; }
    if (g_dmabuf_prev_img)  { vkDestroyImage(g_dev, g_dmabuf_prev_img, NULL);       g_dmabuf_prev_img  = VK_NULL_HANDLE; }
    if (g_dmabuf_prev_mem)  { vkFreeMemory(g_dev, g_dmabuf_prev_mem, NULL);         g_dmabuf_prev_mem  = VK_NULL_HANDLE; }
    if (g_dmabuf_prev_release_fn) {
        g_dmabuf_prev_release_fn(g_dmabuf_prev_release_ctx);
        g_dmabuf_prev_release_fn  = NULL;
        g_dmabuf_prev_release_ctx = NULL;
    }
}

// vk_render_frame_dmabuf — zero-copy counterpart to vk_render_frame: imports
// the caller's dma-buf as a VkImage and samples it directly into the
// swapchain instead of blitting an uploaded RGBA staging texture. Takes
// ownership of f->fd and f->release_ctx/release_fn regardless of outcome
// (matches vk_video_try_submit_dmabuf's contract) -- callers must not touch
// either afterward.
static int vk_render_frame_dmabuf(DmabufFrame *f) {
    if (!g_dev || !g_swap) { close(f->fd); if (f->release_fn) f->release_fn(f->release_ctx); return 0; }
    if (!vk_ycbcr_ensure_pipeline()) {
        // Pipeline creation failed for a reason vk_create_device's
        // extension check couldn't catch (e.g. an unexpected format/
        // modifier combination on some other GPU) -- disable the
        // zero-copy path for the rest of this session so deliver_frame
        // falls back to the CPU path on every subsequent frame instead of
        // silently dropping frames forever (vk_video_zerocopy_supported()
        // is what it checks before ever calling here again).
        g_zerocopy_supported = 0;
        close(f->fd); if (f->release_fn) f->release_fn(f->release_ctx);
        return 0;
    }
    char dbg[128];

    uint32_t img_idx = 0;
    g_render_stage = 3;
    VkResult res = vkAcquireNextImageKHR(g_dev, g_swap, 3000000000ULL,
                                          g_img_sem, VK_NULL_HANDLE, &img_idx);
    if (res == VK_ERROR_OUT_OF_DATE_KHR) {
        g_render_stage = 7; vk_recreate_swapchain(); g_render_stage = 1;
        close(f->fd); if (f->release_fn) f->release_fn(f->release_ctx);
        return 0;
    }
    if (res != VK_SUCCESS && res != VK_SUBOPTIMAL_KHR) {
        snprintf(dbg, sizeof(dbg), "AcquireNextImage (dmabuf) failed res=%d", (int)res);
        goVKLog(dbg, 2);
        g_render_stage = 1;
        close(f->fd); if (f->release_fn) f->release_fn(f->release_ctx);
        return 0;
    }

    g_render_stage = 4;
    VkResult fence_res = vkWaitForFences(g_dev, 1, &g_fence, VK_TRUE, 2000000000ULL);
    if (fence_res == VK_TIMEOUT) {
        goVKLog("WaitForFences TIMEOUT 2s (dmabuf path) — GPU hang?", 2);
        vkResetFences(g_dev, 1, &g_fence);
        g_render_stage = 1;
        close(f->fd); if (f->release_fn) f->release_fn(f->release_ctx);
        return 0;
    }
    vkResetFences(g_dev, 1, &g_fence);

    // The fence wait above just confirmed the PREVIOUS zero-copy frame's
    // GPU read has fully retired -- safe to tear it down now (this frame's
    // own resources become "prev" further down, released on the call after
    // this one).
    vk_dmabuf_release_prev();

    // ---- import this frame's dma-buf as a VkImage ----
    VkFormat fmt = VK_FORMAT_G8_B8R8_2PLANE_420_UNORM; // NV12 8-bit -- the only format deliver_frame exports today
    VkSubresourceLayout planeLayouts[2] = {0};
    planeLayouts[0].offset = f->offset0; planeLayouts[0].rowPitch = f->pitch0;
    planeLayouts[1].offset = f->offset1; planeLayouts[1].rowPitch = f->pitch1;

    VkImageDrmFormatModifierExplicitCreateInfoEXT explicitInfo = {
        VK_STRUCTURE_TYPE_IMAGE_DRM_FORMAT_MODIFIER_EXPLICIT_CREATE_INFO_EXT
    };
    explicitInfo.drmFormatModifier = f->modifier;
    explicitInfo.drmFormatModifierPlaneCount = f->plane_count;
    explicitInfo.pPlaneLayouts = planeLayouts;

    VkExternalMemoryImageCreateInfo extMemImgInfo = {
        VK_STRUCTURE_TYPE_EXTERNAL_MEMORY_IMAGE_CREATE_INFO, &explicitInfo
    };
    extMemImgInfo.handleTypes = VK_EXTERNAL_MEMORY_HANDLE_TYPE_DMA_BUF_BIT_EXT;

    VkImageCreateInfo ici = { VK_STRUCTURE_TYPE_IMAGE_CREATE_INFO, &extMemImgInfo };
    ici.imageType = VK_IMAGE_TYPE_2D;
    ici.format = fmt;
    ici.extent = (VkExtent3D){ (uint32_t)f->surf_w, (uint32_t)f->surf_h, 1 };
    ici.mipLevels = 1; ici.arrayLayers = 1;
    ici.samples = VK_SAMPLE_COUNT_1_BIT;
    ici.tiling = VK_IMAGE_TILING_DRM_FORMAT_MODIFIER_EXT;
    ici.usage = VK_IMAGE_USAGE_SAMPLED_BIT;
    ici.sharingMode = VK_SHARING_MODE_EXCLUSIVE;
    ici.initialLayout = VK_IMAGE_LAYOUT_UNDEFINED;

    VkImage img = VK_NULL_HANDLE;
    if (vkCreateImage(g_dev, &ici, NULL, &img) != VK_SUCCESS) {
        goVKLog("vk_render_frame_dmabuf: vkCreateImage failed", 2);
        close(f->fd); if (f->release_fn) f->release_fn(f->release_ctx);
        g_render_stage = 1; return 0;
    }

    VkMemoryRequirements mr;
    vkGetImageMemoryRequirements(g_dev, img, &mr);
    VkPhysicalDeviceMemoryProperties mp;
    vkGetPhysicalDeviceMemoryProperties(g_pdev, &mp);
    uint32_t memType = vk_find_mem(&mp, mr.memoryTypeBits, 0);
    if (memType == UINT32_MAX) {
        goVKLog("vk_render_frame_dmabuf: no compatible memory type", 2);
        vkDestroyImage(g_dev, img, NULL);
        close(f->fd); if (f->release_fn) f->release_fn(f->release_ctx);
        g_render_stage = 1; return 0;
    }

    VkImportMemoryFdInfoKHR importInfo = { VK_STRUCTURE_TYPE_IMPORT_MEMORY_FD_INFO_KHR };
    importInfo.handleType = VK_EXTERNAL_MEMORY_HANDLE_TYPE_DMA_BUF_BIT_EXT;
    importInfo.fd = f->fd;
    VkMemoryDedicatedAllocateInfo dedicated = { VK_STRUCTURE_TYPE_MEMORY_DEDICATED_ALLOCATE_INFO, &importInfo };
    dedicated.image = img;
    VkMemoryAllocateInfo mai = { VK_STRUCTURE_TYPE_MEMORY_ALLOCATE_INFO, &dedicated };
    mai.allocationSize = mr.size;
    mai.memoryTypeIndex = memType;

    VkDeviceMemory mem = VK_NULL_HANDLE;
    VkResult mres = vkAllocateMemory(g_dev, &mai, NULL, &mem);
    if (mres != VK_SUCCESS) {
        // Import failed -- per VkImportMemoryFdInfoKHR semantics ownership
        // only transfers on success, so we still own f->fd here.
        snprintf(dbg, sizeof(dbg), "vk_render_frame_dmabuf: vkAllocateMemory (import) failed res=%d", (int)mres);
        goVKLog(dbg, 2);
        vkDestroyImage(g_dev, img, NULL);
        close(f->fd); if (f->release_fn) f->release_fn(f->release_ctx);
        g_render_stage = 1; return 0;
    }
    // Import succeeded: the driver now owns f->fd (spec: "the application
    // must not perform any operations on the file descriptor after a
    // successful import") -- do not close it ourselves from here on.

    if (vkBindImageMemory(g_dev, img, mem, 0) != VK_SUCCESS) {
        goVKLog("vk_render_frame_dmabuf: vkBindImageMemory failed", 2);
        vkFreeMemory(g_dev, mem, NULL);
        vkDestroyImage(g_dev, img, NULL);
        if (f->release_fn) f->release_fn(f->release_ctx);
        g_render_stage = 1; return 0;
    }

    VkSamplerYcbcrConversionInfo convInfo = { VK_STRUCTURE_TYPE_SAMPLER_YCBCR_CONVERSION_INFO };
    convInfo.conversion = g_yconv;
    VkImageViewCreateInfo viewCI = { VK_STRUCTURE_TYPE_IMAGE_VIEW_CREATE_INFO, &convInfo };
    viewCI.image = img;
    viewCI.viewType = VK_IMAGE_VIEW_TYPE_2D;
    viewCI.format = fmt;
    viewCI.subresourceRange.aspectMask = VK_IMAGE_ASPECT_COLOR_BIT;
    viewCI.subresourceRange.levelCount = 1;
    viewCI.subresourceRange.layerCount = 1;
    VkImageView view = VK_NULL_HANDLE;
    if (vkCreateImageView(g_dev, &viewCI, NULL, &view) != VK_SUCCESS) {
        goVKLog("vk_render_frame_dmabuf: vkCreateImageView failed", 2);
        vkFreeMemory(g_dev, mem, NULL);
        vkDestroyImage(g_dev, img, NULL);
        if (f->release_fn) f->release_fn(f->release_ctx);
        g_render_stage = 1; return 0;
    }

    VkDescriptorImageInfo imgInfo = { VK_NULL_HANDLE, view, VK_IMAGE_LAYOUT_SHADER_READ_ONLY_OPTIMAL };
    VkWriteDescriptorSet write = { VK_STRUCTURE_TYPE_WRITE_DESCRIPTOR_SET };
    write.dstSet = g_ydset; write.dstBinding = 0; write.descriptorCount = 1;
    write.descriptorType = VK_DESCRIPTOR_TYPE_COMBINED_IMAGE_SAMPLER;
    write.pImageInfo = &imgInfo;
    vkUpdateDescriptorSets(g_dev, 1, &write, 0, NULL);

    // ---- record + submit + present ----
    vkResetCommandBuffer(g_cmdbuf, 0);
    VkCommandBufferBeginInfo bi = { VK_STRUCTURE_TYPE_COMMAND_BUFFER_BEGIN_INFO };
    bi.flags = VK_COMMAND_BUFFER_USAGE_ONE_TIME_SUBMIT_BIT;
    vkBeginCommandBuffer(g_cmdbuf, &bi);

    // Queue-family-ownership acquire: this image was produced entirely
    // outside Vulkan (VAAPI), so it starts life owned by
    // VK_QUEUE_FAMILY_FOREIGN_EXT and must be formally transferred to our
    // queue family before use -- a barrier-only operation, no semaphore
    // needed (the actual GPU-work sync already happened via vaSyncSurface
    // on the CPU before this fd ever reached us).
    {
        VkImageMemoryBarrier b = { VK_STRUCTURE_TYPE_IMAGE_MEMORY_BARRIER };
        b.oldLayout = VK_IMAGE_LAYOUT_UNDEFINED;
        b.newLayout = VK_IMAGE_LAYOUT_SHADER_READ_ONLY_OPTIMAL;
        b.srcQueueFamilyIndex = VK_QUEUE_FAMILY_FOREIGN_EXT;
        b.dstQueueFamilyIndex = g_qfam;
        b.image = img;
        b.subresourceRange.aspectMask = VK_IMAGE_ASPECT_COLOR_BIT;
        b.subresourceRange.levelCount = 1;
        b.subresourceRange.layerCount = 1;
        b.srcAccessMask = 0;
        b.dstAccessMask = VK_ACCESS_SHADER_READ_BIT;
        vkCmdPipelineBarrier(g_cmdbuf, VK_PIPELINE_STAGE_TOP_OF_PIPE_BIT, VK_PIPELINE_STAGE_FRAGMENT_SHADER_BIT,
                              0, 0, NULL, 0, NULL, 1, &b);
    }

    vk_image_barrier(g_cmdbuf, g_swap_imgs[img_idx],
        VK_IMAGE_LAYOUT_UNDEFINED, VK_IMAGE_LAYOUT_COLOR_ATTACHMENT_OPTIMAL,
        0, VK_ACCESS_COLOR_ATTACHMENT_WRITE_BIT,
        VK_PIPELINE_STAGE_TOP_OF_PIPE_BIT, VK_PIPELINE_STAGE_COLOR_ATTACHMENT_OUTPUT_BIT);

    int sw = (int)g_swap_ext.width, sh = (int)g_swap_ext.height;
    int fw = f->vis_w, fh = f->vis_h;
    float fa = (float)fw / (float)(fh ? fh : 1);
    float wa = (float)sw / (float)(sh ? sh : 1);
    int dx = 0, dy = 0, dw = sw, dh = sh;
    if (fa > wa) { dh = (int)(sw / fa + 0.5f); dy = (sh - dh) / 2; }
    else         { dw = (int)(sh * fa + 0.5f); dx = (sw - dw) / 2; }

    VkRenderingAttachmentInfo colorAtt = { VK_STRUCTURE_TYPE_RENDERING_ATTACHMENT_INFO };
    colorAtt.imageView = g_swap_views[img_idx];
    colorAtt.imageLayout = VK_IMAGE_LAYOUT_COLOR_ATTACHMENT_OPTIMAL;
    colorAtt.loadOp = VK_ATTACHMENT_LOAD_OP_CLEAR;
    colorAtt.storeOp = VK_ATTACHMENT_STORE_OP_STORE;
    colorAtt.clearValue.color = (VkClearColorValue){{0,0,0,1}};

    VkRenderingInfo renderInfo = { VK_STRUCTURE_TYPE_RENDERING_INFO };
    renderInfo.renderArea = (VkRect2D){ {0,0}, g_swap_ext };
    renderInfo.layerCount = 1;
    renderInfo.colorAttachmentCount = 1;
    renderInfo.pColorAttachments = &colorAtt;
    vkCmdBeginRendering(g_cmdbuf, &renderInfo);

    VkViewport vp = { (float)dx, (float)dy, (float)dw, (float)dh, 0.0f, 1.0f };
    VkRect2D scissor = { {dx, dy}, {(uint32_t)dw, (uint32_t)dh} };
    vkCmdSetViewport(g_cmdbuf, 0, 1, &vp);
    vkCmdSetScissor(g_cmdbuf, 0, 1, &scissor);

    vkCmdBindPipeline(g_cmdbuf, VK_PIPELINE_BIND_POINT_GRAPHICS, g_ypipeline);
    vkCmdBindDescriptorSets(g_cmdbuf, VK_PIPELINE_BIND_POINT_GRAPHICS, g_yplayout, 0, 1, &g_ydset, 0, NULL);
    float uvScale[2] = {
        f->surf_w > 0 ? (float)f->vis_w / (float)f->surf_w : 1.0f,
        f->surf_h > 0 ? (float)f->vis_h / (float)f->surf_h : 1.0f,
    };
    vkCmdPushConstants(g_cmdbuf, g_yplayout, VK_SHADER_STAGE_VERTEX_BIT, 0, sizeof(uvScale), uvScale);
    vkCmdDraw(g_cmdbuf, 3, 1, 0, 0);

    vkCmdEndRendering(g_cmdbuf);

    vk_image_barrier(g_cmdbuf, g_swap_imgs[img_idx],
        VK_IMAGE_LAYOUT_COLOR_ATTACHMENT_OPTIMAL, VK_IMAGE_LAYOUT_PRESENT_SRC_KHR,
        VK_ACCESS_COLOR_ATTACHMENT_WRITE_BIT, 0,
        VK_PIPELINE_STAGE_COLOR_ATTACHMENT_OUTPUT_BIT, VK_PIPELINE_STAGE_BOTTOM_OF_PIPE_BIT);

    vkEndCommandBuffer(g_cmdbuf);

    VkPipelineStageFlags wait_stage = VK_PIPELINE_STAGE_COLOR_ATTACHMENT_OUTPUT_BIT;
    VkSubmitInfo si = { VK_STRUCTURE_TYPE_SUBMIT_INFO };
    si.waitSemaphoreCount = 1; si.pWaitSemaphores = &g_img_sem; si.pWaitDstStageMask = &wait_stage;
    si.commandBufferCount = 1; si.pCommandBuffers = &g_cmdbuf;
    si.signalSemaphoreCount = 1; si.pSignalSemaphores = &g_rnd_sem;
    g_render_stage = 5;
    vkQueueSubmit(g_queue, 1, &si, g_fence);

    // This frame's resources become "prev" -- released on the NEXT call's
    // fence wait, once we know this submission has retired.
    g_dmabuf_prev_img  = img;
    g_dmabuf_prev_mem  = mem;
    g_dmabuf_prev_view = view;
    g_dmabuf_prev_release_ctx = f->release_ctx;
    g_dmabuf_prev_release_fn  = f->release_fn;

    VkPresentInfoKHR pi = { VK_STRUCTURE_TYPE_PRESENT_INFO_KHR };
    pi.waitSemaphoreCount = 1; pi.pWaitSemaphores = &g_rnd_sem;
    pi.swapchainCount = 1; pi.pSwapchains = &g_swap; pi.pImageIndices = &img_idx;
    g_render_stage = 6;
    res = vkQueuePresentKHR(g_queue, &pi);
    g_render_stage = 1;
    if (res == VK_ERROR_OUT_OF_DATE_KHR || res == VK_SUBOPTIMAL_KHR) {
        g_render_stage = 7; vk_recreate_swapchain(); g_render_stage = 1;
        return 1;
    }
    if (res != VK_SUCCESS) {
        snprintf(dbg, sizeof(dbg), "QueuePresent (dmabuf) failed res=%d", (int)res);
        goVKLog(dbg, 2);
    }
    return (res == VK_SUCCESS) ? 1 : 0;
}

// vk_video_try_submit_dmabuf — zero-copy counterpart to vk_video_try_submit.
// On success (return 1), ownership of `fd` and of (release_ctx, release_fn)
// transfers to this module: it will eventually either import `fd` into a
// VkImage (closing it per vkAllocateMemory's dma-buf-import contract) or
// close it directly on a fallback/drop path, and will call
// release_fn(release_ctx) exactly once, once the corresponding GPU read (if
// any) has retired. On failure (return 0, e.g. renderer not active), the
// caller keeps ownership of both and must release them itself.
int vk_video_try_submit_dmabuf(int fd, uint64_t modifier, int surf_w, int surf_h,
                                int vis_w, int vis_h, uint32_t plane_count,
                                uint32_t offset0, uint32_t pitch0,
                                uint32_t offset1, uint32_t pitch1,
                                void *release_ctx, void (*release_fn)(void*)) {
    if (!atomic_load(&g_active)) return 0;
    pthread_mutex_lock(&g_mu);
    if (!atomic_load(&g_active)) { pthread_mutex_unlock(&g_mu); return 0; }

    // Drop-on-full: a previous zero-copy frame the render thread hasn't
    // picked up yet gets released right here instead of leaking its fd/ref.
    if (g_dmabuf_ready) {
        close(g_dmabuf_pending.fd);
        if (g_dmabuf_pending.release_fn) g_dmabuf_pending.release_fn(g_dmabuf_pending.release_ctx);
    }
    g_ready = 0; // only one of {RGBA, dma-buf} frame modes is live at a time

    g_dmabuf_pending.fd          = fd;
    g_dmabuf_pending.modifier    = modifier;
    g_dmabuf_pending.surf_w      = surf_w;
    g_dmabuf_pending.surf_h      = surf_h;
    g_dmabuf_pending.vis_w       = vis_w;
    g_dmabuf_pending.vis_h       = vis_h;
    g_dmabuf_pending.plane_count = plane_count;
    g_dmabuf_pending.offset0     = offset0;
    g_dmabuf_pending.pitch0      = pitch0;
    g_dmabuf_pending.offset1     = offset1;
    g_dmabuf_pending.pitch1      = pitch1;
    g_dmabuf_pending.release_ctx = release_ctx;
    g_dmabuf_pending.release_fn  = release_fn;
    g_dmabuf_ready = 1;
    g_submitted++;
    pthread_mutex_unlock(&g_mu);
    if (g_pipe_w >= 0) pipe_wake(g_pipe_w, 1);
    return 1;
}

// vk_video_zerocopy_supported lets moonlight_cgo_linux.go check, before
// doing any VAAPI mapping/export work, whether this renderer's device/
// instance actually support the zero-copy path (see vk_create_device) --
// avoids wasted vaExportSurfaceHandle calls when it doesn't.
int vk_video_zerocopy_supported(void) { return g_zerocopy_supported; }

// ─── render one frame ─────────────────────────────────────────────────────────

static int vk_render_frame(uint8_t *pixels, int fw, int fh, int fs) {
    if (!g_dev || !g_swap) return 0;
    char dbg[96];

    size_t frame_sz = (size_t)fh * (size_t)fs;
    g_render_stage = 2;
    if (!vk_ensure_staging(frame_sz)) { g_render_stage = 1; return 0; }
    if (!vk_ensure_tex(fw, fh))       { g_render_stage = 1; return 0; }

    size_t row = (size_t)fw * 4;
    if (!g_tex_is_bgra) {
        // RGBA swapchain: simple copy, no channel reordering needed.
        if ((size_t)fs == row) {
            memcpy(g_stage_ptr, pixels, frame_sz);
        } else {
            uint8_t *dst = (uint8_t *)g_stage_ptr;
            for (int y = 0; y < fh; y++)
                memcpy(dst + (size_t)y * row, pixels + (size_t)y * (size_t)fs, row);
        }
    } else {
        // BGRA swapchain: swap R and B bytes so the blit produces correct colors.
        uint8_t *dst = (uint8_t *)g_stage_ptr;
        for (int y = 0; y < fh; y++) {
            const uint8_t *src = pixels + (size_t)y * (size_t)fs;
            uint8_t *d = dst + (size_t)y * row;
            for (int x = 0; x < fw; x++, src += 4, d += 4) {
                d[0] = src[2]; // B ← R
                d[1] = src[1]; // G ← G
                d[2] = src[0]; // R ← B
                d[3] = src[3]; // A ← A
            }
        }
    }

    uint32_t img_idx = 0;
    g_render_stage = 3;
    double t0 = mono_sec();
    VkResult res = vkAcquireNextImageKHR(g_dev, g_swap, 3000000000ULL,
                                          g_img_sem, VK_NULL_HANDLE, &img_idx);
    double dt = mono_sec() - t0;
    if (dt > 0.1) {
        snprintf(dbg, sizeof(dbg), "SLOW AcquireNextImage %.0f ms res=%d", dt * 1000.0, (int)res);
        goVKLog(dbg, 1);
    }
    if (res == VK_TIMEOUT) {
        goVKLog("AcquireNextImage TIMEOUT 3s", 2);
        g_render_stage = 1; return 0;
    }
    if (res == VK_ERROR_OUT_OF_DATE_KHR) {
        g_render_stage = 7;
        vk_recreate_swapchain();
        g_render_stage = 1; return 0;
    }
    if (res != VK_SUCCESS && res != VK_SUBOPTIMAL_KHR) {
        snprintf(dbg, sizeof(dbg), "AcquireNextImage failed res=%d", (int)res);
        goVKLog(dbg, 2);
        g_render_stage = 1; return 0;
    }

    g_render_stage = 4;
    t0 = mono_sec();
    VkResult fence_res = vkWaitForFences(g_dev, 1, &g_fence, VK_TRUE, 2000000000ULL);
    dt = mono_sec() - t0;
    if (fence_res == VK_TIMEOUT) {
        goVKLog("WaitForFences TIMEOUT 2s — GPU hang?", 2);
        vkResetFences(g_dev, 1, &g_fence);
        g_render_stage = 1; return 0;
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

    int sw = (int)g_swap_ext.width, sh = (int)g_swap_ext.height;
    float fa = (float)fw / (float)(fh ? fh : 1);
    float wa = (float)sw / (float)(sh ? sh : 1);
    int dx = 0, dy = 0, dw = sw, dh = sh;
    if (fa > wa) { dh = (int)(sw / fa + 0.5f); dy = (sh - dh) / 2; }
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
    blt.srcOffsets[1] = (VkOffset3D){fw, fh, 1};
    blt.dstSubresource.aspectMask = VK_IMAGE_ASPECT_COLOR_BIT;
    blt.dstSubresource.layerCount = 1;
    blt.dstOffsets[0] = (VkOffset3D){dx,      dy,      0};
    blt.dstOffsets[1] = (VkOffset3D){dx + dw, dy + dh, 1};
    vkCmdBlitImage(g_cmdbuf,
        g_tex,                VK_IMAGE_LAYOUT_TRANSFER_SRC_OPTIMAL,
        g_swap_imgs[img_idx], VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL,
        1, &blt, VK_FILTER_LINEAR);

    vk_image_barrier(g_cmdbuf, g_swap_imgs[img_idx],
        VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL, VK_IMAGE_LAYOUT_PRESENT_SRC_KHR,
        VK_ACCESS_TRANSFER_WRITE_BIT, 0,
        VK_PIPELINE_STAGE_TRANSFER_BIT, VK_PIPELINE_STAGE_BOTTOM_OF_PIPE_BIT);

    vkEndCommandBuffer(g_cmdbuf);

    VkPipelineStageFlags wait_stage = VK_PIPELINE_STAGE_TRANSFER_BIT;
    VkSubmitInfo si = { VK_STRUCTURE_TYPE_SUBMIT_INFO };
    si.waitSemaphoreCount   = 1;
    si.pWaitSemaphores      = &g_img_sem;
    si.pWaitDstStageMask    = &wait_stage;
    si.commandBufferCount   = 1;
    si.pCommandBuffers      = &g_cmdbuf;
    si.signalSemaphoreCount = 1;
    si.pSignalSemaphores    = &g_rnd_sem;
    g_render_stage = 5;
    vkQueueSubmit(g_queue, 1, &si, g_fence);

    VkPresentInfoKHR pi = { VK_STRUCTURE_TYPE_PRESENT_INFO_KHR };
    pi.waitSemaphoreCount = 1;
    pi.pWaitSemaphores    = &g_rnd_sem;
    pi.swapchainCount     = 1;
    pi.pSwapchains        = &g_swap;
    pi.pImageIndices      = &img_idx;
    g_render_stage = 6;
    t0 = mono_sec();
    res = vkQueuePresentKHR(g_queue, &pi);
    dt = mono_sec() - t0;
    if (dt > 0.1) {
        snprintf(dbg, sizeof(dbg), "SLOW QueuePresent %.0f ms res=%d", dt * 1000.0, (int)res);
        goVKLog(dbg, 1);
    }
    g_render_stage = 1;
    if (res == VK_ERROR_OUT_OF_DATE_KHR || res == VK_SUBOPTIMAL_KHR) {
        g_render_stage = 7;
        vk_recreate_swapchain();
        g_render_stage = 1;
        return 1;
    }
    if (res != VK_SUCCESS) {
        snprintf(dbg, sizeof(dbg), "QueuePresent failed res=%d", (int)res);
        goVKLog(dbg, 2);
    }
    return (res == VK_SUCCESS) ? 1 : 0;
}

// ─── render thread ────────────────────────────────────────────────────────────

static void *vk_render_thread(void *unused) {
    (void)unused;
    double hb_log_t = mono_sec();
    long long consec_fail = 0;

    while (atomic_load(&g_active)) {
        g_render_stage = 0;
        struct timeval tv = {0, 8000}; // 8 ms
        fd_set fds; FD_ZERO(&fds); FD_SET(g_pipe_r, &fds);
        select(g_pipe_r + 1, &fds, NULL, NULL, &tv);
        if (FD_ISSET(g_pipe_r, &fds)) {
            pipe_drain(g_pipe_r);
        }
        g_render_hb++;
        if (!atomic_load(&g_active)) break;

        // Periodic heartbeat log.
        double hb_now = mono_sec();
        if (hb_now - hb_log_t >= 10.0) {
            char hbm[96];
            snprintf(hbm, sizeof(hbm), "render thread alive hb=%lld rendered=%lld stage=%d",
                     (long long)g_render_hb, (long long)g_rendered, g_render_stage);
            goVKLog(hbm, 0);
            hb_log_t = hb_now;
        }

        // When hidden, skip rendering.
        if (atomic_load(&g_hidden)) continue;

        uint8_t *tmp = NULL;
        int fw = 0, fh = 0, fs = 0;
        int have_dmabuf = 0;
        DmabufFrame dmaf;
        pthread_mutex_lock(&g_mu);
        if (g_dmabuf_ready) {
            dmaf = g_dmabuf_pending;
            g_dmabuf_ready = 0;
            have_dmabuf = 1;
        } else if (g_ready && g_buf) {
            fw = g_fw; fh = g_fh; fs = g_fs;
            size_t sz = (size_t)fh * (size_t)fs;
            tmp = malloc(sz);
            if (tmp) memcpy(tmp, g_buf, sz);
            g_ready = 0;
        }
        pthread_mutex_unlock(&g_mu);
        if (!tmp && !have_dmabuf) continue;

        g_render_stage = 1;
        int rf = have_dmabuf ? vk_render_frame_dmabuf(&dmaf) : vk_render_frame(tmp, fw, fh, fs);
        if (have_dmabuf) { fw = dmaf.vis_w; fh = dmaf.vis_h; }
        free(tmp);
        if (!rf) {
            consec_fail++;
            if (consec_fail == 10 || consec_fail == 100 || (consec_fail % 300 == 0)) {
                char fm[80];
                snprintf(fm, sizeof(fm), "vk_render_frame failing consec=%lld rendered=%lld",
                         (long long)consec_fail, (long long)g_rendered);
                goVKLog(fm, 1);
            }
        } else {
            consec_fail = 0;
        }

        g_rendered++;
        g_fps_n++;
        g_stat_rendered  = g_rendered;
        g_stat_submitted = g_submitted;
        double now = mono_sec();
        if (g_rendered == 1) {
            g_stat_first = 1; g_stat_fw = fw; g_stat_fh = fh;
            g_fps_t0 = now; g_fps_n = 0;
        }
        if (now - g_fps_t0 >= 5.0 && g_fps_n > 0) {
            g_stat_fps       = (float)((double)g_fps_n / (now - g_fps_t0));
            g_stat_fps_ready = 1;
            g_fps_t0 = now; g_fps_n = 0;
        }
        if (g_last_blit_ts > 0.0) {
            float gap = (float)((now - g_last_blit_ts) * 1000.0);
            if (gap > g_stat_max_gap_ms) g_stat_max_gap_ms = gap;
        }
        g_last_blit_ts = now;
    }
    return NULL;
}

// ─── Public C API ─────────────────────────────────────────────────────────────

int vk_video_is_active(void) { return atomic_load(&g_active); }

int vk_video_try_submit(uint8_t *rgba, int width, int height, int stride) {
    if (!atomic_load(&g_active)) return 0;
    size_t sz = (size_t)height * (size_t)stride;
    pthread_mutex_lock(&g_mu);
    if (!atomic_load(&g_active)) {
        pthread_mutex_unlock(&g_mu);
        return 0;
    }
    // A session runs either the RGBA path or the dma-buf zero-copy path,
    // never both -- drop any stale pending zero-copy frame the render
    // thread hasn't picked up yet (e.g. right after a codec switch moved
    // this session from hw-dmabuf back to software/RGBA) instead of
    // leaking its fd/AVFrame ref.
    if (g_dmabuf_ready) {
        close(g_dmabuf_pending.fd);
        if (g_dmabuf_pending.release_fn) g_dmabuf_pending.release_fn(g_dmabuf_pending.release_ctx);
        g_dmabuf_ready = 0;
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
    if (g_pipe_w >= 0) { pipe_wake(g_pipe_w, 1); }
    return 1;
}

// vk_video_update_frame — reposition the X11 child window and apply hide/show.
// Called from CGO thread only; safe to call X11 here.
void vk_video_update_frame(int x, int y, int w, int h) {
    if (!atomic_load(&g_active)) return;
    atomic_store(&g_dst_x, x);
    atomic_store(&g_dst_y, y);
    atomic_store(&g_dst_w, w);
    atomic_store(&g_dst_h, h);

    if (g_dpy && g_win && w > 0 && h > 0)
        XMoveResizeWindow(g_dpy, g_win, x, y, (unsigned)w, (unsigned)h);

    // Apply visibility (g_hidden may have been updated by vk_video_set_hidden).
    int want_hidden = atomic_load(&g_hidden);
    if (want_hidden && g_win_visible) {
        if (g_dpy && g_win) XUnmapWindow(g_dpy, g_win);
        g_win_visible = 0;
    } else if (!want_hidden && !g_win_visible) {
        if (g_dpy && g_win) XMapWindow(g_dpy, g_win);
        g_win_visible = 1;
    }
    if (g_dpy) XFlush(g_dpy);
}

// Called from CGO thread — safe to call X11 immediately.
void vk_video_set_hidden(int hidden) {
    atomic_store(&g_hidden, hidden ? 1 : 0);
    if (!g_dpy || !g_win) return;
    if (hidden && g_win_visible) {
        XUnmapWindow(g_dpy, g_win);
        XFlush(g_dpy);
        g_win_visible = 0;
    } else if (!hidden && !g_win_visible) {
        XMapWindow(g_dpy, g_win);
        XFlush(g_dpy);
        g_win_visible = 1;
    }
}

static void vk_full_cleanup(void) {
    atomic_store(&g_active, 0);
    if (g_pipe_w >= 0) { pipe_wake(g_pipe_w, 0); }
    if (g_thread) { pthread_join(g_thread, NULL); g_thread = 0; }
    if (g_pipe_r >= 0) { close(g_pipe_r); g_pipe_r = -1; }
    if (g_pipe_w >= 0) { close(g_pipe_w); g_pipe_w = -1; }

    if (g_dev) {
        vkDeviceWaitIdle(g_dev);
        if (g_stage_ptr && g_stage_mem) { vkUnmapMemory(g_dev, g_stage_mem); g_stage_ptr = NULL; }
        if (g_stage_buf) { vkDestroyBuffer(g_dev, g_stage_buf, NULL); g_stage_buf = VK_NULL_HANDLE; }
        if (g_stage_mem) { vkFreeMemory(g_dev, g_stage_mem, NULL);   g_stage_mem = VK_NULL_HANDLE; }
        g_stage_sz = 0;
        if (g_tex)     { vkDestroyImage(g_dev, g_tex, NULL);   g_tex = VK_NULL_HANDLE; }
        if (g_tex_mem) { vkFreeMemory(g_dev, g_tex_mem, NULL); g_tex_mem = VK_NULL_HANDLE; }
        g_tex_w = 0; g_tex_h = 0;

        // Zero-copy dma-buf path: the render thread is already joined (see
        // above), so nothing else can be touching these -- release whatever
        // frame was in flight (prev, mid-render-defer) or still queued
        // (pending, never picked up) and tear down the fixed NV12 pipeline.
        vk_dmabuf_release_prev();
        if (g_dmabuf_ready) {
            close(g_dmabuf_pending.fd);
            if (g_dmabuf_pending.release_fn) g_dmabuf_pending.release_fn(g_dmabuf_pending.release_ctx);
            g_dmabuf_ready = 0;
        }
        if (g_ydset)     { /* freed with pool below */ g_ydset = VK_NULL_HANDLE; }
        if (g_ydpool)    { vkDestroyDescriptorPool(g_dev, g_ydpool, NULL); g_ydpool = VK_NULL_HANDLE; }
        if (g_ypipeline) { vkDestroyPipeline(g_dev, g_ypipeline, NULL); g_ypipeline = VK_NULL_HANDLE; }
        if (g_yplayout)  { vkDestroyPipelineLayout(g_dev, g_yplayout, NULL); g_yplayout = VK_NULL_HANDLE; }
        if (g_ydsl)      { vkDestroyDescriptorSetLayout(g_dev, g_ydsl, NULL); g_ydsl = VK_NULL_HANDLE; }
        if (g_ysampler)  { vkDestroySampler(g_dev, g_ysampler, NULL); g_ysampler = VK_NULL_HANDLE; }
        if (g_yconv)     { vkDestroySamplerYcbcrConversion(g_dev, g_yconv, NULL); g_yconv = VK_NULL_HANDLE; }
        g_ypipeline_ok = 0;
        g_zerocopy_supported = 0;
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

    if (g_win && g_dpy)  { XDestroyWindow(g_dpy, g_win); g_win = 0; }
    if (g_dpy)           { XCloseDisplay(g_dpy);          g_dpy = NULL; }
    g_parent_win = 0;

    pthread_mutex_lock(&g_mu);
    if (g_buf) { free(g_buf); g_buf = NULL; g_buf_sz = 0; }
    g_ready = 0;
    pthread_mutex_unlock(&g_mu);
    g_ready = 0;
    g_rendered = 0; g_submitted = 0;
    g_win_visible = 1;
}

// vk_video_next_event — drain one pending pointer event from g_dpy.
// Returns 1 if an event was consumed; type values:
//   1 = MotionNotify, 2 = ButtonPress, 3 = ButtonRelease
// Scroll wheel: button 4 = wheel-up, 5 = wheel-down.
// Thread-safe because XInitThreads() is called by GLFW before any Xlib use.
int vk_video_next_event(int *type_out, int *x_out, int *y_out, int *btn_out) {
    *type_out = 0;
    if (!g_dpy || !atomic_load(&g_active)) return 0;
    while (XPending(g_dpy)) {
        XEvent ev;
        XNextEvent(g_dpy, &ev);
        switch (ev.type) {
        case MotionNotify:
            *type_out = 1;
            *x_out    = ev.xmotion.x;
            *y_out    = ev.xmotion.y;
            *btn_out  = 0;
            return 1;
        case ButtonPress:
            *type_out = 2;
            *x_out    = ev.xbutton.x;
            *y_out    = ev.xbutton.y;
            *btn_out  = (int)ev.xbutton.button;
            return 1;
        case ButtonRelease:
            *type_out = 3;
            *x_out    = ev.xbutton.x;
            *y_out    = ev.xbutton.y;
            *btn_out  = (int)ev.xbutton.button;
            return 1;
        default:
            continue;
        }
    }
    return 0;
}

int vk_video_create(uintptr_t parent_xwin, int x, int y, int w, int h) {
    if (atomic_load(&g_active)) vk_full_cleanup();

    if (!parent_xwin) { goVKLog("vk_video_create: parent XID is 0", 2); return 0; }
    g_parent_win = (Window)parent_xwin;

    // Open a dedicated Display for this overlay (same pattern as GL impl).
    Display *dpy = XOpenDisplay(NULL);
    if (!dpy) { goVKLog("vk_video_create: XOpenDisplay failed", 2); return 0; }
    g_dpy = dpy;

    int screen = DefaultScreen(dpy);
    int cw = w > 0 ? w : 1, ch = h > 0 ? h : 1;

    XSetWindowAttributes wa = {0};
    wa.background_pixel = BlackPixel(dpy, screen);
    wa.border_pixel     = 0;
    wa.override_redirect = False;
    Window child = XCreateWindow(dpy, (Window)parent_xwin,
                                  x, y, (unsigned)cw, (unsigned)ch,
                                  0, CopyFromParent, InputOutput, CopyFromParent,
                                  CWBackPixel | CWBorderPixel, &wa);
    XMapWindow(dpy, child);
    // Subscribe to pointer events on the overlay window so we can forward them
    // to Go. GLFW receives LeaveNotify when cursor enters this child window and
    // stops delivering mouse events to Fyne — we compensate by reading events
    // here and forwarding them via vk_video_next_event (see video_widget_gl_linux.go).
    // XInitThreads() was already called by GLFW, so concurrent Xlib access is safe.
    XSelectInput(dpy, child,
        PointerMotionMask | ButtonPressMask | ButtonReleaseMask | ButtonMotionMask);
    XFlush(dpy);
    g_win = child;
    g_win_visible = 1;

    // Vulkan init.
    if (!vk_create_instance())  { goVKLog("vk_video_create: vkCreateInstance failed", 2); goto fail; }
    if (!vk_select_device())    { goVKLog("vk_video_create: no suitable GPU", 2);          goto fail; }
    if (!vk_create_device())    { goVKLog("vk_video_create: vkCreateDevice failed", 2);    goto fail; }

    {
        VkXlibSurfaceCreateInfoKHR sci = { VK_STRUCTURE_TYPE_XLIB_SURFACE_CREATE_INFO_KHR };
        sci.dpy    = dpy;
        sci.window = child;
        if (vkCreateXlibSurfaceKHR(g_inst, &sci, NULL, &g_surf) != VK_SUCCESS) {
            goVKLog("vk_video_create: vkCreateXlibSurfaceKHR failed", 2); goto fail;
        }
    }

    if (!vk_create_swapchain(cw, ch)) {
        goVKLog("vk_video_create: swapchain creation failed", 2); goto fail;
    }

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
        if (vkCreateFence(g_dev, &fci,  NULL, &g_fence)       != VK_SUCCESS) goto fail;
    }

    atomic_store(&g_dst_x, x);
    atomic_store(&g_dst_y, y);
    atomic_store(&g_dst_w, w);
    atomic_store(&g_dst_h, h);
    atomic_store(&g_hidden, 0);

    {
        int fds[2];
        if (pipe(fds) != 0) { goVKLog("vk_video_create: pipe failed", 2); goto fail; }
        g_pipe_r = fds[0]; g_pipe_w = fds[1];
    }

    g_submitted = 0; g_rendered = 0; g_fps_n = 0; g_fps_t0 = 0;
    g_ready = 0; g_stat_first = 0;
    g_stat_max_gap_ms = 0; g_last_blit_ts = 0;
    atomic_store(&g_active, 1);

    if (pthread_create(&g_thread, NULL, vk_render_thread, NULL) != 0) {
        atomic_store(&g_active, 0);
        goVKLog("vk_video_create: pthread_create failed", 2);
        goto fail;
    }

    {
        char msg[512];
        VkPhysicalDeviceProperties pr;
        vkGetPhysicalDeviceProperties(g_pdev, &pr);
        snprintf(msg, sizeof(msg),
                 "Vulkan/Linux renderer ready — GPU=%s rect=(%d,%d,%dx%d)",
                 pr.deviceName, x, y, w, h);
        goVKLog(msg, 0);
    }
    return 1;

fail:
    vk_full_cleanup();
    return 0;
}

void vk_video_destroy(void) {
    if (!atomic_load(&g_active)) return;
    char msg[192];
    snprintf(msg, sizeof(msg),
             "Vulkan/Linux renderer destroyed — rendered=%lld submitted=%lld",
             g_rendered, g_submitted);
    vk_full_cleanup();
    goVKLog(msg, 0);
}

void vk_video_get_stats(long long *rendered, long long *submitted,
                        float *fps, int *fps_ready,
                        int *first_frame, int *fw, int *fh,
                        float *max_gap_ms) {
    *rendered    = g_stat_rendered;
    *submitted   = g_stat_submitted;
    *fps         = g_stat_fps;
    *fps_ready   = g_stat_fps_ready;
    *first_frame = g_stat_first;
    *fw          = g_stat_fw;
    *fh          = g_stat_fh;
    *max_gap_ms  = g_stat_max_gap_ms;
}

void vk_video_clear_pending_stats(void) {
    g_stat_fps_ready  = 0;
    g_stat_first      = 0;
    g_stat_max_gap_ms = 0.0f;
}

void vk_video_get_diag(long long *hb, int *stage) {
    *hb    = g_render_hb;
    *stage = g_render_stage;
}

#endif // defined(__linux__) && !defined(__ANDROID__)
