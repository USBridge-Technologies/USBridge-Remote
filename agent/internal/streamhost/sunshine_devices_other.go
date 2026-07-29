//go:build !linux && !windows

package streamhost

// ListCaptureDevices has no Sunshine-log-derived equivalent on this OS
// (macOS's Sunshine backend doesn't log a device enumeration block the way
// Linux/Windows do); callers fall back to their own OS-native enumeration.
func (b *sunshineBackend) ListCaptureDevices() []CaptureDevice { return nil }
