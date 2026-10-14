// pyrowave_decode_windows.c -- PyroWave decode for the Windows client, fully
// on the GPU: the intra-only wavelet codec a USBridge host (rust-shine) sends
// when the session negotiated VIDEO_FORMAT_PYROWAVE.
//
// Unlike the Linux/macOS paths (CPU readback + upload), nothing leaves the
// GPU here. PyroWave runs on the renderer's own VkDevice (the ffmpeg-created
// device from vk_hwdev_bridge_windows.c, which already enables everything
// PyroWave's decoder needs: Vulkan 1.3 subgroup size control, 8/16-bit
// storage...) on a compute queue no one else submits to, and decodes straight
// into a ring of G8_B8_R8_3PLANE_420 images the renderer samples through its
// BT.709 YCbCr sampler. A timeline semaphore orders decode before sampling.
//
// Why the same device rather than PyroWave's own device + exported memory:
// sharing an image between two VkDevices through OPAQUE_WIN32 memory came out
// with one plane intermittently empty on an RTX 3090 (4 of 5 runs), while the
// same-device path verified byte-exact against PyroWave's CPU decode on both
// an RTX 3090 and a Radeon 780M, every run (tools/decode_bench/pyrowave_bench.c,
// BENCH_SHARED=1 BENCH_VERIFY=N). 4K@120 at 400 Mb/s decodes in ~2-3 ms.
//
// Decode is called from the pull decode thread (moonlight_cgo_windows.go): a
// decode only records and submits GPU work, it never waits for it, so no
// separate thread is needed. Every PyroWave frame is a keyframe; a frame with
// a lost shard never reaches here (moonlight-common-c drops it).
//
// Color upgrades (VIDEO_FORMAT_PYROWAVE_444 / _HDR): the sequence header says
// what a frame is -- chroma resolution (4:4:4 -> *_3PLANE_444 ring images) and
// the transfer function (PQ -> 16-bit G16_B16_R16 planes, BT.2020; the wavelet
// itself has no bit depth, the host fed it 10-bit samples). The renderer keys
// its YCbCr conversion and the HDR10 swapchain off those formats.
//
// Own translation unit for the same cgo "multiple definition" reason as
// vk_hwdev_bridge_windows.c.

#ifdef _WIN32

#define VK_USE_PLATFORM_WIN32_KHR
#include <windows.h>
#include <vulkan/vulkan.h>
#include <pyrowave.h>
#include <libavutil/hwcontext.h>
#include <libavutil/hwcontext_vulkan.h>
#include <stdio.h>
#include <string.h>
#include "pyrowave_framing.h"

extern void goVTLog(char *msg);
extern AVBufferRef *win_vk_hwdev_ctx_ref(void);
extern void vk_video_queue_lock(void);
extern void vk_video_queue_unlock(void);
extern void vk_video_forget_image(void *vk_image);

#define PW_RING 4

static CRITICAL_SECTION g_pw_cs; // decode-thread state vs. teardown
static INIT_ONCE g_pw_once = INIT_ONCE_STATIC_INIT;
static BOOL CALLBACK pw_init_once(PINIT_ONCE o, PVOID p, PVOID *c) { (void)o; (void)p; (void)c; InitializeCriticalSection(&g_pw_cs); return TRUE; }

// Device (process lifetime, like the Vulkan device it borrows).
static int g_pw_dev_state = 0; // 0 untried, 1 ok, -1 unusable
static pyrowave_device g_pw_dev = NULL;
static VkDevice g_pw_vkdev = VK_NULL_HANDLE;
static VkPhysicalDevice g_pw_phys = VK_NULL_HANDLE;
static uint32_t g_pw_gfx_qf = 0, g_pw_comp_qf = 0;
static VkSemaphore g_pw_sem = VK_NULL_HANDLE;
static uint64_t g_pw_val = 0;

// Decoder + ring (per stream size).
typedef struct {
    VkImage img;
    VkDeviceMemory mem;
    volatile LONG busy; // with the renderer
} PwSlot;
static pyrowave_decoder g_pw_dec = NULL;
static PwSlot g_pw_ring[PW_RING];
static pyrowave_gpu_buffers g_pw_bufs[PW_RING];
static int g_pw_w = 0, g_pw_h = 0, g_pw_next = 0;
static int g_pw_444 = 0, g_pw_hdr = 0;
static VkFormat g_pw_fmt = VK_FORMAT_UNDEFINED;

