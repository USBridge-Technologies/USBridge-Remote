package service

import (
	"image"
	"os"

	"usbridge-client/internal/models"
)

// VideoClient defines the common interface for video receiving and rendering services.
type VideoClient interface {
	ConnectToRTP() error
	ConnectToUDPViaPipe(pipeReader *os.File) error
	Disconnect() error
	Reconnect() error

	SetOnFrameReceived(callback func(image.Image))
	SetOnStateChanged(callback func(string))
	SetOnError(callback func(error))

	IsConnected() bool
	GetStats() map[string]interface{}
	GetConfig() *models.AppConfig

	GetBindHost() string
	UpdateHost(host string)
	UpdateVideoPort(port int)
	UpdateVideoUDPPort(port int)

	SetVideoMode(mode string)
	SetExpectedVideoSize(width, height int)

	SupportsNativeFullscreen() bool
	IsNativeFullscreenActive() bool
	StartNativeFullscreen() error
	StopNativeFullscreen() error

	ResetRuntimeDecoderFallback()
	SetAutoReconnect(enabled bool)
	SetMaxReconnectAttempts(max int)
}

// MoonlightInputSender is implemented by MoonlightService when a Moonlight stream is
// active. It routes input through LiSend* APIs instead of WebSocket HID.
type MoonlightInputSender interface {
	SendMoonlightKey(vkCode int16, action int8, modifiers int8)
	SendMoonlightMouseMove(dx, dy int16)
	SendMoonlightMouseButton(action int8, button int)
	SendMoonlightScroll(clicks int8)
}

// Moonlight protocol constants matching Limelight.h KEY_ACTION_* / BUTTON_ACTION_* / BUTTON_*.
const (
	LiKeyActionDown      = int8(0x03)
	LiKeyActionUp        = int8(0x04)
	LiMouseButtonPress   = int8(0x07)
	LiMouseButtonRelease = int8(0x08)
	LiMouseButtonLeft    = 1
	LiMouseButtonMiddle  = 2
	LiMouseButtonRight   = 3
)

// Ensure GStreamerService implements VideoClient
var _ VideoClient = (*GStreamerService)(nil)
