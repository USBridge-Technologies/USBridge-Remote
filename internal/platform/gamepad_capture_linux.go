//go:build linux && !android

package platform

import (
	"encoding/binary"
	"fmt"
	"syscall"
	"time"
	"unsafe"
)

const (
	linuxEvKey    = 1
	linuxEvAbs    = 3
	linuxEvSyn    = 0
	linuxEventSize = 24 // struct input_event on 64-bit Linux

	linuxAbsX     = 0
	linuxAbsY     = 1
	linuxAbsZ     = 2
	linuxAbsRX    = 3
	linuxAbsRY    = 4
	linuxAbsRZ    = 5
	linuxAbsHat0X = 16
	linuxAbsHat0Y = 17

	linuxBtnA       = 0x130
	linuxBtnB       = 0x131
	linuxBtnX       = 0x133
	linuxBtnY       = 0x134
	linuxBtnTL      = 0x136
	linuxBtnTR      = 0x137
	linuxBtnSelect  = 0x13A
	linuxBtnStart   = 0x13B
	linuxBtnMode    = 0x13C
	linuxBtnThumbL  = 0x13D
	linuxBtnThumbR  = 0x13E
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
	leftX, leftY int8
	rightX, rightY int8
	leftTrigger  uint8
	rightTrigger uint8
	hatX, hatY   int32
	axes         map[uint16]linuxAxisRange
}

func (s *linuxState) normalizeAxis(code uint16, raw int32) int8 {
	r, ok := s.axes[code]
	if !ok || r.max == r.min {
		return 0
	}
	v := float64(raw-r.min)/float64(r.max-r.min)*254.0 - 127.0
	if v > 127 {
		return 127
	}
	if v < -127 {
		return -127
	}
	return int8(v)
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
		buttons |= 0x0004
	} else if s.hatX > 0 {
		buttons |= 0x0008
	}
	if s.hatY < 0 {
		buttons |= 0x0001
	} else if s.hatY > 0 {
		buttons |= 0x0002
	}
	return GamepadCaptureState{
		Buttons:      buttons,
		LeftX:        int16(s.leftX) << 8,
		LeftY:        int16(s.leftY) << 8,
		RightX:       int16(s.rightX) << 8,
		RightY:       int16(s.rightY) << 8,
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
		}()

		st := &linuxState{axes: make(map[uint16]linuxAxisRange)}
		for _, axis := range []uint16{linuxAbsX, linuxAbsY, linuxAbsZ, linuxAbsRX, linuxAbsRY, linuxAbsRZ} {
			if info, ok := linuxEviocgabs(fd, axis); ok && info.Maximum > info.Minimum {
				st.axes[axis] = linuxAxisRange{min: info.Minimum, max: info.Maximum}
			}
		}

		buf := make([]byte, linuxEventSize)

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
				return
			}

			n2, err := syscall.Read(fd, buf)
			if err == syscall.EAGAIN || err == syscall.EWOULDBLOCK || err == syscall.EINTR {
				continue
			}
			if err != nil || n2 < linuxEventSize {
				return // device gone
			}

			evType := binary.LittleEndian.Uint16(buf[16:18])
			evCode := binary.LittleEndian.Uint16(buf[18:20])
			evValue := int32(binary.LittleEndian.Uint32(buf[20:24]))

			switch evType {
			case linuxEvKey:
				if bit, ok := linuxButtonMap[evCode]; ok {
					if evValue != 0 {
						st.buttons |= bit
					} else {
						st.buttons &^= bit
					}
				}
			case linuxEvAbs:
				switch uint16(evCode) {
				case linuxAbsX:
					st.leftX = st.normalizeAxis(linuxAbsX, evValue)
				case linuxAbsY:
					st.leftY = st.normalizeAxis(linuxAbsY, evValue)
				case linuxAbsRX:
					st.rightX = st.normalizeAxis(linuxAbsRX, evValue)
				case linuxAbsRY:
					st.rightY = st.normalizeAxis(linuxAbsRY, evValue)
				case linuxAbsZ:
					st.leftTrigger = st.normalizeTrigger(linuxAbsZ, evValue)
				case linuxAbsRZ:
					st.rightTrigger = st.normalizeTrigger(linuxAbsRZ, evValue)
				case linuxAbsHat0X:
					st.hatX = evValue
				case linuxAbsHat0Y:
					st.hatY = evValue
				}
			case linuxEvSyn:
				onState(st.toCapture())
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
