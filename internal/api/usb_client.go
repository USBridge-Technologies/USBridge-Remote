package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"usbridge-client/internal/models"

	"github.com/gorilla/websocket"
	"github.com/sirupsen/logrus"
)

// progressReader оборачивает io.Reader и вызывает callback при чтении (для потоковой загрузки)
type progressReader struct {
	reader    io.Reader
	total     int64
	current   *int64
	startTime time.Time
	lastLog   time.Time
	callback  UploadProgressCallback
	mu        sync.Mutex
}

func (pr *progressReader) Read(p []byte) (int, error) {
	n, err := pr.reader.Read(p)
	if n > 0 && pr.callback != nil && pr.total > 0 {
		cur := atomic.AddInt64(pr.current, int64(n))
		pr.mu.Lock()
		now := time.Now()
		shouldUpdate := now.Sub(pr.lastLog) >= 100*time.Millisecond
		if shouldUpdate {
			pr.lastLog = now
		}
		pr.mu.Unlock()
		if shouldUpdate {
			percent := float64(cur) / float64(pr.total) * 100
			if percent > 100 {
				percent = 100
			}
			elapsed := now.Sub(pr.startTime).Seconds()
			var speed float64
			if elapsed > 0 {
				speed = float64(cur) / elapsed / 1024 / 1024
			}
			remaining := pr.total - cur
			var eta time.Duration
			if elapsed > 0 && cur > 0 && remaining > 0 {
				eta = time.Duration(float64(remaining)/(float64(cur)/elapsed)) * time.Second
			}
			pr.callback(percent, cur, pr.total, speed, eta)
		}
	}
	return n, err
}

// USBClient HTTP клиент для USBridge 2 API
type USBClient struct {
	baseURL    string
	httpClient *http.Client
	apiKey     string

	// WebSocket для управления мышью
	mouseWS       *websocket.Conn
	mouseWSMutex  sync.Mutex
	mouseWSActive bool
}

// NewUSBClient создает новый USB клиент
func NewUSBClient(host string, port int, timeout int) *USBClient {
	return &USBClient{
		baseURL: fmt.Sprintf("http://%s:%d", host, port),
		httpClient: &http.Client{
			Timeout: time.Duration(timeout) * time.Second,
		},
	}
}

// GetStatus получает статус системы
func (c *USBClient) GetStatus() (*models.USBStatus, error) {
	resp, err := c.makeRequest("GET", "/api/status", nil)
	if err != nil {
		return nil, err
	}

	var status models.USBStatus
	if err := json.Unmarshal(resp, &status); err != nil {
		return nil, fmt.Errorf("failed to parse status: %v", err)
	}

	return &status, nil
}

// GetServiceStatus получает статус сервиса
func (c *USBClient) GetServiceStatus() (*models.APIResponse, error) {
	resp, err := c.makeRequest("GET", "/api/service/status", nil)
	if err != nil {
		return nil, err
	}

	var apiResp models.APIResponse
	if err := json.Unmarshal(resp, &apiResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %v", err)
	}

	return &apiResp, nil
}

// StartDevice запускает устройство (клавиатура или устройство) - старый API для совместимости
func (c *USBClient) StartDevice(request *models.DeviceStartRequest) (*models.APIResponse, error) {
	// Конвертируем в новый формат массива
	batchRequest := models.DeviceStartBatchRequest{*request}
	return c.StartDevicesBatch(batchRequest)
}

// StartDevicesBatch запускает несколько устройств через массив DeviceRequest
func (c *USBClient) StartDevicesBatch(requests models.DeviceStartBatchRequest) (*models.APIResponse, error) {
	requestJSON, err := json.Marshal(requests)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %v", err)
	}

	url := c.baseURL + "/api/device/start"
	logrus.Infof("🚀 [API-START-DEVICES] POST %s", url)
	logrus.Infof("   📤 [API-START-DEVICES] Request body (JSON): %s", string(requestJSON))
	for i, req := range requests {
		logrus.Infof("   📤 [API-START-DEVICES] Device %d: device=%s", i+1, req.Device)
		if req.Device == "mouse" {
			logrus.Infof("      🖱️ pointer type=%q (mouse=touchpad, touchscreen=touchscreen, absolute=absolute)", req.Type)
		}
		if req.Device == "rndis" {
			logrus.Infof("      🌐 rndis_mode=%q", req.RNDISMode)
		}
		if req.Device == "drive" {
			if req.Port > 0 {
				logrus.Infof("      NBD: server=%s, port=%d, export=%s (bridge connects to 127.0.0.1:%d via FRP)", req.Server, req.Port, req.ExportName, req.Port)
			} else {
				logrus.Infof("      Local: %s", req.Server)
			}
		}
	}

	// 200 = синхронный успех, 202 = Accepted (монтирование в фоне)
	respBody, statusCode, err := c.makeRequestWithAcceptStatuses("POST", "/api/device/start", requestJSON, []int{http.StatusOK, http.StatusAccepted})
	if err != nil {
		logrus.Errorf("❌ [API-START-DEVICES] Request failed: %v", err)
		return nil, err
	}

	var apiResp models.APIResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		logrus.Errorf("❌ [API-START-DEVICES] Failed to parse response: %v, body=%s", err, string(respBody))
		return nil, fmt.Errorf("failed to parse response: %v", err)
	}

	logrus.Infof("✅ [API-START-DEVICES] Server response: HTTP %d, success=%v, message=%s", statusCode, apiResp.Success, apiResp.Message)
	if !apiResp.Success {
		return nil, fmt.Errorf("failed to start devices: %s", apiResp.Message)
	}

	if statusCode == http.StatusAccepted {
		logrus.Infof("⏳ [API-START-DEVICES] 202 Accepted: mounting in background, poll /api/device/info")
	}

	logrus.Infof("✅ Successfully started %d devices", len(requests))
	return &apiResp, nil
}

