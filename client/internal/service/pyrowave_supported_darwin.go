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

// PyroWaveColorDecodeSupported: the 4:4:4 / HDR PyroWave streams
// (VIDEO_FORMAT_PYROWAVE_444 / _HDR) are decoded on Windows only so far; this
// decoder is 8-bit 4:2:0, so the client never asks for them here.
func PyroWaveColorDecodeSupported(color444, hdr bool) bool {
	return !color444 && !hdr && PyroWaveDecodeSupported()
}

// PyroWaveDecodedFrames counts the PyroWave frames the current stream has
// decoded, rendered or not.
func PyroWaveDecodedFrames() uint64 {
	return uint64(C.pyrowave_darwin_frames())
}
