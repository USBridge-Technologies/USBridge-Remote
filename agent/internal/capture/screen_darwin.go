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
)

type Service struct{}

func New() *Service { return &Service{} }

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
	out = append(out, cameraDevices()...)
	return out
}
