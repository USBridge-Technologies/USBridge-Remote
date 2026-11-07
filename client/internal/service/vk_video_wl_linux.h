//go:build linux && !android

// Native-Wayland host surface for the Vulkan video overlay
// (see vk_video_wl_linux.c).
#ifndef USBRIDGE_VK_VIDEO_WL_LINUX_H
#define USBRIDGE_VK_VIDEO_WL_LINUX_H

struct wl_display;
struct wl_surface;

// Logical (surface-local) coordinates, parent-relative. 1 on success.
int  vk_wl_create(void *display, void *parent, int lx, int ly, int lw, int lh);
void vk_wl_set_rect(int lx, int ly, int lw, int lh);
// Holes in buffer pixels; scale = buffer pixels per logical unit.
void vk_wl_set_holes(const int *xywh, int n, float scale);
void vk_wl_set_hidden(int hidden);
void vk_wl_destroy(void);
struct wl_display *vk_wl_display(void);
struct wl_surface *vk_wl_surface(void);

#endif