// StopDevice останавливает устройство по ID - старый API для совместимости
func (c *USBClient) StopDevice(deviceID int) error {
	// В новом API все устройства останавливаются одним запросом
	return c.StopAllDevices()
}

// StopAllDevices останавливает все устройства (новый API)
func (c *USBClient) StopAllDevices() error {
	logrus.Infof("🛑 Stopping all devices")

	resp, err := c.makeRequest("POST", "/api/device/stop", nil)
	if err != nil {
		return err
	}

	var apiResp models.APIResponse
	if err := json.Unmarshal(resp, &apiResp); err != nil {
		return fmt.Errorf("failed to parse response: %v", err)
	}

	if !apiResp.Success {
		return fmt.Errorf("failed to stop devices: %s", apiResp.Message)
	}

	logrus.Infof("✅ All devices stopped")
	return nil
}

// GetDeviceInfo получает информацию об устройствах (новый API)
func (c *USBClient) GetDeviceInfo() (*models.DeviceInfoResponse, error) {
	resp, err := c.makeRequest("GET", "/api/device/info", nil)
	if err != nil {
		return nil, err
	}

	var apiResp models.APIResponse
	if err := json.Unmarshal(resp, &apiResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %v", err)
	}

	if !apiResp.Success {
		return nil, fmt.Errorf("failed to get device information: %s", apiResp.Message)
	}

	// Парсим data в DeviceInfoResponse
	dataBytes, err := json.Marshal(apiResp.Data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal data: %v", err)
	}

	var deviceInfo models.DeviceInfoResponse
	if err := json.Unmarshal(dataBytes, &deviceInfo); err != nil {
		return nil, fmt.Errorf("failed to parse device information: %v", err)
	}
	for _, d := range deviceInfo.Devices {
		logrus.Infof("🔎 [API-DEVICE-INFO] device=%s type=%s status=%s name=%s product=%s",
			d.Device, d.Type, d.Status, d.Name, d.ProductName)
	}

	return &deviceInfo, nil
}

// GetLocalDrives получает список локальных устройств (новый API)
func (c *USBClient) GetLocalDrives() (*models.LocalDrivesResponse, error) {
	resp, err := c.makeRequest("GET", "/api/device/local_drives", nil)
	if err != nil {
		return nil, err
	}

	var apiResp models.APIResponse
	if err := json.Unmarshal(resp, &apiResp); err != nil {
		return nil, fmt.Errorf("ошибка парсинга ответа: %v", err)
	}

	if !apiResp.Success {
		return nil, fmt.Errorf("ошибка получения локальных устройств: %s", apiResp.Message)
	}

	// Парсим data в LocalDrivesResponse
	dataBytes, err := json.Marshal(apiResp.Data)
	if err != nil {
		return nil, fmt.Errorf("ошибка сериализации данных: %v", err)
	}

	var localDrives models.LocalDrivesResponse
	if err := json.Unmarshal(dataBytes, &localDrives); err != nil {
		return nil, fmt.Errorf("ошибка парсинга локальных устройств: %v", err)
	}

	return &localDrives, nil
}

// GetISOSpace получает информацию о месте на SD-карте (btrfs раздел iso/data/backup)
func (c *USBClient) GetISOSpace() (*models.ISOSpaceInfo, error) {
	resp, err := c.makeRequest("GET", "/api/iso/space", nil)
	if err != nil {
		return nil, err
	}

	var apiResp models.APIResponse
	if err := json.Unmarshal(resp, &apiResp); err != nil {
		return nil, fmt.Errorf("ошибка парсинга ответа: %v", err)
	}

	if !apiResp.Success {
		return nil, fmt.Errorf("ошибка получения информации о месте: %s", apiResp.Message)
	}

	dataBytes, err := json.Marshal(apiResp.Data)
	if err != nil {
		return nil, fmt.Errorf("ошибка сериализации данных: %v", err)
	}

	var spaceInfo models.ISOSpaceInfo
	if err := json.Unmarshal(dataBytes, &spaceInfo); err != nil {
		return nil, fmt.Errorf("ошибка парсинга информации о месте: %v", err)
	}

	return &spaceInfo, nil
}

// GetDeviceStatus получает статус устройств (новый API)
func (c *USBClient) GetDeviceStatus() (*models.DeviceStatusResponse, error) {
	resp, err := c.makeRequest("GET", "/api/device/status", nil)
	if err != nil {
		return nil, err
	}

	var apiResp models.APIResponse
	if err := json.Unmarshal(resp, &apiResp); err != nil {
		return nil, fmt.Errorf("ошибка парсинга ответа: %v", err)
	}

	if !apiResp.Success {
		return nil, fmt.Errorf("ошибка получения статуса устройств: %s", apiResp.Message)
	}

	// Парсим data в DeviceStatusResponse
	dataBytes, err := json.Marshal(apiResp.Data)
	if err != nil {
		return nil, fmt.Errorf("ошибка сериализации данных: %v", err)
	}

	var deviceStatus models.DeviceStatusResponse
	if err := json.Unmarshal(dataBytes, &deviceStatus); err != nil {
		return nil, fmt.Errorf("ошибка парсинга статуса устройств: %v", err)
	}

	return &deviceStatus, nil
}

// StartService запускает сервис USBridge 2 (старый API для совместимости)
func (c *USBClient) StartService() error {
	return c.startServiceWithRetry(0)
}

