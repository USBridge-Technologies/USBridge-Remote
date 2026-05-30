//go:build linux && !android

package platform

import (
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

// ioctlEVIOCGNAME256 = _IOC(IOC_READ, 'E', 0x06, 256)
const ioctlEVIOCGNAME256 = uintptr(0x81004506)

func evdevName(path string) string {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return ""
	}
	defer syscall.Close(fd)

	buf := make([]byte, 256)
	r, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), ioctlEVIOCGNAME256, uintptr(unsafe.Pointer(&buf[0])))
	if errno != 0 || r == 0 {
		return ""
	}
	s := string(buf)
	if idx := strings.IndexByte(s, 0); idx >= 0 {
		s = s[:idx]
	}
	return strings.TrimSpace(s)
}

func isGamepadName(name string) bool {
	lower := strings.ToLower(name)
	keywords := []string{
		"gamepad", "controller", "xbox", "dualshock", "dualsense",
		"joystick", "game pad", "x360", "x-box", "x box", "joypad",
	}
	for _, kw := range keywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

// GamepadDevice describes a system gamepad.
type GamepadDevice struct {
	ID   string
	Name string
}

// EnumerateGamepads returns all gamepads currently connected to the system.
// On Linux it scans /dev/input/event* for devices with gamepad-like names.
func EnumerateGamepads() []GamepadDevice {
	entries, err := filepath.Glob("/dev/input/event*")
	if err != nil {
		return nil
	}
	var result []GamepadDevice
	for _, path := range entries {
		name := evdevName(path)
		if name == "" || !isGamepadName(name) {
			continue
		}
		result = append(result, GamepadDevice{ID: path, Name: name})
	}
	return result
}
