// d3d11_interop_windows.c — D3D11VA decode feeding the Vulkan renderer.
//
// D3D11VA is what moonlight-qt decodes with on Windows, and on a Radeon 780M
// it decodes 4K HEVC at ~232 fps with one thread, against ~151 fps for
// ffmpeg's Vulkan Video decoder (docs/WINDOWS_DECODE_PIPELINE.md). Our
// renderer is Vulkan, so each decoded frame takes one GPU-side copy:
//
//   D3D11VA decoder array slice --CopySubresourceRegion--> shared NV12/P010
//   texture (ring of D3DX_RING) --imported as VkImage--> vk_video_impl
//
// On NVIDIA (and anything that isn't AMD) the copy is a D3D11 video
// processor blit into BGRA8 (SDR) / RGB10A2 PQ BT.2020 (HDR10) instead:
// NVIDIA's Vulkan driver accepts an imported NV12 D3D11 texture but reads its
// chroma plane wrong (luma byte-exact, ~90% of chroma bytes differ -- ghosted
// colors on screen), while single-plane RGB formats import byte-exact on both
// vendors (tools/decode_bench/d3d11_interop_bench.c, BENCH_RGB/BENCH_VERIFY).
// AMD keeps the plain copy: it's verified exact and ~2ms faster there.
// USBRIDGE_D3DX_MODE=nv12|rgb overrides the choice.
//
// HDR10 (P010) on a display that is not in HDR mode always goes through the
// video processor, on any vendor, which tone-maps PQ/BT.2020 down to SDR
// BGRA8 -- shown on an SDR swapchain, raw PQ would look washed out.
//
// Either way the result is ordered by a shared ID3D11Fence that Vulkan imports as a timeline
// semaphore: the decode thread signals value N after the copy, the render
// thread waits for N before sampling. The copy is a few hundred
// microseconds of GPU time, and the renderer reads its own image, never one
// libavcodec still uses as a reference.
//
// The D3D11 device is created on the adapter whose LUID matches the shared
// Vulkan device (vk_hwdev_bridge_windows.c), and lives as long as that
// device does: the process. The ring is recreated when the frame size or
// format changes.
//
// Own translation unit for the same cgo "multiple definition" reason as
// vk_hwdev_bridge_windows.c.

#ifdef _WIN32

#define COBJMACROS
#define INITGUID
#define VK_USE_PLATFORM_WIN32_KHR
#include <windows.h>
#include <d3d11_4.h>
#include <dxgi1_6.h>
#include <vulkan/vulkan.h>
#include <vulkan/vulkan_win32.h>
#include <libavcodec/avcodec.h>
#include <libavutil/hwcontext.h>
#include <libavutil/hwcontext_d3d11va.h>
#include <libavutil/hwcontext_vulkan.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

extern void goVTLog(char *msg);
extern AVBufferRef *win_vk_hwdev_ctx_ref(void);
extern void vk_video_forget_image(void *vk_image);
extern int vk_video_hdr_display_available(void);

#define D3DX_RING 4

static CRITICAL_SECTION g_dx_cs;
static int g_dx_cs_init = 0;
static int g_dx_tried = 0;
static AVBufferRef *g_dx_hwdev = NULL;          // ffmpeg D3D11VA device wrapping g_dx_dev
static AVD3D11VADeviceContext *g_dx_avctx = NULL;
static ID3D11Device5 *g_dx_dev = NULL;
static ID3D11DeviceContext4 *g_dx_ctx = NULL;
static ID3D11Fence *g_dx_fence = NULL;
static UINT64 g_dx_fence_val = 0;

static VkDevice g_dx_vkdev = VK_NULL_HANDLE;
static VkPhysicalDevice g_dx_vkphys = VK_NULL_HANDLE;
static VkSemaphore g_dx_vksem = VK_NULL_HANDLE;

static int g_dx_use_vp = 0; // convert with the video processor instead of copying planar frames
static ID3D11VideoDevice *g_dx_vdev = NULL;
static ID3D11VideoContext *g_dx_vctx = NULL;
static ID3D11VideoContext1 *g_dx_vctx1 = NULL; // color spaces beyond BT.601/709 SDR (HDR10)

typedef struct {
    ID3D11Texture2D *tex;
    ID3D11VideoProcessorOutputView *vout; // video-processor mode only
    VkImage img;
    VkDeviceMemory mem;
    volatile LONG busy; // handed to the renderer, not yet released
} DxSlot;
typedef struct {
    DxSlot slot[D3DX_RING];
    int w, h;
    DXGI_FORMAT src_fmt;   // decoder output format (NV12/P010) the ring was made for
    VkFormat vkfmt;        // what the renderer samples
    int vp;                // 1: video processor blit, 0: plain copy
    int hdr_display;       // HDR10 swapchain was available when the ring was made (HDR sources only)
    ID3D11VideoProcessorEnumerator *venum;
    ID3D11VideoProcessor *vproc;
    int next;
} DxRing;
// g_dx_cur is the ring in use. A size/format change retires it to
// g_dx_old instead of destroying it, since the renderer may still hold one
// of its slots; it's freed once none are busy.
static DxRing *g_dx_cur = NULL;
static DxRing *g_dx_old = NULL;

