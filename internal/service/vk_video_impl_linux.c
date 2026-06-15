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

extern void goVKLog(char *msg, int level);

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
    VkInstanceCreateInfo ci = { VK_STRUCTURE_TYPE_INSTANCE_CREATE_INFO };
    ci.enabledExtensionCount   = 2;
    ci.ppEnabledExtensionNames = exts;
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
            char tmp[64]; read(g_pipe_r, tmp, sizeof(tmp));
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
        pthread_mutex_lock(&g_mu);
        if (g_ready && g_buf) {
            fw = g_fw; fh = g_fh; fs = g_fs;
            size_t sz = (size_t)fh * (size_t)fs;
            tmp = malloc(sz);
            if (tmp) memcpy(tmp, g_buf, sz);
            g_ready = 0;
        }
        pthread_mutex_unlock(&g_mu);
        if (!tmp) continue;

        g_render_stage = 1;
        int rf = vk_render_frame(tmp, fw, fh, fs);
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
    if (g_pipe_w >= 0) { char c = 0; write(g_pipe_w, &c, 1); }
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

    if (g_buf) { free(g_buf); g_buf = NULL; g_buf_sz = 0; }
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
    int pending = XPending(g_dpy);
    if (pending > 0) {
        char dbg[128];
        snprintf(dbg, sizeof(dbg), "vk_video_next_event: XPending=%d", pending);
        goVKLog(dbg, 0);
    }
    while (XPending(g_dpy)) {
        XEvent ev;
        XNextEvent(g_dpy, &ev);
        char dbg[128];
        snprintf(dbg, sizeof(dbg), "vk_video_next_event: event type=%d", ev.type);
        goVKLog(dbg, 0);
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
            {
                char msg[128];
                snprintf(msg, sizeof(msg), "vk_video_next_event: ButtonPress btn=%d x=%d y=%d", (int)ev.xbutton.button, (int)ev.xbutton.x, (int)ev.xbutton.y);
                goVKLog(msg, 0);
            }
            return 1;
        case ButtonRelease:
            *type_out = 3;
            *x_out    = ev.xbutton.x;
            *y_out    = ev.xbutton.y;
            *btn_out  = (int)ev.xbutton.button;
            return 1;
        default:
            snprintf(dbg, sizeof(dbg), "vk_video_next_event: discarding event type=%d", ev.type);
            goVKLog(dbg, 0);
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
