//go:build linux

package capture

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image/png"
	"time"

	"github.com/kbinani/screenshot"
	"usbridge_agent/internal/api"
	"usbridge_agent/internal/display"
)

type Service struct{}

func New() *Service { return &Service{} }

func (s *Service) Snapshot() (*api.ScreenSnapshot, error) {
	env := GetLinuxEnv()
	if env == "Wayland" {
		// Wayland high-quality snapshot placeholder
	}

	if screenshot.NumActiveDisplays() == 0 {
		if env == "Wayland" {
			return nil, fmt.Errorf("Wayland capture requires portal initialization")
		}
		return nil, fmt.Errorf("no active displays")
	}

	bounds := screenshot.GetDisplayBounds(0)
	img, err := screenshot.CaptureRect(bounds)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return &api.ScreenSnapshot{
		Format:      "png-base64",
		Width:       bounds.Dx(),
		Height:      bounds.Dy(),
		ImageBase64: base64.StdEncoding.EncodeToString(buf.Bytes()),
		Timestamp:   time.Now().Format(time.RFC3339Nano),
	}, nil
}

// Devices reports real display metadata (native resolution, supported FPS)
// for descriptive purposes only — it never touches the XDG desktop portal.
// Sunshine does the actual capturing (and requests its own portal session
// if/when it needs one); triggering a second, independent portal session
// here just to describe available displays caused a confusing extra
// permission prompt after Sunshine had already connected successfully.
// screenshot.NumActiveDisplays/GetDisplayBounds work under Wayland too via
// XWayland, so this needs no Wayland-specific branch.
func (s *Service) Devices() []api.VideoDeviceInfo {
	num := screenshot.NumActiveDisplays()
	out := make([]api.VideoDeviceInfo, 0, num)
	for i := 0; i < num; i++ {
		out = append(out, api.VideoDeviceInfo{
			Path:           fmt.Sprintf("display:%d", i),
			Name:           fmt.Sprintf("Display %d%s", i, GetDisplayResString(i)),
			Bus:            "x11",
			Index:          i,
			Connected:      true,
			SupportedModes: linuxDisplayModes(i),
		})
	}
	return out
}

// linuxDisplayModes reports the modes a connected output actually supports,
// read from /sys/class/drm (see display.ConnectorResolutions) rather than
// guessed from a fixed shortlist. Falls back to the generic
// native-size-filtered list only if DRM can't be read (no permissions,
// non-DRM setup) or the display index has no matching connector.
func linuxDisplayModes(index int) []api.VideoCaptureMode {
	connectors := display.ConnectorResolutions()
	if index < 0 || index >= len(connectors) || len(connectors[index]) == 0 {
		return GetDisplayModes(index)
	}
	modes := make([]api.VideoCaptureMode, 0, len(connectors[index]))
	for _, r := range connectors[index] {
		modes = append(modes, api.VideoCaptureMode{
			Width:       r.Width,
			Height:      r.Height,
			FPS:         standardFPS,
			PixelFormat: "BGRA",
		})
	}
	return modes
}
