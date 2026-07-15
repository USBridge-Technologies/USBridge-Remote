//go:build !darwin || ios

package controller

import (
	"fmt"
	"usbridge-client/internal/service"
)

func newPlatformNativeFullscreenCapture(_ func() service.MoonlightInputSender, _ func()) nativeFullscreenCapture {
	return &noopNativeFullscreenCapture{}
}

type noopNativeFullscreenCapture struct{}

func (n *noopNativeFullscreenCapture) Start() error {
	return fmt.Errorf("native fullscreen capture is not implemented on this platform")
}

func (n *noopNativeFullscreenCapture) Stop() error {
	return nil
}
