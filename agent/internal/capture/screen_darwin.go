//go:build darwin

package capture

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"github.com/kbinani/screenshot"

	"usbridge_agent/internal/api"
	"usbridge_agent/internal/displaypower"
	"usbridge_agent/internal/monitors"
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
	// The monitors as displaypower sees them (by CGDirectDisplayID): which one
	// is on and which is the primary one, for the client's On/Off and Main
	// buttons, and the ones the agent switched off.
	byID := map[string]displaypower.Monitor{}
	all, _ := displaypower.List()
	for _, m := range all {
		byID[m.ID] = m
	}
	ids := monitors.ActiveDisplayIDs()
	out := make([]api.VideoDeviceInfo, 0, screenshot.NumActiveDisplays())
	for i := 0; i < screenshot.NumActiveDisplays(); i++ {
		w, h := physicalDisplaySize(i)
		info := api.VideoDeviceInfo{
			Path:           fmt.Sprintf("display:%d", i),
			Name:           fmt.Sprintf("Display %d (%dx%d)", i, w, h),
			Bus:            "screen",
			Index:          i,
			Connected:      true,
			SupportedModes: GetDisplayModes(i),
		}
		if i < len(ids) {
			if m, ok := byID[strconv.FormatUint(uint64(ids[i]), 10)]; ok {
				if m.Virtual {
					// The virtual monitor, up for a session: the API's "virtual:"
					// entry, never a monitor of its own.
					continue
				}
				on, primary := true, m.Primary
				info.Name = fmt.Sprintf("%s (%dx%d)", monitorName(m), w, h)
				info.MonitorID, info.Enabled, info.MonitorPrimary = m.ID, &on, &primary
			}
		}
		out = append(out, info)
	}
	for _, m := range all {
		if m.Active || m.Virtual {
			continue
		}
		off := false
		out = append(out, api.VideoDeviceInfo{
			Path:           "monitor:" + m.ID,
			Name:           monitorName(m) + " (off)",
			Bus:            "screen",
			Index:          len(out),
			MonitorID:      m.ID,
			Enabled:        &off,
			MonitorPrimary: &off,
		})
	}
	return out
}
