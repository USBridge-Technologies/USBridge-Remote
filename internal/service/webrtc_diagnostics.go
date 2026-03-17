// +build !android

package service

import (
	"fmt"
	"time"

	"github.com/pion/webrtc/v3"
	"github.com/sirupsen/logrus"
)

// WebRTCDiagnostics диагностика WebRTC соединения
type WebRTCDiagnostics struct {
	// Статистика соединения
	connectionAttempts    int
	successfulConnections int
	failedConnections     int

	// Временные метки
	lastConnectionAttempt    time.Time
	lastSuccessfulConnection time.Time
	lastFailure              time.Time

	// Детали ошибок
	lastError   string
	errorCounts map[string]int
}

// NewWebRTCDiagnostics создает новый объект диагностики
func NewWebRTCDiagnostics() *WebRTCDiagnostics {
	return &WebRTCDiagnostics{
		errorCounts: make(map[string]int),
	}
}

// RecordConnectionAttempt записывает попытку подключения
func (wd *WebRTCDiagnostics) RecordConnectionAttempt() {
	wd.connectionAttempts++
	wd.lastConnectionAttempt = time.Now()
	logrus.Infof("🔍 Попытка подключения #%d", wd.connectionAttempts)
}

// RecordSuccessfulConnection записывает успешное подключение
func (wd *WebRTCDiagnostics) RecordSuccessfulConnection() {
	wd.successfulConnections++
	wd.lastSuccessfulConnection = time.Now()
	logrus.Infof("✅ Успешное подключение #%d", wd.successfulConnections)
}

// RecordFailedConnection записывает неудачное подключение
func (wd *WebRTCDiagnostics) RecordFailedConnection(err error) {
	wd.failedConnections++
	wd.lastFailure = time.Now()
	wd.lastError = err.Error()

	// Увеличиваем счетчик для этого типа ошибки
	wd.errorCounts[err.Error()]++

	logrus.Errorf("❌ Неудачное подключение #%d: %v", wd.failedConnections, err)
}

// GetStats возвращает статистику диагностики
func (wd *WebRTCDiagnostics) GetStats() map[string]interface{} {
	return map[string]interface{}{
		"connection_attempts":        wd.connectionAttempts,
		"successful_connections":     wd.successfulConnections,
		"failed_connections":         wd.failedConnections,
		"last_connection_attempt":    wd.lastConnectionAttempt,
		"last_successful_connection": wd.lastSuccessfulConnection,
		"last_failure":               wd.lastFailure,
		"last_error":                 wd.lastError,
		"error_counts":               wd.errorCounts,
		"success_rate":               wd.calculateSuccessRate(),
	}
}

// calculateSuccessRate вычисляет процент успешных подключений
func (wd *WebRTCDiagnostics) calculateSuccessRate() float64 {
	if wd.connectionAttempts == 0 {
		return 0
	}
	return float64(wd.successfulConnections) / float64(wd.connectionAttempts) * 100
}

// LogDetailedStats выводит детальную статистику
func (wd *WebRTCDiagnostics) LogDetailedStats() {
	logrus.Info("📊 Детальная статистика WebRTC:")
	logrus.Infof("  Попыток подключения: %d", wd.connectionAttempts)
	logrus.Infof("  Успешных подключений: %d", wd.successfulConnections)
	logrus.Infof("  Неудачных подключений: %d", wd.failedConnections)
	logrus.Infof("  Процент успеха: %.1f%%", wd.calculateSuccessRate())

	if wd.lastError != "" {
		logrus.Infof("  Последняя ошибка: %s", wd.lastError)
	}

	if len(wd.errorCounts) > 0 {
		logrus.Info("  Частота ошибок:")
		for errorType, count := range wd.errorCounts {
			logrus.Infof("    %s: %d раз", errorType, count)
		}
	}
}

// CheckConnectionHealth проверяет здоровье соединения
func (wd *WebRTCDiagnostics) CheckConnectionHealth() string {
	if wd.connectionAttempts == 0 {
		return "Нет попыток подключения"
	}

	successRate := wd.calculateSuccessRate()

	if successRate >= 80 {
		return "Отличное"
	} else if successRate >= 60 {
		return "Хорошее"
	} else if successRate >= 40 {
		return "Удовлетворительное"
	} else {
		return "Плохое"
	}
}

// Reset сбрасывает статистику
func (wd *WebRTCDiagnostics) Reset() {
	wd.connectionAttempts = 0
	wd.successfulConnections = 0
	wd.failedConnections = 0
	wd.lastError = ""
	wd.errorCounts = make(map[string]int)
	logrus.Info("🔄 Статистика WebRTC сброшена")
}

// WebRTCConnectionValidator валидатор WebRTC соединения
type WebRTCConnectionValidator struct {
	diagnostics *WebRTCDiagnostics
}

// NewWebRTCConnectionValidator создает новый валидатор
func NewWebRTCConnectionValidator() *WebRTCConnectionValidator {
	return &WebRTCConnectionValidator{
		diagnostics: NewWebRTCDiagnostics(),
	}
}

// ValidateOffer проверяет валидность WebRTC offer
func (wv *WebRTCConnectionValidator) ValidateOffer(offer *webrtc.SessionDescription) error {
	if offer == nil {
		return fmt.Errorf("offer не может быть nil")
	}

	if offer.SDP == "" {
		return fmt.Errorf("SDP не может быть пустым")
	}

	// Проверяем наличие H.264 в SDP
	if !wv.containsH264Codec(offer.SDP) {
		logrus.Warn("⚠️ H.264 кодек не найден в SDP")
	}

	logrus.Debugf("✅ Offer валиден, размер SDP: %d байт", len(offer.SDP))
	return nil
}

// ValidateAnswer проверяет валидность WebRTC answer
func (wv *WebRTCConnectionValidator) ValidateAnswer(answer *webrtc.SessionDescription) error {
	if answer == nil {
		return fmt.Errorf("answer не может быть nil")
	}

	if answer.SDP == "" {
		return fmt.Errorf("SDP не может быть пустым")
	}

	logrus.Debugf("✅ Answer валиден, размер SDP: %d байт", len(answer.SDP))
	return nil
}

// containsH264Codec проверяет наличие H.264 кодеков в SDP
func (wv *WebRTCConnectionValidator) containsH264Codec(sdp string) bool {
	// Простая проверка на наличие H.264 в SDP
	return contains(sdp, "H264") || contains(sdp, "h264")
}

// contains проверяет наличие подстроки в строке
func contains(s, substr string) bool {
	return len(s) >= len(substr) &&
		(s == substr ||
			(len(s) > len(substr) &&
				(s[:len(substr)] == substr ||
					s[len(s)-len(substr):] == substr ||
					containsInMiddle(s, substr))))
}

// containsInMiddle проверяет наличие подстроки в середине строки
func containsInMiddle(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// GetDiagnostics возвращает объект диагностики
func (wv *WebRTCConnectionValidator) GetDiagnostics() *WebRTCDiagnostics {
	return wv.diagnostics
}









