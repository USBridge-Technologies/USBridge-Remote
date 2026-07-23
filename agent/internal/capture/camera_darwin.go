//go:build darwin

package capture

/*
#cgo LDFLAGS: -framework AVFoundation -framework CoreMedia
#include <stdlib.h>
extern char *usbridge_enumerate_cameras(void);
extern char *usbridge_camera_formats(const char *uniqueID);
*/
import "C"

import (
	"sort"
	"strconv"
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
			Path:           "cam:" + uniqueID,
			Name:           name,
			Bus:            "camera",
			Index:          i,
			Connected:      true,
			SupportedModes: cameraFormats(uniqueID),
		})
	}
	return devices
}

// cameraFormats reports the real capture formats a camera supports, straight
// from AVCaptureDevice.formats (via usbridge_camera_formats in
// camera_enum_darwin.m) — the camera equivalent of GetDisplayModes for
// monitors, but real discrete modes instead of a guessed resolution ladder,
// since unlike monitors a camera's AVCaptureDeviceFormat list is the actual
// ground truth of what it can deliver.
func cameraFormats(uniqueID string) []api.VideoCaptureMode {
	cUniqueID := C.CString(uniqueID)
	defer C.free(unsafe.Pointer(cUniqueID))

	cStr := C.usbridge_camera_formats(cUniqueID)
	if cStr == nil {
		return nil
	}
	defer C.free(unsafe.Pointer(cStr))
	out := C.GoString(cStr)

	var modes []api.VideoCaptureMode
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) != 3 {
			continue
		}
		width, err := strconv.Atoi(parts[0])
		if err != nil {
			continue
		}
		height, err := strconv.Atoi(parts[1])
		if err != nil {
			continue
		}
		var fps []int
		for _, f := range strings.Split(parts[2], ",") {
			if v, err := strconv.Atoi(f); err == nil {
				fps = append(fps, v)
			}
		}
		sort.Ints(fps)
		modes = append(modes, api.VideoCaptureMode{Width: width, Height: height, FPS: fps})
	}

	// Largest resolution first, matching GetDisplayModes' ordering for monitors.
	sort.Slice(modes, func(i, j int) bool {
		if modes[i].Width != modes[j].Width {
			return modes[i].Width > modes[j].Width
		}
		return modes[i].Height > modes[j].Height
	})
	return modes
}
