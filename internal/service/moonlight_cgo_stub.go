//go:build !darwin

package service

import (
	"fmt"
	"os"
)

// MoonlightCgoWrapper is a stub on non-darwin platforms.
type MoonlightCgoWrapper struct {
	host string
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
	onStop func(error),
) error {
	return fmt.Errorf("moonlight-common-c not built for this platform")
}

func (w *MoonlightCgoWrapper) StopStream() {}
