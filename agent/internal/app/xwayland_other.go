//go:build !linux

package app

// forceXWaylandForGUI is a no-op outside Linux -- see xwayland_linux.go.
func forceXWaylandForGUI() (restore func()) { return func() {} }
