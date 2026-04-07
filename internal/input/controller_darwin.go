//go:build darwin

package input

/*
#cgo darwin LDFLAGS: -framework ApplicationServices
#include <ApplicationServices/ApplicationServices.h>

static CGEventRef usbridge_create_keyboard_event(CGKeyCode key, int down) {
	return CGEventCreateKeyboardEvent(NULL, key, down ? true : false);
}

static CGEventRef usbridge_create_scroll_event(int32_t delta) {
	return CGEventCreateScrollWheelEvent(NULL, kCGScrollEventUnitLine, 1, delta);
}

static CGEventRef usbridge_create_event(void) {
	return CGEventCreate(NULL);
}

static CGEventRef usbridge_create_mouse_event(CGEventType eventType, CGPoint point, CGMouseButton button) {
	return CGEventCreateMouseEvent(NULL, eventType, point, button);
}
*/
import "C"

import (
	"fmt"
	"image"
	"math"
	"unicode/utf16"
)

func New() *Controller { return &Controller{} }

func (c *Controller) Key(key uint8) error {
	return c.tapHIDKey(key)
}

func (c *Controller) Combo(modifiers, key uint8) error {
	modKeys := modifierHIDKeys(modifiers)
	for _, mod := range modKeys {
		if err := c.postKey(mod, true); err != nil {
			return err
		}
	}
	if err := c.tapHIDKey(key); err != nil {
		_ = c.releaseModifiers(modKeys)
		return err
	}
	return c.releaseModifiers(modKeys)
}

func (c *Controller) Text(text string) error {
	for _, unit := range utf16.Encode([]rune(text)) {
		if err := postUnicode(unit, true); err != nil {
			return err
		}
		if err := postUnicode(unit, false); err != nil {
			return err
		}
	}
	return nil
}

func (c *Controller) MouseMove(dx, dy int8) error {
	loc := currentMouseLocation()
	target := image.Pt(int(loc.X)+int(dx), int(loc.Y)+int(dy))
	return c.postPointerMove(target)
}

func (c *Controller) MouseClick(button uint8) error {
	buttonType, downType, upType, ok := macButton(button)
	if !ok {
		return fmt.Errorf("unsupported button: %d", button)
	}
	loc := currentMouseLocation()
	if err := postMouseEvent(downType, buttonType, loc); err != nil {
		return err
	}
	return postMouseEvent(upType, buttonType, loc)
}

func (c *Controller) MouseScroll(delta int8) error {
	event := C.usbridge_create_scroll_event(C.int32_t(delta))
	if event == 0 {
		return fmt.Errorf("CGEventCreateScrollWheelEvent failed")
	}
	defer C.CFRelease(C.CFTypeRef(event))
	C.CGEventPost(C.kCGHIDEventTap, event)
	return nil
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
	point := macAbsolutePoint(x, y)

	c.mu.Lock()
	prev := c.buttonState
	c.buttonState = buttonsMask
	c.mu.Unlock()

	for _, change := range []struct {
		mask   uint8
		button C.CGMouseButton
		down   C.CGEventType
		up     C.CGEventType
	}{
		{mask: 0x01, button: C.kCGMouseButtonLeft, down: C.kCGEventLeftMouseDown, up: C.kCGEventLeftMouseUp},
		{mask: 0x02, button: C.kCGMouseButtonRight, down: C.kCGEventRightMouseDown, up: C.kCGEventRightMouseUp},
		{mask: 0x04, button: C.kCGMouseButtonCenter, down: C.kCGEventOtherMouseDown, up: C.kCGEventOtherMouseUp},
	} {
		prevOn := prev&change.mask != 0
		nextOn := buttonsMask&change.mask != 0
		if !prevOn && nextOn {
			if err := postMouseEventAt(change.down, change.button, point); err != nil {
				return err
			}
		}
		if prevOn && !nextOn {
			if err := postMouseEventAt(change.up, change.button, point); err != nil {
				return err
			}
		}
	}

	if err := c.postPointerMove(point); err != nil {
		return err
	}
	if wheel != 0 {
		return c.MouseScroll(wheel)
	}
	return nil
}

func (c *Controller) tapHIDKey(key uint8) error {
	if err := c.postKey(key, true); err != nil {
		return err
	}
	return c.postKey(key, false)
}

