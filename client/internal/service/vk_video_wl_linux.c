// vk_video_wl_linux.c — native-Wayland host surface for the Vulkan video
// overlay (vk_video_impl_linux.c). The X11 path keeps its child window; this
// is the Wayland counterpart of that child window.
//
//   • A wl_subsurface of the Fyne/GLFW window's wl_surface, in desync mode:
//     every vkQueuePresentKHR reaches the compositor on its own, without
//     waiting for Fyne to redraw (same latency as the X11 child window).
//   • wp_viewport maps the swapchain's pixel size onto the logical rect, so
//     the picture is rendered at the output's real (fractional) scale.
//   • An empty input region: the pointer and keyboard keep going to the
//     parent surface, so GLFW/Fyne see every event over the video and no
//     X11-style event forwarding is needed.
//   • An opaque region (minus menu holes) lets the compositor skip blending.
//
// All objects live on a private event queue so nothing here is dispatched by
// GLFW's own loop (and GLFW's objects are never dispatched from here).
// libwayland requests are thread-safe; these functions are called from the
// Go/CGO side only, never from the render thread.

#if defined(__linux__) && !defined(__ANDROID__)

#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include <math.h>
#include <wayland-client.h>

#include "wl-viewporter-client-protocol.h"
#include "wl-viewporter-protocol.c.inc"

#include "vk_video_wl_linux.h"

extern void goVKLog(char *msg, int level);

static struct wl_display       *w_dpy      = NULL;
static struct wl_event_queue   *w_queue    = NULL;
static struct wl_display       *w_wrapper  = NULL;
static struct wl_registry      *w_registry = NULL;
static struct wl_compositor    *w_comp     = NULL;
static struct wl_subcompositor *w_subcomp  = NULL;
static struct wp_viewporter    *w_vporter  = NULL;
static struct wl_surface       *w_parent   = NULL;
static struct wl_surface       *w_surf     = NULL;
static struct wl_subsurface    *w_sub      = NULL;
static struct wp_viewport      *w_vport    = NULL;
static int w_below = 0; // stacked under the parent (hidden)
static int w_lx, w_ly, w_lw, w_lh;

static void reg_global(void *data, struct wl_registry *r, uint32_t name,
                       const char *iface, uint32_t version) {
    (void)data;
    if (strcmp(iface, wl_compositor_interface.name) == 0) {
        w_comp = wl_registry_bind(r, name, &wl_compositor_interface, version < 4 ? version : 4);
    } else if (strcmp(iface, wl_subcompositor_interface.name) == 0) {
        w_subcomp = wl_registry_bind(r, name, &wl_subcompositor_interface, 1);
    } else if (strcmp(iface, ub_wp_viewporter_interface.name) == 0) {
        w_vporter = wl_registry_bind(r, name, &ub_wp_viewporter_interface, 1);
    }
}
static void reg_remove(void *data, struct wl_registry *r, uint32_t name) {
    (void)data; (void)r; (void)name;
}
static const struct wl_registry_listener reg_listener = { reg_global, reg_remove };

struct wl_display *vk_wl_display(void) { return w_dpy; }
struct wl_surface *vk_wl_surface(void) { return w_surf; }

static void vk_wl_apply_opaque(const int *holes_xywh, int n, float scale) {
    if (!w_comp || !w_surf) return;
    struct wl_region *reg = wl_compositor_create_region(w_comp);
    wl_region_add(reg, 0, 0, w_lw, w_lh);
    for (int i = 0; i < n; i++) {
        // Holes arrive in buffer pixels; grow them to whole logical units so
        // a fractional edge is never claimed opaque.
        int x0 = (int)floorf(holes_xywh[i*4]   / scale);
        int y0 = (int)floorf(holes_xywh[i*4+1] / scale);
        int x1 = (int)ceilf((holes_xywh[i*4]   + holes_xywh[i*4+2]) / scale);
        int y1 = (int)ceilf((holes_xywh[i*4+1] + holes_xywh[i*4+3]) / scale);
        if (x1 > x0 && y1 > y0) wl_region_subtract(reg, x0, y0, x1 - x0, y1 - y0);
    }
    wl_surface_set_opaque_region(w_surf, reg);
    wl_region_destroy(reg);
}

