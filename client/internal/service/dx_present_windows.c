// dx_present_windows.c — Direct3D 11 presentation for the video overlay window.
//
// The fallback between the Vulkan renderer and GDI (GPU safe mode level 2,
// or no working Vulkan at all -- VMs, old iGPUs, broken Vulkan drivers).
// vk_video_impl_windows.c still owns the overlay window, its input queues,
// menu holes and standalone fullscreen; with this backend its render thread
// calls in here instead of into Vulkan.
//
// D3D11 is there on practically every Windows machine: feature level 10.0+
// on any GPU of the last 15 years, and WARP (the CPU rasterizer that ships
// with Windows) where there is no usable GPU driver at all, so this backend
// almost never fails to start.
//
// Frames:
//   - D3D11VA decode on the shared device below: one GPU copy of the
//     decoder's array slice into an NV12/P010 texture, converted to RGB in
//     the pixel shader and scaled on the GPU. No CPU readback, no sws_scale.
//   - Anything else (software decode, a decoder on another device, a GPU
//     without NV12 shader views): the RGBA frame from the CPU path, uploaded
//     to a texture and scaled on the GPU.
//
// The shared device (dxs_*) lives for the process, like the shared Vulkan
// device: moonlight_cgo_windows.go's plain D3D11VA tier decodes on it
// (win_dx_hwdev_ref), so the renderer can sample its frames directly. Both
// sides take the same lock around the immediate context.
//
// Shaders are compiled at startup with d3dcompiler_47.dll (part of Windows
// 10/11). Without it the backend is unavailable and the caller uses GDI.
//
// Own translation unit for the same cgo "multiple definition" reason as
// vk_hwdev_bridge_windows.c.

#ifdef _WIN32

#define COBJMACROS
#define INITGUID
#include <windows.h>
#include <d3d11.h>
#include <dxgi1_2.h>
#include <d3dcommon.h>
#include <libavutil/frame.h>
#include <libavutil/hwcontext.h>
#include <libavutil/hwcontext_d3d11va.h>
#include <libavutil/pixfmt.h>
#include <stdarg.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <math.h>

extern void goVKLog(char *msg, int level);

static void dxp_log(int level, const char *fmt, ...) {
    char m[320];
    va_list ap;
    va_start(ap, fmt);
    vsnprintf(m, sizeof(m), fmt, ap);
    va_end(ap);
    goVKLog(m, level);
}

// ─── shared device ───────────────────────────────────────────────────────────

static INIT_ONCE        g_dxs_once = INIT_ONCE_STATIC_INIT;
static CRITICAL_SECTION g_dxs_lock;
static ID3D11Device        *g_dxs_dev = NULL;
static ID3D11DeviceContext *g_dxs_ctx = NULL;
static int  g_dxs_video = 0;        // created with VIDEO_SUPPORT on a hardware adapter
static int  g_dxs_warp = 0;
static int  g_dxs_nv12 = 0;         // NV12 textures can be sampled through R8/R8G8 views
static int  g_dxs_p010 = 0;         // same for P010 through R16/R16G16
static D3D_FEATURE_LEVEL g_dxs_fl = 0;
static char g_dxs_name[128] = "";

static void dxs_lock_cb(void *ctx)   { (void)ctx; EnterCriticalSection(&g_dxs_lock); }
static void dxs_unlock_cb(void *ctx) { (void)ctx; LeaveCriticalSection(&g_dxs_lock); }
static void dxs_lock(void)   { EnterCriticalSection(&g_dxs_lock); }
static void dxs_unlock(void) { LeaveCriticalSection(&g_dxs_lock); }

static HRESULT dxs_create(D3D_DRIVER_TYPE type, UINT flags) {
    static const D3D_FEATURE_LEVEL levels[] = {
        D3D_FEATURE_LEVEL_11_1, D3D_FEATURE_LEVEL_11_0, D3D_FEATURE_LEVEL_10_1, D3D_FEATURE_LEVEL_10_0,
    };
    HRESULT hr = D3D11CreateDevice(NULL, type, NULL, flags, levels, 4, D3D11_SDK_VERSION,
                                   &g_dxs_dev, &g_dxs_fl, &g_dxs_ctx);
    if (hr == E_INVALIDARG) // runtime without 11.1
        hr = D3D11CreateDevice(NULL, type, NULL, flags, levels + 1, 3, D3D11_SDK_VERSION,
                               &g_dxs_dev, &g_dxs_fl, &g_dxs_ctx);
    return hr;
}

static int dxs_format_sampleable(DXGI_FORMAT f) {
    UINT s = 0;
    if (FAILED(ID3D11Device_CheckFormatSupport(g_dxs_dev, f, &s))) return 0;
    return (s & D3D11_FORMAT_SUPPORT_TEXTURE2D) && (s & D3D11_FORMAT_SUPPORT_SHADER_SAMPLE);
}