// startServiceWithRetry запускает сервис с ограниченным количеством попыток
func (c *USBClient) startServiceWithRetry(retryCount int) error {
	const maxRetries = 2

	resp, err := c.makeRequest("POST", "/api/service/start", nil)
	if err != nil {
		return err
	}

	var apiResp models.APIResponse
	if err := json.Unmarshal(resp, &apiResp); err != nil {
		return fmt.Errorf("ошибка парсинга ответа: %v", err)
	}

	if !apiResp.Success {
		// Проверяем, не является ли ошибка "device or resource busy"
		if (strings.Contains(apiResp.Message, "device or resource busy") ||
			strings.Contains(apiResp.Error, "device or resource busy") ||
			strings.Contains(apiResp.Message, "UDC") ||
			strings.Contains(apiResp.Error, "UDC")) && retryCount < maxRetries {

			logrus.Warnf("⚠️ USB gadget занят, попытка восстановления #%d/%d...", retryCount+1, maxRetries)

			// Принудительно отключаем USB gadget
			if disconnectErr := c.ForceDisconnectUSBGadget(); disconnectErr != nil {
				logrus.Warnf("⚠️ Ошибка принудительного отключения USB gadget: %v", disconnectErr)
			}

			// Останавливаем сервис
			if stopErr := c.StopService(); stopErr != nil {
				logrus.Warnf("⚠️ Ошибка остановки сервиса: %v", stopErr)
			}

			// Ждем немного
			time.Sleep(3 * time.Second)

			// Пытаемся запустить снова
			logrus.Info("🔄 Повторная попытка запуска сервиса...")
			return c.startServiceWithRetry(retryCount + 1)
		}
		return fmt.Errorf("ошибка запуска сервиса: %s", apiResp.Message)
	}

	logrus.Info("✅ Сервис USBridge 2 запущен")
	return nil
}

// StopService останавливает сервис USBridge 2
func (c *USBClient) StopService() error {
	resp, err := c.makeRequest("POST", "/api/service/stop", nil)
	if err != nil {
		return err
	}

	var apiResp models.APIResponse
	if err := json.Unmarshal(resp, &apiResp); err != nil {
		return fmt.Errorf("ошибка парсинга ответа: %v", err)
	}

	if !apiResp.Success {
		return fmt.Errorf("ошибка остановки сервиса: %s", apiResp.Message)
	}

	logrus.Info("🛑 Сервис USBridge 2 остановлен")
	return nil
}

// ForceDisconnectUSBGadget принудительно отключает USB gadget
func (c *USBClient) ForceDisconnectUSBGadget() error {
	logrus.Info("🔧 Принудительное отключение USB gadget...")

	resp, err := c.makeRequest("POST", "/api/usb/disconnect", nil)
	if err != nil {
		return err
	}

	var apiResp models.APIResponse
	if err := json.Unmarshal(resp, &apiResp); err != nil {
		return fmt.Errorf("ошибка парсинга ответа: %v", err)
	}

	if !apiResp.Success {
		logrus.Warnf("⚠️ Ошибка принудительного отключения USB gadget: %s", apiResp.Message)
		// Не возвращаем ошибку, так как это может быть нормально
	}

	logrus.Info("✅ USB gadget принудительно отключен")
	return nil
}

// RestartService перезапускает сервис USBridge 2
func (c *USBClient) RestartService() error {
	resp, err := c.makeRequest("POST", "/api/service/restart", nil)
	if err != nil {
		return err
	}

	var apiResp models.APIResponse
	if err := json.Unmarshal(resp, &apiResp); err != nil {
		return fmt.Errorf("ошибка парсинга ответа: %v", err)
	}

	if !apiResp.Success {
		return fmt.Errorf("ошибка перезапуска сервиса: %s", apiResp.Message)
	}

	logrus.Info("🔄 Сервис USBridge 2 перезапущен")
	return nil
}

// GetDevices получает список устройств
func (c *USBClient) GetDevices() (*models.APIResponse, error) {
	resp, err := c.makeRequest("GET", "/api/devices", nil)
	if err != nil {
		return nil, err
	}

	var apiResp models.APIResponse
	if err := json.Unmarshal(resp, &apiResp); err != nil {
		return nil, fmt.Errorf("ошибка парсинга ответа: %v", err)
	}

	return &apiResp, nil
}

// GetConfig получает конфигурацию
func (c *USBClient) GetConfig() (*models.APIResponse, error) {
	resp, err := c.makeRequest("GET", "/api/config", nil)
	if err != nil {
		return nil, err
	}

	var apiResp models.APIResponse
	if err := json.Unmarshal(resp, &apiResp); err != nil {
		return nil, fmt.Errorf("ошибка парсинга ответа: %v", err)
	}

	return &apiResp, nil
}

// UpdateConfig обновляет конфигурацию
func (c *USBClient) UpdateConfig(config *models.ConfigRequest) error {
	configJSON, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("ошибка сериализации конфигурации: %v", err)
	}

	resp, err := c.makeRequest("POST", "/api/config", configJSON)
	if err != nil {
		return err
	}

	var apiResp models.APIResponse
	if err := json.Unmarshal(resp, &apiResp); err != nil {
		return fmt.Errorf("ошибка парсинга ответа: %v", err)
	}

	if !apiResp.Success {
		return fmt.Errorf("ошибка обновления конфигурации: %s", apiResp.Message)
	}

	logrus.Info("✅ Конфигурация USBridge 2 обновлена")
	return nil
}

// SendKey отправляет клавишу
func (c *USBClient) SendKey(keyCode int) error {
	request := models.KeyboardRequest{
		Action:  "key",
		KeyCode: keyCode,
	}

	return c.sendKeyboardRequest(request)
}

// SendCombo отправляет комбинацию клавиш
func (c *USBClient) SendCombo(modifiers int, keyCode int) error {
	request := models.KeyboardRequest{
		Action:    "combo",
		Modifiers: modifiers,
		KeyCode:   keyCode,
	}

	return c.sendKeyboardRequest(request)
}

// SendText отправляет текст
func (c *USBClient) SendText(text string) error {
	request := models.KeyboardRequest{
		Action: "text",
		Text:   text,
	}

	return c.sendKeyboardRequest(request)
}

