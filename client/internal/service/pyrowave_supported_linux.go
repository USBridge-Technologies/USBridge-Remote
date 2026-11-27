//go:build linux && !android && cgo

package service

/*
#include <stdint.h>
extern uint64_t pyrowave_linux_frames(void);
extern int vk_video_pyrowave_color_supported(int c444, int hdr);
*/
import "C"

import "sync"

var (
	linuxPwColorMu        sync.Mutex
	linuxPwColorSupported = map[[2]bool]bool{}
)

// PyroWaveDecodeSupported reports whether this client can decode a PyroWave
// stream (models.VideoModePyroWave). On Linux the decoder is built in:
// Vulkan compute, pyrowave_decode_linux.c / vk_video_impl_linux.c.
func PyroWaveDecodeSupported() bool {
	return true
}

// PyroWaveColorDecodeSupported reports whether this Linux GPU can decode
// PyroWave 4:4:4 and/or HDR10 (16-bit BT.2020 PQ) directly on Vulkan.
func PyroWaveColorDecodeSupported(color444, hdr bool) bool {
	if !color444 && !hdr {
		return PyroWaveDecodeSupported()
	}
	r := VideoRenderer()
	if r != VideoRendererAuto && r != VideoRendererVulkan {
		return false
	}
	key := [2]bool{color444, hdr}
	linuxPwColorMu.Lock()
	defer linuxPwColorMu.Unlock()
	if ok, done := linuxPwColorSupported[key]; done {
		return ok
	}
	c444, chdr := C.int(0), C.int(0)
	if color444 {
		c444 = 1
	}
	if hdr {
		chdr = 1
	}
	ok := C.vk_video_pyrowave_color_supported(c444, chdr) != 0
	linuxPwColorSupported[key] = ok
	return ok
}

// PyroWaveDecodedFrames counts the PyroWave frames the current stream has
// decoded, rendered or not.
func PyroWaveDecodedFrames() uint64 {
	return uint64(C.pyrowave_linux_frames())
}
