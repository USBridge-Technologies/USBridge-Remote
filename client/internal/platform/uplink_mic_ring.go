//go:build cgo && (windows || darwin || android)

package platform

/*
#include "uplink_mic_ring.h"
*/
import "C"

import (
	"fmt"
	"unsafe"
)

// ringMic is a microphone whose platform API pushes PCM into a mic_ring
// (uplink_mic_ring.h): WASAPI, AudioQueue, AAudio.
type ringMic struct{ r *C.mic_ring }

func openRingMic() (micSource, error) {
	var errBuf [256]C.char
	r := C.plat_mic_open(&errBuf[0], C.int(len(errBuf)))
	if r == nil {
		return nil, fmt.Errorf("%s", C.GoString(&errBuf[0]))
	}
	return &ringMic{r: r}, nil
}

func (m *ringMic) read(pcm []int16) (bool, error) {
	// A frame and a half: on time it's there, a stalled device times out.
	switch C.mic_ring_pop(m.r, (*C.int16_t)(unsafe.Pointer(&pcm[0])), C.int(len(pcm)), 30) {
	case 1:
		return true, nil
	case 0:
		return false, nil
	}
	return false, fmt.Errorf("the microphone went away")
}

func (m *ringMic) close() { C.plat_mic_close(m.r) }
