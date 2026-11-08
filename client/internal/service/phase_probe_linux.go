//go:build linux && !android

package service

/*
extern void phase_probe_set(int reporting, int arm);
extern void phase_probe_set_enabled(int on);
extern int  phase_probe_dump(const char *path);
extern void phase_probe_last(long long *lead_us, long long *spread_us);
#include <stdlib.h>
*/
import "C"
import "unsafe"

func init() {
	phaseSyncApplyFn = func(on bool) {
		v := C.int(0)
		if on {
			v = 1
		}
		C.phase_probe_set_enabled(v)
	}
}

// PhaseProbeSet turns the 0x5530 reports on or off (which locks or releases the
// host within 3 s) and labels the frames recorded from here on with arm.
func PhaseProbeSet(reporting bool, arm int) {
	r := C.int(0)
	if reporting {
		r = 1
	}
	C.phase_probe_set(r, C.int(arm))
}

// PhaseProbeDump writes the per-frame record as CSV and returns its row count.
func PhaseProbeDump(path string) int {
	p := C.CString(path)
	defer C.free(unsafe.Pointer(p))
	return int(C.phase_probe_dump(p))
}

// PhaseProbeLast is the slack of the last window, as last reported.
func PhaseProbeLast() (leadUs, spreadUs int64) {
	var l, s C.longlong
	C.phase_probe_last(&l, &s)
	return int64(l), int64(s)
}
