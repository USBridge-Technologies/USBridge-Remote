//go:build !(js && wasm)

package gui

import (
	"usbridge-client/internal/models"
	"usbridge-client/internal/service"
)

// newPlatformVideoClient constructs the real Moonlight/GameStream client on
// every platform except the browser build — see
// video_client_factory_wasm.go for the WebRTC counterpart.
func newPlatformVideoClient(cfg *models.AppConfig) service.VideoClient {
	return service.NewMoonlightService(cfg)
}
