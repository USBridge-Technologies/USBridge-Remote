// Raw relative mouse capture for Capture mode on Windows.
//
// Mirrors vk_video_impl_windows.c's own Raw Input idiom (RIDEV_INPUTSINK,
// same RAWINPUTDEVICE setup) but targets Fyne's own GLFW window directly
// instead of a separate WS_POPUP overlay, since Capture mode runs in the
// normal windowed/fullscreen-fallback view, not the exclusive Vulkan
// fullscreen path.
//
// WM_INPUT is observed via a WH_GETMESSAGE hook on the window's own thread
// rather than by subclassing its WNDPROC: a hook only watches messages
// already being pumped by that thread's own GetMessage/PeekMessage loop (in
// this same process), so it can't collide with however GLFW/Fyne structure
// their own message handling, and -- same as Raw Input itself -- Windows
// has no user-facing permission gate for either API.

#ifdef _WIN32

#define WIN32_LEAN_AND_MEAN
#include <windows.h>
#include <stdint.h>

#include "mouse_capture_windows.h"

extern void goMouseCaptureMove(int dx, int dy);
extern void goMouseCaptureButton(int button, int pressed);
extern void goMouseCaptureScroll(int delta);

static HWND g_hwnd = NULL;
static HHOOK g_hook = NULL;
static int g_raw_registered = 0;
static int g_cursor_hidden = 0;
static int g_active = 0;

// Only set for an absolute-reporting device (touchpad/RDP/VM/tablet) --
// Raw Input gives us its absolute sample each time, not a delta, so we
// diff successive samples ourselves. -1 means "no previous sample yet".
static int g_last_abs_x = -1;
static int g_last_abs_y = -1;

static void usbridge_handle_rawmouse(RAWMOUSE *rm) {
    if (rm->usFlags & MOUSE_MOVE_ABSOLUTE) {
        BOOL vd = (rm->usFlags & MOUSE_VIRTUAL_DESKTOP) != 0;
        int x = MulDiv((int)rm->lLastX, GetSystemMetrics(vd ? SM_CXVIRTUALSCREEN : SM_CXSCREEN), 65535);
        int y = MulDiv((int)rm->lLastY, GetSystemMetrics(vd ? SM_CYVIRTUALSCREEN : SM_CYSCREEN), 65535);
        if (g_last_abs_x >= 0) {
            int dx = x - g_last_abs_x;
            int dy = y - g_last_abs_y;
            if (dx != 0 || dy != 0) {
                goMouseCaptureMove(dx, dy);
            }
        }
        g_last_abs_x = x;
        g_last_abs_y = y;
    } else if (rm->lLastX != 0 || rm->lLastY != 0) {
        goMouseCaptureMove((int)rm->lLastX, (int)rm->lLastY);
    }

    // Button numbers match Limelight.h's BUTTON_LEFT/MIDDLE/RIGHT (1/2/3)
    // directly.
    USHORT bf = rm->usButtonFlags;
    if (bf & RI_MOUSE_LEFT_BUTTON_DOWN)   goMouseCaptureButton(1, 1);
    if (bf & RI_MOUSE_LEFT_BUTTON_UP)     goMouseCaptureButton(1, 0);
    if (bf & RI_MOUSE_RIGHT_BUTTON_DOWN)  goMouseCaptureButton(3, 1);
    if (bf & RI_MOUSE_RIGHT_BUTTON_UP)    goMouseCaptureButton(3, 0);
    if (bf & RI_MOUSE_MIDDLE_BUTTON_DOWN) goMouseCaptureButton(2, 1);
    if (bf & RI_MOUSE_MIDDLE_BUTTON_UP)   goMouseCaptureButton(2, 0);
    if (bf & RI_MOUSE_WHEEL) {
        goMouseCaptureScroll((int)((SHORT)rm->usButtonData) / WHEEL_DELTA);
    }
}