static BOOL CALLBACK dxs_init_once(PINIT_ONCE o, PVOID p, PVOID *c) {
    (void)o; (void)p; (void)c;
    InitializeCriticalSection(&g_dxs_lock);
    if (getenv("USBRIDGE_D3D11_WARP")) { // testing: the CPU rasterizer a VM without a GPU driver gets
        if (SUCCEEDED(dxs_create(D3D_DRIVER_TYPE_WARP, D3D11_CREATE_DEVICE_BGRA_SUPPORT))) g_dxs_warp = 1;
    }
    if (!g_dxs_dev) {
        if (SUCCEEDED(dxs_create(D3D_DRIVER_TYPE_HARDWARE,
                                 D3D11_CREATE_DEVICE_BGRA_SUPPORT | D3D11_CREATE_DEVICE_VIDEO_SUPPORT)))
            g_dxs_video = 1;
        else if (SUCCEEDED(dxs_create(D3D_DRIVER_TYPE_HARDWARE, D3D11_CREATE_DEVICE_BGRA_SUPPORT)))
            ;
        else if (SUCCEEDED(dxs_create(D3D_DRIVER_TYPE_WARP, D3D11_CREATE_DEVICE_BGRA_SUPPORT)))
            g_dxs_warp = 1;
    }
    if (!g_dxs_dev) { dxp_log(2, "d3d11: no D3D11 device (hardware or WARP)"); return TRUE; }

    // Decode (ffmpeg's threads) and this renderer share the immediate
    // context; the lock below orders whole sequences, this makes each call
    // safe on its own.
    ID3D10Multithread *mt = NULL;
    if (SUCCEEDED(ID3D11Device_QueryInterface(g_dxs_dev, &IID_ID3D10Multithread, (void **)&mt)) && mt) {
        ID3D10Multithread_SetMultithreadProtected(mt, TRUE);
        ID3D10Multithread_Release(mt);
    }

    IDXGIDevice *xd = NULL;
    if (SUCCEEDED(ID3D11Device_QueryInterface(g_dxs_dev, &IID_IDXGIDevice, (void **)&xd)) && xd) {
        IDXGIAdapter *ad = NULL;
        if (SUCCEEDED(IDXGIDevice_GetAdapter(xd, &ad)) && ad) {
            DXGI_ADAPTER_DESC d;
            if (SUCCEEDED(IDXGIAdapter_GetDesc(ad, &d)))
                WideCharToMultiByte(CP_UTF8, 0, d.Description, -1, g_dxs_name, sizeof(g_dxs_name), NULL, NULL);
            IDXGIAdapter_Release(ad);
        }
        IDXGIDevice_Release(xd);
    }
    g_dxs_nv12 = dxs_format_sampleable(DXGI_FORMAT_NV12);
    g_dxs_p010 = dxs_format_sampleable(DXGI_FORMAT_P010);
    dxp_log(0, "d3d11: device on %s%s, feature level %x, video decode %s, NV12 views %s, P010 views %s",
            g_dxs_name[0] ? g_dxs_name : "?", g_dxs_warp ? " (WARP)" : "", (unsigned)g_dxs_fl,
            g_dxs_video ? "yes" : "no", g_dxs_nv12 ? "yes" : "no", g_dxs_p010 ? "yes" : "no");
    return TRUE;
}

static int dxs_ensure(void) {
    InitOnceExecuteOnce(&g_dxs_once, dxs_init_once, NULL, NULL);
    return g_dxs_dev != NULL;
}

// win_dx_hwdev_ref: an ffmpeg D3D11VA device wrapping the shared device, for
// moonlight_cgo_windows.go's plain D3D11VA decode tier. NULL when there is no
// hardware device with video support (the caller then creates its own).
AVBufferRef *win_dx_hwdev_ref(void) {
    if (!dxs_ensure() || !g_dxs_video) return NULL;
    AVBufferRef *ref = av_hwdevice_ctx_alloc(AV_HWDEVICE_TYPE_D3D11VA);
    if (!ref) return NULL;
    AVHWDeviceContext *dc = (AVHWDeviceContext *)ref->data;
    AVD3D11VADeviceContext *h = (AVD3D11VADeviceContext *)dc->hwctx;
    ID3D11Device_AddRef(g_dxs_dev); // ffmpeg releases its own reference
    h->device   = g_dxs_dev;
    h->lock     = dxs_lock_cb;
    h->unlock   = dxs_unlock_cb;
    h->lock_ctx = NULL;
    if (av_hwdevice_ctx_init(ref) < 0) { av_buffer_unref(&ref); return NULL; }
    return ref;
}

// dxp_available: a D3D11 device exists and d3dcompiler_47.dll loads.
typedef HRESULT (WINAPI *PFN_D3DCOMPILE)(LPCVOID, SIZE_T, LPCSTR, const D3D_SHADER_MACRO *, ID3DInclude *,
                                         LPCSTR, LPCSTR, UINT, UINT, ID3DBlob **, ID3DBlob **);
static PFN_D3DCOMPILE g_d3dcompile = NULL;

static int dxp_load_compiler(void) {
    if (g_d3dcompile) return 1;
    HMODULE m = LoadLibraryW(L"d3dcompiler_47.dll");
    if (!m) { dxp_log(2, "d3d11: d3dcompiler_47.dll not found"); return 0; }
    g_d3dcompile = (PFN_D3DCOMPILE)(void *)GetProcAddress(m, "D3DCompile");
    if (!g_d3dcompile) { dxp_log(2, "d3d11: D3DCompile not exported"); return 0; }
    return 1;
}

// ─── renderer ────────────────────────────────────────────────────────────────

