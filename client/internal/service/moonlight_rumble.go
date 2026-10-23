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

// PadTriggersHandler receives a DualSense's adaptive-trigger effects
// (IDX_DS_ADAPTIVE_TRIGGERS): flags says which changed (0x04 right, 0x08
// left), then each trigger's mode and its ten parameter bytes.
type PadTriggersHandler func(controller uint16, flags, typeLeft, typeRight uint8, left, right [10]byte)

// PadSoundHandler receives a pad's sound from the host: haptics (format 1,
// 48 kHz S16LE stereo for the two voice coils) or speaker/headset audio
// (format 1 PCM or 2 Opus). frames is per channel. Called on the control
// receive thread: it must only queue.
type PadSoundHandler func(controller uint16, sequence uint32, format uint8, data []byte, frames int)

// Pad sound formats (UB_PAD_AUDIO_FORMAT_*).
const (
	PadSoundPCM  uint8 = 1
	PadSoundOpus uint8 = 2
)

var (
	padTriggersHandler atomic.Pointer[PadTriggersHandler]
	padHapticsHandler  atomic.Pointer[PadSoundHandler]
	padAudioHandler    atomic.Pointer[PadSoundHandler]
)

// SetPadTriggersHandler installs (or, with nil, removes) the adaptive-trigger handler.
func SetPadTriggersHandler(h PadTriggersHandler) {
	if h == nil {
		padTriggersHandler.Store(nil)
		return
	}
	padTriggersHandler.Store(&h)
}

// SetPadHapticsHandler installs (or, with nil, removes) the haptics handler.
func SetPadHapticsHandler(h PadSoundHandler) {
	if h == nil {
		padHapticsHandler.Store(nil)
		return
	}
	padHapticsHandler.Store(&h)
}

// SetPadAudioHandler installs (or, with nil, removes) the speaker/headset handler.
func SetPadAudioHandler(h PadSoundHandler) {
	if h == nil {
		padAudioHandler.Store(nil)
		return
	}
	padAudioHandler.Store(&h)
}

func dispatchPadTriggers(controller uint16, flags, typeLeft, typeRight uint8, left, right [10]byte) {
	if p := padTriggersHandler.Load(); p != nil {
		(*p)(controller, flags, typeLeft, typeRight, left, right)
	}
}

func dispatchPadHaptics(controller uint16, sequence uint32, pcm []byte, frames int) {
	if p := padHapticsHandler.Load(); p != nil {
		(*p)(controller, sequence, PadSoundPCM, pcm, frames)
	}
}

func dispatchPadAudio(controller uint16, sequence uint32, format uint8, data []byte, frames int) {
	if p := padAudioHandler.Load(); p != nil {
		(*p)(controller, sequence, format, data, frames)
	}
}
