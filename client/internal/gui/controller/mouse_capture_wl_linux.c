//go:build linux && !android

// Raw relative mouse capture for Capture mode on native Wayland — the
// counterpart of mouse_capture_linux.c's XInput2 path.
//
//   • zwp_pointer_constraints_v1 locks the pointer to the Fyne window's
//     surface (the compositor activates the lock once the pointer is over it,
//     and drops it when focus goes elsewhere, like the X11 grab).
//   • zwp_relative_pointer_v1 delivers the unaccelerated deltas (the host
//     applies its own acceleration, same reason as the X11 raw valuators).
//   • A second wl_pointer from the seat sees the buttons and the wheel; GLFW
//     keeps its own wl_pointer, so Fyne still receives its events, which is
//     what the Go-side inCaptureMode guards already expect on X11.
//
// Everything lives on a private event queue dispatched by a thread of our
// own (the prepare_read / read_events protocol, which coexists with GLFW's
// loop on the same wl_display). Events are forwarded only while the lock is
// active.

#if defined(__linux__) && !defined(__ANDROID__)

#include <errno.h>
#include <poll.h>
#include <pthread.h>
#include <stdint.h>
#include <string.h>
#include <unistd.h>
#include <linux/input-event-codes.h>
#include <wayland-client.h>

#include "wl-pointer-constraints-client-protocol.h"
#include "wl-pointer-constraints-protocol.c.inc"
#include "wl-relative-pointer-client-protocol.h"
#include "wl-relative-pointer-protocol.c.inc"

#include "mouse_capture_linux.h"

extern void goMouseCaptureMove(int dx, int dy);
extern void goMouseCaptureButton(int button, int pressed);
extern void goMouseCaptureScroll(int delta);

static struct wl_display *m_dpy = NULL;
static struct wl_event_queue *m_queue = NULL;
static struct wl_display *m_wrapper = NULL;
static struct wl_registry *m_registry = NULL;
static struct wl_seat *m_seat = NULL;
static uint32_t m_seat_version = 0;
static struct wl_pointer *m_pointer = NULL;
static struct zwp_pointer_constraints_v1 *m_constraints = NULL;
static struct zwp_relative_pointer_manager_v1 *m_relmgr = NULL;
static struct zwp_relative_pointer_v1 *m_rel = NULL;
static struct zwp_locked_pointer_v1 *m_lock = NULL;
static struct wl_surface *m_surface = NULL;

static pthread_t m_thread;
static int m_thread_running = 0;
static volatile int m_active = 0;
static int m_wake[2] = {-1, -1};
static volatile int m_locked = 0;
static uint32_t m_enter_serial = 0;
static int m_has_enter = 0;
static double m_rem_x = 0, m_rem_y = 0;
static double m_axis_acc = 0;
static int m_frame_discrete = 0;

static void hide_cursor(void) {
    if (m_pointer && m_has_enter) wl_pointer_set_cursor(m_pointer, m_enter_serial, NULL, 0, 0);
}

// ── registry / seat ──────────────────────────────────────────────────────────

static void seat_name(void *data, struct wl_seat *seat, const char *name) {
    (void)data; (void)seat; (void)name;
}
static uint32_t m_seat_caps = 0; // taken after the second roundtrip
static void seat_caps_store(void *data, struct wl_seat *seat, uint32_t caps) {
    (void)data; (void)seat; m_seat_caps = caps;
}
static const struct wl_seat_listener seat_listener_store = { seat_caps_store, seat_name };

static void reg_global(void *data, struct wl_registry *r, uint32_t name,
                       const char *iface, uint32_t version) {
    (void)data;
    if (strcmp(iface, wl_seat_interface.name) == 0 && !m_seat) {
        // <= 7: v8 replaces axis_discrete with axis_value120.
        m_seat_version = version < 7 ? version : 7;
        m_seat = wl_registry_bind(r, name, &wl_seat_interface, m_seat_version);
        wl_seat_add_listener(m_seat, &seat_listener_store, NULL);
    } else if (strcmp(iface, ub_zwp_pointer_constraints_v1_interface.name) == 0) {
        m_constraints = wl_registry_bind(r, name, &ub_zwp_pointer_constraints_v1_interface, 1);
    } else if (strcmp(iface, ub_zwp_relative_pointer_manager_v1_interface.name) == 0) {
        m_relmgr = wl_registry_bind(r, name, &ub_zwp_relative_pointer_manager_v1_interface, 1);
    }
}
static void reg_remove(void *data, struct wl_registry *r, uint32_t name) {
    (void)data; (void)r; (void)name;
}
static const struct wl_registry_listener reg_listener = { reg_global, reg_remove };

// ── pointer ──────────────────────────────────────────────────────────────────

