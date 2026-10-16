//go:build cgo && (darwin || ios || linux || windows)

package service

/*
#include <stdint.h>
#include <stdlib.h>
#include <Limelight.h>
#include <opus.h>

// The USBridge uplink extension of the stream, one copy for every native
// platform (Android included): MIDI, microphone and camera the host plays.
static int uplink_host_flags(void) {
    uint32_t f = LiGetHostFeatureFlags();
    return ((f & LI_FF_USBRIDGE_MIDI) ? 1 : 0) | ((f & LI_FF_USBRIDGE_MIC) ? 2 : 0) |
           ((f & LI_FF_USBRIDGE_CAMERA) ? 4 : 0);
}

static OpusEncoder *uplink_mic_encoder(int *err) {
    OpusEncoder *enc = opus_encoder_create(48000, 1, OPUS_APPLICATION_VOIP, err);
    if (enc) {
        opus_encoder_ctl(enc, OPUS_SET_BITRATE(48000));
        opus_encoder_ctl(enc, OPUS_SET_SIGNAL(OPUS_SIGNAL_VOICE));
    }
    return enc;
}

static int uplink_mic_encode(OpusEncoder *enc, const int16_t *pcm, int samples, unsigned char *out, int outlen) {
    return opus_encode(enc, pcm, samples, out, outlen);
}
*/
import "C"

import (
	"fmt"
	"sync"
	"unsafe"

	"usbridge-client/internal/platform"
)

func (w *MoonlightCgoWrapper) UplinkSupport() (midi, mic bool) {
	if !liStartConnectionActive.Load() {
		return false, false
	}
	f := C.uplink_host_flags()
	return f&1 != 0, f&2 != 0
}

func (w *MoonlightCgoWrapper) SendMoonlightMIDI(data []byte) bool {
	if !liStartConnectionActive.Load() || len(data) == 0 {
		return false
	}
	return C.LiSendMidiEvent((*C.uchar)(unsafe.Pointer(&data[0])), C.ushort(len(data))) == 0
}

func (w *MoonlightCgoWrapper) SendMoonlightMic(sequence uint16, opus []byte) bool {
	if !liStartConnectionActive.Load() || len(opus) == 0 {
		return false
	}
	return C.LiSendMicAudio(C.ushort(sequence), (*C.uchar)(unsafe.Pointer(&opus[0])), C.ushort(len(opus))) == 0
}

func (w *MoonlightCgoWrapper) CameraUplinkSupported() bool {
	return liStartConnectionActive.Load() && C.uplink_host_flags()&4 != 0
}

func (w *MoonlightCgoWrapper) SendMoonlightCamera(frame uint16, keyframe bool, au []byte) bool {
	if !liStartConnectionActive.Load() || len(au) == 0 {
		return false
	}
	flags := C.uchar(0)
	if keyframe {
		flags = 1
	}
	return C.LiSendCameraFrame(C.ushort(frame), flags, (*C.uchar)(unsafe.Pointer(&au[0])), C.uint(len(au))) == 0
}

// MicUplinkSupported reports whether StartMicUplink can work here.
func MicUplinkSupported() bool { return platform.MicSupported() }

type micUplink struct {
	capture platform.UplinkCapture
	enc     *C.OpusEncoder
	once    sync.Once
}

func (u *micUplink) Stop() {
	u.once.Do(func() {
		u.capture.Stop() // the frame callback below no longer runs after this
		C.opus_encoder_destroy(u.enc)
	})
}

// StartMicUplink records the default microphone (platform.StartMicCapture)
// and hands send each 20ms frame Opus-encoded (48kHz mono, at most 200
// bytes, what the host's microphone takes) with a running sequence number,
// from the capture's goroutine. The same on every platform; only the
// capture itself is the platform's.
func StartMicUplink(send func(seq uint16, opus []byte)) (platform.UplinkCapture, error) {
	var cerr C.int
	enc := C.uplink_mic_encoder(&cerr)
	if enc == nil {
		return nil, fmt.Errorf("opus_encoder_create: %d", int(cerr))
	}
	out := make([]byte, 200)
	var seq uint16
	capture, err := platform.StartMicCapture(func(pcm []int16) {
		n := C.uplink_mic_encode(enc, (*C.int16_t)(unsafe.Pointer(&pcm[0])), C.int(len(pcm)),
			(*C.uchar)(unsafe.Pointer(&out[0])), C.int(len(out)))
		seq++
		if n > 0 {
			send(seq, append([]byte(nil), out[:n]...))
		}
	})
	if err != nil {
		C.opus_encoder_destroy(enc)
		return nil, err
	}
	return &micUplink{capture: capture, enc: enc}, nil
}
