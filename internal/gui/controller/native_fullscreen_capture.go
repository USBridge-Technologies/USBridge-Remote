package controller

import "usbridge-client/internal/api"

type nativeFullscreenCapture interface {
	Start() error
	Stop() error
}

func newNativeFullscreenCapture(usbClient *api.USBClient, onExit func()) nativeFullscreenCapture {
	return newPlatformNativeFullscreenCapture(usbClient, onExit)
}
