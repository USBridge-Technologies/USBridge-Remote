package api

import (
	"bytes"
	"context"
	"crypto/tls"
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

// progressReader wraps io.Reader and calls callback on read (for stream upload)
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

// USBClient HTTP client for USBridge 2 API
type USBClient struct {
	baseURL               string
	httpClient            *http.Client
	apiKey                string
	transportErrorHandler func(error)
	cursorUpdateHandler   func(models.CursorState)
	keyboardQueue         chan keyboardRequestTask
	transportErrorMu      sync.Mutex
	transportErrorCount   int
	lastTransportErrorAt  time.Time

	// WebSocket for mouse control
	mouseWS             *websocket.Conn
	mouseWSMutex        sync.Mutex
	mouseWSActive       bool
	mouseWSIntended     bool         // true while we WANT a WS connection
	mouseWSReconnectCh  chan struct{} // closed to stop the reconnect goroutine

	// WebSocket for gamepad control
	gamepadWS            *websocket.Conn
	gamepadWSMutex       sync.Mutex
	gamepadWSActive      bool
	gamepadWSIntended    bool         // true while we WANT a WS connection
	gamepadWSReconnectCh chan struct{} // closed to stop the reconnect goroutine
}

type keyboardRequestTask struct {
	request models.KeyboardRequest
}

// NewUSBClient creates a new USB client
func NewUSBClient(host string, port int, timeout int) *USBClient {
	return NewUSBClientWithHTTPClient(host, port, timeout, &http.Client{
		Timeout: time.Duration(timeout) * time.Second,
	})
}

func NewUSBClientWithHTTPClient(host string, port int, timeout int, httpClient *http.Client) *USBClient {
	if httpClient == nil {
		httpClient = &http.Client{
			Timeout: time.Duration(timeout) * time.Second,
		}
	} else if timeout > 0 && httpClient.Timeout == 0 {
		// Tsnet and other custom clients are passed without a timeout - applying it.
		// Without this, the first request via tsnet on Android can hang infinitely.
		httpClient.Timeout = time.Duration(timeout) * time.Second
	}
	client := &USBClient{
		baseURL:       fmt.Sprintf("http://%s:%d", host, port),
		httpClient:    httpClient,
		keyboardQueue: make(chan keyboardRequestTask, 512),
	}
	go client.runKeyboardWorker()
	return client
}

func (c *USBClient) SetOnTransportError(handler func(error)) {
	c.transportErrorHandler = handler
}

func (c *USBClient) SetCursorUpdateHandler(handler func(models.CursorState)) {
	c.cursorUpdateHandler = handler
}

func (c *USBClient) noteSuccessfulTransportRequest() {
	c.transportErrorMu.Lock()
	c.transportErrorCount = 0
	c.lastTransportErrorAt = time.Time{}
	c.transportErrorMu.Unlock()
}

func (c *USBClient) shouldNotifyTransportError(err error) bool {
	if err == nil || !IsConnectionLostError(err) {
		return false
	}

	c.transportErrorMu.Lock()
	defer c.transportErrorMu.Unlock()

	now := time.Now()
	if !c.lastTransportErrorAt.IsZero() && now.Sub(c.lastTransportErrorAt) > 4*time.Second {
		c.transportErrorCount = 0
	}

	c.transportErrorCount++
	c.lastTransportErrorAt = now

	// Do not break active connection due to a single background HTTP failure.
	return c.transportErrorCount >= 3
}

// GetStatus gets system status
func (c *USBClient) GetStatus() (*models.USBStatus, error) {
	resp, err := c.makeRequest("GET", "/api/status", nil)
	if err != nil {
		return nil, err
	}
	logrus.Infof("📦 [API-ISO-SPACE] raw response: %s", string(resp))

	var status models.USBStatus
	if err := json.Unmarshal(resp, &status); err != nil {
		return nil, fmt.Errorf("failed to parse status: %v", err)
	}

	return &status, nil
}

// GetServiceStatus gets service status
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

// StartDevice starts device (keyboard or device) - legacy API for compatibility
func (c *USBClient) StartDevice(request *models.DeviceStartRequest) (*models.APIResponse, error) {
	// Convert to new array format
	batchRequest := models.DeviceStartBatchRequest{*request}
	return c.StartDevicesBatch(batchRequest)
}

// StartDevicesBatch starts multiple devices via DeviceRequest array (Full Replace)
func (c *USBClient) StartDevicesBatch(requests models.DeviceStartBatchRequest) (*models.APIResponse, error) {
	return c.StartDevicesBatchWithMerge(requests, false)
}

// StartDevicesBatchWithMerge starts multiple devices and optionally merges with currently connected ones
func (c *USBClient) StartDevicesBatchWithMerge(requests models.DeviceStartBatchRequest, merge bool) (*models.APIResponse, error) {
	var payload interface{}
	if merge {
		payload = map[string]interface{}{
			"merge":   true,
			"devices": requests,
		}
	} else {
		// For backward compatibility or if merge=false, either an object or an array can be sent.
		// New API supports object with merge: false
		payload = map[string]interface{}{
			"merge":   false,
			"devices": requests,
		}
	}

	requestJSON, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %v", err)
	}

	url := c.baseURL + "/api/device/start"
	logrus.Infof("🚀 [API-START-DEVICES] POST %s (merge=%v)", url, merge)
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

	// 200 = synchronous success, 202 = Accepted (mounting in background)
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
		logrus.Infof("⏳ [API-START-DEVICES] 202 Accepted: mounting in background, waiting up to 30s...")

		mountTimeout := time.NewTimer(30 * time.Second)
		pollTicker := time.NewTicker(1 * time.Second)
	pollLoop:
		for {
			select {
			case <-mountTimeout.C:
				pollTicker.Stop()
				logrus.Warnf("⚠️ [API-START-DEVICES] Timed out waiting for mount to complete")
				break pollLoop
			case <-pollTicker.C:
				pollCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				info, pollErr := c.GetDeviceInfoWithContext(pollCtx)
				cancel()
				if pollErr != nil {
					logrus.Warnf("⚠️ [API-START-DEVICES] Error polling device info: %v", pollErr)
					continue pollLoop
				}
				if !info.MountInProgress {
					mountTimeout.Stop()
					pollTicker.Stop()
					if info.LastMountError != "" {
						logrus.Errorf("❌ [API-START-DEVICES] Mount error: %s", info.LastMountError)
						return nil, fmt.Errorf("mount error: %s", info.LastMountError)
					}
					logrus.Infof("✅ [API-START-DEVICES] Mount completed successfully")
					break pollLoop
				}
			}
		}
	}

	logrus.Infof("✅ Successfully started %d devices", len(requests))
	return &apiResp, nil
}

// StopDevice stops device by ID - legacy API for compatibility
func (c *USBClient) StopDevice(deviceID int) error {
	// In the new API, all devices are stopped with a single request
	return c.StopAllDevices()
}

// StopAllDevices stops all devices (new API)
func (c *USBClient) StopAllDevices() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return c.StopAllDevicesWithContext(ctx)
}

