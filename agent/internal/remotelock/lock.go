// Package remotelock blocks remote (injected) mouse and keyboard from
// driving the agent window, while leaving real local hardware input alone.
//
// Why here and not "disable the UI when a stream is connected": a local
// person at the PC still needs Account, protocol, and Quit during a
// session. The streamer injects via SendInput, which Windows marks
// LLMHF_INJECTED / LLKHF_INJECTED; a low-level hook can drop those when
// they target this process and let everything else through. No session
// detector is required — with nobody connected there is no injected input.
package remotelock

import "sync/atomic"

var enabled atomic.Bool

// SetEnabled arms or disarms the platform hook. No-op off Windows.
func SetEnabled(on bool) {
	enabled.Store(on)
	setHookEnabled(on)
}

func isArmed() bool { return enabled.Load() }
