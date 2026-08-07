//go:build darwin

package capture

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/kbinani/screenshot"

	"usbridge_agent/internal/api"
	"usbridge_agent/internal/streamhost"
)

// Service has no macOS Sunshine-log-derived device correlation (see
// streamhost's darwin ListCaptureDevices, which always returns nil); devices
// is accepted anyway so the constructor signature matches the other
// platforms' build-tagged files, which all define the same exported Service/New.
type Service struct {
	devices streamhost.CaptureDeviceLister //nolint:unused
}

func New(devices streamhost.CaptureDeviceLister) *Service { return &Service{devices: devices} }

// SetDevices re-points device correlation at a different backend -- needed
// after App.SetStreamBackend swaps streamhost.Backend, since New captured
// the old one by value and nothing else here re-reads it.
func (s *Service) SetDevices(devices streamhost.CaptureDeviceLister) { s.devices = devices } //nolint:unused

func (s *Service) Snapshot() (*api.ScreenSnapshot, error) {
	if screenshot.NumActiveDisplays() == 0 {
		return nil, fmt.Errorf("no active displays")
	}

	tmpDir, err := os.MkdirTemp("", "usbridge-screen-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpDir)

	pngPath := filepath.Join(tmpDir, "screen.png")
	cmd := exec.Command("screencapture", "-x", "-D", "1", "-t", "png", pngPath)
	if output, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("screencapture failed: %v (%s)", err, string(output))
	}

	data, err := os.ReadFile(pngPath)
	if err != nil {
		return nil, err
	}

	bounds := screenshot.GetDisplayBounds(0)
	return &api.ScreenSnapshot{
		Format:      "png-base64",
		Width:       bounds.Dx(),
		Height:      bounds.Dy(),
		ImageBase64: base64.StdEncoding.EncodeToString(data),
		Timestamp:   time.Now().Format(time.RFC3339Nano),
	}, nil
}

func (s *Service) Devices() []api.VideoDeviceInfo {
	out := make([]api.VideoDeviceInfo, 0, screenshot.NumActiveDisplays())
	for i := 0; i < screenshot.NumActiveDisplays(); i++ {
		w, h := physicalDisplaySize(i)
		out = append(out, api.VideoDeviceInfo{
			Path:           fmt.Sprintf("display:%d", i),
			Name:           fmt.Sprintf("Display %d (%dx%d)", i, w, h),
			Bus:            "screen",
			Index:          i,
			Connected:      true,
			SupportedModes: GetDisplayModes(i),
		})
	}
	return out
}
