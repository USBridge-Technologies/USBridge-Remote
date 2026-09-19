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
#include "fsr_arrays.h"
#include <vulkan/vulkan_win32.h>
#include <stdint.h>
#include <string.h>
#include <stdlib.h>
#include <stdio.h>
#include <stdatomic.h>
#include <math.h>

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

static VkDescriptorSetLayout g_fsr_dsl = VK_NULL_HANDLE;
static VkPipelineLayout g_fsr_playout = VK_NULL_HANDLE;
static VkPipeline g_fsr_easu_pipeline = VK_NULL_HANDLE;
static VkPipeline g_fsr_rcas_pipeline = VK_NULL_HANDLE;
static VkDescriptorSet g_fsr_easu_dset = VK_NULL_HANDLE;
static VkDescriptorSet g_fsr_rcas_dset = VK_NULL_HANDLE;
static VkImage g_fsr_easu_tex = VK_NULL_HANDLE;
static VkImageView g_fsr_easu_view = VK_NULL_HANDLE;
static VkDeviceMemory g_fsr_easu_mem = VK_NULL_HANDLE;
static VkImage g_fsr_rcas_tex = VK_NULL_HANDLE;
static VkImageView g_fsr_rcas_view = VK_NULL_HANDLE;
static VkDeviceMemory g_fsr_rcas_mem = VK_NULL_HANDLE;
static int g_fsr_w = 0, g_fsr_h = 0;
int g_enable_fsr = 0;

void vk_set_fsr(int enable) {
    g_enable_fsr = enable;
}

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

// ─── Net Graph HUD overlay ──────────────────────────────────────────────────
// A small (VK_HUD_W x VK_HUD_H) RGBA texture holding the most recently pushed
// HUD canvas (net_graph.go's netGraphCachedImg, ~10Hz), drawn as a second,
// alpha-blended draw call after the video frame in the SAME dynamic-rendering
// pass -- see vk_hud_ensure_resources/vk_hud_maybe_upload/vk_hud_record_draw.
// This exists so hardware Vulkan Video Decode's zero-copy path (
// vk_render_frame_vkimage) can show the HUD without ever reading the actual
// video frame back to the CPU -- only this tiny texture is CPU-uploaded, and
// only on the ~10Hz ticks where net_graph.go actually rebuilds it, not once
// per rendered video frame. VK_HUD_W/H/MARGIN must match net_graph.go's
// netGraphCanvasW/netGraphCanvasH/netGraphHudMargin exactly -- there is no
// shared constant across the Go/C boundary, so keep them in sync by hand.
#define VK_HUD_W      640
#define VK_HUD_H      400
#define VK_HUD_MARGIN 24

static VkPipeline            g_hud_pipeline  = VK_NULL_HANDLE;
static VkPipelineLayout      g_hud_playout   = VK_NULL_HANDLE;
static VkDescriptorSetLayout g_hud_dsl       = VK_NULL_HANDLE;
static VkDescriptorPool      g_hud_dpool     = VK_NULL_HANDLE;
static VkDescriptorSet       g_hud_dset      = VK_NULL_HANDLE;
static VkSampler             g_hud_sampler   = VK_NULL_HANDLE;
static int                   g_hud_resources_ok = 0; // 1 once the above are created, -1 if creation failed (don't retry)

static VkImage        g_hud_tex        = VK_NULL_HANDLE;
static VkDeviceMemory g_hud_tex_mem    = VK_NULL_HANDLE;
static VkImageView    g_hud_tex_view   = VK_NULL_HANDLE;
static VkImageLayout  g_hud_tex_layout = VK_IMAGE_LAYOUT_UNDEFINED;

// Tiny dedicated staging buffer (persistently mapped, host-visible+coherent) --
// separate from g_stage_buf above since that one is sized for full video
// frames and reused/resized per-resolution, while this one is a fixed
// VK_HUD_W*VK_HUD_H*4 bytes for the life of the process.
static VkBuffer       g_hud_stage_buf  = VK_NULL_HANDLE;
static VkDeviceMemory g_hud_stage_mem  = VK_NULL_HANDLE;
static void          *g_hud_stage_ptr  = NULL;

// Cross-thread handoff (Go's net_graph.go tick goroutine -> this render
// thread), protected by g_cs like g_buf/g_ready above. g_hud_active mirrors
// the checkbox state (set on push, cleared on vk_hud_clear) so the render
// thread knows whether to draw at all; g_hud_dirty means g_hud_pixels holds
// data not yet uploaded to g_hud_tex.
static uint8_t        g_hud_pixels[VK_HUD_W * VK_HUD_H * 4];
static volatile int   g_hud_dirty  = 0;
static volatile int   g_hud_active = 0;

// ─── AI Vision overlay: full-frame-sized counterpart to the HUD above ──────
// Same shared pipeline/sampler/layout (see vk_hud_ensure_resources), but its
// own descriptor set + texture, resized to match the live decode resolution
// (fw x fh) rather than fixed -- ai_vision.go's detection boxes are
// positioned in that same pixel space, so the overlay has to cover it
// exactly for boxes to land on the right spot.
static VkDescriptorSet g_aivision_dset      = VK_NULL_HANDLE; // allocated alongside g_hud_dset in vk_hud_ensure_resources
static VkImage         g_aivision_tex       = VK_NULL_HANDLE;
static VkDeviceMemory  g_aivision_tex_mem   = VK_NULL_HANDLE;
static VkImageView     g_aivision_tex_view  = VK_NULL_HANDLE;
static VkImageLayout   g_aivision_tex_layout = VK_IMAGE_LAYOUT_UNDEFINED;
static int             g_aivision_tex_w = 0, g_aivision_tex_h = 0;

// Staging buffer, resized on demand like g_stage_buf (full video frames can
// be large, unlike the HUD's fixed small canvas, so this isn't a fixed-size
// static array).
static VkBuffer       g_aivision_stage_buf = VK_NULL_HANDLE;
static VkDeviceMemory g_aivision_stage_mem = VK_NULL_HANDLE;
static void          *g_aivision_stage_ptr = NULL;
static VkDeviceSize   g_aivision_stage_sz  = 0;

// Cross-thread handoff (ai_vision.go's detection goroutines, via
// pushAIVisionOverlayToVulkan -> this render thread), protected by g_cs like
// g_hud_pixels above. Heap-allocated and resized on demand rather than a
// fixed array for the same reason as the staging buffer.
static uint8_t        *g_aivision_pixels   = NULL;
static size_t          g_aivision_pixels_sz = 0;
static int             g_aivision_pending_w = 0, g_aivision_pending_h = 0;
static volatile int    g_aivision_dirty  = 0;
static volatile int    g_aivision_active = 0;

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

// ─── frame smoothing (motion-extrapolated stall concealment) ────────────────
// Opt-in fallback, RGBA CPU-submit path only (g_frame_mode == VK_FRAME_MODE_RGBA):
// when the network stalls and the next real decoded frame is late, instead of
// leaving the display frozen on the last real frame, motion-extrapolate a
// synthetic one from the last two real frames and present that instead until
// the real frame arrives. See frame_smoothing.go's doc comment for the full
// design and internal/service's approved plan doc for the architecture. Not
// wired into the zero-copy VkImage decode path (vk_render_frame_vkimage) --
// those frames are YCbCr, not RGBA; sampling them from a compute shader needs
// its own YCbCr-aware plumbing, a deliberate follow-up, not attempted here.
//
// All *decision* math (is this gap long enough to conceal, how far forward to
// extrapolate) lives in Go (frame_smoothing.go) and is unit tested there; this
// file only executes the decision via the goFrameSmoothingDecide/
// goFrameSmoothingUpdateInterval cgo exports (frame_smoothing_windows.go),
// mirroring the existing goVKLog pattern.
extern int    goFrameSmoothingDecide(double elapsedMs, double expectedMs, int consecutive, float *extrapolateTOut);
extern double goFrameSmoothingUpdateInterval(double prevEma, double newIntervalMs);
extern int    goFrameSmoothingMaxConsecutive(void);
extern void   goFrameSmoothingTraceWrite(int concealed, double t, double centerVx, double centerVy,
    double wholeVx, double wholeVy, double nonzeroPct, double gapMs);

static atomic_int g_conceal_enabled = 0; // set via vk_video_set_concealment_enabled
static int vk_conceal_ensure_tex2(int w, int h); // defined below; used by vk_render_frame(_vkimage) above it
static void vk_conceal_note_real_frame(int conceal_slot); // defined below; used by vk_render_frame(_vkimage) above it
static void vk_conceal_debug_log_flow_stats(void); // DEBUG, defined below; used by vk_render_frame(_vkimage) above it too so a stall's flow stats log promptly once it ends, not just on the next stall
static void vk_conceal_maybe_log_summary(void); // defined below; used by vk_render_frame(_vkimage) above it too, same reason
static void vk_conceal_consume_prior(void); // defined below; used by vk_render_frame(_vkimage) above it too, same reason

// block-match block size, px. Was 16 -- live per-sample trace (frame
// smoothing trace log, 2026-09-19) on a small-particle VFX (sparks: each
// one a few px, moving at varying speed/direction, some fading out) showed
// nonzero% pinned near 0% throughout continuous, visible particle motion:
// at 16x16 (256px), a handful of moving spark pixels are diluted into a
// block that's otherwise unchanged background, so the block's overall SAD
// barely improves for ANY candidate offset regardless of confidence
// threshold -- a resolution problem, not a confidence problem (raising the
// confidence floor, as done for smoke, doesn't help here: the signal
// itself is too weak at this granularity). 8x8 (64px) quadruples the block
// count -- flow is still computed once per STALL not per tick, so the
// extra one-time cost per stall is affordable -- and lets a small bright
// particle actually dominate its own block's SAD instead of being diluted
// by 250+ pixels of unrelated static background around it. Does not help
// particles that are FADING (an opacity change, not a position change --
// no block-translation model can represent that) or genuinely too small to
// influence even an 8x8 block's SAD; those remain a hard limit of this
// technique, not something further tuning can fix.
#define VK_CONCEAL_BLOCK    8
// block-match search radius, px. Was 8 -- live DEBUG readback (flow field
// stats, 2026-09-19 bad-wifi session, see vk_conceal_debug_log_flow_stats)
// showed avgMag repeatedly within ~1px of maxMag, and maxMag repeatedly
// exactly at the search window's corner (12.73px = sqrt(9^2+9^2), R=8
// refined +-1) across independent stalls -- real on-screen motion (cursor,
// scroll, animation) routinely exceeds an 8px search window between two
// real frames on this connection, so the matcher was clipping at the
// window edge and returning a truncated-magnitude (but often
// direction-correct) vector, which extrapolateT then stretched into a
// visible smear in that direction ("смазывает вверх" -- consistent with a
// real, direction-correct but magnitude-clipped motion estimate). 24 gives
// real headroom; only costs more compute once per stall (flow is cached
// and reused across a stall's ticks), not per render tick.
#define VK_CONCEAL_SEARCH_R 24

// g_conceal_max_consecutive: fetched once (see vk_conceal_note_real_frame's
// first call) from goFrameSmoothingMaxConsecutive() rather than duplicated
// as a #define -- used only for the telemetry summary's "exhausted" counter
// (a stall that ran out of synthesized-frame budget before a real frame
// arrived, i.e. fell back to a visible freeze), never for decision logic
// (that stays exclusively in Go, decided via goFrameSmoothingDecide). A
// hand-maintained mirror constant here silently drifted out of sync the
// first time the Go budget was tuned (4 -> 25, 2026-09-19), making
// "exhausted" measure a stale threshold -- fetching the real value at
// runtime makes that class of bug impossible.
static int g_conceal_max_consecutive = 0; // 0 = not yet fetched

// Dual real-frame textures (sampled — separate from g_tex, which is
// blit-only/no SAMPLED usage, so the happy path with concealment disabled
// never allocates or touches any of this). [g_conceal_cur] holds the most
// recently rendered real frame; the other slot holds the one before it.
static VkImage        g_conceal_tex[2];
static VkDeviceMemory g_conceal_tex_mem[2];
static VkImageView    g_conceal_tex_view[2];
static VkImageLayout  g_conceal_tex_layout[2] = { VK_IMAGE_LAYOUT_UNDEFINED, VK_IMAGE_LAYOUT_UNDEFINED };
static int            g_conceal_tex_w = 0, g_conceal_tex_h = 0;
static int            g_conceal_cur = 0;
static int            g_conceal_have_prev = 0;    // 1 once 2 real frames have been captured
static int            g_conceal_capture_count = 0; // capped at 2; see its use in vk_render_frame

// Flow field (block-grid resolution, R16G16_SFLOAT) and synthesized frame
// (full resolution, R8G8B8A8_UNORM) — both kept in VK_IMAGE_LAYOUT_GENERAL
// except g_synth_tex's brief excursion to TRANSFER_SRC_OPTIMAL for the
// present blit (see vk_render_frame_conceal).
static VkImage        g_flow_tex;
static VkDeviceMemory g_flow_mem;
static VkImageView    g_flow_view;
static int            g_flow_w = 0, g_flow_h = 0;

// DEBUG (temporary -- added to diagnose the "smeared/jumping noise on
// concealed frames" report on a lossy connection, 2026-09-19): host-visible
// readback of g_flow_tex so vk_conceal_debug_log_flow_stats can log actual
// per-block motion-vector statistics once per stall, instead of guessing
// blind from the "concealing a stall" gap/expected/t line alone. See its
// call sites in vk_render_frame_conceal.
static VkBuffer       g_flow_dbg_buf = VK_NULL_HANDLE;
static VkDeviceMemory g_flow_dbg_mem = VK_NULL_HANDLE;
static VkDeviceSize   g_flow_dbg_sz  = 0;
static int            g_flow_dbg_pending = 0; // flow readback queued, not yet logged
static int            g_flow_dbg_grid_w = 0, g_flow_dbg_grid_h = 0;
static float          g_flow_dbg_t = 0.0f; // extrapolateT at the time flow was (re)computed
static double         g_flow_dbg_next_allowed_ts = 0.0; // throttle: don't queue a new readback before this

// Latest flow-field sample, folded into the periodic summary instead of its
// own log line -- see vk_conceal_debug_log_flow_stats/vk_conceal_maybe_log_summary.
static int    g_flow_dbg_have_sample = 0;
static double g_flow_dbg_sample_nonzero_pct = 0.0;
static int    g_flow_dbg_sample_nonzero_count = 0, g_flow_dbg_sample_total_blocks = 0;
static double g_flow_dbg_sample_avg_mag = 0.0;
static double g_flow_dbg_sample_max_mag = 0.0;
// Signed average vector (not just magnitude) among nonzero blocks, plus the
// PREVIOUS sample's signed average and a running same-direction/flip
// tally across consecutive throttled samples -- added to directly check a
// reported "micro-jitter in 2 directions" complaint: magnitude-only stats
// can't distinguish a flow field that's consistently pointing one way
// (real, coherent motion) from one that's flip-flopping direction between
// samples (noise/instability, which would look exactly like the reported
// back-and-forth jitter once stretched by extrapolateT). See
// vk_conceal_maybe_log_summary's "dirFlips" output.
static double g_flow_dbg_sample_avg_vx = 0.0, g_flow_dbg_sample_avg_vy = 0.0;
static int    g_flow_dbg_have_prev_dir = 0;
static double g_flow_dbg_prev_avg_vx = 0.0, g_flow_dbg_prev_avg_vy = 0.0;
static long long g_conceal_summary_dir_samples = 0; // throttled samples compared since last summary
static long long g_conceal_summary_dir_flips   = 0; // of those, how many reversed direction (dot product < 0) from the previous one

// Temporal-direction prior: fed INTO flow_blockmatch.comp as a push
// constant (g_conceal_prior_vx/vy) so it can break ties between two
// candidates with near-identical SAD but OPPOSITE sign (my earlier
// "prefer closer to zero" tie-break can't discriminate these -- they're
// equally close). Classic case: a periodic/tiled background texture (this
// app's actual content includes streamed games) where true motion +d and
// its alias -d (or +d minus a full tile period) score almost identically,
// so the search flip-flops between them stall to stall purely from
// floating-point noise -- reported live 2026-09-19 as the game's
// background visibly jittering back-and-forth while the character (a
// unique, non-repeating shape with no alias to confuse it) tracked
// correctly. Fed from a SMALL (VK_CONCEAL_PRIOR_CROP^2 texel) center-crop
// readback of the flow field, queued every stall (not throttled like the
// full-grid debug sample -- see g_flow_dbg_buf's doc comment for why THAT
// one is throttled; this one is ~100x smaller so the same cost concern
// doesn't apply) and consumed on the following render call once the fence
// proves it's safe to read, same pattern as g_flow_dbg_pending.
#define VK_CONCEAL_PRIOR_CROP 8
static VkBuffer       g_flow_prior_buf = VK_NULL_HANDLE;
static VkDeviceMemory g_flow_prior_mem = VK_NULL_HANDLE;
static int            g_flow_prior_pending = 0;
static double         g_conceal_prior_vx = 0.0, g_conceal_prior_vy = 0.0;

static VkImage        g_synth_tex;
static VkDeviceMemory g_synth_mem;
static VkImageView    g_synth_view;
static VkImageLayout  g_synth_layout = VK_IMAGE_LAYOUT_UNDEFINED;

static VkSampler             g_conceal_sampler = VK_NULL_HANDLE; // linear, clamp-to-edge
static VkDescriptorSetLayout g_flow_dsl = VK_NULL_HANDLE, g_warp_dsl = VK_NULL_HANDLE;
static VkPipelineLayout      g_flow_playout = VK_NULL_HANDLE, g_warp_playout = VK_NULL_HANDLE;
static VkPipeline            g_flow_pipeline = VK_NULL_HANDLE, g_warp_pipeline = VK_NULL_HANDLE;
static VkDescriptorPool      g_conceal_dpool = VK_NULL_HANDLE;
static VkDescriptorSet       g_flow_dset = VK_NULL_HANDLE, g_warp_dset = VK_NULL_HANDLE;
static int                   g_conceal_pipelines_ok = 0; // 0=not tried 1=ok -1=failed permanently