// sendKeyboardRequest отправляет запрос клавиатуры
func (c *USBClient) sendKeyboardRequest(request models.KeyboardRequest) error {
	requestJSON, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("ошибка сериализации запроса: %v", err)
	}

	resp, err := c.makeRequest("POST", "/api/keyboard", requestJSON)
	if err != nil {
		return err
	}

	var apiResp models.APIResponse
	if err := json.Unmarshal(resp, &apiResp); err != nil {
		return fmt.Errorf("ошибка парсинга ответа: %v", err)
	}

	if !apiResp.Success {
		return fmt.Errorf("ошибка отправки команды: %s", apiResp.Message)
	}

	return nil
}

// SendMouseMove отправляет относительное перемещение мыши (тачпад)
func (c *USBClient) SendMouseMove(dx, dy int) error {
	request := models.MouseRequest{
		Action: "move",
		DX:     dx,
		DY:     dy,
	}
	return c.sendMouseRequest(request)
}

// SendTouch отправляет касание тачскрина (action: "touch").
// x, y — абсолютные координаты 0..4095; tip: true = касание, false = отпускание.
func (c *USBClient) SendTouch(x, y int, tip bool) error {
	action := "отпущено"
	if tip {
		action = "нажато"
	}
	logrus.Infof("🖐️ [Touch] отправлено: x=%d y=%d %s", x, y, action)
	request := models.MouseRequest{
		Action: "touch",
		X:      x,
		Y:      y,
		Tip:    tip,
	}
	return c.sendMouseRequest(request)
}

// SendTouchPositionOnly отправляет только позицию тача без эмуляции левой кнопки (action: "touch_position").
// Используется для правой кнопки в режиме тача: позиция по тачу, клик кнопкой 2 отдельно.
func (c *USBClient) SendTouchPositionOnly(x, y int, tip bool) error {
	request := models.MouseRequest{
		Action: "touch_position",
		X:      x,
		Y:      y,
		Tip:    tip,
	}
	return c.sendMouseRequest(request)
}

// SendMouseClick отправляет клик мыши
func (c *USBClient) SendMouseClick(button int) error {
	request := models.MouseRequest{
		Action: "click",
		Button: button,
	}
	return c.sendMouseRequest(request)
}

// SendMouseScroll отправляет прокрутку колесика
func (c *USBClient) SendMouseScroll(scroll int) error {
	request := models.MouseRequest{
		Action: "scroll",
		Scroll: scroll,
	}
	return c.sendMouseRequest(request)
}

// SendAbsoluteEvent отправляет атомарное абсолютное событие (позиция + кнопки + колесо).
func (c *USBClient) SendAbsoluteEvent(x, y int, buttons uint8, scroll int) error {
	if scroll > 127 {
		scroll = 127
	} else if scroll < -127 {
		scroll = -127
	}
	request := models.MouseRequest{
		Action:      "absolute_event",
		X:           x,
		Y:           y,
		ButtonState: int(buttons),
		Scroll:      scroll,
	}
	return c.sendMouseRequest(request)
}

// SendMouseAction отправляет комплексное действие мыши
func (c *USBClient) SendMouseAction(button, dx, dy, scroll int) error {
	request := models.MouseRequest{
		Action: "action",
		Button: button,
		DX:     dx,
		DY:     dy,
		Scroll: scroll,
	}
	return c.sendMouseRequest(request)
}

// sendMouseRequest отправляет запрос мыши через WebSocket или HTTP
func (c *USBClient) sendMouseRequest(request models.MouseRequest) error {
	// Пытаемся отправить через WebSocket
	if c.mouseWSActive {
		c.mouseWSMutex.Lock()
		if c.mouseWS != nil {
			err := c.mouseWS.WriteJSON(request)
			c.mouseWSMutex.Unlock()
			if err == nil {
				return nil
			}
			// Если ошибка WebSocket, закрываем соединение
			logrus.Warnf("⚠️ WebSocket ошибка, переключаемся на HTTP: %v", err)
			c.DisconnectMouseWebSocket()
		} else {
			c.mouseWSMutex.Unlock()
		}
	}

	// Fallback на HTTP если WebSocket не активен или произошла ошибка
	requestJSON, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("ошибка сериализации запроса: %v", err)
	}

	resp, err := c.makeRequest("POST", "/api/mouse", requestJSON)
	if err != nil {
		return err
	}

	var apiResp models.APIResponse
	if err := json.Unmarshal(resp, &apiResp); err != nil {
		return fmt.Errorf("ошибка парсинга ответа: %v", err)
	}

	if !apiResp.Success {
		return fmt.Errorf("ошибка отправки команды мыши: %s", apiResp.Message)
	}

	return nil
}

// ConnectMouseWebSocket подключается к WebSocket для управления мышью
func (c *USBClient) ConnectMouseWebSocket() error {
	c.mouseWSMutex.Lock()
	defer c.mouseWSMutex.Unlock()

	// Если уже подключено, не переподключаемся
	if c.mouseWS != nil && c.mouseWSActive {
		return nil
	}

	// Закрываем старое соединение если есть
	if c.mouseWS != nil {
		c.mouseWS.Close()
		c.mouseWS = nil
	}

	// Формируем WebSocket URL
	wsURL, err := c.getWebSocketURL("/api/mouse/ws")
	if err != nil {
		return fmt.Errorf("ошибка формирования WebSocket URL: %v", err)
	}

	logrus.Infof("🔌 Подключение к WebSocket: %s", wsURL)

	// Подключаемся к WebSocket
	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	conn, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		return fmt.Errorf("ошибка подключения к WebSocket: %v", err)
	}

	c.mouseWS = conn
	c.mouseWSActive = true

	// Запускаем горутину для чтения ответов
	go c.readMouseWebSocketResponses()

	logrus.Info("✅ WebSocket для мыши подключен")
	return nil
}

