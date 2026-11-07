// vk_hwdev_bridge_windows.c — the shared Vulkan hwaccel device used by both
// video decode (moonlight_cgo_windows.go) and presentation
// (vk_video_impl_windows.c), so the two can share one VkDevice/VkImage with
// zero cross-device copy.
//
// This lives in its own translation unit rather than inline in
// moonlight_cgo_windows.go's cgo preamble comment because cgo duplicates any
// non-static function *body* written directly in a preamble comment into the
// generated _cgo_export.c (needed there to get type declarations right for
// //export'd Go functions) — linking then fails with "multiple definition".
// Functions defined in a plain .c file in the package don't have this
// problem; only the preamble's *declarations* of them are duplicated, which
// is fine.
//
// ffmpeg's own auto-created Vulkan device (av_hwdevice_ctx_create with no
// externally-supplied VkInstance/VkDevice) is the only device-creation path
// that's been proven to actually work for real Vulkan Video Decode on this
// codebase's target hardware/driver: handing ffmpeg a from-scratch,
// manually-created VkDevice (matching every extension and Vulkan11/12/13
// feature ffmpeg's own auto-create enables) reliably crashed deep in
// libavcodec's Vulkan decode internals. So this always lets ffmpeg create
// the device, and vk_video_impl_windows.c adopts it afterwards for its
// swapchain/presentation instead of creating a separate device of its own.

#ifdef _WIN32

#define VK_USE_PLATFORM_WIN32_KHR
#define COBJMACROS
#include <windows.h>
#include <vulkan/vulkan.h>
#include <vulkan/vulkan_win32.h>
#include <libavcodec/avcodec.h>
#include <libavutil/dict.h>
#include <libavutil/error.h>
#include <libavutil/hwcontext.h>
#include <libavutil/hwcontext_vulkan.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <dxgi.h>

extern void goVTLog(char *msg);

static AVBufferRef     *g_vk_hw_ctx      = NULL;
static CRITICAL_SECTION g_vk_cs;
static int              g_vk_cs_init     = 0;
static int              g_vk_hwdev_tried = 0;
static int              g_vk_hwdev_ok    = 0;

// GPU safe mode, process-wide (see gpu_safe_mode_windows.go):
//   0 normal
//   1 no D3D11VA->Vulkan interop (d3dx_device_ref answers NULL)
//   2 no Vulkan at all: the shared hwaccel device is not offered for decode
//     or presentation, so decode takes plain D3D11VA/software and the overlay
//     falls back to GDI.
// Raised (never lowered) at startup from USBRIDGE_RENDERER or a remembered
// GPU stall, or mid-session when the renderer reports the GPU stuck.
static volatile LONG g_gpu_safe_mode = 0;
void win_gpu_safe_mode_raise(int level) {
    LONG cur;
    while ((cur = g_gpu_safe_mode) < level &&
           InterlockedCompareExchange(&g_gpu_safe_mode, level, cur) != cur) {}
}
int win_gpu_safe_mode(void) { return (int)g_gpu_safe_mode; }

// win_intel_legacy_gpu: an Intel iGPU up to Gen11 (Ice Lake) -- HD/UHD/Iris
// of the 1st-10th Core generations, Atom/Celeron/Pentium of the same era.
// Their Windows driver is Intel's frozen legacy branch: on an HD Graphics
// 520 the D3D11->Vulkan shared fence never signalled (renderer froze, 3.2.23)
// and Vulkan Video Decode rejected every frame (no video, 3.2.27), so these
// skip Vulkan entirely (GPU safe mode 2: D3D11VA decode, D3D11 renderer,
// software decode if D3D11VA fails). Matched on the PCI device ID's high byte,
// not the name: a current Xe iGPU (i7-12650H) also calls itself just
// "Intel(R) UHD Graphics". The list is closed -- these generations are done
// -- so anything newer, and every non-Intel GPU, keeps the full pipeline.
// USBRIDGE_FAKE_GPU_ID=0x1916 pretends to be that device (testing).
int win_intel_legacy_gpu(unsigned vendor, unsigned device) {
    if (vendor != 0x8086) return 0;
    switch ((device >> 8) & 0xFF) {
    case 0x00: // Ironlake
    case 0x01: // Sandy Bridge, Ivy Bridge
    case 0x04: case 0x0A: case 0x0C: case 0x0D: // Haswell
    case 0x0F: // Bay Trail
    case 0x16: // Broadwell
    case 0x19: // Skylake (HD 510-580, Iris 540-580)
    case 0x22: // Cherry Trail / Braswell
    case 0x31: // Gemini Lake
    case 0x3E: // Coffee Lake, Whiskey Lake (UHD 610-630)
    case 0x59: // Kaby Lake (HD/UHD 610-650)
    case 0x5A: // Apollo Lake
    case 0x87: // Amber Lake
    case 0x8A: // Ice Lake (Iris Plus G1-G7)
    case 0x9B: // Comet Lake
        return 1;
    }
    return 0;
}

