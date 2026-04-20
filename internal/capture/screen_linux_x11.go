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
)

type Service struct{}

func New() *Service { return &Service{} }

func (s *Service) Snapshot() (*api.ScreenSnapshot, error) {
	env := GetLinuxEnv()
	if env == "Wayland" {
		// Wayland snapshot could be implemented here via dbus screenshot portal or grim
		// For now we fall back to screenshot lib which works under XWayland sometimes
	}

	if screenshot.NumActiveDisplays() == 0 {
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

func (s *Service) Devices() []api.VideoDeviceInfo {
	num := screenshot.NumActiveDisplays()
	out := make([]api.VideoDeviceInfo, 0, num)
	env := GetLinuxEnv()
	
	if env == "Wayland" {
		nodeID := GetPortalPipeWireNodeID()
		out = append(out, api.VideoDeviceInfo{
			Path:      fmt.Sprintf("pipewire:%d", nodeID),
			Name:      fmt.Sprintf("Wayland PipeWire Portal Node %d", nodeID),
			Bus:       "wayland",
			Index:     0,
			Connected: true,
		})
		return out // In wayland, we usually capture the whole selected stream from portal
	}

	for i := 0; i < num; i++ {
		bounds := screenshot.GetDisplayBounds(i)
		out = append(out, api.VideoDeviceInfo{
			Path:      fmt.Sprintf("display:%d", i),
			Name:      fmt.Sprintf("Display %d (%dx%d) [%s]", i, bounds.Dx(), bounds.Dy(), env),
			Bus:       "x11",
			Index:     i,
			Connected: true,
		})
	}
	return out
}