func (c *USBClient) StopAllDevicesWithContext(ctx context.Context) error {
	logrus.Infof("🛑 Stopping all devices")

	resp, err := c.makeRequestWithContext(ctx, "POST", "/api/device/stop", nil, nil)
	if err != nil {
		logrus.Errorf("❌ [API-STOP] Request failed: %v", err)
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

// GetDeviceInfo gets device information (new API)
func (c *USBClient) GetDeviceInfo() (*models.DeviceInfoResponse, error) {
	return c.GetDeviceInfoWithContext(context.Background())
}

func (c *USBClient) GetDeviceInfoWithContext(ctx context.Context) (*models.DeviceInfoResponse, error) {
	resp, err := c.makeRequestWithContext(ctx, "GET", "/api/device/info", nil, nil)
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

	// Parse data into DeviceInfoResponse
	dataBytes, err := json.Marshal(apiResp.Data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal data: %v", err)
	}

	var deviceInfo models.DeviceInfoResponse
	if err := json.Unmarshal(dataBytes, &deviceInfo); err != nil {
		return nil, fmt.Errorf("failed to parse device information: %v", err)
	}
	for _, d := range deviceInfo.Devices {
		logrus.Debugf("🔎 [API-DEVICE-INFO] device=%s type=%s status=%s name=%s product=%s",
			d.Device, d.Type, d.Status, d.Name, d.ProductName)
	}

	return &deviceInfo, nil
}

// GetLocalDrives gets local devices list (new API)
func (c *USBClient) GetLocalDrives() (*models.LocalDrivesResponse, error) {
	resp, err := c.makeRequest("GET", "/api/device/local_drives", nil)
	if err != nil {
		return nil, err
	}

	var apiResp models.APIResponse
	if err := json.Unmarshal(resp, &apiResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %v", err)
	}

	if !apiResp.Success {
		return nil, fmt.Errorf("failed to get local devices: %s", apiResp.Message)
	}

	// Parse data into LocalDrivesResponse
	dataBytes, err := json.Marshal(apiResp.Data)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize data: %v", err)
	}

	var localDrives models.LocalDrivesResponse
	if err := json.Unmarshal(dataBytes, &localDrives); err != nil {
		return nil, fmt.Errorf("failed to parse local devices: %v", err)
	}

	return &localDrives, nil
}

// GetISOSpace gets SD card space info (btrfs partition iso/data/backup)
func (c *USBClient) GetISOSpace() (*models.ISOSpaceInfo, error) {
	resp, err := c.makeRequest("GET", "/api/iso/space", nil)
	if err != nil {
		return nil, err
	}

	var apiResp models.APIResponse
	if err := json.Unmarshal(resp, &apiResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %v", err)
	}

	if !apiResp.Success {
		return nil, fmt.Errorf("failed to get space info: %s", apiResp.Message)
	}

	dataBytes, err := json.Marshal(apiResp.Data)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize data: %v", err)
	}

	var spaceInfo models.ISOSpaceInfo
	if err := json.Unmarshal(dataBytes, &spaceInfo); err != nil {
		return nil, fmt.Errorf("failed to parse space info: %v", err)
	}

	logrus.Infof("📦 [API-ISO-SPACE] parsed total=%d available=%d used=%d used_pct=%.2f dir=%s",
		spaceInfo.TotalSpace,
		spaceInfo.AvailableSpace,
		spaceInfo.UsedSpace,
		spaceInfo.UsedPercent,
		spaceInfo.ISODirectory,
	)
	return &spaceInfo, nil
}

// GetDeviceStatus gets device status (new API)
func (c *USBClient) GetDeviceStatus() (*models.DeviceStatusResponse, error) {
	resp, err := c.makeRequest("GET", "/api/device/status", nil)
	if err != nil {
		return nil, err
	}

	var apiResp models.APIResponse
	if err := json.Unmarshal(resp, &apiResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %v", err)
	}

	if !apiResp.Success {
		return nil, fmt.Errorf("failed to get device status: %s", apiResp.Message)
	}

	// Parse data into DeviceStatusResponse
	dataBytes, err := json.Marshal(apiResp.Data)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize data: %v", err)
	}

	var deviceStatus models.DeviceStatusResponse
	if err := json.Unmarshal(dataBytes, &deviceStatus); err != nil {
		return nil, fmt.Errorf("failed to parse device status: %v", err)
	}

	return &deviceStatus, nil
}

// StartService starts USBridge 2 service (legacy API for compatibility)
func (c *USBClient) StartService() error {
	return c.startServiceWithRetry(0)
}

// startServiceWithRetry starts service with limited retries
func (c *USBClient) startServiceWithRetry(retryCount int) error {
	const maxRetries = 2

	resp, err := c.makeRequest("POST", "/api/service/start", nil)
	if err != nil {
		return err
	}

	var apiResp models.APIResponse
	if err := json.Unmarshal(resp, &apiResp); err != nil {
		return fmt.Errorf("failed to parse response: %v", err)
	}

	if !apiResp.Success {
		// Check if error is \"device or resource busy\"
		if (strings.Contains(apiResp.Message, "device or resource busy") ||
			strings.Contains(apiResp.Error, "device or resource busy") ||
			strings.Contains(apiResp.Message, "UDC") ||
			strings.Contains(apiResp.Error, "UDC")) && retryCount < maxRetries {

			logrus.Warnf("⚠️ USB gadget is busy, recovery attempt #%d/%d...", retryCount+1, maxRetries)

			// Forcefully disconnect USB gadget
			if disconnectErr := c.ForceDisconnectUSBGadget(); disconnectErr != nil {
				logrus.Warnf("⚠️ Force disconnect USB gadget error: %v", disconnectErr)
			}

			// Stopping service
			if stopErr := c.StopService(); stopErr != nil {
				logrus.Warnf("⚠️ Stop service error: %v", stopErr)
			}

			// Wait a bit
			time.Sleep(3 * time.Second)

			// Trying to start again
			logrus.Info("🔄 Retrying service start...")
			return c.startServiceWithRetry(retryCount + 1)
		}
		return fmt.Errorf("failed to start service: %s", apiResp.Message)
	}

	logrus.Info("✅ USBridge 2 service started")
	return nil
}

// StopService stops USBridge 2 service
func (c *USBClient) StopService() error {
	resp, err := c.makeRequest("POST", "/api/service/stop", nil)
	if err != nil {
		errText := strings.ToLower(err.Error())
		if strings.Contains(errText, "http error 404") || strings.Contains(errText, "404 page not found") {
			logrus.Warn("⚠️ StopService endpoint is not available on the server (HTTP 404), skipping")
			return nil
		}
		return err
	}

	var apiResp models.APIResponse
	if err := json.Unmarshal(resp, &apiResp); err != nil {
		return fmt.Errorf("failed to parse response: %v", err)
	}

	if !apiResp.Success {
		return fmt.Errorf("failed to stop service: %s", apiResp.Message)
	}

	logrus.Info("🛑 USBridge 2 service stopped")
	return nil
}

// ForceDisconnectUSBGadget forcefully disconnects USB gadget
func (c *USBClient) ForceDisconnectUSBGadget() error {
	logrus.Info("🔧 Forcefully disconnecting USB gadget...")

	resp, err := c.makeRequest("POST", "/api/usb/disconnect", nil)
	if err != nil {
		return err
	}

	var apiResp models.APIResponse
	if err := json.Unmarshal(resp, &apiResp); err != nil {
		return fmt.Errorf("failed to parse response: %v", err)
	}

	if !apiResp.Success {
		logrus.Warnf("⚠️ Force disconnect USB gadget error: %s", apiResp.Message)
		// Do not return error, as this might be normal
	}

	logrus.Info("✅ USB gadget forcefully disconnected")
	return nil
}

// RestartService restarts USBridge 2 service
func (c *USBClient) RestartService() error {
	resp, err := c.makeRequest("POST", "/api/service/restart", nil)
	if err != nil {
		return err
	}

	var apiResp models.APIResponse
	if err := json.Unmarshal(resp, &apiResp); err != nil {
		return fmt.Errorf("failed to parse response: %v", err)
	}

	if !apiResp.Success {
		return fmt.Errorf("failed to restart service: %s", apiResp.Message)
	}

	logrus.Info("🔄 USBridge 2 service restarted")
	return nil
}

// GetDevices gets device list
func (c *USBClient) GetDevices() (*models.APIResponse, error) {
	resp, err := c.makeRequest("GET", "/api/devices", nil)
	if err != nil {
		return nil, err
	}

	var apiResp models.APIResponse
	if err := json.Unmarshal(resp, &apiResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %v", err)
	}

	return &apiResp, nil
}

