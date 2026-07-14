//go:build darwin

package capture

/*
#cgo darwin LDFLAGS: -framework ApplicationServices
#include <ApplicationServices/ApplicationServices.h>
*/
import "C"

// physicalDisplaySize returns the maximum pixel resolution of the display at
// the given index by scanning all available display modes.
//
// CGDisplayPixelsWide/High and CGDisplayBounds both return logical points
// (e.g. 1440×900 on a Retina screen running at "looks like 1440×900").
// CGDisplayModeGetPixelWidth/Height on the current mode returns the render-
// buffer size (e.g. 2880×1800 at 2× scaling). Neither matches the physical
// LCD panel. Iterating CGDisplayCopyAllDisplayModes and taking the maximum
// pixel width/height yields the actual panel resolution (e.g. 2560×1600).
func physicalDisplaySize(index int) (int, int) {
	var count C.uint32_t
	if C.CGGetActiveDisplayList(0, nil, &count) != C.kCGErrorSuccess || count == 0 || index < 0 || index >= int(count) {
		return 0, 0
	}
	displays := make([]C.CGDirectDisplayID, count)
	if C.CGGetActiveDisplayList(count, &displays[0], &count) != C.kCGErrorSuccess {
		return 0, 0
	}
	id := displays[index]

	modes := C.CGDisplayCopyAllDisplayModes(id, 0)
	if modes == 0 {
		return 0, 0
	}
	defer C.CFRelease(C.CFTypeRef(modes))

	var maxW, maxH C.size_t
	n := C.CFArrayGetCount(modes)
	for i := C.CFIndex(0); i < n; i++ {
		mode := (C.CGDisplayModeRef)(C.CFArrayGetValueAtIndex(modes, i))
		w := C.CGDisplayModeGetPixelWidth(mode)
		h := C.CGDisplayModeGetPixelHeight(mode)
		if w > maxW {
			maxW = w
			maxH = h
		}
	}
	return int(maxW), int(maxH)
}
