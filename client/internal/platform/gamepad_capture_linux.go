//go:build linux && !android

package platform

import (
	"encoding/binary"
	"fmt"
	"syscall"
	"time"
	"unsafe"

	"github.com/sirupsen/logrus"
)

const (
	linuxEvKey     = 1
	linuxEvAbs     = 3
	linuxEvSyn     = 0
	linuxEventSize = 24 // struct input_event on 64-bit Linux

	linuxAbsX     = 0
	linuxAbsY     = 1
	linuxAbsZ     = 2
	linuxAbsRX    = 3
	linuxAbsRY    = 4
	linuxAbsRZ    = 5
	linuxAbsHat0X = 16
	linuxAbsHat0Y = 17

	linuxBtnA         = 0x130
	linuxBtnB         = 0x131
	linuxBtnX         = 0x133
	linuxBtnY         = 0x134
	linuxBtnTL        = 0x136
	linuxBtnTR        = 0x137
	linuxBtnSelect    = 0x13A
	linuxBtnStart     = 0x13B
	linuxBtnMode      = 0x13C
	linuxBtnThumbL    = 0x13D
	linuxBtnThumbR    = 0x13E
	linuxBtnDpadUp    = 0x220
	linuxBtnDpadDown  = 0x221
	linuxBtnDpadLeft  = 0x222
	linuxBtnDpadRight = 0x223
)

var linuxButtonMap = map[uint16]uint16{
	linuxBtnDpadUp:    0x0001,
	linuxBtnDpadDown:  0x0002,
	linuxBtnDpadLeft:  0x0004,
	linuxBtnDpadRight: 0x0008,
	linuxBtnStart:     0x0010,
	linuxBtnSelect:    0x0020,
	linuxBtnThumbL:    0x0040,
	linuxBtnThumbR:    0x0080,
	linuxBtnTL:        0x0100,
	linuxBtnTR:        0x0200,
	linuxBtnMode:      0x0400,
	linuxBtnA:         0x1000,
	linuxBtnB:         0x2000,
	linuxBtnX:         0x4000,
	linuxBtnY:         0x8000,
}

type linuxInputAbsinfo struct {
	Value, Minimum, Maximum, Fuzz, Flat, Resolution int32
}

func linuxEviocgabs(fd int, axis uint16) (linuxInputAbsinfo, bool) {
	var info linuxInputAbsinfo
	ioc := uintptr(0x80000000) | uintptr(unsafe.Sizeof(info))<<16 | uintptr('E')<<8 | uintptr(0x40+axis)
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), ioc, uintptr(unsafe.Pointer(&info)))
	return info, errno == 0
}

type linuxAxisRange struct{ min, max int32 }

type linuxState struct {
	buttons      uint16
	leftTrigger  uint8
	rightTrigger uint8
	hatX, hatY   int32
	axes         map[uint16]linuxAxisRange
	// Raw int32 values from evdev for full 16-bit precision.
	leftXRaw, leftYRaw   int32
	rightXRaw, rightYRaw int32
}

// rawAxisToInt16 maps raw evdev value to signed 16-bit [-32768..32767].
func (s *linuxState) rawAxisToInt16(code uint16, raw int32) int16 {
	r, ok := s.axes[code]
	if !ok || r.max == r.min {
		return 0
	}
	v := (int64(raw-r.min) * 65535) / int64(r.max-r.min)
	v -= 32768
	if v > 32767 {
		return 32767
	}
	if v < -32768 {
		return -32768
	}
	return int16(v)
}

func (s *linuxState) normalizeTrigger(code uint16, raw int32) uint8 {
	r, ok := s.axes[code]
	if !ok || r.max == r.min {
		return 0
	}
	v := float64(raw-r.min) / float64(r.max-r.min) * 255.0
	if v > 255 {
		return 255
	}
	if v < 0 {
		return 0
	}
	return uint8(v)
}

