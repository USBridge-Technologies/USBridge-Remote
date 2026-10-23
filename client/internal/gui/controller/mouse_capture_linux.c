//go:build linux && !android

// Raw relative mouse capture for Capture mode on X11.
//
// go/build's filename-based matching treats "_linux" C/H files as valid for
// GOOS=android too (Android's kernel is Linux) -- unlike mouse_capture_linux.go,
// this file has no build-tag comment of its own to override that, so without
// the explicit tag above it gets pulled into Android builds and fails on
// missing X11/Xlib.h. Same fix v4l2camera_impl_linux.c/.h already carry.
//
// The pointer is read as XInput2 raw events (XI_RawMotion, XI_RawButtonPress/
// Release) selected on the root window. Not core events on the Fyne window:
// ButtonPress is an exclusive selection in X11 (one client per window), GLFW
// already holds it, and asking for it from a second connection is BadAccess --
// which Xlib's default error handler turns into exit() of the whole client.
// The video overlay (vk_video_impl_linux.c) is also a child window with its
// own selections, so core motion over the picture never reaches the parent.
// Raw events have neither problem and are true relative deltas. libXi is
// dlopen()ed (GLFW needs it anyway), so the build gains no link dependency.
//
// The deltas are the device's unaccelerated raw values: the host applies its
// own pointer acceleration, and a second one here made the cursor feel
// accelerated and laggy. The pointer is grabbed with a blank cursor and
// confined to the window, so it neither shows nor wanders out; raw events
// (XI 2.1+) still arrive during a grab, so no recentring warp is needed.
// Raw events are global, so they are forwarded only while the window has the
// input focus, and the grab is dropped while it does not.
//
// IMPORTANT Xlib footgun: if one thread is blocked in Xlib on a Display and
// another calls XCloseDisplay on it, Xlib's IO error handler exit()s the
// process. So stop() wakes the event thread through a pipe (the thread polls
// the X connection and the pipe), joins it, and only then closes the display.

#ifdef __linux__

#include <X11/Xlib.h>
#include <X11/extensions/XInput2.h>
#include <dlfcn.h>
#include <poll.h>
#include <pthread.h>
#include <stdint.h>
#include <string.h>
#include <unistd.h>

#include "mouse_capture_linux.h"

extern void goMouseCaptureMove(int dx, int dy);
extern void goMouseCaptureButton(int button, int pressed);
extern void goMouseCaptureScroll(int delta);

typedef Status (*xi_query_version_fn)(Display *, int *, int *);
typedef int (*xi_select_events_fn)(Display *, Window, XIEventMask *, int);

static xi_query_version_fn p_XIQueryVersion = NULL;
static xi_select_events_fn p_XISelectEvents = NULL;

static Display *g_dpy = NULL;
static Window g_win = 0;
static Cursor g_blank_cursor = None;
static pthread_t g_thread;
static int g_thread_running = 0;
static volatile int g_active = 0;
static int g_wake[2] = {-1, -1};
static int g_xi_opcode = 0;
static int g_focused = 1;
// Fractions of a pixel the raw valuators carry, kept so slow motion adds up.
static double g_rem_x = 0, g_rem_y = 0;

static int usbridge_load_xi(void) {
    if (p_XIQueryVersion != NULL && p_XISelectEvents != NULL) {
        return 1;
    }
    void *l = dlopen("libXi.so.6", RTLD_NOW | RTLD_LOCAL);
    if (l == NULL) {
        return 0;
    }
    p_XIQueryVersion = (xi_query_version_fn)dlsym(l, "XIQueryVersion");
    p_XISelectEvents = (xi_select_events_fn)dlsym(l, "XISelectEvents");
    return p_XIQueryVersion != NULL && p_XISelectEvents != NULL;
}

// Confines the hidden pointer to the window. The grab's own core events are
// not selected: the raw events carry everything.
static void usbridge_grab(int on) {
    if (on) {
        XGrabPointer(g_dpy, g_win, False, 0, GrabModeAsync, GrabModeAsync,
                     g_win, g_blank_cursor, CurrentTime);
    } else {
        XUngrabPointer(g_dpy, CurrentTime);
    }
    XFlush(g_dpy);
}

static Cursor usbridge_make_blank_cursor(Display *dpy, Window win) {
    char data[1] = {0};
    Pixmap blank = XCreateBitmapFromData(dpy, win, data, 1, 1);
    if (blank == None) {
        return None;
    }
    XColor black;
    memset(&black, 0, sizeof(black));
    Cursor cursor = XCreatePixmapCursor(dpy, blank, blank, &black, &black, 0, 0);
    XFreePixmap(dpy, blank);
    return cursor;
}

// The first two valuators of a raw motion (x, y), unaccelerated.
static void usbridge_raw_xy(const XIRawEvent *re, double *x, double *y) {
    *x = 0;
    *y = 0;
    const double *v = re->raw_values;
    for (int i = 0; i < re->valuators.mask_len * 8 && i < 2; i++) {
        if (XIMaskIsSet(re->valuators.mask, i)) {
            if (i == 0) {
                *x = *v;
            } else {
                *y = *v;
            }
            v++;
        }
    }
}