// GetSystemDevices gets bridge system devices list from /api/devices.
func (c *USBClient) GetSystemDevices() ([]models.SystemDevice, error) {
	apiResp, err := c.GetDevices()
	if err != nil {
		return nil, err
	}

	dataBytes, err := json.Marshal(apiResp.Data)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize device list: %v", err)
	}

	var devices []models.SystemDevice
	if err := json.Unmarshal(dataBytes, &devices); err != nil {
		return nil, fmt.Errorf("failed to parse device list: %v", err)
	}

	return devices, nil
}

// GetConfig gets configuration
func (c *USBClient) GetConfig() (*models.APIResponse, error) {
	resp, err := c.makeRequest("GET", "/api/config", nil)
	if err != nil {
		return nil, err
	}

	var apiResp models.APIResponse
	if err := json.Unmarshal(resp, &apiResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %v", err)
	}

	return &apiResp, nil
}

// UpdateConfig updates configuration
func (c *USBClient) UpdateConfig(config *models.ConfigRequest) error {
	configJSON, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to serialize configuration: %v", err)
	}

	resp, err := c.makeRequest("POST", "/api/config", configJSON)
	if err != nil {
		return err
	}

	var apiResp models.APIResponse
	if err := json.Unmarshal(resp, &apiResp); err != nil {
		return fmt.Errorf("failed to parse response: %v", err)
	}

	if !apiResp.Success {
		return fmt.Errorf("failed to update configuration: %s", apiResp.Message)
	}

	logrus.Info("✅ USBridge 2 configuration updated")
	return nil
}

// SendKey sends key
func (c *USBClient) SendKey(keyCode int) error {
	request := models.KeyboardRequest{
		Action:  "key",
		KeyCode: keyCode,
	}

	return c.sendKeyboardRequest(request)
}

// SendCombo sends key combo
func (c *USBClient) SendCombo(modifiers int, keyCode int) error {
	request := models.KeyboardRequest{
		Action:    "combo",
		Modifiers: modifiers,
		KeyCode:   keyCode,
	}

	return c.sendKeyboardRequest(request)
}

// SendText sends text
func (c *USBClient) SendText(text string) error {
	request := models.KeyboardRequest{
		Action: "text",
		Text:   text,
	}

	return c.sendKeyboardRequest(request)
}

// sendKeyboardRequest sends keyboard request
func (c *USBClient) sendKeyboardRequest(request models.KeyboardRequest) error {
	if c.keyboardQueue == nil {
		go c.doSendKeyboardRequest(request)
		return nil
	}

	select {
	case c.keyboardQueue <- keyboardRequestTask{request: request}:
		// Successfully queued
	default:
		logrus.Warn("⌨️ Keyboard queue is full, dropping key request")
	}
	return nil
}

func (c *USBClient) runKeyboardWorker() {
	for task := range c.keyboardQueue {
		err := c.doSendKeyboardRequest(task.request)
		if err != nil {
			logrus.Errorf("⌨️ Failed to send key: %v", err)
		}
	}
}

func (c *USBClient) doSendKeyboardRequest(request models.KeyboardRequest) error {
	requestJSON, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("failed to serialize request: %v", err)
	}

	resp, err := c.makeRequest("POST", "/api/keyboard", requestJSON)
	if err != nil {
		return err
	}

	var apiResp models.APIResponse
	if err := json.Unmarshal(resp, &apiResp); err != nil {
		return fmt.Errorf("failed to parse response: %v", err)
	}

	if !apiResp.Success {
		return fmt.Errorf("failed to send command: %s", apiResp.Message)
	}

	return nil
}

// SendMouseMove sends relative mouse movement (touchpad)
func (c *USBClient) SendMouseMove(dx, dy int) error {
	request := models.MouseRequest{
		Action: "move",
		DX:     dx,
		DY:     dy,
	}
	return c.sendMouseRequest(request)
}

// SendTouch sends touchscreen touch (action: \"touch\").
// x, y — absolute coordinates 0..32767; tip: true = touch, false = release.
func (c *USBClient) SendTouch(x, y int, tip bool) error {
	action := "released"
	if tip {
		action = "pressed"
	}
	logrus.Infof("🖐️ [Touch] sent: x=%d y=%d %s", x, y, action)
	request := models.MouseRequest{
		Action: "touch",
		X:      intPtr(x),
		Y:      intPtr(y),
		Tip:    tip,
	}
	return c.sendMouseRequest(request)
}

// SendTouchPositionOnly sends only touch position without left button emulation (action: \"touch_position\").
// Used for right button in touch mode: touch position, separate button 2 click.
func (c *USBClient) SendTouchPositionOnly(x, y int, tip bool) error {
	request := models.MouseRequest{
		Action: "touch_position",
		X:      intPtr(x),
		Y:      intPtr(y),
		Tip:    tip,
	}
	return c.sendMouseRequest(request)
}

// SendMouseClick sends mouse click
func (c *USBClient) SendMouseClick(button int) error {
	request := models.MouseRequest{
		Action: "click",
		Button: button,
	}
	return c.sendMouseRequest(request)
}

// SendMouseScroll sends mouse scroll
func (c *USBClient) SendMouseScroll(scroll int) error {
	request := models.MouseRequest{
		Action: "scroll",
		Scroll: scroll,
	}
	return c.sendMouseRequest(request)
}

// SendAbsoluteEvent sends atomic absolute event (position + buttons + scroll).
func (c *USBClient) SendAbsoluteEvent(x, y int, buttons uint8, scroll int) error {
	if scroll > 127 {
		scroll = 127
	} else if scroll < -127 {
		scroll = -127
	}
	request := models.MouseRequest{
		Action:      "absolute_event",
		X:           intPtr(x),
		Y:           intPtr(y),
		ButtonState: int(buttons),
		Scroll:      scroll,
	}
	return c.sendMouseRequest(request)
}

func intPtr(v int) *int {
	return &v
}

// SendMouseAction sends complex mouse action
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

// sendMouseRequest sends mouse request via WebSocket or HTTP
func (c *USBClient) sendMouseRequest(request models.MouseRequest) error {
	// Trying to send via WebSocket
	c.mouseWSMutex.Lock()
	if c.mouseWSActive && c.mouseWS != nil {
		err := c.mouseWS.WriteJSON(request)
		c.mouseWSMutex.Unlock()
		if err == nil {
			return nil
		}
		// If WebSocket error, close connection
		logrus.Warnf("⚠️ WebSocket error, switching to HTTP: %v", err)
		c.DisconnectMouseWebSocket()
	} else {
		c.mouseWSMutex.Unlock()
	}

	// Fallback to HTTP if WebSocket is inactive or an error occurred
	requestJSON, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("failed to serialize request: %v", err)
	}

	resp, err := c.makeRequest("POST", "/api/mouse", requestJSON)
	if err != nil {
		return err
	}

	var apiResp models.APIResponse
	if err := json.Unmarshal(resp, &apiResp); err != nil {
		return fmt.Errorf("failed to parse response: %v", err)
	}

	if !apiResp.Success {
		return fmt.Errorf("failed to send mouse command: %s", apiResp.Message)
	}
	c.handleMouseResponseData(apiResp.Data)

	return nil
}

// ConnectMouseWebSocket connects to WebSocket for mouse control
func (c *USBClient) ConnectMouseWebSocket() error {
	c.mouseWSMutex.Lock()
	defer c.mouseWSMutex.Unlock()

	// If already connected, do not reconnect
	if c.mouseWS != nil && c.mouseWSActive {
		return nil
	}

	// Close old connection if exists
	if c.mouseWS != nil {
		c.mouseWS.Close()
		c.mouseWS = nil
	}

	// Signal that we want to maintain WS and start background reconnect
	if !c.mouseWSIntended {
		c.mouseWSIntended = true
		ch := make(chan struct{})
		c.mouseWSReconnectCh = ch
		go c.runMouseWSReconnectLoop(ch)
	}

	return c.doConnectMouseWSLocked()
}

