package controller

import (
	"errors"
	"math"
	"sync"
	"sync/atomic"

	"usbridge-client/internal/platform"
	"usbridge-client/internal/service"

	"github.com/sirupsen/logrus"
)

// The HID extras of a captured DualShock 4-layout pad -- touchpad, click,
// motion sensors, battery -- read from the pad's own HID input report next to
// the normal capture (platform.StartGamepadHID).
//
// Where they go depends on the host. A host with Sunshine's controller
// extensions (LI_FF_CONTROLLER_TOUCH_EVENTS) and a pad announced as a
// DualShock 4 gets them as they are, for its virtual DualShock 4: the
// contacts as controller touch events, the click as TOUCHPAD_FLAG in the
// pad's buttons, the sensors once the host asks for them (setMotionEventState)
// and the battery when it changes. Any other host gets the touchpad as a
// relative mouse.

// padHID is one pad's HID reader and what has been sent from it.
type padHID struct {
	id      string
	capture *platform.TouchpadCapture
	ds4     bool // announced to the host as a DualShock 4

	mu      sync.Mutex
	mouse   platform.TouchpadMouse
	touch   [2]platform.DS4Finger // contacts as last sent
	click   bool
	battery uint8
	last    platform.GamepadCaptureState // the capture's latest state, to resend with a click change
	hasLast bool
}

// pointerID tells the two contacts of a touchpad apart, and a new touch from
// the one before it on the same contact.
func pointerID(f platform.DS4Finger, contact int) uint32 { return uint32(contact)<<8 | uint32(f.ID) }

// padMotionWanted is, per controller number, which sensors the host asked
// for (bit 0 the accelerometer, bit 1 the gyro). Set from Moonlight's callback
// thread, read from the HID readers.
var padMotionWanted [maxGamepadSlots]atomic.Uint32

// startPadHID starts the HID reader of a captured pad, if it is a DualShock
// 4-layout one; ds4 says whether it was announced as a DualShock 4.
func (dw *DiskWidget) startPadHID(id string, ds4 bool) {
	p := &padHID{id: id, ds4: ds4}
	capture, err := platform.StartGamepadHID(id, func(r platform.DS4Report) { dw.onPadHIDReport(p, r) })
	if err != nil {
		if !errors.Is(err, platform.ErrNoTouchpad) {
			logrus.Warnf("🎮 [GAMEPAD] HID report of %s unavailable: %v", id, err)
		}
		return
	}
	p.capture = capture
	dw.padHIDs.Store(id, p)
}

// stopPadHID ends a pad's HID reader and releases whatever it held: the
// mouse button, the touchpad contacts.
func (dw *DiskWidget) stopPadHID(id string) {
	v, ok := dw.padHIDs.LoadAndDelete(id)
	if !ok {
		return
	}
	p := v.(*padHID)
	p.capture.Stop()
	p.mu.Lock()
	defer p.mu.Unlock()
	if ev, ok := p.mouse.Release(); ok {
		dw.forwardTouchpad(ev)
	}
	if p.touch[0].Active || p.touch[1].Active {
		if pad, slot, ok := dw.padExtensionSender(p); ok {
			pad.SendMoonlightControllerTouch(uint8(slot), service.LiTouchCancelAll, 0, 0, 0, 0)
		}
	}
}

// padExtensionSender is the sender for p's extras and its controller number,
// when the host takes them for this pad.
func (dw *DiskWidget) padExtensionSender(p *padHID) (service.MoonlightPadSender, int, bool) {
	if !p.ds4 || dw.moonlightProvider == nil {
		return nil, 0, false
	}
	sender := dw.moonlightProvider()
	if sender == nil || !sender.IsInputActive() {
		return nil, 0, false
	}
	pad, ok := sender.(service.MoonlightPadSender)
	if !ok || !pad.HostSupportsControllerExtensions() {
		return nil, 0, false
	}
	slot, ok := dw.padSlots.slot(p.id)
	return pad, slot, ok
}

// onPadHIDReport handles one HID input report. Runs on the reader goroutine.
func (dw *DiskWidget) onPadHIDReport(p *padHID, r platform.DS4Report) {
	pad, slot, native := dw.padExtensionSender(p)
	p.mu.Lock()
	defer p.mu.Unlock()
	if !native {
		if ev, ok := p.mouse.Feed(r); ok {
			dw.forwardTouchpad(ev)
		}
		return
	}
	cn := uint8(slot)

	if r.Click != p.click {
		p.click = r.Click
		if p.hasLast {
			dw.sendPadState(p.id, slot, p.last, p.click)
		}
	}

	for i, f := range r.Fingers {
		was := p.touch[i]
		switch {
		case f.Active && (!was.Active || was.ID != f.ID):
			if was.Active {
				pad.SendMoonlightControllerTouch(cn, service.LiTouchUp, pointerID(was, i), 0, 0, 0)
			}
			pad.SendMoonlightControllerTouch(cn, service.LiTouchDown, pointerID(f, i), touchX(f), touchY(f), 1)
		case f.Active && (f.X != was.X || f.Y != was.Y):
			pad.SendMoonlightControllerTouch(cn, service.LiTouchMove, pointerID(f, i), touchX(f), touchY(f), 1)
		case !f.Active && was.Active:
			pad.SendMoonlightControllerTouch(cn, service.LiTouchUp, pointerID(was, i), 0, 0, 0)
		}
		p.touch[i] = f
	}

	if wanted := padMotionWanted[slot].Load(); wanted != 0 && r.HasMotion() {
		if wanted&1 != 0 {
			x, y, z := r.AccelMS2()
			pad.SendMoonlightControllerMotion(cn, service.LiMotionAccel, x, y, z)
		}
		if wanted&2 != 0 {
			x, y, z := r.GyroDegS()
			pad.SendMoonlightControllerMotion(cn, service.LiMotionGyro, x, y, z)
		}
	}

	if r.Battery != p.battery {
		p.battery = r.Battery
		if state, pct, ok := r.BatteryState(); ok {
			pad.SendMoonlightControllerBattery(cn, state, pct)
		}
	}
}

