//go:build linux

package capture

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image/png"
	"strconv"
	"time"

	"github.com/kbinani/screenshot"
	"usbridge_agent/internal/api"
	"usbridge_agent/internal/display"
	"usbridge_agent/internal/streamhost"
)

type Service struct {
	devices streamhost.CaptureDeviceLister
}

func New(devices streamhost.CaptureDeviceLister) *Service { return &Service{devices: devices} }

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
		if s.devices != nil {
			for _, d := range s.devices.ListCaptureDevices() {
				if d.Key == "" {
					continue
				}
				if idx, err := strconv.Atoi(d.OutputName); err == nil {
					byName[d.Key] = idx
				}
			}
		}
		out := make([]api.VideoDeviceInfo, 0, len(connectors))
		for _, c := range connectors {
			idx := c.Index
			if realIdx, ok := byName[c.Name]; ok {
				idx = realIdx
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
			out = append(out, api.VideoDeviceInfo{
				Path:           fmt.Sprintf("drm:%d", idx),
				Name:           c.Name,
				Bus:            "drm",
				Index:          idx,
				Connected:      true,
				SupportedModes: modes,
			})
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
