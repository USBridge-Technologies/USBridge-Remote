package service

// MoonlightPadSender sends what Sunshine's controller extensions add to a
// pad's state: its touchpad contacts, motion sensors and battery. A host
// that can show them (a virtual DualShock 4) advertises
// LI_FF_CONTROLLER_TOUCH_EVENTS, and the library refuses them otherwise.
// Kept apart from MoonlightInputSender: only a native Moonlight stream can
// do this.
type MoonlightPadSender interface {
	HostSupportsControllerExtensions() bool
	// SendMoonlightControllerTouch sends one contact of the pad's touchpad:
	// eventType is LI_TOUCH_EVENT_* (LiTouchDown/Move/Up/CancelAll), x/y are
	// normalized 0.0..1.0 across the touchpad.
	SendMoonlightControllerTouch(controller, eventType uint8, pointerID uint32, x, y, pressure float32)
	// SendMoonlightControllerMotion sends one sensor sample: the
	// accelerometer in m/s^2 (gravity included) or the gyro in degrees per
	// second, on the pad's own axes (+X right, +Y up out of the face, +Z
	// toward the player).
	SendMoonlightControllerMotion(controller, motionType uint8, x, y, z float32)
	// SendMoonlightControllerBattery sends LI_BATTERY_STATE_* and a
	// percentage (LiBatteryPercentUnknown if the pad does not say).
	SendMoonlightControllerBattery(controller, state, percent uint8)
}

// Limelight.h constants for MoonlightPadSender.
const (
	LiTouchDown      uint8 = 0x01
	LiTouchUp        uint8 = 0x02
	LiTouchMove      uint8 = 0x03
	LiTouchCancelAll uint8 = 0x07

	LiMotionAccel uint8 = 0x01
	LiMotionGyro  uint8 = 0x02

	LiBatteryUnknown     uint8 = 0x00
	LiBatteryNotPresent  uint8 = 0x01
	LiBatteryDischarging uint8 = 0x02
	LiBatteryCharging    uint8 = 0x03
	LiBatteryNotCharging uint8 = 0x04
	LiBatteryFull        uint8 = 0x05

	LiBatteryPercentUnknown uint8 = 0xFF

	// LI_CCAP_* capability bits of a controller arrival.
	LiCCapAnalogTriggers uint16 = 0x01
	LiCCapRumble         uint16 = 0x02
	LiCCapTouchpad       uint16 = 0x08
	LiCCapAccel          uint16 = 0x10
	LiCCapGyro           uint16 = 0x20
	LiCCapBatteryState   uint16 = 0x40
	LiCCapRGBLED         uint16 = 0x80
	// LiCCapPadAudio (USBridge, LI_CCAP_UB_PAD_AUDIO): the client plays the
	// pad's speaker/headset sound on the pad. LiCCapHapticsPCM (Vibeshine,
	// LI_CCAP_HAPTICS_PCM): it plays a DualSense's haptics on its voice coils.
	LiCCapPadAudio   uint16 = 0x4000
	LiCCapHapticsPCM uint16 = 0x8000

	// LiButtonTouchpad is TOUCHPAD_FLAG, the touchpad click in the 32-bit
	// buttonFlags of SendMoonlightControllerEvent.
	LiButtonTouchpad uint32 = 0x100000
)

func (m *MoonlightService) HostSupportsControllerExtensions() bool {
	w := m.activeWrapper
	return w != nil && w.HostSupportsControllerExtensions()
}

func (m *MoonlightService) SendMoonlightControllerTouch(controller, eventType uint8, pointerID uint32, x, y, pressure float32) {
	if w := m.activeWrapper; w != nil {
		w.SendMoonlightControllerTouch(controller, eventType, pointerID, x, y, pressure)
	}
}

func (m *MoonlightService) SendMoonlightControllerMotion(controller, motionType uint8, x, y, z float32) {
	if w := m.activeWrapper; w != nil {
		w.SendMoonlightControllerMotion(controller, motionType, x, y, z)
	}
}

func (m *MoonlightService) SendMoonlightControllerBattery(controller, state, percent uint8) {
	if w := m.activeWrapper; w != nil {
		w.SendMoonlightControllerBattery(controller, state, percent)
	}
}
