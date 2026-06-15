//go:build windows

package platform

import (
	"fmt"
	"time"
	"unsafe"

	"github.com/sirupsen/logrus"
)

var (
	procJoyGetPosEx = winmm.NewProc("joyGetPosEx")
)

// joyInfoEx matches the Win32 JOYINFOEX struct (52 bytes).
type joyInfoEx struct {
	dwSize         uint32
	dwFlags        uint32
	dwXpos         uint32
	dwYpos         uint32
	dwZpos         uint32
	dwRpos         uint32
	dwUpos         uint32
	dwVpos         uint32
	dwButtons      uint32
	dwButtonNumber uint32
	dwPOV          uint32
	dwReserved1    uint32
	dwReserved2    uint32
}

const (
	// joyGetPosEx flags
	joyReturnX       = 0x00000001
	joyReturnY       = 0x00000002
	joyReturnZ       = 0x00000004
	joyReturnR       = 0x00000008
	joyReturnU       = 0x00000010
	joyReturnV       = 0x00000020
	joyReturnPOV     = 0x00000040
	joyReturnButtons = 0x00000080
	joyReturnAll     = joyReturnX | joyReturnY | joyReturnZ | joyReturnR |
		joyReturnU | joyReturnV | joyReturnPOV | joyReturnButtons

	joyPOVCentered = 0xFFFF
	joyPOVForward  = 0
)

// winmmButtonToMoonlight maps WinMM button bit positions to Moonlight controller flags.
// This follows the standard XInput/Xbox button layout used by most modern gamepads.
var winmmButtonToMoonlight = [16]uint16{
	0x1000, // btn 0 (A)
	0x2000, // btn 1 (B)
	0x4000, // btn 2 (X)
	0x8000, // btn 3 (Y)
	0x0100, // btn 4 (LB)
	0x0200, // btn 5 (RB)
	0x0020, // btn 6 (Back/Select)
	0x0010, // btn 7 (Start)
	0x0040, // btn 8 (Left Thumb)
	0x0080, // btn 9 (Right Thumb)
	0x0400, // btn 10 (Guide)
	0, 0, 0, 0, 0, // btn 11-15 unmapped
}

// GamepadCaptureState holds the current decoded state of a gamepad.
type GamepadCaptureState struct {
	Buttons                   uint16
	LeftX, LeftY              int16
	RightX, RightY            int16
	LeftTrigger, RightTrigger uint8
}

// GamepadCapture manages an active WinMM joystick capture.
type GamepadCapture struct {
	joyID uint32
	stop  chan struct{}
	done  chan struct{}
}

// StartGamepadCapture opens the gamepad identified by deviceID ("winmm:N") and calls
// onState at ~60 Hz with the latest decoded input. Returns an error if the device cannot be opened.
func StartGamepadCapture(deviceID string, onState func(GamepadCaptureState)) (*GamepadCapture, error) {
	var joyID uint32
	if _, err := fmt.Sscanf(deviceID, "winmm:%d", &joyID); err != nil {
		return nil, fmt.Errorf("invalid Windows gamepad device ID %q (expected winmm:N)", deviceID)
	}

	var info joyInfo
	ret, _, _ := procJoyGetPos.Call(uintptr(joyID), uintptr(unsafe.Pointer(&info)))
	if ret != joyErrNoError {
		return nil, fmt.Errorf("gamepad winmm:%d not connected (joyGetPos returned %d)", joyID, ret)
	}

	caps, err := winmmGetCaps(joyID)
	if err != nil {
		return nil, fmt.Errorf("failed to get caps for winmm:%d: %w", joyID, err)
	}

	logrus.Infof("🎮 [WinMM] Starting capture for winmm:%d name=%q caps: x=[%d..%d] y=[%d..%d] z=[%d..%d] r=[%d..%d] u=[%d..%d] v=[%d..%d]",
		joyID, winmmJoystickName(uintptr(joyID)),
		caps.xMin, caps.xMax, caps.yMin, caps.yMax,
		caps.zMin, caps.zMax, caps.rMin, caps.rMax,
		caps.uMin, caps.uMax, caps.vMin, caps.vMax)

	cap := &GamepadCapture{
		joyID: joyID,
		stop:  make(chan struct{}),
		done:  make(chan struct{}),
	}

	go func() {
		defer close(cap.done)
		defer logrus.Infof("🎮 [WinMM] Capture stopped for winmm:%d", joyID)

		ticker := time.NewTicker(16 * time.Millisecond) // ~60 Hz
		defer ticker.Stop()

		var logSeq uint64
		for {
			select {
			case <-cap.stop:
				return
			case <-ticker.C:
				state, ok := pollWinMMState(joyID, caps)
				if !ok {
					logrus.Warnf("🎮 [WinMM] Device winmm:%d disconnected during capture", joyID)
					return
				}
				logSeq++
				hasInput := state.Buttons != 0 || state.LeftTrigger != 0 || state.RightTrigger != 0 ||
					state.LeftX != 0 || state.LeftY != 0 || state.RightX != 0 || state.RightY != 0
				if hasInput {
					logrus.Infof("🎮 [WinMM] winmm:%d buttons=0x%04x lt=%d rt=%d lx=%d ly=%d rx=%d ry=%d",
						joyID, state.Buttons, state.LeftTrigger, state.RightTrigger,
						state.LeftX, state.LeftY, state.RightX, state.RightY)
				} else if logSeq%300 == 1 {
					logrus.Debugf("🎮 [WinMM] winmm:%d idle heartbeat #%d", joyID, logSeq)
				}
				onState(state)
			}
		}
	}()

	return cap, nil
}

