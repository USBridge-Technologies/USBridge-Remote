//go:build cgo && !android && !ios && (linux || darwin || windows)

package service

/*
extern void phase_report_set_enabled(int on);
*/
import "C"

func init() {
	phaseSyncApplyFn = func(on bool) {
		v := C.int(0)
		if on {
			v = 1
		}
		C.phase_report_set_enabled(v)
	}
}
