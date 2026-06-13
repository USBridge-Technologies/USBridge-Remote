package controller

import "usbridge-client/internal/service"

type nativeFullscreenCapture interface {
	Start() error
	Stop() error
}

func newNativeFullscreenCapture(miProvider func() service.MoonlightInputSender, onExit func()) nativeFullscreenCapture {
	return newPlatformNativeFullscreenCapture(miProvider, onExit)
}