static void dx_log(const char *m) { goVTLog((char *)m); }

static uint32_t dx_mem_type(uint32_t bits) {
    VkPhysicalDeviceMemoryProperties mp;
    vkGetPhysicalDeviceMemoryProperties(g_dx_vkphys, &mp);
    for (uint32_t i = 0; i < mp.memoryTypeCount; i++)
        if ((bits & (1u << i)) && (mp.memoryTypes[i].propertyFlags & VK_MEMORY_PROPERTY_DEVICE_LOCAL_BIT)) return i;
    for (uint32_t i = 0; i < mp.memoryTypeCount; i++)
        if (bits & (1u << i)) return i;
    return UINT32_MAX;
}

// d3dx_device_ref returns a new reference to a D3D11VA hwdevice on the same
// GPU as the shared Vulkan device, with the shared fence imported into
// Vulkan -- or NULL if any piece is missing (no D3D11.4 fences, no external
// memory/semaphore support, no LUID match...), in which case the caller
// falls back to Vulkan Video decode.
AVBufferRef *d3dx_device_ref(void) {
    if (!g_dx_cs_init) { InitializeCriticalSection(&g_dx_cs); g_dx_cs_init = 1; }
    EnterCriticalSection(&g_dx_cs);
    if (g_dx_tried) {
        AVBufferRef *r = g_dx_hwdev ? av_buffer_ref(g_dx_hwdev) : NULL;
        LeaveCriticalSection(&g_dx_cs);
        return r;
    }
    g_dx_tried = 1;
    const char *fail = NULL;
    IDXGIFactory1 *fac = NULL;
    IDXGIAdapter1 *pick = NULL;
    ID3D11Device *dev0 = NULL;
    ID3D11DeviceContext *ctx0 = NULL;
    HANDLE fh = NULL;

    AVBufferRef *vkref = win_vk_hwdev_ctx_ref();
    if (!vkref) { fail = "no shared Vulkan device"; goto out; }
    AVVulkanDeviceContext *vk = (AVVulkanDeviceContext *)((AVHWDeviceContext *)vkref->data)->hwctx;
    g_dx_vkdev = vk->act_dev;
    g_dx_vkphys = vk->phys_dev;
    av_buffer_unref(&vkref); // the device itself lives for the process (vk_hwdev_bridge_windows.c)

    VkPhysicalDeviceIDProperties idp = { VK_STRUCTURE_TYPE_PHYSICAL_DEVICE_ID_PROPERTIES };
    VkPhysicalDeviceProperties2 pp = { VK_STRUCTURE_TYPE_PHYSICAL_DEVICE_PROPERTIES_2 };
    pp.pNext = &idp;
    vkGetPhysicalDeviceProperties2(g_dx_vkphys, &pp);
    if (!idp.deviceLUIDValid) { fail = "Vulkan device has no LUID"; goto out; }

    if (CreateDXGIFactory1(&IID_IDXGIFactory1, (void **)&fac) != S_OK) { fail = "CreateDXGIFactory1"; goto out; }
    IDXGIAdapter1 *ad = NULL;
    for (UINT i = 0; IDXGIFactory1_EnumAdapters1(fac, i, &ad) == S_OK; i++) {
        DXGI_ADAPTER_DESC1 d;
        IDXGIAdapter1_GetDesc1(ad, &d);
        if (!memcmp(&d.AdapterLuid, idp.deviceLUID, sizeof(LUID))) {
            pick = ad;
            const char *mode = getenv("USBRIDGE_D3DX_MODE");
            if (mode && _stricmp(mode, "rgb") == 0) g_dx_use_vp = 1;
            else if (mode && _stricmp(mode, "nv12") == 0) g_dx_use_vp = 0;
            else g_dx_use_vp = d.VendorId != 0x1002; // AMD: plain copy (see header)
            break;
        }
        IDXGIAdapter1_Release(ad);
    }
    if (!pick) { fail = "no DXGI adapter matches the Vulkan device"; goto out; }
    if (D3D11CreateDevice((IDXGIAdapter *)pick, D3D_DRIVER_TYPE_UNKNOWN, NULL, D3D11_CREATE_DEVICE_VIDEO_SUPPORT,
                          NULL, 0, D3D11_SDK_VERSION, &dev0, NULL, &ctx0) != S_OK) { fail = "D3D11CreateDevice"; goto out; }
    if (ID3D11Device_QueryInterface(dev0, &IID_ID3D11Device5, (void **)&g_dx_dev) != S_OK) { fail = "no ID3D11Device5 (fences)"; goto out; }
    if (ID3D11DeviceContext_QueryInterface(ctx0, &IID_ID3D11DeviceContext4, (void **)&g_dx_ctx) != S_OK) { fail = "no ID3D11DeviceContext4"; goto out; }

    if (ID3D11Device5_CreateFence(g_dx_dev, 0, D3D11_FENCE_FLAG_SHARED, &IID_ID3D11Fence, (void **)&g_dx_fence) != S_OK) { fail = "CreateFence"; goto out; }
    if (ID3D11Fence_CreateSharedHandle(g_dx_fence, NULL, GENERIC_ALL, NULL, &fh) != S_OK) { fail = "fence CreateSharedHandle"; goto out; }
    PFN_vkImportSemaphoreWin32HandleKHR importSem =
        (PFN_vkImportSemaphoreWin32HandleKHR)vkGetDeviceProcAddr(g_dx_vkdev, "vkImportSemaphoreWin32HandleKHR");
    if (!importSem || !vkGetDeviceProcAddr(g_dx_vkdev, "vkGetMemoryWin32HandlePropertiesKHR")) {
        fail = "Vulkan device lacks VK_KHR_external_{memory,semaphore}_win32"; goto out;
    }
    VkSemaphoreTypeCreateInfo stci = { VK_STRUCTURE_TYPE_SEMAPHORE_TYPE_CREATE_INFO };
    stci.semaphoreType = VK_SEMAPHORE_TYPE_TIMELINE;
    VkSemaphoreCreateInfo sci = { VK_STRUCTURE_TYPE_SEMAPHORE_CREATE_INFO };
    sci.pNext = &stci;
    if (vkCreateSemaphore(g_dx_vkdev, &sci, NULL, &g_dx_vksem) != VK_SUCCESS) { fail = "vkCreateSemaphore"; goto out; }
    VkImportSemaphoreWin32HandleInfoKHR isi = { VK_STRUCTURE_TYPE_IMPORT_SEMAPHORE_WIN32_HANDLE_INFO_KHR };
    isi.semaphore = g_dx_vksem;
    isi.handleType = VK_EXTERNAL_SEMAPHORE_HANDLE_TYPE_D3D11_FENCE_BIT;
    isi.handle = fh;
    if (importSem(g_dx_vkdev, &isi) != VK_SUCCESS) { fail = "import ID3D11Fence into Vulkan"; goto out; }

    AVBufferRef *hw = av_hwdevice_ctx_alloc(AV_HWDEVICE_TYPE_D3D11VA);
    if (!hw) { fail = "av_hwdevice_ctx_alloc(D3D11VA)"; goto out; }
    g_dx_avctx = (AVD3D11VADeviceContext *)((AVHWDeviceContext *)hw->data)->hwctx;
    g_dx_avctx->device = dev0; // ffmpeg owns this reference now
    dev0 = NULL;
    if (av_hwdevice_ctx_init(hw) < 0) { av_buffer_unref(&hw); g_dx_avctx = NULL; fail = "av_hwdevice_ctx_init(D3D11VA)"; goto out; }
    g_dx_hwdev = hw;
    if (ID3D11Device5_QueryInterface(g_dx_dev, &IID_ID3D11VideoDevice, (void **)&g_dx_vdev) != S_OK ||
        ID3D11DeviceContext4_QueryInterface(g_dx_ctx, &IID_ID3D11VideoContext, (void **)&g_dx_vctx) != S_OK) {
        g_dx_vdev = NULL; g_dx_vctx = NULL;
        if (g_dx_use_vp) dx_log("libavcodec/win: D3D11 video processor unavailable -- copying planar frames instead");
        g_dx_use_vp = 0;
    } else if (ID3D11DeviceContext4_QueryInterface(g_dx_ctx, &IID_ID3D11VideoContext1, (void **)&g_dx_vctx1) != S_OK) {
        g_dx_vctx1 = NULL; // SDR still works through the D3D11.0 color space API; no HDR10 color spaces
    }

out:
    if (fh) CloseHandle(fh);
    if (ctx0) ID3D11DeviceContext_Release(ctx0);
    if (dev0) ID3D11Device_Release(dev0);
    if (pick) IDXGIAdapter1_Release(pick);
    if (fac) IDXGIFactory1_Release(fac);
    char msg[200];
    if (fail) {
        snprintf(msg, sizeof(msg), "libavcodec/win: D3D11VA->Vulkan interop unavailable: %s", fail);
    } else {
        snprintf(msg, sizeof(msg), "libavcodec/win: D3D11VA->Vulkan interop ready (shared fence imported, %s)",
                 g_dx_use_vp ? "video processor -> RGB" : "planar copy");
    }
    dx_log(msg);
    AVBufferRef *r = g_dx_hwdev ? av_buffer_ref(g_dx_hwdev) : NULL;
    LeaveCriticalSection(&g_dx_cs);
    return r;
}