func touchX(f platform.DS4Finger) float32 {
	return float32(f.X) / (platform.DS4TouchpadWidth - 1)
}

func touchY(f platform.DS4Finger) float32 {
	return float32(f.Y) / (platform.DS4TouchpadHeight - 1)
}

// padTouchpadClick records the capture's latest state for a pad with a HID
// reader and returns whether its touchpad is held down (as the host takes it).
func (dw *DiskWidget) padTouchpadClick(id string, state platform.GamepadCaptureState) bool {
	v, ok := dw.padHIDs.Load(id)
	if !ok {
		return false
	}
	p := v.(*padHID)
	p.mu.Lock()
	defer p.mu.Unlock()
	p.last, p.hasLast = state, true
	return p.click
}

// sendPadState sends one multi-controller event, with the touchpad click.
func (dw *DiskWidget) sendPadState(id string, slot int, state platform.GamepadCaptureState, click bool) {
	if dw.moonlightProvider == nil {
		return
	}
	sender := dw.moonlightProvider()
	if sender == nil || !sender.IsInputActive() {
		return
	}
	buttons := uint32(state.Buttons)
	if click {
		buttons |= service.LiButtonTouchpad
	}
	sender.SendMoonlightControllerEvent(uint16(slot), dw.padSlots.mask(), buttons,
		state.LeftTrigger, state.RightTrigger, state.LeftX, state.LeftY, state.RightX, state.RightY)
}

// padArrivalCapabilities is what a pad announced as a DualShock 4 can do
// (LI_CCAP_*): with a HID reader, its touchpad, sensors, battery and light
// reach the host.
func (dw *DiskWidget) padArrivalCapabilities(id string) uint16 {
	caps := service.LiCCapAnalogTriggers | service.LiCCapRumble
	if _, ok := dw.padHIDs.Load(id); ok {
		caps |= service.LiCCapTouchpad | service.LiCCapAccel | service.LiCCapGyro | service.LiCCapBatteryState | service.LiCCapRGBLED
	}
	return caps
}

// onHostPadMotion records which sensors the host wants for a controller.
// Runs on Moonlight's callback thread.
func (dw *DiskWidget) onHostPadMotion(controller uint16, motionType uint8, rateHz uint16) {
	if int(controller) >= maxGamepadSlots || motionType < 1 || motionType > 2 {
		return
	}
	bit := uint32(1) << (motionType - 1)
	w := &padMotionWanted[controller]
	for {
		old := w.Load()
		v := old &^ bit
		if rateHz > 0 {
			v |= bit
		}
		if w.CompareAndSwap(old, v) {
			break
		}
	}
	logrus.Infof("🎮 [GAMEPAD] host %s motion sensor %d of controller %d (%d Hz)",
		map[bool]string{true: "wants", false: "stops"}[rateHz > 0], motionType, controller, rateHz)
}

// onHostPadLight sets the light of the pad that holds a controller number to
// what a game set on the host's virtual pad. Runs on Moonlight's callback thread.
func (dw *DiskWidget) onHostPadLight(controller uint16, r, g, b uint8) {
	id, ok := dw.padSlots.idAt(int(controller))
	if !ok {
		return
	}
	logrus.Infof("🎮 [GAMEPAD] host light controller=%d rgb=%02x%02x%02x -> %s", controller, r, g, b, id)
	platform.SetGamepadLight(id, r, g, b)
}

// forwardTouchpad sends touchpad motion and clicks to the host as a relative mouse.
// Called from the HID reader goroutine.
func (dw *DiskWidget) forwardTouchpad(ev platform.TouchpadEvent) {
	if dw.moonlightProvider == nil {
		return
	}
	sender := dw.moonlightProvider()
	if sender == nil || !sender.IsInputActive() {
		return
	}
	clamp := func(v int) int16 {
		if v > math.MaxInt16 {
			return math.MaxInt16
		}
		if v < math.MinInt16 {
			return math.MinInt16
		}
		return int16(v)
	}
	if ev.DX != 0 || ev.DY != 0 {
		sender.SendMoonlightMouseMove(clamp(ev.DX), clamp(ev.DY))
	}
	if ev.ClickChanged {
		action := service.LiMouseButtonRelease
		if ev.ClickDown {
			action = service.LiMouseButtonPress
		}
		sender.SendMoonlightMouseButton(action, service.LiMouseButtonLeft)
	}
}
