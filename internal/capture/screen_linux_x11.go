//go:build linux

package capture

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image/png"
	"time"

	"github.com/kbinani/screenshot"
	"github.com/sirupsen/logrus"
	"usbridge_agent/internal/api"
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

func (s *Service) Devices() []api.VideoDeviceInfo {
	env := GetLinuxEnv()
	resStr := GetDisplayResString(0)

	if env == "Wayland" {
		portal := GetPortal()
		nodeID := portal.NodeID()
		
		if nodeID == 0 {
			go func() {
				if err := portal.Init(); err != nil {
					logrus.Errorf("[capture] failed to auto-init portal: %v", err)
				}
			}()
			
			return []api.VideoDeviceInfo{
				{
					Path:      "pipewire:auto",
					Name:      "Wayland Screen" + resStr + " (Portal not yet active)",
					Bus:       "wayland",
					Index:     0,
					Connected: false,
				},
			}
		}

		return []api.VideoDeviceInfo{
			{
				Path:           fmt.Sprintf("pipewire:%d", nodeID),
				Name:           fmt.Sprintf("Wayland Shared Screen%s (Node %d)", resStr, nodeID),
				Bus:            "wayland",
				Index:          0,
				Connected:      true,
				SupportedModes: GetDisplayModes(0),
			},
		}
	}

	num := screenshot.NumActiveDisplays()
	out := make([]api.VideoDeviceInfo, 0, num)
	for i := 0; i < num; i++ {
		out = append(out, api.VideoDeviceInfo{
			Path:           fmt.Sprintf("display:%d", i),
			Name:           fmt.Sprintf("Display %d%s", i, GetDisplayResString(i)),
			Bus:            "x11",
			Index:          i,
			Connected:      true,
			SupportedModes: GetDisplayModes(i),
		})
	}
	return out
}
