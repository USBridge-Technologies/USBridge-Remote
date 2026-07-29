//go:build rustshine && !linux && !windows

package streamhost

// ListCaptureDevices: no confirmed --list-capture-devices output shape for
// macOS in the private repo at the time this was written; callers fall
// back to their own OS-native enumeration.
func (b *rustshineBackend) ListCaptureDevices() []CaptureDevice { return nil }
