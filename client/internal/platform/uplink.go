package platform

import (
	"errors"
	"sync"
)

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
// Windows, Settings → Privacy & security → Microphone; on macOS, iOS and
// Android, the app's own microphone permission).
var ErrMicAccessDenied = errors.New("microphone access is off in the system privacy settings")

// MicAuth is what the OS says about this app using the microphone.
type MicAuth int

const (
	MicAuthGranted      MicAuth = iota
	MicAuthUndetermined         // not asked yet: RequestMicPermission shows the prompt
	MicAuthDenied               // refused: only the OS settings turn it back on (OpenMicSettings)
)

var micPermWaiters struct {
	sync.Mutex
	fns []func(granted bool)
}

// micPermRequest queues done for the answer and runs ask only for the
// first waiter: one prompt however many times the row is switched.
func micPermRequest(done func(granted bool), ask func()) {
	micPermWaiters.Lock()
	micPermWaiters.fns = append(micPermWaiters.fns, done)
	first := len(micPermWaiters.fns) == 1
	micPermWaiters.Unlock()
	if first {
		ask()
	}
}

// micPermAnswered hands the user's answer to everyone waiting for it.
func micPermAnswered(granted bool) {
	micPermWaiters.Lock()
	fns := micPermWaiters.fns
	micPermWaiters.fns = nil
	micPermWaiters.Unlock()
	for _, fn := range fns {
		fn(granted)
	}
}