static void usbridge_raw_event(XGenericEventCookie *cookie) {
    const XIRawEvent *re = (const XIRawEvent *)cookie->data;
    switch (cookie->evtype) {
    case XI_RawMotion: {
        double x, y;
        usbridge_raw_xy(re, &x, &y);
        g_rem_x += x;
        g_rem_y += y;
        int dx = (int)g_rem_x, dy = (int)g_rem_y;
        g_rem_x -= dx;
        g_rem_y -= dy;
        if (dx != 0 || dy != 0) {
            goMouseCaptureMove(dx, dy);
        }
        break;
    }
    case XI_RawButtonPress:
    case XI_RawButtonRelease: {
        int pressed = (cookie->evtype == XI_RawButtonPress) ? 1 : 0;
        // Button numbers match Limelight.h's BUTTON_LEFT/MIDDLE/RIGHT
        // (1/2/3) directly -- X11's own Button1/2/3 already line up.
        switch (re->detail) {
        case 1: case 2: case 3: goMouseCaptureButton(re->detail, pressed); break;
        case 4: if (pressed) goMouseCaptureScroll(1);  break; // wheel up
        case 5: if (pressed) goMouseCaptureScroll(-1); break; // wheel down
        default: break;
        }
        break;
    }
    default:
        break;
    }
}

static void *usbridge_event_thread(void *arg) {
    (void)arg;
    struct pollfd fds[2];
    fds[0].fd = ConnectionNumber(g_dpy);
    fds[0].events = POLLIN;
    fds[1].fd = g_wake[0];
    fds[1].events = POLLIN;
    while (g_active) {
        if (XPending(g_dpy) == 0) {
            if (poll(fds, 2, -1) < 0) {
                continue;
            }
            if (fds[1].revents != 0 || !g_active) {
                break;
            }
            if (XPending(g_dpy) == 0) {
                continue;
            }
        }
        XEvent ev;
        XNextEvent(g_dpy, &ev);
        if (!g_active) {
            break;
        }
        if (ev.type == FocusIn || ev.type == FocusOut) {
            // Focus moving between our own windows is not leaving them.
            if (ev.xfocus.detail != NotifyInferior && ev.xfocus.detail != NotifyPointer) {
                g_focused = (ev.type == FocusIn);
                usbridge_grab(g_focused);
            }
            continue;
        }
        XGenericEventCookie *cookie = &ev.xcookie;
        if (ev.type != GenericEvent || cookie->extension != g_xi_opcode) {
            continue;
        }
        if (XGetEventData(g_dpy, cookie)) {
            if (g_focused) {
                usbridge_raw_event(cookie);
            }
            XFreeEventData(g_dpy, cookie);
        }
    }
    return NULL;
}

int usbridge_mouse_capture_start(uintptr_t xwindow) {
    if (g_active) {
        return 1;
    }
    if (xwindow == 0 || !usbridge_load_xi()) {
        return 0;
    }

    Display *dpy = XOpenDisplay(NULL);
    if (!dpy) {
        return 0;
    }
    int ev_base, err_base, major = 2, minor = 2;
    if (!XQueryExtension(dpy, "XInputExtension", &g_xi_opcode, &ev_base, &err_base) ||
        p_XIQueryVersion(dpy, &major, &minor) != Success) {
        XCloseDisplay(dpy);
        return 0;
    }

    Window win = (Window)xwindow;
    XWindowAttributes attrs;
    if (!XGetWindowAttributes(dpy, win, &attrs)) {
        XCloseDisplay(dpy);
        return 0;
    }

    if (pipe(g_wake) != 0) {
        XCloseDisplay(dpy);
        return 0;
    }

    unsigned char bits[XIMaskLen(XI_LASTEVENT)];
    memset(bits, 0, sizeof(bits));
    XISetMask(bits, XI_RawMotion);
    XISetMask(bits, XI_RawButtonPress);
    XISetMask(bits, XI_RawButtonRelease);
    XIEventMask mask;
    mask.deviceid = XIAllMasterDevices;
    mask.mask_len = sizeof(bits);
    mask.mask = bits;
    p_XISelectEvents(dpy, DefaultRootWindow(dpy), &mask, 1);
    // FocusChangeMask is not exclusive, unlike ButtonPressMask.
    XSelectInput(dpy, win, FocusChangeMask);

    Cursor blank = usbridge_make_blank_cursor(dpy, win);
    if (blank != None) {
        XDefineCursor(dpy, win, blank);
    }

    g_dpy = dpy;
    g_win = win;
    g_blank_cursor = blank;
    usbridge_grab(1);
    g_focused = 1;
    g_rem_x = g_rem_y = 0;
    g_active = 1;

    if (pthread_create(&g_thread, NULL, usbridge_event_thread, NULL) != 0) {
        g_active = 0;
        g_thread_running = 0;
        usbridge_mouse_capture_stop();
        return 0;
    }
    g_thread_running = 1;

    XFlush(dpy);
    return 1;
}

void usbridge_mouse_capture_stop(void) {
    if (g_dpy == NULL) {
        return;
    }
    g_active = 0;

    if (g_wake[1] >= 0) {
        char b = 1;
        ssize_t n = write(g_wake[1], &b, 1);
        (void)n;
    }
    if (g_thread_running) {
        pthread_join(g_thread, NULL);
        g_thread_running = 0;
    }

    XUngrabPointer(g_dpy, CurrentTime);
    if (g_blank_cursor != None) {
        XUndefineCursor(g_dpy, g_win);
        XFreeCursor(g_dpy, g_blank_cursor);
        g_blank_cursor = None;
    }
    XCloseDisplay(g_dpy);
    g_dpy = NULL;
    g_win = 0;
    for (int i = 0; i < 2; i++) {
        if (g_wake[i] >= 0) {
            close(g_wake[i]);
            g_wake[i] = -1;
        }
    }
}

#endif
