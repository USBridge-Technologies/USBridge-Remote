//go:build (!darwin && !ios && !linux && !windows && !android) || !cgo

package service

import (
	"fmt"
	"os"

	"usbridge-client/internal/platform"
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
func (w *MoonlightCgoWrapper) SendMoonlightControllerArrival(
	cn uint16, am uint16, ctype uint8, supportedButtonFlags uint32, capabilities uint16,
) {
}
func (w *MoonlightCgoWrapper) SendMoonlightUtf8Text(text string) {}
func (w *MoonlightCgoWrapper) SendMoonlightPenEvent(
	eventType, toolType, penButtons uint8,
	x, y, pressureOrDistance float32,
	rotation uint16, tilt uint8,
) {
}

func (w *MoonlightCgoWrapper) UplinkSupport() (midi, mic bool)                    { return false, false }
func (w *MoonlightCgoWrapper) SendMoonlightMIDI(data []byte) bool                 { return false }
func (w *MoonlightCgoWrapper) SendMoonlightMic(sequence uint16, opus []byte) bool { return false }
func (w *MoonlightCgoWrapper) CameraUplinkSupported() bool                        { return false }
func (w *MoonlightCgoWrapper) SendMoonlightCamera(frame uint16, keyframe bool, au []byte) bool {
	return false
}

func (w *MoonlightCgoWrapper) RawHIDEpoch() uint64 { return 0 }

func (w *MoonlightCgoWrapper) SendMoonlightRawHID(kind, slot, endpoint uint8, total, offset uint16, data []byte, reliable bool) bool {
	return false
}

// MicUplinkSupported: no microphone uplink without the native stream.
func MicUplinkSupported() bool { return false }

// StartMicUplink: see MicUplinkSupported.
func StartMicUplink(send func(seq uint16, opus []byte)) (platform.UplinkCapture, error) {
	return nil, fmt.Errorf("microphone uplink is not supported on this platform")
}
