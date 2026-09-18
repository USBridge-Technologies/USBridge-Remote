// vk_video_impl_windows.c — Vulkan overlay video renderer for Windows.
//
// Architecture:
//   • WS_POPUP + WS_EX_TOPMOST overlay window created on its own Win32 thread (vk_hwnd_thread).
//     The dedicated thread runs GetMessage/DispatchMessage, giving the overlay a separate
//     Windows input queue from Fyne's GLFW thread. This prevents DWM from serialising
//     vkQueuePresentKHR with Fyne's wglSwapBuffers — on AMD iGPUs where OpenGL and Vulkan
//     share a single hardware present queue, being on the same input queue causes
//     wglSwapBuffers to block permanently once Vulkan starts presenting.
//   • VkSwapchainKHR with FIFO_RELAXED (or FIFO) present mode.
//   • Render thread: staging buffer → VkImage (sampled texture) → blit to swapchain.
//   • Frame queue: capacity 1, drop-on-full (always-latest semantics).
//   • Fallback: on VK init failure the Go side falls back to the GDI renderer.
//
// Thread safety:
//   • vk_video_create / vk_video_destroy — called from CGO (Go thread), safe.
//   • vk_video_try_submit — called from C decoder thread OR CGO; protected by g_cs.
//   • vk_video_update_frame — converts client→screen coords via ClientToScreen then
//     calls SetWindowPos(SWP_ASYNCWINDOWPOS) which is thread-safe for owned popups.

#ifdef _WIN32

#define WIN32_LEAN_AND_MEAN
#define VK_USE_PLATFORM_WIN32_KHR
#include <windows.h>
#include <vulkan/vulkan.h>
#include <vulkan/vulkan_win32.h>
#include <stdint.h>
#include <string.h>
#include <stdlib.h>
#include <stdio.h>
#include <stdatomic.h>

extern void goVKLog(char *msg, int level);

// win_vk_hwdev_get (moonlight_cgo_windows.go) — lazily creates (or returns
// already-created) the shared ffmpeg-owned Vulkan hwaccel device used for
// decode, and hands back its raw VkInstance/VkPhysicalDevice/VkDevice plus a
// presentable graphics queue family index. When this succeeds, decode and
// presentation share one VkDevice/VkImage with zero cross-device copy; see
// the long comment at vk_video_init_common's call site for why this is the
// ONLY device-sharing direction that's actually been proven to work.
extern int win_vk_hwdev_get(void **out_inst, void **out_phys, void **out_dev, uint32_t *out_gfx_qf);
// vk_frame_release_avframe (moonlight_cgo_windows.go) — drops the AVFrame ref
// that was keeping a zero-copy decoded VkImage's memory alive, once our own
// GPU read of it has retired. Passed back via the pending-frame slot below.
extern void vk_frame_release_avframe(void *ctx);

// Video rect atomics — declared early so vk_wnd_proc can read them.
static atomic_int g_dst_x, g_dst_y, g_dst_w, g_dst_h;

// ─── mouse event queue (ring buffer, capacity 512) ───────────────────────────
// The overlay window captures all pointer events and queues them here.
// Go polls via vk_video_next_event() and dispatches to TouchpadWrapper on the
// Fyne main goroutine — mirroring the Linux XSelectInput + vk_video_next_event
// mechanism. Thread-safe: wnd_proc (hwnd thread) writes; CGO goroutine reads.
//
// Movement is delivered via Raw Input (WM_INPUT + RIDEV_INPUTSINK) which fires
// on every hardware sample with no coalescing — matching macOS NSTrackingArea
// behaviour. WM_MOUSEMOVE is kept as a fallback if Raw Input registration fails.

#define VK_EQ_CAP 512
typedef struct { int type; int x, y, btn; } VkMouseEvt;
// type: 1=move  2=button-press  3=button-release
// btn for press/release: 1=left 2=middle 3=right 4=wheel-up 5=wheel-down

static VkMouseEvt    g_eq[VK_EQ_CAP];
static volatile int  g_eq_head = 0, g_eq_tail = 0; // [tail, head)
static CRITICAL_SECTION g_eq_cs;
static int           g_eq_cs_init = 0;
static volatile int  g_raw_mouse  = 0; // 1 = Raw Input registered; WM_MOUSEMOVE becomes no-op
static volatile int  g_btn_down_mask = 0; // bit0=left bit1=middle bit2=right — held-button tracking for raw-input bounds gating

static void vk_eq_push(int type, int x, int y, int btn) {
    if (!g_eq_cs_init) return;
    EnterCriticalSection(&g_eq_cs);
    int next = (g_eq_head + 1) % VK_EQ_CAP;
    if (next != g_eq_tail) { // drop when full
        g_eq[g_eq_head].type = type;
        g_eq[g_eq_head].x    = x;
        g_eq[g_eq_head].y    = y;
        g_eq[g_eq_head].btn  = btn;
        g_eq_head = next;
    }
    LeaveCriticalSection(&g_eq_cs);
}

// ─── keyboard event queue (standalone fullscreen mode only) ──────────────────
// In standalone mode the VK window is the only window and receives keyboard focus.
// WM_KEYDOWN/WM_KEYUP are pushed here; Go polls via vk_video_next_key_event().
// type: 1=keydown, 2=keyup. vk_code = Win32 Virtual Key code (wParam).

#define VK_KQ_CAP 256
typedef struct { int type; int vk_code; } VkKeyEvt;

static VkKeyEvt     g_kq[VK_KQ_CAP];
static volatile int g_kq_head = 0, g_kq_tail = 0;
static CRITICAL_SECTION g_kq_cs;
static int          g_kq_cs_init = 0;

static void vk_kq_push(int type, int vk_code) {
    if (!g_kq_cs_init) return;
    EnterCriticalSection(&g_kq_cs);
    int next = (g_kq_head + 1) % VK_KQ_CAP;
    if (next != g_kq_tail) {
        g_kq[g_kq_head].type    = type;
        g_kq[g_kq_head].vk_code = vk_code;
        g_kq_head = next;
    }
    LeaveCriticalSection(&g_kq_cs);
}

// 1 = standalone fullscreen (no parent window, covers full screen, has keyboard focus)
static int g_standalone = 0;

// ─── overlay window class ─────────────────────────────────────────────────────

static ATOM   g_wndcls      = 0;
static HWND   g_child_hwnd  = NULL;  // Vulkan overlay WS_POPUP window
static HWND   g_parent_hwnd = NULL;  // Fyne/GLFW HWND — used for ClientToScreen only

// Window thread: the overlay HWND lives on its own Win32 thread so it has a
// separate Windows input queue from Fyne's GLFW thread. With the same input
// queue DWM serialises vkQueuePresentKHR with Fyne's wglSwapBuffers, which on
// AMD integrated GPUs (shared hardware present queue) deadlocks permanently.
// A dedicated window thread + message pump gives the overlay its own DWM present
// context, completely independent from OpenGL.
static HANDLE g_hwnd_thread = NULL;
static HANDLE g_hwnd_ready  = NULL; // signaled once g_child_hwnd is assigned

typedef struct { HWND parent; int x, y, w, h; int standalone; } VkWndArgs;
static VkWndArgs g_hwnd_args;

static LRESULT CALLBACK vk_wnd_proc(HWND hw, UINT msg, WPARAM wp, LPARAM lp) {
    if (msg == WM_ERASEBKGND) return 1;
    // In overlay mode: don't steal keyboard focus on click.
    // In standalone mode: allow activation so keyboard works.
    if (msg == WM_MOUSEACTIVATE) return g_standalone ? MA_ACTIVATE : MA_NOACTIVATE;

    // Standalone mode keyboard: push raw Win32 VK codes into the key queue.
    // Go polls via vk_video_next_key_event() and forwards to Moonlight directly.
    if (g_standalone) {
        if (msg == WM_KEYDOWN || msg == WM_SYSKEYDOWN) {
            int vk = (int)wp;
            // Map generic modifier VKs to left/right variants using scan code.
            if (vk == VK_SHIFT || vk == VK_CONTROL || vk == VK_MENU) {
                UINT scan = (HIWORD(lp)) & 0xFF;
                UINT mapped = MapVirtualKeyW(scan, MAPVK_VSC_TO_VK_EX);
                if (mapped != 0) vk = (int)mapped;
            }
            vk_kq_push(1, vk);
            return 0;
        }
        if (msg == WM_KEYUP || msg == WM_SYSKEYUP) {
            int vk = (int)wp;
            if (vk == VK_SHIFT || vk == VK_CONTROL || vk == VK_MENU) {
                UINT scan = (HIWORD(lp)) & 0xFF;
                UINT mapped = MapVirtualKeyW(scan, MAPVK_VSC_TO_VK_EX);
                if (mapped != 0) vk = (int)mapped;
            }
            vk_kq_push(2, vk);
            return 0;
        }
        // Suppress WM_CHAR — key routing goes through WM_KEYDOWN/UP only.
        if (msg == WM_CHAR || msg == WM_SYSCHAR || msg == WM_DEADCHAR) {
            return 0;
        }
    }

    // Raw Input: fires on every hardware mouse sample (no WM_MOUSEMOVE coalescing).
    // RIDEV_INPUTSINK delivers even when not foreground; we filter by foreground window
    // so we don't intercept input from other applications.
    if (msg == WM_INPUT) {
        HWND _fg = GetForegroundWindow();
        // In standalone mode g_parent_hwnd is NULL; check only g_child_hwnd.
        if (_fg == g_child_hwnd || (g_parent_hwnd && _fg == g_parent_hwnd)) {
            UINT sz = 0;
            GetRawInputData((HRAWINPUT)lp, RID_INPUT, NULL, &sz, sizeof(RAWINPUTHEADER));
            if (sz > 0 && sz <= 256) {
                BYTE buf[256];
                if (GetRawInputData((HRAWINPUT)lp, RID_INPUT, buf, &sz, sizeof(RAWINPUTHEADER)) != (UINT)-1) {
                    RAWINPUT *ri = (RAWINPUT*)buf;
                    if (ri->header.dwType == RIM_TYPEMOUSE) {
                        RAWMOUSE *rm = &ri->data.mouse;
                        if (rm->lLastX != 0 || rm->lLastY != 0) {
                            POINT cur;
                            if (rm->usFlags & MOUSE_MOVE_ABSOLUTE) {
                                // Absolute device: touchpad, RDP, VM, tablet
                                BOOL vd = (rm->usFlags & MOUSE_VIRTUAL_DESKTOP) != 0;
                                cur.x = MulDiv((int)rm->lLastX,
                                    GetSystemMetrics(vd ? SM_CXVIRTUALSCREEN : SM_CXSCREEN), 65535);
                                cur.y = MulDiv((int)rm->lLastY,
                                    GetSystemMetrics(vd ? SM_CYVIRTUALSCREEN : SM_CYSCREEN), 65535);
                            } else {
                                // Relative device: hardware mouse — system tracks absolute pos
                                GetCursorPos(&cur);
                            }
                            if (hw) ScreenToClient(hw, &cur);
                            // RIDEV_INPUTSINK reports every hardware sample system-wide as
                            // long as our window is foreground, regardless of where the
                            // cursor physically is. Foreground alone is not "the pointer is
                            // over the remote video" -- e.g. the cursor can sit on another
                            // monitor, the taskbar, or just outside our window edge while
                            // this window keeps focus. Forwarding those samples anyway
                            // clamps PositionToAbsolute to the nearest edge and drives the
                            // remote cursor straight into a corner on every such sample, as
                            // if the client were stuck replaying its own local pointer
                            // instead of the real one. Only forward when the cursor is
                            // actually inside our client area, or a button we own is still
                            // held (so an in-progress drag that briefly overshoots the edge
                            // keeps tracking, matching MouseUp's own implicit-capture logic).
                            RECT rc;
                            BOOL inside = FALSE;
                            if (hw && GetClientRect(hw, &rc)) {
                                inside = PtInRect(&rc, cur);
                            }
                            if (inside || g_btn_down_mask != 0) {
                                vk_eq_push(1, cur.x, cur.y, 0);
                            }
                        }
                    }
                }
            }
        }
        return DefWindowProcW(hw, msg, wp, lp);
    }
    // WM_MOUSEMOVE fallback: only used when Raw Input registration failed.
    if (msg == WM_MOUSEMOVE) {
        if (!g_raw_mouse)
            vk_eq_push(1, (int)(short)LOWORD(lp), (int)(short)HIWORD(lp), 0);
        return 0;
    }
    if (msg == WM_LBUTTONDOWN) { g_btn_down_mask |= 1; vk_eq_push(2, (int)(short)LOWORD(lp), (int)(short)HIWORD(lp), 1); return 0; }
    if (msg == WM_LBUTTONUP)   { g_btn_down_mask &= ~1; vk_eq_push(3, (int)(short)LOWORD(lp), (int)(short)HIWORD(lp), 1); return 0; }
    if (msg == WM_RBUTTONDOWN) { g_btn_down_mask |= 4; vk_eq_push(2, (int)(short)LOWORD(lp), (int)(short)HIWORD(lp), 3); return 0; }
    if (msg == WM_RBUTTONUP)   { g_btn_down_mask &= ~4; vk_eq_push(3, (int)(short)LOWORD(lp), (int)(short)HIWORD(lp), 3); return 0; }
    if (msg == WM_MBUTTONDOWN) { g_btn_down_mask |= 2; vk_eq_push(2, (int)(short)LOWORD(lp), (int)(short)HIWORD(lp), 2); return 0; }
    if (msg == WM_MBUTTONUP)   { g_btn_down_mask &= ~2; vk_eq_push(3, (int)(short)LOWORD(lp), (int)(short)HIWORD(lp), 2); return 0; }
    if (msg == WM_MOUSEWHEEL) {
        // Encode scroll as button 4 (up) / 5 (down) — same convention as Linux X11.
        int btn = GET_WHEEL_DELTA_WPARAM(wp) > 0 ? 4 : 5;
        POINT pt = { (int)(short)LOWORD(lp), (int)(short)HIWORD(lp) };
        if (hw) ScreenToClient(hw, &pt);
        vk_eq_push(2, pt.x, pt.y, btn);
        return 0;
    }
    // WM_USER+1/+2: hide/show requests posted by the render thread.
    if (msg == WM_USER+1) { ShowWindow(hw, SW_HIDE); return 0; }
    if (msg == WM_USER+2) {
        // Re-assert HWND_TOPMOST synchronously on the window thread before making
        // the overlay visible. Between vk_video_bring_to_top (called from Go) and
        // this ShowWindow, the GLFW fullscreen window may have raised itself back to
        // the top of the TOPMOST Z-order (via its WM_ACTIVATE / WM_SETFOCUS handler).
        // By re-asserting here we guarantee VK appears on top in the single operation
        // that transitions the window from hidden to visible.
        SetWindowPos(hw, HWND_TOPMOST, 0, 0, 0, 0, SWP_NOMOVE | SWP_NOSIZE | SWP_NOACTIVATE);
        ShowWindow(hw, SW_SHOWNOACTIVATE);
        return 0;
    }
    if (msg == WM_CLOSE)   { DestroyWindow(hw); return 0; }
    if (msg == WM_DESTROY) { PostQuitMessage(0); return 0; }
    return DefWindowProcW(hw, msg, wp, lp);
}