func (s *linuxState) toCapture() GamepadCaptureState {
	buttons := s.buttons
	if s.hatX < 0 {
		buttons |= 0x0004 // D-pad Left
	} else if s.hatX > 0 {
		buttons |= 0x0008 // D-pad Right
	}
	if s.hatY < 0 {
		buttons |= 0x0001 // D-pad Up
	} else if s.hatY > 0 {
		buttons |= 0x0002 // D-pad Down
	}
	// Negate Y axes: evdev convention is Y-positive=down, but Moonlight/XInput
	// expects Y-positive=up. Both LY and RY must be inverted.
	return GamepadCaptureState{
		Buttons:      buttons,
		LeftX:        s.rawAxisToInt16(linuxAbsX, s.leftXRaw),
		LeftY:        -s.rawAxisToInt16(linuxAbsY, s.leftYRaw),
		RightX:       s.rawAxisToInt16(linuxAbsRX, s.rightXRaw),
		RightY:       -s.rawAxisToInt16(linuxAbsRY, s.rightYRaw),
		LeftTrigger:  s.leftTrigger,
		RightTrigger: s.rightTrigger,
	}
}

// GamepadCaptureState holds the current decoded state of a gamepad.
type GamepadCaptureState struct {
	Buttons                   uint16
	LeftX, LeftY              int16
	RightX, RightY            int16
	LeftTrigger, RightTrigger uint8
}

// GamepadCapture manages an active evdev capture for one gamepad.
type GamepadCapture struct {
	stop chan struct{}
	done chan struct{}
}

