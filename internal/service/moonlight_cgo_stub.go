//go:build (!darwin && (!linux || android) && !windows) || !cgo

package service

import (
	"fmt"
	"os"
)

// MoonlightCgoWrapper is a stub on non-darwin platforms.
type MoonlightCgoWrapper struct {
	host       string
	audioMuted bool
}

func NewMoonlightCgoWrapper(host string) *MoonlightCgoWrapper {
	return &MoonlightCgoWrapper{host: host}
}

func (w *MoonlightCgoWrapper) StartStream(
	rtspSessionUrl string,
	rikey []byte,
	appVersion, gfeVersion string,
	serverCodecModeSupport int,
	width, height, fps, bitrate int,
	pipeWrite *os.File,
	audioPipeWrite *os.File,
	onStop func(error),
) error {
	return fmt.Errorf("moonlight-common-c not built for this platform")
}

func (w *MoonlightCgoWrapper) StopStream() {}

func (w *MoonlightCgoWrapper) SendMoonlightKey(vkCode int16, action int8, modifiers int8) {}
func (w *MoonlightCgoWrapper) SendMoonlightMouseMove(dx, dy int16)                        {}
func (w *MoonlightCgoWrapper) SendMoonlightMouseButton(action int8, button int)            {}
func (w *MoonlightCgoWrapper) SendMoonlightScroll(clicks int8)                            {}

func (w *MoonlightCgoWrapper) SetAudioMuted(muted bool) { w.audioMuted = muted }
func (w *MoonlightCgoWrapper) GetAudioMuted() bool      { return w.audioMuted }
