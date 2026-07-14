//go:build linux

package input

import (
	"fmt"
	"log"
	"os"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

// uinput ioctl constants for linux/amd64
// _IO('U', n)        = 0x5500 | n
// _IOW('U', n, int)  = 0x40045500 | n  (int = 4 bytes)
// _IOW('U', n, T)    = 0x40000000 | (sizeof(T)<<16) | 0x5500 | n
const (
	evSyn = 0x00
	evKey = 0x01
	evRel = 0x02
	evAbs = 0x03

	synReport = 0

	btnLeft   = 0x110
	btnRight  = 0x111
	btnMiddle = 0x112

	relX     = 0x00
	relY     = 0x01
	relWheel = 0x08

	absX = 0x00
	absY = 0x01

	busUsb = 0x03

	inputPropPointer = 0x00

	uiDevCreate = 0x5501 // _IO('U', 1)
	uiSetEvbit  = 0x40045564 // _IOW('U', 100, int)
	uiSetKeybit = 0x40045565 // _IOW('U', 101, int)
	uiSetRelbit = 0x40045566 // _IOW('U', 102, int)
	uiSetAbsbit = 0x40045567 // _IOW('U', 103, int)
	uiSetPropbit = 0x4004556e // _IOW('U', 110, int)

	// UI_DEV_SETUP = _IOW('U', 3, uinput_setup{8+80+4=92=0x5c bytes})
	uiDevSetup = 0x405c5503
	// UI_ABS_SETUP = _IOW('U', 4, uinput_abs_setup{2+2pad+24=28=0x1c bytes})
	uiAbsSetup = 0x401c5504
)

// inputEvent matches struct input_event on linux/amd64 (24 bytes)
type inputEvent struct {
	Sec   uint64
	Usec  uint64
	Type  uint16
	Code  uint16
	Value int32
}

// inputID matches struct input_id (8 bytes)
type inputID struct {
	Bustype uint16
	Vendor  uint16
	Product uint16
	Version uint16
}

// uinputSetup matches struct uinput_setup (92 bytes)
type uinputSetup struct {
	ID           inputID
	Name         [80]byte
	FfEffectsMax uint32
}

// inputAbsinfo matches struct input_absinfo (24 bytes)
type inputAbsinfo struct {
	Value      int32
	Minimum    int32
	Maximum    int32
	Fuzz       int32
	Flat       int32
	Resolution int32
}

// uinputAbsSetup matches struct uinput_abs_setup (28 bytes)
type uinputAbsSetup struct {
	Code    uint16
	_       [2]byte
	Absinfo inputAbsinfo
}

var (
	uinputInitMu   sync.Mutex
	uinputLastTry  time.Time
	uinputRetryGap = 3 * time.Second
)

func New() *Controller {
	c := &Controller{}
	c.tryInitUinput()
	return c
}

func (c *Controller) tryInitUinput() {
	uinputInitMu.Lock()
	defer uinputInitMu.Unlock()

	if c.mouseFile != nil && c.kbdFile != nil {
		return
	}
	if !uinputLastTry.IsZero() && time.Since(uinputLastTry) < uinputRetryGap {
		return
	}
	uinputLastTry = time.Now()

	mouseFile, err := createMouseDevice()
	if err != nil {
		log.Printf("[input] uinput mouse init failed: %v", err)
		return
	}
	kbdFile, err := createKeyboardDevice()
	if err != nil {
		log.Printf("[input] uinput keyboard init failed: %v", err)
		mouseFile.Close()
		return
	}
	c.mouseFile = mouseFile
	c.kbdFile = kbdFile
	log.Printf("[input] uinput devices initialized")
}

func ioctlInt(fd uintptr, req uintptr, val int) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, req, uintptr(val))
	if errno != 0 {
		return errno
	}
	return nil
}

func ioctlPtr(fd uintptr, req uintptr, ptr unsafe.Pointer) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, req, uintptr(ptr))
	if errno != 0 {
		return errno
	}
	return nil
}

