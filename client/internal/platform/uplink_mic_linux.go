//go:build linux && !android && cgo

package platform

/*
#cgo pkg-config: libpulse-simple
#include <stdio.h>
#include <stdlib.h>
#include <pulse/simple.h>
#include <pulse/error.h>

static pa_simple *pulse_mic_open(char *err, int errlen) {
    int e = 0;
    pa_sample_spec ss = { .format = PA_SAMPLE_S16LE, .rate = 48000, .channels = 1 };
    // Small fragments: a frame is read as soon as it is captured.
    pa_buffer_attr attr = { .maxlength = (uint32_t)-1, .tlength = (uint32_t)-1,
        .prebuf = (uint32_t)-1, .minreq = (uint32_t)-1, .fragsize = 960 * 2 };
    pa_simple *pa = pa_simple_new(NULL, "USBridge", PA_STREAM_RECORD, NULL, "Microphone to the remote PC",
                                  &ss, NULL, &attr, &e);
    if (!pa) snprintf(err, errlen, "PulseAudio: %s", pa_strerror(e));
    return pa;
}

static int pulse_mic_read(pa_simple *pa, short *pcm, int samples) {
    int e = 0;
    return pa_simple_read(pa, pcm, (size_t)samples * 2, &e);
}
*/
import "C"

import (
	"fmt"
	"unsafe"
)

// MicSupported reports whether StartMicCapture can work in this build.
func MicSupported() bool { return true }

// pulseMic is the default microphone through PulseAudio (or PipeWire's
// Pulse server).
type pulseMic struct{ pa *C.pa_simple }

func openMic() (micSource, error) {
	var errBuf [256]C.char
	pa := C.pulse_mic_open(&errBuf[0], C.int(len(errBuf)))
	if pa == nil {
		return nil, fmt.Errorf("%s", C.GoString(&errBuf[0]))
	}
	return &pulseMic{pa: pa}, nil
}

// read blocks until a whole frame is captured: never longer than a frame.
func (m *pulseMic) read(pcm []int16) (bool, error) {
	if C.pulse_mic_read(m.pa, (*C.short)(unsafe.Pointer(&pcm[0])), C.int(len(pcm))) < 0 {
		return false, fmt.Errorf("PulseAudio read failed")
	}
	return true, nil
}

func (m *pulseMic) close() { C.pa_simple_free(m.pa) }
