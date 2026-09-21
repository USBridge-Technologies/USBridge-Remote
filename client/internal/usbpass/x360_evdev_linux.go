//go:build linux

package usbpass

// evdev source for the synthetic Xbox 360 controller on a Linux client. The
// kernel's xpad driver already speaks every Xbox protocol (360, One/GIP, Series)
// to the real pad, so instead of claiming the pad raw -- which a Windows importer
// cannot start, a GIP pad announces itself only once after power-up -- the client
// leaves xpad bound, reads the pad's input events and presents them as an Xbox
// 360 controller (x360_backend.go). Rumble comes back through evdev force
// feedback. No libusb, no cgo.

import (
	"encoding/binary"
	"fmt"
	"os"
	"sync"
	"syscall"
	"unsafe"

	"github.com/sirupsen/logrus"
)

// ioctl request numbers (linux/input.h); the direction/size layout is the same
// on amd64, arm64 and x86.
func evIOC(dir, nr, size uintptr) uintptr { return dir<<30 | size<<16 | 'E'<<8 | nr }

const (
	iocWrite = 1
	iocRead  = 2

	ffRumble = 0x50
	ffEffect = 48 // sizeof(struct ff_effect)
)

var (
	evIOCGRAB = evIOC(iocWrite, 0x90, 4)
	evIOCSFF  = evIOC(iocWrite, 0x80, ffEffect)
	evIOCRMFF = evIOC(iocWrite, 0x81, 4)
)

func evIOCGBIT(ev, n uintptr) uintptr { return evIOC(iocRead, 0x20+ev, n) }
func evIOCGABS(abs uintptr) uintptr   { return evIOC(iocRead, 0x40+abs, 24) }
func evIOCGKEY(n uintptr) uintptr     { return evIOC(iocRead, 0x18, n) }

var evSize = int(unsafe.Sizeof(syscall.Timeval{})) + 8 // struct input_event

type evdevPad struct {
	f    *os.File
	name string
	m    *evdevMapper

	ffMu      sync.Mutex
	ffID      int16
	ffLoaded  bool
	ffPlaying bool
	grabbed   bool
}

func (p *evdevPad) ioctl(req uintptr, arg unsafe.Pointer) error {
	rc, err := p.f.SyscallConn()
	if err != nil {
		return err
	}
	var errno syscall.Errno
	if cerr := rc.Control(func(fd uintptr) {
		_, _, errno = syscall.Syscall(syscall.SYS_IOCTL, fd, req, uintptr(arg))
	}); cerr != nil {
		return cerr
	}
	if errno != 0 {
		return errno
	}
	return nil
}

func bitSet(mask []byte, bit int) bool {
	return bit/8 < len(mask) && mask[bit/8]&(1<<(uint(bit)%8)) != 0
}

// openEvdevPad opens an evdev node, checks it is a gamepad (BTN_SOUTH plus a
// left stick) and reads its axis ranges and current state.
func openEvdevPad(path string) (*evdevPad, error) {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	p := &evdevPad{f: f, name: path, m: newEvdevMapper(), ffID: -1}

	var keys [96]byte
	var abs [8]byte
	if err := p.ioctl(evIOCGBIT(evKey, uintptr(len(keys))), unsafe.Pointer(&keys[0])); err != nil {
		f.Close()
		return nil, fmt.Errorf("EVIOCGBIT(EV_KEY): %w", err)
	}
	if err := p.ioctl(evIOCGBIT(evAbs, uintptr(len(abs))), unsafe.Pointer(&abs[0])); err != nil {
		f.Close()
		return nil, fmt.Errorf("EVIOCGBIT(EV_ABS): %w", err)
	}
	if !bitSet(keys[:], btnSouth) || !bitSet(abs[:], absX) || !bitSet(abs[:], absY) {
		f.Close()
		return nil, fmt.Errorf("%s is not a gamepad", path)
	}
	p.syncState(abs[:])
	return p, nil
}

// syncState loads the axes' real ranges/values and the held buttons, so a stick
// or button already held when the export starts (or after SYN_DROPPED) is right.
func (p *evdevPad) syncState(absMask []byte) {
	for code := uint16(0); code < absCount; code++ {
		if !bitSet(absMask, int(code)) {
			continue
		}
		var info [6]int32 // value, minimum, maximum, fuzz, flat, resolution
		if err := p.ioctl(evIOCGABS(uintptr(code)), unsafe.Pointer(&info[0])); err != nil {
			continue
		}
		p.m.setRange(code, info[1], info[2])
		p.m.setAbs(code, info[0])
	}
	var held [96]byte
	if err := p.ioctl(evIOCGKEY(uintptr(len(held))), unsafe.Pointer(&held[0])); err == nil {
		for code := range evdevButtons {
			p.m.setKey(code, bitSet(held[:], int(code)))
		}
	}
}