static unsigned win_fake_gpu_id(void) {
    const char *e = getenv("USBRIDGE_FAKE_GPU_ID");
    return (e && *e) ? (unsigned)strtoul(e, NULL, 0) : 0;
}

// win_legacy_intel_default_adapter: the default DXGI adapter -- the one the
// D3D11 device, and normally the display, use -- is a legacy Intel iGPU
// (win_intel_legacy_gpu). Fills name (UTF-8) and the device ID either way.
int win_legacy_intel_default_adapter(char *name, int n, unsigned *dev_id) {
    name[0] = 0; *dev_id = 0;
    IDXGIFactory1 *fac = NULL;
    if (CreateDXGIFactory1(&IID_IDXGIFactory1, (void **)&fac) != S_OK || !fac) return 0;
    IDXGIAdapter1 *ad = NULL;
    int legacy = 0;
    if (IDXGIFactory1_EnumAdapters1(fac, 0, &ad) == S_OK && ad) {
        DXGI_ADAPTER_DESC1 d;
        if (IDXGIAdapter1_GetDesc1(ad, &d) == S_OK) {
            WideCharToMultiByte(CP_UTF8, 0, d.Description, -1, name, n, NULL, NULL);
            unsigned vendor = d.VendorId, device = d.DeviceId;
            unsigned fake = win_fake_gpu_id();
            if (fake) { vendor = 0x8086; device = fake; }
            *dev_id = device;
            legacy = win_intel_legacy_gpu(vendor, device);
        }
        IDXGIAdapter1_Release(ad);
    }
    IDXGIFactory1_Release(fac);
    return legacy;
}

// win_vk_decode_supported: the shared device's GPU has a video decode queue
// for codec (0 H.264 High, 1 HEVC Main/Main10 by bit_depth) and its profile
// capabilities cover w x h. ffmpeg's decoder opens without checking any of
// this (an HD Graphics 520 then failed every frame), so win_av_init asks
// before choosing the Vulkan Video tier. why gets the reason on failure.
int win_vk_decode_supported(int codec, int bit_depth, int w, int h, char *why, int n);

