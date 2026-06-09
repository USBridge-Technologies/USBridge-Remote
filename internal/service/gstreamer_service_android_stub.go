//go:build android && !gstreamer

package service

import (
	"fmt"
	"image"
	"os"

	"usbridge-client/internal/models"
)

// GStreamerService is a no-op stub for Android builds without GStreamer.
// The app uses Moonlight (VideoProtocol=moonlight) instead.
// To enable GStreamer, add build tag: -tags=gstreamer
type GStreamerService struct {
	config           *models.AppConfig
	onStateChanged   func(string)
	onError          func(error)
	onFrameReceived  func(image.Image)
}

func NewGStreamerService(config *models.AppConfig) *GStreamerService {
	return &GStreamerService{config: config}
}

func (gs *GStreamerService) ConnectToRTP() error {
	return fmt.Errorf("GStreamer not available: build with -tags=gstreamer or use Moonlight protocol")
}
func (gs *GStreamerService) ConnectToUDPViaPipe(_ *os.File) error {
	return fmt.Errorf("GStreamer not available")
}
func (gs *GStreamerService) ConnectToUDP(_ int) error {
	return fmt.Errorf("GStreamer not available")
}
func (gs *GStreamerService) Disconnect() error  { return nil }
func (gs *GStreamerService) Reconnect() error   { return nil }

func (gs *GStreamerService) SetOnFrameReceived(cb func(image.Image)) { gs.onFrameReceived = cb }
func (gs *GStreamerService) SetOnStateChanged(cb func(string))       { gs.onStateChanged = cb }
func (gs *GStreamerService) SetOnError(cb func(error))               { gs.onError = cb }

func (gs *GStreamerService) IsConnected() bool                         { return false }
func (gs *GStreamerService) GetStats() map[string]interface{}          { return map[string]interface{}{} }
func (gs *GStreamerService) GetConfig() *models.AppConfig              { return gs.config }
func (gs *GStreamerService) GetBindHost() string                       { return "" }
func (gs *GStreamerService) UpdateHost(_ string)                       {}
func (gs *GStreamerService) UpdateVideoPort(_ int)                     {}
func (gs *GStreamerService) UpdateVideoUDPPort(_ int)                  {}
func (gs *GStreamerService) SetVideoMode(_ string)                     {}
func (gs *GStreamerService) SetExpectedVideoSize(_, _ int)             {}
func (gs *GStreamerService) SupportsNativeFullscreen() bool            { return false }
func (gs *GStreamerService) IsNativeFullscreenActive() bool            { return false }
func (gs *GStreamerService) StartNativeFullscreen() error              { return fmt.Errorf("not supported") }
func (gs *GStreamerService) StopNativeFullscreen() error               { return fmt.Errorf("not supported") }
func (gs *GStreamerService) ResetRuntimeDecoderFallback()              {}
func (gs *GStreamerService) SetAutoReconnect(_ bool)       {}
func (gs *GStreamerService) SetMaxReconnectAttempts(_ int) {}