static const char g_hlsl[] =
    "cbuffer C : register(b0) { float4 rect; float4 m0; float4 m1; float4 m2; float4 opt; };\n"
    "Texture2D t0 : register(t0); Texture2D t1 : register(t1); SamplerState s0 : register(s0);\n"
    "struct V { float4 pos : SV_Position; float2 uv : TEXCOORD0; };\n"
    "V vs(uint id : SV_VertexID) {\n"
    "  float2 t = float2(id & 1, id >> 1);\n"
    "  V o; o.pos = float4(lerp(rect.x, rect.z, t.x), lerp(rect.y, rect.w, t.y), 0, 1); o.uv = t; return o;\n"
    "}\n"
    "float4 ps_rgb(V i) : SV_Target { return float4(t0.Sample(s0, i.uv).rgb, 1); }\n"
    "float4 ps_ovl(V i) : SV_Target { return t0.Sample(s0, i.uv); }\n"
    "float3 pq_to_nits(float3 e) {\n"
    "  float3 p = pow(saturate(e), 1.0 / 78.84375);\n"
    "  return 10000.0 * pow(max(p - 0.8359375, 0) / (18.8515625 - 18.6875 * p), 1.0 / 0.1593017578125);\n"
    "}\n"
    "float4 ps_yuv(V i) : SV_Target {\n"
    "  float3 yuv = float3(t0.Sample(s0, i.uv).r, t1.Sample(s0, i.uv).rg);\n"
    "  float3 rgb = float3(dot(m0.xyz, yuv) + m0.w, dot(m1.xyz, yuv) + m1.w, dot(m2.xyz, yuv) + m2.w);\n"
    "  if (opt.x > 0.5) {\n"
    // HDR10 on an SDR swapchain: PQ -> linear (SDR white = 203 nits),
    // BT.2020 -> BT.709 primaries, extended Reinhard up to 1000 nits, gamma.
    "    float3 l = pq_to_nits(rgb) / 203.0;\n"
    "    l = float3(dot(float3(1.6605, -0.5876, -0.0728), l), dot(float3(-0.1246, 1.1329, -0.0083), l),\n"
    "               dot(float3(-0.0182, -0.1006, 1.1187), l));\n"
    "    l = max(l, 0); l = l * (1 + l / 24.3) / (1 + l);\n"
    "    rgb = pow(saturate(l), 1.0 / 2.2);\n"
    "  }\n"
    "  return float4(saturate(rgb), 1);\n"
    "}\n";

typedef struct { float rect[4]; float m0[4]; float m1[4]; float m2[4]; float opt[4]; } DxpConsts;

static HWND                     g_hwnd = NULL;
static IDXGISwapChain          *g_sc = NULL;
static ID3D11RenderTargetView  *g_rtv = NULL;
static int                      g_sc_w = 0, g_sc_h = 0;
static int                      g_flip = 0;
static ID3D11VertexShader      *g_vs = NULL;
static ID3D11PixelShader       *g_ps_rgb = NULL, *g_ps_yuv = NULL, *g_ps_ovl = NULL;
static ID3D11SamplerState      *g_samp = NULL;
static ID3D11BlendState        *g_blend = NULL;
static ID3D11RasterizerState   *g_rs = NULL;
static ID3D11Buffer            *g_cb = NULL;

// Video source: kind 1 = RGBA texture, 2 = NV12/P010 texture.
static int g_src_kind = 0, g_src_w = 0, g_src_h = 0, g_src_hdr = 0;
static float g_src_m[3][4];
static ID3D11Texture2D          *g_rgb_tex = NULL;
static ID3D11ShaderResourceView *g_rgb_srv = NULL;
static int g_rgb_w = 0, g_rgb_h = 0;
static ID3D11Texture2D          *g_yuv_tex = NULL;
static ID3D11ShaderResourceView *g_yuv_srv_y = NULL, *g_yuv_srv_c = NULL;
static int g_yuv_w = 0, g_yuv_h = 0;
static DXGI_FORMAT g_yuv_fmt = DXGI_FORMAT_UNKNOWN;
static int g_yuv_warned = 0;

// Overlays drawn over GPU-sourced frames (the CPU path burns them in).
typedef struct { ID3D11Texture2D *tex; ID3D11ShaderResourceView *srv; int w, h, on; } DxpOvl;
static DxpOvl g_hud, g_aiv;

#define REL(p, T) do { if (p) { T##_Release(p); (p) = NULL; } } while (0)

static void dxp_release_targets(void) { REL(g_rtv, ID3D11RenderTargetView); }

static int dxp_make_rtv(void) {
    ID3D11Texture2D *bb = NULL;
    if (FAILED(IDXGISwapChain_GetBuffer(g_sc, 0, &IID_ID3D11Texture2D, (void **)&bb)) || !bb) return 0;
    HRESULT hr = ID3D11Device_CreateRenderTargetView(g_dxs_dev, (ID3D11Resource *)bb, NULL, &g_rtv);
    ID3D11Texture2D_Release(bb);
    return SUCCEEDED(hr);
}

static ID3DBlob *dxp_compile(const char *entry, const char *target) {
    ID3DBlob *code = NULL, *err = NULL;
    HRESULT hr = g_d3dcompile(g_hlsl, sizeof(g_hlsl) - 1, "usbridge", NULL, NULL, entry, target,
                              (1 << 15) /* OPTIMIZATION_LEVEL3 */, 0, &code, &err);
    if (FAILED(hr)) {
        dxp_log(2, "d3d11: compiling %s failed: %s", entry,
                err ? (const char *)ID3D10Blob_GetBufferPointer(err) : "?");
        if (err) ID3D10Blob_Release(err);
        return NULL;
    }
    if (err) ID3D10Blob_Release(err);
    return code;
}

static int dxp_make_ps(const char *entry, ID3D11PixelShader **out) {
    ID3DBlob *b = dxp_compile(entry, "ps_4_0");
    if (!b) return 0;
    HRESULT hr = ID3D11Device_CreatePixelShader(g_dxs_dev, ID3D10Blob_GetBufferPointer(b),
                                                ID3D10Blob_GetBufferSize(b), NULL, out);
    ID3D10Blob_Release(b);
    return SUCCEEDED(hr);
}