func createMouseDevice() (*os.File, error) {
	f, err := os.OpenFile("/dev/uinput", os.O_WRONLY, 0)
	if err != nil {
		return nil, fmt.Errorf("open /dev/uinput: %w", err)
	}
	fd := f.Fd()

	for _, evbit := range []int{evKey, evRel, evAbs} {
		if err := ioctlInt(fd, uiSetEvbit, evbit); err != nil {
			f.Close()
			return nil, fmt.Errorf("UI_SET_EVBIT %d: %w", evbit, err)
		}
	}
	for _, btn := range []int{btnLeft, btnRight, btnMiddle} {
		if err := ioctlInt(fd, uiSetKeybit, btn); err != nil {
			f.Close()
			return nil, fmt.Errorf("UI_SET_KEYBIT 0x%x: %w", btn, err)
		}
	}
	for _, rel := range []int{relX, relY, relWheel} {
		if err := ioctlInt(fd, uiSetRelbit, rel); err != nil {
			f.Close()
			return nil, fmt.Errorf("UI_SET_RELBIT %d: %w", rel, err)
		}
	}
	for _, abs := range []int{absX, absY} {
		if err := ioctlInt(fd, uiSetAbsbit, abs); err != nil {
			f.Close()
			return nil, fmt.Errorf("UI_SET_ABSBIT %d: %w", abs, err)
		}
	}
	// INPUT_PROP_POINTER marks this as an indirect-coordinate pointer device
	if err := ioctlInt(fd, uiSetPropbit, inputPropPointer); err != nil {
		log.Printf("[input] UI_SET_PROPBIT INPUT_PROP_POINTER: %v (ignored)", err)
	}

	setup := uinputSetup{
		ID: inputID{Bustype: busUsb, Vendor: 0x1234, Product: 0x5678, Version: 1},
	}
	copy(setup.Name[:], "usbridge-mouse")
	if err := ioctlPtr(fd, uiDevSetup, unsafe.Pointer(&setup)); err != nil {
		f.Close()
		return nil, fmt.Errorf("UI_DEV_SETUP: %w", err)
	}

	for _, axis := range []uinputAbsSetup{
		{Code: absX, Absinfo: inputAbsinfo{Maximum: absoluteCoordinateMax}},
		{Code: absY, Absinfo: inputAbsinfo{Maximum: absoluteCoordinateMax}},
	} {
		if err := ioctlPtr(fd, uiAbsSetup, unsafe.Pointer(&axis)); err != nil {
			f.Close()
			return nil, fmt.Errorf("UI_ABS_SETUP axis %d: %w", axis.Code, err)
		}
	}

	if err := ioctlInt(fd, uiDevCreate, 0); err != nil {
		f.Close()
		return nil, fmt.Errorf("UI_DEV_CREATE: %w", err)
	}

	time.Sleep(100 * time.Millisecond)
	return f, nil
}

func createKeyboardDevice() (*os.File, error) {
	f, err := os.OpenFile("/dev/uinput", os.O_WRONLY, 0)
	if err != nil {
		return nil, fmt.Errorf("open /dev/uinput: %w", err)
	}
	fd := f.Fd()

	if err := ioctlInt(fd, uiSetEvbit, evKey); err != nil {
		f.Close()
		return nil, fmt.Errorf("UI_SET_EVBIT EV_KEY: %w", err)
	}
	for _, spec := range hidKeyTable {
		_ = ioctlInt(fd, uiSetKeybit, int(spec.linuxKey))
	}

	setup := uinputSetup{
		ID: inputID{Bustype: busUsb, Vendor: 0x1234, Product: 0x5679, Version: 1},
	}
	copy(setup.Name[:], "usbridge-keyboard")
	if err := ioctlPtr(fd, uiDevSetup, unsafe.Pointer(&setup)); err != nil {
		f.Close()
		return nil, fmt.Errorf("UI_DEV_SETUP: %w", err)
	}
	if err := ioctlInt(fd, uiDevCreate, 0); err != nil {
		f.Close()
		return nil, fmt.Errorf("UI_DEV_CREATE: %w", err)
	}

	time.Sleep(100 * time.Millisecond)
	return f, nil
}

func writeEvent(f *os.File, evType, code uint16, value int32) error {
	now := time.Now()
	ev := inputEvent{
		Sec:   uint64(now.Unix()),
		Usec:  uint64(now.Nanosecond() / 1000),
		Type:  evType,
		Code:  code,
		Value: value,
	}
	b := unsafe.Slice((*byte)(unsafe.Pointer(&ev)), unsafe.Sizeof(ev))
	_, err := f.Write(b)
	return err
}

func syncReport(f *os.File) error {
	return writeEvent(f, evSyn, synReport, 0)
}

func (c *Controller) Key(keycode uint8) error {
	c.tryInitUinput()
	if c.kbdFile == nil {
		return nil
	}
	spec, ok := hidSpec(keycode)
	if !ok {
		return fmt.Errorf("unsupported HID key: %d", keycode)
	}
	if err := writeEvent(c.kbdFile, evKey, spec.linuxKey, 1); err != nil {
		return err
	}
	if err := syncReport(c.kbdFile); err != nil {
		return err
	}
	if err := writeEvent(c.kbdFile, evKey, spec.linuxKey, 0); err != nil {
		return err
	}
	return syncReport(c.kbdFile)
}