// StartGamepadCapture opens the evdev device (deviceID = path like /dev/input/event3)
// and calls onState on each EV_SYN event at the hardware report rate (~60–125 Hz).
func StartGamepadCapture(deviceID string, onState func(GamepadCaptureState)) (*GamepadCapture, error) {
	fd, err := syscall.Open(deviceID, syscall.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", deviceID, err)
	}

	cap := &GamepadCapture{
		stop: make(chan struct{}),
		done: make(chan struct{}),
	}

	go func() {
		defer func() {
			syscall.Close(fd)
			close(cap.done)
			logrus.Infof("🎮 [LinuxCapture] Stopped: %s", deviceID)
		}()

		st := &linuxState{axes: make(map[uint16]linuxAxisRange)}
		axisName := map[uint16]string{linuxAbsX: "LX", linuxAbsY: "LY", linuxAbsZ: "LT", linuxAbsRX: "RX", linuxAbsRY: "RY", linuxAbsRZ: "RT"}
		for _, axis := range []uint16{linuxAbsX, linuxAbsY, linuxAbsZ, linuxAbsRX, linuxAbsRY, linuxAbsRZ} {
			if info, ok := linuxEviocgabs(fd, axis); ok && info.Maximum > info.Minimum {
				st.axes[axis] = linuxAxisRange{min: info.Minimum, max: info.Maximum}
				logrus.Infof("🎮 [LinuxCapture] axis %s (code=%d) range=[%d..%d]",
					axisName[axis], axis, info.Minimum, info.Maximum)
			} else {
				logrus.Warnf("🎮 [LinuxCapture] axis %s (code=%d) calibration failed", axisName[axis], axis)
			}
		}
		logrus.Infof("🎮 [LinuxCapture] Ready: %s", deviceID)

		buf := make([]byte, linuxEventSize)
		var logSeq uint64

		// Human-readable button names for logging
		btnName := map[uint16]string{
			linuxBtnA: "A", linuxBtnB: "B", linuxBtnX: "X", linuxBtnY: "Y",
			linuxBtnTL: "LB", linuxBtnTR: "RB", linuxBtnSelect: "Back", linuxBtnStart: "Start",
			linuxBtnMode: "Guide", linuxBtnThumbL: "L3", linuxBtnThumbR: "R3",
			linuxBtnDpadUp: "DUp", linuxBtnDpadDown: "DDown", linuxBtnDpadLeft: "DLeft", linuxBtnDpadRight: "DRight",
		}

		for {
			select {
			case <-cap.stop:
				return
			default:
			}

			var rSet syscall.FdSet
			rSet.Bits[fd/64] |= 1 << (uint(fd) % 64)
			tv := syscall.Timeval{Sec: 0, Usec: int64(100 * time.Millisecond / time.Microsecond)}
			n, err := syscall.Select(fd+1, &rSet, nil, nil, &tv)
			if err == syscall.EINTR || n == 0 {
				continue
			}
			if err != nil {
				logrus.Warnf("🎮 [LinuxCapture] select error: %v", err)
				return
			}

			n2, err := syscall.Read(fd, buf)
			if err == syscall.EAGAIN || err == syscall.EWOULDBLOCK || err == syscall.EINTR {
				continue
			}
			if err != nil || n2 < linuxEventSize {
				logrus.Warnf("🎮 [LinuxCapture] device gone: %v", err)
				return
			}

			evType := binary.LittleEndian.Uint16(buf[16:18])
			evCode := binary.LittleEndian.Uint16(buf[18:20])
			evValue := int32(binary.LittleEndian.Uint32(buf[20:24]))

			switch evType {
			case linuxEvKey:
				if bit, ok := linuxButtonMap[evCode]; ok {
					prev := st.buttons
					if evValue != 0 {
						st.buttons |= bit
					} else {
						st.buttons &^= bit
					}
					if st.buttons != prev {
						name := btnName[evCode]
						if name == "" {
							name = "?"
						}
						action := "RELEASE"
						if evValue != 0 {
							action = "PRESS"
						}
						logrus.Infof("🎮 [LinuxCapture] %s %s (code=0x%04x buttons=0x%04x)", name, action, evCode, st.buttons)
					}
				} else {
					logrus.Debugf("🎮 [LinuxCapture] unknown KEY code=0x%04x val=%d", evCode, evValue)
				}
			case linuxEvAbs:
				switch uint16(evCode) {
				case linuxAbsX:
					st.leftXRaw = evValue
					logrus.Debugf("🎮 [LinuxCapture] ABS LX raw=%d", evValue)
				case linuxAbsY:
					st.leftYRaw = evValue
					logrus.Debugf("🎮 [LinuxCapture] ABS LY raw=%d (will invert)", evValue)
				case linuxAbsRX:
					st.rightXRaw = evValue
					logrus.Debugf("🎮 [LinuxCapture] ABS RX raw=%d", evValue)
				case linuxAbsRY:
					st.rightYRaw = evValue
					logrus.Debugf("🎮 [LinuxCapture] ABS RY raw=%d (will invert)", evValue)
				case linuxAbsZ:
					st.leftTrigger = st.normalizeTrigger(linuxAbsZ, evValue)
					logrus.Debugf("🎮 [LinuxCapture] ABS LT raw=%d → %d", evValue, st.leftTrigger)
				case linuxAbsRZ:
					st.rightTrigger = st.normalizeTrigger(linuxAbsRZ, evValue)
					logrus.Debugf("🎮 [LinuxCapture] ABS RT raw=%d → %d", evValue, st.rightTrigger)
				case linuxAbsHat0X:
					st.hatX = evValue
					logrus.Debugf("🎮 [LinuxCapture] HAT X=%d", evValue)
				case linuxAbsHat0Y:
					st.hatY = evValue
					logrus.Debugf("🎮 [LinuxCapture] HAT Y=%d", evValue)
				default:
					logrus.Debugf("🎮 [LinuxCapture] unknown ABS code=%d val=%d", evCode, evValue)
				}
			case linuxEvSyn:
				state := st.toCapture()
				logSeq++
				hasInput := state.Buttons != 0 || state.LeftTrigger != 0 || state.RightTrigger != 0 ||
					state.LeftX != 0 || state.LeftY != 0 || state.RightX != 0 || state.RightY != 0
				if hasInput {
					logrus.Infof("🎮 [LinuxCapture] SYN #%d buttons=0x%04x lt=%d rt=%d lx=%d ly=%d rx=%d ry=%d",
						logSeq, state.Buttons, state.LeftTrigger, state.RightTrigger,
						state.LeftX, state.LeftY, state.RightX, state.RightY)
				}
				onState(state)
			}
		}
	}()

	return cap, nil
}

// Stop halts the capture goroutine and closes the evdev fd.
func (c *GamepadCapture) Stop() {
	close(c.stop)
	<-c.done
}