// The ring image format for a stream: 8-bit SDR or 16-bit (PQ) planes, 4:2:0 or 4:4:4.
static VkFormat pw_ring_format(int c444, int hdr) {
    if (hdr) return c444 ? VK_FORMAT_G16_B16_R16_3PLANE_444_UNORM : VK_FORMAT_G16_B16_R16_3PLANE_420_UNORM;
    return c444 ? VK_FORMAT_G8_B8_R8_3PLANE_444_UNORM : VK_FORMAT_G8_B8_R8_3PLANE_420_UNORM;
}
static volatile LONG64 g_pw_frames = 0;
static uint64_t g_pw_failures = 0;

static void pw_log(const char *m) { goVTLog((char *)m); }
static void pw_fail(const char *why, int v) {
    g_pw_failures++;
    if (g_pw_failures > 5 && g_pw_failures % 300 != 0) return;
    char msg[160];
    snprintf(msg, sizeof(msg), "pyrowave: frame not decoded (%s, %d); %d so far", why, v, (int)g_pw_failures);
    pw_log(msg);
}

// PyroWave locks "all queues it can touch" around its own submissions; the
// renderer takes the same lock around its submits and vkDeviceWaitIdle.
static void pw_qlock(void *u) { (void)u; vk_video_queue_lock(); }
static void pw_qunlock(void *u) { (void)u; vk_video_queue_unlock(); }