int vk_wl_create(void *display, void *parent, int lx, int ly, int lw, int lh) {
    vk_wl_destroy();
    if (!display || !parent) { goVKLog("vk_wl_create: no wl_display / parent wl_surface", 2); return 0; }
    w_dpy = (struct wl_display *)display;
    w_parent = (struct wl_surface *)parent;

    w_queue = wl_display_create_queue(w_dpy);
    w_wrapper = wl_proxy_create_wrapper(w_dpy);
    if (!w_queue || !w_wrapper) { goVKLog("vk_wl_create: event queue failed", 2); goto fail; }
    wl_proxy_set_queue((struct wl_proxy *)w_wrapper, w_queue);
    w_registry = wl_display_get_registry(w_wrapper);
    wl_registry_add_listener(w_registry, &reg_listener, NULL);
    if (wl_display_roundtrip_queue(w_dpy, w_queue) < 0) { goVKLog("vk_wl_create: roundtrip failed", 2); goto fail; }
    if (!w_comp || !w_subcomp) { goVKLog("vk_wl_create: compositor lacks wl_subcompositor", 2); goto fail; }
    if (!w_vporter) goVKLog("vk_wl_create: no wp_viewporter -- video drawn at logical resolution", 1);

    w_surf = wl_compositor_create_surface(w_comp);
    w_sub = wl_subcompositor_get_subsurface(w_subcomp, w_surf, w_parent);
    if (!w_surf || !w_sub) { goVKLog("vk_wl_create: subsurface creation failed", 2); goto fail; }
    wl_subsurface_set_desync(w_sub);
    struct wl_region *empty = wl_compositor_create_region(w_comp);
    wl_surface_set_input_region(w_surf, empty);
    wl_region_destroy(empty);
    if (w_vporter) w_vport = wp_viewporter_get_viewport(w_vporter, w_surf);

    w_below = 0;
    w_lw = w_lh = 0;
    vk_wl_set_rect(lx, ly, lw, lh);
    wl_surface_commit(w_surf);
    wl_display_flush(w_dpy);
    return 1;

fail:
    vk_wl_destroy();
    return 0;
}

// vk_wl_set_rect positions the subsurface (logical units, parent-local) and
// sets its on-screen size. Position and stacking are parent state, applied on
// Fyne's next frame; the viewport size goes out with the next video present.
void vk_wl_set_rect(int lx, int ly, int lw, int lh) {
    if (!w_sub || lw <= 0 || lh <= 0) return;
    if (lx != w_lx || ly != w_ly) wl_subsurface_set_position(w_sub, lx, ly);
    w_lx = lx; w_ly = ly;
    if (lw != w_lw || lh != w_lh) {
        w_lw = lw; w_lh = lh;
        if (w_vport) wp_viewport_set_destination(w_vport, lw, lh);
    }
    wl_display_flush(w_dpy);
}

void vk_wl_set_holes(const int *xywh, int n, float scale) {
    if (!w_surf) return;
    if (scale <= 0) scale = 1;
    vk_wl_apply_opaque(xywh, n, scale);
    wl_display_flush(w_dpy);
}

// Hiding stacks the subsurface under the (opaque) parent instead of
// detaching its buffer, which belongs to the Vulkan swapchain.
void vk_wl_set_hidden(int hidden) {
    if (!w_sub || !w_parent) return;
    if (hidden && !w_below) wl_subsurface_place_below(w_sub, w_parent);
    else if (!hidden && w_below) wl_subsurface_place_above(w_sub, w_parent);
    else return;
    w_below = hidden ? 1 : 0;
    wl_display_flush(w_dpy);
}

// Call only after the VkSurfaceKHR on w_surf has been destroyed.
void vk_wl_destroy(void) {
    if (w_vport)    { wp_viewport_destroy(w_vport);       w_vport = NULL; }
    if (w_sub)      { wl_subsurface_destroy(w_sub);       w_sub = NULL; }
    if (w_surf)     { wl_surface_destroy(w_surf);         w_surf = NULL; }
    if (w_vporter)  { wp_viewporter_destroy(w_vporter);   w_vporter = NULL; }
    if (w_subcomp)  { wl_subcompositor_destroy(w_subcomp); w_subcomp = NULL; }
    if (w_comp)     { wl_compositor_destroy(w_comp);      w_comp = NULL; }
    if (w_registry) { wl_registry_destroy(w_registry);    w_registry = NULL; }
    if (w_wrapper)  { wl_proxy_wrapper_destroy(w_wrapper); w_wrapper = NULL; }
    if (w_dpy) wl_display_flush(w_dpy);
    if (w_queue)    { wl_event_queue_destroy(w_queue);    w_queue = NULL; }
    w_dpy = NULL; w_parent = NULL;
    w_below = 0;
    w_lx = w_ly = w_lw = w_lh = 0;
}

#endif