// DisconnectMouseWebSocket отключается от WebSocket
func (c *USBClient) DisconnectMouseWebSocket() {
	c.mouseWSMutex.Lock()
	defer c.mouseWSMutex.Unlock()

	c.mouseWSActive = false
	if c.mouseWS != nil {
		c.mouseWS.Close()
		c.mouseWS = nil
		logrus.Info("🔌 WebSocket для мыши отключен")
	}
}

// IsMouseWebSocketActive проверяет, активен ли WebSocket
func (c *USBClient) IsMouseWebSocketActive() bool {
	c.mouseWSMutex.Lock()
	defer c.mouseWSMutex.Unlock()
	return c.mouseWSActive && c.mouseWS != nil
}

// readMouseWebSocketResponses читает ответы от WebSocket сервера
func (c *USBClient) readMouseWebSocketResponses() {
	for {
		c.mouseWSMutex.Lock()
		conn := c.mouseWS
		active := c.mouseWSActive
		c.mouseWSMutex.Unlock()

		if !active || conn == nil {
			break
		}

		var response models.APIResponse
		err := conn.ReadJSON(&response)
		if err != nil {
			if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				logrus.Debugf("WebSocket read error: %v", err)
			}
			c.DisconnectMouseWebSocket()
			break
		}

		// Обрабатываем ответ если нужно
		if !response.Success {
			logrus.Warnf("⚠️ WebSocket mouse error: %s", response.Error)
		}
	}
}

// getWebSocketURL преобразует HTTP URL в WebSocket URL
func (c *USBClient) getWebSocketURL(path string) (string, error) {
	parsedURL, err := url.Parse(c.baseURL)
	if err != nil {
		return "", err
	}

	// Меняем схему на ws:// или wss://
	scheme := "ws"
	if parsedURL.Scheme == "https" {
		scheme = "wss"
	}

	wsURL := fmt.Sprintf("%s://%s%s", scheme, parsedURL.Host, path)
	return wsURL, nil
}

// GetVideoInfo получает информацию о видео
func (c *USBClient) GetVideoInfo() (*models.APIResponse, error) {
	resp, err := c.makeRequest("GET", "/api/video/info", nil)
	if err != nil {
		return nil, err
	}

	var apiResp models.APIResponse
	if err := json.Unmarshal(resp, &apiResp); err != nil {
		return nil, fmt.Errorf("ошибка парсинга ответа: %v", err)
	}

	return &apiResp, nil
}

// StartVideo запускает видео стриминг с параметрами (новый API)
func (c *USBClient) StartVideo(request *models.VideoStartRequest) error {
	requestJSON, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("ошибка сериализации запроса: %v", err)
	}

	mode := request.VideoMode
	if mode == "" {
		mode = models.VideoModeH264
	}
	logrus.Infof("🎥 Запуск видео стриминга: mode=%s %dx%d@%dfps, качество: %d, битрейт: %s",
		mode, request.VideoWidth, request.VideoHeight, request.VideoFPS, request.VideoQuality, request.VideoBitrate)

	resp, err := c.makeRequest("POST", "/api/video/start", requestJSON)
	if err != nil {
		return err
	}

	var apiResp models.APIResponse
	if err := json.Unmarshal(resp, &apiResp); err != nil {
		return fmt.Errorf("ошибка парсинга ответа: %v", err)
	}

	if !apiResp.Success {
		// Проверяем, не является ли ошибка сообщением о том, что видео уже запущено
		if apiResp.Message != "" &&
			(strings.Contains(apiResp.Message, "already running") ||
				strings.Contains(apiResp.Message, "уже запущен") ||
				strings.Contains(apiResp.Message, "already started") ||
				strings.Contains(apiResp.Message, "уже запущено")) {
			logrus.Info("🎥 Видео стриминг уже запущен")
			return nil
		}
		return fmt.Errorf("ошибка запуска видео: %s", apiResp.Message)
	}

	logrus.Info("✅ Видео стриминг запущен")
	return nil
}

func (c *USBClient) BootstrapWireGuard(request *models.WireGuardBootstrapRequest) (*models.WireGuardBootstrapResponse, error) {
	requestJSON, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal WireGuard bootstrap request: %v", err)
	}

	resp, err := c.makeRequest("POST", "/api/auth/wireguard/bootstrap", requestJSON)
	if err != nil {
		return nil, err
	}

	var apiResp models.APIResponse
	if err := json.Unmarshal(resp, &apiResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %v", err)
	}
	if !apiResp.Success {
		return nil, fmt.Errorf("wireguard bootstrap failed: %s", apiResp.Message)
	}

	raw, err := json.Marshal(apiResp.Data)
	if err != nil {
		return nil, fmt.Errorf("failed to re-marshal bootstrap data: %v", err)
	}
	var parsed models.WireGuardBootstrapResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("failed to parse bootstrap payload: %v", err)
	}
	return &parsed, nil
}

// StartVideoLegacy запускает видео стриминг (старый API для совместимости)
func (c *USBClient) StartVideoLegacy() error {
	resp, err := c.makeRequest("POST", "/api/video/start", nil)
	if err != nil {
		return err
	}

	var apiResp models.APIResponse
	if err := json.Unmarshal(resp, &apiResp); err != nil {
		return fmt.Errorf("ошибка парсинга ответа: %v", err)
	}

	if !apiResp.Success {
		// Проверяем, не является ли ошибка сообщением о том, что видео уже запущено
		if apiResp.Message != "" &&
			(strings.Contains(apiResp.Message, "already running") ||
				strings.Contains(apiResp.Message, "уже запущен") ||
				strings.Contains(apiResp.Message, "already started") ||
				strings.Contains(apiResp.Message, "уже запущено")) {
			logrus.Info("🎥 Видео стриминг уже запущен")
			return nil
		}
		return fmt.Errorf("ошибка запуска видео: %s", apiResp.Message)
	}

	logrus.Info("🎥 Видео стриминг запущен")
	return nil
}

