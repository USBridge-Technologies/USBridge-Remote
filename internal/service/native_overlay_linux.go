//go:build linux && !android && cgo

package service

// NativeVideoOverlayIsActive reports whether the GLX/X11 child-window overlay is live.
func NativeVideoOverlayIsActive() bool {
	return GLVideoIsActive()
}
