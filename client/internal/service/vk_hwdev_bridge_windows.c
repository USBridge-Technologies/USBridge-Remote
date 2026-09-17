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
#include <windows.h>
#include <vulkan/vulkan.h>
#include <vulkan/vulkan_win32.h>
#include <libavcodec/avcodec.h>
#include <libavutil/dict.h>
#include <libavutil/error.h>
#include <libavutil/hwcontext.h>
#include <libavutil/hwcontext_vulkan.h>
#include <stdio.h>

extern void goVTLog(char *msg);

static AVBufferRef     *g_vk_hw_ctx      = NULL;
static CRITICAL_SECTION g_vk_cs;
static int              g_vk_cs_init     = 0;
static int              g_vk_hwdev_tried = 0;
static int              g_vk_hwdev_ok    = 0;

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
    av_dict_set(&opts, "instance_extensions", "+VK_KHR_surface+VK_KHR_win32_surface", 0);
    av_dict_set(&opts, "device_extensions", "+VK_KHR_swapchain", 0);
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
    if (!win_vk_hwdev_ensure()) return NULL;
    return av_buffer_ref(g_vk_hw_ctx);
}

// win_vk_hwdev_get exposes the shared device's raw handles as void* so
// vk_video_impl_windows.c (built without any ffmpeg headers) can adopt them
// for its own swapchain/presentation setup without this file needing to
// know anything about Win32 window/swapchain plumbing.
int win_vk_hwdev_get(void **out_inst, void **out_phys, void **out_dev, uint32_t *out_gfx_qf) {
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

// vk_frame_release_avframe is handed to vk_video_impl_windows.c as the
// release callback for zero-copy frames: it drops the AVFrame ref that was
// keeping the decoded VkImage's backing memory alive once the renderer's own
// GPU work reading it has retired (see vk_video_try_submit_vkframe's contract).
void vk_frame_release_avframe(void *ctx) {
    AVFrame *f = (AVFrame*)ctx;
    if (f) av_frame_free(&f);
}

#endif // _WIN32
