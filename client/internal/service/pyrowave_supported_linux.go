//go:build linux && !android && cgo

package service

/*
#include <stdint.h>
extern uint64_t pyrowave_linux_frames(void);
*/
import "C"

// PyroWaveDecodeSupported reports whether this client can decode a PyroWave
// stream (models.VideoModePyroWave). On Linux the decoder is built in:
// Vulkan compute, pyrowave_decode_linux.c.
func PyroWaveDecodeSupported() bool {
	return true
}

// PyroWaveColorDecodeSupported: the 4:4:4 / HDR PyroWave streams
// (VIDEO_FORMAT_PYROWAVE_444 / _HDR) are decoded on Windows and macOS so far;
// this decoder is 8-bit 4:2:0, so the client never asks for them here.
func PyroWaveColorDecodeSupported(color444, hdr bool) bool {
	return !color444 && !hdr && PyroWaveDecodeSupported()
}

// PyroWaveDecodedFrames counts the PyroWave frames the current stream has
// decoded, rendered or not.
func PyroWaveDecodedFrames() uint64 {
	return uint64(C.pyrowave_linux_frames())
}