// doConnectMouseWSLocked performs one connection attempt. Called under mouseWSMutex.
func (c *USBClient) doConnectMouseWSLocked() error {
	wsURL, err := c.getWebSocketURL("/api/mouse/ws")
	if err != nil {
		return fmt.Errorf("failed to build WebSocket URL: %v", err)
	}

	logrus.Debugf("🔌 Connecting to WebSocket: %s", wsURL)

	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}
	if transport, ok := c.httpClient.Transport.(*http.Transport); ok && transport != nil {
		dialer.Proxy = transport.Proxy
		dialer.NetDialContext = transport.DialContext
		dialer.TLSClientConfig = transport.TLSClientConfig
	}
	if c.httpClient.Transport == nil {
		if transport, ok := http.DefaultTransport.(*http.Transport); ok && transport != nil {
			dialer.Proxy = transport.Proxy
			dialer.NetDialContext = transport.DialContext
			if transport.TLSClientConfig != nil {
				dialer.TLSClientConfig = transport.TLSClientConfig.Clone()
			}
		}
	}
	if dialer.TLSClientConfig == nil && strings.HasPrefix(wsURL, "wss://") {
		dialer.TLSClientConfig = &tls.Config{}
	}

	conn, resp, err := dialer.Dial(wsURL, nil)
	if err != nil {
		if resp != nil {
			return fmt.Errorf("failed to connect to WebSocket: %v (status=%s)", err, resp.Status)
		}
		return fmt.Errorf("failed to connect to WebSocket: %v", err)
	}

	// Server sends PingMessage every ~25s; pong is sent automatically by the library.
	// Set PongHandler to ensure read goroutine does not block on ping.
	conn.SetPongHandler(func(string) error { return nil })

	c.mouseWS = conn
	c.mouseWSActive = true

	go c.readMouseWebSocketResponses()

	logrus.Info("✅ Mouse WebSocket connected")
	return nil
}

// runMouseWSReconnectLoop retries connection on disconnect, while mouseWSIntended == true.
func (c *USBClient) runMouseWSReconnectLoop(stopCh chan struct{}) {
	backoff := 2 * time.Second
	const maxBackoff = 30 * time.Second

	for {
		select {
		case <-stopCh:
			return
		case <-time.After(backoff):
		}

		c.mouseWSMutex.Lock()
		if !c.mouseWSIntended {
			c.mouseWSMutex.Unlock()
			return
		}
		if c.mouseWSActive && c.mouseWS != nil {
			// Already reconnected (possibly by sendMouseRequest)
			c.mouseWSMutex.Unlock()
			backoff = 2 * time.Second
			continue
		}
		logrus.Infof("🔄 Reconnecting mouse WebSocket (backoff=%s)", backoff)
		err := c.doConnectMouseWSLocked()
		c.mouseWSMutex.Unlock()

		if err != nil {
			logrus.Warnf("⚠️ Mouse WebSocket: reconnect failed: %v", err)
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		} else {
			logrus.Info("✅ Mouse WebSocket reconnected")
			backoff = 2 * time.Second
		}
	}
}

// DisconnectMouseWebSocket disconnects from WebSocket and stops auto-reconnect.
func (c *USBClient) DisconnectMouseWebSocket() {
	c.mouseWSMutex.Lock()

	c.mouseWSActive = false
	c.mouseWSIntended = false
	ch := c.mouseWSReconnectCh
	c.mouseWSReconnectCh = nil

	if c.mouseWS != nil {
		c.mouseWS.Close()
		c.mouseWS = nil
		logrus.Info("🔌 Mouse WebSocket disconnected")
	}
	c.mouseWSMutex.Unlock()

	// Stop reconnect goroutine outside mutex
	if ch != nil {
		close(ch)
	}
}

// IsMouseWebSocketActive checks if WebSocket is active
func (c *USBClient) IsMouseWebSocketActive() bool {
	c.mouseWSMutex.Lock()
	defer c.mouseWSMutex.Unlock()
	return c.mouseWSActive && c.mouseWS != nil
}

// Disconnect closes all active connections and cleans up client resources
func (c *USBClient) Disconnect() {
	c.mouseWSMutex.Lock()
	c.mouseWSActive = false
	c.mouseWSIntended = false
	ch := c.mouseWSReconnectCh
	c.mouseWSReconnectCh = nil
	if c.mouseWS != nil {
		c.mouseWS.Close()
		c.mouseWS = nil
	}
	c.mouseWSMutex.Unlock()
	if ch != nil {
		close(ch)
	}

	c.DisconnectGamepadWebSocket()

	// Interrupt keyboard queue
	if c.keyboardQueue != nil {
		// Just close the channel, worker will exit
		// But better not close if it is shared,
		// although we have a new client per connection
	}

	// Make httpClient inactive for future requests
	if c.httpClient != nil {
		c.httpClient.CloseIdleConnections()
	}

	logrus.Info("🔌 USBClient: all connections closed")
}

// ── Gamepad WebSocket ──────────────────────────────────────────────────────────

// ConnectGamepadWebSocket opens the /api/gamepad/ws WebSocket and starts
// the auto-reconnect loop. Idempotent: safe to call when already connected.
func (c *USBClient) ConnectGamepadWebSocket() error {
	c.gamepadWSMutex.Lock()
	defer c.gamepadWSMutex.Unlock()

	if c.gamepadWS != nil && c.gamepadWSActive {
		return nil
	}
	if c.gamepadWS != nil {
		c.gamepadWS.Close()
		c.gamepadWS = nil
	}
	if !c.gamepadWSIntended {
		c.gamepadWSIntended = true
		ch := make(chan struct{})
		c.gamepadWSReconnectCh = ch
		go c.runGamepadWSReconnectLoop(ch)
	}
	return c.doConnectGamepadWSLocked()
}

func (c *USBClient) doConnectGamepadWSLocked() error {
	wsURL, err := c.getWebSocketURL("/api/gamepad/ws")
	if err != nil {
		return fmt.Errorf("gamepad WS URL: %w", err)
	}

	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	if transport, ok := c.httpClient.Transport.(*http.Transport); ok && transport != nil {
		dialer.Proxy = transport.Proxy
		dialer.NetDialContext = transport.DialContext
		dialer.TLSClientConfig = transport.TLSClientConfig
	}
	if dialer.TLSClientConfig == nil && strings.HasPrefix(wsURL, "wss://") {
		dialer.TLSClientConfig = &tls.Config{}
	}

	conn, resp, err := dialer.Dial(wsURL, nil)
	if err != nil {
		if resp != nil {
			return fmt.Errorf("gamepad WS dial: %w (status=%s)", err, resp.Status)
		}
		return fmt.Errorf("gamepad WS dial: %w", err)
	}
	conn.SetPongHandler(func(string) error { return nil })

	c.gamepadWS = conn
	c.gamepadWSActive = true
	go c.readGamepadWSMessages()
	logrus.Info("✅ Gamepad WebSocket connected")
	return nil
}

// runGamepadWSReconnectLoop retries on disconnect while gamepadWSIntended == true.
func (c *USBClient) runGamepadWSReconnectLoop(stopCh chan struct{}) {
	backoff := 2 * time.Second
	const maxBackoff = 30 * time.Second
	for {
		select {
		case <-stopCh:
			return
		case <-time.After(backoff):
		}
		c.gamepadWSMutex.Lock()
		if !c.gamepadWSIntended {
			c.gamepadWSMutex.Unlock()
			return
		}
		if c.gamepadWSActive && c.gamepadWS != nil {
			c.gamepadWSMutex.Unlock()
			backoff = 2 * time.Second
			continue
		}
		logrus.Infof("🔄 Reconnecting gamepad WebSocket (backoff=%s)", backoff)
		err := c.doConnectGamepadWSLocked()
		c.gamepadWSMutex.Unlock()
		if err != nil {
			logrus.Warnf("⚠️ Gamepad WebSocket reconnect failed: %v", err)
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		} else {
			logrus.Info("✅ Gamepad WebSocket reconnected")
			backoff = 2 * time.Second
		}
	}
}

