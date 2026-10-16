package service

import (
	"sync/atomic"

	"usbridge-client/internal/api/moonlight"

	"github.com/sirupsen/logrus"
)

// The virtual display of a plain Apollo/Vibepollo host, reached without a
// usbridge agent (an empty master key). There the agent's virtual-display
// API (MttVDD, /api/video/virtual) does not exist; the host makes its own
// per-client monitor (SudoVDA) on /launch instead, in the requested mode,
// and Apollo's launch parameter virtualDisplay=1|0 forces it on or off for
// the session over the host's own virtual_display_mode setting -- the same
// switch Artemis offers. With the agent the parameter is never sent: the
// agent turns Vibepollo's own display off and attaches MttVDD itself.

// hostVirtualDisplayOff is the user's "Virtual display" choice in the video
// dialog, inverted so the zero value means on: a capable host streams its
// own virtual monitor by default (any refresh rate, the client's size, no
// physical screen needed).
var hostVirtualDisplayOff atomic.Bool

// SetHostVirtualDisplayEnabled records the choice for the next launch.
func SetHostVirtualDisplayEnabled(enabled bool) { hostVirtualDisplayOff.Store(!enabled) }

// HostVirtualDisplayEnabled reports the recorded choice.
func HostVirtualDisplayEnabled() bool { return !hostVirtualDisplayOff.Load() }

// SetGameStreamOnly tells the service it streams from a host with no
// usbridge agent behind it, which turns on the stock-host launch parameters.
func (m *MoonlightService) SetGameStreamOnly(enabled bool) {
	m.gameStreamOnly.Store(enabled)
	if !enabled {
		m.hostVirtualDisplay.Store(false)
		m.hostVirtualDisplayOK.Store(false)
	}
}

// HostVirtualDisplayCapable reports whether the connected plain GameStream
// host can stream its own virtual display right now (Apollo/Vibepollo with
// its driver up), as its last /serverinfo said. Always false with a
// usbridge agent. A host whose driver is missing (VirtualDisplayDriverReady
// false) is left to capture its physical screen: asking would only fail.
func (m *MoonlightService) HostVirtualDisplayCapable() bool {
	return m.gameStreamOnly.Load() && m.hostVirtualDisplay.Load() && m.hostVirtualDisplayOK.Load()
}

func (m *MoonlightService) noteHostVirtualDisplay(info *moonlight.ServerInfo) {
	if info == nil {
		return
	}
	m.hostVirtualDisplay.Store(info.VirtualDisplayCapable)
	m.hostVirtualDisplayOK.Store(info.VirtualDisplayDriverReady)
}

// stockHostLaunchParams are the extra /launch parameters for a plain
// GameStream host; nil with a usbridge agent.
func (m *MoonlightService) stockHostLaunchParams() map[string]string {
	if !m.gameStreamOnly.Load() {
		return nil
	}
	params := map[string]string{}
	// Standard Moonlight parameter (Sunshine and its forks read it): the
	// host turns HDR on for the captured display -- on a virtual one that
	// is what makes it an HDR monitor at all.
	if m.hdr {
		params["hdrMode"] = "1"
	}
	if m.HostVirtualDisplayCapable() {
		want := HostVirtualDisplayEnabled()
		params["virtualDisplay"] = boolParam(want)
		logrus.Infof("🖥️ [Moonlight] host virtual display requested: %v", want)
	} else if m.hostVirtualDisplay.Load() {
		logrus.Info("🖥️ [Moonlight] host's virtual display driver is not ready; it streams its own screen")
	}
	return params
}

func boolParam(v bool) string {
	if v {
		return "1"
	}
	return "0"
}