static int dx_ring_busy(DxRing *r) {
    for (int i = 0; i < D3DX_RING; i++) if (r->slot[i].busy) return 1;
    return 0;
}

static void dx_ring_free(DxRing *r) {
    if (!r) return;
    for (int i = 0; i < D3DX_RING; i++) {
        DxSlot *s = &r->slot[i];
        if (s->img) { vk_video_forget_image((void *)s->img); vkDestroyImage(g_dx_vkdev, s->img, NULL); }
        if (s->mem) vkFreeMemory(g_dx_vkdev, s->mem, NULL);
        if (s->vout) ID3D11VideoProcessorOutputView_Release(s->vout);
        if (s->tex) ID3D11Texture2D_Release(s->tex);
    }
    if (r->vproc) ID3D11VideoProcessor_Release(r->vproc);
    if (r->venum) ID3D11VideoProcessorEnumerator_Release(r->venum);
    free(r);
}

static DxRing *dx_ring_create(int w, int h, DXGI_FORMAT src_fmt, int hdr_display) {
    int vp = g_dx_use_vp;
    int hdr = src_fmt == DXGI_FORMAT_P010;
    int hdr_out = hdr && hdr_display;  // keep PQ for an HDR10 swapchain
    int tonemap = hdr && !hdr_display; // PQ -> SDR in the video processor
    if (tonemap && g_dx_vctx1) vp = 1;
    if (hdr && vp && !g_dx_vctx1) vp = 0; // no HDR10 color spaces on this driver; planar P010 still renders
    DXGI_FORMAT fmt = vp ? (hdr_out ? DXGI_FORMAT_R10G10B10A2_UNORM : DXGI_FORMAT_B8G8R8A8_UNORM) : src_fmt;
    VkFormat vkfmt = vp ? (hdr_out ? VK_FORMAT_A2B10G10R10_UNORM_PACK32 : VK_FORMAT_B8G8R8A8_UNORM)
                        : (hdr ? VK_FORMAT_G10X6_B10X6R10X6_2PLANE_420_UNORM_3PACK16 : VK_FORMAT_G8_B8R8_2PLANE_420_UNORM);
    PFN_vkGetMemoryWin32HandlePropertiesKHR getProps =
        (PFN_vkGetMemoryWin32HandlePropertiesKHR)vkGetDeviceProcAddr(g_dx_vkdev, "vkGetMemoryWin32HandlePropertiesKHR");
    DxRing *r = (DxRing *)calloc(1, sizeof(DxRing));
    if (!r) return NULL;
    r->w = w; r->h = h; r->src_fmt = src_fmt; r->vkfmt = vkfmt; r->vp = vp; r->hdr_display = hdr_display;
    for (int i = 0; i < D3DX_RING; i++) {
        DxSlot *s = &r->slot[i];
        D3D11_TEXTURE2D_DESC td = { 0 };
        td.Width = w; td.Height = h; td.MipLevels = 1; td.ArraySize = 1; td.Format = fmt;
        td.SampleDesc.Count = 1; td.Usage = D3D11_USAGE_DEFAULT;
        td.BindFlags = D3D11_BIND_SHADER_RESOURCE | (vp ? D3D11_BIND_RENDER_TARGET : 0);
        td.MiscFlags = D3D11_RESOURCE_MISC_SHARED | D3D11_RESOURCE_MISC_SHARED_NTHANDLE;
        if (ID3D11Device5_CreateTexture2D(g_dx_dev, &td, NULL, &s->tex) != S_OK) goto fail;
        IDXGIResource1 *res = NULL;
        HANDLE shared = NULL;
        ID3D11Texture2D_QueryInterface(s->tex, &IID_IDXGIResource1, (void **)&res);
        HRESULT hr = res ? IDXGIResource1_CreateSharedHandle(res, NULL, DXGI_SHARED_RESOURCE_READ | DXGI_SHARED_RESOURCE_WRITE, NULL, &shared) : E_FAIL;
        if (res) IDXGIResource1_Release(res);
        if (hr != S_OK) goto fail;

        VkExternalMemoryImageCreateInfo emi = { VK_STRUCTURE_TYPE_EXTERNAL_MEMORY_IMAGE_CREATE_INFO };
        emi.handleTypes = VK_EXTERNAL_MEMORY_HANDLE_TYPE_D3D11_TEXTURE_BIT;
        VkImageCreateInfo ici = { VK_STRUCTURE_TYPE_IMAGE_CREATE_INFO };
        ici.pNext = &emi; ici.imageType = VK_IMAGE_TYPE_2D; ici.format = vkfmt;
        ici.extent.width = w; ici.extent.height = h; ici.extent.depth = 1;
        ici.mipLevels = 1; ici.arrayLayers = 1; ici.samples = VK_SAMPLE_COUNT_1_BIT;
        ici.tiling = VK_IMAGE_TILING_OPTIMAL;
        ici.usage = VK_IMAGE_USAGE_SAMPLED_BIT | VK_IMAGE_USAGE_TRANSFER_SRC_BIT;
        ici.initialLayout = VK_IMAGE_LAYOUT_UNDEFINED;
        int ok = vkCreateImage(g_dx_vkdev, &ici, NULL, &s->img) == VK_SUCCESS;
        VkMemoryWin32HandlePropertiesKHR hp = { VK_STRUCTURE_TYPE_MEMORY_WIN32_HANDLE_PROPERTIES_KHR };
        ok = ok && getProps(g_dx_vkdev, VK_EXTERNAL_MEMORY_HANDLE_TYPE_D3D11_TEXTURE_BIT, shared, &hp) == VK_SUCCESS;
        if (ok) {
            VkMemoryRequirements mr;
            vkGetImageMemoryRequirements(g_dx_vkdev, s->img, &mr);
            VkMemoryDedicatedAllocateInfo ded = { VK_STRUCTURE_TYPE_MEMORY_DEDICATED_ALLOCATE_INFO };
            ded.image = s->img;
            VkImportMemoryWin32HandleInfoKHR imp = { VK_STRUCTURE_TYPE_IMPORT_MEMORY_WIN32_HANDLE_INFO_KHR };
            imp.pNext = &ded; imp.handleType = VK_EXTERNAL_MEMORY_HANDLE_TYPE_D3D11_TEXTURE_BIT; imp.handle = shared;
            VkMemoryAllocateInfo mai = { VK_STRUCTURE_TYPE_MEMORY_ALLOCATE_INFO };
            mai.pNext = &imp; mai.allocationSize = mr.size;
            mai.memoryTypeIndex = dx_mem_type(mr.memoryTypeBits & hp.memoryTypeBits);
            ok = vkAllocateMemory(g_dx_vkdev, &mai, NULL, &s->mem) == VK_SUCCESS &&
                 vkBindImageMemory(g_dx_vkdev, s->img, s->mem, 0) == VK_SUCCESS;
        }
        CloseHandle(shared);
        if (!ok) goto fail;
    }
    if (vp) {
        D3D11_VIDEO_PROCESSOR_CONTENT_DESC cd = { 0 };
        cd.InputFrameFormat = D3D11_VIDEO_FRAME_FORMAT_PROGRESSIVE;
        cd.InputWidth = w; cd.InputHeight = h; cd.OutputWidth = w; cd.OutputHeight = h;
        cd.Usage = D3D11_VIDEO_USAGE_OPTIMAL_SPEED;
        if (ID3D11VideoDevice_CreateVideoProcessorEnumerator(g_dx_vdev, &cd, &r->venum) != S_OK ||
            ID3D11VideoDevice_CreateVideoProcessor(g_dx_vdev, r->venum, 0, &r->vproc) != S_OK) goto fail;
        for (int i = 0; i < D3DX_RING; i++) {
            D3D11_VIDEO_PROCESSOR_OUTPUT_VIEW_DESC ovd = { 0 };
            ovd.ViewDimension = D3D11_VPOV_DIMENSION_TEXTURE2D;
            if (ID3D11VideoDevice_CreateVideoProcessorOutputView(g_dx_vdev, (ID3D11Resource *)r->slot[i].tex, r->venum,
                                                                 &ovd, &r->slot[i].vout) != S_OK) goto fail;
        }
        // SDR: same conversion as the renderer's own sampler (BT.601 studio
        // range in, full range out). HDR10: keep PQ / BT.2020 -- the output
        // is still PQ-encoded, for an HDR10 swapchain.
        if (g_dx_vctx1) {
            ID3D11VideoContext1_VideoProcessorSetStreamColorSpace1(g_dx_vctx1, r->vproc, 0,
                hdr ? DXGI_COLOR_SPACE_YCBCR_STUDIO_G2084_LEFT_P2020 : DXGI_COLOR_SPACE_YCBCR_STUDIO_G22_LEFT_P601);
            ID3D11VideoContext1_VideoProcessorSetOutputColorSpace1(g_dx_vctx1, r->vproc,
                hdr_out ? DXGI_COLOR_SPACE_RGB_FULL_G2084_NONE_P2020 : DXGI_COLOR_SPACE_RGB_FULL_G22_NONE_P709);
        } else {
            D3D11_VIDEO_PROCESSOR_COLOR_SPACE in = { 0 }, out = { 0 };
            in.YCbCr_Matrix = 0; in.Nominal_Range = D3D11_VIDEO_PROCESSOR_NOMINAL_RANGE_16_235;
            out.RGB_Range = 0; out.Nominal_Range = D3D11_VIDEO_PROCESSOR_NOMINAL_RANGE_0_255;
            ID3D11VideoContext_VideoProcessorSetStreamColorSpace(g_dx_vctx, r->vproc, 0, &in);
            ID3D11VideoContext_VideoProcessorSetOutputColorSpace(g_dx_vctx, r->vproc, &out);
        }
        ID3D11VideoContext_VideoProcessorSetStreamAutoProcessingMode(g_dx_vctx, r->vproc, 0, FALSE);
        ID3D11VideoContext_VideoProcessorSetStreamFrameFormat(g_dx_vctx, r->vproc, 0, D3D11_VIDEO_FRAME_FORMAT_PROGRESSIVE);
        // Only the frame's w x h: the decoder's surfaces are sized for the
        // mode the stream asked for (800x600, 1920x1088), and without a
        // source rect the Blt scales the whole surface into the output. A
        // host that keeps its desktop's aspect (800x450 for 800x600) then
        // shows squeezed into the top (or left) with the unused part of the
        // surface as a black bar, and the mouse maps onto the whole rect.
        RECT rc = { 0, 0, w, h };
        ID3D11VideoContext_VideoProcessorSetStreamSourceRect(g_dx_vctx, r->vproc, 0, TRUE, &rc);
        ID3D11VideoContext_VideoProcessorSetStreamDestRect(g_dx_vctx, r->vproc, 0, TRUE, &rc);
        ID3D11VideoContext_VideoProcessorSetOutputTargetRect(g_dx_vctx, r->vproc, TRUE, &rc);
    }
    {
        char msg[200];
        snprintf(msg, sizeof(msg), "libavcodec/win: D3D11VA->Vulkan ring ready: %d x %dx%d %s -> %s",
                 D3DX_RING, w, h, hdr ? "P010" : "NV12",
                 !vp ? "planar copy" : hdr_out ? "RGB10A2 PQ BT.2020 (video processor)"
                     : tonemap ? "BGRA8, HDR tone-mapped to SDR (video processor)" : "BGRA8 (video processor)");
        dx_log(msg);
    }
    return r;
fail:
    dx_ring_free(r);
    return NULL;
}

