package service

import (
	"math"
	"sync/atomic"
)

// videoDisplayAspect is the target screen's width/height the native GPU
// renderers show the picture in (float64 bits; 0 = the frame's own shape).
var videoDisplayAspect atomic.Uint64

// SetVideoDisplayAspect shows the picture in the target screen's aspect --
// width/height from the client's video source setting -- instead of the
// decoded frame's, where the native renderer supports it (Vulkan/GL on
// Linux, Windows and Android). A capture card's anamorphic modes (a 16:10
// monitor stretched into 1024x768) then look right; 0 restores the frame's
// own aspect. The absolute mouse follows the renderer's dest rect either way
// (NativeVideoDestRect), so it stays on the picture.
func SetVideoDisplayAspect(aspect float64) {
	if aspect < 0 || math.IsNaN(aspect) || math.IsInf(aspect, 0) {
		aspect = 0
	}
	videoDisplayAspect.Store(math.Float64bits(aspect))
	setNativeDisplayAspect(float32(aspect))
}

// VideoDisplayAspect is SetVideoDisplayAspect's last value.
func VideoDisplayAspect() float64 {
	return math.Float64frombits(videoDisplayAspect.Load())
}

// NativeDisplayAspect is SetVideoDisplayAspect's value where a native
// renderer applies it, 0 otherwise (and when unset).
func NativeDisplayAspect() float64 {
	if !nativeDisplayAspectSupported {
		return 0
	}
	return VideoDisplayAspect()
}
