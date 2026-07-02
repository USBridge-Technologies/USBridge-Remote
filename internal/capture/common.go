package capture

import (
	"fmt"
	"github.com/kbinani/screenshot"
	"usbridge_agent/internal/api"
)

func GetCommonModes() []api.VideoCaptureMode {
	return []api.VideoCaptureMode{
		{Width: 1920, Height: 1080, FPS: []int{30, 60, 120}},
		{Width: 1280, Height: 720, FPS: []int{30, 60, 120, 144, 165, 240}},
		{Width: 640, Height: 480, FPS: []int{30, 60, 120, 144, 165, 240}},
	}
}

func GetCommonResolutions() []string {
	res := []string{"1920x1080", "1280x720", "640x480"}
	if screenshot.NumActiveDisplays() > 0 {
		bounds := screenshot.GetDisplayBounds(0)
		native := fmt.Sprintf("%dx%d", bounds.Dx(), bounds.Dy())
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
	return res
}

func GetDisplayModes(index int) []api.VideoCaptureMode {
	modes := GetCommonModes()
	if index >= 0 && index < screenshot.NumActiveDisplays() {
		bounds := screenshot.GetDisplayBounds(index)
		nativeMode := api.VideoCaptureMode{
			Width:       bounds.Dx(),
			Height:      bounds.Dy(),
			FPS:         []int{30, 60, 120, 144, 165, 240},
			PixelFormat: "BGRA",
		}
		// Check if native resolution is already in common modes
		exists := false
		for _, m := range modes {
			if m.Width == nativeMode.Width && m.Height == nativeMode.Height {
				exists = true
				break
			}
		}
		if !exists {
			// Prepend native mode as the best option
			modes = append([]api.VideoCaptureMode{nativeMode}, modes...)
		}
	}
	return modes
}

func GetDisplayResString(index int) string {
	if index < 0 || index >= screenshot.NumActiveDisplays() {
		return ""
	}
	bounds := screenshot.GetDisplayBounds(index)
	return fmt.Sprintf(" (%dx%d)", bounds.Dx(), bounds.Dy())
}
