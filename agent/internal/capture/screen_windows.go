//go:build windows

package capture

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"strings"
	"sync"
	"time"

	"github.com/kbinani/screenshot"

	"usbridge_agent/internal/api"
	"usbridge_agent/internal/displaypower"
	"usbridge_agent/internal/monitors"
	"usbridge_agent/internal/streamhost"
)

type Service struct {
	devices streamhost.CaptureDeviceLister

	// Streamed-monitor resolution is cached: StreamedBounds sits on the
	// absolute-mouse hot path (every pointer move) and resolving it parses
	// the backend's device list and enumerates monitors. The cache is keyed
	// by the selected output so picking another monitor in the client takes
	// effect on the very next event, and expires so a hot-plug is noticed.
	mu         sync.Mutex
	cacheKey   string
	cacheIdx   int
	cacheValid time.Time
}

func New(devices streamhost.CaptureDeviceLister) *Service { return &Service{devices: devices} }

// SetDevices re-points device correlation at a different backend -- needed
// after App.SetStreamBackend swaps streamhost.Backend, since New captured
// the old one by value and nothing else here re-reads it.
func (s *Service) SetDevices(devices streamhost.CaptureDeviceLister) { s.devices = devices }

// StreamedDisplayIndex is the screenshot display index of the monitor being
// streamed (see pickStreamedDisplay).
func (s *Service) StreamedDisplayIndex() int {
	n := screenshot.NumActiveDisplays()
	if n == 0 || s.devices == nil {
		return 0
	}
	output := ""
	if o, ok := s.devices.(interface{ OutputName() string }); ok {
		output = o.OutputName()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cacheKey == output && time.Now().Before(s.cacheValid) {
		return s.cacheIdx
	}
	idx := pickStreamedDisplay(monitors.GDINames(), s.devices.ListCaptureDevices(), output)
	if idx < 0 || idx >= n {
		idx = 0
	}
	s.cacheKey, s.cacheIdx, s.cacheValid = output, idx, time.Now().Add(streamedDisplayCacheTTL)
	return idx
}

// streamedDisplayCacheTTL bounds how stale the cached monitor index may get
// when the selected output itself did not change (monitor hot-plug, a
// re-enumeration after the streamer restarted).
const streamedDisplayCacheTTL = 2 * time.Second

// StreamedBounds is the streamed monitor's rectangle in virtual-desktop
// pixels -- the space SetCursorPos works in. ok=false when no display exists.
func (s *Service) StreamedBounds() (image.Rectangle, bool) {
	if screenshot.NumActiveDisplays() == 0 {
		return image.Rectangle{}, false
	}
	idx := s.StreamedDisplayIndex()
	// Physical pixels by GDI name: the screenshot library reports whatever
	// the process's DPI awareness yields (1280x720 for a 4K monitor at 300%),
	// while the cursor is positioned in physical pixels.
	if names := monitors.GDINames(); idx >= 0 && idx < len(names) {
		if list, err := monitors.List(); err == nil {
			if m, ok := monitors.Find(list, names[idx]); ok && m.Width > 1 && m.Height > 1 {
				return image.Rect(m.X, m.Y, m.X+m.Width, m.Y+m.Height), true
			}
		}
	}
	return screenshot.GetDisplayBounds(idx), true
}

func (s *Service) Snapshot() (*api.ScreenSnapshot, error) {
	if screenshot.NumActiveDisplays() == 0 {
		return nil, fmt.Errorf("no active displays")
	}
	bounds := screenshot.GetDisplayBounds(s.StreamedDisplayIndex())
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
		gdi := make([]string, 0, len(devices))
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
			gdi = append(gdi, d.GDIName)
		}
		return withMonitorPower(out, gdi)
	}

	// No device list from the stream host (Vibepollo/Sunshine before their
	// first start): the monitors as the user's desktop has them. The agent
	// is a service, and enumerating from session 0 sees one fake 1024x768
	// display instead of them.
	if listed := sessionMonitorDevices(); len(listed) > 0 {
		return listed
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
	// kbinani/screenshot indexes displays in EnumDisplayMonitors order.
	return withMonitorPower(out, monitors.GDINames())
}

// sessionMonitorDevices lists the physical monitors of the user's desktop by
// their device path ("monitor:<id>", which video_set_device resolves to the
// stream host's name for it), the active ones with their modes, the
// switched-off ones after them; nil when that list can't be had. The
// virtual monitor is listed by the API as its "virtual:" entry instead.
func sessionMonitorDevices() []api.VideoDeviceInfo {
	all, err := displaypower.ListInSession()
	if err != nil {
		return nil
	}
	var out []api.VideoDeviceInfo
	for _, m := range all {
		if m.Virtual || !m.Active || m.Width <= 0 || m.Height <= 0 {
			continue
		}
		on, primary := true, m.Primary
		out = append(out, api.VideoDeviceInfo{
			Path:           "monitor:" + m.ID,
			Name:           fmt.Sprintf("%s (%dx%d)", monitorName(m), m.Width, m.Height),
			MonitorPrimary: &primary,
			Bus:            "dxgi",
			Index:          len(out),
			Connected:      true,
			SupportedModes: ModesForResolution(m.Width, m.Height),
			MonitorID:      m.ID,
			Enabled:        &on,
		})
	}
	if len(out) == 0 {
		return nil
	}
	for _, m := range all {
		if m.Virtual || m.Active {
			continue
		}
		off := false
		out = append(out, api.VideoDeviceInfo{
			Path:      "monitor:" + m.ID,
			Name:      monitorName(m) + " (off)",
			Bus:       "dxgi",
			Index:     len(out),
			Connected: false,
			MonitorID: m.ID,
			Enabled:   &off,
		})
	}
	return out
}

// withMonitorPower adds the on/off switch (displaypower) to the listed monitors -- gdi[i]
// is out[i]'s GDI name, "" when unknown -- and lists the switched-off monitors after them,
// so the client can switch them back on.
func withMonitorPower(out []api.VideoDeviceInfo, gdi []string) []api.VideoDeviceInfo {
	all, err := displaypower.ListInSession()
	if err != nil {
		return out
	}
	listed := map[string]bool{}
	for i := range out {
		if i >= len(gdi) || gdi[i] == "" {
			continue
		}
		for _, m := range all {
			if m.Active && strings.EqualFold(m.GDIName, gdi[i]) {
				on, primary := true, m.Primary
				out[i].MonitorID, out[i].Enabled, out[i].MonitorPrimary = m.ID, &on, &primary
				listed[m.ID] = true
				// The monitor's own name, not the stream host's: that one is the
				// fallback's "Display N" or the DXGI adapter's, which on a VM
				// without 3D is "\\.\DISPLAY1 Microsoft Basic Render".
				if j := strings.LastIndex(out[i].Name, " ("); j >= 0 {
					out[i].Name = monitorName(m) + out[i].Name[j:]
				}
			}
		}
	}
	for _, m := range all {
		// The virtual monitor is the API's own "virtual:" entry, not a switched-off one.
		if m.Active || m.Virtual || listed[m.ID] {
			continue
		}
		off := false
		out = append(out, api.VideoDeviceInfo{
			Path:      "monitor:" + m.ID,
			Name:      monitorName(m) + " (off)",
			Bus:       "dxgi",
			Index:     len(out),
			Connected: false,
			MonitorID: m.ID,
			Enabled:   &off,
		})
	}
	return out
}