static int dxp_make_pipeline(void) {
    ID3DBlob *b = dxp_compile("vs", "vs_4_0");
    if (!b) return 0;
    HRESULT hr = ID3D11Device_CreateVertexShader(g_dxs_dev, ID3D10Blob_GetBufferPointer(b),
                                                 ID3D10Blob_GetBufferSize(b), NULL, &g_vs);
    ID3D10Blob_Release(b);
    if (FAILED(hr)) return 0;
    if (!dxp_make_ps("ps_rgb", &g_ps_rgb) || !dxp_make_ps("ps_yuv", &g_ps_yuv) || !dxp_make_ps("ps_ovl", &g_ps_ovl))
        return 0;

    D3D11_SAMPLER_DESC sd = { 0 };
    sd.Filter = D3D11_FILTER_MIN_MAG_MIP_LINEAR;
    sd.AddressU = sd.AddressV = sd.AddressW = D3D11_TEXTURE_ADDRESS_CLAMP;
    sd.ComparisonFunc = D3D11_COMPARISON_NEVER;
    sd.MaxLOD = D3D11_FLOAT32_MAX;
    if (FAILED(ID3D11Device_CreateSamplerState(g_dxs_dev, &sd, &g_samp))) return 0;

    D3D11_BLEND_DESC bd = { 0 };
    bd.RenderTarget[0].BlendEnable = TRUE;
    bd.RenderTarget[0].SrcBlend = D3D11_BLEND_SRC_ALPHA;
    bd.RenderTarget[0].DestBlend = D3D11_BLEND_INV_SRC_ALPHA;
    bd.RenderTarget[0].BlendOp = D3D11_BLEND_OP_ADD;
    bd.RenderTarget[0].SrcBlendAlpha = D3D11_BLEND_ONE;
    bd.RenderTarget[0].DestBlendAlpha = D3D11_BLEND_INV_SRC_ALPHA;
    bd.RenderTarget[0].BlendOpAlpha = D3D11_BLEND_OP_ADD;
    bd.RenderTarget[0].RenderTargetWriteMask = D3D11_COLOR_WRITE_ENABLE_ALL;
    if (FAILED(ID3D11Device_CreateBlendState(g_dxs_dev, &bd, &g_blend))) return 0;

    D3D11_RASTERIZER_DESC rd = { 0 };
    rd.FillMode = D3D11_FILL_SOLID;
    rd.CullMode = D3D11_CULL_NONE;
    rd.DepthClipEnable = TRUE;
    if (FAILED(ID3D11Device_CreateRasterizerState(g_dxs_dev, &rd, &g_rs))) return 0;

    D3D11_BUFFER_DESC cd = { 0 };
    cd.ByteWidth = sizeof(DxpConsts);
    cd.Usage = D3D11_USAGE_DYNAMIC;
    cd.BindFlags = D3D11_BIND_CONSTANT_BUFFER;
    cd.CPUAccessFlags = D3D11_CPU_ACCESS_WRITE;
    return SUCCEEDED(ID3D11Device_CreateBuffer(g_dxs_dev, &cd, NULL, &g_cb));
}

static int dxp_make_swapchain(HWND hwnd, int w, int h) {
    IDXGIDevice *xd = NULL;
    IDXGIAdapter *ad = NULL;
    IDXGIFactory1 *f1 = NULL;
    IDXGIFactory2 *f2 = NULL;
    int ok = 0;
    if (FAILED(ID3D11Device_QueryInterface(g_dxs_dev, &IID_IDXGIDevice, (void **)&xd))) goto out;
    if (FAILED(IDXGIDevice_GetAdapter(xd, &ad))) goto out;
    if (FAILED(IDXGIAdapter_GetParent(ad, &IID_IDXGIFactory1, (void **)&f1))) goto out;

    // One frame of queueing: this is a live stream, not a movie.
    IDXGIDevice1 *xd1 = NULL;
    if (SUCCEEDED(IDXGIDevice_QueryInterface(xd, &IID_IDXGIDevice1, (void **)&xd1)) && xd1) {
        IDXGIDevice1_SetMaximumFrameLatency(xd1, 1);
        IDXGIDevice1_Release(xd1);
    }

    // USBRIDGE_D3D11_BLT=1: skip the flip model (drivers that mishandle it;
    // also makes a fullscreen window visible to GDI screen captures).
    if (!getenv("USBRIDGE_D3D11_BLT") &&
        SUCCEEDED(IDXGIFactory1_QueryInterface(f1, &IID_IDXGIFactory2, (void **)&f2)) && f2) {
        static const DXGI_SWAP_EFFECT effects[] = { DXGI_SWAP_EFFECT_FLIP_DISCARD, DXGI_SWAP_EFFECT_FLIP_SEQUENTIAL };
        for (int i = 0; i < 2 && !g_sc; i++) {
            DXGI_SWAP_CHAIN_DESC1 d = { 0 };
            d.Width = w; d.Height = h;
            d.Format = DXGI_FORMAT_B8G8R8A8_UNORM;
            d.SampleDesc.Count = 1;
            d.BufferUsage = DXGI_USAGE_RENDER_TARGET_OUTPUT;
            d.BufferCount = 2;
            d.Scaling = DXGI_SCALING_STRETCH;
            d.SwapEffect = effects[i];
            d.AlphaMode = DXGI_ALPHA_MODE_IGNORE;
            IDXGISwapChain1 *sc1 = NULL;
            if (SUCCEEDED(IDXGIFactory2_CreateSwapChainForHwnd(f2, (IUnknown *)g_dxs_dev, hwnd, &d, NULL, NULL, &sc1)) && sc1) {
                g_sc = (IDXGISwapChain *)sc1;
                g_flip = 1;
            }
        }
    }
    if (!g_sc) { // pre-flip-model DXGI (or a driver that refuses it)
        DXGI_SWAP_CHAIN_DESC d = { 0 };
        d.BufferDesc.Width = w; d.BufferDesc.Height = h;
        d.BufferDesc.Format = DXGI_FORMAT_B8G8R8A8_UNORM;
        d.SampleDesc.Count = 1;
        d.BufferUsage = DXGI_USAGE_RENDER_TARGET_OUTPUT;
        d.BufferCount = 1;
        d.OutputWindow = hwnd;
        d.Windowed = TRUE;
        d.SwapEffect = DXGI_SWAP_EFFECT_DISCARD;
        if (FAILED(IDXGIFactory1_CreateSwapChain(f1, (IUnknown *)g_dxs_dev, &d, &g_sc))) g_sc = NULL;
        g_flip = 0;
    }
    if (g_sc) {
        IDXGIFactory1_MakeWindowAssociation(f1, hwnd, DXGI_MWA_NO_ALT_ENTER | DXGI_MWA_NO_WINDOW_CHANGES);
        g_sc_w = w; g_sc_h = h;
        ok = dxp_make_rtv();
    }
out:
    if (f2) IDXGIFactory2_Release(f2);
    if (f1) IDXGIFactory1_Release(f1);
    if (ad) IDXGIAdapter_Release(ad);
    if (xd) IDXGIDevice_Release(xd);
    return ok;
}