static DWORD WINAPI vk_hwnd_thread(LPVOID unused) {
    (void)unused;
    int px, py, cw, ch;
    DWORD ex_style;

    if (g_hwnd_args.standalone) {
        // Standalone fullscreen: cover the monitor chosen by
        // vk_video_create_standalone (the one hosting the client HWND).
        // No TOPMOST (only window on screen), no NOACTIVATE (needs keyboard focus).
        px = g_hwnd_args.x;
        py = g_hwnd_args.y;
        cw = g_hwnd_args.w > 0 ? g_hwnd_args.w : GetSystemMetrics(SM_CXSCREEN);
        ch = g_hwnd_args.h > 0 ? g_hwnd_args.h : GetSystemMetrics(SM_CYSCREEN);
        ex_style = 0;
    } else {
        POINT pt = { g_hwnd_args.x, g_hwnd_args.y };
        if (g_hwnd_args.parent) ClientToScreen(g_hwnd_args.parent, &pt);
        px = pt.x; py = pt.y;
        cw = g_hwnd_args.w > 0 ? g_hwnd_args.w : 1;
        ch = g_hwnd_args.h > 0 ? g_hwnd_args.h : 1;
        // Overlay mode: TOPMOST keeps us above Fyne; NOACTIVATE prevents focus theft.
        ex_style = WS_EX_NOACTIVATE | WS_EX_TOPMOST;
    }

    // Create window on THIS thread — own message queue, independent DWM context.
    // No owner (NULL hwndParent) decouples from Fyne's present queue entirely.
    g_child_hwnd = CreateWindowExW(
        ex_style,
        L"usbridgeVKVideo", L"",
        WS_POPUP | WS_VISIBLE,
        px, py, cw, ch,
        NULL,                          // no owner — independent DWM context
        NULL, GetModuleHandleW(NULL), NULL);

    SetEvent(g_hwnd_ready);            // wake vk_video_create (with or without HWND)
    if (!g_child_hwnd) return 1;

    // In standalone mode grab keyboard focus so WM_KEYDOWN/UP reach this window.
    if (g_hwnd_args.standalone) {
        SetForegroundWindow(g_child_hwnd);
        SetFocus(g_child_hwnd);
    }

    // Register for Raw Mouse Input on this window.
    // RIDEV_INPUTSINK: deliver WM_INPUT even when the window is not foreground.
    // This gives us uncoalesced hardware mouse samples instead of the
    // coalesced WM_MOUSEMOVE messages — same effect as NSTrackingArea on macOS.
    {
        RAWINPUTDEVICE rid = {0};
        rid.usUsagePage = 0x01; // HID_USAGE_PAGE_GENERIC
        rid.usUsage     = 0x02; // HID_USAGE_GENERIC_MOUSE
        rid.dwFlags     = RIDEV_INPUTSINK;
        rid.hwndTarget  = g_child_hwnd;
        g_raw_mouse = RegisterRawInputDevices(&rid, 1, sizeof(rid)) ? 1 : 0;
    }

    MSG msg;
    while (GetMessageW(&msg, NULL, 0, 0) > 0) {
        TranslateMessage(&msg);
        DispatchMessageW(&msg);
    }
    return 0;
}

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
static VkExtent2D               g_swap_ext     = {0,0};

// 1 when g_inst/g_pdev/g_dev were adopted from win_vk_hwdev_get() (ffmpeg
// owns their lifetime via its AVBufferRef) rather than created by
// vk_create_instance/vk_create_device — vk_full_cleanup must NOT destroy
// them in that case.
static int                      g_ext_device   = 0;

// ─── zero-copy Vulkan-decode frame path ──────────────────────────────────────
// Parallel to the RGBA staging/blit path above: when decode is running on the
// shared Vulkan hwaccel device (moonlight_cgo_windows.go's win_deliver_frame_
// vulkan), frames arrive as an already-decoded VkImage instead of CPU pixels.
// g_frame_mode picks which payload the render thread's single-slot queue
// currently holds; a session uses exactly one mode throughout (decided once
// by which hwaccel tier win_av_init committed to).
#define VK_FRAME_MODE_RGBA    0
#define VK_FRAME_MODE_VKIMAGE 1
static int g_frame_mode = VK_FRAME_MODE_RGBA;

typedef struct {
    VkSamplerYcbcrConversion conv;
    VkSampler                sampler;
    VkDescriptorSetLayout    dsl;
    VkPipelineLayout         playout;
    VkPipeline               pipeline;
    VkDescriptorPool         dpool;
    VkFormat                 fmt; // VK_FORMAT_UNDEFINED = unused slot
} VkYcbcrPipeline;
#define VK_YCBCR_PIPELINE_CACHE_SIZE 4
static VkYcbcrPipeline g_ycbcr_pipelines[VK_YCBCR_PIPELINE_CACHE_SIZE];

// Small LRU-ish cache of VkImageView (+ its descriptor set) keyed by the
// source VkImage handle — ffmpeg's internal Vulkan frame pool round-robins a
// small, fixed set of VkImages, so this stays tiny in practice.
typedef struct {
    VkImage       img;
    VkImageView   view;
    VkDescriptorSet dset;
    VkFormat      fmt; // which g_ycbcr_pipelines[] entry dset was built against
} VkImgViewCacheEntry;
#define VK_IMGVIEW_CACHE_SIZE 8
static VkImgViewCacheEntry g_imgview_cache[VK_IMGVIEW_CACHE_SIZE];

// Pending zero-copy frame — single-slot, parallel to g_buf/g_ready above.
static VkImage    g_vkf_img          = VK_NULL_HANDLE;
static VkFormat   g_vkf_fmt          = VK_FORMAT_UNDEFINED;
static VkImageLayout g_vkf_layout    = VK_IMAGE_LAYOUT_UNDEFINED;
static int        g_vkf_w = 0, g_vkf_h = 0;
static void      *g_vkf_release_ctx  = NULL;
static void      (*g_vkf_release_fn)(void*) = NULL;
static volatile int g_vkf_ready      = 0;

// Previous frame's release context — freed once the NEXT frame's render
// fence-wait confirms our GPU read of it has fully retired (mirrors the
// existing g_fence wait-then-reset pattern in vk_render_frame).
static void      *g_vkf_prev_release_ctx = NULL;
static void      (*g_vkf_prev_release_fn)(void*) = NULL;

static VkQueue    g_decode_queue = VK_NULL_HANDLE;
static uint32_t   g_decode_qfam  = UINT32_MAX;

// Staging buffer (host-visible, coherent) — one per in-flight frame is fine for
// the 1-frame queue we use; no need for double-buffering.
static VkBuffer                 g_stage_buf    = VK_NULL_HANDLE;
static VkDeviceMemory           g_stage_mem    = VK_NULL_HANDLE;
static void                    *g_stage_ptr    = NULL; // persistently mapped
static VkDeviceSize             g_stage_sz     = 0;

// Device-local sampled image (upload target, then blit source).
static VkImage                  g_tex          = VK_NULL_HANDLE;
static VkDeviceMemory           g_tex_mem      = VK_NULL_HANDLE;
static int                      g_tex_w        = 0, g_tex_h = 0;

// Synchronisation
static VkCommandPool            g_cmdpool      = VK_NULL_HANDLE;
static VkCommandBuffer          g_cmdbuf       = VK_NULL_HANDLE;
static VkFence                  g_fence        = VK_NULL_HANDLE;
static VkSemaphore              g_img_sem      = VK_NULL_HANDLE;
static VkSemaphore              g_rnd_sem      = VK_NULL_HANDLE;

// ─── render-thread state ──────────────────────────────────────────────────────

static volatile atomic_int g_active;
// Set to 1 by vk_video_set_hidden to hide overlay (e.g. while a Fyne menu is open).
static volatile atomic_int g_hidden;
// Set from vk_video_create/vk_video_create_standalone's vsync argument, read by
// vk_create_swapchain (including on vk_recreate_swapchain resize/out-of-date
// recreation, hence atomic rather than a plain int passed as a parameter).
static volatile atomic_int g_vsync = 1;

// Pending frame — single-slot queue, protected by g_cs.
static uint8_t          *g_buf      = NULL;
static size_t            g_buf_sz   = 0;
static int               g_fw = 0, g_fh = 0, g_fs = 0;
static volatile int      g_ready    = 0;
static volatile int      g_has_frame = 0;

static CRITICAL_SECTION g_cs;
static int               g_cs_init  = 0;
static HANDLE            g_thread   = NULL;
static HANDLE            g_event    = NULL;

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

// Diagnostics: heartbeat (incremented each render loop iteration) and current stage.
// Stage values: 0=idle 1=got-frame 2=staging 3=acquire 4=fence-wait 5=queue-submit 6=present 7=recreate
static volatile long long g_render_hb    = 0;
static volatile int       g_render_stage = 0;

// ─── helpers ──────────────────────────────────────────────────────────────────

