//go:build windows

package input

import (
	"fmt"
	"image"
	"math"
	"sync"
	"unicode/utf16"
	"unsafe"

	"github.com/kbinani/screenshot"
	"golang.org/x/sys/windows"
)

const (
	inputMouse    = 0
	inputKeyboard = 1

	keyeventfKeyUp   = 0x0002
	keyeventfUnicode = 0x0004

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

type Controller struct {
	mu          sync.Mutex
	buttonState uint8
}

func New() *Controller { return &Controller{} }

func (c *Controller) Key(key uint8) error {
	vk, ok := hidToVK(key)
	if !ok {
		return fmt.Errorf("unsupported HID key: %d", key)
	}
	if err := c.sendKey(vk, 0); err != nil {
		return err
	}
	return c.sendKey(vk, keyeventfKeyUp)
}

func (c *Controller) Combo(modifiers, key uint8) error {
	modKeys := modifiersToVK(modifiers)
	for _, vk := range modKeys {
		if err := c.sendKey(vk, 0); err != nil {
			return err
		}
	}
	vk, ok := hidToVK(key)
	if !ok {
		return fmt.Errorf("unsupported HID key: %d", key)
	}
	if err := c.sendKey(vk, 0); err != nil {
		return err
	}
	if err := c.sendKey(vk, keyeventfKeyUp); err != nil {
		return err
	}
	for i := len(modKeys) - 1; i >= 0; i-- {
		if err := c.sendKey(modKeys[i], keyeventfKeyUp); err != nil {
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

func (c *Controller) sendKey(vk uint16, flags uint32) error {
	pkt := keyboardPacket{Type: inputKeyboard, Ki: keyboardInput{WVk: vk, DwFlags: flags}}
	r1, _, err := procSendInput.Call(uintptr(1), uintptr(unsafe.Pointer(&pkt)), uintptr(unsafe.Sizeof(pkt)))
	if r1 == 0 {
		return err
	}
	return nil
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

func modifiersToVK(modifiers uint8) []uint16 {
	out := make([]uint16, 0, 4)
	if modifiers&0x01 != 0 {
		out = append(out, 0x11)
	}
	if modifiers&0x02 != 0 {
		out = append(out, 0x10)
	}
	if modifiers&0x04 != 0 {
		out = append(out, 0x12)
	}
	if modifiers&0x08 != 0 {
		out = append(out, 0x5B)
	}
	return out
}

func hidToVK(key uint8) (uint16, bool) {
	switch {
	case key >= 4 && key <= 29:
		return uint16('A' + (key - 4)), true
	case key >= 30 && key <= 38:
		return uint16('1' + (key - 30)), true
	case key == 39:
		return '0', true
	}
	switch key {
	case 40:
		return 0x0D, true
	case 41:
		return 0x1B, true
	case 42:
		return 0x08, true
	case 43:
		return 0x09, true
	case 44:
		return 0x20, true
	case 58, 59, 60, 61, 62, 63, 64, 65, 66, 67, 68, 69:
		return 0x70 + uint16(key-58), true
	case 74:
		return 0x24, true
	case 75:
		return 0x21, true
	case 76:
		return 0x2E, true
	case 77:
		return 0x23, true
	case 78:
		return 0x22, true
	case 79:
		return 0x27, true
	case 80:
		return 0x25, true
	case 81:
		return 0x28, true
	case 82:
		return 0x26, true
	default:
		return 0, false
	}
}
