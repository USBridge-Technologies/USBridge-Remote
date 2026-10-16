//go:build !(linux || windows) || android || !cgo

package platform

import "fmt"

// MicSupported: microphone capture is implemented on Linux and Windows.
func MicSupported() bool { return false }

// StartMicCapture: see MicSupported.
func StartMicCapture(onFrame func(seq uint16, opus []byte)) (UplinkCapture, error) {
	return nil, fmt.Errorf("microphone capture is not supported on this platform")
}
