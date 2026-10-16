//go:build darwin

package monitors

/*
#cgo darwin LDFLAGS: -framework ApplicationServices
#include <ApplicationServices/ApplicationServices.h>
*/
import "C"

// ActiveDisplayIDs lists the CGDirectDisplayIDs of the active displays in
// CGGetActiveDisplayList order (main display first) -- the order the agent
// numbers monitors in ("display:N", see capture's screen_darwin.go).
func ActiveDisplayIDs() []uint32 {
	var count C.uint32_t
	if C.CGGetActiveDisplayList(0, nil, &count) != C.kCGErrorSuccess || count == 0 {
		return nil
	}
	ids := make([]C.CGDirectDisplayID, count)
	if C.CGGetActiveDisplayList(count, &ids[0], &count) != C.kCGErrorSuccess {
		return nil
	}
	out := make([]uint32, count)
	for i := range out {
		out[i] = uint32(ids[i])
	}
	return out
}