static double mono_sec(void) {
    LARGE_INTEGER f, c;
    QueryPerformanceFrequency(&f);
    QueryPerformanceCounter(&c);
    return (double)c.QuadPart / (double)f.QuadPart;
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
    const char *inst_exts[] = {
        VK_KHR_SURFACE_EXTENSION_NAME,
        VK_KHR_WIN32_SURFACE_EXTENSION_NAME,
    };
    VkInstanceCreateInfo ci = { VK_STRUCTURE_TYPE_INSTANCE_CREATE_INFO };
    ci.enabledExtensionCount   = 2;
    ci.ppEnabledExtensionNames = inst_exts;
    return vkCreateInstance(&ci, NULL, &g_inst) == VK_SUCCESS;
}

static int vk_select_device(void) {
    uint32_t n = 0;
    vkEnumeratePhysicalDevices(g_inst, &n, NULL);
    if (!n) return 0;
    VkPhysicalDevice *devs = (VkPhysicalDevice*)malloc(n * sizeof(VkPhysicalDevice));
    vkEnumeratePhysicalDevices(g_inst, &n, devs);

    // Prefer discrete GPU; otherwise first device with graphics queue.
    VkPhysicalDevice best = VK_NULL_HANDLE;
    int best_score = -1;
    for (uint32_t i = 0; i < n; i++) {
        VkPhysicalDeviceProperties pr;
        vkGetPhysicalDeviceProperties(devs[i], &pr);
        int score = (pr.deviceType == VK_PHYSICAL_DEVICE_TYPE_DISCRETE_GPU) ? 2
                  : (pr.deviceType == VK_PHYSICAL_DEVICE_TYPE_INTEGRATED_GPU) ? 1 : 0;

        uint32_t qn = 0;
        vkGetPhysicalDeviceQueueFamilyProperties(devs[i], &qn, NULL);
        VkQueueFamilyProperties *qp = (VkQueueFamilyProperties*)malloc(qn * sizeof(*qp));
        vkGetPhysicalDeviceQueueFamilyProperties(devs[i], &qn, qp);
        int has_gfx = 0;
        for (uint32_t j = 0; j < qn; j++)
            if (qp[j].queueFlags & VK_QUEUE_GRAPHICS_BIT) { has_gfx = 1; g_qfam = j; break; }
        free(qp);
        if (!has_gfx) continue;

        if (score > best_score) { best_score = score; best = devs[i]; }
    }
    free(devs);
    if (best == VK_NULL_HANDLE) return 0;
    g_pdev = best;

    // Re-find the queue family for the selected device.
    uint32_t qn = 0;
    vkGetPhysicalDeviceQueueFamilyProperties(g_pdev, &qn, NULL);
    VkQueueFamilyProperties *qp = (VkQueueFamilyProperties*)malloc(qn * sizeof(*qp));
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
    // Surface capabilities
    VkSurfaceCapabilitiesKHR caps;
    vkGetPhysicalDeviceSurfaceCapabilitiesKHR(g_pdev, g_surf, &caps);

    // Choose format: prefer BGRA8 SRGB or UNORM
    uint32_t nfmt = 0;
    vkGetPhysicalDeviceSurfaceFormatsKHR(g_pdev, g_surf, &nfmt, NULL);
    VkSurfaceFormatKHR *fmts = (VkSurfaceFormatKHR*)malloc(nfmt * sizeof(*fmts));
    vkGetPhysicalDeviceSurfaceFormatsKHR(g_pdev, g_surf, &nfmt, fmts);
    g_swap_fmt = fmts[0].format;
    VkColorSpaceKHR csp = fmts[0].colorSpace;
    for (uint32_t i = 0; i < nfmt; i++) {
        if (fmts[i].format == VK_FORMAT_B8G8R8A8_UNORM ||
            fmts[i].format == VK_FORMAT_B8G8R8A8_SRGB) {
            g_swap_fmt = fmts[i].format; csp = fmts[i].colorSpace; break;
        }
    }
    free(fmts);

    // Present mode preference depends on g_vsync (see vk_video_create's vsync arg):
    //   vsync off  -> IMMEDIATE → MAILBOX → FIFO_RELAXED → FIFO
    //   vsync on   -> MAILBOX → FIFO_RELAXED → FIFO → IMMEDIATE
    // With a WS_POPUP overlay (independent DWM window), IMMEDIATE is non-blocking:
    // vkQueuePresentKHR returns without waiting for DWM vsync, so it cannot block
    // or deadlock against Fyne's wglSwapBuffers on the parent window -- but it tears
    // in fast motion, since each decoded frame flips mid-scanout instead of at vblank.
    // MAILBOX gives the same non-blocking guarantee (the presentation engine just
    // swaps the queued image instead of stalling the submitter) while only ever
    // flipping at vblank, so it's preferred over IMMEDIATE when the caller wants
    // tear-free output. FIFO / FIFO_RELAXED, unlike MAILBOX, both involve DWM vsync
    // messaging that can stall the parent window's message pump even behind a
    // separate WS_POPUP, so they stay behind MAILBOX in both orders, only used if
    // the driver doesn't expose MAILBOX at all.
    int want_vsync = atomic_load(&g_vsync);
    uint32_t npm = 0;
    vkGetPhysicalDeviceSurfacePresentModesKHR(g_pdev, g_surf, &npm, NULL);
    VkPresentModeKHR *pms = (VkPresentModeKHR*)malloc(npm * sizeof(*pms));
    vkGetPhysicalDeviceSurfacePresentModesKHR(g_pdev, g_surf, &npm, pms);
    VkPresentModeKHR pm = VK_PRESENT_MODE_FIFO_KHR; // safe fallback
    int have_pm = 0;
    if (!want_vsync) {
        for (uint32_t i = 0; i < npm; i++) {
            if (pms[i] == VK_PRESENT_MODE_IMMEDIATE_KHR) { pm = pms[i]; have_pm = 1; break; }
        }
    }
    if (!have_pm) {
        for (uint32_t i = 0; i < npm; i++) {
            if (pms[i] == VK_PRESENT_MODE_MAILBOX_KHR) { pm = pms[i]; have_pm = 1; break; }
        }
    }
    if (!have_pm) {
        for (uint32_t i = 0; i < npm; i++) {
            if (pms[i] == VK_PRESENT_MODE_FIFO_RELAXED_KHR) { pm = pms[i]; have_pm = 1; }
        }
    }
    if (!have_pm && !want_vsync) {
        for (uint32_t i = 0; i < npm; i++) {
            if (pms[i] == VK_PRESENT_MODE_IMMEDIATE_KHR) { pm = pms[i]; have_pm = 1; break; }
        }
    }
    free(pms);

    g_swap_ext.width  = (uint32_t)(w > 0 ? w : caps.currentExtent.width);
    g_swap_ext.height = (uint32_t)(h > 0 ? h : caps.currentExtent.height);
    if (g_swap_ext.width  == 0) g_swap_ext.width  = 1;
    if (g_swap_ext.height == 0) g_swap_ext.height = 1;

    // Need at least 3 images with FIFO on a child HWND: with only 2 images
    // vkAcquireNextImageKHR blocks when DWM holds both, deadlocking Fyne's wglSwapBuffers.
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
        const char *pm_name = (pm == VK_PRESENT_MODE_IMMEDIATE_KHR)    ? "IMMEDIATE"     :
                              (pm == VK_PRESENT_MODE_MAILBOX_KHR)      ? "MAILBOX"       :
                              (pm == VK_PRESENT_MODE_FIFO_RELAXED_KHR) ? "FIFO_RELAXED"  :
                              (pm == VK_PRESENT_MODE_FIFO_KHR)         ? "FIFO"          : "OTHER";
        char pmlog[64];
        snprintf(pmlog, sizeof(pmlog), "swapchain present mode: %s (%d images)", pm_name, (int)sci.minImageCount);
        goVKLog(pmlog, 0);
    }
    if (vkCreateSwapchainKHR(g_dev, &sci, NULL, &g_swap) != VK_SUCCESS) return 0;

    vkGetSwapchainImagesKHR(g_dev, g_swap, &g_swap_count, NULL);
    g_swap_imgs  = (VkImage*)malloc(g_swap_count * sizeof(VkImage));
    g_swap_views = (VkImageView*)malloc(g_swap_count * sizeof(VkImageView));
    vkGetSwapchainImagesKHR(g_dev, g_swap, &g_swap_count, g_swap_imgs);

    for (uint32_t i = 0; i < g_swap_count; i++) {
        VkImageViewCreateInfo vci = { VK_STRUCTURE_TYPE_IMAGE_VIEW_CREATE_INFO };
        vci.image    = g_swap_imgs[i];
        vci.viewType = VK_IMAGE_VIEW_TYPE_2D;
        vci.format   = g_swap_fmt;
        vci.subresourceRange.aspectMask     = VK_IMAGE_ASPECT_COLOR_BIT;
        vci.subresourceRange.levelCount     = 1;
        vci.subresourceRange.layerCount     = 1;
        vkCreateImageView(g_dev, &vci, NULL, &g_swap_views[i]);
    }
    return 1;
}

static void vk_destroy_swapchain(void); // forward declaration

