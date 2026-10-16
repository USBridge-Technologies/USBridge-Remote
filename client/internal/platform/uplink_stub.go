//go:build !(linux || windows) || android

package platform

import "fmt"

// ListMIDIInputs: MIDI input capture is implemented on Linux and Windows.
func ListMIDIInputs() []MIDIInputInfo { return nil }

// StartMIDICapture: see ListMIDIInputs.
func StartMIDICapture(id string, onData func([]byte)) (UplinkCapture, error) {
	return nil, fmt.Errorf("MIDI input capture is not supported on this platform")
}
