//go:build android

package service

/*
#include <stdint.h>
extern int64_t android_max_hw_decode_size(int is_hevc);
*/
import "C"

import "strings"

// MaxHardwareDecodeResolution returns the maximum (width, height) supported by
// the device's hardware video decoder for videoMode ("h265"/"hevc" vs "h264").
// Returns (0, 0) if the capability could not be queried.
func MaxHardwareDecodeResolution(videoMode string) (int, int) {
	m := strings.ToLower(strings.TrimSpace(videoMode))
	isHevc := C.int(0)
	if m == "" || strings.Contains(m, "265") || strings.Contains(m, "hevc") {
		isHevc = 1
	}
	packed := uint64(C.android_max_hw_decode_size(isHevc))
	if packed == 0 {
		return 0, 0
	}
	return int(packed >> 32), int(packed & 0xFFFFFFFF)
}
