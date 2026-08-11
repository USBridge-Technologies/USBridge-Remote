//go:build js && wasm

package gui

import (
	"usbridge-client/internal/models"
	"usbridge-client/internal/service"
)

// newPlatformVideoClient constructs the WebRTC-backed video client for the
// browser build — see video_client_factory_default.go for the real
// Moonlight/GameStream counterpart every other platform uses.
func newPlatformVideoClient(cfg *models.AppConfig) service.VideoClient {
	return service.NewWebRTCVideoClient(cfg)
}
