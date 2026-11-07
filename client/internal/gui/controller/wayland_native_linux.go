//go:build linux && !android

package controller

import (
	"unsafe"

	"github.com/go-gl/glfw/v3.4/glfw"
)

// vkOnWaylandPlatform reports whether GLFW (and so Fyne) runs as a native
// Wayland client rather than on X11 / XWayland. One binary does both: GLFW
// 3.4 picks the platform at startup (FYNE_PLATFORM=x11 forces XWayland).
func vkOnWaylandPlatform() bool {
	return glfw.GetPlatform() == glfw.PlatformWayland
}

// waylandDisplay is GLFW's wl_display, 0 when not on Wayland.
func waylandDisplay() uintptr {
	if !vkOnWaylandPlatform() {
		return 0
	}
	return uintptr(unsafe.Pointer(glfw.GetWaylandDisplay()))
}