// Stall bookkeeping — render thread only, no lock needed (matches
// g_render_stage/g_render_hb's own single-writer discipline).
static double g_conceal_last_real_ts = 0.0; // mono_sec() of the last real frame render
static double g_conceal_expected_ms  = 0.0; // rolling EMA, updated via goFrameSmoothingUpdateInterval
static int    g_conceal_consecutive  = 0;   // consecutive synthesized frames this stall
static int    g_conceal_flow_fresh   = 0;   // flow already computed for the current stall?

static volatile long long g_stat_concealed_frames = 0;
static volatile int       g_stat_concealing = 0;

// Quality/telemetry summary (temporary, added 2026-09-19 to debug reported
// smearing + a "jitter grows when frame smoothing is on" regression on a
// second, otherwise-healthy connection): every real and every concealed
// frame updates cheap CPU-only counters below -- no GPU work, so this part
// costs nothing worth measuring even at full frame rate. A periodic summary
// (vk_conceal_maybe_log_summary, gated to ~every 2s so app.log isn't
// flooded) is the only place any of it gets logged. This replaced an
// earlier version of this diagnostic that did a full GPU readback of the
// flow field and logged it on EVERY stall onset -- on a bad connection
// stalls can fire several times a second, and that readback + its CPU-side
// scan ran synchronously on the render thread, right when the network was
// already struggling -- a very plausible contributor to the reported
// jitter-while-enabled regression on the second connection. The GPU
// readback still exists for when per-block detail is actually needed, but
// is now throttled to the same ~2s cadence as everything else (see
// g_flow_dbg_next_allowed_ts).
static double     g_conceal_summary_last_ts    = 0.0;
static long long  g_conceal_summary_real       = 0; // real frames rendered since last summary
static long long  g_conceal_summary_concealed  = 0; // concealed (synthesized) frames since last summary
static long long  g_conceal_summary_exhausted  = 0; // stalls that hit frameSmoothingMaxConsecutive before a real frame arrived (visible freeze, not just a smoothed gap)
static double     g_conceal_summary_t_sum      = 0.0;
static float      g_conceal_summary_t_max      = 0.0f;
// Presentation-timing telemetry (gap between consecutive actual presents,
// real or concealed) -- separate from flow-content correctness above.
// Requested live 2026-09-19 to directly measure micro-freezes as gaps in
// presentation timing, not infer smoothness from flow-field stats (correct
// flow content doesn't guarantee steady presentation cadence).
static long long  g_conceal_summary_gap_count     = 0;
static double     g_conceal_summary_gap_sum       = 0.0;
static double     g_conceal_summary_gap_max       = 0.0;
static long long  g_conceal_summary_stutter_count = 0; // gaps exceeding the adaptive threshold, see the g_last_blit_ts update site
// Of the stutters above, how many happened between two CONCEALED presents
// within the same active stall (both sides of the gap were synthesized,
// consecutive ticks of one ongoing stall) -- distinguishes "something in
// this render path is itself periodically slow" (high count here) from
// "the network/real-frame arrival is just jittery" (stutters mostly NOT
// concealed-to-concealed, e.g. around a real frame boundary instead).
// g_last_present_was_concealed tracks which case the CURRENT gap is; set
// at the bottom of vk_render_frame(_vkimage)/vk_render_frame_conceal,
// read (before being overwritten) at the same g_last_blit_ts gap-check
// site that counts stutters.
static long long  g_conceal_summary_stutter_concealed_to_concealed = 0;
static int        g_last_present_was_concealed = 0;
static double     g_last_present_t = -1.0; // extrapolateT of the tick about to be stats-logged; -1 = real frame (not applicable). Set in vk_render_frame_conceal on success, reset to -1 by vk_conceal_note_real_frame (every real frame).
#define VK_CONCEAL_SUMMARY_INTERVAL_SEC 2.0
// How often the flow field is actually READ BACK from the GPU, separate
// from how often the aggregate line gets LOGGED (VK_CONCEAL_SUMMARY_INTERVAL_SEC
// above). These used to share one timer/constant, which meant testing a
// "does direction flip between adjacent stalls" hypothesis (reported live,
// 2026-09-19: "micro-jitter in 2 directions") against 2s-apart samples --
// far enough apart that a flip could just as easily be real content change
// (a character reversing course) as estimation instability. 0.5s still
// keeps the render-thread readback cost an order of magnitude rarer than
// "every stall" (which can fire several times a second on a bad
// connection -- the exact cost that caused a previous "jitter grows when
// frame smoothing is on" regression), while being dense enough to catch
// several samples between most real stalls' natural spacing.
#define VK_FLOW_DBG_SAMPLE_INTERVAL_SEC 0.15