static int pw_ensure_device(void) {
    if (g_pw_dev_state) return g_pw_dev_state > 0;
    g_pw_dev_state = -1;
    AVBufferRef *ref = win_vk_hwdev_ctx_ref();
    if (!ref) { pw_log("pyrowave: no shared Vulkan device -- cannot decode"); return 0; }
    AVVulkanDeviceContext *vk = (AVVulkanDeviceContext *)((AVHWDeviceContext *)ref->data)->hwctx;
    av_buffer_unref(&ref); // the device lives for the process (vk_hwdev_bridge_windows.c)
    g_pw_vkdev = vk->act_dev;
    g_pw_phys = vk->phys_dev;

    // PyroWave reads the create infos to learn which features/extensions are
    // on: rebuild them from what ffmpeg enabled. Static, must outlive the device.
    static VkDeviceQueueCreateInfo qci[64];
    static float prio[64];
    static VkDeviceCreateInfo dci;
    static VkInstanceCreateInfo ici;
    static VkApplicationInfo app;
    static pyrowave_device_create_queue_info pq;
    for (int i = 0; i < 64; i++) prio[i] = 1.0f;
    int have_gfx = 0, have_comp = 0;
    for (int i = 0; i < vk->nb_qf && i < 64; i++) {
        qci[i] = (VkDeviceQueueCreateInfo){ VK_STRUCTURE_TYPE_DEVICE_QUEUE_CREATE_INFO };
        qci[i].queueFamilyIndex = vk->qf[i].idx;
        qci[i].queueCount = vk->qf[i].num;
        qci[i].pQueuePriorities = prio;
        if ((vk->qf[i].flags & VK_QUEUE_GRAPHICS_BIT) && !have_gfx) { g_pw_gfx_qf = vk->qf[i].idx; have_gfx = 1; }
        if ((vk->qf[i].flags & VK_QUEUE_COMPUTE_BIT) && !(vk->qf[i].flags & VK_QUEUE_GRAPHICS_BIT) && !have_comp) {
            g_pw_comp_qf = vk->qf[i].idx; have_comp = 1;
        }
    }
    if (!have_gfx) { pw_log("pyrowave: shared device has no graphics queue"); return 0; }
    if (!have_comp) g_pw_comp_qf = g_pw_gfx_qf; // no async compute: share the graphics family (locked)
    dci = (VkDeviceCreateInfo){ VK_STRUCTURE_TYPE_DEVICE_CREATE_INFO };
    dci.pNext = &vk->device_features;
    dci.queueCreateInfoCount = (uint32_t)vk->nb_qf;
    dci.pQueueCreateInfos = qci;
    dci.enabledExtensionCount = (uint32_t)vk->nb_enabled_dev_extensions;
    dci.ppEnabledExtensionNames = vk->enabled_dev_extensions;
    app = (VkApplicationInfo){ VK_STRUCTURE_TYPE_APPLICATION_INFO };
    app.apiVersion = VK_API_VERSION_1_3;
    ici = (VkInstanceCreateInfo){ VK_STRUCTURE_TYPE_INSTANCE_CREATE_INFO };
    ici.pApplicationInfo = &app;
    ici.enabledExtensionCount = (uint32_t)vk->nb_enabled_inst_extensions;
    ici.ppEnabledExtensionNames = vk->enabled_inst_extensions;
    VkQueue cq = VK_NULL_HANDLE;
    vkGetDeviceQueue(g_pw_vkdev, g_pw_comp_qf, 0, &cq);
    pq = (pyrowave_device_create_queue_info){ cq, g_pw_comp_qf, 0 };

    pyrowave_device_create_info pci;
    memset(&pci, 0, sizeof(pci));
    pci.GetInstanceProcAddr = vkGetInstanceProcAddr;
    pci.instance = vk->inst;
    pci.physical_device = g_pw_phys;
    pci.device = g_pw_vkdev;
    pci.instance_create_info = &ici;
    pci.device_create_info = &dci;
    pci.queue_info = &pq;
    pci.queue_info_count = 1;
    pci.queue_lock_callback = pw_qlock;
    pci.queue_unlock_callback = pw_qunlock;
    pyrowave_result r = pyrowave_create_device(&pci, &g_pw_dev);
    if (r != PYROWAVE_SUCCESS || !g_pw_dev) {
        char msg[128];
        snprintf(msg, sizeof(msg), "pyrowave: pyrowave_create_device on the shared device failed (%d) -- cannot decode", (int)r);
        pw_log(msg);
        g_pw_dev = NULL;
        return 0;
    }
    pyrowave_device_set_queue_type(g_pw_dev, have_comp ? VK_QUEUE_COMPUTE_BIT : VK_QUEUE_GRAPHICS_BIT);

    VkSemaphoreTypeCreateInfo st = { VK_STRUCTURE_TYPE_SEMAPHORE_TYPE_CREATE_INFO };
    st.semaphoreType = VK_SEMAPHORE_TYPE_TIMELINE;
    VkSemaphoreCreateInfo sci = { VK_STRUCTURE_TYPE_SEMAPHORE_CREATE_INFO };
    sci.pNext = &st;
    if (vkCreateSemaphore(g_pw_vkdev, &sci, NULL, &g_pw_sem) != VK_SUCCESS) {
        pw_log("pyrowave: timeline semaphore creation failed -- cannot decode");
        return 0;
    }
    VkPhysicalDeviceProperties pr;
    vkGetPhysicalDeviceProperties(g_pw_phys, &pr);
    char msg[256];
    snprintf(msg, sizeof(msg), "pyrowave: decoding on the renderer's device (%s), compute queue family %u%s",
             pr.deviceName, g_pw_comp_qf, have_comp ? "" : " (shared with graphics)");
    pw_log(msg);
    g_pw_dev_state = 1;
    return 1;
}

static uint32_t pw_mem_type(uint32_t bits) {
    VkPhysicalDeviceMemoryProperties mp;
    vkGetPhysicalDeviceMemoryProperties(g_pw_phys, &mp);
    for (uint32_t i = 0; i < mp.memoryTypeCount; i++)
        if ((bits & (1u << i)) && (mp.memoryTypes[i].propertyFlags & VK_MEMORY_PROPERTY_DEVICE_LOCAL_BIT)) return i;
    for (uint32_t i = 0; i < mp.memoryTypeCount; i++)
        if (bits & (1u << i)) return i;
    return UINT32_MAX;
}

// The renderer must be done with every slot before they're destroyed: callers
// only get here between streams or on a size change, after waiting for idle.
static void pw_destroy_ring_and_decoder(void) {
    if (g_pw_dec) { pyrowave_decoder_destroy(g_pw_dec); g_pw_dec = NULL; }
    if (g_pw_vkdev) {
        vk_video_queue_lock();
        vkDeviceWaitIdle(g_pw_vkdev);
        vk_video_queue_unlock();
    }
    for (int i = 0; i < PW_RING; i++) {
        PwSlot *s = &g_pw_ring[i];
        if (s->img) { vk_video_forget_image((void *)s->img); vkDestroyImage(g_pw_vkdev, s->img, NULL); }
        if (s->mem) vkFreeMemory(g_pw_vkdev, s->mem, NULL);
        memset(s, 0, sizeof(*s));
    }
    g_pw_w = g_pw_h = 0;
    g_pw_444 = g_pw_hdr = 0;
    g_pw_fmt = VK_FORMAT_UNDEFINED;
    g_pw_next = 0;
}

