// +build !android

package service

import (
	"fmt"
	"sync"
	"time"
	"usbridge-client/internal/models"

	"github.com/pion/webrtc/v3"
	"github.com/sirupsen/logrus"
)

// WebRTCConfig конфигурация WebRTC соединения
type WebRTCConfig struct {
	// ICE серверы
	STUNServers []string
	TURNServers []string

	// Настройки кодеков
	PreferredVideoCodec string // H.264, VP8, VP9
	PreferredAudioCodec string // Opus, G.711

	// Настройки качества
	VideoWidth   int
	VideoHeight  int
	VideoFPS     int
	VideoBitrate int

	// Таймауты
	ConnectionTimeout time.Duration
	KeepAliveInterval time.Duration
}

// DefaultWebRTCConfig возвращает конфигурацию WebRTC по умолчанию
func DefaultWebRTCConfig() *WebRTCConfig {
	return &WebRTCConfig{
		STUNServers:         []string{"stun:stun.l.google.com:19302"},
		TURNServers:         []string{},
		PreferredVideoCodec: "H.264",
		PreferredAudioCodec: "Opus",
		VideoWidth:          640,
		VideoHeight:         480,
		VideoFPS:            30,
		VideoBitrate:        2000,
		ConnectionTimeout:   30 * time.Second,
		KeepAliveInterval:   5 * time.Second,
	}
}

// WebRTCConnectionManager менеджер WebRTC соединений
type WebRTCConnectionManager struct {
	config      *WebRTCConfig
	connections map[string]*WebRTCService
	mutex       sync.RWMutex

	// Callbacks
	onConnectionEstablished func(string)
	onConnectionLost        func(string)
	onError                 func(string, error)
}

// NewWebRTCConnectionManager создает новый менеджер соединений
func NewWebRTCConnectionManager(config *WebRTCConfig) *WebRTCConnectionManager {
	return &WebRTCConnectionManager{
		config:      config,
		connections: make(map[string]*WebRTCService),
	}
}

// CreateConnection создает новое WebRTC соединение
func (cm *WebRTCConnectionManager) CreateConnection(id string, appConfig *models.AppConfig) (*WebRTCService, error) {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	// Проверяем, не существует ли уже соединение с таким ID
	if _, exists := cm.connections[id]; exists {
		return nil, fmt.Errorf("соединение с ID %s уже существует", id)
	}

	// Создаем новое соединение
	service := NewWebRTCService(appConfig)

	// Настраиваем callbacks
	service.SetOnStateChanged(func(state webrtc.ICEConnectionState) {
		switch state {
		case webrtc.ICEConnectionStateConnected:
			if cm.onConnectionEstablished != nil {
				cm.onConnectionEstablished(id)
			}
		case webrtc.ICEConnectionStateDisconnected, webrtc.ICEConnectionStateFailed:
			if cm.onConnectionLost != nil {
				cm.onConnectionLost(id)
			}
		}
	})

	service.SetOnError(func(err error) {
		if cm.onError != nil {
			cm.onError(id, err)
		}
	})

	cm.connections[id] = service
	logrus.Infof("Создано WebRTC соединение: %s", id)

	return service, nil
}

// GetConnection возвращает соединение по ID
func (cm *WebRTCConnectionManager) GetConnection(id string) (*WebRTCService, bool) {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	service, exists := cm.connections[id]
	return service, exists
}

// RemoveConnection удаляет соединение
func (cm *WebRTCConnectionManager) RemoveConnection(id string) error {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	service, exists := cm.connections[id]
	if !exists {
		return fmt.Errorf("соединение с ID %s не найдено", id)
	}

	// Отключаем соединение
	if err := service.Disconnect(); err != nil {
		logrus.Errorf("Ошибка отключения соединения %s: %v", id, err)
	}

	delete(cm.connections, id)
	logrus.Infof("Удалено WebRTC соединение: %s", id)

	return nil
}

// GetAllConnections возвращает все активные соединения
func (cm *WebRTCConnectionManager) GetAllConnections() map[string]*WebRTCService {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	// Создаем копию карты
	connections := make(map[string]*WebRTCService)
	for id, service := range cm.connections {
		connections[id] = service
	}

	return connections
}

// GetConnectionStats возвращает статистику всех соединений
func (cm *WebRTCConnectionManager) GetConnectionStats() map[string]interface{} {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	stats := make(map[string]interface{})

	for id, service := range cm.connections {
		stats[id] = service.GetStats()
	}

	return stats
}

// SetOnConnectionEstablished устанавливает callback для установленных соединений
func (cm *WebRTCConnectionManager) SetOnConnectionEstablished(callback func(string)) {
	cm.onConnectionEstablished = callback
}

// SetOnConnectionLost устанавливает callback для потерянных соединений
func (cm *WebRTCConnectionManager) SetOnConnectionLost(callback func(string)) {
	cm.onConnectionLost = callback
}

// SetOnError устанавливает callback для ошибок
func (cm *WebRTCConnectionManager) SetOnError(callback func(string, error)) {
	cm.onError = callback
}

// CloseAllConnections закрывает все соединения
func (cm *WebRTCConnectionManager) CloseAllConnections() {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	for id, service := range cm.connections {
		if err := service.Disconnect(); err != nil {
			logrus.Errorf("Ошибка отключения соединения %s: %v", id, err)
		}
	}

	cm.connections = make(map[string]*WebRTCService)
	logrus.Info("Все WebRTC соединения закрыты")
}