static void dxp_ovl_free(DxpOvl *o) {
    REL(o->srv, ID3D11ShaderResourceView);
    REL(o->tex, ID3D11Texture2D);
    o->w = o->h = o->on = 0;
}

static void dxp_release_all(void) {
    dxp_release_targets();
    REL(g_sc, IDXGISwapChain);
    REL(g_vs, ID3D11VertexShader);
    REL(g_ps_rgb, ID3D11PixelShader);
    REL(g_ps_yuv, ID3D11PixelShader);
    REL(g_ps_ovl, ID3D11PixelShader);
    REL(g_samp, ID3D11SamplerState);
    REL(g_blend, ID3D11BlendState);
    REL(g_rs, ID3D11RasterizerState);
    REL(g_cb, ID3D11Buffer);
    REL(g_rgb_srv, ID3D11ShaderResourceView);
    REL(g_rgb_tex, ID3D11Texture2D);
    REL(g_yuv_srv_y, ID3D11ShaderResourceView);
    REL(g_yuv_srv_c, ID3D11ShaderResourceView);
    REL(g_yuv_tex, ID3D11Texture2D);
    dxp_ovl_free(&g_hud);
    dxp_ovl_free(&g_aiv);
    g_rgb_w = g_rgb_h = g_yuv_w = g_yuv_h = 0;
    g_yuv_fmt = DXGI_FORMAT_UNKNOWN;
    g_src_kind = g_src_w = g_src_h = g_src_hdr = 0;
    g_sc_w = g_sc_h = 0;
    g_hwnd = NULL;
}

// dxp_init creates the swapchain and pipeline for hwnd (the overlay window).
// Render thread only from here on, except dxp_frame_free / win_dx_hwdev_ref.
int dxp_init(HWND hwnd, int w, int h) {
    if (!dxs_ensure() || !dxp_load_compiler()) return 0;
    if (w < 1) w = 1;
    if (h < 1) h = 1;
    dxs_lock();
    int ok = dxp_make_pipeline() && dxp_make_swapchain(hwnd, w, h);
    if (ok) g_hwnd = hwnd;
    else dxp_release_all();
    dxs_unlock();
    if (ok)
        dxp_log(0, "d3d11: renderer ready -- %s swapchain %dx%d", g_flip ? "flip-model" : "blt-model", w, h);
    else
        dxp_log(2, "d3d11: renderer setup failed");
    return ok;
}

// dxp_destroy: leak=1 when the GPU stopped responding -- releasing objects
// that pending work still references could block or crash.
void dxp_destroy(int leak) {
    if (leak) {
        g_sc = NULL; g_rtv = NULL; g_hwnd = NULL;
        return;
    }
    dxs_lock();
    if (g_dxs_ctx) {
        ID3D11DeviceContext_ClearState(g_dxs_ctx);
        ID3D11DeviceContext_Flush(g_dxs_ctx);
    }
    dxp_release_all();
    dxs_unlock();
}

const char *dxp_adapter_name(void) { return g_dxs_name; }
int dxp_is_warp(void) { return g_dxs_warp; }

// ─── frame upload ────────────────────────────────────────────────────────────

void dxp_frame_free(void *frame) {
    AVFrame *f = (AVFrame *)frame;
    av_frame_free(&f);
}

static int dxp_ensure_tex(ID3D11Texture2D **tex, ID3D11ShaderResourceView **srv, int *cw, int *ch,
                          int w, int h, DXGI_FORMAT fmt) {
    if (*tex && *cw == w && *ch == h) return 1;
    REL(*srv, ID3D11ShaderResourceView);
    REL(*tex, ID3D11Texture2D);
    D3D11_TEXTURE2D_DESC td = { 0 };
    td.Width = w; td.Height = h; td.MipLevels = 1; td.ArraySize = 1;
    td.Format = fmt;
    td.SampleDesc.Count = 1;
    td.Usage = D3D11_USAGE_DYNAMIC;
    td.BindFlags = D3D11_BIND_SHADER_RESOURCE;
    td.CPUAccessFlags = D3D11_CPU_ACCESS_WRITE;
    if (FAILED(ID3D11Device_CreateTexture2D(g_dxs_dev, &td, NULL, tex))) { *tex = NULL; return 0; }
    if (FAILED(ID3D11Device_CreateShaderResourceView(g_dxs_dev, (ID3D11Resource *)*tex, NULL, srv))) {
        REL(*tex, ID3D11Texture2D);
        return 0;
    }
    *cw = w; *ch = h;
    return 1;
}