// StopVideo останавливает видео стриминг (новый API)
func (c *USBClient) StopVideo() error {
	logrus.Info("🛑 Остановка видео стриминга...")

	resp, err := c.makeRequest("POST", "/api/video/stop", nil)
	if err != nil {
		return err
	}

	var apiResp models.APIResponse
	if err := json.Unmarshal(resp, &apiResp); err != nil {
		return fmt.Errorf("ошибка парсинга ответа: %v", err)
	}

	if !apiResp.Success {
		return fmt.Errorf("ошибка остановки видео: %s", apiResp.Message)
	}

	logrus.Info("✅ Видео стриминг остановлен")
	return nil
}

// StopVideoLegacy останавливает видео стриминг (старый API для совместимости)
func (c *USBClient) StopVideoLegacy() error {
	resp, err := c.makeRequest("POST", "/api/video/stop", nil)
	if err != nil {
		return err
	}

	var apiResp models.APIResponse
	if err := json.Unmarshal(resp, &apiResp); err != nil {
		return fmt.Errorf("ошибка парсинга ответа: %v", err)
	}

	if !apiResp.Success {
		return fmt.Errorf("ошибка остановки видео: %s", apiResp.Message)
	}

	logrus.Info("🛑 Видео стриминг остановлен")
	return nil
}

// makeRequest выполняет HTTP запрос
func (c *USBClient) makeRequest(method, endpoint string, body []byte) ([]byte, error) {
	url := c.baseURL + endpoint

	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}

	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("ошибка создания запроса: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ошибка выполнения запроса: %v", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("ошибка чтения ответа: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP ошибка %d: %s", resp.StatusCode, string(respBody))
	}

	return respBody, nil
}

// makeRequestWithAcceptStatuses выполняет HTTP запрос, принимая указанные статус-коды как успешные
func (c *USBClient) makeRequestWithAcceptStatuses(method, endpoint string, body []byte, acceptStatuses []int) ([]byte, int, error) {
	url := c.baseURL + endpoint

	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}

	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		return nil, 0, fmt.Errorf("ошибка создания запроса: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("ошибка выполнения запроса: %v", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, fmt.Errorf("ошибка чтения ответа: %v", err)
	}

	for _, code := range acceptStatuses {
		if resp.StatusCode == code {
			return respBody, resp.StatusCode, nil
		}
	}
	return nil, resp.StatusCode, fmt.Errorf("HTTP ошибка %d: %s", resp.StatusCode, string(respBody))
}

// TestConnection проверяет соединение с USBridge 2
func (c *USBClient) TestConnection() error {
	_, err := c.GetDeviceInfo()
	if err != nil {
		return fmt.Errorf("не удается подключиться к USBridge 2: %v", err)
	}

	logrus.Info("✅ Соединение с USBridge 2 установлено")
	return nil
}

// GetSnapshots получает список снапшотов
func (c *USBClient) GetSnapshots() (*models.SnapshotsResponse, error) {
	resp, err := c.makeRequest("POST", "/api/backup/get_snapshots", []byte("{}"))
	if err != nil {
		return nil, err
	}

	var apiResp models.APIResponse
	if err := json.Unmarshal(resp, &apiResp); err != nil {
		return nil, fmt.Errorf("ошибка парсинга ответа: %v", err)
	}

	if !apiResp.Success {
		return nil, fmt.Errorf("ошибка получения снапшотов: %s", apiResp.Message)
	}

	// Парсим data в SnapshotsJSONResponse
	dataBytes, err := json.Marshal(apiResp.Data)
	if err != nil {
		return nil, fmt.Errorf("ошибка сериализации данных: %v", err)
	}

	var snapshotsJSON models.SnapshotsJSONResponse
	if err := json.Unmarshal(dataBytes, &snapshotsJSON); err != nil {
		return nil, fmt.Errorf("ошибка парсинга снапшотов: %v", err)
	}

	// Преобразуем JSON структуру в обычную структуру с правильным временем
	snapshots := snapshotsJSON.ToSnapshotsResponse()
	return snapshots, nil
}

// GetBaseURL возвращает базовый URL USB клиента
func (c *USBClient) GetBaseURL() string {
	return c.baseURL
}

// GetPCPanelLeds получает состояние POWER и HDD LEDs целевого компьютера
func (c *USBClient) GetPCPanelLeds() (*models.PCPanelLedsResponse, error) {
	resp, err := c.makeRequest("GET", "/api/pcpanel/leds", nil)
	if err != nil {
		return nil, err
	}

	var apiResp models.PCPanelLedsResponse
	if err := json.Unmarshal(resp, &apiResp); err != nil {
		return nil, fmt.Errorf("ошибка парсинга ответа LEDs: %v", err)
	}

	if !apiResp.Success {
		return nil, fmt.Errorf("ошибка чтения LEDs: %s", apiResp.Message)
	}

	return &apiResp, nil
}

// PressPCPanelButton имитирует нажатие кнопки Power или Reset на целевом ПК
// durationSec — длительность зажатия в секундах (0 = короткое нажатие).
// TODO: когда API поддерживает long press, передавать durationSec в запросе
func (c *USBClient) PressPCPanelButton(button string, durationSec int) error {
	if button != "power" && button != "reset" {
		return fmt.Errorf("неверная кнопка: используйте power или reset")
	}

	req := models.PCPanelButtonRequest{Button: button}
	requestJSON, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("ошибка сериализации запроса: %v", err)
	}

	resp, err := c.makeRequest("POST", "/api/pcpanel/button", requestJSON)
	if err != nil {
		return err
	}

	var apiResp models.APIResponse
	if err := json.Unmarshal(resp, &apiResp); err != nil {
		return fmt.Errorf("ошибка парсинга ответа: %v", err)
	}

	if !apiResp.Success {
		return fmt.Errorf("ошибка нажатия кнопки: %s", apiResp.Message)
	}

	_ = durationSec // Заглушка для будущей поддержки long press
	logrus.Infof("✅ PC Panel: кнопка %s нажата", button)
	return nil
}