static int pw_ring_busy(void) {
    for (int i = 0; i < PW_RING; i++) if (g_pw_ring[i].busy) return 1;
    return 0;
}

// pw_format_usable: the ring format can be written through R8/R16 plane
// storage views and sampled through a YCbCr conversion on this GPU.
static int pw_format_usable(VkFormat fmt) {
    VkFormatProperties fp;
    vkGetPhysicalDeviceFormatProperties(g_pw_phys, fmt, &fp);
    if (!(fp.optimalTilingFeatures & VK_FORMAT_FEATURE_SAMPLED_IMAGE_BIT)) return 0;
    VkImageFormatProperties ifp;
    return vkGetPhysicalDeviceImageFormatProperties(g_pw_phys, fmt, VK_IMAGE_TYPE_2D, VK_IMAGE_TILING_OPTIMAL,
               VK_IMAGE_USAGE_SAMPLED_BIT | VK_IMAGE_USAGE_STORAGE_BIT | VK_IMAGE_USAGE_TRANSFER_SRC_BIT,
               VK_IMAGE_CREATE_MUTABLE_FORMAT_BIT | VK_IMAGE_CREATE_EXTENDED_USAGE_BIT, &ifp) == VK_SUCCESS;
}

static int pw_ensure_decoder(int w, int h, int c444, int hdr) {
    if (g_pw_dec && g_pw_w == w && g_pw_h == h && g_pw_444 == c444 && g_pw_hdr == hdr) return 1;
    if (!pw_ensure_device()) return 0;
    if (g_pw_dec && pw_ring_busy()) return 0; // size/format change: wait for the renderer to give the old slots back
    pw_destroy_ring_and_decoder();
    VkFormat fmt = pw_ring_format(c444, hdr);
    if (!pw_format_usable(fmt)) {
        char msg[128];
        snprintf(msg, sizeof(msg), "pyrowave: this GPU cannot decode into %s%s planes", c444 ? "4:4:4" : "4:2:0", hdr ? " 16-bit" : "");
        pw_log(msg);
        return 0;
    }

    pyrowave_decoder_create_info info;
    memset(&info, 0, sizeof(info));
    info.device = g_pw_dev;
    info.width = w;
    info.height = h;
    info.chroma = c444 ? PYROWAVE_CHROMA_SUBSAMPLING_444 : PYROWAVE_CHROMA_SUBSAMPLING_420;
    info.fragment_path = false;
    vk_video_queue_lock(); // creation may upload on queues the renderer also uses
    pyrowave_result r = pyrowave_decoder_create(&info, &g_pw_dec);
    vk_video_queue_unlock();
    if (r != PYROWAVE_SUCCESS || !g_pw_dec) {
        g_pw_dec = NULL;
        char msg[96];
        snprintf(msg, sizeof(msg), "pyrowave: decoder create failed for %dx%d (%d)", w, h, (int)r);
        pw_log(msg);
        return 0;
    }

    // Ring: written by PyroWave's compute queue, sampled by the renderer's
    // graphics queue -- concurrent sharing, kept in GENERAL throughout.
    uint32_t fams[2] = { g_pw_gfx_qf, g_pw_comp_qf };
    for (int i = 0; i < PW_RING; i++) {
        PwSlot *s = &g_pw_ring[i];
        VkImageCreateInfo ici = { VK_STRUCTURE_TYPE_IMAGE_CREATE_INFO };
        ici.flags = VK_IMAGE_CREATE_MUTABLE_FORMAT_BIT | VK_IMAGE_CREATE_EXTENDED_USAGE_BIT;
        ici.imageType = VK_IMAGE_TYPE_2D;
        ici.format = fmt;
        ici.extent.width = w; ici.extent.height = h; ici.extent.depth = 1;
        ici.mipLevels = 1; ici.arrayLayers = 1; ici.samples = VK_SAMPLE_COUNT_1_BIT;
        ici.tiling = VK_IMAGE_TILING_OPTIMAL;
        ici.usage = VK_IMAGE_USAGE_SAMPLED_BIT | VK_IMAGE_USAGE_STORAGE_BIT | VK_IMAGE_USAGE_TRANSFER_SRC_BIT;
        ici.sharingMode = g_pw_gfx_qf != g_pw_comp_qf ? VK_SHARING_MODE_CONCURRENT : VK_SHARING_MODE_EXCLUSIVE;
        ici.queueFamilyIndexCount = g_pw_gfx_qf != g_pw_comp_qf ? 2 : 0;
        ici.pQueueFamilyIndices = fams;
        ici.initialLayout = VK_IMAGE_LAYOUT_UNDEFINED;
        if (vkCreateImage(g_pw_vkdev, &ici, NULL, &s->img) != VK_SUCCESS) goto fail;
        VkMemoryRequirements mr;
        vkGetImageMemoryRequirements(g_pw_vkdev, s->img, &mr);
        VkMemoryAllocateInfo mai = { VK_STRUCTURE_TYPE_MEMORY_ALLOCATE_INFO };
        mai.allocationSize = mr.size;
        mai.memoryTypeIndex = pw_mem_type(mr.memoryTypeBits);
        if (vkAllocateMemory(g_pw_vkdev, &mai, NULL, &s->mem) != VK_SUCCESS) goto fail;
        if (vkBindImageMemory(g_pw_vkdev, s->img, s->mem, 0) != VK_SUCCESS) goto fail;
        for (int pl = 0; pl < 3; pl++) {
            pyrowave_image_view *v = &g_pw_bufs[i].planes[pl];
            memset(v, 0, sizeof(*v));
            v->image = s->img;
            v->width = (uint32_t)w; v->height = (uint32_t)h;
            v->image_format = fmt;
            v->view_format = hdr ? VK_FORMAT_R16_UNORM : VK_FORMAT_R8_UNORM;
            v->aspect = (VkImageAspectFlagBits)(VK_IMAGE_ASPECT_PLANE_0_BIT << pl);
            v->swizzle = VK_COMPONENT_SWIZZLE_IDENTITY;
            v->layout = VK_IMAGE_LAYOUT_GENERAL;
        }
    }

    // UNDEFINED -> GENERAL once, on PyroWave's compute queue's family pool.
    {
        VkCommandPoolCreateInfo pci = { VK_STRUCTURE_TYPE_COMMAND_POOL_CREATE_INFO };
        pci.queueFamilyIndex = g_pw_comp_qf;
        VkCommandPool pool = VK_NULL_HANDLE;
        if (vkCreateCommandPool(g_pw_vkdev, &pci, NULL, &pool) != VK_SUCCESS) goto fail;
        VkCommandBufferAllocateInfo cai = { VK_STRUCTURE_TYPE_COMMAND_BUFFER_ALLOCATE_INFO };
        cai.commandPool = pool; cai.level = VK_COMMAND_BUFFER_LEVEL_PRIMARY; cai.commandBufferCount = 1;
        VkCommandBuffer cb;
        vkAllocateCommandBuffers(g_pw_vkdev, &cai, &cb);
        VkCommandBufferBeginInfo bi = { VK_STRUCTURE_TYPE_COMMAND_BUFFER_BEGIN_INFO };
        vkBeginCommandBuffer(cb, &bi);
        VkImageMemoryBarrier b[PW_RING];
        for (int i = 0; i < PW_RING; i++) {
            VkImageMemoryBarrier x = { VK_STRUCTURE_TYPE_IMAGE_MEMORY_BARRIER };
            x.oldLayout = VK_IMAGE_LAYOUT_UNDEFINED; x.newLayout = VK_IMAGE_LAYOUT_GENERAL;
            x.srcQueueFamilyIndex = VK_QUEUE_FAMILY_IGNORED; x.dstQueueFamilyIndex = VK_QUEUE_FAMILY_IGNORED;
            x.image = g_pw_ring[i].img;
            x.subresourceRange.aspectMask = VK_IMAGE_ASPECT_PLANE_0_BIT | VK_IMAGE_ASPECT_PLANE_1_BIT | VK_IMAGE_ASPECT_PLANE_2_BIT;
            x.subresourceRange.levelCount = 1; x.subresourceRange.layerCount = 1;
            b[i] = x;
        }
        vkCmdPipelineBarrier(cb, VK_PIPELINE_STAGE_TOP_OF_PIPE_BIT, VK_PIPELINE_STAGE_BOTTOM_OF_PIPE_BIT, 0, 0, NULL, 0, NULL, PW_RING, b);
        vkEndCommandBuffer(cb);
        VkQueue q;
        vkGetDeviceQueue(g_pw_vkdev, g_pw_comp_qf, 0, &q);
        VkSubmitInfo si = { VK_STRUCTURE_TYPE_SUBMIT_INFO };
        si.commandBufferCount = 1; si.pCommandBuffers = &cb;
        vk_video_queue_lock();
        VkResult sr = vkQueueSubmit(q, 1, &si, VK_NULL_HANDLE);
        if (sr == VK_SUCCESS) vkQueueWaitIdle(q);
        vk_video_queue_unlock();
        vkDestroyCommandPool(g_pw_vkdev, pool, NULL);
        if (sr != VK_SUCCESS) goto fail;
    }
    g_pw_w = w; g_pw_h = h;
    g_pw_444 = c444; g_pw_hdr = hdr;
    g_pw_fmt = fmt;
    {
        char msg[160];
        snprintf(msg, sizeof(msg), "pyrowave: GPU decoder ready %dx%d %s %s (zero-copy, %d-image ring)", w, h,
                 c444 ? "4:4:4" : "4:2:0", hdr ? "HDR10 BT.2020 PQ 16-bit" : "BT.709 8-bit", PW_RING);
        pw_log(msg);
    }
    return 1;
fail:
    pw_log("pyrowave: GPU ring creation failed -- cannot decode");
    pw_destroy_ring_and_decoder();
    return 0;
}