// DisconnectGamepadWebSocket closes the connection and stops auto-reconnect.
func (c *USBClient) DisconnectGamepadWebSocket() {
	c.gamepadWSMutex.Lock()
	c.gamepadWSActive = false
	c.gamepadWSIntended = false
	ch := c.gamepadWSReconnectCh
	c.gamepadWSReconnectCh = nil
	if c.gamepadWS != nil {
		c.gamepadWS.Close()
		c.gamepadWS = nil
		logrus.Info("🔌 Gamepad WebSocket disconnected")
	}
	c.gamepadWSMutex.Unlock()
	if ch != nil {
		close(ch)
	}
}

// SendGamepadReportWS sends one gamepad state frame over the WebSocket.
// Returns false if the WebSocket is not connected (caller may fall back to Moonlight).
func (c *USBClient) SendGamepadReportWS(req models.GamepadRequest) bool {
	c.gamepadWSMutex.Lock()
	if !c.gamepadWSActive || c.gamepadWS == nil {
		c.gamepadWSMutex.Unlock()
		return false
	}
	err := c.gamepadWS.WriteJSON(req)
	c.gamepadWSMutex.Unlock()
	if err != nil {
		logrus.Warnf("⚠️ Gamepad WS write error: %v", err)
		c.DisconnectGamepadWebSocket()
		return false
	}
	return true
}

// readGamepadWSMessages drains server messages and detects disconnects.
func (c *USBClient) readGamepadWSMessages() {
	c.gamepadWSMutex.Lock()
	conn := c.gamepadWS
	c.gamepadWSMutex.Unlock()
	if conn == nil {
		return
	}
	for {
		// Server sends no responses; this loop exists only to detect disconnects.
		if _, _, err := conn.ReadMessage(); err != nil {
			intended := false
			c.gamepadWSMutex.Lock()
			if c.gamepadWS == conn {
				c.gamepadWSActive = false
				c.gamepadWS = nil
				intended = c.gamepadWSIntended
			}
			c.gamepadWSMutex.Unlock()
			conn.Close()
			if intended && !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				logrus.Warnf("⚠️ Gamepad WS read error (will reconnect): %v", err)
			}
			return
		}
	}
}

// readMouseWebSocketResponses reads responses from WebSocket server.
// On disconnect marks connection as inactive; reconnect goroutine will restore it.
func (c *USBClient) readMouseWebSocketResponses() {
	c.mouseWSMutex.Lock()
	conn := c.mouseWS
	c.mouseWSMutex.Unlock()

	if conn == nil {
		return
	}

	for {
		var response models.APIResponse
		err := conn.ReadJSON(&response)
		if err != nil {
			intended := false
			c.mouseWSMutex.Lock()
			if c.mouseWS == conn {
				c.mouseWSActive = false
				c.mouseWS = nil
				intended = c.mouseWSIntended
			}
			c.mouseWSMutex.Unlock()
			conn.Close()

			if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				logrus.Warnf("⚠️ Mouse WebSocket disconnected: %v (reconnect=%v)", err, intended)
			} else {
				logrus.Infof("🔌 Mouse WebSocket closed normally")
			}
			return
		}

		if !response.Success {
			logrus.Warnf("⚠️ WebSocket mouse error: %s", response.Error)
			continue
		}
		c.handleMouseResponseData(response.Data)
	}
}

func (c *USBClient) handleMouseResponseData(data interface{}) {
	if data == nil || c.cursorUpdateHandler == nil {
		return
	}

	raw, err := json.Marshal(data)
	if err != nil {
		return
	}

	var payload models.MouseResponseData
	if err := json.Unmarshal(raw, &payload); err != nil {
		return
	}
	if payload.Cursor == nil {
		return
	}
	logrus.Debugf("[cursor-ws] recv cursor: vis=%v x=%.0f y=%.0f size=%dx%d src=%s",
		payload.Cursor.Visible, payload.Cursor.X, payload.Cursor.Y,
		payload.Cursor.Width, payload.Cursor.Height, payload.Cursor.Source)
	c.cursorUpdateHandler(*payload.Cursor)
}

// getWebSocketURL converts HTTP URL to WebSocket URL
func (c *USBClient) getWebSocketURL(path string) (string, error) {
	parsedURL, err := url.Parse(c.baseURL)
	if err != nil {
		return "", err
	}

	// Change scheme to ws:// or wss://
	scheme := "ws"
	if parsedURL.Scheme == "https" {
		scheme = "wss"
	}

	wsURL := fmt.Sprintf("%s://%s%s", scheme, parsedURL.Host, path)
	return wsURL, nil
}

// GetVideoInfo gets video information
func (c *USBClient) GetVideoInfo() (*models.APIResponse, error) {
	return c.GetVideoInfoForDevice("")
}

func (c *USBClient) GetVideoInfoForDevice(devicePath string) (*models.APIResponse, error) {
	path := "/api/video/info"
	if strings.TrimSpace(devicePath) != "" {
		path = path + "?device=" + url.QueryEscape(strings.TrimSpace(devicePath))
	}

	resp, err := c.makeRequest("GET", path, nil)
	if err != nil {
		return nil, err
	}

	var apiResp models.APIResponse
	if err := json.Unmarshal(resp, &apiResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %v", err)
	}

	return &apiResp, nil
}

func (c *USBClient) GetVideoDevices() ([]models.SystemDevice, error) {
	resp, err := c.makeRequest("GET", "/api/video/devices", nil)
	if err != nil {
		return nil, err
	}

	var apiResp models.APIResponse
	if err := json.Unmarshal(resp, &apiResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %v", err)
	}
	if !apiResp.Success {
		return nil, fmt.Errorf("failed to get video devices: %s", apiResp.Message)
	}

	dataBytes, err := json.Marshal(apiResp.Data)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize video devices list: %v", err)
	}

	var payload struct {
		Devices []models.SystemDevice `json:"devices"`
	}
	if err := json.Unmarshal(dataBytes, &payload); err != nil {
		return nil, fmt.Errorf("failed to parse video devices list: %v", err)
	}

	return payload.Devices, nil
}

// StartVideo starts video streaming with parameters (new API)
func (c *USBClient) StartVideo(request *models.VideoStartRequest) error {
	requestJSON, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("failed to serialize request: %v", err)
	}

	mode := request.VideoMode
	if mode == "" {
		mode = models.VideoModeH264
	}
	logrus.Infof("🎥 [VideoHTTP %s] POST %s/api/video/start device=%s mode=%s size=%dx%d fps=%d bitrate=%s client=%s:%d",
		request.TraceID, c.baseURL, request.VideoDevice, mode, request.VideoWidth, request.VideoHeight, request.VideoFPS, request.VideoBitrate, request.ClientHost, request.ClientPort)
	logrus.Infof("🎥 [VideoHTTP %s] body=%s", request.TraceID, string(requestJSON))

	headers := map[string]string{}
	if strings.TrimSpace(request.TraceID) != "" {
		headers["X-USBridge-Video-Trace"] = request.TraceID
	}
	resp, err := c.makeRequestWithHeaders("POST", "/api/video/start", requestJSON, headers)
	if err != nil {
		return err
	}

	var apiResp models.APIResponse
	if err := json.Unmarshal(resp, &apiResp); err != nil {
		return fmt.Errorf("failed to parse response: %v", err)
	}

	if !apiResp.Success {
		// Check if the error is a message that video is already running
		if apiResp.Message != "" &&
			(strings.Contains(apiResp.Message, "already running") ||
				strings.Contains(apiResp.Message, "already running") ||
				strings.Contains(apiResp.Message, "already started") ||
				strings.Contains(apiResp.Message, "already started")) {
			logrus.Info("🎥 Video streaming is already running")
			return nil
		}
		return fmt.Errorf("failed to start video: %s", apiResp.Message)
	}

	logrus.Info("✅ Video streaming started")
	return nil
}

