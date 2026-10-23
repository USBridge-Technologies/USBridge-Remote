//go:build linux && !android

#ifndef USBRIDGE_MOUSE_CAPTURE_LINUX_H
#define USBRIDGE_MOUSE_CAPTURE_LINUX_H

#include <stdint.h>

// Starts raw relative mouse capture on the given X11 window (passed as
// uintptr_t, same convention as the macOS/Windows cgo boundaries in this
// package). X11-only: callers must check for a Wayland native context
// themselves and skip calling this at all in that case (same fallback
// pattern video_widget_gl_linux.go already uses for its own overlay).
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

#endif