void pyrowave_win_release_slot(void *ctx) {
    PwSlot *s = (PwSlot *)ctx;
    if (s) InterlockedExchange(&s->busy, 0);
}

// pyrowave_win_decode decodes one access unit. On success it fills the
// image, format and semaphore value the renderer waits on, and the slot to
// release (pyrowave_win_release_slot); returns 1. Returns 0 if the unit could
// not be decoded or no slot is free (the frame is skipped -- every frame is a
// keyframe, so skipping costs nothing beyond the frame itself).
static int pyrowave_win_decode_dense(const uint8_t *au, size_t len, void **out_img, int *out_vkfmt, void **out_sem,
                                     uint64_t *out_val, int *out_w, int *out_h, int *out_narrow, void **out_slot) {
    InitOnceExecuteOnce(&g_pw_once, pw_init_once, NULL, NULL);
    // BitstreamSequenceHeader (pyrowave_common.hpp): width-1 in bits 0..13,
    // height-1 in 14..27, `extended` in bit 31 of word 0; chroma resolution in
    // bit 26 of word 1, transfer function (PQ) in bit 28. Same parse as
    // pyrowave_decode_linux.c.
    if (len < 8) { pw_fail("too short", (int)len); return 0; }
    uint32_t w0 = (uint32_t)au[0] | (uint32_t)au[1] << 8 | (uint32_t)au[2] << 16 | (uint32_t)au[3] << 24;
    uint32_t w1 = (uint32_t)au[4] | (uint32_t)au[5] << 8 | (uint32_t)au[6] << 16 | (uint32_t)au[7] << 24;
    if (!(w0 >> 31)) { pw_fail("no sequence header", (int)len); return 0; }
    int w = (int)(w0 & 0x3fff) + 1;
    int h = (int)((w0 >> 14) & 0x3fff) + 1;
    int c444 = (int)((w1 >> 26) & 1);
    int hdr = (int)((w1 >> 28) & 1);
    int narrow = (int)((w1 >> 30) & 1); // YCbCr range: 1 = limited (rust-shine's scaled path sends full)
    if ((w | h) & 1) { pw_fail("odd size", w); return 0; }

    int ok = 0;
    EnterCriticalSection(&g_pw_cs);
    if (!pw_ensure_decoder(w, h, c444, hdr)) goto out;
    PwSlot *slot = NULL;
    int si = 0;
    for (int i = 0; i < PW_RING; i++) {
        int k = (g_pw_next + i) % PW_RING;
        if (InterlockedCompareExchange(&g_pw_ring[k].busy, 1, 0) == 0) { slot = &g_pw_ring[k]; si = k; break; }
    }
    if (!slot) { pw_fail("renderer holds every slot", PW_RING); goto out; }
    g_pw_next = (si + 1) % PW_RING;

    pyrowave_decoder_clear(g_pw_dec); // anything queued belongs to a frame that never completed
    if (pyrowave_decoder_push_packet(g_pw_dec, au, len) != PYROWAVE_SUCCESS) { pw_fail("packet rejected", (int)len); goto release; }
    if (!pyrowave_decoder_decode_is_ready(g_pw_dec, false)) { pw_fail("frame incomplete", (int)len); goto release; }
    uint64_t v = ++g_pw_val;
    pyrowave_gpu_sync_operation rel;
    memset(&rel, 0, sizeof(rel));
    rel.sync.semaphore = g_pw_sem;
    rel.sync.value = v;
    if (pyrowave_decoder_decode_gpu_buffer(g_pw_dec, NULL, &rel, &g_pw_bufs[si]) != PYROWAVE_SUCCESS) {
        pw_fail("decode failed", (int)len);
        g_pw_val--;
        goto release;
    }
    *out_img = (void *)slot->img;
    *out_vkfmt = (int)g_pw_fmt;
    *out_sem = (void *)g_pw_sem;
    *out_val = v;
    *out_w = w;
    *out_h = h;
    *out_narrow = narrow;
    *out_slot = slot;
    if (InterlockedIncrement64(&g_pw_frames) == 1) {
        char msg[128];
        snprintf(msg, sizeof(msg), "pyrowave: first frame decoded on the GPU (%dx%d%s%s)", w, h, c444 ? " 4:4:4" : "", hdr ? " HDR" : "");
        pw_log(msg);
    }
    ok = 1;
    goto out;
release:
    InterlockedExchange(&slot->busy, 0);
out:
    LeaveCriticalSection(&g_pw_cs);
    return ok;
}

