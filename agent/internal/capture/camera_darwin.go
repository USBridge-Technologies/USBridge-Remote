//go:build darwin

package capture

import (
	"encoding/json"
	"os/exec"

	"usbridge_agent/internal/api"
)

// cameraProfilerEntry mirrors one entry of `system_profiler SPCameraDataType -json`.
type cameraProfilerEntry struct {
	Name     string `json:"_name"`
	UniqueID string `json:"spcamera_unique-id"`
}

type cameraProfilerOutput struct {
	Cameras []cameraProfilerEntry `json:"SPCameraDataType"`
}

// cameraDevices enumerates built-in and USB/UVC cameras visible to
// AVFoundation (system_profiler shares the same device list). Sunshine's
// macOS capture backend selects a camera by this same unique ID via the
// "camera:<unique-id>" output_name convention (see videoSetDevice).
func cameraDevices() []api.VideoDeviceInfo {
	out, err := exec.Command("system_profiler", "SPCameraDataType", "-json").Output()
	if err != nil {
		return nil
	}

	var parsed cameraProfilerOutput
	if err := json.Unmarshal(out, &parsed); err != nil {
		return nil
	}

	devices := make([]api.VideoDeviceInfo, 0, len(parsed.Cameras))
	for i, cam := range parsed.Cameras {
		if cam.UniqueID == "" {
			continue
		}
		devices = append(devices, api.VideoDeviceInfo{
			Path:      "cam:" + cam.UniqueID,
			Name:      cam.Name,
			Bus:       "camera",
			Index:     i,
			Connected: true,
		})
	}
	return devices
}
