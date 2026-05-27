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

// Ensure GStreamerService implements VideoClient
var _ VideoClient = (*GStreamerService)(nil)
