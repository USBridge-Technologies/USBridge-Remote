//go:build windows

package input

import (
	"fmt"
	"image"
	"math"
	"unicode/utf16"
	"unsafe"

	"github.com/kbinani/screenshot"
	"golang.org/x/sys/windows"
)

const (
	inputMouse    = 0
	inputKeyboard = 1

	keyeventfExtendedKey = 0x0001
	keyeventfKeyUp       = 0x0002
	keyeventfScancode    = 0x0008
	keyeventfUnicode     = 0x0004

	mouseeventfMove       = 0x0001
	mouseeventfLeftDown   = 0x0002
	mouseeventfLeftUp     = 0x0004
	mouseeventfRightDown  = 0x0008
	mouseeventfRightUp    = 0x0010
	mouseeventfMiddleDown = 0x0020
	mouseeventfMiddleUp   = 0x0040
	mouseeventfWheel      = 0x0800
)

type keyboardInput struct {
	WVk         uint16
	WScan       uint16
	DwFlags     uint32
	Time        uint32
	DwExtraInfo uintptr
}

type mouseInput struct {
	Dx          int32
	Dy          int32
	MouseData   uint32
	DwFlags     uint32
	Time        uint32
	DwExtraInfo uintptr
}

type keyboardPacket struct {
	Type uint32
	_    uint32
	Ki   keyboardInput
}

type mousePacket struct {
	Type uint32
	_    uint32
	Mi   mouseInput
}

var (
	user32           = windows.NewLazySystemDLL("user32.dll")
	procSendInput    = user32.NewProc("SendInput")
	procSetCursorPos = user32.NewProc("SetCursorPos")
)

func New() *Controller { return &Controller{} }

func (c *Controller) Key(key uint8) error {
	if err := c.sendHID(key, false); err != nil {
		return err
	}
	return c.sendHID(key, true)
}

func (c *Controller) Combo(modifiers, key uint8) error {
	modKeys := modifierHIDKeys(modifiers)
	for _, mod := range modKeys {
		if err := c.sendHID(mod, false); err != nil {
			return err
		}
	}
	if err := c.sendHID(key, false); err != nil {
		return err
	}
	if err := c.sendHID(key, true); err != nil {
		return err
	}
	for i := len(modKeys) - 1; i >= 0; i-- {
		if err := c.sendHID(modKeys[i], true); err != nil {
			return err
		}
	}
	return nil
}

func (c *Controller) Text(text string) error {
	for _, r := range utf16.Encode([]rune(text)) {
		if err := c.sendUnicode(r, 0); err != nil {
			return err
		}
		if err := c.sendUnicode(r, keyeventfKeyUp); err != nil {
			return err
		}
	}
	return nil
}

func (c *Controller) MouseMove(dx, dy int8) error {
	return c.sendMouse(mouseInput{Dx: int32(dx), Dy: int32(dy), DwFlags: mouseeventfMove})
}

func (c *Controller) MouseClick(button uint8) error {
	down, up, ok := buttonFlags(button)
	if !ok {
		return fmt.Errorf("unsupported button: %d", button)
	}
	if err := c.sendMouse(mouseInput{DwFlags: down}); err != nil {
		return err
	}
	return c.sendMouse(mouseInput{DwFlags: up})
}

func (c *Controller) MouseScroll(delta int8) error {
	return c.sendMouse(mouseInput{DwFlags: mouseeventfWheel, MouseData: uint32(int32(delta) * 120)})
}

func (c *Controller) MouseAction(button uint8, dx, dy, scroll int8) error {
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
	if scroll != 0 {
		return c.MouseScroll(scroll)
	}
	return nil
}

func (c *Controller) AbsoluteEvent(buttonsMask uint8, x, y uint16, wheel int8) error {
	if err := setCursorAbsolute(x, y); err != nil {
		return err
	}
	c.mu.Lock()
	prev := c.buttonState
	c.buttonState = buttonsMask
	c.mu.Unlock()
	for _, ch := range []struct {
		mask uint8
		down uint32
		up   uint32
	}{
		{0x01, mouseeventfLeftDown, mouseeventfLeftUp},
		{0x02, mouseeventfRightDown, mouseeventfRightUp},
		{0x04, mouseeventfMiddleDown, mouseeventfMiddleUp},
	} {
		prevOn := prev&ch.mask != 0
		nextOn := buttonsMask&ch.mask != 0
		if !prevOn && nextOn {
			if err := c.sendMouse(mouseInput{DwFlags: ch.down}); err != nil {
				return err
			}
		}
		if prevOn && !nextOn {
			if err := c.sendMouse(mouseInput{DwFlags: ch.up}); err != nil {
				return err
			}
		}
	}
	if wheel != 0 {
		return c.MouseScroll(wheel)
	}
	return nil
}

func (c *Controller) sendKey(vk, scan uint16, flags uint32) error {
	pkt := keyboardPacket{Type: inputKeyboard, Ki: keyboardInput{WVk: vk, WScan: scan, DwFlags: flags}}
	r1, _, err := procSendInput.Call(uintptr(1), uintptr(unsafe.Pointer(&pkt)), uintptr(unsafe.Sizeof(pkt)))
	if r1 == 0 {
		return err
	}
	return nil
}

func (c *Controller) sendHID(key uint8, up bool) error {
	spec, ok := hidSpec(key)
	if !ok {
		return fmt.Errorf("unsupported HID key: %d", key)
	}
	flags := uint32(keyeventfScancode)
	if spec.windowsExtended {
		flags |= keyeventfExtendedKey
	}
	if up {
		flags |= keyeventfKeyUp
	}
	return c.sendKey(0, spec.windowsScan, flags)
}

func (c *Controller) sendUnicode(scan uint16, flags uint32) error {
	pkt := keyboardPacket{Type: inputKeyboard, Ki: keyboardInput{WScan: scan, DwFlags: keyeventfUnicode | flags}}
	r1, _, err := procSendInput.Call(uintptr(1), uintptr(unsafe.Pointer(&pkt)), uintptr(unsafe.Sizeof(pkt)))
	if r1 == 0 {
		return err
	}
	return nil
}

func (c *Controller) sendMouse(mi mouseInput) error {
	pkt := mousePacket{Type: inputMouse, Mi: mi}
	r1, _, err := procSendInput.Call(uintptr(1), uintptr(unsafe.Pointer(&pkt)), uintptr(unsafe.Sizeof(pkt)))
	if r1 == 0 {
		return err
	}
	return nil
}

func setCursorAbsolute(x, y uint16) error {
	bounds := primaryBounds()
	px := bounds.Min.X + int(math.Round(float64(x)/65535.0*float64(bounds.Dx()-1)))
	py := bounds.Min.Y + int(math.Round(float64(y)/65535.0*float64(bounds.Dy()-1)))
	r1, _, err := procSetCursorPos.Call(uintptr(px), uintptr(py))
	if r1 == 0 {
		return err
	}
	return nil
}

func primaryBounds() image.Rectangle {
	if screenshot.NumActiveDisplays() == 0 {
		return image.Rect(0, 0, 1920, 1080)
	}
	return screenshot.GetDisplayBounds(0)
}

func buttonFlags(button uint8) (uint32, uint32, bool) {
	switch button {
	case 1:
		return mouseeventfLeftDown, mouseeventfLeftUp, true
	case 2:
		return mouseeventfRightDown, mouseeventfRightUp, true
	case 3:
		return mouseeventfMiddleDown, mouseeventfMiddleUp, true
	default:
		return 0, 0, false
	}
}
