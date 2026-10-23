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

// PadMotionHandler receives the host's request to stream a pad's motion
// sensor (IDX_SET_MOTION_EVENT): motionType is LiMotionAccel or LiMotionGyro,
// rateHz the report rate the host wants, 0 to stop.
type PadMotionHandler func(controller uint16, motionType uint8, rateHz uint16)

// PadLightHandler receives the light colour a game set on the host's virtual
// pad (IDX_SET_RGB_LED), for the real pad's lightbar.
type PadLightHandler func(controller uint16, r, g, b uint8)

var (
	padMotionHandler atomic.Pointer[PadMotionHandler]
	padLightHandler  atomic.Pointer[PadLightHandler]
)

// SetPadMotionHandler installs (or, with nil, removes) the process-wide
// motion-sensor request handler.
func SetPadMotionHandler(h PadMotionHandler) {
	if h == nil {
		padMotionHandler.Store(nil)
		return
	}
	padMotionHandler.Store(&h)
}

// SetPadLightHandler installs (or, with nil, removes) the process-wide pad
// light handler.
func SetPadLightHandler(h PadLightHandler) {
	if h == nil {
		padLightHandler.Store(nil)
		return
	}
	padLightHandler.Store(&h)
}

func dispatchPadMotion(controller uint16, motionType uint8, rateHz uint16) {
	if p := padMotionHandler.Load(); p != nil {
		(*p)(controller, motionType, rateHz)
	}
}

func dispatchPadLight(controller uint16, r, g, b uint8) {
	if p := padLightHandler.Load(); p != nil {
		(*p)(controller, r, g, b)
	}
}
