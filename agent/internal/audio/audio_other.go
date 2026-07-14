//go:build !linux

package audio

import (
	"fmt"

	"usbridge_agent/internal/api"
)

// ListSinks/DefaultSink are not yet implemented for Windows/macOS (no
// pactl-equivalent wired up here yet); callers should treat the error as
// "audio device selection unavailable on this platform" rather than fatal.
func ListSinks() ([]api.AudioSink, error) {
	return nil, fmt.Errorf("audio device enumeration not implemented on this platform")
}

func DefaultSink() (string, error) {
	return "", fmt.Errorf("audio device enumeration not implemented on this platform")
}