// win_vk_hwdev_ensure lazily creates the shared Vulkan hwaccel device on
// first use (from whichever caller — vk_video_impl_windows.c's overlay init
// or win_av_init's decode probe — runs first) and is idempotent afterwards.
static int win_vk_hwdev_ensure(void) {
    if (!g_vk_cs_init) { InitializeCriticalSection(&g_vk_cs); g_vk_cs_init = 1; }
    EnterCriticalSection(&g_vk_cs);
    if (g_vk_hwdev_tried) {
        int ok = g_vk_hwdev_ok;
        LeaveCriticalSection(&g_vk_cs);
        return ok;
    }
    g_vk_hwdev_tried = 1;

    AVDictionary *opts = NULL;
    // VK_EXT_swapchain_colorspace: lets the surface report HDR10_ST2084 (HDR output).
    av_dict_set(&opts, "instance_extensions", "+VK_KHR_surface+VK_KHR_win32_surface+VK_EXT_swapchain_colorspace", 0);
    // external_memory/semaphore_win32: D3D11VA decode output is imported
    // into this device (d3d11_interop_windows.c).
    av_dict_set(&opts, "device_extensions", "+VK_KHR_swapchain+VK_KHR_external_memory_win32+VK_KHR_external_semaphore_win32", 0);
    AVBufferRef *hw_ctx = NULL;
    int err = av_hwdevice_ctx_create(&hw_ctx, AV_HWDEVICE_TYPE_VULKAN, NULL, opts, 0);
    av_dict_free(&opts);
    if (err < 0) {
        char errbuf[AV_ERROR_MAX_STRING_SIZE] = {0};
        av_strerror(err, errbuf, sizeof(errbuf));
        char msg[224];
        snprintf(msg, sizeof(msg), "libavcodec/win: Vulkan hwaccel device unavailable: %d (%s) -- decode+overlay fall back to D3D11VA/GDI", err, errbuf);
        goVTLog(msg);
        LeaveCriticalSection(&g_vk_cs);
        return 0;
    }

    AVHWDeviceContext      *hwctx = (AVHWDeviceContext*)hw_ctx->data;
    AVVulkanDeviceContext  *vkctx = (AVVulkanDeviceContext*)hwctx->hwctx;
    int gfxQF = -1;
    for (int i = 0; i < vkctx->nb_qf; i++) {
        if (vkctx->qf[i].flags & VK_QUEUE_GRAPHICS_BIT) { gfxQF = vkctx->qf[i].idx; break; }
    }
    if (gfxQF < 0 || !vkGetPhysicalDeviceWin32PresentationSupportKHR(vkctx->phys_dev, (uint32_t)gfxQF)) {
        goVTLog((char*)"libavcodec/win: Vulkan device has no presentable graphics queue -- falling back to D3D11VA/GDI");
        av_buffer_unref(&hw_ctx);
        LeaveCriticalSection(&g_vk_cs);
        return 0;
    }

    {
        // The GPU ffmpeg picked for Vulkan can differ from the default DXGI
        // adapter checked at startup (hybrid laptops), so check it as well.
        VkPhysicalDeviceProperties pr;
        vkGetPhysicalDeviceProperties(vkctx->phys_dev, &pr);
        unsigned vendor = pr.vendorID, device = pr.deviceID, fake = win_fake_gpu_id();
        if (fake) { vendor = 0x8086; device = fake; }
        if (win_intel_legacy_gpu(vendor, device)) {
            char msg[224];
            snprintf(msg, sizeof(msg), "libavcodec/win: Vulkan device %s (0x%04x) is a legacy Intel iGPU -- not using Vulkan, decode+overlay go to D3D11VA/D3D11", pr.deviceName, device);
            goVTLog(msg);
            win_gpu_safe_mode_raise(2);
            av_buffer_unref(&hw_ctx);
            LeaveCriticalSection(&g_vk_cs);
            return 0;
        }
    }
    g_vk_hw_ctx = hw_ctx;
    g_vk_hwdev_ok = 1;
    {
        VkPhysicalDeviceProperties pr;
        vkGetPhysicalDeviceProperties(vkctx->phys_dev, &pr);
        char msg[192];
        snprintf(msg, sizeof(msg), "libavcodec/win: Vulkan hwaccel device ready (GPU=%s) -- decode+render share one VkDevice", pr.deviceName);
        goVTLog(msg);
    }
    LeaveCriticalSection(&g_vk_cs);
    return 1;
}

// win_vk_hwdev_ctx_ref returns a new reference to the shared hwaccel device
// context (creating it on first call), or NULL if unavailable. Caller owns
// the returned ref and must av_buffer_unref it (or transfer ownership, e.g.
// to an AVCodecContext.hw_device_ctx).
AVBufferRef *win_vk_hwdev_ctx_ref(void) {
    if (win_gpu_safe_mode() >= 2) return NULL;
    if (!win_vk_hwdev_ensure()) return NULL;
    return av_buffer_ref(g_vk_hw_ctx);
}