// Stop halts the capture goroutine.
func (c *GamepadCapture) Stop() {
	close(c.stop)
	<-c.done
}

type winmmCaps struct {
	xMin, xMax uint32
	yMin, yMax uint32
	zMin, zMax uint32 // Z  = Left Trigger
	rMin, rMax uint32 // R  = Right Stick X (Rx)
	uMin, uMax uint32 // U  = Right Stick Y (Ry)
	vMin, vMax uint32 // V  = Right Trigger (Rz)
}

func winmmGetCaps(joyID uint32) (winmmCaps, error) {
	var raw joyCapsW
	ret, _, _ := procJoyGetDevCapsW.Call(
		uintptr(joyID),
		uintptr(unsafe.Pointer(&raw)),
		unsafe.Sizeof(raw),
	)
	if ret != joyErrNoError {
		return winmmCaps{}, fmt.Errorf("joyGetDevCapsW returned %d", ret)
	}
	return winmmCaps{
		xMin: raw.wXmin, xMax: raw.wXmax,
		yMin: raw.wYmin, yMax: raw.wYmax,
		zMin: raw.wZmin, zMax: raw.wZmax,
		rMin: raw.wRmin, rMax: raw.wRmax,
		uMin: raw.wUmin, uMax: raw.wUmax,
		vMin: raw.wVmin, vMax: raw.wVmax,
	}, nil
}

func pollWinMMState(joyID uint32, caps winmmCaps) (GamepadCaptureState, bool) {
	var info joyInfoEx
	info.dwSize = uint32(unsafe.Sizeof(info))
	info.dwFlags = joyReturnAll

	ret, _, _ := procJoyGetPosEx.Call(uintptr(joyID), uintptr(unsafe.Pointer(&info)))
	if ret != joyErrNoError {
		return GamepadCaptureState{}, false
	}

	// Map buttons: iterate over the lower 16 bits of dwButtons.
	var buttons uint16
	for i := 0; i < 16; i++ {
		if info.dwButtons&(1<<uint(i)) != 0 {
			buttons |= winmmButtonToMoonlight[i]
		}
	}

	// POV hat → D-pad bits. dwPOV is in 100ths of a degree (0=North, 9000=East…).
	if info.dwPOV != joyPOVCentered {
		deg := info.dwPOV / 100
		if deg <= 45 || deg >= 315 {
			buttons |= 0x0001 // DPAD_UP
		}
		if deg >= 45 && deg <= 135 {
			buttons |= 0x0008 // DPAD_RIGHT
		}
		if deg >= 135 && deg <= 225 {
			buttons |= 0x0002 // DPAD_DOWN
		}
		if deg >= 225 && deg <= 315 {
			buttons |= 0x0004 // DPAD_LEFT
		}
	}

	// WinMM axis → Moonlight axis mapping for Xbox-layout controllers.
	// WinMM / DirectInput axis order: X=LeftX, Y=LeftY, Z=LT, R=RightX, U=RightY, V=RT.
	// Y axes are inverted: WinMM min=up, Moonlight positive=up, so negate both LY and RY.
	return GamepadCaptureState{
		Buttons:      buttons,
		LeftX:        winmmScaleAxis(info.dwXpos, caps.xMin, caps.xMax),
		LeftY:        -winmmScaleAxis(info.dwYpos, caps.yMin, caps.yMax),
		RightX:       winmmScaleAxis(info.dwRpos, caps.rMin, caps.rMax),
		RightY:       -winmmScaleAxis(info.dwUpos, caps.uMin, caps.uMax),
		LeftTrigger:  winmmScaleTrigger(info.dwZpos, caps.zMin, caps.zMax),
		RightTrigger: winmmScaleTrigger(info.dwVpos, caps.vMin, caps.vMax),
	}, true
}

// winmmScaleAxis maps a raw WinMM axis value (min..max) to int16 (-32767..32767).
func winmmScaleAxis(val, min, max uint32) int16 {
	if max <= min {
		return 0
	}
	mid := (int64(max) + int64(min)) / 2
	half := (int64(max) - int64(min)) / 2
	if half == 0 {
		return 0
	}
	scaled := (int64(val)-mid)*32767/half
	if scaled > 32767 {
		return 32767
	}
	if scaled < -32767 {
		return -32767
	}
	return int16(scaled)
}

// winmmScaleTrigger maps a raw WinMM axis value (min..max) to uint8 (0..255).
func winmmScaleTrigger(val, min, max uint32) uint8 {
	if max <= min {
		return 0
	}
	scaled := (int64(val)-int64(min))*255 / (int64(max) - int64(min))
	if scaled < 0 {
		return 0
	}
	if scaled > 255 {
		return 255
	}
	return uint8(scaled)
}

// Ensure proc reference is live (prevents dead-code elimination by some linkers).
var _ = procJoyGetPosEx