func (c *USBClient) GetTailscaleStatus() (*models.TailscaleStatus, error) {
	return c.GetTailscaleStatusWithContext(context.Background())
}

func (c *USBClient) GetTailscaleStatusWithContext(ctx context.Context) (*models.TailscaleStatus, error) {
	logrus.Infof("🛰️ [API-TS] GET %s/api/auth/tailscale/status", c.baseURL)
	resp, err := c.makeRequestWithContext(ctx, "GET", "/api/auth/tailscale/status", nil, nil)
	if err != nil {
		return nil, err
	}

	var apiResp models.APIResponse
	if err := json.Unmarshal(resp, &apiResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %v", err)
	}
	if !apiResp.Success {
		return nil, fmt.Errorf("tailscale status failed: %s", apiResp.Message)
	}

	raw, err := json.Marshal(apiResp.Data)
	if err != nil {
		return nil, fmt.Errorf("failed to re-marshal tailscale status: %v", err)
	}
	var parsed models.TailscaleStatus
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("failed to parse tailscale status payload: %v", err)
	}
	logrus.Infof("🛰️ [API-TS] status backend=%s logged_in=%v dns=%s ip=%s auth_url=%t", parsed.Backend, parsed.LoggedIn, parsed.DNSName, parsed.IP4, strings.TrimSpace(parsed.AuthURL) != "")
	return &parsed, nil
}

func (c *USBClient) RegisterTailscale(request *models.TailscaleRegistrationRequest) (*models.TailscaleStatus, error) {
	return c.RegisterTailscaleWithContext(context.Background(), request)
}

func (c *USBClient) RegisterTailscaleWithContext(ctx context.Context, request *models.TailscaleRegistrationRequest) (*models.TailscaleStatus, error) {
	requestJSON, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal tailscale registration request: %v", err)
	}
	logrus.Infof("🛰️ [API-TS] POST %s/api/auth/tailscale/register hostname=%s device_token_len=%d auth_key_len=%d", c.baseURL, request.Hostname, len(strings.TrimSpace(request.DeviceToken)), len(strings.TrimSpace(request.AuthKey)))

	resp, err := c.makeRequestWithContext(ctx, "POST", "/api/auth/tailscale/register", requestJSON, nil)
	if err != nil {
		return nil, err
	}

	var apiResp models.APIResponse
	if err := json.Unmarshal(resp, &apiResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %v", err)
	}
	if !apiResp.Success {
		return nil, fmt.Errorf("tailscale registration failed: %s", apiResp.Message)
	}

	raw, err := json.Marshal(apiResp.Data)
	if err != nil {
		return nil, fmt.Errorf("failed to re-marshal tailscale registration data: %v", err)
	}
	var parsed models.TailscaleStatus
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("failed to parse tailscale registration payload: %v", err)
	}
	logrus.Infof("🛰️ [API-TS] register backend=%s logged_in=%v dns=%s ip=%s auth_url=%t", parsed.Backend, parsed.LoggedIn, parsed.DNSName, parsed.IP4, strings.TrimSpace(parsed.AuthURL) != "")
	return &parsed, nil
}

// StartVideoLegacy starts video streaming (legacy API for compatibility)
func (c *USBClient) StartVideoLegacy() error {
	resp, err := c.makeRequest("POST", "/api/video/start", nil)
	if err != nil {
		return err
	}

	var apiResp models.APIResponse
	if err := json.Unmarshal(resp, &apiResp); err != nil {
		return fmt.Errorf("failed to parse response: %v", err)
	}

	if !apiResp.Success {
		// Check if the error is a message that video is already running
		if apiResp.Message != "" &&
			(strings.Contains(apiResp.Message, "already running") ||
				strings.Contains(apiResp.Message, "already running") ||
				strings.Contains(apiResp.Message, "already started") ||
				strings.Contains(apiResp.Message, "already started")) {
			logrus.Info("🎥 Video streaming is already running")
			return nil
		}
		return fmt.Errorf("failed to start video: %s", apiResp.Message)
	}

	logrus.Info("🎥 Video streaming started")
	return nil
}

// StopVideo stops video streaming (new API)
func (c *USBClient) StopVideo() error {
	logrus.Info("🛑 Stopping video streaming...")

	resp, err := c.makeRequest("POST", "/api/video/stop", nil)
	if err != nil {
		return err
	}

	var apiResp models.APIResponse
	if err := json.Unmarshal(resp, &apiResp); err != nil {
		return fmt.Errorf("failed to parse response: %v", err)
	}

	if !apiResp.Success {
		return fmt.Errorf("failed to stop video: %s", apiResp.Message)
	}

	logrus.Info("✅ Video streaming stopped")
	return nil
}

// StopVideoLegacy stops video streaming (legacy API for compatibility)
func (c *USBClient) StopVideoLegacy() error {
	resp, err := c.makeRequest("POST", "/api/video/stop", nil)
	if err != nil {
		return err
	}

	var apiResp models.APIResponse
	if err := json.Unmarshal(resp, &apiResp); err != nil {
		return fmt.Errorf("failed to parse response: %v", err)
	}

	if !apiResp.Success {
		return fmt.Errorf("failed to stop video: %s", apiResp.Message)
	}

	logrus.Info("🛑 Video streaming stopped")
	return nil
}

// makeRequest makes HTTP request
func (c *USBClient) makeRequest(method, endpoint string, body []byte) ([]byte, error) {
	return c.makeRequestWithContext(context.Background(), method, endpoint, body, nil)
}

func (c *USBClient) makeRequestWithContext(ctx context.Context, method, endpoint string, body []byte, headers map[string]string) ([]byte, error) {
	url := c.baseURL + endpoint

	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("request creation failed: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	for key, value := range headers {
		if strings.TrimSpace(key) == "" || strings.TrimSpace(value) == "" {
			continue
		}
		req.Header.Set(key, value)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		wrappedErr := fmt.Errorf("request failed: %v", err)
		if c.transportErrorHandler != nil && c.shouldNotifyTransportError(wrappedErr) {
			go c.transportErrorHandler(wrappedErr)
		}
		return nil, wrappedErr
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("response read failed: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP error %d: %s", resp.StatusCode, string(respBody))
	}

	c.noteSuccessfulTransportRequest()
	return respBody, nil
}

func (c *USBClient) makeRequestWithHeaders(method, endpoint string, body []byte, headers map[string]string) ([]byte, error) {
	return c.makeRequestWithContext(context.Background(), method, endpoint, body, headers)
}

// makeRequestWithAcceptStatuses makes HTTP request, accepting specified status codes as success
func (c *USBClient) makeRequestWithAcceptStatuses(method, endpoint string, body []byte, acceptStatuses []int) ([]byte, int, error) {
	return c.makeRequestWithAcceptStatusesWithContext(context.Background(), method, endpoint, body, acceptStatuses)
}

func (c *USBClient) makeRequestWithAcceptStatusesWithContext(ctx context.Context, method, endpoint string, body []byte, acceptStatuses []int) ([]byte, int, error) {
	url := c.baseURL + endpoint

	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, 0, fmt.Errorf("request creation failed: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		wrappedErr := fmt.Errorf("request failed: %v", err)
		if c.transportErrorHandler != nil && c.shouldNotifyTransportError(wrappedErr) {
			go c.transportErrorHandler(wrappedErr)
		}
		return nil, 0, wrappedErr
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, fmt.Errorf("response read failed: %v", err)
	}

	for _, code := range acceptStatuses {
		if resp.StatusCode == code {
			c.noteSuccessfulTransportRequest()
			return respBody, resp.StatusCode, nil
		}
	}
	return nil, resp.StatusCode, fmt.Errorf("HTTP error %d: %s", resp.StatusCode, string(respBody))
}

