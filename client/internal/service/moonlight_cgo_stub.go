//go:build (!darwin && !ios && !linux && !windows && !android) || !cgo

package service

import (
	"fmt"
	"os"
)

// MoonlightCgoWrapper is a stub on unsupported platforms.
type MoonlightCgoWrapper struct {
	host       string
	audioMuted bool
}

func NewMoonlightCgoWrapper(host string) *MoonlightCgoWrapper {
	return &MoonlightCgoWrapper{host: host}
}

func (w *MoonlightCgoWrapper) StartStream(
	rtspSessionUrl string, rikey []byte,
	appVersion, gfeVersion string, serverCodecModeSupport int,
	videoFormat int,
	width, height, fps, bitrate int,
	pipeWrite *os.File, audioPipeWrite *os.File, onStop func(error),
) error {
	return fmt.Errorf("moonlight-common-c not built for this platform")
}

func (w *MoonlightCgoWrapper) StopStream()                                                {}
func (w *MoonlightCgoWrapper) SetAudioMuted(muted bool)                                   { w.audioMuted = muted }
func (w *MoonlightCgoWrapper) GetAudioMuted() bool                                        { return w.audioMuted }
func (w *MoonlightCgoWrapper) IsInputActive() bool                                        { return false }
func (w *MoonlightCgoWrapper) NegotiatedVideoCodecName() (string, bool)                   { return "", false }
func (w *MoonlightCgoWrapper) SendMoonlightKey(vkCode int16, action int8, modifiers int8) {}
func (w *MoonlightCgoWrapper) SendMoonlightMouseMove(dx, dy int16)                        {}
func (w *MoonlightCgoWrapper) SendMoonlightMousePosition(x, y, refW, refH int16)          {}
func (w *MoonlightCgoWrapper) SendMoonlightMouseButton(action int8, button int)           {}
func (w *MoonlightCgoWrapper) SendMoonlightScroll(clicks int8)                            {}
func (w *MoonlightCgoWrapper) SendMoonlightControllerEvent(
	cn uint16, am uint16, b uint16, lt uint8, rt uint8, lx int16, ly int16, rx int16, ry int16,
) {
}
func (w *MoonlightCgoWrapper) SendMoonlightUtf8Text(text string) {}