// win_vk_hwdev_get exposes the shared device's raw handles as void* so
// vk_video_impl_windows.c (built without any ffmpeg headers) can adopt them
// for its own swapchain/presentation setup without this file needing to
// know anything about Win32 window/swapchain plumbing.
int win_vk_hwdev_get(void **out_inst, void **out_phys, void **out_dev, uint32_t *out_gfx_qf) {
    if (win_gpu_safe_mode() >= 2) return 0;
    if (!win_vk_hwdev_ensure()) return 0;
    AVHWDeviceContext     *hwctx = (AVHWDeviceContext*)g_vk_hw_ctx->data;
    AVVulkanDeviceContext *vkctx = (AVVulkanDeviceContext*)hwctx->hwctx;
    *out_inst = (void*)vkctx->inst;
    *out_phys = (void*)vkctx->phys_dev;
    *out_dev  = (void*)vkctx->act_dev;
    *out_gfx_qf = 0;
    for (int i = 0; i < vkctx->nb_qf; i++) {
        if (vkctx->qf[i].flags & VK_QUEUE_GRAPHICS_BIT) { *out_gfx_qf = (uint32_t)vkctx->qf[i].idx; break; }
    }
    return 1;
}

int win_vk_decode_supported(int codec, int bit_depth, int w, int h, char *why, int n) {
    why[0] = 0;
    if (!g_vk_hw_ctx) { snprintf(why, n, "no Vulkan device"); return 0; }
    const char *fake = getenv("USBRIDGE_FAKE_NO_VK_DECODE");
    if (fake && *fake && strcmp(fake, "0") != 0) { snprintf(why, n, "USBRIDGE_FAKE_NO_VK_DECODE"); return 0; }
    AVHWDeviceContext     *hwctx = (AVHWDeviceContext*)g_vk_hw_ctx->data;
    AVVulkanDeviceContext *vkctx = (AVVulkanDeviceContext*)hwctx->hwctx;
    VkPhysicalDevice pd = vkctx->phys_dev;
    VkVideoCodecOperationFlagBitsKHR op = codec == 1 ? VK_VIDEO_CODEC_OPERATION_DECODE_H265_BIT_KHR
                                                     : VK_VIDEO_CODEC_OPERATION_DECODE_H264_BIT_KHR;

    // A queue family that decodes this codec.
    PFN_vkGetPhysicalDeviceQueueFamilyProperties2 qfp2 = (PFN_vkGetPhysicalDeviceQueueFamilyProperties2)
        vkGetInstanceProcAddr(vkctx->inst, "vkGetPhysicalDeviceQueueFamilyProperties2");
    if (!qfp2) { snprintf(why, n, "no vkGetPhysicalDeviceQueueFamilyProperties2"); return 0; }
    uint32_t nq = 0;
    qfp2(pd, &nq, NULL);
    if (nq == 0 || nq > 64) { snprintf(why, n, "no queue families"); return 0; }
    VkQueueFamilyProperties2 qp[64];
    VkQueueFamilyVideoPropertiesKHR vp[64];
    for (uint32_t i = 0; i < nq; i++) {
        memset(&qp[i], 0, sizeof(qp[i])); memset(&vp[i], 0, sizeof(vp[i]));
        qp[i].sType = VK_STRUCTURE_TYPE_QUEUE_FAMILY_PROPERTIES_2;
        vp[i].sType = VK_STRUCTURE_TYPE_QUEUE_FAMILY_VIDEO_PROPERTIES_KHR;
        qp[i].pNext = &vp[i];
    }
    qfp2(pd, &nq, qp);
    int have_queue = 0;
    for (uint32_t i = 0; i < nq; i++)
        if ((qp[i].queueFamilyProperties.queueFlags & VK_QUEUE_VIDEO_DECODE_BIT_KHR) && (vp[i].videoCodecOperations & op))
            have_queue = 1;
    if (!have_queue) { snprintf(why, n, "no video decode queue for %s", codec == 1 ? "HEVC" : "H.264"); return 0; }

    // The profile's capabilities: supported at all, and large enough.
    PFN_vkGetPhysicalDeviceVideoCapabilitiesKHR caps_fn = (PFN_vkGetPhysicalDeviceVideoCapabilitiesKHR)
        vkGetInstanceProcAddr(vkctx->inst, "vkGetPhysicalDeviceVideoCapabilitiesKHR");
    if (!caps_fn) { snprintf(why, n, "no vkGetPhysicalDeviceVideoCapabilitiesKHR"); return 0; }
    VkVideoDecodeH264ProfileInfoKHR h264p = { VK_STRUCTURE_TYPE_VIDEO_DECODE_H264_PROFILE_INFO_KHR };
    h264p.stdProfileIdc = STD_VIDEO_H264_PROFILE_IDC_HIGH;
    h264p.pictureLayout = VK_VIDEO_DECODE_H264_PICTURE_LAYOUT_PROGRESSIVE_KHR;
    VkVideoDecodeH265ProfileInfoKHR h265p = { VK_STRUCTURE_TYPE_VIDEO_DECODE_H265_PROFILE_INFO_KHR };
    h265p.stdProfileIdc = bit_depth > 8 ? STD_VIDEO_H265_PROFILE_IDC_MAIN_10 : STD_VIDEO_H265_PROFILE_IDC_MAIN;
    VkVideoComponentBitDepthFlagsKHR depth = bit_depth > 8 ? VK_VIDEO_COMPONENT_BIT_DEPTH_10_BIT_KHR
                                                           : VK_VIDEO_COMPONENT_BIT_DEPTH_8_BIT_KHR;
    VkVideoProfileInfoKHR prof = { VK_STRUCTURE_TYPE_VIDEO_PROFILE_INFO_KHR };
    prof.pNext = codec == 1 ? (void *)&h265p : (void *)&h264p;
    prof.videoCodecOperation = op;
    prof.chromaSubsampling = VK_VIDEO_CHROMA_SUBSAMPLING_420_BIT_KHR;
    prof.lumaBitDepth = depth;
    prof.chromaBitDepth = depth;
    VkVideoDecodeH264CapabilitiesKHR h264c = { VK_STRUCTURE_TYPE_VIDEO_DECODE_H264_CAPABILITIES_KHR };
    VkVideoDecodeH265CapabilitiesKHR h265c = { VK_STRUCTURE_TYPE_VIDEO_DECODE_H265_CAPABILITIES_KHR };
    VkVideoDecodeCapabilitiesKHR dcaps = { VK_STRUCTURE_TYPE_VIDEO_DECODE_CAPABILITIES_KHR };
    dcaps.pNext = codec == 1 ? (void *)&h265c : (void *)&h264c;
    VkVideoCapabilitiesKHR caps = { VK_STRUCTURE_TYPE_VIDEO_CAPABILITIES_KHR };
    caps.pNext = &dcaps;
    VkResult r = caps_fn(pd, &prof, &caps);
    if (r != VK_SUCCESS) { snprintf(why, n, "%s profile not supported (res=%d)", codec == 1 ? (bit_depth > 8 ? "HEVC Main10" : "HEVC Main") : "H.264 High", (int)r); return 0; }
    if ((int)caps.maxCodedExtent.width < w || (int)caps.maxCodedExtent.height < h) {
        snprintf(why, n, "max %ux%u < %dx%d", caps.maxCodedExtent.width, caps.maxCodedExtent.height, w, h);
        return 0;
    }
    if (!(dcaps.flags & (VK_VIDEO_DECODE_CAPABILITY_DPB_AND_OUTPUT_COINCIDE_BIT_KHR | VK_VIDEO_DECODE_CAPABILITY_DPB_AND_OUTPUT_DISTINCT_BIT_KHR))) {
        snprintf(why, n, "no DPB/output mode");
        return 0;
    }
    snprintf(why, n, "max %ux%u", caps.maxCodedExtent.width, caps.maxCodedExtent.height);
    return 1;
}

// vk_frame_release_avframe is handed to vk_video_impl_windows.c as the
// release callback for zero-copy frames: it drops the AVFrame ref that was
// keeping the decoded VkImage's backing memory alive once the renderer's own
// GPU work reading it has retired (see vk_video_try_submit_vkframe's contract).
void vk_frame_release_avframe(void *ctx) {
    AVFrame *f = (AVFrame*)ctx;
    if (f) av_frame_free(&f);
}

#endif // _WIN32
