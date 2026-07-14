package capture

import (
	"fmt"
	"github.com/kbinani/screenshot"
	"usbridge_agent/internal/api"
)

// standardResolutions lists common streaming resolutions from highest to lowest.
// GetDisplayModes filters this list to those that fit within the native display size.
var standardResolutions = []struct{ w, h int }{
	{3840, 2160},
	{2560, 1440},
	{1920, 1080},
	{1280, 720},
	{640, 480},
}

var standardFPS = []int{30, 60, 120, 144, 165, 240}

func GetCommonModes() []api.VideoCaptureMode {
	return []api.VideoCaptureMode{
		{Width: 1920, Height: 1080, FPS: standardFPS},
		{Width: 1280, Height: 720, FPS: standardFPS},
		{Width: 640, Height: 480, FPS: standardFPS},
	}
}

func GetCommonResolutions() []string {
	res := []string{"1920x1080", "1280x720", "640x480"}
	if screenshot.NumActiveDisplays() > 0 {
		w, h := physicalDisplaySize(0)
		if w > 0 && h > 0 {
			native := fmt.Sprintf("%dx%d", w, h)
			exists := false
			for _, r := range res {
				if r == native {
					exists = true
					break
				}
			}
			if !exists {
				res = append([]string{native}, res...)
			}
		}
	}
	return res
}

func GetDisplayModes(index int) []api.VideoCaptureMode {
	nativeW, nativeH := physicalDisplaySize(index)
	if nativeW <= 0 || nativeH <= 0 {
		return GetCommonModes()
	}

	var modes []api.VideoCaptureMode
	for _, r := range standardResolutions {
		if r.w <= nativeW && r.h <= nativeH {
			modes = append(modes, api.VideoCaptureMode{
				Width:       r.w,
				Height:      r.h,
				FPS:         standardFPS,
				PixelFormat: "BGRA",
			})
		}
	}

	// If the native resolution isn't a standard one, prepend it.
	if len(modes) == 0 || modes[0].Width != nativeW || modes[0].Height != nativeH {
		native := api.VideoCaptureMode{
			Width:       nativeW,
			Height:      nativeH,
			FPS:         standardFPS,
			PixelFormat: "BGRA",
		}
		modes = append([]api.VideoCaptureMode{native}, modes...)
	}

	return modes
}

func GetDisplayResString(index int) string {
	w, h := physicalDisplaySize(index)
	if w <= 0 || h <= 0 {
		return ""
	}
	return fmt.Sprintf(" (%dx%d)", w, h)
}
