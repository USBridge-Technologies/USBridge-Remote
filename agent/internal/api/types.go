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

// AudioSink describes a real system audio output device Sunshine can be
// pointed at (its "audio_sink" setting) — enumerated live, not a stub.
type AudioSink struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Default     bool   `json:"default"`
}

type DeviceRequest struct {
	Device       string `json:"device"`
	Type         string `json:"type,omitempty"`
	Server       string `json:"server,omitempty"` // iSCSI portal IP
	Port         int    `json:"port,omitempty"`   // iSCSI portal port
	ExportName   string `json:"export_name,omitempty"`
	ReadOnly     bool   `json:"read_only,omitempty"`
	VendorID     string `json:"vendor_id,omitempty"`
	ProductID    string `json:"product_id,omitempty"`
	ProductName  string `json:"product_name,omitempty"`
	Manufacturer string `json:"manufacturer,omitempty"`
	RNDISMode    string `json:"rndis_mode,omitempty"`

	// iSCSI (Transport == "iscsi", the only drive transport today)
	Transport    string `json:"transport,omitempty"`
	TargetIQN    string `json:"target_iqn,omitempty"`
	LUN          int    `json:"lun,omitempty"`
	CHAPUsername string `json:"chap_username,omitempty"`
	CHAPSecret   string `json:"chap_secret,omitempty"`
}

type DeviceStartBatchRequest struct {
	Devices []DeviceRequest `json:"devices"`
}

type DeviceInfo struct {
	ID              int       `json:"id"`
	Device          string    `json:"device"`
	Status          string    `json:"status"`
	VendorID        string    `json:"vendor_id,omitempty"`
	ProductID       string    `json:"product_id,omitempty"`
	ProductName     string    `json:"product_name,omitempty"`
	Manufacturer    string    `json:"manufacturer,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	Server          string    `json:"server,omitempty"`
	Port            int       `json:"port,omitempty"`
	Type            string    `json:"type,omitempty"`
	Name            string    `json:"name,omitempty"`
	Transport       string    `json:"transport,omitempty"`
	LocalDevicePath string    `json:"local_device_path,omitempty"`
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

type VideoCaptureMode struct {
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	FPS         []int  `json:"fps"`
	PixelFormat string `json:"pixel_format"`
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
	Streamer  string        `json:"streamer,omitempty"`
}

type ScreenSnapshot struct {
	Format      string `json:"format"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	ImageBase64 string `json:"image_base64,omitempty"`
	Timestamp   string `json:"timestamp"`
}

// ClipboardEvent is a clipboard-changed notification exchanged over
// /api/clipboard/ws. For Kind=="text", Text carries the content inline
// (small). For Kind=="image"/"file" this frame carries no bytes at all —
// just BlobID + Size — the actual payload moves separately via
// PUT/GET /api/clipboard/blob/{id} so a large transfer never blocks this
// signaling channel behind it.
//
// Pending==true marks a fast, best-effort pre-announcement sent the moment a
// file/directory clipboard change is detected — before the (possibly slow,
// for many/large files) local read+upload has even started. It carries no
// Hash/BlobID (there's nothing to fetch yet) and must not be applied to the
// local clipboard; the real event with a usable BlobID follows once the
// transfer is ready. Its only purpose is letting the peer react instantly
// ("receiving N files...") instead of appearing to hang until the transfer
// finishes.
type ClipboardEvent struct {
	Kind      string `json:"kind"` // "text" | "image" | "file"
	Text      string `json:"text,omitempty"`
	Hash      string `json:"hash"`
	Size      int64  `json:"size,omitempty"`
	FileName  string `json:"file_name,omitempty"`
	MimeType  string `json:"mime_type,omitempty"`
	BlobID    string `json:"blob_id,omitempty"`
	Pending   bool   `json:"pending,omitempty"`
	FileCount int    `json:"file_count,omitempty"`
}