static void p_enter(void *data, struct wl_pointer *p, uint32_t serial,
                    struct wl_surface *s, wl_fixed_t x, wl_fixed_t y) {
    (void)data; (void)p; (void)x; (void)y;
    if (s != m_surface) return;
    m_enter_serial = serial;
    m_has_enter = 1;
    if (m_locked) hide_cursor();
}
static void p_leave(void *data, struct wl_pointer *p, uint32_t serial, struct wl_surface *s) {
    (void)data; (void)p; (void)serial;
    if (s == m_surface) m_has_enter = 0;
}
static void p_motion(void *data, struct wl_pointer *p, uint32_t t, wl_fixed_t x, wl_fixed_t y) {
    (void)data; (void)p; (void)t; (void)x; (void)y;
}
static void p_button(void *data, struct wl_pointer *p, uint32_t serial, uint32_t t,
                     uint32_t button, uint32_t state) {
    (void)data; (void)p; (void)serial; (void)t;
    if (!m_active || !m_locked) return;
    int pressed = state == WL_POINTER_BUTTON_STATE_PRESSED;
    // Limelight.h BUTTON_LEFT/MIDDLE/RIGHT = 1/2/3, as on X11.
    switch (button) {
    case BTN_LEFT:   goMouseCaptureButton(1, pressed); break;
    case BTN_MIDDLE: goMouseCaptureButton(2, pressed); break;
    case BTN_RIGHT:  goMouseCaptureButton(3, pressed); break;
    default: break;
    }
}
static void p_axis(void *data, struct wl_pointer *p, uint32_t t, uint32_t axis, wl_fixed_t value) {
    (void)data; (void)p; (void)t;
    if (!m_active || !m_locked || axis != WL_POINTER_AXIS_VERTICAL_SCROLL) return;
    if (m_seat_version >= 5 && m_frame_discrete) return; // the wheel came as axis_discrete
    // Touchpads / smooth wheels: one step per 10 units (a libinput wheel
    // detent is ~15), positive = down.
    m_axis_acc += wl_fixed_to_double(value);
    while (m_axis_acc >= 10.0)  { goMouseCaptureScroll(-1); m_axis_acc -= 10.0; }
    while (m_axis_acc <= -10.0) { goMouseCaptureScroll(1);  m_axis_acc += 10.0; }
}
static void p_frame(void *data, struct wl_pointer *p) {
    (void)data; (void)p;
    m_frame_discrete = 0;
}
static void p_axis_source(void *data, struct wl_pointer *p, uint32_t src) {
    (void)data; (void)p; (void)src;
}
static void p_axis_stop(void *data, struct wl_pointer *p, uint32_t t, uint32_t axis) {
    (void)data; (void)p; (void)t;
    if (axis == WL_POINTER_AXIS_VERTICAL_SCROLL) m_axis_acc = 0;
}
static void p_axis_discrete(void *data, struct wl_pointer *p, uint32_t axis, int32_t discrete) {
    (void)data; (void)p;
    if (axis != WL_POINTER_AXIS_VERTICAL_SCROLL) return;
    // axis_discrete precedes its axis event in the same frame.
    m_frame_discrete = 1;
    if (!m_active || !m_locked || discrete == 0) return;
    goMouseCaptureScroll(discrete > 0 ? -1 : 1);
}
static const struct wl_pointer_listener pointer_listener = {
    .enter = p_enter, .leave = p_leave, .motion = p_motion, .button = p_button,
    .axis = p_axis, .frame = p_frame, .axis_source = p_axis_source,
    .axis_stop = p_axis_stop, .axis_discrete = p_axis_discrete,
};

// ── relative motion / lock ───────────────────────────────────────────────────

static void rel_motion(void *data, struct zwp_relative_pointer_v1 *r,
                       uint32_t hi, uint32_t lo, wl_fixed_t dx, wl_fixed_t dy,
                       wl_fixed_t dx_unaccel, wl_fixed_t dy_unaccel) {
    (void)data; (void)r; (void)hi; (void)lo; (void)dx; (void)dy;
    if (!m_active || !m_locked) return;
    m_rem_x += wl_fixed_to_double(dx_unaccel);
    m_rem_y += wl_fixed_to_double(dy_unaccel);
    int ix = (int)m_rem_x, iy = (int)m_rem_y;
    if (ix == 0 && iy == 0) return;
    m_rem_x -= ix;
    m_rem_y -= iy;
    goMouseCaptureMove(ix, iy);
}
static const struct zwp_relative_pointer_v1_listener rel_listener = { rel_motion };

static void lock_locked(void *data, struct zwp_locked_pointer_v1 *l) {
    (void)data; (void)l;
    m_locked = 1;
    m_rem_x = m_rem_y = 0;
    hide_cursor();
}
static void lock_unlocked(void *data, struct zwp_locked_pointer_v1 *l) {
    (void)data; (void)l;
    m_locked = 0; // persistent lifetime: comes back when the window is focused again
}
static const struct zwp_locked_pointer_v1_listener lock_listener = { lock_locked, lock_unlocked };

// ── event thread ─────────────────────────────────────────────────────────────