// UploadProgressCallback вызывается для обновления прогресса загрузки
type UploadProgressCallback func(percent float64, current, total int64, speed float64, eta time.Duration)

// progressWriter отслеживает прогресс записи в HTTP соединение
type progressWriter struct {
	writer        io.Writer
	total         int64
	current       *int64 // используем указатель для atomic операций
	lastLog       time.Time
	lastLogOutput time.Time // когда последний раз выводили в лог
	startTime     time.Time
	lastCurrent   int64
	callback      UploadProgressCallback
	mu            sync.Mutex
}

func (pw *progressWriter) Write(p []byte) (int, error) {
	n, err := pw.writer.Write(p)
	if n > 0 {
		atomic.AddInt64(pw.current, int64(n))

		// Обновляем прогресс каждые 100ms для плавной анимации
		pw.mu.Lock()
		now := time.Now()
		shouldUpdate := now.Sub(pw.lastLog) >= 100*time.Millisecond
		pw.mu.Unlock()

		if shouldUpdate {
			current := atomic.LoadInt64(pw.current)
			percent := float64(current) / float64(pw.total) * 100
			if percent > 100 {
				percent = 100
			}

			// Вычисляем скорость загрузки
			elapsed := now.Sub(pw.startTime).Seconds()
			var speed float64
			if elapsed > 0 {
				speed = float64(current) / elapsed / 1024 / 1024 // МБ/с
			}

			// Оценка оставшегося времени
			remaining := pw.total - current
			var eta time.Duration
			if elapsed > 0 && current > 0 && remaining > 0 {
				eta = time.Duration(float64(remaining)/(float64(current)/elapsed)) * time.Second
			}

			// Логируем прогресс каждую секунду
			pw.mu.Lock()
			shouldLog := now.Sub(pw.lastLogOutput) >= time.Second
			if shouldLog {
				pw.lastLogOutput = now
			}
			pw.lastLog = now
			pw.lastCurrent = current
			pw.mu.Unlock()

			if shouldLog {
				logrus.Infof("📊 Прогресс: %.1f%% (%.2f МБ / %.2f МБ) | Скорость: %.2f МБ/с | Осталось: %v",
					percent,
					float64(current)/1024/1024,
					float64(pw.total)/1024/1024,
					speed,
					eta.Round(time.Second))
			}

			// Вызываем callback для обновления UI каждые 100ms
			if pw.callback != nil {
				pw.callback(percent, current, pw.total, speed, eta)
			}
		}
	}

	return n, err
}

// UploadISO загружает ISO образ на устройство
func (c *USBClient) UploadISO(filePath string, fileReader io.Reader) error {
	return c.UploadISOWithProgress(filePath, fileReader, nil)
}

// isRetriableUploadError возвращает true, если ошибка загрузки может быть исправлена повтором.
// FRP туннель периодически переподключается — при broken pipe можно повторить запрос.
func isRetriableUploadError(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "broken pipe") ||
		strings.Contains(s, "connection reset") ||
		strings.Contains(s, "connection refused") ||
		strings.Contains(s, "io: read/write on closed pipe") ||
		strings.Contains(s, "use of closed network connection") ||
		errors.Is(err, io.EOF)
}

// computeMultipartSize вычисляет точный размер multipart тела через реальную генерацию заголовков.
// Использует тот же multipart.Writer — размер гарантированно совпадает с фактическим телом запроса.
func computeMultipartSize(boundary, fileName string, fileSize int64) int64 {
	buf := &bytes.Buffer{}
	w := multipart.NewWriter(buf)
	_ = w.SetBoundary(boundary)
	if _, err := w.CreateFormFile("file", fileName); err != nil {
		return 0
	}
	if err := w.Close(); err != nil {
		return 0
	}
	// buf = --{boundary}\r\n + headers + \r\n + \r\n--{boundary}--\r\n (для пустого файла)
	// С файлом: те же байты, но вместо пустого тела — fileSize байт
	return int64(buf.Len()) + fileSize
}

// UploadISOWithProgress загружает ISO образ на устройство с callback для прогресса.
// Использует потоковую передачу — файл не загружается целиком в память, UI не зависает.
// При broken pipe (переподключение FRP туннеля) автоматически повторяет до 3 раз с паузой 3 сек.
func (c *USBClient) UploadISOWithProgress(filePath string, fileReader io.Reader, progressCallback UploadProgressCallback) error {
	logrus.Infof("📤 Загрузка ISO образа на устройство: %s", filePath)

	// Определяем размер файла для прогресс-бара
	var fileSize int64
	if f, ok := fileReader.(*os.File); ok {
		if info, err := f.Stat(); err == nil {
			fileSize = info.Size()
			logrus.Infof("📊 Размер файла: %.2f МБ", float64(fileSize)/1024/1024)
		}
	}

	// Проверяем, можно ли повторить при ошибке (нужен io.Seeker для сброса позиции)
	canRetry := false
	if _, ok := fileReader.(io.Seeker); ok {
		canRetry = true
	}

	const maxRetries = 3
	const retryDelay = 3 * time.Second

	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			if !canRetry {
				return lastErr
			}
			seeker := fileReader.(io.Seeker)
			if _, err := seeker.Seek(0, 0); err != nil {
				return fmt.Errorf("невозможно повторить загрузку: %w", err)
			}
			logrus.Warnf("🔄 Повтор загрузки (попытка %d/%d) после ошибки: %v", attempt+1, maxRetries, lastErr)
			time.Sleep(retryDelay)
		}

		lastErr = c.doUploadISOAttempt(filePath, fileReader, fileSize, progressCallback)
		if lastErr == nil {
			logrus.Infof("✅ Образ успешно загружен на устройство")
			return nil
		}
		if !isRetriableUploadError(lastErr) {
			return lastErr
		}
	}
	return lastErr
}