static int dxp_upload(ID3D11Texture2D *tex, const uint8_t *px, int w, int h, int stride) {
    D3D11_MAPPED_SUBRESOURCE m;
    if (FAILED(ID3D11DeviceContext_Map(g_dxs_ctx, (ID3D11Resource *)tex, 0, D3D11_MAP_WRITE_DISCARD, 0, &m)))
        return 0;
    size_t row = (size_t)w * 4;
    if (m.RowPitch == (UINT)stride && (size_t)stride == row) memcpy(m.pData, px, row * (size_t)h);
    else
        for (int y = 0; y < h; y++)
            memcpy((uint8_t *)m.pData + (size_t)y * m.RowPitch, px + (size_t)y * (size_t)stride, row);
    ID3D11DeviceContext_Unmap(g_dxs_ctx, (ID3D11Resource *)tex, 0);
    return 1;
}

int dxp_upload_rgba(const uint8_t *px, int w, int h, int stride) {
    if (!g_sc || w <= 0 || h <= 0) return 0;
    dxs_lock();
    int ok = dxp_ensure_tex(&g_rgb_tex, &g_rgb_srv, &g_rgb_w, &g_rgb_h, w, h, DXGI_FORMAT_R8G8B8A8_UNORM) &&
             dxp_upload(g_rgb_tex, px, w, h, stride);
    dxs_unlock();
    if (ok) { g_src_kind = 1; g_src_w = w; g_src_h = h; g_src_hdr = 0; }
    return ok;
}

// YCbCr -> RGB for normalized texture values (UNORM of the stored code).
static void dxp_set_matrix(const AVFrame *f, int bits) {
    double kr = 0.299, kb = 0.114; // BT.601: moonlight-common-c's default for 8-bit
    enum AVColorSpace cs = f->colorspace;
    if (cs == AVCOL_SPC_BT709) { kr = 0.2126; kb = 0.0722; }
    else if (cs == AVCOL_SPC_BT2020_NCL || cs == AVCOL_SPC_BT2020_CL || (cs == AVCOL_SPC_UNSPECIFIED && bits > 8)) {
        kr = 0.2627; kb = 0.0593;
    }
    double kg = 1.0 - kr - kb;
    int full = f->color_range == AVCOL_RANGE_JPEG;
    double sh = (double)(1 << (bits - 8));
    double scale = bits > 8 ? 65535.0 / 64.0 : 255.0; // texture value -> code (P010 keeps 10 bits in the top)
    double yoff = full ? 0 : 16 * sh, yr = full ? (255 * sh) : 219 * sh;
    double coff = 128 * sh, cr = full ? (255 * sh) : 224 * sh;
    // Y = y*a + b, C = c*e + g  with y,c the texture values
    double ya = scale / yr, yb = -yoff / yr;
    double ca = scale / cr, cb = -coff / cr;
    double rv = 2 * (1 - kr), bu = 2 * (1 - kb);
    double gu = -2 * kb * (1 - kb) / kg, gv = -2 * kr * (1 - kr) / kg;
    // rgb = Y + coefU*U + coefV*V
    double co[3][2] = { { 0, rv }, { gu, gv }, { bu, 0 } };
    for (int i = 0; i < 3; i++) {
        g_src_m[i][0] = (float)ya;
        g_src_m[i][1] = (float)(co[i][0] * ca);
        g_src_m[i][2] = (float)(co[i][1] * ca);
        g_src_m[i][3] = (float)(yb + (co[i][0] + co[i][1]) * cb);
    }
}

// dxp_accepts_d3d11: frame can go through dxp_upload_d3d11 (decoded on the
// shared device, in a format this GPU can sample). Any thread.
int dxp_accepts_d3d11(const void *frame) {
    const AVFrame *f = (const AVFrame *)frame;
    if (!g_dxs_dev || !f || f->format != AV_PIX_FMT_D3D11 || !f->hw_frames_ctx) return 0;
    AVHWFramesContext *fc = (AVHWFramesContext *)f->hw_frames_ctx->data;
    AVD3D11VADeviceContext *dh = (AVD3D11VADeviceContext *)fc->device_ctx->hwctx;
    if (dh->device != g_dxs_dev) return 0;
    if (fc->sw_format == AV_PIX_FMT_NV12) return g_dxs_nv12;
    if (fc->sw_format == AV_PIX_FMT_P010) return g_dxs_p010;
    return 0;
}

