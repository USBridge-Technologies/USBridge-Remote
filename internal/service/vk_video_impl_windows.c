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

typedef struct { HWND parent; int x, y, w, h; } VkWndArgs;
static VkWndArgs g_hwnd_args;

static LRESULT CALLBACK vk_wnd_proc(HWND hw, UINT msg, WPARAM wp, LPARAM lp) {
    if (msg == WM_ERASEBKGND) return 1;
    // Don't let the overlay steal keyboard focus on click.
    if (msg == WM_MOUSEACTIVATE) return MA_NOACTIVATE;
    // Raw Input: fires on every hardware mouse sample (no WM_MOUSEMOVE coalescing).
    // RIDEV_INPUTSINK delivers even when not foreground; we filter by foreground window
    // so we don't intercept input from other applications.
    if (msg == WM_INPUT) {
        HWND _fg = GetForegroundWindow();
        if (g_parent_hwnd && (_fg == g_parent_hwnd || _fg == g_child_hwnd)) {
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
                            vk_eq_push(1, cur.x, cur.y, 0);
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
    if (msg == WM_LBUTTONDOWN) { vk_eq_push(2, (int)(short)LOWORD(lp), (int)(short)HIWORD(lp), 1); return 0; }
    if (msg == WM_LBUTTONUP)   { vk_eq_push(3, (int)(short)LOWORD(lp), (int)(short)HIWORD(lp), 1); return 0; }
    if (msg == WM_RBUTTONDOWN) { vk_eq_push(2, (int)(short)LOWORD(lp), (int)(short)HIWORD(lp), 3); return 0; }
    if (msg == WM_RBUTTONUP)   { vk_eq_push(3, (int)(short)LOWORD(lp), (int)(short)HIWORD(lp), 3); return 0; }
    if (msg == WM_MBUTTONDOWN) { vk_eq_push(2, (int)(short)LOWORD(lp), (int)(short)HIWORD(lp), 2); return 0; }
    if (msg == WM_MBUTTONUP)   { vk_eq_push(3, (int)(short)LOWORD(lp), (int)(short)HIWORD(lp), 2); return 0; }
    if (msg == WM_MOUSEWHEEL) {
        // Encode scroll as button 4 (up) / 5 (down) — same convention as Linux X11.
        int btn = GET_WHEEL_DELTA_WPARAM(wp) > 0 ? 4 : 5;
        POINT pt = { (int)(short)LOWORD(lp), (int)(short)HIWORD(lp) };
        if (hw) ScreenToClient(hw, &pt);
        vk_eq_push(2, pt.x, pt.y, btn);
        return 0;
    }
    // WM_USER+1/+2: hide/show requests posted by the render thread.
    if (msg == WM_USER+1) { ShowWindow(hw, SW_HIDE);           return 0; }
    if (msg == WM_USER+2) { ShowWindow(hw, SW_SHOWNOACTIVATE); return 0; }
    if (msg == WM_CLOSE)   { DestroyWindow(hw); return 0; }
    if (msg == WM_DESTROY) { PostQuitMessage(0); return 0; }
    return DefWindowProcW(hw, msg, wp, lp);
}

static DWORD WINAPI vk_hwnd_thread(LPVOID unused) {
    (void)unused;
    POINT pt = { g_hwnd_args.x, g_hwnd_args.y };
    if (g_hwnd_args.parent) ClientToScreen(g_hwnd_args.parent, &pt);
    int cw = g_hwnd_args.w > 0 ? g_hwnd_args.w : 1;
    int ch = g_hwnd_args.h > 0 ? g_hwnd_args.h : 1;

    // Create overlay on THIS thread — own message queue, independent DWM context.
    // WS_EX_TOPMOST keeps the overlay above Fyne without sharing its present queue.
    // WS_EX_NOACTIVATE prevents stealing keyboard focus on click.
    // Mouse events are captured by vk_wnd_proc (no WS_EX_TRANSPARENT) and queued
    // for the Go polling goroutine — same pattern as the Linux X11 implementation.
    // No owner (NULL hwndParent) decouples from Fyne's present queue entirely.
    g_child_hwnd = CreateWindowExW(
        WS_EX_NOACTIVATE | WS_EX_TOPMOST,
        L"usbridgeVKVideo", L"",
        WS_POPUP | WS_VISIBLE,
        pt.x, pt.y, cw, ch,
        NULL,                          // no owner — independent DWM context
        NULL, GetModuleHandleW(NULL), NULL);

    SetEvent(g_hwnd_ready);            // wake vk_video_create (with or without HWND)
    if (!g_child_hwnd) return 1;

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

    // Present mode preference: IMMEDIATE → MAILBOX → FIFO_RELAXED → FIFO.
    // With a WS_POPUP overlay (independent DWM window), IMMEDIATE is safe and preferred:
    // vkQueuePresentKHR returns without waiting for DWM vsync, so it cannot block
    // or deadlock against Fyne's wglSwapBuffers on the parent window.
    // FIFO / FIFO_RELAXED both involve DWM vsync messaging which can stall the parent
    // window's message pump even when the overlay window is a separate WS_POPUP.
    uint32_t npm = 0;
    vkGetPhysicalDeviceSurfacePresentModesKHR(g_pdev, g_surf, &npm, NULL);
    VkPresentModeKHR *pms = (VkPresentModeKHR*)malloc(npm * sizeof(*pms));
    vkGetPhysicalDeviceSurfacePresentModesKHR(g_pdev, g_surf, &npm, pms);
    VkPresentModeKHR pm = VK_PRESENT_MODE_FIFO_KHR; // safe fallback
    for (uint32_t i = 0; i < npm; i++) {
        if (pms[i] == VK_PRESENT_MODE_IMMEDIATE_KHR)    { pm = pms[i]; break; }
    }
    if (pm != VK_PRESENT_MODE_IMMEDIATE_KHR) {
        for (uint32_t i = 0; i < npm; i++) {
            if (pms[i] == VK_PRESENT_MODE_MAILBOX_KHR)      { pm = pms[i]; break; }
            if (pms[i] == VK_PRESENT_MODE_FIFO_RELAXED_KHR) { pm = pms[i]; }
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
    ici.format      = VK_FORMAT_R8G8B8A8_UNORM; // GStreamer/Moonlight always RGBA
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

    // Clear black bars via vkCmdClearColorImage before blit.
    vk_image_barrier(g_cmdbuf, g_swap_imgs[img_idx],
        VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL, VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL,
        VK_ACCESS_TRANSFER_WRITE_BIT, VK_ACCESS_TRANSFER_WRITE_BIT,
        VK_PIPELINE_STAGE_TRANSFER_BIT, VK_PIPELINE_STAGE_TRANSFER_BIT);
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

        uint8_t *tmp = NULL;
        int fw = 0, fh = 0, fs = 0;
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

void vk_video_update_frame(int x, int y, int w, int h) {
    if (!atomic_load(&g_active)) return;
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
        if (g_img_sem) { vkDestroySemaphore(g_dev, g_img_sem, NULL); g_img_sem = VK_NULL_HANDLE; }
        if (g_rnd_sem) { vkDestroySemaphore(g_dev, g_rnd_sem, NULL); g_rnd_sem = VK_NULL_HANDLE; }
        if (g_fence)   { vkDestroyFence(g_dev, g_fence, NULL);       g_fence = VK_NULL_HANDLE; }
        if (g_cmdbuf && g_cmdpool) { vkFreeCommandBuffers(g_dev, g_cmdpool, 1, &g_cmdbuf); g_cmdbuf = VK_NULL_HANDLE; }
        if (g_cmdpool) { vkDestroyCommandPool(g_dev, g_cmdpool, NULL); g_cmdpool = VK_NULL_HANDLE; }
        vk_destroy_swapchain();
        if (g_surf) { vkDestroySurfaceKHR(g_inst, g_surf, NULL); g_surf = VK_NULL_HANDLE; }
        vkDestroyDevice(g_dev, NULL); g_dev = VK_NULL_HANDLE;
    }
    if (g_inst) { vkDestroyInstance(g_inst, NULL); g_inst = VK_NULL_HANDLE; }
    g_pdev = VK_NULL_HANDLE;

    // Destroy overlay window: post WM_CLOSE to its owning thread (vk_hwnd_thread).
    // DestroyWindow from a different thread is not allowed; WM_CLOSE triggers
    // DestroyWindow from within vk_wnd_proc on the correct thread.
    if (g_child_hwnd) { PostMessageW(g_child_hwnd, WM_CLOSE, 0, 0); g_child_hwnd = NULL; }
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
    g_parent_hwnd = NULL;
    g_raw_mouse = 0;
    g_rendered = 0; g_submitted = 0;
}

// vk_video_create — initialise Vulkan renderer.
// Returns 1 on success, 0 if Vulkan is unavailable (caller falls back to GDI).
int vk_video_create(uintptr_t parent_hwnd, int x, int y, int w, int h) {
    if (atomic_load(&g_active)) vk_full_cleanup();

    HWND parent = (HWND)(uintptr_t)parent_hwnd;
    if (!parent) { goVKLog("vk_video_create: parent HWND is null", 2); return 0; }
    g_parent_hwnd = parent;

    // Register overlay window class once.
    if (!g_wndcls) {
        WNDCLASSEXW wc = { sizeof(wc) };
        wc.lpfnWndProc   = vk_wnd_proc;
        wc.hInstance     = GetModuleHandleW(NULL);
        wc.lpszClassName = L"usbridgeVKVideo";
        wc.hbrBackground = (HBRUSH)GetStockObject(BLACK_BRUSH);
        wc.hCursor       = LoadCursor(NULL, IDC_ARROW); // show arrow cursor over video area
        g_wndcls = RegisterClassExW(&wc);
        if (!g_wndcls) { goVKLog("vk_video_create: RegisterClass failed", 2); return 0; }
    }

    // Create overlay HWND on a dedicated Win32 thread (vk_hwnd_thread).
    // That thread runs its own GetMessage/DispatchMessage pump, giving the overlay
    // a separate Windows input queue and independent DWM present context.
    // This prevents DWM from serialising our vkQueuePresentKHR with Fyne's
    // wglSwapBuffers — the root cause of the permanent Fyne-main-loop freeze on
    // AMD iGPUs where OpenGL and Vulkan share a single hardware present queue.
    g_hwnd_ready = CreateEventW(NULL, FALSE, FALSE, NULL);
    if (!g_hwnd_ready) { goVKLog("vk_video_create: CreateEvent failed", 2); return 0; }
    g_hwnd_args.parent = parent; g_hwnd_args.x = x; g_hwnd_args.y = y;
    g_hwnd_args.w = w; g_hwnd_args.h = h;
    g_hwnd_thread = CreateThread(NULL, 0, vk_hwnd_thread, NULL, 0, NULL);
    if (!g_hwnd_thread) {
        CloseHandle(g_hwnd_ready); g_hwnd_ready = NULL;
        goVKLog("vk_video_create: CreateThread(hwnd) failed", 2); return 0;
    }
    if (WaitForSingleObject(g_hwnd_ready, 5000) != WAIT_OBJECT_0 || !g_child_hwnd) {
        // Window thread failed to create the HWND within 5 s.
        goVKLog("vk_video_create: overlay window creation failed/timeout", 2);
        CloseHandle(g_hwnd_ready); g_hwnd_ready = NULL;
        if (g_hwnd_thread) { WaitForSingleObject(g_hwnd_thread, 1000); CloseHandle(g_hwnd_thread); g_hwnd_thread = NULL; }
        return 0;
    }
    CloseHandle(g_hwnd_ready); g_hwnd_ready = NULL;

    // Vulkan init
    if (!vk_create_instance()) { goVKLog("vk_video_create: vkCreateInstance failed", 2); goto fail; }
    if (!vk_select_device())   { goVKLog("vk_video_create: no suitable GPU found", 2);   goto fail; }
    if (!vk_create_device())   { goVKLog("vk_video_create: vkCreateDevice failed", 2);   goto fail; }

    {
        VkWin32SurfaceCreateInfoKHR sci = { VK_STRUCTURE_TYPE_WIN32_SURFACE_CREATE_INFO_KHR };
        sci.hinstance = GetModuleHandleW(NULL);
        sci.hwnd      = g_child_hwnd;
        if (vkCreateWin32SurfaceKHR(g_inst, &sci, NULL, &g_surf) != VK_SUCCESS) {
            goVKLog("vk_video_create: vkCreateWin32SurfaceKHR failed", 2); goto fail;
        }
    }

    if (!vk_create_swapchain(w > 0 ? w : 1, h > 0 ? h : 1)) {
        goVKLog("vk_video_create: swapchain creation failed", 2); goto fail;
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
    g_eq_head = 0; g_eq_tail = 0; // reset event queue
    g_event = CreateEventW(NULL, FALSE, FALSE, NULL);
    if (!g_event) goto fail;

    g_submitted = 0; g_rendered = 0; g_fps_n = 0; g_fps_t0 = 0;
    g_ready = 0; g_has_frame = 0; g_stat_first = 0;
    g_stat_max_gap_ms = 0; g_last_blit_ts = 0;
    atomic_store(&g_active, 1);

    g_thread = CreateThread(NULL, 0, vk_render_thread, NULL, 0, NULL);
    if (!g_thread) {
        atomic_store(&g_active, 0);
        goto fail;
    }

    {
        char m[256];
        VkPhysicalDeviceProperties pr;
        vkGetPhysicalDeviceProperties(g_pdev, &pr);
        snprintf(m, sizeof(m), "Vulkan renderer created — GPU=%s rect=(%d,%d,%dx%d)", pr.deviceName, x, y, w, h);
        goVKLog(m, 0);
    }
    return 1;

fail:
    vk_full_cleanup();
    return 0;
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
// On the 2nd+ fullscreen entry the Fyne GLFW window calls SetForegroundWindow /
// BringWindowToTop (via glfwFocusWindow inside RequestFocus) ~500 ms after show,
// which promotes it above our TOPMOST overlay and causes a black screen.
// Calling this after RequestFocus brings the overlay back to the front.
void vk_video_bring_to_top(void) {
    HWND hw = g_child_hwnd;
    if (hw && atomic_load(&g_active)) {
        goVKLog((char*)"vk_video_bring_to_top: re-asserting HWND_TOPMOST", 0);
        SetWindowPos(hw, HWND_TOPMOST, 0, 0, 0, 0,
                     SWP_NOMOVE | SWP_NOSIZE | SWP_NOACTIVATE | SWP_ASYNCWINDOWPOS);
    } else {
        char m[80];
        snprintf(m, sizeof(m), "vk_video_bring_to_top: no-op (hw=%p active=%d)", hw, (int)atomic_load(&g_active));
        goVKLog(m, 1);
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

#endif // _WIN32