func IsHTTPNotFound(err error) bool {
	if err == nil {
		return false
	}
	errText := strings.ToLower(err.Error())
	return strings.Contains(errText, "http error 404") ||
		strings.Contains(errText, "http error 404") ||
		strings.Contains(errText, "404 page not found")
}

// TestConnection tests connection with USBridge 2
func (c *USBClient) TestConnection() error {
	return c.TestConnectionWithContext(context.Background())
}

func (c *USBClient) TestConnectionWithContext(ctx context.Context) error {
	_, err := c.makeRequestWithContext(ctx, "GET", "/api/healthz", nil, nil)
	if err == nil {
		logrus.Info("✅ Connected to USBridge 2")
		return nil
	}

	// Compatibility with old server without healthz.
	if IsHTTPNotFound(err) {
		// GetDeviceInfo uses makeRequest, which uses makeRequestWithContext(Background)
		// For consistency, we should probably add context to GetDeviceInfo too,
		// but let's just use makeRequestWithContext directly here for the fallback check.
		_, err := c.makeRequestWithContext(ctx, "GET", "/api/device/info", nil, nil)
		if err == nil {
			logrus.Info("✅ Connected to USBridge 2 (fallback)")
			return nil
		}
		return fmt.Errorf("unable to connect to USBridge 2: %v", err)
	}

	return fmt.Errorf("unable to connect to USBridge 2: %v", err)
}

// GetSnapshots gets snapshots list
func (c *USBClient) GetSnapshots() (*models.SnapshotsResponse, error) {
	resp, err := c.makeRequest("POST", "/api/backup/get_snapshots", []byte("{}"))
	if err != nil {
		return nil, err
	}

	var apiResp models.APIResponse
	if err := json.Unmarshal(resp, &apiResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %v", err)
	}

	if !apiResp.Success {
		return nil, fmt.Errorf("failed to get snapshots: %s", apiResp.Message)
	}

	// Parse data into SnapshotsJSONResponse
	dataBytes, err := json.Marshal(apiResp.Data)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize data: %v", err)
	}

	var snapshotsJSON models.SnapshotsJSONResponse
	if err := json.Unmarshal(dataBytes, &snapshotsJSON); err != nil {
		return nil, fmt.Errorf("failed to parse snapshots: %v", err)
	}

	// Convert JSON structure to normal structure with correct time
	snapshots := snapshotsJSON.ToSnapshotsResponse()
	return snapshots, nil
}

// GetBaseURL returns USB client base URL
func (c *USBClient) GetBaseURL() string {
	return c.baseURL
}

// GetPCPanelLeds gets POWER and HDD LEDs state of target computer
func (c *USBClient) GetPCPanelLeds() (*models.PCPanelLedsResponse, error) {
	resp, err := c.makeRequest("GET", "/api/pcpanel/leds", nil)
	if err != nil {
		return nil, err
	}

	var apiResp models.PCPanelLedsResponse
	if err := json.Unmarshal(resp, &apiResp); err != nil {
		return nil, fmt.Errorf("failed to parse response LEDs: %v", err)
	}

	if !apiResp.Success {
		return nil, fmt.Errorf("failed to read LEDs: %s", apiResp.Message)
	}

	return &apiResp, nil
}

// PressPCPanelButton simulates pressing Power or Reset button on target PC
// durationSec — press duration in seconds (0 = short press).
// TODO: when API supports long press, pass durationSec in request
func (c *USBClient) PressPCPanelButton(button string, durationSec int) error {
	if button != "power" && button != "reset" {
		return fmt.Errorf("invalid button: use power or reset")
	}

	req := models.PCPanelButtonRequest{Button: button}
	requestJSON, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to serialize request: %v", err)
	}

	resp, err := c.makeRequest("POST", "/api/pcpanel/button", requestJSON)
	if err != nil {
		return err
	}

	var apiResp models.APIResponse
	if err := json.Unmarshal(resp, &apiResp); err != nil {
		return fmt.Errorf("failed to parse response: %v", err)
	}

	if !apiResp.Success {
		return fmt.Errorf("failed to press button: %s", apiResp.Message)
	}

	_ = durationSec // Placeholder for future long press support
	logrus.Infof("✅ PC Panel: button %s pressed", button)
	return nil
}

// UploadProgressCallback called to update upload progress
type UploadProgressCallback func(percent float64, current, total int64, speed float64, eta time.Duration)

// progressWriter tracks write progress to HTTP connection
type progressWriter struct {
	writer        io.Writer
	total         int64
	current       *int64 // use pointer for atomic operations
	lastLog       time.Time
	lastLogOutput time.Time // when last logged
	startTime     time.Time
	lastCurrent   int64
	callback      UploadProgressCallback
	mu            sync.Mutex
}

func (pw *progressWriter) Write(p []byte) (int, error) {
	n, err := pw.writer.Write(p)
	if n > 0 {
		atomic.AddInt64(pw.current, int64(n))

		// Update progress every 100ms for smooth animation
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

			// Calculate upload speed
			elapsed := now.Sub(pw.startTime).Seconds()
			var speed float64
			if elapsed > 0 {
				speed = float64(current) / elapsed / 1024 / 1024 // MB/s
			}

			// Estimated remaining time
			remaining := pw.total - current
			var eta time.Duration
			if elapsed > 0 && current > 0 && remaining > 0 {
				eta = time.Duration(float64(remaining)/(float64(current)/elapsed)) * time.Second
			}

			// Log progress every second
			pw.mu.Lock()
			shouldLog := now.Sub(pw.lastLogOutput) >= time.Second
			if shouldLog {
				pw.lastLogOutput = now
			}
			pw.lastLog = now
			pw.lastCurrent = current
			pw.mu.Unlock()

			if shouldLog {
				logrus.Infof("📊 Progress: %.1f%% (%.2f MB / %.2f MB) | Speed: %.2f MB/s | Remaining: %v",
					percent,
					float64(current)/1024/1024,
					float64(pw.total)/1024/1024,
					speed,
					eta.Round(time.Second))
			}

			// Call callback to update UI every 100ms
			if pw.callback != nil {
				pw.callback(percent, current, pw.total, speed, eta)
			}
		}
	}

	return n, err
}

// UploadISO uploads ISO image to device
func (c *USBClient) UploadISO(filePath string, fileReader io.Reader) error {
	return c.UploadISOWithProgress(filePath, fileReader, nil)
}

// isRetriableUploadError returns true if upload error can be fixed by retrying.
// FRP tunnel periodically reconnects - on broken pipe request can be retried.
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

func IsConnectionLostError(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "connection refused") ||
		strings.Contains(s, "connection reset") ||
		strings.Contains(s, "no route to host") ||
		strings.Contains(s, "network is unreachable") ||
		strings.Contains(s, "host is down") ||
		strings.Contains(s, "i/o timeout") ||
		strings.Contains(s, "context deadline exceeded") ||
		strings.Contains(s, "use of closed network connection") ||
		errors.Is(err, io.EOF)
}

// computeMultipartSize computes exact multipart body size via real header generation.
// Uses same multipart.Writer - size is guaranteed to match actual request body.
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
	// buf = --{boundary}\r\n + headers + \r\n + \r\n--{boundary}--\r\n (for empty file)
	// With file: same bytes, but instead of empty body - fileSize bytes
	return int64(buf.Len()) + fileSize
}

