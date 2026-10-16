//go:build !(linux && !android) && !windows && !(cgo && (darwin || android))

package platform

import "fmt"

// ListMIDIInputs: no MIDI input without cgo, or in the browser (the web
// client streams over WebRTC, which has no uplink).
func ListMIDIInputs() []MIDIInputInfo { return nil }

// StartMIDICapture: see ListMIDIInputs.
func StartMIDICapture(id string, onData func([]byte)) (UplinkCapture, error) {
	return nil, fmt.Errorf("MIDI input capture is not supported on this platform")
}