func (p *evdevPad) absMask() []byte {
	var abs [8]byte
	_ = p.ioctl(evIOCGBIT(evAbs, uintptr(len(abs))), unsafe.Pointer(&abs[0]))
	return abs[:]
}

// grab takes the pad exclusively so games on this machine do not also react to
// input that belongs to the remote one.
func (p *evdevPad) grab() {
	one := int32(1)
	if err := p.ioctl(evIOCGRAB, unsafe.Pointer(&one)); err != nil {
		logrus.Warnf("usbpass: x360: cannot grab %s exclusively (%v); local applications will see the pad too", p.name, err)
		return
	}
	p.grabbed = true
}

// run reads events until the device goes away or the pad is closed, calling set
// with each new state. It returns when the read fails.
func (p *evdevPad) run(set func(X360State)) {
	buf := make([]byte, evSize*64)
	tv := evSize - 8
	set(p.m.state())
	for {
		n, err := p.f.Read(buf)
		if err != nil {
			set(X360State{}) // pad gone or closed: release everything
			return
		}
		for off := 0; off+evSize <= n; off += evSize {
			typ := binary.LittleEndian.Uint16(buf[off+tv:])
			code := binary.LittleEndian.Uint16(buf[off+tv+2:])
			val := int32(binary.LittleEndian.Uint32(buf[off+tv+4:]))
			switch typ {
			case evKey:
				p.m.setKey(code, val != 0)
			case evAbs:
				p.m.setAbs(code, val)
			case evSyn:
				switch code {
				case synReport:
					set(p.m.state())
				case synDropped:
					p.syncState(p.absMask())
				}
			}
		}
	}
}

func (p *evdevPad) writeEvent(typ, code uint16, val int32) {
	ev := make([]byte, evSize)
	tv := evSize - 8
	binary.LittleEndian.PutUint16(ev[tv:], typ)
	binary.LittleEndian.PutUint16(ev[tv+2:], code)
	binary.LittleEndian.PutUint32(ev[tv+4:], uint32(val))
	_, _ = p.f.Write(ev)
}

// rumble drives the pad's two motors (left = strong/low frequency, right =
// weak/high frequency), 0-255 each; 0/0 stops.
func (p *evdevPad) rumble(left, right uint8) {
	p.ffMu.Lock()
	defer p.ffMu.Unlock()
	if left == 0 && right == 0 {
		if p.ffPlaying {
			p.writeEvent(evFF, uint16(p.ffID), 0)
			p.ffPlaying = false
		}
		return
	}
	var eff [ffEffect]byte
	binary.LittleEndian.PutUint16(eff[0:], ffRumble)
	binary.LittleEndian.PutUint16(eff[2:], uint16(p.ffID)) // -1 asks the kernel for a new id
	binary.LittleEndian.PutUint16(eff[10:], 0xFFFF)        // replay.length: the importer re-sends or stops
	binary.LittleEndian.PutUint16(eff[16:], uint16(left)*257)
	binary.LittleEndian.PutUint16(eff[18:], uint16(right)*257)
	if err := p.ioctl(evIOCSFF, unsafe.Pointer(&eff[0])); err != nil {
		logrus.Debugf("usbpass: x360: EVIOCSFF: %v", err)
		return
	}
	p.ffID = int16(binary.LittleEndian.Uint16(eff[2:]))
	p.ffLoaded = true
	p.writeEvent(evFF, uint16(p.ffID), 1)
	p.ffPlaying = true
}

// close stops rumble, gives the pad back to local applications and unblocks run.
func (p *evdevPad) close() {
	p.ffMu.Lock()
	if p.ffLoaded {
		if p.ffPlaying {
			p.writeEvent(evFF, uint16(p.ffID), 0)
		}
		id := int32(p.ffID)
		_ = p.ioctl(evIOCRMFF, unsafe.Pointer(&id))
		p.ffLoaded, p.ffPlaying = false, false
	}
	p.ffMu.Unlock()
	if p.grabbed {
		zero := int32(0)
		_ = p.ioctl(evIOCGRAB, unsafe.Pointer(&zero))
	}
	_ = p.f.Close()
}