// UploadISOWithProgress uploads ISO image to device with progress callback.
// Uses streaming - file is not fully loaded into memory, UI does not freeze.
// On broken pipe (FRP tunnel reconnect) automatically retries up to 3 times with 3 sec pause.
func (c *USBClient) UploadISOWithProgress(filePath string, fileReader io.Reader, progressCallback UploadProgressCallback) error {
	logrus.Infof("📤 Uploading ISO image to device: %s", filePath)

	// Determine file size for progress bar
	var fileSize int64
	if f, ok := fileReader.(*os.File); ok {
		if info, err := f.Stat(); err == nil {
			fileSize = info.Size()
			logrus.Infof("📊 File size: %.2f MB", float64(fileSize)/1024/1024)
		}
	}

	// Check if retry is possible on error (needs io.Seeker to reset position)
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
				return fmt.Errorf("cannot retry upload: %w", err)
			}
			logrus.Warnf("🔄 Retrying upload (attempt %d/%d) after error: %v", attempt+1, maxRetries, lastErr)
			time.Sleep(retryDelay)
		}

		lastErr = c.doUploadISOAttempt(filePath, fileReader, fileSize, progressCallback)
		if lastErr == nil {
			logrus.Infof("✅ Image successfully uploaded to device")
			return nil
		}
		if !isRetriableUploadError(lastErr) {
			return lastErr
		}
	}
	return lastErr
}

// doUploadISOAttempt performs one ISO upload attempt.
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
			logrus.Errorf("❌ Failed to create form field: %v", err)
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
			logrus.Errorf("❌ Failed to read file: %v", copyErr)
			pipeWriter.CloseWithError(copyErr)
			return
		}
		logrus.Infof("📊 Read for sending: %.2f MB", float64(written)/1024/1024)

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
		return fmt.Errorf("failed to create request: %v", err)
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
	logrus.Infof("🔍 HTTP REQUEST:")
	logrus.Infof("   Method: %s", req.Method)
	logrus.Infof("   URL: %s", url)
	logrus.Infof("   Headers:")
	for key, values := range req.Header {
		for _, value := range values {
			if key == "Authorization" && value != "" {
				logrus.Infof("     %s: Bearer [HIDDEN]", key)
			} else {
				logrus.Infof("     %s: %s", key, value)
			}
		}
	}
	logrus.Info("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	logrus.Info("⏳ Sending request (streaming mode)...")
	uploadClient := &http.Client{
		Timeout: 3600 * time.Second,
		Transport: &http.Transport{
			ExpectContinueTimeout: 10 * time.Second,
		},
	}
	resp, err := uploadClient.Do(req)
	if err != nil {
		logrus.Errorf("❌ Failed to execute request: %v", err)
		return fmt.Errorf("failed to execute request: %v", err)
	}
	defer resp.Body.Close()

	logrus.Info("✅ Response received")

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		logrus.Errorf("❌ Failed to read response: %v", err)
		return fmt.Errorf("failed to read response: %v", err)
	}

	logrus.Info("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	logrus.Infof("🔍 HTTP RESPONSE:")
	logrus.Infof("   Status: %d %s", resp.StatusCode, resp.Status)
	logrus.Infof("   Headers:")
	for key, values := range resp.Header {
		for _, value := range values {
			logrus.Infof("     %s: %s", key, value)
		}
	}
	logrus.Infof("   Response body (%d bytes):", len(respBody))
	if len(respBody) > 0 {
		logrus.Infof("   %s", string(respBody))
	} else {
		logrus.Info("   [empty]")
	}
	logrus.Info("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP error %d: %s", resp.StatusCode, string(respBody))
	}

	var apiResp models.APIResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return fmt.Errorf("failed to parse response: %v", err)
	}

	if !apiResp.Success {
		return fmt.Errorf("failed to upload image: %s", apiResp.Message)
	}

	return nil
}

// DeleteISO deletes ISO image from device
func (c *USBClient) DeleteISO(filename string) error {
	logrus.Infof("🗑️ Deleting ISO image from device: %s", filename)

	// Prepare request
	request := map[string]string{
		"filename": filename,
	}

	requestJSON, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("failed to serialize request: %v", err)
	}

	resp, err := c.makeRequest("POST", "/api/iso/delete", requestJSON)
	if err != nil {
		return err
	}

	var apiResp models.APIResponse
	if err := json.Unmarshal(resp, &apiResp); err != nil {
		return fmt.Errorf("failed to parse response: %v", err)
	}

	if !apiResp.Success {
		return fmt.Errorf("failed to delete image: %s", apiResp.Message)
	}

	logrus.Infof("✅ Image successfully deleted from device")
	return nil
}

// GetStorageStatus gets detailed storage status (SD card and eMMC)
func (c *USBClient) GetStorageStatus() (*models.StorageStatusData, error) {
	resp, err := c.makeRequest("GET", "/api/storage/status", nil)
	if err != nil {
		return nil, err
	}

	var apiResp models.APIResponse
	if err := json.Unmarshal(resp, &apiResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %v", err)
	}

	if !apiResp.Success {
		return nil, fmt.Errorf("failed to get storage status: %s", apiResp.Message)
	}

	dataBytes, err := json.Marshal(apiResp.Data)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize data: %v", err)
	}

	var storageStatus models.StorageStatusData
	if err := json.Unmarshal(dataBytes, &storageStatus); err != nil {
		return nil, fmt.Errorf("failed to parse storage status: %v", err)
	}

	return &storageStatus, nil
}

// ListScripts lists available Starlark scripts
func (c *USBClient) ListScripts() ([]models.ScriptInfo, error) {
	resp, err := c.makeRequest("GET", "/api/scripts/list", nil)
	if err != nil {
		return nil, err
	}

	var apiResp models.APIResponse
	if err := json.Unmarshal(resp, &apiResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %v", err)
	}

	if !apiResp.Success {
		return nil, fmt.Errorf("failed to list scripts: %s", apiResp.Message)
	}

	dataBytes, err := json.Marshal(apiResp.Data)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize data: %v", err)
	}

	var scripts []models.ScriptInfo
	if err := json.Unmarshal(dataBytes, &scripts); err != nil {
		return nil, fmt.Errorf("failed to parse scripts: %v", err)
	}

	return scripts, nil
}

// RunScript starts a script by path
func (c *USBClient) RunScript(path string) error {
	request := models.ScriptRunRequest{Path: path}
	requestJSON, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("failed to serialize request: %v", err)
	}

	resp, err := c.makeRequest("POST", "/api/scripts/run", requestJSON)
	if err != nil {
		return err
	}

	var apiResp models.APIResponse
	if err := json.Unmarshal(resp, &apiResp); err != nil {
		return fmt.Errorf("failed to parse response: %v", err)
	}

	if !apiResp.Success {
		return fmt.Errorf("failed to run script: %s", apiResp.Message)
	}

	return nil
}

// GetScriptContent gets raw script content
func (c *USBClient) GetScriptContent(path string) (string, error) {
	endpoint := fmt.Sprintf("/api/scripts/read?path=%s", url.QueryEscape(path))
	resp, err := c.makeRequest("GET", endpoint, nil)
	if err != nil {
		return "", err
	}

	var apiResp models.APIResponse
	if err := json.Unmarshal(resp, &apiResp); err != nil {
		return "", fmt.Errorf("failed to parse response: %v", err)
	}

	if !apiResp.Success {
		return "", fmt.Errorf("failed to get script content: %s", apiResp.Message)
	}

	if content, ok := apiResp.Data.(string); ok {
		return content, nil
	}

	// Try to parse from map if it was returned as a structured data
	if dataMap, ok := apiResp.Data.(map[string]interface{}); ok {
		if content, ok := dataMap["content"].(string); ok {
			return content, nil
		}
	}

	return "", fmt.Errorf("invalid script content format")
}

// SaveScript saves script content
func (c *USBClient) SaveScript(path, content string) error {
	request := models.ScriptSaveRequest{Path: path, Content: content}
	requestJSON, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("failed to serialize request: %v", err)
	}

	resp, err := c.makeRequest("POST", "/api/scripts/write", requestJSON)
	if err != nil {
		return err
	}

	var apiResp models.APIResponse
	if err := json.Unmarshal(resp, &apiResp); err != nil {
		return fmt.Errorf("failed to parse response: %v", err)
	}

	if !apiResp.Success {
		return fmt.Errorf("failed to save script: %s", apiResp.Message)
	}

	return nil
}
