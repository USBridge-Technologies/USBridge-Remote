package api

import "time"

type APIResponse struct {
	Success bool        `json:"success"`
	Message string      `json:"message,omitempty"`
	Error   string      `json:"error,omitempty"`
	Details string      `json:"details,omitempty"`
	Data    interface{} `json:"data,omitempty"`
}

type CursorState struct {
	Visible  bool    `json:"visible"`
	X        float64 `json:"x"`
	Y        float64 `json:"y"`
	Width    int     `json:"width,omitempty"`
	Height   int     `json:"height,omitempty"`
	HotspotX int     `json:"hotspot_x,omitempty"`
	HotspotY int     `json:"hotspot_y,omitempty"`
	Source   string  `json:"source,omitempty"`
}

type MouseResponseData struct {
	Cursor *CursorState `json:"cursor,omitempty"`
}

type DeviceRequest struct {
	Device                  string `json:"device"`
	Type                    string `json:"type,omitempty"`
	Server                  string `json:"server,omitempty"`
	Port                    int    `json:"port,omitempty"`
	ExportName              string `json:"export_name,omitempty"`
	NBDHandshakeEmptyExport bool   `json:"nbd_handshake_empty_export,omitempty"`
	ReadOnly                bool   `json:"read_only,omitempty"`
	VendorID                string `json:"vendor_id,omitempty"`
	ProductID               string `json:"product_id,omitempty"`
	ProductName             string `json:"product_name,omitempty"`
	Manufacturer            string `json:"manufacturer,omitempty"`
	RNDISMode               string `json:"rndis_mode,omitempty"`
}

type DeviceStartBatchRequest struct {
	Devices []DeviceRequest `json:"devices"`
}

type LegacyDeviceStartRequest struct {
	Device                  string `json:"device"`
	Type                    string `json:"type,omitempty"`
	Server                  string `json:"server,omitempty"`
	Port                    int    `json:"port,omitempty"`
	ExportName              string `json:"export_name,omitempty"`
	NBDHandshakeEmptyExport bool   `json:"nbd_handshake_empty_export,omitempty"`
	ReadOnly                bool   `json:"read_only,omitempty"`
	VendorID                string `json:"vendor_id,omitempty"`
	ProductID               string `json:"product_id,omitempty"`
	ProductName             string `json:"product_name,omitempty"`
	Manufacturer            string `json:"manufacturer,omitempty"`
	RNDISMode               string `json:"rndis_mode,omitempty"`
}

type DeviceInfo struct {
	ID           int       `json:"id"`
	Device       string    `json:"device"`
	Status       string    `json:"status"`
	VendorID     string    `json:"vendor_id,omitempty"`
	ProductID    string    `json:"product_id,omitempty"`
	ProductName  string    `json:"product_name,omitempty"`
	Manufacturer string    `json:"manufacturer,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	Server       string    `json:"server,omitempty"`
	Port         int       `json:"port,omitempty"`
	Type         string    `json:"type,omitempty"`
	Name         string    `json:"name,omitempty"`
}

type DeviceInfoResponse struct {
	Devices         []DeviceInfo `json:"devices"`
	Count           int          `json:"count"`
	MountInProgress bool         `json:"mount_in_progress,omitempty"`
	LastMountError  string       `json:"last_mount_error,omitempty"`
	AgentOS         string       `json:"agent_os,omitempty"`
	AgentDisplay    string       `json:"agent_display,omitempty"`
}

type MountDriveStatus struct {
	Available         bool     `json:"available"`
	UnmountAvailable  bool     `json:"unmount_available"`
	Version           string   `json:"version,omitempty"`
	ConnectedDevices  []string `json:"connected_devices"`
	KeyboardAvailable bool     `json:"keyboard_available"`
	RNDISAvailable    bool     `json:"rndis_available"`
}

type KeyboardRequest struct {
	Action    string  `json:"action"`
	KeyCode   *uint8  `json:"key_code,omitempty"`
	Modifiers *uint8  `json:"modifiers,omitempty"`
	Text      *string `json:"text,omitempty"`
}

type MouseRequest struct {
	Action      string `json:"action"`
	DX          *int8  `json:"dx,omitempty"`
	DY          *int8  `json:"dy,omitempty"`
	Button      *uint8 `json:"button,omitempty"`
	Scroll      *int8  `json:"scroll,omitempty"`
	X           *int   `json:"x,omitempty"`
	Y           *int   `json:"y,omitempty"`
	Tip         *bool  `json:"tip,omitempty"`
	ButtonState *uint8 `json:"button_state,omitempty"`
}

type VideoStartRequest struct {
	VideoDevice  string `json:"video_device,omitempty"`
	VideoWidth   int    `json:"video_width"`
	VideoHeight  int    `json:"video_height"`
	VideoFPS     int    `json:"video_fps"`
	VideoQuality int    `json:"video_quality"`
	VideoBitrate string `json:"video_bitrate"`
	VideoMode    string `json:"video_mode,omitempty"`
	ClientHost   string `json:"client_host,omitempty"`
	ClientPort   int    `json:"client_port,omitempty"`
	ShowMouse    bool   `json:"show_mouse,omitempty"`
	TraceID      string `json:"-"`
}

type VideoCaptureMode struct {
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	FPS         []int  `json:"fps"`
	PixelFormat string `json:"pixel_format"`
}

type VideoTransportMode struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	Description       string `json:"description"`
	Transport         string `json:"transport"`
	Encoding          string `json:"encoding"`
	ServerDecodesJPEG bool   `json:"server_decodes_jpeg"`
}

type VideoDeviceInfo struct {
	Path           string             `json:"path"`
	Name           string             `json:"name"`
	Bus            string             `json:"bus"`
	Index          int                `json:"index"`
	Connected      bool               `json:"connected"`
	SupportedModes []VideoCaptureMode `json:"supported_modes,omitempty"`
}

type LegacyDeviceInfo struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	Connected   bool   `json:"connected"`
	Description string `json:"description"`
}

type ServiceStatus struct {
	Status    string    `json:"status"`
	Timestamp time.Time `json:"timestamp"`
	Uptime    string    `json:"uptime,omitempty"`
}

type SystemStatus struct {
	Service   ServiceStatus `json:"service"`
	Timestamp time.Time     `json:"timestamp"`
	OS        string        `json:"os,omitempty"`
}

type ScreenSnapshot struct {
	Format      string `json:"format"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	ImageBase64 string `json:"image_base64,omitempty"`
	Timestamp   string `json:"timestamp"`
}