// vk_recreate_swapchain — called from render thread when swapchain is out-of-date.
// Also resizes the popup overlay to match the stored atomic rect.
static int vk_recreate_swapchain(void) {
    if (!g_dev || !g_surf) return 0;
    vkDeviceWaitIdle(g_dev);
    vk_destroy_swapchain();

    // Reposition popup overlay to current stored rect.
    // Coordinates are client-relative; convert to screen for the WS_POPUP window.
    int x = atomic_load(&g_dst_x);
    int y = atomic_load(&g_dst_y);
    int w = atomic_load(&g_dst_w);
    int h = atomic_load(&g_dst_h);
    if (g_child_hwnd && g_parent_hwnd && w > 0 && h > 0) {
        POINT pt = {x, y};
        ClientToScreen(g_parent_hwnd, &pt);
        SetWindowPos(g_child_hwnd, HWND_TOPMOST, pt.x, pt.y, w, h,
                     SWP_NOACTIVATE | SWP_ASYNCWINDOWPOS);
    }

    if (w <= 0) w = 1;
    if (h <= 0) h = 1;

    char m[128];
    snprintf(m, sizeof(m), "vk: recreating swapchain %dx%d", w, h);
    goVKLog(m, 0);
    int ok = vk_create_swapchain(w, h);
    if (!ok) goVKLog("vk: swapchain recreation failed", 2);
    return ok;
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

static int vk_ensure_tex(int w, int h) {
    if (g_tex != VK_NULL_HANDLE && g_tex_w == w && g_tex_h == h) return 1;

    // Destroy old
    if (g_tex != VK_NULL_HANDLE) {
        vkDeviceWaitIdle(g_dev);
        vkFreeMemory(g_dev, g_tex_mem, NULL); g_tex_mem = VK_NULL_HANDLE;
        vkDestroyImage(g_dev, g_tex, NULL);   g_tex     = VK_NULL_HANDLE;
    }

    VkImageCreateInfo ici = { VK_STRUCTURE_TYPE_IMAGE_CREATE_INFO };
    ici.imageType   = VK_IMAGE_TYPE_2D;
    ici.format      = VK_FORMAT_R8G8B8A8_UNORM; // Moonlight decoder output is always RGBA
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
    bci.size  = sz;
    bci.usage = VK_BUFFER_USAGE_TRANSFER_SRC_BIT;
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

// ─── image layout transition helper ──────────────────────────────────────────

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

// ─── zero-copy Vulkan-decode render path ─────────────────────────────────────
// Renders a decoded VkImage (NV12 8-bit or P010 10-bit, whichever the source
// stream negotiated) directly into the swapchain via a real
// VkSamplerYcbcrConversion + fragment shader — the GPU does the YCbCr->RGB
// conversion during sampling, so there is no CPU-side pixel path at all.
// Validated standalone (vk_ycbcr_render_test.c: decoded solid-red test frame
// sampled through this exact mechanism read back as RGBA(254,0,0,255)) before
// being wired in here.

#include "shader_arrays.h"

static VkShaderModule vk_shader_from_spv(const uint32_t *code, size_t code_size) {
    VkShaderModuleCreateInfo ci = { VK_STRUCTURE_TYPE_SHADER_MODULE_CREATE_INFO };
    ci.codeSize = code_size;
    ci.pCode    = code;
    VkShaderModule mod = VK_NULL_HANDLE;
    vkCreateShaderModule(g_dev, &ci, NULL, &mod);
    return mod;
}

// vk_ycbcr_pipeline_get returns the cached ycbcr sampler+pipeline bundle for
// the given multi-planar VkFormat (NV12 8-bit and P010 10-bit HDR each get
// their own entry — the sampler's YCbCr conversion is format-specific),
// creating it on first use. Returns NULL on failure.
static VkYcbcrPipeline *vk_ycbcr_pipeline_get(VkFormat fmt) {
    for (int i = 0; i < VK_YCBCR_PIPELINE_CACHE_SIZE; i++) {
        if (g_ycbcr_pipelines[i].fmt == fmt) return &g_ycbcr_pipelines[i];
    }
    int slot = -1;
    for (int i = 0; i < VK_YCBCR_PIPELINE_CACHE_SIZE; i++) {
        if (g_ycbcr_pipelines[i].fmt == VK_FORMAT_UNDEFINED) { slot = i; break; }
    }
    if (slot < 0) { goVKLog("vk_ycbcr_pipeline_get: cache full", 2); return NULL; }
    VkYcbcrPipeline *p = &g_ycbcr_pipelines[slot];

    VkSamplerYcbcrConversionCreateInfo convCI = { VK_STRUCTURE_TYPE_SAMPLER_YCBCR_CONVERSION_CREATE_INFO };
    convCI.format = fmt;
    convCI.ycbcrModel = VK_SAMPLER_YCBCR_MODEL_CONVERSION_YCBCR_601;
    convCI.ycbcrRange = VK_SAMPLER_YCBCR_RANGE_ITU_NARROW;
    convCI.components.r = VK_COMPONENT_SWIZZLE_IDENTITY;
    convCI.components.g = VK_COMPONENT_SWIZZLE_IDENTITY;
    convCI.components.b = VK_COMPONENT_SWIZZLE_IDENTITY;
    convCI.components.a = VK_COMPONENT_SWIZZLE_IDENTITY;
    convCI.xChromaOffset = VK_CHROMA_LOCATION_COSITED_EVEN;
    convCI.yChromaOffset = VK_CHROMA_LOCATION_COSITED_EVEN;
    convCI.chromaFilter = VK_FILTER_LINEAR;
    if (vkCreateSamplerYcbcrConversion(g_dev, &convCI, NULL, &p->conv) != VK_SUCCESS) {
        goVKLog("vk_ycbcr_pipeline_get: vkCreateSamplerYcbcrConversion failed", 2);
        memset(p, 0, sizeof(*p)); return NULL;
    }

    VkSamplerYcbcrConversionInfo convInfo = { VK_STRUCTURE_TYPE_SAMPLER_YCBCR_CONVERSION_INFO };
    convInfo.conversion = p->conv;
    VkSamplerCreateInfo sampCI = { VK_STRUCTURE_TYPE_SAMPLER_CREATE_INFO };
    sampCI.pNext = &convInfo;
    sampCI.magFilter = VK_FILTER_LINEAR;
    sampCI.minFilter = VK_FILTER_LINEAR;
    sampCI.addressModeU = VK_SAMPLER_ADDRESS_MODE_CLAMP_TO_EDGE;
    sampCI.addressModeV = VK_SAMPLER_ADDRESS_MODE_CLAMP_TO_EDGE;
    sampCI.addressModeW = VK_SAMPLER_ADDRESS_MODE_CLAMP_TO_EDGE;
    if (vkCreateSampler(g_dev, &sampCI, NULL, &p->sampler) != VK_SUCCESS) {
        goVKLog("vk_ycbcr_pipeline_get: vkCreateSampler failed", 2);
        vkDestroySamplerYcbcrConversion(g_dev, p->conv, NULL);
        memset(p, 0, sizeof(*p)); return NULL;
    }

    VkDescriptorSetLayoutBinding binding = {0};
    binding.binding = 0;
    binding.descriptorType = VK_DESCRIPTOR_TYPE_COMBINED_IMAGE_SAMPLER;
    binding.descriptorCount = 1;
    binding.stageFlags = VK_SHADER_STAGE_FRAGMENT_BIT;
    binding.pImmutableSamplers = &p->sampler;
    VkDescriptorSetLayoutCreateInfo dslCI = { VK_STRUCTURE_TYPE_DESCRIPTOR_SET_LAYOUT_CREATE_INFO };
    dslCI.bindingCount = 1; dslCI.pBindings = &binding;
    if (vkCreateDescriptorSetLayout(g_dev, &dslCI, NULL, &p->dsl) != VK_SUCCESS) goto fail;

    {
        VkPipelineLayoutCreateInfo plCI = { VK_STRUCTURE_TYPE_PIPELINE_LAYOUT_CREATE_INFO };
        plCI.setLayoutCount = 1; plCI.pSetLayouts = &p->dsl;
        if (vkCreatePipelineLayout(g_dev, &plCI, NULL, &p->playout) != VK_SUCCESS) goto fail;
    }

    // Pool sized for the small image-view cache — one descriptor set per
    // cached VkImage, all bound to this format's immutable sampler.
    {
        VkDescriptorPoolSize poolSize = { VK_DESCRIPTOR_TYPE_COMBINED_IMAGE_SAMPLER, VK_IMGVIEW_CACHE_SIZE };
        VkDescriptorPoolCreateInfo poolCI = { VK_STRUCTURE_TYPE_DESCRIPTOR_POOL_CREATE_INFO };
        poolCI.maxSets = VK_IMGVIEW_CACHE_SIZE; poolCI.poolSizeCount = 1; poolCI.pPoolSizes = &poolSize;
        if (vkCreateDescriptorPool(g_dev, &poolCI, NULL, &p->dpool) != VK_SUCCESS) goto fail;
    }

    {
        VkShaderModule vs = vk_shader_from_spv(g_ycbcr_vert_spv, sizeof(g_ycbcr_vert_spv));
        VkShaderModule fs = vk_shader_from_spv(g_ycbcr_frag_spv, sizeof(g_ycbcr_frag_spv));
        if (!vs || !fs) {
            if (vs) vkDestroyShaderModule(g_dev, vs, NULL);
            if (fs) vkDestroyShaderModule(g_dev, fs, NULL);
            goto fail;
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

        VkGraphicsPipelineCreateInfo pipeCI = { VK_STRUCTURE_TYPE_GRAPHICS_PIPELINE_CREATE_INFO };
        pipeCI.pNext = &renderingCI;
        pipeCI.stageCount = 2; pipeCI.pStages = stages;
        pipeCI.pVertexInputState = &vi; pipeCI.pInputAssemblyState = &ia;
        pipeCI.pViewportState = &vpState; pipeCI.pRasterizationState = &rs;
        pipeCI.pMultisampleState = &ms; pipeCI.pColorBlendState = &cb;
        pipeCI.pDynamicState = &dynCI;
        pipeCI.layout = p->playout;
        VkResult pr = vkCreateGraphicsPipelines(g_dev, VK_NULL_HANDLE, 1, &pipeCI, NULL, &p->pipeline);
        vkDestroyShaderModule(g_dev, vs, NULL);
        vkDestroyShaderModule(g_dev, fs, NULL);
        if (pr != VK_SUCCESS) goto fail;
    }

    p->fmt = fmt;
    return p;

fail:
    if (p->pipeline) vkDestroyPipeline(g_dev, p->pipeline, NULL);
    if (p->playout)  vkDestroyPipelineLayout(g_dev, p->playout, NULL);
    if (p->dpool)    vkDestroyDescriptorPool(g_dev, p->dpool, NULL);
    if (p->dsl)      vkDestroyDescriptorSetLayout(g_dev, p->dsl, NULL);
    if (p->sampler)  vkDestroySampler(g_dev, p->sampler, NULL);
    if (p->conv)     vkDestroySamplerYcbcrConversion(g_dev, p->conv, NULL);
    memset(p, 0, sizeof(*p));
    goVKLog("vk_ycbcr_pipeline_get: pipeline creation failed", 2);
    return NULL;
}

// vk_imgview_cache_get returns a descriptor set bound to (img, fmt), creating
// and caching the VkImageView + VkDescriptorSet on first sight of this
// VkImage. ffmpeg's internal Vulkan frame pool round-robins a small fixed set
// of images, so this cache stays tiny (VK_IMGVIEW_CACHE_SIZE) in practice.
static VkDescriptorSet vk_imgview_cache_get(VkImage img, VkFormat fmt, VkYcbcrPipeline *pl) {
    int free_slot = -1;
    for (int i = 0; i < VK_IMGVIEW_CACHE_SIZE; i++) {
        if (g_imgview_cache[i].img == img && g_imgview_cache[i].fmt == fmt) return g_imgview_cache[i].dset;
        if (free_slot < 0 && g_imgview_cache[i].img == VK_NULL_HANDLE) free_slot = i;
    }
    if (free_slot < 0) {
        // Cache full — reuse slot 0. vkDeviceWaitIdle in the caller's fence
        // wait already guarantees no in-flight command buffer references the
        // old view before we get here (single command buffer, single frame
        // in flight, same as the RGBA path above).
        free_slot = 0;
        if (g_imgview_cache[0].view) vkDestroyImageView(g_dev, g_imgview_cache[0].view, NULL);
    }

    VkSamplerYcbcrConversionInfo convInfo = { VK_STRUCTURE_TYPE_SAMPLER_YCBCR_CONVERSION_INFO };
    convInfo.conversion = pl->conv;
    VkImageViewCreateInfo viewCI = { VK_STRUCTURE_TYPE_IMAGE_VIEW_CREATE_INFO };
    viewCI.pNext = &convInfo;
    viewCI.image = img;
    viewCI.viewType = VK_IMAGE_VIEW_TYPE_2D;
    viewCI.format = fmt;
    viewCI.subresourceRange.aspectMask = VK_IMAGE_ASPECT_COLOR_BIT;
    viewCI.subresourceRange.levelCount = 1;
    viewCI.subresourceRange.layerCount = 1;
    VkImageView view;
    if (vkCreateImageView(g_dev, &viewCI, NULL, &view) != VK_SUCCESS) {
        goVKLog("vk_imgview_cache_get: vkCreateImageView failed", 2);
        return VK_NULL_HANDLE;
    }

    VkDescriptorSet dset = g_imgview_cache[free_slot].dset;
    if (!dset) {
        VkDescriptorSetAllocateInfo dsAI = { VK_STRUCTURE_TYPE_DESCRIPTOR_SET_ALLOCATE_INFO };
        dsAI.descriptorPool = pl->dpool; dsAI.descriptorSetCount = 1; dsAI.pSetLayouts = &pl->dsl;
        if (vkAllocateDescriptorSets(g_dev, &dsAI, &dset) != VK_SUCCESS) {
            goVKLog("vk_imgview_cache_get: vkAllocateDescriptorSets failed", 2);
            vkDestroyImageView(g_dev, view, NULL);
            return VK_NULL_HANDLE;
        }
    }
    VkDescriptorImageInfo imgInfo = { VK_NULL_HANDLE, view, VK_IMAGE_LAYOUT_SHADER_READ_ONLY_OPTIMAL };
    VkWriteDescriptorSet write = { VK_STRUCTURE_TYPE_WRITE_DESCRIPTOR_SET };
    write.dstSet = dset; write.dstBinding = 0; write.descriptorCount = 1;
    write.descriptorType = VK_DESCRIPTOR_TYPE_COMBINED_IMAGE_SAMPLER;
    write.pImageInfo = &imgInfo;
    vkUpdateDescriptorSets(g_dev, 1, &write, 0, NULL);

    g_imgview_cache[free_slot].img  = img;
    g_imgview_cache[free_slot].fmt  = fmt;
    g_imgview_cache[free_slot].view = view;
    g_imgview_cache[free_slot].dset = dset;
    return dset;
}

// vk_render_frame_vkimage — zero-copy counterpart to vk_render_frame: samples
// a decoded VkImage directly into the swapchain instead of blitting an
// uploaded RGBA staging texture. Same acquire/fence/present machinery.
static int vk_render_frame_vkimage(VkImage img, VkFormat fmt, VkImageLayout src_layout, int fw, int fh) {
    if (!g_dev || !g_swap) return 0;
    char _dbg[96];

    VkYcbcrPipeline *pl = vk_ycbcr_pipeline_get(fmt);
    if (!pl) return 0;
    VkDescriptorSet dset = vk_imgview_cache_get(img, fmt, pl);
    if (!dset) return 0;

    // In-order-execution sync: our own graphics-queue command buffer is
    // recorded/submitted strictly after this wait, so waiting for the
    // decode queue to go idle here guarantees the image's decode write has
    // completed before we sample it — the same mechanism proven correct in
    // the standalone same-device prototype (vk_samedev_readback_test.c /
    // vk_ycbcr_render_test.c), used here instead of the AVVkFrame timeline-
    // semaphore fields to avoid also having to hand ffmpeg's frame state
    // back in sync (layout/access/sem_value) after an external read.
    if (g_decode_queue) vkQueueWaitIdle(g_decode_queue);

    uint32_t img_idx = 0;
    g_render_stage = 3; // acquire
    double t0 = mono_sec();
    VkResult res = vkAcquireNextImageKHR(g_dev, g_swap, 3000000000ULL, g_img_sem, VK_NULL_HANDLE, &img_idx);
    double dt = mono_sec() - t0;
    if (dt > 0.1) {
        snprintf(_dbg, sizeof(_dbg), "SLOW AcquireNextImage %.0f ms res=%d", dt * 1000.0, (int)res);
        goVKLog(_dbg, 1);
    }
    if (res == VK_TIMEOUT) {
        goVKLog("AcquireNextImage TIMEOUT 3s — possible DWM/driver deadlock", 2);
        g_render_stage = 1; return 0;
    }
    if (res == VK_ERROR_OUT_OF_DATE_KHR) {
        g_render_stage = 7; vk_recreate_swapchain(); g_render_stage = 1; return 0;
    }
    if (res != VK_SUCCESS && res != VK_SUBOPTIMAL_KHR) {
        snprintf(_dbg, sizeof(_dbg), "AcquireNextImage failed res=%d", (int)res);
        goVKLog(_dbg, 2);
        g_render_stage = 1; return 0;
    }

    g_render_stage = 4; // fence-wait
    t0 = mono_sec();
    VkResult fence_res = vkWaitForFences(g_dev, 1, &g_fence, VK_TRUE, 2000000000ULL);
    dt = mono_sec() - t0;
    if (fence_res == VK_TIMEOUT) {
        goVKLog("WaitForFences TIMEOUT 2s — GPU hang?", 2);
        vkResetFences(g_dev, 1, &g_fence);
        g_render_stage = 1; return 0;
    }
    if (dt > 0.1) {
        snprintf(_dbg, sizeof(_dbg), "SLOW WaitForFences %.0f ms", dt * 1000.0);
        goVKLog(_dbg, 1);
    }
    vkResetFences(g_dev, 1, &g_fence);

    // Our own render work reading the previous frame's VkImage is now known
    // to have retired (the fence we just waited on guards exactly that) —
    // safe to release the AVFrame ref keeping it alive.
    if (g_vkf_prev_release_fn) { g_vkf_prev_release_fn(g_vkf_prev_release_ctx); }
    g_vkf_prev_release_ctx = NULL; g_vkf_prev_release_fn = NULL;

    vkResetCommandBuffer(g_cmdbuf, 0);
    VkCommandBufferBeginInfo bi = { VK_STRUCTURE_TYPE_COMMAND_BUFFER_BEGIN_INFO };
    bi.flags = VK_COMMAND_BUFFER_USAGE_ONE_TIME_SUBMIT_BIT;
    vkBeginCommandBuffer(g_cmdbuf, &bi);

    // Decoded image: transition from ffmpeg's actual current layout (passed
    // through from AVVkFrame.layout[0]) -> SHADER_READ_ONLY. Using
    // VK_IMAGE_LAYOUT_UNDEFINED here (as an earlier version of this code
    // did) tells the driver it may discard the image's existing contents
    // instead of preserving/converting them -- that produced a solid green
    // frame (correct decode, garbage/discarded data by the time the shader
    // sampled it), exactly the bug the standalone prototype's real-layout
    // barrier (vk_ycbcr_render_test.c) never hit.
    {
        VkImageMemoryBarrier b = { VK_STRUCTURE_TYPE_IMAGE_MEMORY_BARRIER };
        b.oldLayout = src_layout;
        b.newLayout = VK_IMAGE_LAYOUT_SHADER_READ_ONLY_OPTIMAL;
        b.srcQueueFamilyIndex = VK_QUEUE_FAMILY_IGNORED;
        b.dstQueueFamilyIndex = VK_QUEUE_FAMILY_IGNORED;
        b.image = img;
        b.subresourceRange.aspectMask = VK_IMAGE_ASPECT_COLOR_BIT;
        b.subresourceRange.levelCount = 1; b.subresourceRange.layerCount = 1;
        b.srcAccessMask = VK_ACCESS_MEMORY_WRITE_BIT;
        b.dstAccessMask = VK_ACCESS_SHADER_READ_BIT;
        vkCmdPipelineBarrier(g_cmdbuf, VK_PIPELINE_STAGE_ALL_COMMANDS_BIT, VK_PIPELINE_STAGE_FRAGMENT_SHADER_BIT,
                              0, 0, NULL, 0, NULL, 1, &b);
    }
    vk_image_barrier(g_cmdbuf, g_swap_imgs[img_idx],
        VK_IMAGE_LAYOUT_UNDEFINED, VK_IMAGE_LAYOUT_COLOR_ATTACHMENT_OPTIMAL,
        0, VK_ACCESS_COLOR_ATTACHMENT_WRITE_BIT,
        VK_PIPELINE_STAGE_TOP_OF_PIPE_BIT, VK_PIPELINE_STAGE_COLOR_ATTACHMENT_OUTPUT_BIT);

    // Aspect-ratio letterboxing, same math as the RGBA blit path.
    int sw = g_swap_ext.width, sh = g_swap_ext.height;
    float fa = (float)fw / (float)(fh ? fh : 1);
    float wa = (float)sw / (float)(sh ? sh : 1);
    int dx = 0, dy = 0, dw = sw, dh = sh;
    if (fa > wa) { dh = (int)(sw / fa + 0.5f); dy = (sh - dh) / 2; }
    else         { dw = (int)(sh * fa + 0.5f); dx = (sw - dw) / 2; }

    PFN_vkCmdBeginRendering pfnBeginRendering = (PFN_vkCmdBeginRendering)vkGetDeviceProcAddr(g_dev, "vkCmdBeginRendering");
    PFN_vkCmdEndRendering   pfnEndRendering   = (PFN_vkCmdEndRendering)vkGetDeviceProcAddr(g_dev, "vkCmdEndRendering");
    if (!pfnBeginRendering || !pfnEndRendering) {
        goVKLog("vk_render_frame_vkimage: vkCmdBeginRendering unavailable (need Vulkan 1.3)", 2);
        vkEndCommandBuffer(g_cmdbuf);
        g_render_stage = 1; return 0;
    }

    VkRenderingAttachmentInfo colorAtt = { VK_STRUCTURE_TYPE_RENDERING_ATTACHMENT_INFO };
    colorAtt.imageView = g_swap_views[img_idx];
    colorAtt.imageLayout = VK_IMAGE_LAYOUT_COLOR_ATTACHMENT_OPTIMAL;
    colorAtt.loadOp = VK_ATTACHMENT_LOAD_OP_CLEAR;
    colorAtt.storeOp = VK_ATTACHMENT_STORE_OP_STORE;
    colorAtt.clearValue.color.float32[3] = 1.0f;

    VkRenderingInfo renderInfo = { VK_STRUCTURE_TYPE_RENDERING_INFO };
    renderInfo.renderArea.extent.width = (uint32_t)sw; renderInfo.renderArea.extent.height = (uint32_t)sh;
    renderInfo.layerCount = 1;
    renderInfo.colorAttachmentCount = 1; renderInfo.pColorAttachments = &colorAtt;

    pfnBeginRendering(g_cmdbuf, &renderInfo);
    VkViewport vp = { (float)dx, (float)dy, (float)dw, (float)dh, 0.0f, 1.0f };
    VkRect2D sc = { { dx, dy }, { (uint32_t)dw, (uint32_t)dh } };
    vkCmdSetViewport(g_cmdbuf, 0, 1, &vp);
    vkCmdSetScissor(g_cmdbuf, 0, 1, &sc);
    vkCmdBindPipeline(g_cmdbuf, VK_PIPELINE_BIND_POINT_GRAPHICS, pl->pipeline);
    vkCmdBindDescriptorSets(g_cmdbuf, VK_PIPELINE_BIND_POINT_GRAPHICS, pl->playout, 0, 1, &dset, 0, NULL);
    vkCmdDraw(g_cmdbuf, 3, 1, 0, 0);
    pfnEndRendering(g_cmdbuf);

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
    g_render_stage = 5; // queue-submit
    vkQueueSubmit(g_queue, 1, &si, g_fence);

    VkPresentInfoKHR pi = { VK_STRUCTURE_TYPE_PRESENT_INFO_KHR };
    pi.waitSemaphoreCount = 1; pi.pWaitSemaphores = &g_rnd_sem;
    pi.swapchainCount = 1; pi.pSwapchains = &g_swap; pi.pImageIndices = &img_idx;
    g_render_stage = 6; // present
    t0 = mono_sec();
    res = vkQueuePresentKHR(g_queue, &pi);
    dt = mono_sec() - t0;
    if (dt > 0.1) {
        snprintf(_dbg, sizeof(_dbg), "SLOW QueuePresent %.0f ms res=%d", dt * 1000.0, (int)res);
        goVKLog(_dbg, 1);
    }
    g_render_stage = 1;
    if (res == VK_ERROR_OUT_OF_DATE_KHR || res == VK_SUBOPTIMAL_KHR) {
        g_render_stage = 7; vk_recreate_swapchain(); g_render_stage = 1;
        return 1;
    }
    if (res != VK_SUCCESS) {
        snprintf(_dbg, sizeof(_dbg), "QueuePresent failed res=%d", (int)res);
        goVKLog(_dbg, 2);
    }
    return (res == VK_SUCCESS) ? 1 : 0;
}

// ─── render one frame ─────────────────────────────────────────────────────────
// Called from the render thread. Returns 1 on success, 0 on recoverable error
// (e.g. swapchain out of date), -1 on fatal error.

static int vk_render_frame(uint8_t *pixels, int fw, int fh, int fs) {
    if (!g_dev || !g_swap) return 0;
    char _dbg[96];

    size_t frame_sz = (size_t)fh * (size_t)fs;

    g_render_stage = 2; // staging
    if (!vk_ensure_staging(frame_sz)) { g_render_stage = 1; return 0; }
    if (!vk_ensure_tex(fw, fh))       { g_render_stage = 1; return 0; }

    // Upload frame to staging buffer.
    size_t row = (size_t)fw * 4;
    if ((size_t)fs == row) {
        memcpy(g_stage_ptr, pixels, frame_sz);
    } else {
        uint8_t *dst = (uint8_t*)g_stage_ptr;
        for (int y = 0; y < fh; y++)
            memcpy(dst + (size_t)y * row, pixels + (size_t)y * (size_t)fs, row);
    }

    // Acquire swapchain image — 3 s timeout so we don't hang forever on DWM deadlock.
    uint32_t img_idx = 0;
    g_render_stage = 3; // acquire
    double t0 = mono_sec();
    VkResult res = vkAcquireNextImageKHR(g_dev, g_swap, 3000000000ULL, g_img_sem, VK_NULL_HANDLE, &img_idx);
    double dt = mono_sec() - t0;
    if (dt > 0.1) {
        snprintf(_dbg, sizeof(_dbg), "SLOW AcquireNextImage %.0f ms res=%d", dt * 1000.0, (int)res);
        goVKLog(_dbg, 1);
    }
    if (res == VK_TIMEOUT) {
        goVKLog("AcquireNextImage TIMEOUT 3s — possible DWM/driver deadlock", 2);
        g_render_stage = 1; return 0;
    }
    if (res == VK_ERROR_OUT_OF_DATE_KHR) {
        // Swapchain out of date — recreate and skip this frame.
        // NOTE: AcquireNextImage with OUT_OF_DATE did NOT signal g_img_sem, so
        // we must NOT wait on it in QueueSubmit this iteration.
        g_render_stage = 7; // recreate
        vk_recreate_swapchain();
        g_render_stage = 1; return 0;
    }
    if (res != VK_SUCCESS && res != VK_SUBOPTIMAL_KHR) {
        snprintf(_dbg, sizeof(_dbg), "AcquireNextImage failed res=%d", (int)res);
        goVKLog(_dbg, 2);
        g_render_stage = 1; return 0;
    }

    // Wait for previous work on this command buffer (2-second timeout guards driver hang).
    g_render_stage = 4; // fence-wait
    t0 = mono_sec();
    VkResult fence_res = vkWaitForFences(g_dev, 1, &g_fence, VK_TRUE, 2000000000ULL);
    dt = mono_sec() - t0;
    if (fence_res == VK_TIMEOUT) {
        goVKLog("WaitForFences TIMEOUT 2s — GPU hang?", 2);
        vkResetFences(g_dev, 1, &g_fence);
        g_render_stage = 1; return 0;
    }
    if (dt > 0.1) {
        snprintf(_dbg, sizeof(_dbg), "SLOW WaitForFences %.0f ms", dt * 1000.0);
        goVKLog(_dbg, 1);
    }
    vkResetFences(g_dev, 1, &g_fence);

    // Record commands.
    vkResetCommandBuffer(g_cmdbuf, 0);
    VkCommandBufferBeginInfo bi = { VK_STRUCTURE_TYPE_COMMAND_BUFFER_BEGIN_INFO };
    bi.flags = VK_COMMAND_BUFFER_USAGE_ONE_TIME_SUBMIT_BIT;
    vkBeginCommandBuffer(g_cmdbuf, &bi);

    // staging → texture
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

    // Swapchain image → TRANSFER_DST
    vk_image_barrier(g_cmdbuf, g_swap_imgs[img_idx],
        VK_IMAGE_LAYOUT_UNDEFINED, VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL,
        0, VK_ACCESS_TRANSFER_WRITE_BIT,
        VK_PIPELINE_STAGE_TOP_OF_PIPE_BIT, VK_PIPELINE_STAGE_TRANSFER_BIT);

    // Blit with aspect-ratio letterboxing.
    int sw = g_swap_ext.width, sh = g_swap_ext.height;
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
        g_tex,              VK_IMAGE_LAYOUT_TRANSFER_SRC_OPTIMAL,
        g_swap_imgs[img_idx], VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL,
        1, &blt, VK_FILTER_LINEAR);

    // Swapchain image → PRESENT
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
    g_render_stage = 5; // queue-submit
    vkQueueSubmit(g_queue, 1, &si, g_fence);

    VkPresentInfoKHR pi = { VK_STRUCTURE_TYPE_PRESENT_INFO_KHR };
    pi.waitSemaphoreCount = 1;
    pi.pWaitSemaphores    = &g_rnd_sem;
    pi.swapchainCount     = 1;
    pi.pSwapchains        = &g_swap;
    pi.pImageIndices      = &img_idx;
    g_render_stage = 6; // present
    t0 = mono_sec();
    res = vkQueuePresentKHR(g_queue, &pi);
    dt = mono_sec() - t0;
    if (dt > 0.1) {
        snprintf(_dbg, sizeof(_dbg), "SLOW QueuePresent %.0f ms res=%d", dt * 1000.0, (int)res);
        goVKLog(_dbg, 1);
    }
    g_render_stage = 1;
    if (res == VK_ERROR_OUT_OF_DATE_KHR || res == VK_SUBOPTIMAL_KHR) {
        g_render_stage = 7; // recreate
        vk_recreate_swapchain(); // recreate for next frame; this frame was presented (or lost)
        g_render_stage = 1;
        return 1; // frame counts as rendered
    }
    if (res != VK_SUCCESS) {
        snprintf(_dbg, sizeof(_dbg), "QueuePresent failed res=%d", (int)res);
        goVKLog(_dbg, 2);
    }
    return (res == VK_SUCCESS) ? 1 : 0;
}

// ─── render thread ────────────────────────────────────────────────────────────

static DWORD WINAPI vk_render_thread(LPVOID unused) {
    (void)unused;
    double hb_log_t = mono_sec(); // last time we printed a heartbeat log
    long long consec_fail = 0;    // consecutive vk_render_frame failures
    POINT last_parent_pt  = {-1, -1}; // last known screen origin of parent client area
    int   last_want_hidden = -1;       // -1=unknown; 0=visible; 1=hidden (iconic OR g_hidden)
    while (atomic_load(&g_active)) {
        g_render_stage = 0; // idle — waiting for next frame event
        WaitForSingleObject(g_event, 8);
        g_render_hb++;      // advance heartbeat each iteration (visible to Go watchdog)
        if (!atomic_load(&g_active)) break;

        if (g_parent_hwnd && g_child_hwnd) {
            // Unified visibility: hide when:
            //   • parent minimized (iconic)
            //   • Fyne window is not foreground (another app has focus)
            //   • Go requested hide: open menu/popup (same as macOS Metal SetHidden)
            int iconic     = IsIconic(g_parent_hwnd) ? 1 : 0;
            HWND _fg2 = GetForegroundWindow();
            DWORD fg_pid = 0;
            if (_fg2) GetWindowThreadProcessId(_fg2, &fg_pid);
            int fg_hidden  = (fg_pid != GetCurrentProcessId()) ? 1 : 0;
            int want_hidden = iconic | fg_hidden | atomic_load(&g_hidden);
            if (want_hidden != last_want_hidden) {
                char vis[128];
                snprintf(vis, sizeof(vis),
                    "overlay visibility change: want_hidden=%d (iconic=%d fg_hidden=%d g_hidden=%d)",
                    want_hidden, iconic, fg_hidden, (int)atomic_load(&g_hidden));
                goVKLog(vis, want_hidden ? 1 : 0);
                last_want_hidden = want_hidden;
                // Post to window thread — ShowWindow cross-thread needs message pump.
                PostMessageW(g_child_hwnd, want_hidden ? WM_USER+1 : WM_USER+2, 0, 0);
            }

            if (!last_want_hidden) {
                // Track parent window movement (WS_EX_TOPMOST keeps Z-order stable).
                POINT origin = {0, 0};
                ClientToScreen(g_parent_hwnd, &origin);
                if (origin.x != last_parent_pt.x || origin.y != last_parent_pt.y) {
                    last_parent_pt = origin;
                    int cx = atomic_load(&g_dst_x);
                    int cy = atomic_load(&g_dst_y);
                    int cw = atomic_load(&g_dst_w);
                    int ch = atomic_load(&g_dst_h);
                    if (cw > 0 && ch > 0) {
                        POINT pt = {cx, cy};
                        ClientToScreen(g_parent_hwnd, &pt);
                        SetWindowPos(g_child_hwnd, HWND_TOPMOST, pt.x, pt.y, cw, ch,
                                     SWP_NOACTIVATE | SWP_ASYNCWINDOWPOS);
                    }
                }
            }
        }

        // Periodic render-thread heartbeat visible in the log even if Go goroutines freeze.
        double hb_now = mono_sec();
        if (hb_now - hb_log_t >= 5.0) {
            char hbm[128];
            snprintf(hbm, sizeof(hbm),
                "render thread alive hb=%lld rendered=%lld submitted=%lld stage=%d hidden=%d",
                (long long)g_render_hb, (long long)g_rendered, (long long)g_submitted,
                g_render_stage, (int)atomic_load(&g_hidden));
            goVKLog(hbm, 0);
            hb_log_t = hb_now;
        }

        int rf;
        int fw = 0, fh = 0;
        if (g_frame_mode == VK_FRAME_MODE_VKIMAGE) {
            VkImage img = VK_NULL_HANDLE; VkFormat fmt = VK_FORMAT_UNDEFINED;
            VkImageLayout layout = VK_IMAGE_LAYOUT_UNDEFINED;
            void *rel_ctx = NULL;
            void (*rel_fn)(void*) = NULL;
            EnterCriticalSection(&g_cs);
            if (g_vkf_ready) {
                img = g_vkf_img; fmt = g_vkf_fmt; layout = g_vkf_layout; fw = g_vkf_w; fh = g_vkf_h;
                rel_ctx = g_vkf_release_ctx; rel_fn = g_vkf_release_fn;
                g_vkf_ready = 0;
            }
            LeaveCriticalSection(&g_cs);

            if (img == VK_NULL_HANDLE) continue;
            g_has_frame = 1;

            g_render_stage = 1; // got frame — entering vk_render_frame_vkimage
            rf = vk_render_frame_vkimage(img, fmt, layout, fw, fh);
            if (rf) {
                // Fence-wait at the top of the NEXT call confirms this
                // frame's GPU read has retired before its ref is dropped.
                g_vkf_prev_release_ctx = rel_ctx;
                g_vkf_prev_release_fn  = rel_fn;
            } else if (rel_fn) {
                // No GPU work was submitted for this frame (acquire/pipeline
                // failure) — nothing reads the image, safe to release now.
                rel_fn(rel_ctx);
            }
        } else {
            uint8_t *tmp = NULL;
            int fs = 0;
            EnterCriticalSection(&g_cs);
            if (g_ready && g_buf) {
                fw = g_fw; fh = g_fh; fs = g_fs;
                size_t sz = (size_t)fh * (size_t)fs;
                tmp = (uint8_t*)malloc(sz);
                if (tmp) memcpy(tmp, g_buf, sz);
                g_ready = 0;
            }
            LeaveCriticalSection(&g_cs);

            if (!tmp) continue;
            g_has_frame = 1;

            g_render_stage = 1; // got frame — entering vk_render_frame
            rf = vk_render_frame(tmp, fw, fh, fs);
            free(tmp);
        }
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

        // Stats
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
    return 0;
}

// ─── Public C API ─────────────────────────────────────────────────────────────

int vk_video_is_active(void) { return atomic_load(&g_active); }

int vk_video_try_submit(uint8_t *rgba, int width, int height, int stride) {
    if (!atomic_load(&g_active)) return 0;
    if (!g_cs_init) return 0;
    size_t sz = (size_t)height * (size_t)stride;
    EnterCriticalSection(&g_cs);
    if (!atomic_load(&g_active)) {
        LeaveCriticalSection(&g_cs);
        return 0;
    }
    if (!g_buf || g_buf_sz < sz) {
        free(g_buf);
        g_buf    = (uint8_t*)malloc(sz);
        g_buf_sz = g_buf ? sz : 0;
    }
    if (g_buf) {
        memcpy(g_buf, rgba, sz);
        g_fw = width; g_fh = height; g_fs = stride;
        g_ready = 1; g_submitted++;
    }
    LeaveCriticalSection(&g_cs);
    SetEvent(g_event);
    return 1;
}

// vk_video_try_submit_vkframe — zero-copy counterpart to vk_video_try_submit:
// hands a decoded VkImage straight to the render thread instead of copying
// RGBA pixels. release_fn(release_ctx) is called once the renderer's GPU
// read of vk_frame has retired (or immediately, if the frame is dropped
// without ever being read) — see moonlight_cgo_windows.go's
// win_deliver_frame_vulkan for the AVFrame-ref lifetime this protects.
// Returns 1 if the frame was accepted (release_fn will be called later), 0
// if rejected (caller must release immediately).
int vk_video_try_submit_vkframe(void *vk_image, int vk_format, int vk_layout, int width, int height,
                                 int narrow_range, void *release_ctx, void (*release_fn)(void *)) {
    (void)narrow_range; // reserved: VK_SAMPLER_YCBCR_RANGE_ITU_NARROW is currently hardcoded, matches Moonlight's H264/HEVC streams
    if (!atomic_load(&g_active) || !g_cs_init) return 0;
    EnterCriticalSection(&g_cs);
    if (!atomic_load(&g_active)) {
        LeaveCriticalSection(&g_cs);
        return 0;
    }
    g_frame_mode = VK_FRAME_MODE_VKIMAGE;
    // Single-slot, drop-on-full semantics (same as the RGBA path): if a
    // previous submission is still waiting to be picked up by the render
    // thread, release it now — its data was never read by anyone.
    if (g_vkf_ready && g_vkf_release_fn) {
        g_vkf_release_fn(g_vkf_release_ctx);
    }
    g_vkf_img = (VkImage)vk_image;
    g_vkf_fmt = (VkFormat)vk_format;
    g_vkf_layout = (VkImageLayout)vk_layout;
    g_vkf_w = width; g_vkf_h = height;
    g_vkf_release_ctx = release_ctx;
    g_vkf_release_fn  = release_fn;
    g_vkf_ready = 1;
    g_submitted++;
    LeaveCriticalSection(&g_cs);
    SetEvent(g_event);
    return 1;
}

void vk_video_update_frame(int x, int y, int w, int h) {
    if (!atomic_load(&g_active)) return;
    // In standalone mode the window IS the full screen — nothing to reposition.
    if (g_standalone) return;
    atomic_store(&g_dst_x, x);
    atomic_store(&g_dst_y, y);
    atomic_store(&g_dst_w, w);
    atomic_store(&g_dst_h, h);
    // For a WS_POPUP overlay, SetWindowPos with SWP_ASYNCWINDOWPOS is safe from any
    // thread and does NOT synchronise with DWM (unlike WS_CHILD repositioning).
    if (g_child_hwnd && g_parent_hwnd && w > 0 && h > 0) {
        POINT pt = {x, y};
        ClientToScreen(g_parent_hwnd, &pt);
        SetWindowPos(g_child_hwnd, HWND_TOPMOST, pt.x, pt.y, w, h,
                     SWP_NOACTIVATE | SWP_ASYNCWINDOWPOS);
    }
}

static void vk_full_cleanup(void) {
    atomic_store(&g_active, 0);

    // Hide/close the overlay FIRST so it stops eating mouse input even if
    // vkDeviceWaitIdle or the render-thread join later stall (dead GPU /
    // powered-off KVM). ShowWindow from this thread would SendMessage and
    // can deadlock; post hide+close onto the hwnd thread instead.
    if (g_child_hwnd) {
        PostMessageW(g_child_hwnd, WM_USER+1, 0, 0);
        PostMessageW(g_child_hwnd, WM_CLOSE, 0, 0);
        g_child_hwnd = NULL;
    }

    if (g_thread)  { SetEvent(g_event); WaitForSingleObject(g_thread, 3000); CloseHandle(g_thread); g_thread = NULL; }
    if (g_event)   { CloseHandle(g_event); g_event = NULL; }

    if (g_dev) {
        vkDeviceWaitIdle(g_dev);
        if (g_stage_ptr && g_stage_mem) { vkUnmapMemory(g_dev, g_stage_mem); g_stage_ptr = NULL; }
        if (g_stage_buf)  { vkDestroyBuffer(g_dev, g_stage_buf, NULL); g_stage_buf = VK_NULL_HANDLE; }
        if (g_stage_mem)  { vkFreeMemory(g_dev, g_stage_mem, NULL);  g_stage_mem = VK_NULL_HANDLE; }
        g_stage_sz = 0;
        if (g_tex)     { vkDestroyImage(g_dev, g_tex, NULL);      g_tex = VK_NULL_HANDLE; }
        if (g_tex_mem) { vkFreeMemory(g_dev, g_tex_mem, NULL);    g_tex_mem = VK_NULL_HANDLE; }
        g_tex_w = 0; g_tex_h = 0;

        // Zero-copy path: drop any pending/in-flight decoded frame refs, and
        // tear down the cached image views + per-format ycbcr pipelines.
        if (g_vkf_ready && g_vkf_release_fn) { g_vkf_release_fn(g_vkf_release_ctx); }
        g_vkf_ready = 0; g_vkf_img = VK_NULL_HANDLE; g_vkf_release_ctx = NULL; g_vkf_release_fn = NULL;
        if (g_vkf_prev_release_fn) { g_vkf_prev_release_fn(g_vkf_prev_release_ctx); }
        g_vkf_prev_release_ctx = NULL; g_vkf_prev_release_fn = NULL;
        for (int i = 0; i < VK_IMGVIEW_CACHE_SIZE; i++) {
            if (g_imgview_cache[i].view) vkDestroyImageView(g_dev, g_imgview_cache[i].view, NULL);
            memset(&g_imgview_cache[i], 0, sizeof(g_imgview_cache[i]));
        }
        for (int i = 0; i < VK_YCBCR_PIPELINE_CACHE_SIZE; i++) {
            VkYcbcrPipeline *p = &g_ycbcr_pipelines[i];
            if (p->pipeline) vkDestroyPipeline(g_dev, p->pipeline, NULL);
            if (p->playout)  vkDestroyPipelineLayout(g_dev, p->playout, NULL);
            if (p->dpool)    vkDestroyDescriptorPool(g_dev, p->dpool, NULL);
            if (p->dsl)      vkDestroyDescriptorSetLayout(g_dev, p->dsl, NULL);
            if (p->sampler)  vkDestroySampler(g_dev, p->sampler, NULL);
            if (p->conv)     vkDestroySamplerYcbcrConversion(g_dev, p->conv, NULL);
            memset(p, 0, sizeof(*p));
        }
        g_decode_queue = VK_NULL_HANDLE; g_decode_qfam = UINT32_MAX;

        if (g_img_sem) { vkDestroySemaphore(g_dev, g_img_sem, NULL); g_img_sem = VK_NULL_HANDLE; }
        if (g_rnd_sem) { vkDestroySemaphore(g_dev, g_rnd_sem, NULL); g_rnd_sem = VK_NULL_HANDLE; }
        if (g_fence)   { vkDestroyFence(g_dev, g_fence, NULL);       g_fence = VK_NULL_HANDLE; }
        if (g_cmdbuf && g_cmdpool) { vkFreeCommandBuffers(g_dev, g_cmdpool, 1, &g_cmdbuf); g_cmdbuf = VK_NULL_HANDLE; }
        if (g_cmdpool) { vkDestroyCommandPool(g_dev, g_cmdpool, NULL); g_cmdpool = VK_NULL_HANDLE; }
        vk_destroy_swapchain();
        if (g_surf) { vkDestroySurfaceKHR(g_inst, g_surf, NULL); g_surf = VK_NULL_HANDLE; }
        // g_dev/g_inst are owned by ffmpeg's AVBufferRef when adopted
        // (g_ext_device) -- only destroy them when WE created them.
        if (!g_ext_device) vkDestroyDevice(g_dev, NULL);
        g_dev = VK_NULL_HANDLE;
    }
    if (g_inst && !g_ext_device) vkDestroyInstance(g_inst, NULL);
    g_inst = VK_NULL_HANDLE;
    g_pdev = VK_NULL_HANDLE;
    g_ext_device = 0;
    g_frame_mode = VK_FRAME_MODE_RGBA;

    // Overlay HWND was already posted WM_CLOSE at the start of cleanup.
    if (g_hwnd_thread) { WaitForSingleObject(g_hwnd_thread, 3000); CloseHandle(g_hwnd_thread); g_hwnd_thread = NULL; }
    if (g_hwnd_ready)  { CloseHandle(g_hwnd_ready); g_hwnd_ready = NULL; }
    if (g_cs_init) {
        EnterCriticalSection(&g_cs);
        if (g_buf)        { free(g_buf); g_buf = NULL; g_buf_sz = 0; }
        g_has_frame = 0; g_ready = 0;
        LeaveCriticalSection(&g_cs);
        DeleteCriticalSection(&g_cs);
        g_cs_init = 0;
    }
    if (g_eq_cs_init) {
        EnterCriticalSection(&g_eq_cs);
        g_eq_head = 0; g_eq_tail = 0;
        LeaveCriticalSection(&g_eq_cs);
        DeleteCriticalSection(&g_eq_cs);
        g_eq_cs_init = 0;
    }
    if (g_kq_cs_init) {
        EnterCriticalSection(&g_kq_cs);
        g_kq_head = 0; g_kq_tail = 0;
        LeaveCriticalSection(&g_kq_cs);
        DeleteCriticalSection(&g_kq_cs);
        g_kq_cs_init = 0;
    }
    g_parent_hwnd = NULL;
    g_raw_mouse = 0;
    g_standalone = 0;
    g_btn_down_mask = 0;
    g_rendered = 0; g_submitted = 0;
}

// ─── Common Vulkan initialisation ─────────────────────────────────────────────
// Shared by vk_video_create (overlay) and vk_video_create_standalone (fullscreen).
// g_parent_hwnd, g_standalone, g_hwnd_args must be set before calling.

static int vk_video_init_common(int x, int y, int w, int h) {
    // Register overlay window class once.
    if (!g_wndcls) {
        WNDCLASSEXW wc = { sizeof(wc) };
        wc.lpfnWndProc   = vk_wnd_proc;
        wc.hInstance     = GetModuleHandleW(NULL);
        wc.lpszClassName = L"usbridgeVKVideo";
        wc.hbrBackground = (HBRUSH)GetStockObject(BLACK_BRUSH);
        wc.hCursor       = LoadCursor(NULL, IDC_ARROW);
        g_wndcls = RegisterClassExW(&wc);
        if (!g_wndcls) { goVKLog("vk_video: RegisterClass failed", 2); return 0; }
    }

    // Spawn dedicated window thread — owns its message pump and DWM present context.
    g_hwnd_ready = CreateEventW(NULL, FALSE, FALSE, NULL);
    if (!g_hwnd_ready) { goVKLog("vk_video: CreateEvent failed", 2); return 0; }
    g_hwnd_thread = CreateThread(NULL, 0, vk_hwnd_thread, NULL, 0, NULL);
    if (!g_hwnd_thread) {
        CloseHandle(g_hwnd_ready); g_hwnd_ready = NULL;
        goVKLog("vk_video: CreateThread(hwnd) failed", 2); return 0;
    }
    if (WaitForSingleObject(g_hwnd_ready, 5000) != WAIT_OBJECT_0 || !g_child_hwnd) {
        goVKLog("vk_video: overlay window creation failed/timeout", 2);
        CloseHandle(g_hwnd_ready); g_hwnd_ready = NULL;
        if (g_hwnd_thread) { WaitForSingleObject(g_hwnd_thread, 1000); CloseHandle(g_hwnd_thread); g_hwnd_thread = NULL; }
        return 0;
    }
    CloseHandle(g_hwnd_ready); g_hwnd_ready = NULL;

    // Vulkan init. Prefer adopting the shared ffmpeg-owned Vulkan hwaccel
    // device (win_vk_hwdev_get, moonlight_cgo_windows.go) so decode and
    // presentation share one VkDevice/VkImage with zero cross-device copy --
    // validated standalone before this integration (real Vulkan Video Decode,
    // same-device plane readback, and a real VkSamplerYcbcrConversion render
    // pass all confirmed correct on this GPU/driver). Only ffmpeg's own
    // auto-create path reliably initializes a Vulkan-video-decode-capable
    // device; handing ffmpeg a from-scratch manually-created VkDevice (the
    // reverse direction) crashed deep in libavcodec's decode internals even
    // after matching every extension/feature ffmpeg's own auto-create enables,
    // so that direction is not attempted. Falls back to creating our own
    // plain graphics-only device (today's behavior, no decode capability)
    // when the shared device is unavailable -- decode then independently
    // falls back to D3D11VA/software in moonlight_cgo_windows.go.
    {
        void *ext_inst = NULL, *ext_phys = NULL, *ext_dev = NULL;
        uint32_t ext_gfx_qf = 0;
        if (win_vk_hwdev_get(&ext_inst, &ext_phys, &ext_dev, &ext_gfx_qf)) {
            g_inst = (VkInstance)ext_inst;
            g_pdev = (VkPhysicalDevice)ext_phys;
            g_dev  = (VkDevice)ext_dev;
            g_qfam = ext_gfx_qf;
            g_ext_device = 1;
            vkGetDeviceQueue(g_dev, g_qfam, 0, &g_queue);

            // Also grab a video-decode-capable queue (if any) for the
            // in-order vkQueueWaitIdle sync the zero-copy submit path uses
            // before sampling a just-decoded frame — proven sufficient for
            // correctness in the standalone same-device prototype.
            uint32_t qn = 0;
            vkGetPhysicalDeviceQueueFamilyProperties(g_pdev, &qn, NULL);
            VkQueueFamilyProperties *qp = (VkQueueFamilyProperties*)malloc(qn * sizeof(*qp));
            vkGetPhysicalDeviceQueueFamilyProperties(g_pdev, &qn, qp);
            for (uint32_t j = 0; j < qn; j++) {
                if (qp[j].queueFlags & VK_QUEUE_VIDEO_DECODE_BIT_KHR) { g_decode_qfam = j; break; }
            }
            free(qp);
            if (g_decode_qfam != UINT32_MAX) vkGetDeviceQueue(g_dev, g_decode_qfam, 0, &g_decode_queue);

            goVKLog("vk_video: adopted shared Vulkan hwaccel device (decode+render on one VkDevice)", 0);
        } else {
            g_ext_device = 0;
            if (!vk_create_instance()) { goVKLog("vk_video: vkCreateInstance failed", 2); goto fail; }
            if (!vk_select_device())   { goVKLog("vk_video: no suitable GPU found", 2);   goto fail; }
            if (!vk_create_device())   { goVKLog("vk_video: vkCreateDevice failed", 2);   goto fail; }
        }
    }

    {
        VkWin32SurfaceCreateInfoKHR sci = { VK_STRUCTURE_TYPE_WIN32_SURFACE_CREATE_INFO_KHR };
        sci.hinstance = GetModuleHandleW(NULL);
        sci.hwnd      = g_child_hwnd;
        if (vkCreateWin32SurfaceKHR(g_inst, &sci, NULL, &g_surf) != VK_SUCCESS) {
            goVKLog("vk_video: vkCreateWin32SurfaceKHR failed", 2); goto fail;
        }
    }

    if (!vk_create_swapchain(w > 0 ? w : 1, h > 0 ? h : 1)) {
        goVKLog("vk_video: swapchain creation failed", 2); goto fail;
    }

    // Command pool + buffer
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

    // Semaphores + fence
    {
        VkSemaphoreCreateInfo semi = { VK_STRUCTURE_TYPE_SEMAPHORE_CREATE_INFO };
        VkFenceCreateInfo fci = { VK_STRUCTURE_TYPE_FENCE_CREATE_INFO };
        fci.flags = VK_FENCE_CREATE_SIGNALED_BIT;
        if (vkCreateSemaphore(g_dev, &semi, NULL, &g_img_sem) != VK_SUCCESS) goto fail;
        if (vkCreateSemaphore(g_dev, &semi, NULL, &g_rnd_sem) != VK_SUCCESS) goto fail;
        if (vkCreateFence(g_dev, &fci, NULL, &g_fence)        != VK_SUCCESS) goto fail;
    }

    atomic_store(&g_dst_x, x);
    atomic_store(&g_dst_y, y);
    atomic_store(&g_dst_w, w);
    atomic_store(&g_dst_h, h);

    InitializeCriticalSection(&g_cs); g_cs_init = 1;
    InitializeCriticalSection(&g_eq_cs); g_eq_cs_init = 1;
    g_eq_head = 0; g_eq_tail = 0;
    InitializeCriticalSection(&g_kq_cs); g_kq_cs_init = 1;
    g_kq_head = 0; g_kq_tail = 0;
    g_event = CreateEventW(NULL, FALSE, FALSE, NULL);
    if (!g_event) goto fail;

    g_submitted = 0; g_rendered = 0; g_fps_n = 0; g_fps_t0 = 0;
    g_ready = 0; g_has_frame = 0; g_stat_first = 0;
    g_stat_max_gap_ms = 0; g_last_blit_ts = 0;
    atomic_store(&g_active, 1);

    g_thread = CreateThread(NULL, 0, vk_render_thread, NULL, 0, NULL);
    if (!g_thread) { atomic_store(&g_active, 0); goto fail; }
    return 1;

fail:
    vk_full_cleanup();
    return 0;
}

// vk_video_create — initialise Vulkan renderer as a child overlay over a Fyne window.
// vsync: see the present-mode preference comment in vk_create_swapchain.
// Returns 1 on success, 0 if Vulkan is unavailable (caller falls back to GDI).
int vk_video_create(uintptr_t parent_hwnd, int x, int y, int w, int h, int vsync) {
    if (atomic_load(&g_active)) vk_full_cleanup();

    HWND parent = (HWND)(uintptr_t)parent_hwnd;
    if (!parent) { goVKLog("vk_video_create: parent HWND is null", 2); return 0; }
    atomic_store(&g_vsync, vsync ? 1 : 0);
    g_parent_hwnd = parent;
    g_standalone = 0;
    g_hwnd_args.parent = parent; g_hwnd_args.x = x; g_hwnd_args.y = y;
    g_hwnd_args.w = w; g_hwnd_args.h = h; g_hwnd_args.standalone = 0;

    if (!vk_video_init_common(x, y, w, h)) return 0;

    {
        char m[256];
        VkPhysicalDeviceProperties pr;
        vkGetPhysicalDeviceProperties(g_pdev, &pr);
        snprintf(m, sizeof(m), "Vulkan overlay created — GPU=%s rect=(%d,%d,%dx%d)", pr.deviceName, x, y, w, h);
        goVKLog(m, 0);
    }
    return 1;
}

// vk_monitor_rect_near_hwnd fills x/y/w/h with the monitor that contains
// hint (MONITOR_DEFAULTTONEAREST). Falls back to the primary display.
static void vk_monitor_rect_near_hwnd(HWND hint, int *x, int *y, int *w, int *h) {
    if (hint) {
        HMONITOR mon = MonitorFromWindow(hint, MONITOR_DEFAULTTONEAREST);
        MONITORINFO mi;
        ZeroMemory(&mi, sizeof(mi));
        mi.cbSize = sizeof(mi);
        if (mon && GetMonitorInfoW(mon, &mi)) {
            *x = mi.rcMonitor.left;
            *y = mi.rcMonitor.top;
            *w = mi.rcMonitor.right - mi.rcMonitor.left;
            *h = mi.rcMonitor.bottom - mi.rcMonitor.top;
            return;
        }
    }
    *x = 0;
    *y = 0;
    *w = GetSystemMetrics(SM_CXSCREEN);
    *h = GetSystemMetrics(SM_CYSCREEN);
}

// vk_video_create_standalone — initialise Vulkan renderer as a standalone fullscreen window.
// hint_hwnd, when non-null, picks the monitor that currently hosts the client
// window; otherwise the primary monitor is used. The window has keyboard focus
// so WM_KEYDOWN/UP are delivered for input forwarding.
// vsync: see the present-mode preference comment in vk_create_swapchain.
// Returns 1 on success, 0 on failure.
int vk_video_create_standalone(uintptr_t hint_hwnd, int vsync) {
    if (atomic_load(&g_active)) vk_full_cleanup();

    atomic_store(&g_vsync, vsync ? 1 : 0);
    int x = 0, y = 0, sw = 0, sh = 0;
    vk_monitor_rect_near_hwnd((HWND)hint_hwnd, &x, &y, &sw, &sh);
    g_parent_hwnd = NULL;
    g_standalone = 1;
    g_hwnd_args.parent = NULL;
    g_hwnd_args.x = x;
    g_hwnd_args.y = y;
    g_hwnd_args.w = sw;
    g_hwnd_args.h = sh;
    g_hwnd_args.standalone = 1;

    if (!vk_video_init_common(x, y, sw, sh)) return 0;

    {
        char m[256];
        VkPhysicalDeviceProperties pr;
        vkGetPhysicalDeviceProperties(g_pdev, &pr);
        snprintf(m, sizeof(m), "Vulkan standalone fullscreen created — GPU=%s origin=(%d,%d) %dx%d hint_hwnd=%p",
                 pr.deviceName, x, y, sw, sh, (void *)hint_hwnd);
        goVKLog(m, 0);
    }
    return 1;
}

void vk_video_destroy(void) {
    if (!atomic_load(&g_active)) return;
    char m[192];
    snprintf(m, sizeof(m), "Vulkan renderer destroyed — rendered=%lld submitted=%lld", g_rendered, g_submitted);
    vk_full_cleanup();
    goVKLog(m, 0);
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

// Returns render-thread heartbeat (increments every loop iteration) and current stage.
// Go watchdog: if heartbeat stops advancing the render thread is stuck; stage tells where.
void vk_video_get_diag(long long *hb, int *stage) {
    *hb    = g_render_hb;
    *stage = g_render_stage;
}

// Hide (hidden=1) or show (hidden=0) the overlay without destroying it.
// Called from Go when a Fyne menu/popup appears or disappears so the native
// overlay doesn't paint over Fyne's own UI — mirrors macOS MetalVideoSetHidden.
void vk_video_set_hidden(int hidden) {
    atomic_store(&g_hidden, hidden ? 1 : 0);
}

// vk_video_bring_to_top — re-assert HWND_TOPMOST on the overlay window.
// Not needed in standalone mode (there's no competing Fyne window).
void vk_video_bring_to_top(void) {
    if (g_standalone) return;
    HWND hw = g_child_hwnd;
    if (hw) {
        int active = (int)atomic_load(&g_active);
        char m[96];
        snprintf(m, sizeof(m), "vk_video_bring_to_top: re-asserting HWND_TOPMOST (sync, active=%d)", active);
        goVKLog(m, 0);
        SetWindowPos(hw, HWND_TOPMOST, 0, 0, 0, 0,
                     SWP_NOMOVE | SWP_NOSIZE | SWP_NOACTIVATE);
    } else {
        goVKLog((char*)"vk_video_bring_to_top: no-op (hw=NULL)", 1);
    }
}

// vk_video_next_event — drain one pending pointer event from the overlay window.
// Returns 1 if an event was consumed; type values:
//   1 = mouse move  2 = button/scroll press  3 = button release
// Buttons: 1=left 2=middle 3=right 4=wheel-up 5=wheel-down.
// Thread-safe; called from the Go polling goroutine.
int vk_video_next_event(int *type_out, int *x_out, int *y_out, int *btn_out) {
    *type_out = 0;
    if (!g_eq_cs_init || !atomic_load(&g_active)) return 0;
    EnterCriticalSection(&g_eq_cs);
    if (g_eq_tail == g_eq_head) {
        LeaveCriticalSection(&g_eq_cs);
        return 0;
    }
    *type_out = g_eq[g_eq_tail].type;
    *x_out    = g_eq[g_eq_tail].x;
    *y_out    = g_eq[g_eq_tail].y;
    *btn_out  = g_eq[g_eq_tail].btn;
    g_eq_tail = (g_eq_tail + 1) % VK_EQ_CAP;
    LeaveCriticalSection(&g_eq_cs);
    return 1;
}

// vk_video_get_dst_size — return the current overlay/standalone window dimensions in pixels.
// In standalone mode this equals the chosen monitor's resolution.
// In overlay mode this is the video rect passed to vk_video_create / vk_video_update_frame.
void vk_video_get_dst_size(int *w, int *h) {
    *w = atomic_load(&g_dst_w);
    *h = atomic_load(&g_dst_h);
}

// vk_video_next_key_event — drain one pending keyboard event (standalone mode only).
// Returns 1 if an event was consumed. type: 1=keydown, 2=keyup.
// vk_out receives the Win32 Virtual Key code (same values Moonlight expects).
// Thread-safe; called from the Go key-forwarding goroutine.
int vk_video_next_key_event(int *type_out, int *vk_out) {
    *type_out = 0; *vk_out = 0;
    if (!g_kq_cs_init || !atomic_load(&g_active)) return 0;
    EnterCriticalSection(&g_kq_cs);
    if (g_kq_tail == g_kq_head) {
        LeaveCriticalSection(&g_kq_cs);
        return 0;
    }
    *type_out = g_kq[g_kq_tail].type;
    *vk_out   = g_kq[g_kq_tail].vk_code;
    g_kq_tail = (g_kq_tail + 1) % VK_KQ_CAP;
    LeaveCriticalSection(&g_kq_cs);
    return 1;
}

#endif // _WIN32
