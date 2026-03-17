// +build android

package service

import (
	"fmt"
	"image"

	"usbridge-client/internal/models"
)

// ICEConnectionState заглушка для Android
type ICEConnectionState int

// Константы для совместимости с webrtc.ICEConnectionState
const (
	ICEConnectionStateNew ICEConnectionState = iota
	ICEConnectionStateChecking
	ICEConnectionStateConnected
	ICEConnectionStateCompleted
	ICEConnectionStateFailed
	ICEConnectionStateDisconnected
	ICEConnectionStateClosed
)

// Экспортируем константы с префиксом для обратной совместимости
const (
	// Дублируем константы для использования в UI
	_ = ICEConnectionStateNew
	_ = ICEConnectionStateChecking
	_ = ICEConnectionStateConnected
	_ = ICEConnectionStateCompleted
	_ = ICEConnectionStateFailed
	_ = ICEConnectionStateDisconnected
	_ = ICEConnectionStateClosed
)

// WebRTCService заглушка для Android (WebRTC пока не поддерживается)
type WebRTCService struct {
	config *models.AppConfig
}

// NewWebRTCService создает заглушку WebRTC сервиса для Android
func NewWebRTCService(config *models.AppConfig) *WebRTCService {
	return &WebRTCService{
		config: config,
	}
}

// ConnectToMediaMTX заглушка
func (s *WebRTCService) ConnectToMediaMTX() error {
	return fmt.Errorf("WebRTC пока не поддерживается на Android")
}

// Disconnect заглушка
func (s *WebRTCService) Disconnect() error {
	return nil
}

// SetAutoReconnect заглушка
func (s *WebRTCService) SetAutoReconnect(enabled bool) {
}

// SetMaxReconnectAttempts заглушка
func (s *WebRTCService) SetMaxReconnectAttempts(max int) {
}

// SetOnFrameReceived заглушка
func (s *WebRTCService) SetOnFrameReceived(callback func(image.Image)) {
}

// SetOnStateChanged заглушка
func (s *WebRTCService) SetOnStateChanged(callback func(ICEConnectionState)) {
}

// SetOnError заглушка
func (s *WebRTCService) SetOnError(callback func(error)) {
}

// IsConnected заглушка
func (s *WebRTCService) IsConnected() bool {
	return false
}

// GetConnectionState заглушка
func (s *WebRTCService) GetConnectionState() ICEConnectionState {
	return ICEConnectionStateClosed
}

// GetStats заглушка
func (s *WebRTCService) GetStats() map[string]interface{} {
	return map[string]interface{}{
		"status": "not_supported_on_android",
	}
}

// UpdateHost заглушка
func (s *WebRTCService) UpdateHost(host string) {
	s.config.MediaMTXHost = host
}

// GetConfig заглушка
func (s *WebRTCService) GetConfig() *models.AppConfig {
	return s.config
}

// GetH264Decoder заглушка
func (s *WebRTCService) GetH264Decoder() *H264Decoder {
	decoder, _ := NewH264Decoder()
	return decoder
}

// Close заглушка
func (s *WebRTCService) Close() error {
	return nil
}