static LRESULT CALLBACK usbridge_get_msg_hook(int code, WPARAM wp, LPARAM lp) {
    if (code == HC_ACTION && wp == PM_REMOVE && lp != 0) {
        MSG *msg = (MSG *)lp;
        if (g_active && msg->message == WM_INPUT && msg->hwnd == g_hwnd) {
            UINT sz = 0;
            GetRawInputData((HRAWINPUT)msg->lParam, RID_INPUT, NULL, &sz, sizeof(RAWINPUTHEADER));
            if (sz > 0 && sz <= 256) {
                BYTE buf[256];
                if (GetRawInputData((HRAWINPUT)msg->lParam, RID_INPUT, buf, &sz, sizeof(RAWINPUTHEADER)) != (UINT)-1) {
                    RAWINPUT *ri = (RAWINPUT *)buf;
                    if (ri->header.dwType == RIM_TYPEMOUSE) {
                        usbridge_handle_rawmouse(&ri->data.mouse);
                    }
                }
            }
        }
    }
    return CallNextHookEx(g_hook, code, wp, lp);
}

int usbridge_mouse_capture_start(uintptr_t hwndPtr) {
    if (g_active) {
        return 1;
    }
    HWND hwnd = (HWND)hwndPtr;
    if (hwnd == NULL) {
        return 0;
    }

    RAWINPUTDEVICE rid;
    ZeroMemory(&rid, sizeof(rid));
    rid.usUsagePage = 0x01; // HID_USAGE_PAGE_GENERIC
    rid.usUsage     = 0x02; // HID_USAGE_GENERIC_MOUSE
    rid.dwFlags     = RIDEV_INPUTSINK;
    rid.hwndTarget  = hwnd;
    if (!RegisterRawInputDevices(&rid, 1, sizeof(rid))) {
        return 0;
    }
    g_raw_registered = 1;

    DWORD threadId = GetWindowThreadProcessId(hwnd, NULL);
    g_hook = SetWindowsHookExW(WH_GETMESSAGE, usbridge_get_msg_hook, NULL, threadId);
    if (g_hook == NULL) {
        RAWINPUTDEVICE unrid;
        ZeroMemory(&unrid, sizeof(unrid));
        unrid.usUsagePage = 0x01;
        unrid.usUsage     = 0x02;
        unrid.dwFlags     = RIDEV_REMOVE;
        RegisterRawInputDevices(&unrid, 1, sizeof(unrid));
        g_raw_registered = 0;
        return 0;
    }

    g_hwnd = hwnd;
    g_last_abs_x = -1;
    g_last_abs_y = -1;
    g_active = 1;
    ShowCursor(FALSE);
    g_cursor_hidden = 1;
    // Keep the (hidden) pointer inside the window: without a clip it slips
    // out of the window edge while the deltas keep going to the remote, and
    // clicks then land on whatever is under it locally. Windows lifts the
    // clip by itself when the window loses activation.
    {
        RECT rc;
        POINT tl = {0, 0};
        if (GetClientRect(hwnd, &rc) && ClientToScreen(hwnd, &tl)) {
            RECT clip = { tl.x, tl.y, tl.x + rc.right, tl.y + rc.bottom };
            ClipCursor(&clip);
        }
    }
    return 1;
}

void usbridge_mouse_capture_stop(void) {
    g_active = 0;
    if (g_hook != NULL) {
        UnhookWindowsHookEx(g_hook);
        g_hook = NULL;
    }
    if (g_raw_registered) {
        RAWINPUTDEVICE unrid;
        ZeroMemory(&unrid, sizeof(unrid));
        unrid.usUsagePage = 0x01;
        unrid.usUsage     = 0x02;
        unrid.dwFlags     = RIDEV_REMOVE;
        RegisterRawInputDevices(&unrid, 1, sizeof(unrid));
        g_raw_registered = 0;
    }
    g_hwnd = NULL;
    ClipCursor(NULL);
    if (g_cursor_hidden) {
        ShowCursor(TRUE);
        g_cursor_hidden = 0;
    }
}

#endif
