// Package remotelock blocks remote (injected) mouse and keyboard from
// driving the agent window, while leaving real local hardware input alone.
//
// Why here and not "disable the UI when a stream is connected": a local
// person at the PC still needs Account, protocol, and Quit during a
// session. On Windows the streamer injects via SendInput, which is marked
// LLMHF_INJECTED / LLKHF_INJECTED; a low-level hook drops those when they
// target this process. On macOS the streamer posts CGEvents at the HID tap
// with HIDSystemState (PID stays 0, like hardware). A session event tap
// swallows HIDSystemState / private-source events on this process's windows
// and leaves CombinedSession hardware alone. On Linux there is
// no such flag — uinput looks like hardware to X11/Wayland — so we EVIOCGRAB
// the streamer's virtual evdev nodes (Sunshine "Mouse passthrough" /
// "Keyboard passthrough", plus this agent's own usbridge-* devices) while
// the pointer is over our window. No session detector is required: with
// nobody connected those devices are idle or absent.
package remotelock

import "sync/atomic"

var enabled atomic.Bool

// SetEnabled arms or disarms the platform filter. No-op on unsupported OS.
func SetEnabled(on bool) {
	enabled.Store(on)
	setHookEnabled(on)
}

// SetX11Window records the Fyne/GLFW X11 xid so the Linux filter can
// hit-test the pointer against this window. No-op off Linux.
func SetX11Window(xid uintptr) {
	setX11Window(xid)
}

func isArmed() bool { return enabled.Load() }

// HookInstalled is true when the OS filter is actually running. On macOS
// this stays false if CGEventTapCreate failed (the agent is not in
// Accessibility).
func HookInstalled() bool { return hookInstalled() }
