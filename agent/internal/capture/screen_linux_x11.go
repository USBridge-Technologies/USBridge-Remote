//go:build linux

package capture

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/kbinani/screenshot"
	"usbridge_agent/internal/api"
	"usbridge_agent/internal/display"
	"usbridge_agent/internal/displaypower"
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

// Snapshot never attempts a real capture on Linux -- deliberately. Every
// caller that matters is answered by the Client from its own decoded video
// frame instead, whenever a session is streaming: MCP's screen.get_image
// and ui.parse (client/internal/api/local_ui_intercept.go's
// tryLocalScreenImage/tryLocalUIParse) never reach the Agent at all in that
// case, and mouse.action's click_at/double_click_at before/after diff is
// recomputed Client-side too (client/internal/api/mouse_click_diff.go).
// kbinani/screenshot (still imported below, for Devices()'s unrelated
// display-count fallback only) has no reliable answer on Linux: on Wayland
// it either hangs on an interactive portal dialog nobody's there to answer,
// or returns a blank frame where a compositor blocks plain X11 reads for
// privacy -- not worth attempting for a path that, with a Client actually
// connected and streaming, is never reached anyway. A real Agent-side
// capture belongs in the streaming backend that already captures frames
// for encoding, not here -- see rust-shine/LINUX_SCREENSHOT_EXPORT_TODO.md.
func (s *Service) Snapshot() (*api.ScreenSnapshot, error) {
	return nil, fmt.Errorf("screen capture is not available directly from the Agent on Linux -- connect a Client and open its video view, which answers screen.get_image/ui.parse from the stream instead")
}

// Devices reports real display metadata (native resolution, supported FPS)
// for descriptive purposes only — it never touches the XDG desktop portal.
// Sunshine does the actual capturing (and requests its own portal session
// if/when it needs one); triggering a second, independent portal session
// here just to describe available displays caused a confusing extra
// permission prompt after Sunshine had already connected successfully.
//
// Enumeration is DRM/KMS-based (via /sys/class/drm) rather than X11-based:
// it works with no graphical session at all (headless/systemd autostart,
// before login), and its connected-output order/index is what Sunshine's
// own KMS capture backend uses for its "output_name" monitor pin (see
// display.Connectors), so the paths reported here ("drm:<index>") are
// exactly the values SetSunshineOutputName expects. Falls back to the old
// X11-based enumeration only if no DRM connector could be read at all.
func (s *Service) Devices() []api.VideoDeviceInfo {
	if connectors := display.Connectors(); len(connectors) > 0 {
		// display.Connectors() indexes connectors alphabetically by DRM
		// connector name (its own doc comment flags this as a guess). That
		// only happens to match Sunshine's real output_name index — an
		// artifact of its own DRM plane-enumeration order, which depends on
		// driver/hardware and isn't alphabetical in general — by luck. When
		// Sunshine's log has already told us the real mapping (see
		// streamhost.CaptureDeviceLister), prefer it so "drm:N" always means
		// what Sunshine itself thinks index N means; otherwise keep the
		// alphabetical guess as the only information available (e.g. before
		// Sunshine's first successful Wayland connection this session).
		byName := make(map[string]int)
		// rawOutputName holds a backend's OutputName verbatim for connectors
		// whose value isn't Sunshine's plain numeric index (e.g. rustshine's
		// "cardPath|connector" — see rustshineBackend.OutputName). For those,
		// "drm:<alphabetical-guess-index>" would round-trip through
		// videoSetDevice's numeric-index handling and silently corrupt the
		// value (writing the bare index string as a literal connector name).
		// Reporting the real OutputName as "raw:<value>" instead makes
		// videoSetDevice pass it through unchanged — see its prefix list.
		rawOutputName := make(map[string]string)
		if s.devices != nil {
			for _, d := range s.devices.ListCaptureDevices() {
				if d.Key == "" {
					continue
				}
				if idx, err := strconv.Atoi(d.OutputName); err == nil {
					byName[d.Key] = idx
				} else if d.OutputName != "" {
					rawOutputName[d.Key] = d.OutputName
				}
			}
		}
		// The compositor's view (KDE): which monitor is on and which is the
		// primary one, for the client's On/Off and Main buttons. Keyed by the
		// connector name without its card ("HDMI-A-1").
		desktop := map[string]displaypower.Monitor{}
		if monitors, err := displaypower.List(); err == nil {
			for _, m := range monitors {
				desktop[m.ID] = m
			}
		}
		out := make([]api.VideoDeviceInfo, 0, len(connectors))
		for _, c := range connectors {
			if isVkmsConnectorName(c.Name) {
				// The virtual monitor: the agent's, listed as its "virtual:" entry
				// and up for sessions only (displaypower) -- never a monitor of its own.
				continue
			}
			idx := c.Index
			if realIdx, ok := byName[c.Name]; ok {
				idx = realIdx
			}
			path := fmt.Sprintf("drm:%d", idx)
			if raw, ok := rawOutputName[c.Name]; ok {
				path = "raw:" + raw
			}
			modes := make([]api.VideoCaptureMode, 0, len(c.Modes))
			for _, r := range c.Modes {
				modes = append(modes, api.VideoCaptureMode{
					Width:       r.Width,
					Height:      r.Height,
					FPS:         standardFPS,
					PixelFormat: "BGRA",
				})
			}
			info := api.VideoDeviceInfo{
				Path:           path,
				Name:           c.Name,
				Bus:            "drm",
				Index:          idx,
				Connected:      true,
				SupportedModes: modes,
			}
			if _, conn, ok := strings.Cut(c.Name, "-"); ok {
				if m, ok := desktop[conn]; ok {
					on, primary := m.Active, m.Active && m.Primary
					info.MonitorID, info.Enabled, info.MonitorPrimary = m.ID, &on, &primary
				}
			}
			out = append(out, info)
		}
		return out
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

// isVkmsConnectorName reports whether a DRM connector ("card0-Virtual-1") is
// vkms's.
func isVkmsConnectorName(name string) bool {
	_, conn, ok := strings.Cut(name, "-")
	return ok && strings.HasPrefix(conn, "Virtual-")
}