// pyrowave_win_decode takes the frame payload as it came off the wire (dense, record framed
// or length-prefixed -- see pyrowave_framing.h), reduces it to the dense access unit and decodes.
int pyrowave_win_decode(const uint8_t *au, size_t len, void **out_img, int *out_vkfmt, void **out_sem,
                        uint64_t *out_val, int *out_w, int *out_h, int *out_narrow, void **out_slot) {
    uint8_t *dense = (uint8_t *)malloc(len ? len : 1);
    if (!dense) return 0;
    size_t n = pw_unframe(au, len, dense);
    int ok = 0;
    if (n) ok = pyrowave_win_decode_dense(dense, n, out_img, out_vkfmt, out_sem, out_val, out_w, out_h, out_narrow, out_slot);
    else pw_fail("malformed framing", (int)len);
    free(dense);
    return ok;
}

// pyrowave_win_supported: a decoder can be created on this machine's renderer
// device (tried once, cached).
int pyrowave_win_supported(void) {
    InitOnceExecuteOnce(&g_pw_once, pw_init_once, NULL, NULL);
    EnterCriticalSection(&g_pw_cs);
    int ok = pw_ensure_device();
    LeaveCriticalSection(&g_pw_cs);
    return ok;
}

// pyrowave_win_color_supported: whether this GPU can decode PyroWave 4:4:4
// (c444) / HDR (hdr) streams -- the ring format must be storage-writable and
// sampleable. Decides which VIDEO_FORMAT_PYROWAVE_* bits the client offers.
int pyrowave_win_color_supported(int c444, int hdr) {
    if (!pyrowave_win_supported()) return 0;
    return pw_format_usable(pw_ring_format(c444, hdr));
}

uint64_t pyrowave_win_frames(void) {
    return (uint64_t)g_pw_frames;
}

// pyrowave_win_stream_reset: a new stream starts counting frames from zero.
// The decoder/ring stay for reuse; a size change rebuilds them.
void pyrowave_win_stream_reset(void) {
    InterlockedExchange64(&g_pw_frames, 0);
    g_pw_failures = 0;
}

#endif // _WIN32