// Per-sample trace ring buffer: unlike g_flow_dbg_sample_* above (which
// only ever holds the LATEST throttled sample, overwritten each time), this
// keeps every sample taken since the last periodic log dump, printed as ONE
// consolidated line in vk_conceal_maybe_log_summary instead of one line per
// sample -- "add markers you can see every frame and record a few seconds
// without flooding your own log" (requested live, 2026-09-19, to
// investigate a hard case for block-matching: smoke -- constantly
// deforming/non-rigid content with no rigid shape to translate, reported
// moving in jerks even after the confidence-blend fix). 0.15s sampling
// (VK_FLOW_DBG_SAMPLE_INTERVAL_SEC above, down from 0.5s) over a 2s dump
// window gives ~13 samples per line -- a real per-sample trace, still
// ~7-10x rarer than "every stall" so the render-thread readback cost stays
// well clear of the regression that throttling was originally added to fix.
#define VK_FLOW_TRACE_CAP 16
typedef struct {
    float t;            // extrapolateT at capture time
    float nonzeroPct;
    int   nonzeroCount, totalBlocks; // raw counts -- nonzeroPct alone rounds
                                      // to "0%" at 1 decimal once block count
                                      // is large (e.g. 8px blocks ~32000
                                      // total), hiding a real but small
                                      // nonzero count. Added 2026-09-19
                                      // while investigating small-particle
                                      // (sparks) tracking specifically
                                      // because of that ambiguity.
    float avgMag, maxMag;
    float vx, vy;        // signed average vector among nonzero blocks (WHOLE frame)
    float centerVx, centerVy; // signed average vector from JUST the center
                              // VK_CONCEAL_PRIOR_CROP crop (reuses the
                              // existing temporal-prior sampler, g_conceal_prior_vx/vy
                              // -- see its doc comment) -- requested live
                              // 2026-09-19: a whole-frame average can't
                              // show a wrong/mismatched frame landing
                              // specifically in the middle of the screen,
                              // since it's diluted by everything else;
                              // timing (gap/stutter) telemetry alone can't
                              // show it either, since a wrong-content frame
                              // can still land exactly on schedule.
} VkFlowTraceEntry;
static VkFlowTraceEntry g_flow_trace[VK_FLOW_TRACE_CAP];
static int              g_flow_trace_count = 0; // clamps at VK_FLOW_TRACE_CAP -- newest samples win, see push site

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
    int conceal = atomic_load(&g_conceal_enabled);
    if (!have_pm && !conceal) {
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

// ─── Net Graph HUD + AI Vision overlay pipeline + textures ───────────────────
// Both overlays are "a textured quad, alpha-blended over whatever the video
// draw already wrote into the swapchain image, position given via push
// constant" -- identical shaders/pipeline/sampler/layout, just a different
// descriptor set (and texture behind it) per overlay. vk_hud_ensure_resources
// creates the shared pipeline bits plus the HUD's own fixed-size texture;
// vk_aivision_ensure_tex (further down) creates/resizes AI Vision's texture,
// sized to the live video resolution rather than fixed.

// vk_hud_ensure_resources lazily creates the sampler, descriptor set layout,
// alpha-blended pipeline and descriptor pool shared by both overlays, plus
// this HUD's own persistent VK_HUD_W x VK_HUD_H texture + dedicated staging
// buffer, on first use. Unlike vk_ycbcr_pipeline_get above there's only ever
// one of these (fixed format, fixed size), so no cache. Returns 1 once
// ready, 0 if creation failed (permanent -- doesn't retry).
static int vk_hud_ensure_resources(void) {
    if (g_hud_resources_ok) return g_hud_resources_ok > 0;

    VkSamplerCreateInfo sampCI = { VK_STRUCTURE_TYPE_SAMPLER_CREATE_INFO };
    sampCI.magFilter = VK_FILTER_LINEAR;
    sampCI.minFilter = VK_FILTER_LINEAR;
    sampCI.addressModeU = VK_SAMPLER_ADDRESS_MODE_CLAMP_TO_EDGE;
    sampCI.addressModeV = VK_SAMPLER_ADDRESS_MODE_CLAMP_TO_EDGE;
    sampCI.addressModeW = VK_SAMPLER_ADDRESS_MODE_CLAMP_TO_EDGE;
    if (vkCreateSampler(g_dev, &sampCI, NULL, &g_hud_sampler) != VK_SUCCESS) goto fail;

    {
        VkDescriptorSetLayoutBinding binding = {0};
        binding.binding = 0;
        binding.descriptorType = VK_DESCRIPTOR_TYPE_COMBINED_IMAGE_SAMPLER;
        binding.descriptorCount = 1;
        binding.stageFlags = VK_SHADER_STAGE_FRAGMENT_BIT;
        binding.pImmutableSamplers = &g_hud_sampler;
        VkDescriptorSetLayoutCreateInfo dslCI = { VK_STRUCTURE_TYPE_DESCRIPTOR_SET_LAYOUT_CREATE_INFO };
        dslCI.bindingCount = 1; dslCI.pBindings = &binding;
        if (vkCreateDescriptorSetLayout(g_dev, &dslCI, NULL, &g_hud_dsl) != VK_SUCCESS) goto fail;
    }

    {
        VkPushConstantRange pcr = { VK_SHADER_STAGE_VERTEX_BIT, 0, sizeof(float) * 4 };
        VkPipelineLayoutCreateInfo plCI = { VK_STRUCTURE_TYPE_PIPELINE_LAYOUT_CREATE_INFO };
        plCI.setLayoutCount = 1; plCI.pSetLayouts = &g_hud_dsl;
        plCI.pushConstantRangeCount = 1; plCI.pPushConstantRanges = &pcr;
        if (vkCreatePipelineLayout(g_dev, &plCI, NULL, &g_hud_playout) != VK_SUCCESS) goto fail;
    }

    {
        // Sized for 2 sets: this HUD's own (below) and AI Vision's
        // (g_aivision_dset, allocated once its texture is first created in
        // vk_aivision_ensure_tex) -- both reuse this same sampler/layout/
        // pipeline/pool since it's just "textured, alpha-blended quad" with
        // no per-overlay fixed state, only a different descriptor set +
        // push-constant rect per draw call.
        VkDescriptorPoolSize poolSize = { VK_DESCRIPTOR_TYPE_COMBINED_IMAGE_SAMPLER, 2 };
        VkDescriptorPoolCreateInfo poolCI = { VK_STRUCTURE_TYPE_DESCRIPTOR_POOL_CREATE_INFO };
        poolCI.maxSets = 2; poolCI.poolSizeCount = 1; poolCI.pPoolSizes = &poolSize;
        if (vkCreateDescriptorPool(g_dev, &poolCI, NULL, &g_hud_dpool) != VK_SUCCESS) goto fail;
        VkDescriptorSetAllocateInfo dsai = { VK_STRUCTURE_TYPE_DESCRIPTOR_SET_ALLOCATE_INFO };
        dsai.descriptorPool = g_hud_dpool; dsai.descriptorSetCount = 1; dsai.pSetLayouts = &g_hud_dsl;
        if (vkAllocateDescriptorSets(g_dev, &dsai, &g_hud_dset) != VK_SUCCESS) goto fail;
        if (vkAllocateDescriptorSets(g_dev, &dsai, &g_aivision_dset) != VK_SUCCESS) goto fail;
    }

    // Pipeline: a small positioned quad, alpha-blended over whatever the
    // video draw already wrote into the swapchain image -- unlike
    // vk_ycbcr_pipeline_get's opaque fullscreen-triangle video pipeline,
    // blendEnable is on here and the quad's screen position comes from a
    // push constant (see vk_hud_record_draw) rather than being fixed.
    {
        VkShaderModule vs = vk_shader_from_spv(g_hud_vert_spv, sizeof(g_hud_vert_spv));
        VkShaderModule fs = vk_shader_from_spv(g_hud_frag_spv, sizeof(g_hud_frag_spv));
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
        cba.blendEnable = VK_TRUE;
        cba.srcColorBlendFactor = VK_BLEND_FACTOR_SRC_ALPHA;
        cba.dstColorBlendFactor = VK_BLEND_FACTOR_ONE_MINUS_SRC_ALPHA;
        cba.colorBlendOp = VK_BLEND_OP_ADD;
        cba.srcAlphaBlendFactor = VK_BLEND_FACTOR_ONE;
        cba.dstAlphaBlendFactor = VK_BLEND_FACTOR_ONE_MINUS_SRC_ALPHA;
        cba.alphaBlendOp = VK_BLEND_OP_ADD;
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
        pipeCI.layout = g_hud_playout;
        VkResult pr = vkCreateGraphicsPipelines(g_dev, VK_NULL_HANDLE, 1, &pipeCI, NULL, &g_hud_pipeline);
        vkDestroyShaderModule(g_dev, vs, NULL);
        vkDestroyShaderModule(g_dev, fs, NULL);
        if (pr != VK_SUCCESS) goto fail;
    }

    // Persistent HUD texture (device-local, sampled + transfer dst).
    {
        VkImageCreateInfo ici = { VK_STRUCTURE_TYPE_IMAGE_CREATE_INFO };
        ici.imageType = VK_IMAGE_TYPE_2D;
        ici.format = VK_FORMAT_R8G8B8A8_UNORM;
        ici.extent = (VkExtent3D){ VK_HUD_W, VK_HUD_H, 1 };
        ici.mipLevels = 1; ici.arrayLayers = 1;
        ici.samples = VK_SAMPLE_COUNT_1_BIT;
        ici.tiling = VK_IMAGE_TILING_OPTIMAL;
        ici.usage = VK_IMAGE_USAGE_SAMPLED_BIT | VK_IMAGE_USAGE_TRANSFER_DST_BIT;
        ici.initialLayout = VK_IMAGE_LAYOUT_UNDEFINED;
        if (vkCreateImage(g_dev, &ici, NULL, &g_hud_tex) != VK_SUCCESS) goto fail;

        VkMemoryRequirements mr;
        vkGetImageMemoryRequirements(g_dev, g_hud_tex, &mr);
        VkPhysicalDeviceMemoryProperties mp;
        vkGetPhysicalDeviceMemoryProperties(g_pdev, &mp);
        uint32_t mi = vk_find_mem(&mp, mr.memoryTypeBits, VK_MEMORY_PROPERTY_DEVICE_LOCAL_BIT);
        if (mi == UINT32_MAX) goto fail;
        VkMemoryAllocateInfo mai = { VK_STRUCTURE_TYPE_MEMORY_ALLOCATE_INFO };
        mai.allocationSize = mr.size; mai.memoryTypeIndex = mi;
        if (vkAllocateMemory(g_dev, &mai, NULL, &g_hud_tex_mem) != VK_SUCCESS) goto fail;
        vkBindImageMemory(g_dev, g_hud_tex, g_hud_tex_mem, 0);

        VkImageViewCreateInfo vci = { VK_STRUCTURE_TYPE_IMAGE_VIEW_CREATE_INFO };
        vci.image = g_hud_tex; vci.viewType = VK_IMAGE_VIEW_TYPE_2D; vci.format = VK_FORMAT_R8G8B8A8_UNORM;
        vci.subresourceRange.aspectMask = VK_IMAGE_ASPECT_COLOR_BIT;
        vci.subresourceRange.levelCount = 1; vci.subresourceRange.layerCount = 1;
        if (vkCreateImageView(g_dev, &vci, NULL, &g_hud_tex_view) != VK_SUCCESS) goto fail;

        VkDescriptorImageInfo imgInfo = { VK_NULL_HANDLE, g_hud_tex_view, VK_IMAGE_LAYOUT_SHADER_READ_ONLY_OPTIMAL };
        VkWriteDescriptorSet write = { VK_STRUCTURE_TYPE_WRITE_DESCRIPTOR_SET };
        write.dstSet = g_hud_dset; write.dstBinding = 0; write.descriptorCount = 1;
        write.descriptorType = VK_DESCRIPTOR_TYPE_COMBINED_IMAGE_SAMPLER;
        write.pImageInfo = &imgInfo;
        vkUpdateDescriptorSets(g_dev, 1, &write, 0, NULL);
    }

    // Dedicated staging buffer (separate from g_stage_buf, which is sized
    // for full video frames and resized per-resolution) -- fixed size,
    // persistently mapped, for the life of the process.
    {
        VkDeviceSize sz = (VkDeviceSize)(VK_HUD_W * VK_HUD_H * 4);
        VkBufferCreateInfo bci = { VK_STRUCTURE_TYPE_BUFFER_CREATE_INFO };
        bci.size = sz; bci.usage = VK_BUFFER_USAGE_TRANSFER_SRC_BIT; bci.sharingMode = VK_SHARING_MODE_EXCLUSIVE;
        if (vkCreateBuffer(g_dev, &bci, NULL, &g_hud_stage_buf) != VK_SUCCESS) goto fail;

        VkMemoryRequirements mr;
        vkGetBufferMemoryRequirements(g_dev, g_hud_stage_buf, &mr);
        VkPhysicalDeviceMemoryProperties mp;
        vkGetPhysicalDeviceMemoryProperties(g_pdev, &mp);
        uint32_t mi = vk_find_mem(&mp, mr.memoryTypeBits,
            VK_MEMORY_PROPERTY_HOST_VISIBLE_BIT | VK_MEMORY_PROPERTY_HOST_COHERENT_BIT);
        if (mi == UINT32_MAX) goto fail;
        VkMemoryAllocateInfo mai = { VK_STRUCTURE_TYPE_MEMORY_ALLOCATE_INFO };
        mai.allocationSize = mr.size; mai.memoryTypeIndex = mi;
        if (vkAllocateMemory(g_dev, &mai, NULL, &g_hud_stage_mem) != VK_SUCCESS) goto fail;
        vkBindBufferMemory(g_dev, g_hud_stage_buf, g_hud_stage_mem, 0);
        vkMapMemory(g_dev, g_hud_stage_mem, 0, VK_WHOLE_SIZE, 0, &g_hud_stage_ptr);
    }

    g_hud_resources_ok = 1;
    return 1;

fail:
    goVKLog("vk_hud_ensure_resources: failed -- Net Graph HUD will not render", 2);
    g_hud_resources_ok = -1;
    return 0;
}

// vk_hud_maybe_upload_cmds checks (under g_cs) whether net_graph.go pushed a
// new HUD canvas since the last upload and, if so, records a buffer->image
// copy for it into cb. Must be called after vkBeginCommandBuffer and before
// the render pass begins (a transfer isn't valid inside vkCmdBeginRendering).
// Cheap in the common case: one short-held-lock flag check, no GPU work
// recorded at all when nothing changed since the last frame -- true on
// nearly every call, since net_graph.go only pushes at ~10Hz while this runs
// at the video's own frame rate.
static void vk_hud_maybe_upload_cmds(VkCommandBuffer cb) {
    if (!vk_hud_ensure_resources()) return;
    if (!g_cs_init) return;

    int have_new = 0;
    EnterCriticalSection(&g_cs);
    if (g_hud_dirty) {
        memcpy(g_hud_stage_ptr, g_hud_pixels, sizeof(g_hud_pixels));
        g_hud_dirty = 0;
        have_new = 1;
    }
    LeaveCriticalSection(&g_cs);
    if (!have_new) return;

    VkImageLayout old_layout = g_hud_tex_layout;
    vk_image_barrier(cb, g_hud_tex, old_layout, VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL,
        old_layout == VK_IMAGE_LAYOUT_UNDEFINED ? 0 : VK_ACCESS_SHADER_READ_BIT,
        VK_ACCESS_TRANSFER_WRITE_BIT,
        old_layout == VK_IMAGE_LAYOUT_UNDEFINED ? VK_PIPELINE_STAGE_TOP_OF_PIPE_BIT : VK_PIPELINE_STAGE_FRAGMENT_SHADER_BIT,
        VK_PIPELINE_STAGE_TRANSFER_BIT);

    VkBufferImageCopy bic = {0};
    bic.bufferRowLength = VK_HUD_W;
    bic.bufferImageHeight = VK_HUD_H;
    bic.imageSubresource.aspectMask = VK_IMAGE_ASPECT_COLOR_BIT;
    bic.imageSubresource.layerCount = 1;
    bic.imageExtent = (VkExtent3D){ VK_HUD_W, VK_HUD_H, 1 };
    vkCmdCopyBufferToImage(cb, g_hud_stage_buf, g_hud_tex, VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL, 1, &bic);

    vk_image_barrier(cb, g_hud_tex, VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL, VK_IMAGE_LAYOUT_SHADER_READ_ONLY_OPTIMAL,
        VK_ACCESS_TRANSFER_WRITE_BIT, VK_ACCESS_SHADER_READ_BIT,
        VK_PIPELINE_STAGE_TRANSFER_BIT, VK_PIPELINE_STAGE_FRAGMENT_SHADER_BIT);

    g_hud_tex_layout = VK_IMAGE_LAYOUT_SHADER_READ_ONLY_OPTIMAL;
}

// vk_hud_record_draw issues the HUD's alpha-blended quad draw call, anchored
// to the bottom-right of the fw x fh video frame (native decode resolution,
// NOT the on-screen letterboxed size) -- matches net_graph.go's
// netGraphBlitOverlay anchoring exactly, so the HUD sits in the same place
// relative to the picture regardless of window size. Must be called between
// pfnBeginRendering and pfnEndRendering, with a viewport/scissor already
// bound (reuses whatever the video draw just set).
static void vk_hud_record_draw(VkCommandBuffer cb, int fw, int fh) {
    if (!g_hud_active || g_hud_resources_ok <= 0) return;
    if (fw <= 0 || fh <= 0) return;

    float hx0 = (float)(fw - VK_HUD_MARGIN - VK_HUD_W);
    if (hx0 < VK_HUD_MARGIN) hx0 = (float)VK_HUD_MARGIN;
    float hy0 = (float)(fh - VK_HUD_MARGIN - VK_HUD_H);
    if (hy0 < VK_HUD_MARGIN) hy0 = (float)VK_HUD_MARGIN;
    float hx1 = hx0 + (float)VK_HUD_W;
    float hy1 = hy0 + (float)VK_HUD_H;

    float rect[4] = {
        (hx0 / (float)fw) * 2.0f - 1.0f, (hy0 / (float)fh) * 2.0f - 1.0f,
        (hx1 / (float)fw) * 2.0f - 1.0f, (hy1 / (float)fh) * 2.0f - 1.0f,
    };

    vkCmdBindPipeline(cb, VK_PIPELINE_BIND_POINT_GRAPHICS, g_hud_pipeline);
    vkCmdBindDescriptorSets(cb, VK_PIPELINE_BIND_POINT_GRAPHICS, g_hud_playout, 0, 1, &g_hud_dset, 0, NULL);
    vkCmdPushConstants(cb, g_hud_playout, VK_SHADER_STAGE_VERTEX_BIT, 0, sizeof(rect), rect);
    vkCmdDraw(cb, 6, 1, 0, 0);
}

// vk_aivision_ensure_tex (re)creates AI Vision's texture + view + descriptor
// write and its dedicated staging buffer whenever the requested size differs
// from what's currently allocated -- mirrors vk_ensure_tex/vk_ensure_staging
// above, except device-local+sampled (not transfer-src, this is never
// blitted) and using the shared overlay descriptor set/sampler from
// vk_hud_ensure_resources rather than the ycbcr/blit pipelines' own.
// Returns 1 once ready at (w, h), 0 on failure.
static int vk_aivision_ensure_tex(int w, int h) {
    if (!vk_hud_ensure_resources()) return 0; // shared sampler/dsl/pipeline/pool + g_aivision_dset's allocation
    if (g_aivision_tex != VK_NULL_HANDLE && g_aivision_tex_w == w && g_aivision_tex_h == h) return 1;

    if (g_aivision_tex != VK_NULL_HANDLE) {
        vkDeviceWaitIdle(g_dev);
        if (g_aivision_tex_view) { vkDestroyImageView(g_dev, g_aivision_tex_view, NULL); g_aivision_tex_view = VK_NULL_HANDLE; }
        vkFreeMemory(g_dev, g_aivision_tex_mem, NULL); g_aivision_tex_mem = VK_NULL_HANDLE;
        vkDestroyImage(g_dev, g_aivision_tex, NULL);   g_aivision_tex     = VK_NULL_HANDLE;
        g_aivision_tex_layout = VK_IMAGE_LAYOUT_UNDEFINED;
    }

    VkImageCreateInfo ici = { VK_STRUCTURE_TYPE_IMAGE_CREATE_INFO };
    ici.imageType = VK_IMAGE_TYPE_2D;
    ici.format = VK_FORMAT_R8G8B8A8_UNORM;
    ici.extent = (VkExtent3D){ (uint32_t)w, (uint32_t)h, 1 };
    ici.mipLevels = 1; ici.arrayLayers = 1;
    ici.samples = VK_SAMPLE_COUNT_1_BIT;
    ici.tiling = VK_IMAGE_TILING_OPTIMAL;
    ici.usage = VK_IMAGE_USAGE_SAMPLED_BIT | VK_IMAGE_USAGE_TRANSFER_DST_BIT;
    ici.initialLayout = VK_IMAGE_LAYOUT_UNDEFINED;
    if (vkCreateImage(g_dev, &ici, NULL, &g_aivision_tex) != VK_SUCCESS) return 0;

    VkMemoryRequirements mr;
    vkGetImageMemoryRequirements(g_dev, g_aivision_tex, &mr);
    VkPhysicalDeviceMemoryProperties mp;
    vkGetPhysicalDeviceMemoryProperties(g_pdev, &mp);
    uint32_t mi = vk_find_mem(&mp, mr.memoryTypeBits, VK_MEMORY_PROPERTY_DEVICE_LOCAL_BIT);
    if (mi == UINT32_MAX) return 0;
    VkMemoryAllocateInfo mai = { VK_STRUCTURE_TYPE_MEMORY_ALLOCATE_INFO };
    mai.allocationSize = mr.size; mai.memoryTypeIndex = mi;
    if (vkAllocateMemory(g_dev, &mai, NULL, &g_aivision_tex_mem) != VK_SUCCESS) return 0;
    vkBindImageMemory(g_dev, g_aivision_tex, g_aivision_tex_mem, 0);

    VkImageViewCreateInfo vci = { VK_STRUCTURE_TYPE_IMAGE_VIEW_CREATE_INFO };
    vci.image = g_aivision_tex; vci.viewType = VK_IMAGE_VIEW_TYPE_2D; vci.format = VK_FORMAT_R8G8B8A8_UNORM;
    vci.subresourceRange.aspectMask = VK_IMAGE_ASPECT_COLOR_BIT;
    vci.subresourceRange.levelCount = 1; vci.subresourceRange.layerCount = 1;
    if (vkCreateImageView(g_dev, &vci, NULL, &g_aivision_tex_view) != VK_SUCCESS) return 0;

    VkDescriptorImageInfo imgInfo = { VK_NULL_HANDLE, g_aivision_tex_view, VK_IMAGE_LAYOUT_SHADER_READ_ONLY_OPTIMAL };
    VkWriteDescriptorSet write = { VK_STRUCTURE_TYPE_WRITE_DESCRIPTOR_SET };
    write.dstSet = g_aivision_dset; write.dstBinding = 0; write.descriptorCount = 1;
    write.descriptorType = VK_DESCRIPTOR_TYPE_COMBINED_IMAGE_SAMPLER;
    write.pImageInfo = &imgInfo;
    vkUpdateDescriptorSets(g_dev, 1, &write, 0, NULL);

    VkDeviceSize sz = (VkDeviceSize)w * (VkDeviceSize)h * 4;
    if (g_aivision_stage_buf == VK_NULL_HANDLE || g_aivision_stage_sz < sz) {
        if (g_aivision_stage_buf != VK_NULL_HANDLE) {
            vkUnmapMemory(g_dev, g_aivision_stage_mem);
            vkFreeMemory(g_dev, g_aivision_stage_mem, NULL); g_aivision_stage_mem = VK_NULL_HANDLE;
            vkDestroyBuffer(g_dev, g_aivision_stage_buf, NULL); g_aivision_stage_buf = VK_NULL_HANDLE;
            g_aivision_stage_ptr = NULL; g_aivision_stage_sz = 0;
        }
        VkBufferCreateInfo bci = { VK_STRUCTURE_TYPE_BUFFER_CREATE_INFO };
        bci.size = sz; bci.usage = VK_BUFFER_USAGE_TRANSFER_SRC_BIT; bci.sharingMode = VK_SHARING_MODE_EXCLUSIVE;
        if (vkCreateBuffer(g_dev, &bci, NULL, &g_aivision_stage_buf) != VK_SUCCESS) return 0;
        VkMemoryRequirements smr;
        vkGetBufferMemoryRequirements(g_dev, g_aivision_stage_buf, &smr);
        uint32_t smi = vk_find_mem(&mp, smr.memoryTypeBits,
            VK_MEMORY_PROPERTY_HOST_VISIBLE_BIT | VK_MEMORY_PROPERTY_HOST_COHERENT_BIT);
        if (smi == UINT32_MAX) return 0;
        VkMemoryAllocateInfo smai = { VK_STRUCTURE_TYPE_MEMORY_ALLOCATE_INFO };
        smai.allocationSize = smr.size; smai.memoryTypeIndex = smi;
        if (vkAllocateMemory(g_dev, &smai, NULL, &g_aivision_stage_mem) != VK_SUCCESS) return 0;
        vkBindBufferMemory(g_dev, g_aivision_stage_buf, g_aivision_stage_mem, 0);
        vkMapMemory(g_dev, g_aivision_stage_mem, 0, VK_WHOLE_SIZE, 0, &g_aivision_stage_ptr);
        g_aivision_stage_sz = sz;
    }

    g_aivision_tex_w = w; g_aivision_tex_h = h;
    return 1;
}

// vk_aivision_maybe_upload_cmds: AI Vision's counterpart to
// vk_hud_maybe_upload_cmds -- checks (under g_cs) whether
// pushAIVisionOverlayToVulkan published a fresh detection-boxes canvas since
// the last upload (only happens once per completed detection pass, ~0.5-2Hz,
// see ai_vision.go's package doc comment) and, if so, (re)sizes the texture
// to match and records the buffer->image copy into cb. Must be called after
// vkBeginCommandBuffer and before the render pass begins.
static void vk_aivision_maybe_upload_cmds(VkCommandBuffer cb, int fw, int fh) {
    if (!g_cs_init) return;

    int have_new = 0, w = 0, h = 0;
    EnterCriticalSection(&g_cs);
    if (g_aivision_dirty && g_aivision_pixels) {
        w = g_aivision_pending_w; h = g_aivision_pending_h;
        have_new = 1;
    }
    LeaveCriticalSection(&g_cs);
    if (!have_new) return;
    if (!vk_aivision_ensure_tex(w, h)) return;

    EnterCriticalSection(&g_cs);
    if (g_aivision_dirty && g_aivision_pending_w == w && g_aivision_pending_h == h) {
        memcpy(g_aivision_stage_ptr, g_aivision_pixels, (size_t)w * (size_t)h * 4);
        g_aivision_dirty = 0;
    } else {
        have_new = 0; // size changed again mid-upload (rare) -- pick it up next frame instead
    }
    LeaveCriticalSection(&g_cs);
    if (!have_new) return;

    VkImageLayout old_layout = g_aivision_tex_layout;
    vk_image_barrier(cb, g_aivision_tex, old_layout, VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL,
        old_layout == VK_IMAGE_LAYOUT_UNDEFINED ? 0 : VK_ACCESS_SHADER_READ_BIT,
        VK_ACCESS_TRANSFER_WRITE_BIT,
        old_layout == VK_IMAGE_LAYOUT_UNDEFINED ? VK_PIPELINE_STAGE_TOP_OF_PIPE_BIT : VK_PIPELINE_STAGE_FRAGMENT_SHADER_BIT,
        VK_PIPELINE_STAGE_TRANSFER_BIT);

    VkBufferImageCopy bic = {0};
    bic.bufferRowLength = (uint32_t)w;
    bic.bufferImageHeight = (uint32_t)h;
    bic.imageSubresource.aspectMask = VK_IMAGE_ASPECT_COLOR_BIT;
    bic.imageSubresource.layerCount = 1;
    bic.imageExtent = (VkExtent3D){ (uint32_t)w, (uint32_t)h, 1 };
    vkCmdCopyBufferToImage(cb, g_aivision_stage_buf, g_aivision_tex, VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL, 1, &bic);

    vk_image_barrier(cb, g_aivision_tex, VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL, VK_IMAGE_LAYOUT_SHADER_READ_ONLY_OPTIMAL,
        VK_ACCESS_TRANSFER_WRITE_BIT, VK_ACCESS_SHADER_READ_BIT,
        VK_PIPELINE_STAGE_TRANSFER_BIT, VK_PIPELINE_STAGE_FRAGMENT_SHADER_BIT);

    g_aivision_tex_layout = VK_IMAGE_LAYOUT_SHADER_READ_ONLY_OPTIMAL;
}

// vk_aivision_record_draw issues AI Vision's alpha-blended quad draw call,
// covering the ENTIRE fw x fh video frame (unlike vk_hud_record_draw's
// corner-anchored box) since detection boxes are positioned across the
// whole picture. Draws only when the overlay texture is actually sized to
// match the current frame -- a stale, differently-sized texture (e.g. right
// after a resolution change, before the next detection pass republishes)
// would stretch old boxes to the wrong place, so it's skipped rather than
// shown misaligned until the next detection pass catches up. Must be called
// between pfnBeginRendering and pfnEndRendering, with a viewport/scissor
// already bound.
static void vk_aivision_record_draw(VkCommandBuffer cb, int fw, int fh) {
    if (!g_aivision_active || g_hud_resources_ok <= 0) return;
    if (g_aivision_tex == VK_NULL_HANDLE || g_aivision_tex_w != fw || g_aivision_tex_h != fh) return;

    float rect[4] = { -1.0f, -1.0f, 1.0f, 1.0f }; // full frame, no anchoring math needed

    vkCmdBindPipeline(cb, VK_PIPELINE_BIND_POINT_GRAPHICS, g_hud_pipeline);
    vkCmdBindDescriptorSets(cb, VK_PIPELINE_BIND_POINT_GRAPHICS, g_hud_playout, 0, 1, &g_aivision_dset, 0, NULL);
    vkCmdPushConstants(cb, g_hud_playout, VK_SHADER_STAGE_VERTEX_BIT, 0, sizeof(rect), rect);
    vkCmdDraw(cb, 6, 1, 0, 0);
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
    vk_conceal_debug_log_flow_stats(); // DEBUG: drain any pending flow readback from the stall that just ended
    vk_conceal_maybe_log_summary();
    vk_conceal_consume_prior();

    // Our own render work reading the previous frame's VkImage is now known
    // to have retired (the fence we just waited on guards exactly that) —
    // safe to release the AVFrame ref keeping it alive.
    if (g_vkf_prev_release_fn) { g_vkf_prev_release_fn(g_vkf_prev_release_ctx); }
    g_vkf_prev_release_ctx = NULL; g_vkf_prev_release_fn = NULL;

    // Frame smoothing: lazily (re)size the dual real-frame textures to match
    // this frame's resolution (see vk_conceal_ensure_tex2's doc comment for
    // why they carry COLOR_ATTACHMENT_BIT on this path). Skipped entirely
    // while the feature is off, so the happy path never allocates this GPU
    // memory or adds the extra render pass below.
    int conceal_capture = atomic_load(&g_conceal_enabled) && vk_conceal_ensure_tex2(fw, fh);
    int conceal_slot = conceal_capture ? (1 - g_conceal_cur) : -1;

    vkResetCommandBuffer(g_cmdbuf, 0);
    VkCommandBufferBeginInfo bi = { VK_STRUCTURE_TYPE_COMMAND_BUFFER_BEGIN_INFO };
    bi.flags = VK_COMMAND_BUFFER_USAGE_ONE_TIME_SUBMIT_BIT;
    vkBeginCommandBuffer(g_cmdbuf, &bi);

    // Net Graph HUD: upload a fresh texture if net_graph.go pushed one since
    // the last frame (near-zero cost otherwise -- see the function's own
    // comment). Must be recorded before the render pass begins below.
    vk_hud_maybe_upload_cmds(g_cmdbuf);
    // AI Vision: same idea, but only on the rare frame a detection pass
    // actually completed (~0.5-2Hz) -- see the function's own comment.
    vk_aivision_maybe_upload_cmds(g_cmdbuf, fw, fh);

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
    // Further draw calls, same render pass, same viewport/scissor: AI
    // Vision's detection boxes (drawn first, full-frame) then the Net Graph
    // HUD (corner box, drawn last so it stays on top if they ever overlap)
    // -- both composited straight into the swapchain image with no
    // GPU->CPU readback of the video frame itself. See
    // vk_aivision_record_draw/vk_hud_record_draw.
    vk_aivision_record_draw(g_cmdbuf, fw, fh);
    vk_hud_record_draw(g_cmdbuf, fw, fh);
    pfnEndRendering(g_cmdbuf);

    vk_image_barrier(g_cmdbuf, g_swap_imgs[img_idx],
        VK_IMAGE_LAYOUT_COLOR_ATTACHMENT_OPTIMAL, VK_IMAGE_LAYOUT_PRESENT_SRC_KHR,
        VK_ACCESS_COLOR_ATTACHMENT_WRITE_BIT, 0,
        VK_PIPELINE_STAGE_COLOR_ATTACHMENT_OUTPUT_BIT, VK_PIPELINE_STAGE_BOTTOM_OF_PIPE_BIT);

    // Frame smoothing capture: a second, unletterboxed render of the SAME
    // ycbcr-converted frame (same pipeline/descriptor set/fullscreen
    // triangle as the swapchain draw above, just a different render
    // target and full fw x fh viewport instead of the letterboxed one) into
    // g_conceal_tex[conceal_slot] -- reuses the already-proven YCbCr->RGB
    // conversion instead of writing a second one, and stays entirely on the
    // GPU (no readback), so this costs one extra cheap draw call, not a
    // CPU round-trip.
    if (conceal_slot >= 0) {
        VkPipelineStageFlags src_stage = (g_conceal_tex_layout[conceal_slot] == VK_IMAGE_LAYOUT_UNDEFINED)
            ? VK_PIPELINE_STAGE_TOP_OF_PIPE_BIT : VK_PIPELINE_STAGE_ALL_COMMANDS_BIT;
        vk_image_barrier(g_cmdbuf, g_conceal_tex[conceal_slot],
            g_conceal_tex_layout[conceal_slot], VK_IMAGE_LAYOUT_COLOR_ATTACHMENT_OPTIMAL,
            0, VK_ACCESS_COLOR_ATTACHMENT_WRITE_BIT,
            src_stage, VK_PIPELINE_STAGE_COLOR_ATTACHMENT_OUTPUT_BIT);

        VkRenderingAttachmentInfo concealAtt = { VK_STRUCTURE_TYPE_RENDERING_ATTACHMENT_INFO };
        concealAtt.imageView = g_conceal_tex_view[conceal_slot];
        concealAtt.imageLayout = VK_IMAGE_LAYOUT_COLOR_ATTACHMENT_OPTIMAL;
        concealAtt.loadOp = VK_ATTACHMENT_LOAD_OP_DONT_CARE; // full fw x fh draw below overwrites every pixel
        concealAtt.storeOp = VK_ATTACHMENT_STORE_OP_STORE;

        VkRenderingInfo concealInfo = { VK_STRUCTURE_TYPE_RENDERING_INFO };
        concealInfo.renderArea.extent.width = (uint32_t)fw; concealInfo.renderArea.extent.height = (uint32_t)fh;
        concealInfo.layerCount = 1;
        concealInfo.colorAttachmentCount = 1; concealInfo.pColorAttachments = &concealAtt;

        pfnBeginRendering(g_cmdbuf, &concealInfo);
        VkViewport cvp = { 0.0f, 0.0f, (float)fw, (float)fh, 0.0f, 1.0f };
        VkRect2D csc = { { 0, 0 }, { (uint32_t)fw, (uint32_t)fh } };
        vkCmdSetViewport(g_cmdbuf, 0, 1, &cvp);
        vkCmdSetScissor(g_cmdbuf, 0, 1, &csc);
        vkCmdBindPipeline(g_cmdbuf, VK_PIPELINE_BIND_POINT_GRAPHICS, pl->pipeline);
        vkCmdBindDescriptorSets(g_cmdbuf, VK_PIPELINE_BIND_POINT_GRAPHICS, pl->playout, 0, 1, &dset, 0, NULL);
        vkCmdDraw(g_cmdbuf, 3, 1, 0, 0);
        pfnEndRendering(g_cmdbuf);

        vk_image_barrier(g_cmdbuf, g_conceal_tex[conceal_slot],
            VK_IMAGE_LAYOUT_COLOR_ATTACHMENT_OPTIMAL, VK_IMAGE_LAYOUT_GENERAL,
            VK_ACCESS_COLOR_ATTACHMENT_WRITE_BIT, VK_ACCESS_SHADER_READ_BIT,
            VK_PIPELINE_STAGE_COLOR_ATTACHMENT_OUTPUT_BIT, VK_PIPELINE_STAGE_ALL_COMMANDS_BIT);
        g_conceal_tex_layout[conceal_slot] = VK_IMAGE_LAYOUT_GENERAL;
    }

    vkEndCommandBuffer(g_cmdbuf);

    VkPipelineStageFlags wait_stage = VK_PIPELINE_STAGE_COLOR_ATTACHMENT_OUTPUT_BIT;
    VkSubmitInfo si = { VK_STRUCTURE_TYPE_SUBMIT_INFO };
    si.waitSemaphoreCount = 1; si.pWaitSemaphores = &g_img_sem; si.pWaitDstStageMask = &wait_stage;
    si.commandBufferCount = 1; si.pCommandBuffers = &g_cmdbuf;
    si.signalSemaphoreCount = 1; si.pSignalSemaphores = &g_rnd_sem;
    vk_conceal_note_real_frame(conceal_slot);
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

// vk_conceal_note_real_frame runs the frame-smoothing bookkeeping shared by
// both real-frame render paths (RGBA vk_render_frame and zero-copy
// vk_render_frame_vkimage) once conceal_slot's texture has been populated as
// part of the command buffer about to be submitted -- committed the instant
// that submit happens, independent of whether the present that follows
// succeeds. A real frame just rendered: any in-progress stall is over
// (reset consecutive/flow-fresh), and its measured arrival interval feeds
// the rolling EMA that decideConcealment (Go) compares future gaps against.
// g_conceal_have_prev only flips true after the SECOND capture
// (g_conceal_capture_count reaching 2) -- after the first, the "other" slot
// has never been written (still VK_IMAGE_LAYOUT_UNDEFINED/no real pixel
// data) and must not be treated as a valid "previous" frame. conceal_slot
// < 0 means concealment wasn't capturing this frame (disabled, or
// vk_conceal_ensure_tex2 failed) -- a no-op.
static void vk_conceal_note_real_frame(int conceal_slot) {
    if (conceal_slot < 0) return;
    double now = mono_sec();
    if (g_conceal_last_real_ts > 0.0) {
        double interval_ms = (now - g_conceal_last_real_ts) * 1000.0;
        g_conceal_expected_ms = goFrameSmoothingUpdateInterval(g_conceal_expected_ms, interval_ms);
    }
    g_conceal_summary_real++;
    g_last_present_t = -1.0; // this tick is a real frame -- see g_last_present_t's doc comment
    if (g_conceal_max_consecutive <= 0) g_conceal_max_consecutive = goFrameSmoothingMaxConsecutive();
    // g_conceal_consecutive reaching the cap means the LAST tick before this
    // real frame was refused further concealment (goFrameSmoothingDecide
    // gives up past frameSmoothingMaxConsecutive) -- the display sat frozen
    // on the last synthesized frame for however much longer the real frame
    // took to actually arrive after that. A stall that resolves before
    // hitting the cap is fully covered by smoothing; one that hits it is
    // the "loss" the user asked to be able to see.
    if (g_conceal_consecutive >= g_conceal_max_consecutive) g_conceal_summary_exhausted++;
    g_conceal_last_real_ts = now;
    g_conceal_consecutive  = 0;
    g_conceal_flow_fresh   = 0;
    g_stat_concealing      = 0;
    g_conceal_cur = conceal_slot;
    if (g_conceal_capture_count < 2) {
        g_conceal_capture_count++;
        g_conceal_have_prev = (g_conceal_capture_count >= 2);
    }
}

static int vk_render_frame(uint8_t *pixels, int fw, int fh, int fs) {
    if (!g_dev || !g_swap) return 0;
    char _dbg[96];

    size_t frame_sz = (size_t)fh * (size_t)fs;

    g_render_stage = 2; // staging
    if (!vk_ensure_staging(frame_sz)) { g_render_stage = 1; return 0; }
    if (!vk_ensure_tex(fw, fh))       { g_render_stage = 1; return 0; }

    // Frame smoothing: lazily (re)size the dual real-frame textures to match
    // this frame's resolution. Skipped entirely while the feature is off, so
    // the happy path never allocates this GPU memory. Failure here just means
    // no concealment material this frame -- never fails vk_render_frame itself.
    int conceal_capture = atomic_load(&g_conceal_enabled) && vk_conceal_ensure_tex2(fw, fh);

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
    vk_conceal_debug_log_flow_stats(); // DEBUG: drain any pending flow readback from the stall that just ended
    vk_conceal_maybe_log_summary();
    vk_conceal_consume_prior();

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

    // Frame smoothing: also copy the same already-uploaded staging buffer
    // into the "next" dual-frame slot (reading the same source buffer twice,
    // into two different destination images, is a well-defined read-read —
    // no hazard between this and the g_tex copy above). Ping-pongs
    // g_conceal_cur so the render thread always has the last two real frames
    // available to block-match/warp from during a stall.
    int conceal_slot = -1;
    if (conceal_capture) {
        conceal_slot = 1 - g_conceal_cur;
        VkPipelineStageFlags src_stage = (g_conceal_tex_layout[conceal_slot] == VK_IMAGE_LAYOUT_UNDEFINED)
            ? VK_PIPELINE_STAGE_TOP_OF_PIPE_BIT : VK_PIPELINE_STAGE_ALL_COMMANDS_BIT;
        vk_image_barrier(g_cmdbuf, g_conceal_tex[conceal_slot],
            g_conceal_tex_layout[conceal_slot], VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL,
            0, VK_ACCESS_TRANSFER_WRITE_BIT,
            src_stage, VK_PIPELINE_STAGE_TRANSFER_BIT);
        vkCmdCopyBufferToImage(g_cmdbuf, g_stage_buf, g_conceal_tex[conceal_slot],
                               VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL, 1, &bic);
        vk_image_barrier(g_cmdbuf, g_conceal_tex[conceal_slot],
            VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL, VK_IMAGE_LAYOUT_GENERAL,
            VK_ACCESS_TRANSFER_WRITE_BIT, VK_ACCESS_SHADER_READ_BIT,
            VK_PIPELINE_STAGE_TRANSFER_BIT, VK_PIPELINE_STAGE_ALL_COMMANDS_BIT);
        g_conceal_tex_layout[conceal_slot] = VK_IMAGE_LAYOUT_GENERAL;
    }

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

    vk_conceal_note_real_frame(conceal_slot);

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

// ─── frame smoothing: block-match optical flow + warp/extrapolate ───────────
// Phase 1 "shader" optical-flow backend (see frame_smoothing.go's doc
// comment). A future NVIDIA Optical Flow SDK backend would implement the
// same "produce a flow field from two frames" contract and slot in here
// without touching vk_render_frame_conceal's stall/present logic below --
// this section is the whole of that contract for now, kept deliberately
// self-contained (own pipelines/descriptor sets/textures) rather than
// threaded through the ycbcr/HUD pipeline machinery above.

// Layout mirrors flow_blockmatch.comp's/warp_extrapolate.comp's push_constant
// blocks field-for-field (see shader_arrays.h's doc comment) -- every field
// is a 4-byte int/float so there's no struct-packing mismatch to worry about.
// _pad0 is load-bearing, not cosmetic: GLSL's default push_constant layout
// aligns vec2 to 8 bytes, so the shader's `vec2 priorDir` after `int
// searchRadius` sits at byte offset 24, not 20 -- a plain C struct with no
// forced alignment between two 4-byte members packs them tight at offset
// 20 instead, which would silently misalign every field from there on
// (shader reads garbage for priorDir, and reads priorDir.x's bytes as part
// of searchRadius). Verified against glslc's actual layout, not assumed.
typedef struct { int32_t srcSize[2]; int32_t blockSize[2]; int32_t searchRadius; int32_t _pad0; float priorDir[2]; } VkFlowPushConstants;
typedef struct { int32_t dstSize[2]; int32_t blockSize[2]; float extrapolateT; } VkWarpPushConstants;

// vk_conceal_ensure_tex2 (re)allocates the two dual real-frame SAMPLED
// textures to match (w,h). Mirrors vk_ensure_tex's grow-on-change pattern;
// unlike g_tex these carry VK_IMAGE_USAGE_SAMPLED_BIT since the block-match/
// warp compute shaders read them, and losing their content on resize is
// fine -- it just means concealment needs two fresh real frames again after
// a resolution change, same as any other frame-history-based feature would.
// Also carries COLOR_ATTACHMENT_BIT: on the zero-copy VkImage decode path
// (vk_render_frame_vkimage), populating a slot means rendering the existing
// ycbcr pipeline's fullscreen triangle into it as a real render target
// (reusing the exact same proven YCbCr->RGB conversion that already draws
// to the swapchain) rather than a buffer copy, since these frames are
// NV12/P010 YCbCr, not RGBA -- there's no staging buffer to copy from.
static int vk_conceal_ensure_tex2(int w, int h) {
    if (g_conceal_tex[0] != VK_NULL_HANDLE && g_conceal_tex_w == w && g_conceal_tex_h == h) return 1;

    if (g_conceal_tex[0] != VK_NULL_HANDLE) {
        vkDeviceWaitIdle(g_dev);
        for (int i = 0; i < 2; i++) {
            if (g_conceal_tex_view[i]) { vkDestroyImageView(g_dev, g_conceal_tex_view[i], NULL); g_conceal_tex_view[i] = VK_NULL_HANDLE; }
            if (g_conceal_tex_mem[i])  { vkFreeMemory(g_dev, g_conceal_tex_mem[i], NULL); g_conceal_tex_mem[i] = VK_NULL_HANDLE; }
            if (g_conceal_tex[i])      { vkDestroyImage(g_dev, g_conceal_tex[i], NULL); g_conceal_tex[i] = VK_NULL_HANDLE; }
            g_conceal_tex_layout[i] = VK_IMAGE_LAYOUT_UNDEFINED;
        }
        g_conceal_cur = 0; g_conceal_have_prev = 0; g_conceal_capture_count = 0;
    }

    VkPhysicalDeviceMemoryProperties mp;
    vkGetPhysicalDeviceMemoryProperties(g_pdev, &mp);

    for (int i = 0; i < 2; i++) {
        VkImageCreateInfo ici = { VK_STRUCTURE_TYPE_IMAGE_CREATE_INFO };
        ici.imageType   = VK_IMAGE_TYPE_2D;
        ici.format      = VK_FORMAT_R8G8B8A8_UNORM;
        ici.extent      = (VkExtent3D){(uint32_t)w, (uint32_t)h, 1};
        ici.mipLevels   = 1;
        ici.arrayLayers = 1;
        ici.samples     = VK_SAMPLE_COUNT_1_BIT;
        ici.tiling      = VK_IMAGE_TILING_OPTIMAL;
        ici.usage       = VK_IMAGE_USAGE_TRANSFER_DST_BIT | VK_IMAGE_USAGE_SAMPLED_BIT | VK_IMAGE_USAGE_COLOR_ATTACHMENT_BIT;
        ici.initialLayout = VK_IMAGE_LAYOUT_UNDEFINED;
        if (vkCreateImage(g_dev, &ici, NULL, &g_conceal_tex[i]) != VK_SUCCESS) return 0;

        VkMemoryRequirements mr;
        vkGetImageMemoryRequirements(g_dev, g_conceal_tex[i], &mr);
        uint32_t mi = vk_find_mem(&mp, mr.memoryTypeBits, VK_MEMORY_PROPERTY_DEVICE_LOCAL_BIT);
        if (mi == UINT32_MAX) return 0;

        VkMemoryAllocateInfo mai = { VK_STRUCTURE_TYPE_MEMORY_ALLOCATE_INFO };
        mai.allocationSize  = mr.size;
        mai.memoryTypeIndex = mi;
        if (vkAllocateMemory(g_dev, &mai, NULL, &g_conceal_tex_mem[i]) != VK_SUCCESS) return 0;
        vkBindImageMemory(g_dev, g_conceal_tex[i], g_conceal_tex_mem[i], 0);

        VkImageViewCreateInfo vci = { VK_STRUCTURE_TYPE_IMAGE_VIEW_CREATE_INFO };
        vci.image = g_conceal_tex[i];
        vci.viewType = VK_IMAGE_VIEW_TYPE_2D;
        vci.format = VK_FORMAT_R8G8B8A8_UNORM;
        vci.subresourceRange = (VkImageSubresourceRange){ VK_IMAGE_ASPECT_COLOR_BIT, 0, 1, 0, 1 };
        if (vkCreateImageView(g_dev, &vci, NULL, &g_conceal_tex_view[i]) != VK_SUCCESS) return 0;
    }
    g_conceal_tex_w = w; g_conceal_tex_h = h;
    return 1;
}

// vk_conceal_ensure_flow_synth (re)allocates the block-grid flow texture
// (sized to (w,h) at VK_CONCEAL_BLOCK granularity) and the full-resolution
// synthesized-frame texture. Both are STORAGE images (compute-written via
// imageStore); g_synth_tex additionally carries TRANSFER_SRC_BIT since
// vk_render_frame_conceal blits it straight into the swapchain, reusing
// vk_render_frame's own letterbox-blit approach rather than a second
// presentation code path.
static int vk_conceal_ensure_flow_synth(int w, int h) {
    int grid_w = (w + VK_CONCEAL_BLOCK - 1) / VK_CONCEAL_BLOCK;
    int grid_h = (h + VK_CONCEAL_BLOCK - 1) / VK_CONCEAL_BLOCK;
    int need_resize = (g_flow_tex == VK_NULL_HANDLE || g_flow_w != grid_w || g_flow_h != grid_h ||
                        g_synth_tex == VK_NULL_HANDLE || g_conceal_tex_w != w || g_conceal_tex_h != h);
    if (!need_resize) return 1;

    vkDeviceWaitIdle(g_dev);
    if (g_flow_view)  { vkDestroyImageView(g_dev, g_flow_view, NULL); g_flow_view = VK_NULL_HANDLE; }
    if (g_flow_mem)   { vkFreeMemory(g_dev, g_flow_mem, NULL); g_flow_mem = VK_NULL_HANDLE; }
    if (g_flow_tex)   { vkDestroyImage(g_dev, g_flow_tex, NULL); g_flow_tex = VK_NULL_HANDLE; }
    if (g_synth_view) { vkDestroyImageView(g_dev, g_synth_view, NULL); g_synth_view = VK_NULL_HANDLE; }
    if (g_synth_mem)  { vkFreeMemory(g_dev, g_synth_mem, NULL); g_synth_mem = VK_NULL_HANDLE; }
    if (g_synth_tex)  { vkDestroyImage(g_dev, g_synth_tex, NULL); g_synth_tex = VK_NULL_HANDLE; }
    g_synth_layout = VK_IMAGE_LAYOUT_UNDEFINED;

    VkPhysicalDeviceMemoryProperties mp;
    vkGetPhysicalDeviceMemoryProperties(g_pdev, &mp);

    // Flow texture: R16G16_SFLOAT, block-grid resolution, STORAGE (written
    // by flow_blockmatch.comp) + SAMPLED (read, bilinearly upsampled, by
    // warp_extrapolate.comp).
    {
        VkImageCreateInfo ici = { VK_STRUCTURE_TYPE_IMAGE_CREATE_INFO };
        ici.imageType = VK_IMAGE_TYPE_2D;
        ici.format    = VK_FORMAT_R16G16_SFLOAT;
        ici.extent    = (VkExtent3D){(uint32_t)grid_w, (uint32_t)grid_h, 1};
        ici.mipLevels = 1; ici.arrayLayers = 1;
        ici.samples   = VK_SAMPLE_COUNT_1_BIT;
        ici.tiling    = VK_IMAGE_TILING_OPTIMAL;
        ici.usage     = VK_IMAGE_USAGE_STORAGE_BIT | VK_IMAGE_USAGE_SAMPLED_BIT;
        ici.initialLayout = VK_IMAGE_LAYOUT_UNDEFINED;
        if (vkCreateImage(g_dev, &ici, NULL, &g_flow_tex) != VK_SUCCESS) return 0;

        VkMemoryRequirements mr;
        vkGetImageMemoryRequirements(g_dev, g_flow_tex, &mr);
        uint32_t mi = vk_find_mem(&mp, mr.memoryTypeBits, VK_MEMORY_PROPERTY_DEVICE_LOCAL_BIT);
        if (mi == UINT32_MAX) return 0;
        VkMemoryAllocateInfo mai = { VK_STRUCTURE_TYPE_MEMORY_ALLOCATE_INFO };
        mai.allocationSize = mr.size; mai.memoryTypeIndex = mi;
        if (vkAllocateMemory(g_dev, &mai, NULL, &g_flow_mem) != VK_SUCCESS) return 0;
        vkBindImageMemory(g_dev, g_flow_tex, g_flow_mem, 0);

        VkImageViewCreateInfo vci = { VK_STRUCTURE_TYPE_IMAGE_VIEW_CREATE_INFO };
        vci.image = g_flow_tex; vci.viewType = VK_IMAGE_VIEW_TYPE_2D; vci.format = VK_FORMAT_R16G16_SFLOAT;
        vci.subresourceRange = (VkImageSubresourceRange){ VK_IMAGE_ASPECT_COLOR_BIT, 0, 1, 0, 1 };
        if (vkCreateImageView(g_dev, &vci, NULL, &g_flow_view) != VK_SUCCESS) return 0;
    }

    // DEBUG: host-visible readback buffer for g_flow_tex (see
    // vk_conceal_debug_log_flow_stats). R16G16_SFLOAT = 4 bytes/texel.
    {
        VkDeviceSize sz = (VkDeviceSize)grid_w * (VkDeviceSize)grid_h * 4;
        if (g_flow_dbg_buf != VK_NULL_HANDLE) {
            vkUnmapMemory(g_dev, g_flow_dbg_mem);
            vkFreeMemory(g_dev, g_flow_dbg_mem, NULL); g_flow_dbg_mem = VK_NULL_HANDLE;
            vkDestroyBuffer(g_dev, g_flow_dbg_buf, NULL); g_flow_dbg_buf = VK_NULL_HANDLE;
        }
        VkBufferCreateInfo bci = { VK_STRUCTURE_TYPE_BUFFER_CREATE_INFO };
        bci.size = sz; bci.usage = VK_BUFFER_USAGE_TRANSFER_DST_BIT; bci.sharingMode = VK_SHARING_MODE_EXCLUSIVE;
        if (vkCreateBuffer(g_dev, &bci, NULL, &g_flow_dbg_buf) != VK_SUCCESS) return 0;

        VkMemoryRequirements mr;
        vkGetBufferMemoryRequirements(g_dev, g_flow_dbg_buf, &mr);
        uint32_t mi = vk_find_mem(&mp, mr.memoryTypeBits,
            VK_MEMORY_PROPERTY_HOST_VISIBLE_BIT | VK_MEMORY_PROPERTY_HOST_COHERENT_BIT);
        if (mi == UINT32_MAX) return 0;
        VkMemoryAllocateInfo mai = { VK_STRUCTURE_TYPE_MEMORY_ALLOCATE_INFO };
        mai.allocationSize = mr.size; mai.memoryTypeIndex = mi;
        if (vkAllocateMemory(g_dev, &mai, NULL, &g_flow_dbg_mem) != VK_SUCCESS) return 0;
        vkBindBufferMemory(g_dev, g_flow_dbg_buf, g_flow_dbg_mem, 0);
        g_flow_dbg_sz = sz;
        g_flow_dbg_pending = 0;
    }

    // Temporal-direction prior: a SMALL (fixed VK_CONCEAL_PRIOR_CROP x
    // VK_CONCEAL_PRIOR_CROP center crop, not the whole grid) host-visible
    // readback buffer, queued EVERY stall (unlike the throttled full-field
    // g_flow_dbg_buf above) -- see vk_conceal_record_flow. Small enough
    // (a few hundred bytes) that reading it back every stall costs nothing
    // worth measuring, unlike a full-grid readback would.
    {
        VkDeviceSize sz = (VkDeviceSize)VK_CONCEAL_PRIOR_CROP * (VkDeviceSize)VK_CONCEAL_PRIOR_CROP * 4;
        if (g_flow_prior_buf == VK_NULL_HANDLE) {
            VkBufferCreateInfo bci = { VK_STRUCTURE_TYPE_BUFFER_CREATE_INFO };
            bci.size = sz; bci.usage = VK_BUFFER_USAGE_TRANSFER_DST_BIT; bci.sharingMode = VK_SHARING_MODE_EXCLUSIVE;
            if (vkCreateBuffer(g_dev, &bci, NULL, &g_flow_prior_buf) != VK_SUCCESS) return 0;

            VkMemoryRequirements mr;
            vkGetBufferMemoryRequirements(g_dev, g_flow_prior_buf, &mr);
            uint32_t mi = vk_find_mem(&mp, mr.memoryTypeBits,
                VK_MEMORY_PROPERTY_HOST_VISIBLE_BIT | VK_MEMORY_PROPERTY_HOST_COHERENT_BIT);
            if (mi == UINT32_MAX) return 0;
            VkMemoryAllocateInfo mai = { VK_STRUCTURE_TYPE_MEMORY_ALLOCATE_INFO };
            mai.allocationSize = mr.size; mai.memoryTypeIndex = mi;
            if (vkAllocateMemory(g_dev, &mai, NULL, &g_flow_prior_mem) != VK_SUCCESS) return 0;
            vkBindBufferMemory(g_dev, g_flow_prior_buf, g_flow_prior_mem, 0);
        }
        g_flow_prior_pending = 0;
        g_conceal_prior_vx = 0.0; g_conceal_prior_vy = 0.0; // stale across a resize -- start fresh
    }

    // Synthesized-frame texture: full res, RGBA8, STORAGE (warp writes it)
    // + TRANSFER_SRC (present blit reads it, exactly like g_tex).
    {
        VkImageCreateInfo ici = { VK_STRUCTURE_TYPE_IMAGE_CREATE_INFO };
        ici.imageType = VK_IMAGE_TYPE_2D;
        ici.format    = VK_FORMAT_R8G8B8A8_UNORM;
        ici.extent    = (VkExtent3D){(uint32_t)w, (uint32_t)h, 1};
        ici.mipLevels = 1; ici.arrayLayers = 1;
        ici.samples   = VK_SAMPLE_COUNT_1_BIT;
        ici.tiling    = VK_IMAGE_TILING_OPTIMAL;
        ici.usage     = VK_IMAGE_USAGE_STORAGE_BIT | VK_IMAGE_USAGE_TRANSFER_SRC_BIT;
        ici.initialLayout = VK_IMAGE_LAYOUT_UNDEFINED;
        if (vkCreateImage(g_dev, &ici, NULL, &g_synth_tex) != VK_SUCCESS) return 0;

        VkMemoryRequirements mr;
        vkGetImageMemoryRequirements(g_dev, g_synth_tex, &mr);
        uint32_t mi = vk_find_mem(&mp, mr.memoryTypeBits, VK_MEMORY_PROPERTY_DEVICE_LOCAL_BIT);
        if (mi == UINT32_MAX) return 0;
        VkMemoryAllocateInfo mai = { VK_STRUCTURE_TYPE_MEMORY_ALLOCATE_INFO };
        mai.allocationSize = mr.size; mai.memoryTypeIndex = mi;
        if (vkAllocateMemory(g_dev, &mai, NULL, &g_synth_mem) != VK_SUCCESS) return 0;
        vkBindImageMemory(g_dev, g_synth_tex, g_synth_mem, 0);

        VkImageViewCreateInfo vci = { VK_STRUCTURE_TYPE_IMAGE_VIEW_CREATE_INFO };
        vci.image = g_synth_tex; vci.viewType = VK_IMAGE_VIEW_TYPE_2D; vci.format = VK_FORMAT_R8G8B8A8_UNORM;
        vci.subresourceRange = (VkImageSubresourceRange){ VK_IMAGE_ASPECT_COLOR_BIT, 0, 1, 0, 1 };
        if (vkCreateImageView(g_dev, &vci, NULL, &g_synth_view) != VK_SUCCESS) return 0;
    }

    g_flow_w = grid_w; g_flow_h = grid_h;
    return 1;
}

// vk_conceal_ensure_pipelines lazily creates the compute pipelines shared by
// every stall (not per-stall) -- sampler, both descriptor set
// layouts/pipeline layouts/pipelines, and a descriptor pool sized for both
// sets. Returns 1 once ready, 0 if creation failed (permanent -- doesn't
// retry, mirrors vk_hud_ensure_resources).
static int vk_conceal_ensure_pipelines(void) {
    if (g_conceal_pipelines_ok) return g_conceal_pipelines_ok > 0;

    VkSamplerCreateInfo sampCI = { VK_STRUCTURE_TYPE_SAMPLER_CREATE_INFO };
    sampCI.magFilter = VK_FILTER_LINEAR;
    sampCI.minFilter = VK_FILTER_LINEAR;
    sampCI.addressModeU = VK_SAMPLER_ADDRESS_MODE_CLAMP_TO_EDGE;
    sampCI.addressModeV = VK_SAMPLER_ADDRESS_MODE_CLAMP_TO_EDGE;
    sampCI.addressModeW = VK_SAMPLER_ADDRESS_MODE_CLAMP_TO_EDGE;
    if (vkCreateSampler(g_dev, &sampCI, NULL, &g_conceal_sampler) != VK_SUCCESS) goto fail;

    // flow_blockmatch.comp: binding0=curTex binding1=prevTex (combined
    // sampler) binding2=flowOut (storage image), all compute stage.
    {
        VkDescriptorSetLayoutBinding b[3] = {0};
        b[0].binding = 0; b[0].descriptorType = VK_DESCRIPTOR_TYPE_COMBINED_IMAGE_SAMPLER; b[0].descriptorCount = 1; b[0].stageFlags = VK_SHADER_STAGE_COMPUTE_BIT; b[0].pImmutableSamplers = &g_conceal_sampler;
        b[1].binding = 1; b[1].descriptorType = VK_DESCRIPTOR_TYPE_COMBINED_IMAGE_SAMPLER; b[1].descriptorCount = 1; b[1].stageFlags = VK_SHADER_STAGE_COMPUTE_BIT; b[1].pImmutableSamplers = &g_conceal_sampler;
        b[2].binding = 2; b[2].descriptorType = VK_DESCRIPTOR_TYPE_STORAGE_IMAGE; b[2].descriptorCount = 1; b[2].stageFlags = VK_SHADER_STAGE_COMPUTE_BIT;
        VkDescriptorSetLayoutCreateInfo dslCI = { VK_STRUCTURE_TYPE_DESCRIPTOR_SET_LAYOUT_CREATE_INFO };
        dslCI.bindingCount = 3; dslCI.pBindings = b;
        if (vkCreateDescriptorSetLayout(g_dev, &dslCI, NULL, &g_flow_dsl) != VK_SUCCESS) goto fail;
    }
    // warp_extrapolate.comp: binding0=curTex binding1=flowTex (combined
    // sampler) binding2=outImg (storage image), same shape.
    {
        VkDescriptorSetLayoutBinding b[3] = {0};
        b[0].binding = 0; b[0].descriptorType = VK_DESCRIPTOR_TYPE_COMBINED_IMAGE_SAMPLER; b[0].descriptorCount = 1; b[0].stageFlags = VK_SHADER_STAGE_COMPUTE_BIT; b[0].pImmutableSamplers = &g_conceal_sampler;
        b[1].binding = 1; b[1].descriptorType = VK_DESCRIPTOR_TYPE_COMBINED_IMAGE_SAMPLER; b[1].descriptorCount = 1; b[1].stageFlags = VK_SHADER_STAGE_COMPUTE_BIT; b[1].pImmutableSamplers = &g_conceal_sampler;
        b[2].binding = 2; b[2].descriptorType = VK_DESCRIPTOR_TYPE_STORAGE_IMAGE; b[2].descriptorCount = 1; b[2].stageFlags = VK_SHADER_STAGE_COMPUTE_BIT;
        VkDescriptorSetLayoutCreateInfo dslCI = { VK_STRUCTURE_TYPE_DESCRIPTOR_SET_LAYOUT_CREATE_INFO };
        dslCI.bindingCount = 3; dslCI.pBindings = b;
        if (vkCreateDescriptorSetLayout(g_dev, &dslCI, NULL, &g_warp_dsl) != VK_SUCCESS) goto fail;
    }

    {
        VkPushConstantRange pcr = { VK_SHADER_STAGE_COMPUTE_BIT, 0, sizeof(VkFlowPushConstants) };
        VkPipelineLayoutCreateInfo plCI = { VK_STRUCTURE_TYPE_PIPELINE_LAYOUT_CREATE_INFO };
        plCI.setLayoutCount = 1; plCI.pSetLayouts = &g_flow_dsl;
        plCI.pushConstantRangeCount = 1; plCI.pPushConstantRanges = &pcr;
        if (vkCreatePipelineLayout(g_dev, &plCI, NULL, &g_flow_playout) != VK_SUCCESS) goto fail;
    }
    {
        VkPushConstantRange pcr = { VK_SHADER_STAGE_COMPUTE_BIT, 0, sizeof(VkWarpPushConstants) };
        VkPipelineLayoutCreateInfo plCI = { VK_STRUCTURE_TYPE_PIPELINE_LAYOUT_CREATE_INFO };
        plCI.setLayoutCount = 1; plCI.pSetLayouts = &g_warp_dsl;
        plCI.pushConstantRangeCount = 1; plCI.pPushConstantRanges = &pcr;
        if (vkCreatePipelineLayout(g_dev, &plCI, NULL, &g_warp_playout) != VK_SUCCESS) goto fail;
    }

    {
        VkDescriptorPoolSize sizes[2] = {
            { VK_DESCRIPTOR_TYPE_COMBINED_IMAGE_SAMPLER, 4 }, // 2 bindings * 2 sets
            { VK_DESCRIPTOR_TYPE_STORAGE_IMAGE, 2 },          // 1 binding * 2 sets
        };
        VkDescriptorPoolCreateInfo poolCI = { VK_STRUCTURE_TYPE_DESCRIPTOR_POOL_CREATE_INFO };
        poolCI.maxSets = 2; poolCI.poolSizeCount = 2; poolCI.pPoolSizes = sizes;
        if (vkCreateDescriptorPool(g_dev, &poolCI, NULL, &g_conceal_dpool) != VK_SUCCESS) goto fail;

        VkDescriptorSetAllocateInfo dsai = { VK_STRUCTURE_TYPE_DESCRIPTOR_SET_ALLOCATE_INFO };
        dsai.descriptorPool = g_conceal_dpool; dsai.descriptorSetCount = 1;
        dsai.pSetLayouts = &g_flow_dsl;
        if (vkAllocateDescriptorSets(g_dev, &dsai, &g_flow_dset) != VK_SUCCESS) goto fail;
        dsai.pSetLayouts = &g_warp_dsl;
        if (vkAllocateDescriptorSets(g_dev, &dsai, &g_warp_dset) != VK_SUCCESS) goto fail;
    }

    {
        VkShaderModule flow_cs = vk_shader_from_spv(g_flow_blockmatch_comp_spv, sizeof(g_flow_blockmatch_comp_spv));
        VkShaderModule warp_cs = vk_shader_from_spv(g_warp_extrapolate_comp_spv, sizeof(g_warp_extrapolate_comp_spv));
        if (!flow_cs || !warp_cs) {
            if (flow_cs) vkDestroyShaderModule(g_dev, flow_cs, NULL);
            if (warp_cs) vkDestroyShaderModule(g_dev, warp_cs, NULL);
            goto fail;
        }
        VkComputePipelineCreateInfo cpci[2] = {0};
        cpci[0].sType = VK_STRUCTURE_TYPE_COMPUTE_PIPELINE_CREATE_INFO;
        cpci[0].stage.sType = VK_STRUCTURE_TYPE_PIPELINE_SHADER_STAGE_CREATE_INFO;
        cpci[0].stage.stage = VK_SHADER_STAGE_COMPUTE_BIT;
        cpci[0].stage.module = flow_cs; cpci[0].stage.pName = "main";
        cpci[0].layout = g_flow_playout;
        cpci[1].sType = VK_STRUCTURE_TYPE_COMPUTE_PIPELINE_CREATE_INFO;
        cpci[1].stage.sType = VK_STRUCTURE_TYPE_PIPELINE_SHADER_STAGE_CREATE_INFO;
        cpci[1].stage.stage = VK_SHADER_STAGE_COMPUTE_BIT;
        cpci[1].stage.module = warp_cs; cpci[1].stage.pName = "main";
        cpci[1].layout = g_warp_playout;
        VkPipeline pipelines[2] = { VK_NULL_HANDLE, VK_NULL_HANDLE };
        VkResult pr = vkCreateComputePipelines(g_dev, VK_NULL_HANDLE, 2, cpci, NULL, pipelines);
        vkDestroyShaderModule(g_dev, flow_cs, NULL);
        vkDestroyShaderModule(g_dev, warp_cs, NULL);
        if (pr != VK_SUCCESS) goto fail;
        g_flow_pipeline = pipelines[0];
        g_warp_pipeline = pipelines[1];
    }

    g_conceal_pipelines_ok = 1;
    return 1;

fail:
    goVKLog("vk_conceal_ensure_pipelines: failed -- frame smoothing will not run", 2);
    g_conceal_pipelines_ok = -1;
    return 0;
}

// vk_conceal_record_flow dispatches flow_blockmatch.comp against the two
// real-frame slots, writing block-grid motion vectors into g_flow_tex.
// Called at most once per stall (g_conceal_flow_fresh gates repeats) --
// while a stall continues, vk_conceal_record_warp is re-dispatched each
// render tick against the SAME cached flow field, just with a larger
// extrapolateT, rather than re-estimating flow every tick.
static void vk_conceal_record_flow(VkCommandBuffer cb, int curSlot, int prevSlot, int fw, int fh) {
    VkDescriptorImageInfo cur  = { VK_NULL_HANDLE, g_conceal_tex_view[curSlot],  VK_IMAGE_LAYOUT_GENERAL };
    VkDescriptorImageInfo prev = { VK_NULL_HANDLE, g_conceal_tex_view[prevSlot], VK_IMAGE_LAYOUT_GENERAL };
    VkDescriptorImageInfo flow = { VK_NULL_HANDLE, g_flow_view, VK_IMAGE_LAYOUT_GENERAL };
    VkWriteDescriptorSet w[3] = {0};
    w[0] = (VkWriteDescriptorSet){ VK_STRUCTURE_TYPE_WRITE_DESCRIPTOR_SET, 0, g_flow_dset, 0, 0, 1, VK_DESCRIPTOR_TYPE_COMBINED_IMAGE_SAMPLER, &cur };
    w[1] = (VkWriteDescriptorSet){ VK_STRUCTURE_TYPE_WRITE_DESCRIPTOR_SET, 0, g_flow_dset, 1, 0, 1, VK_DESCRIPTOR_TYPE_COMBINED_IMAGE_SAMPLER, &prev };
    w[2] = (VkWriteDescriptorSet){ VK_STRUCTURE_TYPE_WRITE_DESCRIPTOR_SET, 0, g_flow_dset, 2, 0, 1, VK_DESCRIPTOR_TYPE_STORAGE_IMAGE, &flow };
    vkUpdateDescriptorSets(g_dev, 3, w, 0, NULL);

    vkCmdBindPipeline(cb, VK_PIPELINE_BIND_POINT_COMPUTE, g_flow_pipeline);
    vkCmdBindDescriptorSets(cb, VK_PIPELINE_BIND_POINT_COMPUTE, g_flow_playout, 0, 1, &g_flow_dset, 0, NULL);
    // Designated initializers, not positional -- VkFlowPushConstants has a
    // _pad0 field (alignment, see its doc comment) that positional
    // {a, b, c, {d, e}} init would silently misassign around.
    VkFlowPushConstants pc = { .srcSize = {fw, fh}, .blockSize = {VK_CONCEAL_BLOCK, VK_CONCEAL_BLOCK},
        .searchRadius = VK_CONCEAL_SEARCH_R,
        .priorDir = { (float)g_conceal_prior_vx, (float)g_conceal_prior_vy } };
    vkCmdPushConstants(cb, g_flow_playout, VK_SHADER_STAGE_COMPUTE_BIT, 0, sizeof(pc), &pc);
    vkCmdDispatch(cb, (g_flow_w + 7) / 8, (g_flow_h + 7) / 8, 1);

    // flow_blockmatch writes g_flow_tex via imageStore; warp reads it via a
    // combined-image-sampler bilinear fetch right after in the same command
    // buffer -- needs a barrier between the two, not just between dispatches
    // and the eventual blit.
    vk_image_barrier(cb, g_flow_tex, VK_IMAGE_LAYOUT_GENERAL, VK_IMAGE_LAYOUT_GENERAL,
        VK_ACCESS_SHADER_WRITE_BIT, VK_ACCESS_SHADER_READ_BIT,
        VK_PIPELINE_STAGE_COMPUTE_SHADER_BIT, VK_PIPELINE_STAGE_COMPUTE_SHADER_BIT);

    // Queue this stall's own small center-crop readback (see
    // g_flow_prior_buf's doc comment) -- becomes NEXT stall's prior once
    // consumed by vk_conceal_consume_prior on a later render call.
    if (g_flow_prior_buf != VK_NULL_HANDLE) {
        int cropW = VK_CONCEAL_PRIOR_CROP < g_flow_w ? VK_CONCEAL_PRIOR_CROP : g_flow_w;
        int cropH = VK_CONCEAL_PRIOR_CROP < g_flow_h ? VK_CONCEAL_PRIOR_CROP : g_flow_h;
        int offX = (g_flow_w - cropW) / 2, offY = (g_flow_h - cropH) / 2;
        VkBufferImageCopy region = {0};
        region.imageSubresource.aspectMask = VK_IMAGE_ASPECT_COLOR_BIT;
        region.imageSubresource.layerCount = 1;
        region.imageOffset = (VkOffset3D){ offX, offY, 0 };
        region.imageExtent = (VkExtent3D){ (uint32_t)cropW, (uint32_t)cropH, 1 };
        vkCmdCopyImageToBuffer(cb, g_flow_tex, VK_IMAGE_LAYOUT_GENERAL, g_flow_prior_buf, 1, &region);
        g_flow_prior_pending = 1;
    }
}

static float half_to_float(uint16_t h); // defined below; used here first

// vk_conceal_consume_prior maps g_flow_prior_buf (safe once the caller has
// waited on the fence covering the submission that wrote it -- same
// contract as vk_conceal_debug_log_flow_stats) and averages it into
// g_conceal_prior_vx/vy for the NEXT stall's flow_blockmatch dispatch to
// use as a tie-breaking prior.
static void vk_conceal_consume_prior(void) {
    if (!g_flow_prior_pending || g_flow_prior_buf == VK_NULL_HANDLE) return;
    g_flow_prior_pending = 0;

    int cropW = VK_CONCEAL_PRIOR_CROP < g_flow_w ? VK_CONCEAL_PRIOR_CROP : g_flow_w;
    int cropH = VK_CONCEAL_PRIOR_CROP < g_flow_h ? VK_CONCEAL_PRIOR_CROP : g_flow_h;
    int n = cropW * cropH;
    if (n <= 0) return;

    void *mapped = NULL;
    VkDeviceSize sz = (VkDeviceSize)VK_CONCEAL_PRIOR_CROP * (VkDeviceSize)VK_CONCEAL_PRIOR_CROP * 4;
    if (vkMapMemory(g_dev, g_flow_prior_mem, 0, sz, 0, &mapped) != VK_SUCCESS || !mapped) return;
    const uint16_t *texels = (const uint16_t*)mapped;

    double sumVx = 0.0, sumVy = 0.0;
    int nonzero = 0;
    for (int i = 0; i < n; i++) {
        float vx = half_to_float(texels[i * 2 + 0]);
        float vy = half_to_float(texels[i * 2 + 1]);
        if (vx != 0.0f || vy != 0.0f) { sumVx += (double)vx; sumVy += (double)vy; nonzero++; }
    }
    vkUnmapMemory(g_dev, g_flow_prior_mem);

    // Only update the prior when this crop actually saw motion -- a
    // transiently-static crop (e.g. mid-stall on a mostly-still scene)
    // shouldn't erase a meaningful prior built up from real panning motion
    // moments earlier; it'll naturally update again once motion resumes.
    if (nonzero > 0) {
        g_conceal_prior_vx = sumVx / nonzero;
        g_conceal_prior_vy = sumVy / nonzero;
    }
}

// DEBUG helpers (temporary, see g_flow_dbg_* doc comment). half_to_float
// decodes IEEE754 binary16 -- R16G16_SFLOAT has no native C type, and this
// is only for a diagnostic log, not a hot path, so no need for a fast/SIMD
// version.
static float half_to_float(uint16_t h) {
    uint32_t sign = (uint32_t)(h & 0x8000u) << 16;
    uint32_t exp  = (h >> 10) & 0x1Fu;
    uint32_t mant = h & 0x3FFu;
    uint32_t bits;
    if (exp == 0) {
        if (mant == 0) { bits = sign; }
        else {
            // subnormal half -> normalized float
            int e = -1;
            do { mant <<= 1; e++; } while (!(mant & 0x400u));
            mant &= 0x3FFu;
            bits = sign | ((uint32_t)(127 - 15 - e) << 23) | (mant << 13);
        }
    } else if (exp == 0x1F) {
        bits = sign | 0x7F800000u | (mant << 13); // inf/nan
    } else {
        bits = sign | ((exp - 15 + 127) << 23) | (mant << 13);
    }
    float f;
    memcpy(&f, &bits, sizeof(f));
    return f;
}

// vk_conceal_debug_queue_flow_readback records a copy of the just-computed
// g_flow_tex into the host-visible g_flow_dbg_buf, in the SAME command
// buffer/submission as the flow dispatch that just wrote it (so ordering is
// implicit -- no extra semaphore needed). The caller must not read
// g_flow_dbg_buf until the fence guarding this submission has been waited
// on; see g_flow_dbg_pending's consumer in vk_render_frame_conceal.
static void vk_conceal_debug_queue_flow_readback(VkCommandBuffer cb, int grid_w, int grid_h) {
    if (g_flow_dbg_buf == VK_NULL_HANDLE) return;
    VkBufferImageCopy region = {0};
    region.imageSubresource.aspectMask = VK_IMAGE_ASPECT_COLOR_BIT;
    region.imageSubresource.layerCount = 1;
    region.imageExtent = (VkExtent3D){ (uint32_t)grid_w, (uint32_t)grid_h, 1 };
    vkCmdCopyImageToBuffer(cb, g_flow_tex, VK_IMAGE_LAYOUT_GENERAL, g_flow_dbg_buf, 1, &region);
    g_flow_dbg_pending  = 1;
    g_flow_dbg_grid_w   = grid_w;
    g_flow_dbg_grid_h   = grid_h;
}

// vk_conceal_debug_log_flow_stats maps g_flow_dbg_buf (already known-safe
// to read -- caller just waited on the fence covering the submission that
// wrote it) and logs aggregate per-block motion-vector stats: how many
// blocks got a non-zero vector past the confidence gate, and the
// average/max magnitude among those. Diagnoses whether "smeared/jumping"
// concealed frames are coming from widespread noisy small vectors (avg
// close to max, many nonzero) vs a few outlier blocks (nonzero% low, max >>
// avg) vs genuinely large coherent motion (nonzero% high, avg close to
// max, magnitudes tracking actual on-screen motion).
static void vk_conceal_debug_log_flow_stats(void) {
    if (!g_flow_dbg_pending || g_flow_dbg_buf == VK_NULL_HANDLE) return;
    g_flow_dbg_pending = 0;

    int grid_w = g_flow_dbg_grid_w, grid_h = g_flow_dbg_grid_h;
    int n = grid_w * grid_h;
    if (n <= 0) return;

    void *mapped = NULL;
    if (vkMapMemory(g_dev, g_flow_dbg_mem, 0, g_flow_dbg_sz, 0, &mapped) != VK_SUCCESS || !mapped) return;
    const uint16_t *texels = (const uint16_t*)mapped;

    int nonzero = 0;
    double sumMag = 0.0, sumVx = 0.0, sumVy = 0.0;
    float maxMag = 0.0f;
    int maxGx = -1, maxGy = -1;
    for (int gy = 0; gy < grid_h; gy++) {
        for (int gx = 0; gx < grid_w; gx++) {
            int idx = (gy * grid_w + gx) * 2;
            float vx = half_to_float(texels[idx + 0]);
            float vy = half_to_float(texels[idx + 1]);
            float mag = sqrtf(vx * vx + vy * vy);
            if (mag > 0.0f) {
                nonzero++;
                sumMag += mag;
                sumVx += (double)vx;
                sumVy += (double)vy;
                if (mag > maxMag) { maxMag = mag; maxGx = gx; maxGy = gy; }
            }
        }
    }
    vkUnmapMemory(g_dev, g_flow_dbg_mem);

    // Stored, not logged directly -- vk_conceal_maybe_log_summary folds this
    // latest sample into the periodic aggregate line instead (see its doc
    // comment for why per-stall logging of this was removed).
    g_flow_dbg_have_sample        = 1;
    g_flow_dbg_sample_nonzero_pct = n > 0 ? (100.0 * nonzero / n) : 0.0;
    g_flow_dbg_sample_nonzero_count = nonzero;
    g_flow_dbg_sample_total_blocks  = n;
    g_flow_dbg_sample_avg_mag     = nonzero > 0 ? (sumMag / nonzero) : 0.0;
    g_flow_dbg_sample_max_mag     = (double)maxMag;
    g_flow_dbg_sample_avg_vx      = nonzero > 0 ? (sumVx / nonzero) : 0.0;
    g_flow_dbg_sample_avg_vy      = nonzero > 0 ? (sumVy / nonzero) : 0.0;
    (void)maxGx; (void)maxGy;

    // Direction-flip check: only meaningful once two samples both actually
    // had nonzero content to compare (a static-content sample's avg vector
    // is (0,0) and would spuriously "flip" against anything).
    if (nonzero > 0) {
        if (g_flow_dbg_have_prev_dir) {
            double dot = g_flow_dbg_sample_avg_vx * g_flow_dbg_prev_avg_vx +
                         g_flow_dbg_sample_avg_vy * g_flow_dbg_prev_avg_vy;
            g_conceal_summary_dir_samples++;
            if (dot < 0.0) g_conceal_summary_dir_flips++;
        }
        g_flow_dbg_prev_avg_vx = g_flow_dbg_sample_avg_vx;
        g_flow_dbg_prev_avg_vy = g_flow_dbg_sample_avg_vy;
        g_flow_dbg_have_prev_dir = 1;
    }

    // Per-sample trace: append if there's room, otherwise shift the buffer
    // left and drop the oldest -- keeps the MOST RECENT samples (the ones
    // closest to whatever's about to be logged) rather than the earliest
    // ones from a stretch that ran long between dumps.
    if (g_flow_trace_count >= VK_FLOW_TRACE_CAP) {
        memmove(&g_flow_trace[0], &g_flow_trace[1], sizeof(g_flow_trace[0]) * (VK_FLOW_TRACE_CAP - 1));
        g_flow_trace_count = VK_FLOW_TRACE_CAP - 1;
    }
    if (g_flow_trace_count < VK_FLOW_TRACE_CAP) {
        VkFlowTraceEntry *e = &g_flow_trace[g_flow_trace_count++];
        e->t = g_flow_dbg_t;
        e->nonzeroPct = (float)g_flow_dbg_sample_nonzero_pct;
        e->nonzeroCount = g_flow_dbg_sample_nonzero_count;
        e->totalBlocks  = g_flow_dbg_sample_total_blocks;
        e->avgMag = (float)g_flow_dbg_sample_avg_mag;
        e->maxMag = (float)g_flow_dbg_sample_max_mag;
        e->vx = (float)g_flow_dbg_sample_avg_vx;
        e->vy = (float)g_flow_dbg_sample_avg_vy;
        e->centerVx = (float)g_conceal_prior_vx;
        e->centerVy = (float)g_conceal_prior_vy;
    }
}

// vk_conceal_maybe_log_summary emits one aggregate telemetry line at most
// every VK_CONCEAL_SUMMARY_INTERVAL_SEC, covering every real and concealed
// frame rendered since the last one (cheap CPU counters, updated on every
// single frame -- see their doc comment) plus the latest throttled GPU flow
// sample, if any landed in that window. Called from all three render paths
// at the same point vk_conceal_debug_log_flow_stats used to log directly.
static void vk_conceal_maybe_log_summary(void) {
    double now = mono_sec();
    if (g_conceal_summary_last_ts <= 0.0) { g_conceal_summary_last_ts = now; return; }
    if (now - g_conceal_summary_last_ts < VK_CONCEAL_SUMMARY_INTERVAL_SEC) return;

    long long real = g_conceal_summary_real, concealed = g_conceal_summary_concealed;
    long long total = real + concealed;
    double avgT = concealed > 0 ? (g_conceal_summary_t_sum / (double)concealed) : 0.0;

    double avgGapMs = g_conceal_summary_gap_count > 0 ? (g_conceal_summary_gap_sum / (double)g_conceal_summary_gap_count) : 0.0;

    char m[500];
    int n = snprintf(m, sizeof(m),
        "frame smoothing summary (%.0fs): real=%lld concealed=%lld (%.0f%% of %lld) avgT=%.2f maxT=%.2f exhausted=%lld"
        " gaps[n=%lld avg=%.1fms max=%.1fms stutters=%lld c2c=%lld]",
        now - g_conceal_summary_last_ts, real, concealed,
        total > 0 ? (100.0 * (double)concealed / (double)total) : 0.0, total,
        avgT, (double)g_conceal_summary_t_max, g_conceal_summary_exhausted,
        g_conceal_summary_gap_count, avgGapMs, g_conceal_summary_gap_max, g_conceal_summary_stutter_count,
        g_conceal_summary_stutter_concealed_to_concealed);
    if (g_flow_dbg_have_sample && n > 0 && n < (int)sizeof(m)) {
        n += snprintf(m + n, sizeof(m) - (size_t)n,
            " lastFlowSample[nonzero=%d/%d(%.2f%%) avgMag=%.2fpx maxMag=%.2fpx avgVec=(%.2f,%.2f)]",
            g_flow_dbg_sample_nonzero_count, g_flow_dbg_sample_total_blocks, g_flow_dbg_sample_nonzero_pct,
            g_flow_dbg_sample_avg_mag, g_flow_dbg_sample_max_mag,
            g_flow_dbg_sample_avg_vx, g_flow_dbg_sample_avg_vy);
    }
    // dirFlips: of the throttled samples compared against the one right
    // before them (not necessarily all within this window -- comparisons
    // span window boundaries), how many had the average flow vector point
    // the opposite way (negative dot product) from the previous sample.
    // Added to directly check a reported "micro-jitter in 2 directions"
    // complaint -- magnitude-only stats (avgMag/maxMag above) can't
    // distinguish coherent one-way motion from a flow field that's
    // flip-flopping direction sample to sample (noise/instability, which
    // extrapolateT would stretch into visible back-and-forth). A high
    // flips/samples ratio here (rather than 0 or near it) means the
    // direction itself is unstable, not just timing/placement.
    if (n > 0 && n < (int)sizeof(m) && g_conceal_summary_dir_samples > 0) {
        snprintf(m + n, sizeof(m) - (size_t)n, " dirFlips=%lld/%lld",
            g_conceal_summary_dir_flips, g_conceal_summary_dir_samples);
    }
    goVKLog(m, 0);

    // Per-sample trace dump: ONE consolidated log line covering every
    // sample taken since the last dump (see g_flow_trace's doc comment) --
    // requested live 2026-09-19 to see per-frame detail on a hard
    // block-matching case (smoke: constantly deforming, no rigid shape to
    // translate) without flooding app.log with one line per sample.
    if (g_flow_trace_count > 0) {
        char tm[2048];
        int tn = snprintf(tm, sizeof(tm), "frame smoothing trace (%d samples):", g_flow_trace_count);
        for (int i = 0; i < g_flow_trace_count && tn > 0 && tn < (int)sizeof(tm); i++) {
            VkFlowTraceEntry *e = &g_flow_trace[i];
            tn += snprintf(tm + tn, sizeof(tm) - (size_t)tn,
                " [t=%.2f nz=%d/%d(%.2f%%) avg=%.1fpx max=%.1fpx vec=(%.1f,%.1f) center=(%.1f,%.1f)]",
                (double)e->t, e->nonzeroCount, e->totalBlocks, (double)e->nonzeroPct,
                (double)e->avgMag, (double)e->maxMag, (double)e->vx, (double)e->vy,
                (double)e->centerVx, (double)e->centerVy);
        }
        goVKLog(tm, 0);
        g_flow_trace_count = 0;
    }

    g_conceal_summary_last_ts    = now;
    g_conceal_summary_real       = 0;
    g_conceal_summary_concealed  = 0;
    g_conceal_summary_exhausted  = 0;
    g_conceal_summary_t_sum      = 0.0;
    g_conceal_summary_t_max      = 0.0f;
    g_flow_dbg_have_sample       = 0;
    g_conceal_summary_dir_samples = 0;
    g_conceal_summary_dir_flips   = 0;
    g_conceal_summary_gap_count     = 0;
    g_conceal_summary_gap_sum       = 0.0;
    g_conceal_summary_gap_max       = 0.0;
    g_conceal_summary_stutter_count = 0;
    g_conceal_summary_stutter_concealed_to_concealed = 0;
}

// vk_conceal_record_warp dispatches warp_extrapolate.comp, sampling
// g_conceal_tex[curSlot] + g_flow_tex and writing g_synth_tex. Transitions
// g_synth_tex to GENERAL first (from wherever it was left -- UNDEFINED on
// first use, or TRANSFER_SRC_OPTIMAL after a previous stall's present blit,
// see vk_render_frame_conceal); leaves it in GENERAL on return; the caller
// transitions it to TRANSFER_SRC_OPTIMAL for the blit.
static void vk_conceal_record_warp(VkCommandBuffer cb, int curSlot, int fw, int fh, float extrapolateT) {
    VkPipelineStageFlags src_stage = (g_synth_layout == VK_IMAGE_LAYOUT_UNDEFINED)
        ? VK_PIPELINE_STAGE_TOP_OF_PIPE_BIT : VK_PIPELINE_STAGE_ALL_COMMANDS_BIT;
    vk_image_barrier(cb, g_synth_tex, g_synth_layout, VK_IMAGE_LAYOUT_GENERAL,
        0, VK_ACCESS_SHADER_WRITE_BIT, src_stage, VK_PIPELINE_STAGE_COMPUTE_SHADER_BIT);
    g_synth_layout = VK_IMAGE_LAYOUT_GENERAL;

    VkDescriptorImageInfo cur  = { VK_NULL_HANDLE, g_conceal_tex_view[curSlot], VK_IMAGE_LAYOUT_GENERAL };
    VkDescriptorImageInfo flow = { VK_NULL_HANDLE, g_flow_view, VK_IMAGE_LAYOUT_GENERAL };
    VkDescriptorImageInfo out  = { VK_NULL_HANDLE, g_synth_view, VK_IMAGE_LAYOUT_GENERAL };
    VkWriteDescriptorSet w[3] = {0};
    w[0] = (VkWriteDescriptorSet){ VK_STRUCTURE_TYPE_WRITE_DESCRIPTOR_SET, 0, g_warp_dset, 0, 0, 1, VK_DESCRIPTOR_TYPE_COMBINED_IMAGE_SAMPLER, &cur };
    w[1] = (VkWriteDescriptorSet){ VK_STRUCTURE_TYPE_WRITE_DESCRIPTOR_SET, 0, g_warp_dset, 1, 0, 1, VK_DESCRIPTOR_TYPE_COMBINED_IMAGE_SAMPLER, &flow };
    w[2] = (VkWriteDescriptorSet){ VK_STRUCTURE_TYPE_WRITE_DESCRIPTOR_SET, 0, g_warp_dset, 2, 0, 1, VK_DESCRIPTOR_TYPE_STORAGE_IMAGE, &out };
    vkUpdateDescriptorSets(g_dev, 3, w, 0, NULL);

    vkCmdBindPipeline(cb, VK_PIPELINE_BIND_POINT_COMPUTE, g_warp_pipeline);
    vkCmdBindDescriptorSets(cb, VK_PIPELINE_BIND_POINT_COMPUTE, g_warp_playout, 0, 1, &g_warp_dset, 0, NULL);
    VkWarpPushConstants pc = { {fw, fh}, {VK_CONCEAL_BLOCK, VK_CONCEAL_BLOCK}, extrapolateT };
    vkCmdPushConstants(cb, g_warp_playout, VK_SHADER_STAGE_COMPUTE_BIT, 0, sizeof(pc), &pc);
    vkCmdDispatch(cb, (fw + 7) / 8, (fh + 7) / 8, 1);
}

// vk_render_frame_conceal is vk_render_frame's counterpart for a
// synthesized, motion-extrapolated frame: same acquire/fence-wait/submit/
// present skeleton, but instead of uploading new pixels it dispatches the
// flow/warp compute passes (against the last two real frames already
// captured by vk_render_frame) and blits g_synth_tex into the swapchain.
// Returns 0 (render nothing this tick) whenever concealment isn't
// applicable -- not enough real-frame history yet, pipelines unavailable,
// or decideConcealment (Go) says the current gap doesn't warrant it -- so
// the caller's existing "no new frame this tick" behavior (skip, try again
// next 8ms poll) is unchanged in all of those cases.
static int vk_render_frame_conceal(void) {
    if (!g_dev || !g_swap) return 0;
    if (!g_conceal_have_prev) return 0;
    if (g_conceal_tex_w <= 0 || g_conceal_tex_h <= 0) return 0;
    if (!vk_conceal_ensure_pipelines()) return 0;
    if (!vk_conceal_ensure_flow_synth(g_conceal_tex_w, g_conceal_tex_h)) {
        goVKLog("vk_conceal_ensure_flow_synth failed -- frame smoothing will not run this tick", 1);
        return 0;
    }

    double elapsedMs = (mono_sec() - g_conceal_last_real_ts) * 1000.0;
    float extrapolateT = 0.0f;
    int conceal = goFrameSmoothingDecide(elapsedMs, g_conceal_expected_ms, g_conceal_consecutive, &extrapolateT);
    if (!conceal) return 0;

    // Per-stall onset used to log unconditionally here -- on a bad
    // connection stalls can fire several times a second, which floods
    // app.log without actually being easier to read than an aggregate.
    // Counted into the periodic summary instead (vk_conceal_maybe_log_summary,
    // ~every 2s) -- see g_conceal_summary_concealed/_t_sum/_t_max below.
    g_conceal_summary_concealed++;
    g_conceal_summary_t_sum += (double)extrapolateT;
    if (extrapolateT > g_conceal_summary_t_max) g_conceal_summary_t_max = extrapolateT;
    g_last_present_t = (double)extrapolateT; // read by the shared stats block IF this tick's remaining GPU steps succeed; harmless if they don't (stats block never runs on failure)

    int curSlot  = g_conceal_cur;
    int prevSlot = 1 - g_conceal_cur;
    int fw = g_conceal_tex_w, fh = g_conceal_tex_h;

    uint32_t img_idx = 0;
    VkResult res = vkAcquireNextImageKHR(g_dev, g_swap, 3000000000ULL, g_img_sem, VK_NULL_HANDLE, &img_idx);
    if (res == VK_TIMEOUT) return 0;
    if (res == VK_ERROR_OUT_OF_DATE_KHR) { vk_recreate_swapchain(); return 0; }
    if (res != VK_SUCCESS && res != VK_SUBOPTIMAL_KHR) return 0;

    if (vkWaitForFences(g_dev, 1, &g_fence, VK_TRUE, 2000000000ULL) == VK_TIMEOUT) {
        vkResetFences(g_dev, 1, &g_fence);
        return 0;
    }
    vkResetFences(g_dev, 1, &g_fence);

    // DEBUG: the fence wait above just proved the PREVIOUS submission (the
    // one that queued a flow readback, if any) fully completed and its
    // writes are host-visible -- safe to map and log now.
    vk_conceal_debug_log_flow_stats();
    vk_conceal_maybe_log_summary();
    vk_conceal_consume_prior();

    vkResetCommandBuffer(g_cmdbuf, 0);
    VkCommandBufferBeginInfo bi = { VK_STRUCTURE_TYPE_COMMAND_BUFFER_BEGIN_INFO };
    bi.flags = VK_COMMAND_BUFFER_USAGE_ONE_TIME_SUBMIT_BIT;
    vkBeginCommandBuffer(g_cmdbuf, &bi);

    if (!g_conceal_flow_fresh) {
        vk_conceal_record_flow(g_cmdbuf, curSlot, prevSlot, fw, fh);
        g_conceal_flow_fresh = 1;
        // Throttled: this readback + its CPU-side scan (vk_conceal_debug_log_flow_stats)
        // runs on the render thread. Queuing it on every stall onset was a
        // real, measured contributor to a "jitter grows when frame smoothing
        // is on" regression, precisely because stalls (and therefore this
        // work) cluster exactly when the network is already struggling.
        // VK_FLOW_DBG_SAMPLE_INTERVAL_SEC-spaced sampling is plenty to spot
        // patterns while staying far rarer than "every stall".
        double now = mono_sec();
        if (now >= g_flow_dbg_next_allowed_ts) {
            g_flow_dbg_t = extrapolateT;
            vk_conceal_debug_queue_flow_readback(g_cmdbuf, g_flow_w, g_flow_h);
            g_flow_dbg_next_allowed_ts = now + VK_FLOW_DBG_SAMPLE_INTERVAL_SEC;
        }
    }
    vk_conceal_record_warp(g_cmdbuf, curSlot, fw, fh, extrapolateT);

    vk_image_barrier(g_cmdbuf, g_synth_tex, VK_IMAGE_LAYOUT_GENERAL, VK_IMAGE_LAYOUT_TRANSFER_SRC_OPTIMAL,
        VK_ACCESS_SHADER_WRITE_BIT, VK_ACCESS_TRANSFER_READ_BIT,
        VK_PIPELINE_STAGE_COMPUTE_SHADER_BIT, VK_PIPELINE_STAGE_TRANSFER_BIT);
    g_synth_layout = VK_IMAGE_LAYOUT_TRANSFER_SRC_OPTIMAL;

    vk_image_barrier(g_cmdbuf, g_swap_imgs[img_idx],
        VK_IMAGE_LAYOUT_UNDEFINED, VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL,
        0, VK_ACCESS_TRANSFER_WRITE_BIT,
        VK_PIPELINE_STAGE_TOP_OF_PIPE_BIT, VK_PIPELINE_STAGE_TRANSFER_BIT);

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
    blt.dstOffsets[0] = (VkOffset3D){dx, dy, 0};
    blt.dstOffsets[1] = (VkOffset3D){dx + dw, dy + dh, 1};
    vkCmdBlitImage(g_cmdbuf,
        g_synth_tex,          VK_IMAGE_LAYOUT_TRANSFER_SRC_OPTIMAL,
        g_swap_imgs[img_idx], VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL,
        1, &blt, VK_FILTER_LINEAR);

    // Net Graph HUD / AI Vision overlay: this path used to blit straight to
    // PRESENT_SRC and skip both draws entirely, so every concealed frame
    // (now a large fraction of frames on a lossy connection -- see
    // frame_smoothing.go) made the HUD visibly blink off, only to reappear
    // on the next real frame via vk_render_frame_vkimage's own calls to
    // these same two functions. Same overlay pass as that path: transition
    // to COLOR_ATTACHMENT_OPTIMAL, LOAD (preserve the blit), draw AI Vision
    // then HUD on top over the identical letterboxed viewport/scissor used
    // for the blit above (vk_hud_record_draw/vk_aivision_record_draw's rect
    // math is relative to fw x fh and doesn't care that this viewport is
    // reused rather than freshly computed for a video draw).
    {
        PFN_vkCmdBeginRendering pfnBeginRendering = (PFN_vkCmdBeginRendering)vkGetDeviceProcAddr(g_dev, "vkCmdBeginRendering");
        PFN_vkCmdEndRendering   pfnEndRendering   = (PFN_vkCmdEndRendering)vkGetDeviceProcAddr(g_dev, "vkCmdEndRendering");
        if (pfnBeginRendering && pfnEndRendering) {
            vk_image_barrier(g_cmdbuf, g_swap_imgs[img_idx],
                VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL, VK_IMAGE_LAYOUT_COLOR_ATTACHMENT_OPTIMAL,
                VK_ACCESS_TRANSFER_WRITE_BIT, VK_ACCESS_COLOR_ATTACHMENT_WRITE_BIT,
                VK_PIPELINE_STAGE_TRANSFER_BIT, VK_PIPELINE_STAGE_COLOR_ATTACHMENT_OUTPUT_BIT);

            VkRenderingAttachmentInfo overlayAtt = { VK_STRUCTURE_TYPE_RENDERING_ATTACHMENT_INFO };
            overlayAtt.imageView = g_swap_views[img_idx];
            overlayAtt.imageLayout = VK_IMAGE_LAYOUT_COLOR_ATTACHMENT_OPTIMAL;
            overlayAtt.loadOp = VK_ATTACHMENT_LOAD_OP_LOAD;
            overlayAtt.storeOp = VK_ATTACHMENT_STORE_OP_STORE;

            VkRenderingInfo overlayInfo = { VK_STRUCTURE_TYPE_RENDERING_INFO };
            overlayInfo.renderArea.extent.width = (uint32_t)sw; overlayInfo.renderArea.extent.height = (uint32_t)sh;
            overlayInfo.layerCount = 1;
            overlayInfo.colorAttachmentCount = 1; overlayInfo.pColorAttachments = &overlayAtt;

            pfnBeginRendering(g_cmdbuf, &overlayInfo);
            VkViewport ovp = { (float)dx, (float)dy, (float)dw, (float)dh, 0.0f, 1.0f };
            VkRect2D osc = { { dx, dy }, { (uint32_t)dw, (uint32_t)dh } };
            vkCmdSetViewport(g_cmdbuf, 0, 1, &ovp);
            vkCmdSetScissor(g_cmdbuf, 0, 1, &osc);
            vk_aivision_record_draw(g_cmdbuf, fw, fh);
            vk_hud_record_draw(g_cmdbuf, fw, fh);
            pfnEndRendering(g_cmdbuf);

            vk_image_barrier(g_cmdbuf, g_swap_imgs[img_idx],
                VK_IMAGE_LAYOUT_COLOR_ATTACHMENT_OPTIMAL, VK_IMAGE_LAYOUT_PRESENT_SRC_KHR,
                VK_ACCESS_COLOR_ATTACHMENT_WRITE_BIT, 0,
                VK_PIPELINE_STAGE_COLOR_ATTACHMENT_OUTPUT_BIT, VK_PIPELINE_STAGE_BOTTOM_OF_PIPE_BIT);
        } else {
            vk_image_barrier(g_cmdbuf, g_swap_imgs[img_idx],
                VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL, VK_IMAGE_LAYOUT_PRESENT_SRC_KHR,
                VK_ACCESS_TRANSFER_WRITE_BIT, 0,
                VK_PIPELINE_STAGE_TRANSFER_BIT, VK_PIPELINE_STAGE_BOTTOM_OF_PIPE_BIT);
        }
    }

    vkEndCommandBuffer(g_cmdbuf);

    VkPipelineStageFlags wait_stage = VK_PIPELINE_STAGE_TRANSFER_BIT;
    VkSubmitInfo si = { VK_STRUCTURE_TYPE_SUBMIT_INFO };
    si.waitSemaphoreCount = 1; si.pWaitSemaphores = &g_img_sem; si.pWaitDstStageMask = &wait_stage;
    si.commandBufferCount = 1; si.pCommandBuffers = &g_cmdbuf;
    si.signalSemaphoreCount = 1; si.pSignalSemaphores = &g_rnd_sem;
    vkQueueSubmit(g_queue, 1, &si, g_fence);

    g_conceal_consecutive++;
    g_stat_concealed_frames++;
    g_stat_concealing = 1;

    VkPresentInfoKHR pi = { VK_STRUCTURE_TYPE_PRESENT_INFO_KHR };
    pi.waitSemaphoreCount = 1; pi.pWaitSemaphores = &g_rnd_sem;
    pi.swapchainCount = 1; pi.pSwapchains = &g_swap; pi.pImageIndices = &img_idx;
    res = vkQueuePresentKHR(g_queue, &pi);
    if (res == VK_ERROR_OUT_OF_DATE_KHR || res == VK_SUBOPTIMAL_KHR) {
        vk_recreate_swapchain();
        return 1;
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
    int last_rendered = 0;
    while (atomic_load(&g_active)) {
        g_render_stage = 0; // idle — waiting for next frame event
        // Wait up to 8ms for a real frame to arrive from the network.
        // Because vkAcquireNextImageKHR blocks until a buffer is free (1 VSync ahead),
        // we have roughly 16.6ms to submit the frame. Waiting 8ms gives the network
        // time to deliver a jittery frame, while leaving ~8ms for the GPU to compute FSR.
        WaitForSingleObject(g_event, 8);
        last_rendered = 0;
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
        int is_concealed_this_tick = 0; // set below in either branch's conceal path; read at the shared stats block
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

            if (img != VK_NULL_HANDLE) {
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
                // No new real frame this tick -- see the identical comment
                // on the RGBA branch below for what this does and why. Must
                // NOT fall into the rf/rel_fn handling above: rel_ctx/rel_fn
                // are unset (NULL) here, and g_vkf_prev_release_ctx/fn must
                // keep whatever the last REAL frame left them as.
                int concealed = atomic_load(&g_conceal_enabled) && vk_render_frame_conceal();
                if (!concealed) continue;
                fw = g_conceal_tex_w; fh = g_conceal_tex_h;
                rf = 1;
                is_concealed_this_tick = 1;
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

            if (!tmp) {
                // No new real frame this tick. Frame smoothing: if enabled
                // and the gap since the last real frame warrants it (Go's
                // decideConcealment, called from inside
                // vk_render_frame_conceal), present a motion-extrapolated
                // frame instead of leaving the display on whatever was last
                // drawn. Falls through to the shared stats block below on
                // success so MaxGapMs/FPS reflect what's actually on
                // screen -- the whole point of this feature is to shrink
                // that gap, not hide it from the HUD.
                int concealed = atomic_load(&g_conceal_enabled) && vk_render_frame_conceal();
                if (!concealed) continue;
                fw = g_conceal_tex_w; fh = g_conceal_tex_h;
                rf = 1;
                is_concealed_this_tick = 1;
            } else {
                g_has_frame = 1;
                g_render_stage = 1; // got frame — entering vk_render_frame
                rf = vk_render_frame(tmp, fw, fh, fs);
                free(tmp);
            }
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
            last_rendered = 1;
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
            // Presentation-timing telemetry, separate from flow-content
            // correctness (nonzero%/avgMag/direction, tracked elsewhere) --
            // requested live 2026-09-19: "your task wasn't to see motion,
            // it was to make sure that motion is UNIFORMLY SMOOTH" --
            // flow being computed correctly doesn't guarantee it's being
            // DISPLAYED at a steady cadence; a micro-freeze is a gap
            // between two consecutive presents (real or concealed, doesn't
            // matter which) that's much bigger than the others, whatever
            // the flow content says. Threshold adapts to the stream's own
            // measured cadence (2x expected) with a 20ms floor so it means
            // roughly the same thing ("missed at least one, probably two,
            // expected updates") across different framerates.
            double stutterThresholdMs = 20.0;
            if (g_conceal_expected_ms > 0.0 && g_conceal_expected_ms * 2.0 > stutterThresholdMs) {
                stutterThresholdMs = g_conceal_expected_ms * 2.0;
            }
            g_conceal_summary_gap_count++;
            g_conceal_summary_gap_sum += (double)gap;
            if ((double)gap > g_conceal_summary_gap_max) g_conceal_summary_gap_max = (double)gap;
            if ((double)gap > stutterThresholdMs) {
                g_conceal_summary_stutter_count++;
                if (g_last_present_was_concealed && is_concealed_this_tick) {
                    g_conceal_summary_stutter_concealed_to_concealed++;
                }
            }
            // Per-present JSONL trace (see goFrameSmoothingTraceWrite's doc
            // comment, frame_smoothing_trace_windows.go) -- one line for
            // EVERY actual presentation, not throttled like the flow-field
            // readback above, so real/concealed distribution and exact
            // wall-clock gaps can be computed precisely offline instead of
            // eyeballed from app.log's own throttled summary.
            goFrameSmoothingTraceWrite(is_concealed_this_tick, g_last_present_t,
                g_conceal_prior_vx, g_conceal_prior_vy,
                g_flow_dbg_sample_avg_vx, g_flow_dbg_sample_avg_vy,
                g_flow_dbg_sample_nonzero_pct, (double)gap);
        }
        g_last_blit_ts = now;
        g_last_present_was_concealed = is_concealed_this_tick;
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

// vk_hud_set_pixels is called from Go (net_graph_windows.go's push hook,
// wired to net_graph.go's ~10Hz netGraphMetalPush) with the freshly built
// HUD canvas. Just copies into g_hud_pixels and marks it dirty for the
// render thread to pick up on its next frame (vk_hud_maybe_upload_cmds) --
// does no Vulkan calls itself, so it's safe to call from any Go goroutine,
// not just the render thread.
int vk_hud_set_pixels(const uint8_t *rgba, int w, int h) {
    if (!g_cs_init) return 0;
    if (w != VK_HUD_W || h != VK_HUD_H) {
        goVKLog("vk_hud_set_pixels: HUD canvas size mismatch (net_graph.go's netGraphCanvasW/H changed?)", 2);
        return 0;
    }
    EnterCriticalSection(&g_cs);
    memcpy(g_hud_pixels, rgba, sizeof(g_hud_pixels));
    g_hud_dirty  = 1;
    g_hud_active = 1;
    LeaveCriticalSection(&g_cs);
    return 1;
}

// vk_hud_clear is called from Go when Net Graph is disabled (net_graph.go's
// netGraphMetalClear hook) -- stops the render thread from drawing the (now
// stale) HUD texture. Leaves the pipeline/texture resources alone so
// re-enabling doesn't need to recreate them.
void vk_hud_clear(void) {
    if (!g_cs_init) return;
    EnterCriticalSection(&g_cs);
    g_hud_active = 0;
    g_hud_dirty  = 0;
    LeaveCriticalSection(&g_cs);
}

// vk_aivision_set_pixels is AI Vision's counterpart to vk_hud_set_pixels --
// called from pushAIVisionOverlayToVulkan once per completed detection pass
// (~0.5-2Hz, not per frame) with a fully-rendered w x h RGBA canvas (mostly
// transparent, boxes+tags drawn opaque -- see buildAIVisionOverlayImage).
// Heap-allocates/grows g_aivision_pixels on demand since, unlike the HUD's
// fixed small canvas, this is sized to the live video resolution. Just
// copies and marks dirty; the actual GPU upload happens lazily on the
// render thread's next frame (vk_aivision_maybe_upload_cmds).
int vk_aivision_set_pixels(const uint8_t *rgba, int w, int h) {
    if (!g_cs_init || w <= 0 || h <= 0) return 0;
    size_t sz = (size_t)w * (size_t)h * 4;
    EnterCriticalSection(&g_cs);
    if (!g_aivision_pixels || g_aivision_pixels_sz < sz) {
        uint8_t *grown = (uint8_t*)realloc(g_aivision_pixels, sz);
        if (!grown) {
            LeaveCriticalSection(&g_cs);
            return 0;
        }
        g_aivision_pixels = grown;
        g_aivision_pixels_sz = sz;
    }
    memcpy(g_aivision_pixels, rgba, sz);
    g_aivision_pending_w = w; g_aivision_pending_h = h;
    g_aivision_dirty  = 1;
    g_aivision_active = 1;
    LeaveCriticalSection(&g_cs);
    return 1;
}

// vk_aivision_clear is AI Vision's counterpart to vk_hud_clear -- called
// from Go when the checkbox is turned off, so the render thread stops
// drawing the (now stale) detection boxes. Leaves the texture/pipeline
// resources alone so re-enabling doesn't need to recreate them.
void vk_aivision_clear(void) {
    if (!g_cs_init) return;
    EnterCriticalSection(&g_cs);
    g_aivision_active = 0;
    g_aivision_dirty  = 0;
    LeaveCriticalSection(&g_cs);
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

        // Frame smoothing resources.
        for (int i = 0; i < 2; i++) {
            if (g_conceal_tex_view[i]) { vkDestroyImageView(g_dev, g_conceal_tex_view[i], NULL); g_conceal_tex_view[i] = VK_NULL_HANDLE; }
            if (g_conceal_tex_mem[i])  { vkFreeMemory(g_dev, g_conceal_tex_mem[i], NULL); g_conceal_tex_mem[i] = VK_NULL_HANDLE; }
            if (g_conceal_tex[i])      { vkDestroyImage(g_dev, g_conceal_tex[i], NULL); g_conceal_tex[i] = VK_NULL_HANDLE; }
            g_conceal_tex_layout[i] = VK_IMAGE_LAYOUT_UNDEFINED;
        }
        g_conceal_tex_w = 0; g_conceal_tex_h = 0;
        g_conceal_cur = 0; g_conceal_have_prev = 0; g_conceal_capture_count = 0;
        g_conceal_last_real_ts = 0.0; g_conceal_expected_ms = 0.0;
        g_conceal_consecutive = 0; g_conceal_flow_fresh = 0;
        if (g_flow_view)  { vkDestroyImageView(g_dev, g_flow_view, NULL); g_flow_view = VK_NULL_HANDLE; }
        if (g_flow_mem)   { vkFreeMemory(g_dev, g_flow_mem, NULL); g_flow_mem = VK_NULL_HANDLE; }
        if (g_flow_tex)   { vkDestroyImage(g_dev, g_flow_tex, NULL); g_flow_tex = VK_NULL_HANDLE; }
        g_flow_w = 0; g_flow_h = 0;
        if (g_synth_view) { vkDestroyImageView(g_dev, g_synth_view, NULL); g_synth_view = VK_NULL_HANDLE; }
        if (g_synth_mem)  { vkFreeMemory(g_dev, g_synth_mem, NULL); g_synth_mem = VK_NULL_HANDLE; }
        if (g_synth_tex)  { vkDestroyImage(g_dev, g_synth_tex, NULL); g_synth_tex = VK_NULL_HANDLE; }
        g_synth_layout = VK_IMAGE_LAYOUT_UNDEFINED;
        if (g_flow_pipeline) { vkDestroyPipeline(g_dev, g_flow_pipeline, NULL); g_flow_pipeline = VK_NULL_HANDLE; }
        if (g_warp_pipeline) { vkDestroyPipeline(g_dev, g_warp_pipeline, NULL); g_warp_pipeline = VK_NULL_HANDLE; }
        if (g_flow_playout)  { vkDestroyPipelineLayout(g_dev, g_flow_playout, NULL); g_flow_playout = VK_NULL_HANDLE; }
        if (g_warp_playout)  { vkDestroyPipelineLayout(g_dev, g_warp_playout, NULL); g_warp_playout = VK_NULL_HANDLE; }
        if (g_conceal_dpool) { vkDestroyDescriptorPool(g_dev, g_conceal_dpool, NULL); g_conceal_dpool = VK_NULL_HANDLE; }
        if (g_flow_dsl)      { vkDestroyDescriptorSetLayout(g_dev, g_flow_dsl, NULL); g_flow_dsl = VK_NULL_HANDLE; }
        if (g_warp_dsl)      { vkDestroyDescriptorSetLayout(g_dev, g_warp_dsl, NULL); g_warp_dsl = VK_NULL_HANDLE; }
        if (g_conceal_sampler) { vkDestroySampler(g_dev, g_conceal_sampler, NULL); g_conceal_sampler = VK_NULL_HANDLE; }
        g_flow_dset = VK_NULL_HANDLE; g_warp_dset = VK_NULL_HANDLE;
        g_conceal_pipelines_ok = 0;
        g_stat_concealed_frames = 0; g_stat_concealing = 0;

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

        // Net Graph HUD overlay resources.
        if (g_hud_stage_ptr && g_hud_stage_mem) { vkUnmapMemory(g_dev, g_hud_stage_mem); g_hud_stage_ptr = NULL; }
        if (g_hud_stage_buf) { vkDestroyBuffer(g_dev, g_hud_stage_buf, NULL); g_hud_stage_buf = VK_NULL_HANDLE; }
        if (g_hud_stage_mem) { vkFreeMemory(g_dev, g_hud_stage_mem, NULL);  g_hud_stage_mem = VK_NULL_HANDLE; }
        if (g_hud_tex_view)  { vkDestroyImageView(g_dev, g_hud_tex_view, NULL); g_hud_tex_view = VK_NULL_HANDLE; }
        if (g_hud_tex)       { vkDestroyImage(g_dev, g_hud_tex, NULL);      g_hud_tex = VK_NULL_HANDLE; }
        if (g_hud_tex_mem)   { vkFreeMemory(g_dev, g_hud_tex_mem, NULL);    g_hud_tex_mem = VK_NULL_HANDLE; }
        if (g_hud_pipeline)  { vkDestroyPipeline(g_dev, g_hud_pipeline, NULL); g_hud_pipeline = VK_NULL_HANDLE; }
        if (g_hud_playout)   { vkDestroyPipelineLayout(g_dev, g_hud_playout, NULL); g_hud_playout = VK_NULL_HANDLE; }
        if (g_hud_dpool)     { vkDestroyDescriptorPool(g_dev, g_hud_dpool, NULL); g_hud_dpool = VK_NULL_HANDLE; }
        if (g_hud_dsl)       { vkDestroyDescriptorSetLayout(g_dev, g_hud_dsl, NULL); g_hud_dsl = VK_NULL_HANDLE; }
        if (g_hud_sampler)   { vkDestroySampler(g_dev, g_hud_sampler, NULL); g_hud_sampler = VK_NULL_HANDLE; }
        g_hud_dset = VK_NULL_HANDLE;
        g_hud_tex_layout = VK_IMAGE_LAYOUT_UNDEFINED;
        g_hud_resources_ok = 0;
        g_hud_active = 0; g_hud_dirty = 0;

        // AI Vision overlay resources (g_hud_dpool/dsl/pipeline/playout/
        // sampler above are shared and already torn down; g_aivision_dset
        // is freed implicitly along with that pool).
        if (g_aivision_stage_ptr && g_aivision_stage_mem) { vkUnmapMemory(g_dev, g_aivision_stage_mem); g_aivision_stage_ptr = NULL; }
        if (g_aivision_stage_buf) { vkDestroyBuffer(g_dev, g_aivision_stage_buf, NULL); g_aivision_stage_buf = VK_NULL_HANDLE; }
        if (g_aivision_stage_mem) { vkFreeMemory(g_dev, g_aivision_stage_mem, NULL);  g_aivision_stage_mem = VK_NULL_HANDLE; }
        g_aivision_stage_sz = 0;
        if (g_aivision_tex_view) { vkDestroyImageView(g_dev, g_aivision_tex_view, NULL); g_aivision_tex_view = VK_NULL_HANDLE; }
        if (g_aivision_tex)      { vkDestroyImage(g_dev, g_aivision_tex, NULL);      g_aivision_tex = VK_NULL_HANDLE; }
        if (g_aivision_tex_mem)  { vkFreeMemory(g_dev, g_aivision_tex_mem, NULL);    g_aivision_tex_mem = VK_NULL_HANDLE; }
        g_aivision_dset = VK_NULL_HANDLE;
        g_aivision_tex_layout = VK_IMAGE_LAYOUT_UNDEFINED;
        g_aivision_tex_w = 0; g_aivision_tex_h = 0;
        g_aivision_active = 0; g_aivision_dirty = 0;
        free(g_aivision_pixels); g_aivision_pixels = NULL; g_aivision_pixels_sz = 0;
        g_aivision_pending_w = 0; g_aivision_pending_h = 0;

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
    g_stat_fps = 0; g_stat_fps_ready = 0; // fresh session: don't show a stale FPS from a previous one
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

// vk_video_clear_pending_stats consumes the one-shot "did X happen since
// last check" event flags a caller just logged (video_widget_windows.go's
// RunNative poll: "first frame rendered", "SLOW ..." gap warnings). It does
// NOT clear g_stat_fps_ready/g_stat_fps: FPS is a continuously-valid gauge
// (see the ~5s recompute above), not a one-shot event -- clearing it here
// used to mean whichever of RunNative's poll or net_graph.go's own
// independent ~10Hz VKVideoGetStats() poll happened to run right after the
// other had just logged "fps=..." would see fps_ready=false and read 0,
// making the HUD's FPS line visibly flicker to 0 between every real ~5s
// update even though nothing was actually wrong.
void vk_video_clear_pending_stats(void) {
    g_stat_first      = 0;
    g_stat_max_gap_ms = 0.0f;
}

// Returns render-thread heartbeat (increments every loop iteration) and current stage.
// Go watchdog: if heartbeat stops advancing the render thread is stuck; stage tells where.
void vk_video_get_diag(long long *hb, int *stage) {
    *hb    = g_render_hb;
    *stage = g_render_stage;
}

// vk_video_set_concealment_enabled toggles frame smoothing (see the "frame
// smoothing" section above vk_render_thread). Called from
// frame_smoothing_windows.go's init() hook, wired to
// SetFrameSmoothingEnabled (frame_smoothing.go). Safe from any thread: the
// render thread only ever reads g_conceal_enabled.
void vk_video_set_concealment_enabled(int enabled) {
    atomic_store(&g_conceal_enabled, enabled ? 1 : 0);
}

// vk_video_get_conceal_stats returns the running count of synthesized
// (motion-extrapolated) frames presented so far and whether the render
// thread is in the middle of concealing a stall right now -- surfaced on
// the Net Graph HUD (see net_graph.go/net_graph_windows.go) as the
// before/after signal for how often this feature is actually kicking in.
void vk_video_get_conceal_stats(long long *concealed_frames, int *concealing) {
    *concealed_frames = g_stat_concealed_frames;
    *concealing       = g_stat_concealing;
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
