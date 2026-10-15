package capture

import "usbridge_agent/internal/streamhost"

// pickStreamedDisplay returns the kbinani/screenshot display index (==
// position in gdi, EnumDisplayMonitors order) of the monitor the streamer is
// actually capturing: the device whose OutputName is the backend's persisted
// output, else the primary monitor, else 0.
//
// Both the Agent's own screenshot and the absolute-mouse mapping used to be
// pinned to display 0. On a multi-monitor Windows host the streamed monitor is
// whatever output_name selects, so the cursor was scaled over a different
// monitor than the one the viewer sees -- clicks landed offset/scaled, and
// the right edge of the viewer mapped to the middle of the real screen.
func pickStreamedDisplay(gdi []string, devs []streamhost.CaptureDevice, output string) int {
	indexOf := func(name string) int {
		if name == "" {
			return -1
		}
		for i, g := range gdi {
			if g == name {
				return i
			}
		}
		return -1
	}
	if output != "" {
		for _, d := range devs {
			if d.OutputName == output {
				if i := indexOf(d.GDIName); i >= 0 {
					return i
				}
			}
		}
	}
	for _, d := range devs {
		if d.Primary {
			if i := indexOf(d.GDIName); i >= 0 {
				return i
			}
		}
	}
	return 0
}
