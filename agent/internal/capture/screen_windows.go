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
	"usbridge_agent/internal/streamhost"
)

type Service struct {
	devices streamhost.CaptureDeviceLister
}

func New(devices streamhost.CaptureDeviceLister) *Service { return &Service{devices: devices} }

// SetDevices re-points device correlation at a different backend -- needed
// after App.SetStreamBackend swaps streamhost.Backend, since New captured
// the old one by value and nothing else here re-reads it.
func (s *Service) SetDevices(devices streamhost.CaptureDeviceLister) { s.devices = devices }

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

// Devices reports capturable monitors. Sunshine's Windows display_device
// backend identifies each monitor by a stable device_id (an EDID+instance
// derived string, e.g. "{26932b0f-...}") rather than a simple index — that
// id is what its output_name config key actually expects, and it can only be
// read back from Sunshine's own log (see sunshine.WindowsDisplayDevices),
// never predicted from Windows' own display enumeration order. Without this,
// picking "Display 1" here previously wrote a meaningless literal string
// into output_name, which Sunshine silently ignored, making monitor
// switching on Windows a no-op.
//
// Falls back to kbinani/screenshot-based enumeration ("display:<index>")
// only before Sunshine has ever logged its device list this session (e.g.
// prior to its first launch) — that fallback's index does NOT reliably line
// up with Sunshine's own selection, so switching via it may not work until
// Sunshine has started at least once.
func (s *Service) Devices() []api.VideoDeviceInfo {
	var devices []streamhost.CaptureDevice
	if s.devices != nil {
		devices = s.devices.ListCaptureDevices()
	}
	if len(devices) > 0 {
		out := make([]api.VideoDeviceInfo, 0, len(devices))
		for i, d := range devices {
			name := d.DisplayName
			out = append(out, api.VideoDeviceInfo{
				Path:           fmt.Sprintf("winid:%s", d.OutputName),
				Name:           fmt.Sprintf("%s (%dx%d)", name, d.Width, d.Height),
				Bus:            "dxgi",
				Index:          i,
				Connected:      true,
				SupportedModes: ModesForResolution(d.Width, d.Height),
			})
		}
		return out
	}

	out := make([]api.VideoDeviceInfo, 0, screenshot.NumActiveDisplays())
	for i := 0; i < screenshot.NumActiveDisplays(); i++ {
		bounds := screenshot.GetDisplayBounds(i)
		out = append(out, api.VideoDeviceInfo{
			Path:           fmt.Sprintf("display:%d", i),
			Name:           fmt.Sprintf("Display %d (%dx%d)", i, bounds.Dx(), bounds.Dy()),
			Bus:            "dxgi",
			Index:          i,
			Connected:      true,
			SupportedModes: GetDisplayModes(i),
		})
	}
	return out
}
