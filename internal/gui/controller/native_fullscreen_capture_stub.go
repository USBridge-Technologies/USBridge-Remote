//go:build !darwin

package controller

import (
	"fmt"
	"usbridge-client/internal/api"
)

func newPlatformNativeFullscreenCapture(_ *api.USBClient, _ func()) nativeFullscreenCapture {
	return &noopNativeFullscreenCapture{}
}

type noopNativeFullscreenCapture struct{}

func (n *noopNativeFullscreenCapture) Start() error {
	return fmt.Errorf("native fullscreen capture is not implemented on this platform")
}

func (n *noopNativeFullscreenCapture) Stop() error {
	return nil
}