static void *event_thread(void *arg) {
    (void)arg;
    int fd = wl_display_get_fd(m_dpy);
    while (m_active) {
        while (wl_display_prepare_read_queue(m_dpy, m_queue) != 0) {
            if (wl_display_dispatch_queue_pending(m_dpy, m_queue) < 0) return NULL;
        }
        wl_display_flush(m_dpy);
        struct pollfd pfd[2] = { { fd, POLLIN, 0 }, { m_wake[0], POLLIN, 0 } };
        int r = poll(pfd, 2, -1);
        if (r < 0 && errno != EINTR) { wl_display_cancel_read(m_dpy); break; }
        if (pfd[1].revents) { wl_display_cancel_read(m_dpy); break; }
        if (r > 0 && (pfd[0].revents & POLLIN)) {
            if (wl_display_read_events(m_dpy) < 0) break;
        } else {
            wl_display_cancel_read(m_dpy);
        }
        if (wl_display_dispatch_queue_pending(m_dpy, m_queue) < 0) break;
    }
    return NULL;
}

static void destroy_objects(void) {
    if (m_lock)        { zwp_locked_pointer_v1_destroy(m_lock); m_lock = NULL; }
    if (m_rel)         { zwp_relative_pointer_v1_destroy(m_rel); m_rel = NULL; }
    if (m_pointer) {
        if (m_seat_version >= WL_POINTER_RELEASE_SINCE_VERSION) wl_pointer_release(m_pointer);
        else wl_pointer_destroy(m_pointer);
        m_pointer = NULL;
    }
    if (m_relmgr)      { zwp_relative_pointer_manager_v1_destroy(m_relmgr); m_relmgr = NULL; }
    if (m_constraints) { zwp_pointer_constraints_v1_destroy(m_constraints); m_constraints = NULL; }
    if (m_seat) {
        if (m_seat_version >= WL_SEAT_RELEASE_SINCE_VERSION) wl_seat_release(m_seat);
        else wl_seat_destroy(m_seat);
        m_seat = NULL;
    }
    if (m_registry)    { wl_registry_destroy(m_registry); m_registry = NULL; }
    if (m_wrapper)     { wl_proxy_wrapper_destroy(m_wrapper); m_wrapper = NULL; }
    if (m_dpy) wl_display_flush(m_dpy);
    if (m_queue)       { wl_event_queue_destroy(m_queue); m_queue = NULL; }
    if (m_wake[0] >= 0) { close(m_wake[0]); m_wake[0] = -1; }
    if (m_wake[1] >= 0) { close(m_wake[1]); m_wake[1] = -1; }
    m_dpy = NULL; m_surface = NULL;
    m_seat_version = 0; m_seat_caps = 0;
    m_locked = 0; m_has_enter = 0;
    m_rem_x = m_rem_y = 0; m_axis_acc = 0;
}

int usbridge_mouse_capture_wl_start(uintptr_t display, uintptr_t surface) {
    usbridge_mouse_capture_wl_stop();
    if (!display || !surface) return 0;
    m_dpy = (struct wl_display *)display;
    m_surface = (struct wl_surface *)surface;

    m_queue = wl_display_create_queue(m_dpy);
    m_wrapper = wl_proxy_create_wrapper(m_dpy);
    if (!m_queue || !m_wrapper) goto fail;
    wl_proxy_set_queue((struct wl_proxy *)m_wrapper, m_queue);
    m_registry = wl_display_get_registry(m_wrapper);
    wl_registry_add_listener(m_registry, &reg_listener, NULL);
    // First roundtrip: globals; second: the seat's capabilities.
    if (wl_display_roundtrip_queue(m_dpy, m_queue) < 0) goto fail;
    if (!m_seat || !m_constraints || !m_relmgr) goto fail;
    if (wl_display_roundtrip_queue(m_dpy, m_queue) < 0) goto fail;
    if (!(m_seat_caps & WL_SEAT_CAPABILITY_POINTER)) goto fail;

    m_pointer = wl_seat_get_pointer(m_seat);
    wl_pointer_add_listener(m_pointer, &pointer_listener, NULL);
    m_rel = zwp_relative_pointer_manager_v1_get_relative_pointer(m_relmgr, m_pointer);
    zwp_relative_pointer_v1_add_listener(m_rel, &rel_listener, NULL);
    m_lock = zwp_pointer_constraints_v1_lock_pointer(m_constraints, m_surface, m_pointer, NULL,
                                                     ZWP_POINTER_CONSTRAINTS_V1_LIFETIME_PERSISTENT);
    zwp_locked_pointer_v1_add_listener(m_lock, &lock_listener, NULL);
    wl_display_flush(m_dpy);

    if (pipe(m_wake) != 0) { m_wake[0] = m_wake[1] = -1; goto fail; }
    m_active = 1;
    if (pthread_create(&m_thread, NULL, event_thread, NULL) != 0) { m_active = 0; goto fail; }
    m_thread_running = 1;
    return 1;

fail:
    destroy_objects();
    return 0;
}

void usbridge_mouse_capture_wl_stop(void) {
    m_active = 0;
    if (m_thread_running) {
        if (m_wake[1] >= 0) { char c = 0; (void)!write(m_wake[1], &c, 1); }
        pthread_join(m_thread, NULL);
        m_thread_running = 0;
    }
    if (m_dpy) destroy_objects();
}

#endif