func (c *Controller) postKey(key uint8, down bool) error {
	spec, ok := hidSpec(key)
	if !ok {
		return fmt.Errorf("unsupported HID key: %d", key)
	}
	event := C.usbridge_create_keyboard_event(C.CGKeyCode(spec.macKeyCode), boolToCInt(down))
	if event == 0 {
		return fmt.Errorf("CGEventCreateKeyboardEvent failed")
	}
	defer C.CFRelease(C.CFTypeRef(event))
	C.CGEventPost(C.kCGHIDEventTap, event)
	return nil
}

func (c *Controller) releaseModifiers(modKeys []uint8) error {
	for i := len(modKeys) - 1; i >= 0; i-- {
		if err := c.postKey(modKeys[i], false); err != nil {
			return err
		}
	}
	return nil
}

func postUnicode(unit uint16, down bool) error {
	event := C.usbridge_create_keyboard_event(0, boolToCInt(down))
	if event == 0 {
		return fmt.Errorf("CGEventCreateKeyboardEvent failed")
	}
	defer C.CFRelease(C.CFTypeRef(event))
	value := C.UniChar(unit)
	C.CGEventKeyboardSetUnicodeString(event, 1, &value)
	C.CGEventPost(C.kCGHIDEventTap, event)
	return nil
}

func currentMouseLocation() image.Point {
	event := C.usbridge_create_event()
	if event == 0 {
		return image.Pt(0, 0)
	}
	defer C.CFRelease(C.CFTypeRef(event))
	loc := C.CGEventGetLocation(event)
	return image.Pt(int(loc.x), int(loc.y))
}

func macAbsolutePoint(x, y uint16) image.Point {
	bounds := macPrimaryBounds()
	px := bounds.Min.X + int(math.Round(float64(x)/65535.0*float64(maxInt(bounds.Dx()-1, 1))))
	py := bounds.Min.Y + int(math.Round(float64(y)/65535.0*float64(maxInt(bounds.Dy()-1, 1))))
	return image.Pt(px, py)
}

func macPrimaryBounds() image.Rectangle {
	rect := C.CGDisplayBounds(C.CGMainDisplayID())
	return image.Rect(int(rect.origin.x), int(rect.origin.y), int(rect.origin.x+rect.size.width), int(rect.origin.y+rect.size.height))
}

func (c *Controller) postPointerMove(point image.Point) error {
	var mouseType C.CGEventType = C.kCGEventMouseMoved
	var button C.CGMouseButton = C.kCGMouseButtonLeft

	c.mu.Lock()
	buttons := c.buttonState
	c.mu.Unlock()

	switch {
	case buttons&0x01 != 0:
		mouseType = C.kCGEventLeftMouseDragged
		button = C.kCGMouseButtonLeft
	case buttons&0x02 != 0:
		mouseType = C.kCGEventRightMouseDragged
		button = C.kCGMouseButtonRight
	case buttons&0x04 != 0:
		mouseType = C.kCGEventOtherMouseDragged
		button = C.kCGMouseButtonCenter
	}

	return postMouseEventAt(mouseType, button, point)
}

func macButton(button uint8) (C.CGMouseButton, C.CGEventType, C.CGEventType, bool) {
	switch button {
	case 1:
		return C.kCGMouseButtonLeft, C.kCGEventLeftMouseDown, C.kCGEventLeftMouseUp, true
	case 2:
		return C.kCGMouseButtonRight, C.kCGEventRightMouseDown, C.kCGEventRightMouseUp, true
	case 3:
		return C.kCGMouseButtonCenter, C.kCGEventOtherMouseDown, C.kCGEventOtherMouseUp, true
	default:
		return 0, 0, 0, false
	}
}

func postMouseEvent(eventType C.CGEventType, button C.CGMouseButton, point image.Point) error {
	return postMouseEventAt(eventType, button, point)
}

func postMouseEventAt(eventType C.CGEventType, button C.CGMouseButton, point image.Point) error {
	location := C.CGPoint{x: C.double(point.X), y: C.double(point.Y)}
	event := C.usbridge_create_mouse_event(eventType, location, button)
	if event == 0 {
		return fmt.Errorf("CGEventCreateMouseEvent failed")
	}
	defer C.CFRelease(C.CFTypeRef(event))
	C.CGEventPost(C.kCGHIDEventTap, event)
	return nil
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func boolToCInt(v bool) C.int {
	if v {
		return 1
	}
	return 0
}
