//go:build cgo && (darwin || android)

package platform

/*
#include <stdint.h>
*/
import "C"

import "unsafe"

// The entry the platform MIDI callbacks (uplink_midi_darwin.go,
// uplink_android.go) call with what a device played; see deliverMIDI.

//export usbridgeMIDIData
func usbridgeMIDIData(handle C.int, data *C.uchar, length C.int) {
	if data == nil || length <= 0 {
		return
	}
	deliverMIDI(int(handle), C.GoBytes(unsafe.Pointer(data), length))
}