// doUploadISOAttempt выполняет одну попытку загрузки ISO.
func (c *USBClient) doUploadISOAttempt(filePath string, fileReader io.Reader, fileSize int64, progressCallback UploadProgressCallback) error {
	url := c.baseURL + "/api/iso/upload"
	fileName := filepath.Base(filePath)

	if progressCallback != nil && fileSize > 0 {
		progressCallback(0, 0, fileSize, 0, 0)
	}

	pipeReader, pipeWriter := io.Pipe()
	writer := multipart.NewWriter(pipeWriter)
	contentType := writer.FormDataContentType()
	boundary := writer.Boundary()

	var current int64

	go func() {
		defer pipeWriter.Close()
		defer writer.Close()

		part, err := writer.CreateFormFile("file", fileName)
		if err != nil {
			logrus.Errorf("❌ Ошибка создания form field: %v", err)
			pipeWriter.CloseWithError(err)
			return
		}

		var src io.Reader = fileReader
		if fileSize > 0 && progressCallback != nil {
			src = &progressReader{
				reader:    fileReader,
				total:     fileSize,
				current:   &current,
				startTime: time.Now(),
				lastLog:   time.Now(),
				callback:  progressCallback,
			}
		}

		written, copyErr := io.Copy(part, src)
		if copyErr != nil {
			logrus.Errorf("❌ Ошибка чтения файла: %v", copyErr)
			pipeWriter.CloseWithError(copyErr)
			return
		}
		logrus.Infof("📊 Прочитано для отправки: %.2f МБ", float64(written)/1024/1024)

		if progressCallback != nil {
			total := fileSize
			if total <= 0 {
				total = written
			}
			progressCallback(100, total, total, 0, 0)
		}
	}()

	req, err := http.NewRequest("POST", url, pipeReader)
	if err != nil {
		return fmt.Errorf("ошибка создания запроса: %v", err)
	}

	if fileSize > 0 {
		req.ContentLength = computeMultipartSize(boundary, fileName, fileSize)
	}

	req.Header.Set("Content-Type", contentType)
	if fileSize > 0 {
		req.Header.Set("X-File-Size", strconv.FormatInt(fileSize, 10))
	}
	req.Header.Set("Expect", "100-continue")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	logrus.Info("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	logrus.Infof("🔍 HTTP ЗАПРОС:")
	logrus.Infof("   Метод: %s", req.Method)
	logrus.Infof("   URL: %s", url)
	logrus.Infof("   Заголовки:")
	for key, values := range req.Header {
		for _, value := range values {
			if key == "Authorization" && value != "" {
				logrus.Infof("     %s: Bearer [СКРЫТ]", key)
			} else {
				logrus.Infof("     %s: %s", key, value)
			}
		}
	}
	logrus.Info("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	logrus.Info("⏳ Отправка запроса (streaming mode)...")
	uploadClient := &http.Client{
		Timeout: 3600 * time.Second,
		Transport: &http.Transport{
			ExpectContinueTimeout: 10 * time.Second,
		},
	}
	resp, err := uploadClient.Do(req)
	if err != nil {
		logrus.Errorf("❌ Ошибка выполнения запроса: %v", err)
		return fmt.Errorf("ошибка выполнения запроса: %v", err)
	}
	defer resp.Body.Close()

	logrus.Info("✅ Ответ получен")

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		logrus.Errorf("❌ Ошибка чтения ответа: %v", err)
		return fmt.Errorf("ошибка чтения ответа: %v", err)
	}

	logrus.Info("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	logrus.Infof("🔍 HTTP ОТВЕТ:")
	logrus.Infof("   Статус: %d %s", resp.StatusCode, resp.Status)
	logrus.Infof("   Заголовки:")
	for key, values := range resp.Header {
		for _, value := range values {
			logrus.Infof("     %s: %s", key, value)
		}
	}
	logrus.Infof("   Тело ответа (%d байт):", len(respBody))
	if len(respBody) > 0 {
		logrus.Infof("   %s", string(respBody))
	} else {
		logrus.Info("   [пустое]")
	}
	logrus.Info("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP ошибка %d: %s", resp.StatusCode, string(respBody))
	}

	var apiResp models.APIResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return fmt.Errorf("ошибка парсинга ответа: %v", err)
	}

	if !apiResp.Success {
		return fmt.Errorf("ошибка загрузки образа: %s", apiResp.Message)
	}

	return nil
}

// DeleteISO удаляет ISO образ с устройства
func (c *USBClient) DeleteISO(filename string) error {
	logrus.Infof("🗑️ Удаление ISO образа с устройства: %s", filename)

	// Формируем запрос
	request := map[string]string{
		"filename": filename,
	}

	requestJSON, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("ошибка сериализации запроса: %v", err)
	}

	resp, err := c.makeRequest("POST", "/api/iso/delete", requestJSON)
	if err != nil {
		return err
	}

	var apiResp models.APIResponse
	if err := json.Unmarshal(resp, &apiResp); err != nil {
		return fmt.Errorf("ошибка парсинга ответа: %v", err)
	}

	if !apiResp.Success {
		return fmt.Errorf("ошибка удаления образа: %s", apiResp.Message)
	}

	logrus.Infof("✅ Образ успешно удален с устройства")
	return nil
}
