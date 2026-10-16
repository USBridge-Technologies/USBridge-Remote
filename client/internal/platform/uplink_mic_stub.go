//go:build !cgo || (!linux && !windows && !darwin)

package platform

import "fmt"

// MicSupported: no microphone capture without cgo, or in the browser (the
// web client streams over WebRTC, which has no uplink).
func MicSupported() bool { return false }

func openMic() (micSource, error) {
	return nil, fmt.Errorf("microphone capture is not supported on this platform")
}