void d3dx_release_slot(void *ctx) {
    DxSlot *s = (DxSlot *)ctx;
    if (s) InterlockedExchange(&s->busy, 0);
}

// d3dx_deliver copies one decoded D3D11VA frame into a free ring slot and
// signals the shared fence. On success it fills the VkImage/format/fence
// value the renderer should use and returns 1; the renderer gives the slot
// back through d3dx_release_slot(*out_release_ctx). Returns 0 (frame
// dropped, nothing to release) if no slot is free or the frame can't be
// mapped -- the renderer only ever shows the newest frame anyway.
int d3dx_deliver(AVFrame *frame, void **out_img, int *out_vkfmt, void **out_sem, uint64_t *out_val, void **out_release_ctx) {
    if (!g_dx_hwdev || frame->format != AV_PIX_FMT_D3D11 || !frame->hw_frames_ctx) return 0;
    AVHWFramesContext *fc = (AVHWFramesContext *)frame->hw_frames_ctx->data;
    DXGI_FORMAT fmt;
    if (fc->sw_format == AV_PIX_FMT_NV12) fmt = DXGI_FORMAT_NV12;
    else if (fc->sw_format == AV_PIX_FMT_P010) fmt = DXGI_FORMAT_P010;
    else return 0;
    int w = frame->width, h = frame->height;
    if (g_dx_old && !dx_ring_busy(g_dx_old)) { dx_ring_free(g_dx_old); g_dx_old = NULL; }
    int hdr_display = fmt == DXGI_FORMAT_P010 ? vk_video_hdr_display_available() : 0;
    if (!g_dx_cur || w != g_dx_cur->w || h != g_dx_cur->h || fmt != g_dx_cur->src_fmt || hdr_display != g_dx_cur->hdr_display) {
        if (g_dx_old) return 0; // two size changes in flight; wait for the renderer to let go
        DxRing *r = dx_ring_create(w, h, fmt, hdr_display);
        if (!r) {
            static int logged = 0;
            if (!logged++) dx_log("libavcodec/win: D3D11VA->Vulkan ring creation failed -- frames dropped");
            return 0;
        }
        g_dx_old = g_dx_cur;
        g_dx_cur = r;
    }
    DxRing *ring = g_dx_cur;
    DxSlot *slot = NULL;
    for (int i = 0; i < D3DX_RING; i++) {
        DxSlot *s = &ring->slot[(ring->next + i) % D3DX_RING];
        if (InterlockedCompareExchange(&s->busy, 1, 0) == 0) {
            slot = s;
            ring->next = (int)((s - ring->slot) + 1) % D3DX_RING;
            break;
        }
    }
    if (!slot) return 0;

    ID3D11Texture2D *src = (ID3D11Texture2D *)frame->data[0];
    UINT index = (UINT)(intptr_t)frame->data[1];
    D3D11_BOX box = { 0, 0, 0, (UINT)w, (UINT)h, 1 };
    g_dx_avctx->lock(g_dx_avctx->lock_ctx);
    if (ring->vp) {
        D3D11_VIDEO_PROCESSOR_INPUT_VIEW_DESC ivd = { 0 };
        ivd.ViewDimension = D3D11_VPIV_DIMENSION_TEXTURE2D;
        ivd.Texture2D.ArraySlice = index;
        ID3D11VideoProcessorInputView *iv = NULL;
        HRESULT hr = ID3D11VideoDevice_CreateVideoProcessorInputView(g_dx_vdev, (ID3D11Resource *)src, ring->venum, &ivd, &iv);
        if (hr == S_OK) {
            D3D11_VIDEO_PROCESSOR_STREAM st = { 0 };
            st.Enable = TRUE;
            st.pInputSurface = iv;
            hr = ID3D11VideoContext_VideoProcessorBlt(g_dx_vctx, ring->vproc, slot->vout, 0, 1, &st);
            ID3D11VideoProcessorInputView_Release(iv);
        }
        if (hr != S_OK) {
            g_dx_avctx->unlock(g_dx_avctx->lock_ctx);
            InterlockedExchange(&slot->busy, 0);
            static int logged = 0;
            if (!logged++) {
                char msg[96];
                snprintf(msg, sizeof(msg), "libavcodec/win: VideoProcessorBlt failed 0x%08lx", (unsigned long)hr);
                dx_log(msg);
            }
            return 0;
        }
    } else {
        ID3D11DeviceContext4_CopySubresourceRegion(g_dx_ctx, (ID3D11Resource *)slot->tex, 0, 0, 0, 0,
                                                   (ID3D11Resource *)src, index, &box);
    }
    UINT64 v = ++g_dx_fence_val;
    ID3D11DeviceContext4_Signal(g_dx_ctx, g_dx_fence, v);
    ID3D11DeviceContext4_Flush(g_dx_ctx);
    g_dx_avctx->unlock(g_dx_avctx->lock_ctx);

    *out_img = (void *)slot->img;
    *out_vkfmt = (int)ring->vkfmt;
    *out_sem = (void *)g_dx_vksem;
    *out_val = v;
    *out_release_ctx = slot;
    return 1;
}

