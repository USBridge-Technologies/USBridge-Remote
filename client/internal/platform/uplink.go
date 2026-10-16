package platform

import "errors"

// A local MIDI input or microphone that the client plays on the streaming
// host (USBridge extension, see service.MoonlightUplinkSender).

// MIDIInputInfo is one local MIDI input.
type MIDIInputInfo struct {
	ID   string // stable while the device stays plugged in
	Name string
}

// CameraInfo is one local camera.
type CameraInfo struct {
	ID   string // stable while the camera stays plugged in
	Name string
}

// UplinkCapture is a running MIDI, microphone or camera capture.
type UplinkCapture interface {
	Stop()
}

// MicFrameSamples is one microphone frame: 20ms at 48kHz mono, what
// StartMicCapture hands over Opus-encoded.
const MicFrameSamples = 960

// ErrMicAccessDenied: the OS doesn't let this app use the microphone (on
// Windows, Settings → Privacy & security → Microphone).
var ErrMicAccessDenied = errors.New("microphone access is off in the system privacy settings")
