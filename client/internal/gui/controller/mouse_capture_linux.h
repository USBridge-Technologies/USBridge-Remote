//go:build linux && !android

#ifndef USBRIDGE_MOUSE_CAPTURE_LINUX_H
#define USBRIDGE_MOUSE_CAPTURE_LINUX_H

#include <stdint.h>

// Starts raw relative mouse capture on the given X11 window (passed as
// uintptr_t, same convention as the macOS/Windows cgo boundaries in this
// package). X11-only: on a Wayland native context callers use
// usbridge_mouse_capture_wl_start below instead.
//
// Opens its own Xlib Display connection (same pattern as vk_video_impl_
// linux.c) and reads XInput2 raw pointer events from the root window, which
// does not take any event away from GLFW's own connection (see the .c file
// for why core ButtonPress selection must not be used). Fyne keeps receiving
// its events, which is why the Go-side TouchpadWrapper guards
// (video_mouse_handler.go's inCaptureMode) exist. There is no X11
// permission system to satisfy here, unlike macOS's Accessibility/Input
// Monitoring gate for a global tap.
//
// Returns 1 on success, 0 on failure.
int usbridge_mouse_capture_start(uintptr_t xwindow);

// Wakes (through a pipe) and joins the event thread, then restores the cursor and closes the
// Display connection. Safe to call even if start failed or was never called.
void usbridge_mouse_capture_stop(void);

// Native-Wayland counterpart (mouse_capture_wl_linux.c): display is GLFW's
// wl_display, surface the window's wl_surface. Locks the pointer with
// zwp_pointer_constraints_v1 and reads zwp_relative_pointer_v1 deltas plus a
// wl_pointer of its own for buttons / wheel. Returns 1 on success, 0 when the
// compositor lacks either protocol or a pointer.
int usbridge_mouse_capture_wl_start(uintptr_t display, uintptr_t surface);
void usbridge_mouse_capture_wl_stop(void);

#endif
