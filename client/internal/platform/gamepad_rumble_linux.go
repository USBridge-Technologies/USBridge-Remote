//go:build linux && !android

package platform

import (
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/sirupsen/logrus"
)

// Force feedback for a captured gamepad: the host's Moonlight rumble levels
// become one evdev FF_RUMBLE effect on the pad's event node (the kernel's xpad
// and hid drivers drive the motors from it).

const (
	linuxEvFF     = 0x15
	linuxFFRumble = 0x50

	// A rumble level stays until the next update, so the effect is made as
	// long as evdev allows and re-armed before it runs out.
	linuxFFLength   = 0xFFFF // ms
	linuxFFRearm    = 50 * time.Second
	linuxEviocsffN  = 0x80 // EVIOCSFF: _IOW('E', 0x80, struct ff_effect)
	linuxEviocrmffN = 0x81 // EVIOCRMFF: _IOW('E', 0x81, int)
)

// linuxFFEffect mirrors struct ff_effect on 64-bit Linux (48 bytes): the
// header is 14 bytes plus 2 of padding, then a union aligned to 8 bytes whose
// first 4 bytes are the rumble effect's strong/weak magnitudes.
type linuxFFEffect struct {
	Type            uint16
	ID              int16
	Direction       uint16
	TriggerButton   uint16
	TriggerInterval uint16
	ReplayLength    uint16
	ReplayDelay     uint16
	_               uint16
	U               [4]uint64
}

func linuxIoctlW(nr uintptr, size uintptr) uintptr {
	// _IOC(_IOC_WRITE, 'E', nr, size)
	return 1<<30 | size<<16 | 'E'<<8 | nr
}

type rumbleState struct {
	fd        int
	effectID  int16
	playing   bool
	low, high uint16
	rearm     *time.Timer
}

var (
	rumbleMu     sync.Mutex
	rumbleStates = map[string]*rumbleState{}
	rumbleFailed = map[string]bool{}
)

// SetGamepadRumble drives the pad's motors: low is the large (low-frequency)
// motor and high the small one, both 0..65535 (Moonlight's scale). 0,0 stops
// the vibration. id is the pad's /dev/input/eventN node.
func SetGamepadRumble(id string, low, high uint16) {
	rumbleMu.Lock()
	defer rumbleMu.Unlock()

	st := rumbleStates[id]
	if low == 0 && high == 0 {
		if st != nil {
			st.stopLocked()
		}
		return
	}
	if st == nil {
		if rumbleFailed[id] {
			return
		}
		fd, err := syscall.Open(id, syscall.O_RDWR|syscall.O_NONBLOCK, 0)
		if err != nil {
			rumbleFailed[id] = true
			logrus.Warnf("🎮 [Rumble] cannot open %s for force feedback: %v", id, err)
			return
		}
		st = &rumbleState{fd: fd, effectID: -1}
		rumbleStates[id] = st
	}
	if err := st.playLocked(low, high); err != nil {
		logrus.Warnf("🎮 [Rumble] %s: %v (this pad may not support force feedback)", id, err)
		st.closeLocked()
		delete(rumbleStates, id)
		rumbleFailed[id] = true
		return
	}
	// Re-arm before the effect's own length runs out.
	if st.rearm != nil {
		st.rearm.Stop()
	}
	st.rearm = time.AfterFunc(linuxFFRearm, func() {
		rumbleMu.Lock()
		defer rumbleMu.Unlock()
		if cur := rumbleStates[id]; cur == st && st.playing {
			_ = st.playLocked(st.low, st.high)
			st.rearm.Reset(linuxFFRearm)
		}
	})
}

// StopGamepadRumble silences the pad and releases its force-feedback effect.
func StopGamepadRumble(id string) {
	rumbleMu.Lock()
	defer rumbleMu.Unlock()
	if st := rumbleStates[id]; st != nil {
		st.closeLocked()
		delete(rumbleStates, id)
	}
	delete(rumbleFailed, id)
}

func (s *rumbleState) playLocked(low, high uint16) error {
	eff := linuxFFEffect{Type: linuxFFRumble, ID: s.effectID, ReplayLength: linuxFFLength}
	eff.U[0] = uint64(low) | uint64(high)<<16 // strong, weak
	req := linuxIoctlW(linuxEviocsffN, unsafe.Sizeof(eff))
	// Uploading with an existing ID updates the running effect in place.
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(s.fd), req, uintptr(unsafe.Pointer(&eff))); errno != 0 {
		return errno
	}
	s.effectID = eff.ID
	s.low, s.high = low, high
	if err := s.writeFF(1); err != nil {
		return err
	}
	s.playing = true
	return nil
}

func (s *rumbleState) stopLocked() {
	if s.rearm != nil {
		s.rearm.Stop()
	}
	if s.playing {
		_ = s.writeFF(0)
		s.playing = false
	}
}

func (s *rumbleState) closeLocked() {
	s.stopLocked()
	if s.effectID >= 0 {
		req := linuxIoctlW(linuxEviocrmffN, unsafe.Sizeof(int32(0)))
		syscall.Syscall(syscall.SYS_IOCTL, uintptr(s.fd), req, uintptr(s.effectID))
		s.effectID = -1
	}
	syscall.Close(s.fd)
}

// writeFF plays (1) or stops (0) the uploaded effect with an EV_FF event.
func (s *rumbleState) writeFF(value int32) error {
	var ev [linuxEventSize]byte // struct input_event: 16-byte timeval, type, code, value
	*(*uint16)(unsafe.Pointer(&ev[16])) = linuxEvFF
	*(*uint16)(unsafe.Pointer(&ev[18])) = uint16(s.effectID)
	*(*int32)(unsafe.Pointer(&ev[20])) = value
	_, err := syscall.Write(s.fd, ev[:])
	return err
}
