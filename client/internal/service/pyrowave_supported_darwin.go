//go:build darwin && !ios && cgo

package service

/*
#include <stdint.h>
extern uint64_t pyrowave_darwin_frames(void);
extern int pyrowave_darwin_supported(void);
*/
import "C"

// PyroWaveDecodeSupported reports whether this client can decode a PyroWave
// stream (models.VideoModePyroWave). On macOS the decoder is built in: native
// Metal compute (pyrowave_decode_darwin.m), on any GPU with 32-wide SIMD
// groups -- every Apple GPU, not an Intel Mac's AMD one. Checked once.
func PyroWaveDecodeSupported() bool {
	return C.pyrowave_darwin_supported() != 0
}

// PyroWaveColorDecodeSupported reports whether this client can decode the
// PyroWave color upgrades: 4:4:4 (color444, VIDEO_FORMAT_PYROWAVE_444) and/or
// HDR10 (hdr, VIDEO_FORMAT_PYROWAVE_HDR: 10-bit BT.2020 PQ). The Metal decoder
// takes every combination on any GPU it runs on -- it writes 16-bit and
// full-size chroma planes the same way as 8-bit 4:2:0 ones, into the CoreVideo
// formats Core Animation shows ('x420', '444v', ...; see pyrowave_decode_darwin.m).
func PyroWaveColorDecodeSupported(color444, hdr bool) bool {
	return PyroWaveDecodeSupported()
}

// PyroWaveDecodedFrames counts the PyroWave frames the current stream has
// decoded, rendered or not.
func PyroWaveDecodedFrames() uint64 {
	return uint64(C.pyrowave_darwin_frames())
}