// d3dx_download: CPU copy of a D3D11VA frame for AI Vision / the pre-overlay
// fallback, without stalling decode. av_hwframe_transfer_data maps its
// staging texture while holding ffmpeg's device lock, so it waits for the
// whole GPU queue -- with AI Vision's DirectML inference on the same GPU that
// is long enough to block D3D11VA decode (which takes the same lock) past
// moonlight-common-c's 15-frame queue, and every recovery IDR overflowed it
// again. Here the lock is held only to queue the copy and to try a
// non-blocking Map; between attempts it is released.
static ID3D11Texture2D *g_dx_stage = NULL;
static int g_dx_stage_w = 0, g_dx_stage_h = 0;
static DXGI_FORMAT g_dx_stage_fmt = DXGI_FORMAT_UNKNOWN;

AVFrame *d3dx_download(AVFrame *frame) {
    if (!g_dx_hwdev || frame->format != AV_PIX_FMT_D3D11 || !frame->hw_frames_ctx) return NULL;
    AVHWFramesContext *fc = (AVHWFramesContext *)frame->hw_frames_ctx->data;
    DXGI_FORMAT fmt;
    if (fc->sw_format == AV_PIX_FMT_NV12) fmt = DXGI_FORMAT_NV12;
    else if (fc->sw_format == AV_PIX_FMT_P010) fmt = DXGI_FORMAT_P010;
    else return NULL;
    int w = frame->width, h = frame->height;
    int bpp = fmt == DXGI_FORMAT_P010 ? 2 : 1;

    // Only the readback thread calls this, so the staging texture needs no
    // lock of its own.
    if (!g_dx_stage || g_dx_stage_w != w || g_dx_stage_h != h || g_dx_stage_fmt != fmt) {
        if (g_dx_stage) { ID3D11Texture2D_Release(g_dx_stage); g_dx_stage = NULL; }
        D3D11_TEXTURE2D_DESC td = { 0 };
        td.Width = w; td.Height = h; td.MipLevels = 1; td.ArraySize = 1; td.Format = fmt;
        td.SampleDesc.Count = 1; td.Usage = D3D11_USAGE_STAGING; td.CPUAccessFlags = D3D11_CPU_ACCESS_READ;
        if (ID3D11Device5_CreateTexture2D(g_dx_dev, &td, NULL, &g_dx_stage) != S_OK) { g_dx_stage = NULL; return NULL; }
        g_dx_stage_w = w; g_dx_stage_h = h; g_dx_stage_fmt = fmt;
    }

    ID3D11Texture2D *src = (ID3D11Texture2D *)frame->data[0];
    UINT index = (UINT)(intptr_t)frame->data[1];
    D3D11_BOX box = { 0, 0, 0, (UINT)w, (UINT)h, 1 };
    g_dx_avctx->lock(g_dx_avctx->lock_ctx);
    ID3D11DeviceContext4_CopySubresourceRegion(g_dx_ctx, (ID3D11Resource *)g_dx_stage, 0, 0, 0, 0,
                                               (ID3D11Resource *)src, index, &box);
    ID3D11DeviceContext4_Flush(g_dx_ctx);
    g_dx_avctx->unlock(g_dx_avctx->lock_ctx);

    D3D11_MAPPED_SUBRESOURCE m;
    HRESULT hr = DXGI_ERROR_WAS_STILL_DRAWING;
    for (int tries = 0; tries < 2000; tries++) { // ~2s ceiling
        g_dx_avctx->lock(g_dx_avctx->lock_ctx);
        hr = ID3D11DeviceContext4_Map(g_dx_ctx, (ID3D11Resource *)g_dx_stage, 0, D3D11_MAP_READ,
                                      D3D11_MAP_FLAG_DO_NOT_WAIT, &m);
        if (hr != DXGI_ERROR_WAS_STILL_DRAWING) break; // mapped (lock still held) or failed
        g_dx_avctx->unlock(g_dx_avctx->lock_ctx);
        Sleep(1);
    }
    if (hr != S_OK) {
        if (hr != DXGI_ERROR_WAS_STILL_DRAWING) g_dx_avctx->unlock(g_dx_avctx->lock_ctx);
        return NULL;
    }

    AVFrame *sw = av_frame_alloc();
    int ok = sw != NULL;
    if (ok) {
        sw->format = fc->sw_format; sw->width = w; sw->height = h;
        ok = av_frame_get_buffer(sw, 0) == 0;
    }
    if (ok) {
        // NV12/P010: luma plane, then interleaved chroma at RowPitch * height
        // (D3D11 staging textures lay planar formats out contiguously).
        const uint8_t *base_ptr = (const uint8_t *)m.pData;
        for (int y = 0; y < h; y++)
            memcpy(sw->data[0] + (size_t)y * sw->linesize[0], base_ptr + (size_t)y * m.RowPitch, (size_t)w * bpp);
        const uint8_t *uv = base_ptr + (size_t)m.RowPitch * h;
        for (int y = 0; y < (h + 1) / 2; y++)
            memcpy(sw->data[1] + (size_t)y * sw->linesize[1], uv + (size_t)y * m.RowPitch, (size_t)w * bpp);
    }
    ID3D11DeviceContext4_Unmap(g_dx_ctx, (ID3D11Resource *)g_dx_stage, 0);
    g_dx_avctx->unlock(g_dx_avctx->lock_ctx);
    if (!ok) av_frame_free(&sw);
    return sw;
}

