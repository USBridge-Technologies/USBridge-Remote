//go:build darwin

package capture

/*
#cgo LDFLAGS: -framework AVFoundation
#include <stdlib.h>
extern char *usbridge_enumerate_cameras(void);
*/
import "C"

import (
	"strings"
	"unsafe"

	"usbridge_agent/internal/api"
)

// cameraDevices enumerates cameras the same way Sunshine's own capture
// backend does (platf::enumerate_camera_devices in display.mm, via
// usbridge_enumerate_cameras in camera_enum_darwin.m):
// AVCaptureDeviceTypeBuiltInWideAngleCamera plus, on macOS 14+,
// AVCaptureDeviceTypeExternal for USB/UVC capture devices (e.g. an
// HDMI/UVC capture dongle that presents itself as a camera). This used to
// shell out to `system_profiler SPCameraDataType`, which only reports
// built-in/Continuity cameras and silently omits external UVC devices — a
// USB capture dongle never showed up as selectable even though Sunshine
// itself could open it by unique ID once picked.
func cameraDevices() []api.VideoDeviceInfo {
	cStr := C.usbridge_enumerate_cameras()
	if cStr == nil {
		return nil
	}
	defer C.free(unsafe.Pointer(cStr))
	out := C.GoString(cStr)

	var devices []api.VideoDeviceInfo
	for i, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		uniqueID := parts[0]
		if uniqueID == "" {
			continue
		}
		name := uniqueID
		if len(parts) == 2 && parts[1] != "" {
			name = parts[1]
		}
		devices = append(devices, api.VideoDeviceInfo{
			Path:      "cam:" + uniqueID,
			Name:      name,
			Bus:       "camera",
			Index:     i,
			Connected: true,
		})
	}
	return devices
}