func (c *Controller) Combo(mod, key uint8) error {
	c.tryInitUinput()
	if c.kbdFile == nil {
		return nil
	}
	modKeys := modifierHIDKeys(mod)
	for _, m := range modKeys {
		spec, ok := hidSpec(m)
		if !ok {
			continue
		}
		if err := writeEvent(c.kbdFile, evKey, spec.linuxKey, 1); err != nil {
			return err
		}
		if err := syncReport(c.kbdFile); err != nil {
			return err
		}
	}
	if err := c.Key(key); err != nil {
		return err
	}
	for i := len(modKeys) - 1; i >= 0; i-- {
		spec, ok := hidSpec(modKeys[i])
		if !ok {
			continue
		}
		if err := writeEvent(c.kbdFile, evKey, spec.linuxKey, 0); err != nil {
			return err
		}
		if err := syncReport(c.kbdFile); err != nil {
			return err
		}
	}
	return nil
}

func (c *Controller) Text(text string) error {
	return nil
}

func (c *Controller) MouseMove(dx, dy int8) error {
	c.tryInitUinput()
	if c.mouseFile == nil {
		return nil
	}
	if dx != 0 {
		if err := writeEvent(c.mouseFile, evRel, relX, int32(dx)); err != nil {
			return err
		}
	}
	if dy != 0 {
		if err := writeEvent(c.mouseFile, evRel, relY, int32(dy)); err != nil {
			return err
		}
	}
	return syncReport(c.mouseFile)
}

func (c *Controller) MouseClick(button uint8) error {
	if c.mouseFile == nil {
		return nil
	}
	btn, ok := linuxMouseButton(button)
	if !ok {
		return fmt.Errorf("unsupported button: %d", button)
	}
	if err := writeEvent(c.mouseFile, evKey, btn, 1); err != nil {
		return err
	}
	if err := syncReport(c.mouseFile); err != nil {
		return err
	}
	if err := writeEvent(c.mouseFile, evKey, btn, 0); err != nil {
		return err
	}
	return syncReport(c.mouseFile)
}

func (c *Controller) MouseScroll(dy int8) error {
	if c.mouseFile == nil {
		return nil
	}
	if err := writeEvent(c.mouseFile, evRel, relWheel, int32(dy)); err != nil {
		return err
	}
	return syncReport(c.mouseFile)
}

func (c *Controller) MouseAction(button uint8, dx, dy, wheel int8) error {
	if dx != 0 || dy != 0 {
		if err := c.MouseMove(dx, dy); err != nil {
			return err
		}
	}
	if button != 0 {
		if err := c.MouseClick(button); err != nil {
			return err
		}
	}
	if wheel != 0 {
		return c.MouseScroll(wheel)
	}
	return nil
}

func (c *Controller) AbsoluteEvent(buttonsMask uint8, x, y uint16, wheel int8) error {
	c.tryInitUinput()
	if c.mouseFile == nil {
		return nil
	}

	c.mu.Lock()
	prev := c.buttonState
	c.buttonState = buttonsMask
	c.mu.Unlock()

	if err := writeEvent(c.mouseFile, evAbs, absX, int32(x)); err != nil {
		return err
	}
	if err := writeEvent(c.mouseFile, evAbs, absY, int32(y)); err != nil {
		return err
	}

	for _, ch := range []struct {
		mask uint8
		btn  uint16
	}{
		{0x01, btnLeft},
		{0x02, btnRight},
		{0x04, btnMiddle},
	} {
		prevOn := prev&ch.mask != 0
		nextOn := buttonsMask&ch.mask != 0
		if !prevOn && nextOn {
			if err := writeEvent(c.mouseFile, evKey, ch.btn, 1); err != nil {
				return err
			}
		}
		if prevOn && !nextOn {
			if err := writeEvent(c.mouseFile, evKey, ch.btn, 0); err != nil {
				return err
			}
		}
	}

	if err := syncReport(c.mouseFile); err != nil {
		return err
	}
	if wheel != 0 {
		return c.MouseScroll(wheel)
	}
	return nil
}

func linuxMouseButton(button uint8) (uint16, bool) {
	switch button {
	case 1:
		return btnLeft, true
	case 2:
		return btnRight, true
	case 3:
		return btnMiddle, true
	default:
		return 0, false
	}
}