// ---- HDR capability (service.HdrDisplaySupported, hdr_display_supported_windows.go)

static BOOL CALLBACK dx_find_own_window(HWND hwnd, LPARAM lp) {
    DWORD pid = 0;
    GetWindowThreadProcessId(hwnd, &pid);
    if (pid == GetCurrentProcessId() && IsWindowVisible(hwnd) && !GetWindow(hwnd, GW_OWNER)) {
        *(HWND *)lp = hwnd;
        return FALSE;
    }
    return TRUE;
}

// win_hdr_output_active: Windows HDR is on for the monitor this app's main
// window is on (the primary monitor if no window is up yet).
int win_hdr_output_active(void) {
    HWND own = NULL;
    EnumWindows(dx_find_own_window, (LPARAM)&own);
    HMONITOR mon = own ? MonitorFromWindow(own, MONITOR_DEFAULTTOPRIMARY)
                       : MonitorFromPoint((POINT){ 0, 0 }, MONITOR_DEFAULTTOPRIMARY);
    IDXGIFactory1 *fac = NULL;
    if (CreateDXGIFactory1(&IID_IDXGIFactory1, (void **)&fac) != S_OK) return 0;
    int hdr = 0, found = 0;
    IDXGIAdapter1 *ad = NULL;
    for (UINT i = 0; !found && IDXGIFactory1_EnumAdapters1(fac, i, &ad) == S_OK; i++) {
        IDXGIOutput *out = NULL;
        for (UINT j = 0; !found && IDXGIAdapter1_EnumOutputs(ad, j, &out) == S_OK; j++) {
            IDXGIOutput6 *o6 = NULL;
            if (IDXGIOutput_QueryInterface(out, &IID_IDXGIOutput6, (void **)&o6) == S_OK) {
                DXGI_OUTPUT_DESC1 d;
                if (IDXGIOutput6_GetDesc1(o6, &d) == S_OK && d.Monitor == mon) {
                    found = 1;
                    hdr = d.ColorSpace == DXGI_COLOR_SPACE_RGB_FULL_G2084_NONE_P2020;
                }
                IDXGIOutput6_Release(o6);
            }
            IDXGIOutput_Release(out);
        }
        IDXGIAdapter1_Release(ad);
    }
    IDXGIFactory1_Release(fac);
    return hdr;
}

// d3dx_hevc_main10_supported: the interop is up and its D3D11VA decoder
// takes HEVC Main10 to P010. Cached after the first answer.
int d3dx_hevc_main10_supported(void) {
    static int cached = -1;
    if (cached >= 0) return cached;
    AVBufferRef *ref = d3dx_device_ref();
    if (!ref) return cached = 0;
    av_buffer_unref(&ref);
    BOOL ok = FALSE;
    if (g_dx_vdev)
        ID3D11VideoDevice_CheckVideoDecoderFormat(g_dx_vdev, &D3D11_DECODER_PROFILE_HEVC_VLD_MAIN10, DXGI_FORMAT_P010, &ok);
    return cached = ok ? 1 : 0;
}

#endif // _WIN32