int dxp_upload_d3d11(void *frame) {
    AVFrame *f = (AVFrame *)frame;
    if (!g_sc || !dxp_accepts_d3d11(f)) return 0;
    AVHWFramesContext *fc = (AVHWFramesContext *)f->hw_frames_ctx->data;
    int p010 = fc->sw_format == AV_PIX_FMT_P010;
    DXGI_FORMAT fmt = p010 ? DXGI_FORMAT_P010 : DXGI_FORMAT_NV12;
    // The decoder pool's textures are 16/128-aligned; copy only the picture.
    int w = f->width & ~1, h = f->height & ~1;
    if (w <= 0 || h <= 0) return 0;

    dxs_lock();
    int ok = 1;
    if (!g_yuv_tex || g_yuv_w != w || g_yuv_h != h || g_yuv_fmt != fmt) {
        REL(g_yuv_srv_y, ID3D11ShaderResourceView);
        REL(g_yuv_srv_c, ID3D11ShaderResourceView);
        REL(g_yuv_tex, ID3D11Texture2D);
        D3D11_TEXTURE2D_DESC td = { 0 };
        td.Width = w; td.Height = h; td.MipLevels = 1; td.ArraySize = 1;
        td.Format = fmt;
        td.SampleDesc.Count = 1;
        td.Usage = D3D11_USAGE_DEFAULT;
        td.BindFlags = D3D11_BIND_SHADER_RESOURCE;
        ok = SUCCEEDED(ID3D11Device_CreateTexture2D(g_dxs_dev, &td, NULL, &g_yuv_tex));
        if (ok) {
            D3D11_SHADER_RESOURCE_VIEW_DESC sv = { 0 };
            sv.ViewDimension = D3D11_SRV_DIMENSION_TEXTURE2D;
            sv.Texture2D.MipLevels = 1;
            sv.Format = p010 ? DXGI_FORMAT_R16_UNORM : DXGI_FORMAT_R8_UNORM;
            ok = SUCCEEDED(ID3D11Device_CreateShaderResourceView(g_dxs_dev, (ID3D11Resource *)g_yuv_tex, &sv, &g_yuv_srv_y));
            sv.Format = p010 ? DXGI_FORMAT_R16G16_UNORM : DXGI_FORMAT_R8G8_UNORM;
            ok = ok && SUCCEEDED(ID3D11Device_CreateShaderResourceView(g_dxs_dev, (ID3D11Resource *)g_yuv_tex, &sv, &g_yuv_srv_c));
        }
        if (ok) { g_yuv_w = w; g_yuv_h = h; g_yuv_fmt = fmt; }
        else {
            REL(g_yuv_srv_y, ID3D11ShaderResourceView);
            REL(g_yuv_srv_c, ID3D11ShaderResourceView);
            REL(g_yuv_tex, ID3D11Texture2D);
            g_yuv_w = g_yuv_h = 0;
            if (!g_yuv_warned) { g_yuv_warned = 1; dxp_log(1, "d3d11: cannot create a sampleable %s texture", p010 ? "P010" : "NV12"); }
        }
    }
    if (ok) {
        // Same immediate context as the decoder: the copy is ordered after
        // the decode that wrote the slice and before any later reuse of it.
        D3D11_BOX box = { 0, 0, 0, (UINT)w, (UINT)h, 1 };
        ID3D11DeviceContext_CopySubresourceRegion(g_dxs_ctx, (ID3D11Resource *)g_yuv_tex, 0, 0, 0, 0,
                                                  (ID3D11Resource *)f->data[0], (UINT)(intptr_t)f->data[1], &box);
    }
    dxs_unlock();
    if (!ok) return 0;
    dxp_set_matrix(f, p010 ? 10 : 8);
    g_src_kind = 2; g_src_w = w; g_src_h = h;
    g_src_hdr = p010 && f->color_trc == AVCOL_TRC_SMPTE2084;
    if (p010 && f->color_trc == AVCOL_TRC_UNSPECIFIED) g_src_hdr = 1; // 10-bit Moonlight streams are HDR10
    return 1;
}

static int dxp_ovl_set(DxpOvl *o, const uint8_t *px, int w, int h) {
    if (!px || w <= 0 || h <= 0) { o->on = 0; return 1; }
    dxs_lock();
    int ok = dxp_ensure_tex(&o->tex, &o->srv, &o->w, &o->h, w, h, DXGI_FORMAT_R8G8B8A8_UNORM) &&
             dxp_upload(o->tex, px, w, h, w * 4);
    dxs_unlock();
    o->on = ok;
    return ok;
}
int dxp_set_hud(const uint8_t *px, int w, int h)      { return dxp_ovl_set(&g_hud, px, w, h); }
int dxp_set_aivision(const uint8_t *px, int w, int h) { return dxp_ovl_set(&g_aiv, px, w, h); }
int dxp_source_kind(void) { return g_src_kind; }
void dxp_source_size(int *w, int *h) { *w = g_src_w; *h = g_src_h; }

// ─── draw + present ──────────────────────────────────────────────────────────

static void dxp_draw_quad(ID3D11PixelShader *ps, ID3D11ShaderResourceView *a, ID3D11ShaderResourceView *b,
                          const float rect[4], int mode) {
    D3D11_MAPPED_SUBRESOURCE m;
    if (FAILED(ID3D11DeviceContext_Map(g_dxs_ctx, (ID3D11Resource *)g_cb, 0, D3D11_MAP_WRITE_DISCARD, 0, &m))) return;
    DxpConsts *c = (DxpConsts *)m.pData;
    memcpy(c->rect, rect, sizeof(c->rect));
    memcpy(c->m0, g_src_m[0], sizeof(c->m0));
    memcpy(c->m1, g_src_m[1], sizeof(c->m1));
    memcpy(c->m2, g_src_m[2], sizeof(c->m2));
    c->opt[0] = (float)mode; c->opt[1] = c->opt[2] = c->opt[3] = 0;
    ID3D11DeviceContext_Unmap(g_dxs_ctx, (ID3D11Resource *)g_cb, 0);
    ID3D11ShaderResourceView *srvs[2] = { a, b };
    ID3D11DeviceContext_PSSetShader(g_dxs_ctx, ps, NULL, 0);
    ID3D11DeviceContext_PSSetShaderResources(g_dxs_ctx, 0, 2, srvs);
    ID3D11DeviceContext_Draw(g_dxs_ctx, 4, 0);
}

// rect in pixels of a w x h viewport -> NDC (y up).
static void dxp_ndc(float *r, float x0, float y0, float x1, float y1, float w, float h) {
    r[0] = x0 / w * 2 - 1; r[1] = 1 - y0 / h * 2;
    r[2] = x1 / w * 2 - 1; r[3] = 1 - y1 / h * 2;
}

int dxp_window_changed(void) {
    RECT rc;
    if (!g_hwnd || !GetClientRect(g_hwnd, &rc)) return 0;
    return rc.right > 0 && rc.bottom > 0 && (rc.right != g_sc_w || rc.bottom != g_sc_h);
}

