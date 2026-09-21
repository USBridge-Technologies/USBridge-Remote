package service

import "sync/atomic"

// RumbleHandler receives the host's gamepad rumble request: the large
// (low-frequency) and small (high-frequency) motor levels, 0..65535 each, for
// a Moonlight controller number. It is called from moonlight-common-c's
// callback thread, so it must not block.
type RumbleHandler func(controller, lowFreq, highFreq uint16)

var rumbleHandler atomic.Pointer[RumbleHandler]

// SetRumbleHandler installs (or, with nil, removes) the process-wide rumble
// handler.
func SetRumbleHandler(h RumbleHandler) {
	if h == nil {
		rumbleHandler.Store(nil)
		return
	}
	rumbleHandler.Store(&h)
}

// dispatchRumble is called by the cgo rumble callbacks.
func dispatchRumble(controller, lowFreq, highFreq uint16) {
	if p := rumbleHandler.Load(); p != nil {
		(*p)(controller, lowFreq, highFreq)
	}
}
