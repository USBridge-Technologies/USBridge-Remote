//go:build linux && !android

package platform

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

// ioctlEVIOCGNAME256 = _IOC(IOC_READ, 'E', 0x06, 256)
const ioctlEVIOCGNAME256 = uintptr(0x81004506)

// GamepadDevice describes a system gamepad.
type GamepadDevice struct {
	ID   string
	Name string
}

// EnumerateGamepads returns all gamepads currently connected to the system.
// Primary: parses /proc/bus/input/devices (world-readable, works without udev/input group).
// Fallback: scans /dev/input/event* via ioctl (requires read permission on the device).
func EnumerateGamepads() []GamepadDevice {
	if devices := parseProcInputDevices(); len(devices) > 0 {
		return devices
	}
	return scanEvdevDevices()
}

// parseProcInputDevices reads /proc/bus/input/devices which is readable by all users.
// Each device block is separated by a blank line and contains:
//
//	N: Name="Xbox 360 Wireless Receiver"
//	H: Handlers=js0 event7
func parseProcInputDevices() []GamepadDevice {
	f, err := os.Open("/proc/bus/input/devices")
	if err != nil {
		return nil
	}
	defer f.Close()

	var result []GamepadDevice
	var currentName string
	var currentHandlers []string

	flush := func() {
		defer func() {
			currentName = ""
			currentHandlers = nil
		}()
		if currentName == "" {
			return
		}
		// A "js" handler is the definitive sign of a joystick/gamepad.
		// Also accept devices whose name matches known gamepad keywords.
		hasJS := false
		var eventPath string
		for _, h := range currentHandlers {
			if strings.HasPrefix(h, "js") {
				hasJS = true
			}
			if strings.HasPrefix(h, "event") && eventPath == "" {
				eventPath = "/dev/input/" + h
			}
		}
		if (hasJS || isGamepadName(currentName)) && eventPath != "" {
			result = append(result, GamepadDevice{
				ID:   eventPath,
				Name: currentName,
			})
		}
	}

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case line == "":
			flush()
		case strings.HasPrefix(line, "N: Name="):
			name := strings.TrimPrefix(line, "N: Name=")
			currentName = strings.Trim(name, `"`)
		case strings.HasPrefix(line, "H: Handlers="):
			handlers := strings.TrimPrefix(line, "H: Handlers=")
			currentHandlers = strings.Fields(handlers)
		}
	}
	flush() // last device block (file may not end with blank line)

	return result
}

// scanEvdevDevices is the fallback: scans /dev/input/event* via ioctl.
// May return nothing if the process lacks read permission on the device nodes.
func scanEvdevDevices() []GamepadDevice {
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
		"playstation", "ps3", "ps4", "ps5", "8bitdo", "logitech f",
		"thrustmaster", "saitek", "hori", "razer", "raiju", "sixaxis",
		"steam", "switch pro", "nacon", "astro",
	}
	for _, kw := range keywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}
