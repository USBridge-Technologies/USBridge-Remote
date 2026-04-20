//go:build windows

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
	out := make([]api.VideoDeviceInfo, 0, screenshot.NumActiveDisplays())
	for i := 0; i < screenshot.NumActiveDisplays(); i++ {
		bounds := screenshot.GetDisplayBounds(i)
		out = append(out, api.VideoDeviceInfo{
			Path:      fmt.Sprintf("display:%d", i),
			Name:      fmt.Sprintf("Display %d (%dx%d)", i, bounds.Dx(), bounds.Dy()),
			Bus:       "dxgi",
			Index:     i,
			Connected: true,
		})
	}
	return out
}