// dxp_render draws the current source letterboxed to aspect (frame w/h when
// 0) plus the overlays, and presents. dest gets the picture rect and
// swapchain size (dx, dy, dw, dh, sw, sh). 1 presented, 0 nothing to show,
// -1 the device is gone.
int dxp_render(int vsync, float aspect, int hud_pct, int dest[6]) {
    if (!g_sc || !g_src_kind) return 0;
    RECT rc;
    if (GetClientRect(g_hwnd, &rc) && rc.right > 0 && rc.bottom > 0 &&
        (rc.right != g_sc_w || rc.bottom != g_sc_h)) {
        dxs_lock();
        dxp_release_targets();
        ID3D11DeviceContext_OMSetRenderTargets(g_dxs_ctx, 0, NULL, NULL);
        HRESULT hr = IDXGISwapChain_ResizeBuffers(g_sc, 0, rc.right, rc.bottom, DXGI_FORMAT_UNKNOWN, 0);
        if (SUCCEEDED(hr)) { g_sc_w = rc.right; g_sc_h = rc.bottom; }
        int ok = dxp_make_rtv();
        dxs_unlock();
        if (hr == DXGI_ERROR_DEVICE_REMOVED || hr == DXGI_ERROR_DEVICE_RESET) return -1;
        if (!ok) return 0;
    }
    if (!g_rtv) return 0;

    int sw = g_sc_w, sh = g_sc_h;
    float fa = aspect > 0 ? aspect : (float)g_src_w / (float)(g_src_h ? g_src_h : 1);
    float wa = (float)sw / (float)(sh ? sh : 1);
    int dx = 0, dy = 0, dw = sw, dh = sh;
    if (fa > wa) { dh = (int)(sw / fa + 0.5f); dy = (sh - dh) / 2; }
    else         { dw = (int)(sh * fa + 0.5f); dx = (sw - dw) / 2; }
    dest[0] = dx; dest[1] = dy; dest[2] = dw; dest[3] = dh; dest[4] = sw; dest[5] = sh;

    dxs_lock();
    ID3D11DeviceContext *c = g_dxs_ctx;
    static const float black[4] = { 0, 0, 0, 1 };
    ID3D11DeviceContext_OMSetRenderTargets(c, 1, &g_rtv, NULL);
    ID3D11DeviceContext_ClearRenderTargetView(c, g_rtv, black);
    D3D11_VIEWPORT vp = { 0, 0, (float)sw, (float)sh, 0, 1 };
    ID3D11DeviceContext_RSSetViewports(c, 1, &vp);
    ID3D11DeviceContext_RSSetState(c, g_rs);
    ID3D11DeviceContext_IASetInputLayout(c, NULL);
    ID3D11DeviceContext_IASetPrimitiveTopology(c, D3D11_PRIMITIVE_TOPOLOGY_TRIANGLESTRIP);
    ID3D11DeviceContext_VSSetShader(c, g_vs, NULL, 0);
    ID3D11DeviceContext_VSSetConstantBuffers(c, 0, 1, &g_cb);
    ID3D11DeviceContext_PSSetConstantBuffers(c, 0, 1, &g_cb);
    ID3D11DeviceContext_PSSetSamplers(c, 0, 1, &g_samp);
    ID3D11DeviceContext_OMSetBlendState(c, NULL, NULL, 0xffffffff);

    float r[4];
    dxp_ndc(r, (float)dx, (float)dy, (float)(dx + dw), (float)(dy + dh), (float)sw, (float)sh);
    if (g_src_kind == 2) dxp_draw_quad(g_ps_yuv, g_yuv_srv_y, g_yuv_srv_c, r, g_src_hdr ? 1 : 0);
    else                 dxp_draw_quad(g_ps_rgb, g_rgb_srv, NULL, r, 0);

    // HUD / AI Vision only over GPU frames: the CPU path burns them in.
    if (g_src_kind == 2 && (g_hud.on || g_aiv.on)) {
        ID3D11DeviceContext_OMSetBlendState(c, g_blend, NULL, 0xffffffff);
        if (g_aiv.on && g_aiv.w == g_src_w && g_aiv.h == g_src_h)
            dxp_draw_quad(g_ps_ovl, g_aiv.srv, NULL, r, 0);
        if (g_hud.on) {
            // Same anchoring as vk_hud_record_draw: bottom-right of the
            // frame, in frame pixels, then mapped into the picture rect.
            int pct = hud_pct < 25 ? 25 : hud_pct > 200 ? 200 : hud_pct;
            float fw = (float)g_src_w, fh = (float)g_src_h;
            float hw = (float)g_hud.w * pct / 100.0f, hh = (float)g_hud.h * pct / 100.0f;
            float hx0 = fw - 24 - hw, hy0 = fh - 24 - hh;
            if (hx0 < 24) hx0 = 24;
            if (hy0 < 24) hy0 = 24;
            float sx = (float)dw / fw, sy = (float)dh / fh;
            dxp_ndc(r, dx + hx0 * sx, dy + hy0 * sy, dx + (hx0 + hw) * sx, dy + (hy0 + hh) * sy, (float)sw, (float)sh);
            dxp_draw_quad(g_ps_ovl, g_hud.srv, NULL, r, 0);
        }
    }
    ID3D11ShaderResourceView *none[2] = { NULL, NULL };
    ID3D11DeviceContext_PSSetShaderResources(c, 0, 2, none);
    dxs_unlock();

    // Not under the lock: Present can wait for a vblank, and decode must not.
    HRESULT hr = IDXGISwapChain_Present(g_sc, vsync ? 1 : 0, 0);
    if (hr == DXGI_ERROR_DEVICE_REMOVED || hr == DXGI_ERROR_DEVICE_RESET) {
        HRESULT why = ID3D11Device_GetDeviceRemovedReason(g_dxs_dev);
        dxp_log(2, "d3d11: device removed (0x%08lx, reason 0x%08lx)", (unsigned long)hr, (unsigned long)why);
        return -1;
    }
    return 1;
}

#endif // _WIN32
