package capture

import (
	"fmt"
	"github.com/kbinani/screenshot"
	"usbridge_agent/internal/api"
)

func GetCommonModes() []api.VideoCaptureMode {
	return []api.VideoCaptureMode{
		{Width: 1920, Height: 1080, FPS: []int{30, 60}},
		{Width: 1280, Height: 720, FPS: []int{30, 60}},
		{Width: 640, Height: 480, FPS: []int{30, 60}},
	}
}

func GetCommonResolutions() []string {
	return []string{"1920x1080", "1280x720", "640x480"}
}

func GetDisplayModes(index int) []api.VideoCaptureMode {
	return GetCommonModes()
}

func GetDisplayResString(index int) string {
	if index < 0 || index >= screenshot.NumActiveDisplays() {
		return ""
	}
	bounds := screenshot.GetDisplayBounds(index)
	return fmt.Sprintf(" (%dx%d)", bounds.Dx(), bounds.Dy())
}
