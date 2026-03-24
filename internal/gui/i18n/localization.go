package i18n

// LocalizedStrings contains all UI text strings
type LocalizedStrings struct {
	// Main Window
	AppTitle         string
	ServerAddress    string
	Token            string
	ConnectButton    string
	DisconnectButton string
	TabDevices       string
	TabControl       string
	TabSnapshots     string

	// Connection Manager
	ConnectionManager         string
	SavedConnections          string
	ConnectionManagement      string
	AddressAndTokenHint       string
	ConnectionHeroEyebrow     string
	ConnectionPanelHint       string
	ConnectionNameLabel       string
	ConnectionNamePlaceholder string
	SaveButton                string
	DeleteButton              string
	EditButton                string
	QRScannerButton           string
	EditConnectionTitle       string
	AddConnectionTitle        string
	AddNewDeviceTitle         string
	NoSavedConnections        string
	NoSavedConnectionsHint    string
	OnboardingStepConnect     string
	OnboardingStepIP          string
	OnboardingStepScan        string
	DeleteConnectionTitle     string
	DeleteConnectionConfirm   string

	// Video Widget
	VideoNotStarted      string
	StartVideoButton     string
	StopVideoButton      string
	FullscreenButton     string
	StartingVideoCapture string
	WaitingServerStart   string
	VideoActive          string
	StoppingVideoCapture string
	VideoStopped         string
	ErrorNoConnection    string
	ErrorWindowNotInit   string

	// Backup Widget
	BackupFlash              string
	CurrentFlashAndSnapshots string
	CurrentFlash             string
	ReadyToWork              string
	LoadingSnapshots         string
	WaitingConnection        string
	ErrorLoadingSnapshots    string
	LoadedSnapshots          string
	MountingFlash            string
	MountingSnapshot         string
	FlashMounted             string
	SnapshotMounted          string
	ErrorMounting            string
	ErrorNotConnected        string
	ErrorFlashNotFound       string
	FreeDeviceSlotRequired   string // Для подключения бэкапа/снапшота освободите одно устройство
	ErrorLoadingLocalDevices string
	ErrorMountingFlashMsg    string
	ErrorMountingSnapshotMsg string
	ErrorStatusFormat        string

	// Snapshot details dialog
	SnapshotDetailsTitle   string // "Снапшот: %s"
	SnapshotDetailsDate    string // "Дата: %s"
	SnapshotDetailsSize    string // "Размер: %s"
	SnapshotChangelogTitle string // "Изменения (changelog)"
	SnapshotChangelogEmpty string // "Changelog отсутствует"
	SnapshotTempFile       string // "временный файл"
	OK                     string // "OK"

	// Changelog operations (btrfs)
	ChangelogOpSnapshot string
	ChangelogOpUtimes   string
	ChangelogOpMkfile   string
	ChangelogOpRename   string
	ChangelogOpTruncate string
	ChangelogOpClone    string
	ChangelogOpChown    string
	ChangelogOpChmod    string

	// Disk Widget
	Devices                string
	AllAvailableDevices    string
	LocalDrives            string
	NetworkDrives          string
	MountButton            string
	MountButtonCompact     string
	UnmountButton          string
	UnmountButtonCompact   string
	AddImageButton         string
	RefreshButton          string
	LoadingFromCloud       string
	CloudFilesDetected     string
	AndroidBuffering       string // Android буферизует облачные файлы
	PreparingToMount       string // Подготовка к монтированию
	MayTake30Seconds       string
	PleaseWait             string
	MaxDevicesReached      string // Выбрано максимум 5 устройств
	UnmountAllConfirm      string // Отключить все подключенные устройства?
	UnmountSelectedConfirm string // Отключить выбранные устройства?
	NoMountedDevices       string // Нет подключенных устройств для размонтирования
	SelectDevicesToMount   string // Выберите устройства для подключения
	StoppingAllDevices     string // Остановка всех устройств...
	StoppingNBDServers     string // Остановка NBD серверов...
	AllDevicesUnmounted    string // Все устройства отключены

	// Status Messages
	StatusConnected    string
	StatusDisconnected string
	StatusConnecting   string
	StatusError        string

	// QR Scanner
	QRCodeScanned           string
	Apply                   string
	Cancel                  string
	ServerAddressLabel      string
	TokenLabel              string
	ScanSuccess             string
	ErrorOpeningFile        string
	ErrorDecodingImage      string
	ErrorProcessingImage    string
	ErrorLaunchingQRScanner string
	ImageNotLoaded          string
	QRCodeNotFound          string
	InvalidQRFormat         string
	HostCannotBeEmpty       string
	QRExampleText           string
	CopyText                string
	TextCopiedToClipboard   string
	TestQRCode              string
	QRCodeForConnection     string
	QRCodeLabel             string
	PointCameraAtQR         string
	QRScanning              string
	ErrorStartingCamera     string

	// Dialogs
	Yes                          string
	No                           string
	Error                        string
	Information                  string
	Confirmation                 string
	Done                         string
	Success                      string
	Close                        string
	VideoSettingsApplied         string
	StoragePermissionRequired    string
	StoragePermissionMessage     string
	StoragePermissionSteps       string
	ErrorSelectingFile           string
	UnsupportedFileFormat        string
	FileAlreadyAdded             string
	SelectDiskImage              string
	DeleteImageTitle             string
	DeleteImageConfirm           string
	DeleteImageFromDeviceConfirm string
	UploadImageTitle             string
	UploadImageConfirm           string
	ImageUploadedSuccess         string
	ImageDeletedSuccess          string
	ErrorOpeningFileForUpload    string
	ErrorUploadingImage          string
	ErrorDeletingImage           string
	ConnectViaLink               string
	DeepLinkError                string
	SAFFilePicker                string
	SAFInstructions              string
	FileSelected                 string
	NBDImageSelectedGB           string
	NBDAllowLAN                  string
	NBDStarted                   string
	NBDStartedInstructions       string
	NBDStopped                   string
	NBDStoppedSuccess            string
	NBDStatusStopped             string
	NBDStatusRunning             string
	NBDStatusError               string
	NBDInstructions              string
	NBDStartFailed               string
	NBDStopError                 string
	ConnectionTitle              string

	// Video Settings/Dialogs
	VideoQualitySettings string
	Width                string
	Height               string
	FPS                  string
	FramesPerSecond      string
	Quality              string
	Bitrate              string
	InvalidWidth         string
	InvalidHeight        string
	InvalidFPS           string
	InvalidQuality       string
	InvalidBitrate       string
	WidthRange           string
	HeightRange          string
	FPSRange             string
	QualityRange         string
	BitrateRange         string

	// Video Start Dialog
	VideoParameters             string
	Resolution                  string
	FrameRate                   string
	StreamMode                  string
	StartVideo                  string
	Starting                    string
	SwitchingDevice             string
	ConnectingRTP               string
	VideoLaunchFailed           string
	CancelVideoStart            string
	CaptureDevice               string
	VideoDevicesNotFound        string
	VideoDeviceEmpty            string
	VideoDeviceSelected         string
	VideoDeviceUnavailable      string
	VideoDeviceCurrent          string
	SettingsAction              string
	FullscreenAction            string
	VideoJPEGRTPHint            string
	VideoRawYUYVHint            string
	VideoModeH264Name           string
	VideoModeH264Description    string
	VideoModeJPEGName           string
	VideoModeJPEGDescription    string
	VideoModeRawYUYVName        string
	VideoModeRawYUYVDescription string

	// GStreamer Status
	GStreamerConnected    string
	GStreamerPaused       string
	GStreamerDisconnected string
	GStreamerEndOfStream  string
	GStreamerError        string
	FramesDropped         string
	LowLatencyMode        string

	// Language
	Language        string
	LanguageEnglish string
	LanguageRussian string

	// Connection names
	ConnectionNumber string // "Connection %d"
	BackupFlashName  string // "Backup Flash"

	// NBD Server (Android)
	NBDServerManagement string // "NBD Server - Management"
	NBDServerForAndroid string // "NBD Server for Android"
	NBDImageNotSelected string // "Image not selected"
	NBDImageSelected    string // "Selected: %s\nSize: %d MB"
	NBDSelectImage      string // "Select image (.iso/.img)"
	NBDStartServer      string // "Start NBD server"
	NBDStopServer       string // "Stop NBD server"
	NBDRefreshStatus    string // "Refresh status"
	NBDListenAddress    string // "Listen address:"

	// Virtual keyboard
	VirtualKeyboard string // "Virtual keyboard"

	// Device names
	DeviceKeyboard    string // "Keyboard"
	DeviceMouse       string // "Mouse"
	DeviceTouch       string // "Touch" (touchscreen mode)
	DeviceAbsolute    string // "Absolute" (absolute pointing mode)
	DeviceNetworkCard string // "Network Card (RNDIS)"

	// Deep link handler
	DeepLinkServerAddress string // "Server address:"
	DeepLinkToken         string // "Token:"
	DeepLinkConnectPrompt string // "Do you want to connect to this server?\n\nChoose action:"
	DeepLinkConnect       string // "Connect"
	DeepLinkSave          string // "Save"

	// Error messages
	ErrorMessage               string // "Error: %v"
	ErrorNotConnectedShort     string // "Error: not connected to USBridge"
	ErrorVideoStart            string // "Error starting video: %v"
	ErrorVideoInfo             string // "Error getting video information"
	VideoWaitingConnection     string // "Waiting for connection..."
	VideoInfoReceived          string // "Video information received"
	VideoInfoUnavailable       string // "Video information unavailable"
	VirtualKeyboardClickToType string // "click to type"
	UnitPx                     string // "px"
	UnitPercent                string // "%"
	UnitKbps                   string // "kbps"
	UnitMbps                   string // "Mbps"
	DeviceRowTemplateName      string // placeholder for device list row
	DeviceRowTemplateStatus    string // placeholder for device list row
	FullscreenWindowTitle      string // "USBridge - Fullscreen"
	SnapshotRowTemplateSize    string // placeholder for snapshot list row
	SnapshotRowTemplateDate    string // placeholder for snapshot list row

	// Time formats
	TimeFormat     string
	DateFormat     string
	DateTimeFormat string

	// PC Panel (Power/Reset buttons, LED indicators)
	PCPanelPowerTitle            string // "Power"
	PCPanelPowerConfirm          string // "Press Power button?"
	PCPanelPowerHoldTime         string // "Hold time (seconds):"
	PCPanelPowerShortPress       string // "Short press"
	PCPanelPowerLongPress        string // "Long press (%d s)"
	PCPanelResetTitle            string // "Reset"
	PCPanelResetConfirm          string // "Press Reset button?"
	PCPanelPowerLED              string // "Power LED"
	PCPanelHDDLED                string // "HDD LED"
	PCPanelLongPressNotSupported string // "Long press: will be supported in future"
}

// EN returns English localization
func EN() *LocalizedStrings {
	return &LocalizedStrings{
		// Main Window
		AppTitle:         "USBridge Client",
		ServerAddress:    "Server Address",
		Token:            "Token",
		ConnectButton:    "Connect",
		DisconnectButton: "Disconnect",
		TabDevices:       "💽 Device",
		TabControl:       "📺 Control",
		TabSnapshots:     "💾 Snapshots",

		// Connection Manager
		ConnectionManager:         "🔌 Connection Manager",
		SavedConnections:          "Connections",
		ConnectionManagement:      "💾 Connection Management",
		AddressAndTokenHint:       "💡 Address and token are entered in the bar above",
		ConnectionHeroEyebrow:     "USBRIDGE ACCESS",
		ConnectionPanelHint:       "Launch a saved profile or create a new one.",
		ConnectionNameLabel:       "Name for saving:",
		ConnectionNamePlaceholder: "Connection name (e.g.: My PC)",
		SaveButton:                "💾 Save",
		DeleteButton:              "Delete",
		EditButton:                "✏️",
		QRScannerButton:           "📷 QR",
		EditConnectionTitle:       "Edit connection",
		AddConnectionTitle:        "Add connection",
		AddNewDeviceTitle:         "Add a new device",
		NoSavedConnections:        "No saved connections yet",
		NoSavedConnectionsHint:    "Use QR or add a connection below to get started.",
		OnboardingStepConnect:     "Plug usb-c into target host for power and hid. Connect video path from target hdmi to capture dongle.",
		OnboardingStepIP:          "Navigate to settings -> internet to connect your network.",
		OnboardingStepScan:        "Navigate to the auth token section and scan the qr code. You can also enter the ip and access token manually.",
		DeleteConnectionTitle:     "Delete connection",
		DeleteConnectionConfirm:   "Are you sure you want to delete connection \"%s\"?",

		// Video Widget
		VideoNotStarted:      "Video not started",
		StartVideoButton:     "▶️",
		StopVideoButton:      "⏹️",
		FullscreenButton:     "🔍",
		StartingVideoCapture: "Starting video capture...",
		WaitingServerStart:   "Waiting for server to start...",
		VideoActive:          "✅ Video capture active",
		StoppingVideoCapture: "Stopping video capture...",
		VideoStopped:         "🛑 Video capture stopped",
		ErrorNoConnection:    "Error: not connected to USBridge",
		ErrorWindowNotInit:   "Error: window not initialized",

		// Backup Widget
		BackupFlash:              "💾 Backup Flash",
		CurrentFlashAndSnapshots: "Current flash and available snapshots",
		CurrentFlash:             "Current Flash",
		ReadyToWork:              "Ready to work",
		LoadingSnapshots:         "Loading snapshots...",
		WaitingConnection:        "Waiting for connection...",
		ErrorLoadingSnapshots:    "Error loading snapshots",
		LoadedSnapshots:          "Loaded %d snapshots",
		MountingFlash:            "Mounting flash %s...",
		MountingSnapshot:         "Mounting snapshot %s...",
		FlashMounted:             "Flash %s mounted",
		SnapshotMounted:          "Snapshot %s mounted",
		ErrorMounting:            "Mounting error: %v",
		ErrorNotConnected:        "not connected to USBridge",
		ErrorFlashNotFound:       "current flash not found",
		FreeDeviceSlotRequired:   "To connect backup or snapshot, free one device slot first (on device screen)",
		ErrorLoadingLocalDevices: "Error loading local devices: %v",
		ErrorMountingFlashMsg:    "Error mounting current flash: %v",
		ErrorMountingSnapshotMsg: "Error mounting snapshot: %v",
		ErrorStatusFormat:        "Error: %v",

		SnapshotDetailsTitle:   "Snapshot: %s",
		SnapshotDetailsDate:    "Date: %s",
		SnapshotDetailsSize:    "Size: %s",
		SnapshotChangelogTitle: "Changes (changelog)",
		SnapshotChangelogEmpty: "Changelog unavailable",
		SnapshotTempFile:       "temporary file",
		OK:                     "OK",

		ChangelogOpSnapshot: "snapshot creation",
		ChangelogOpUtimes:   "time update",
		ChangelogOpMkfile:   "file creation",
		ChangelogOpRename:   "rename",
		ChangelogOpTruncate: "file truncation",
		ChangelogOpClone:    "clone",
		ChangelogOpChown:    "owner change",
		ChangelogOpChmod:    "permissions change",

		// Disk Widget
		Devices:                "💽 Devices",
		AllAvailableDevices:    "All available and connected devices",
		LocalDrives:            "Local Drives",
		NetworkDrives:          "Network Drives",
		MountButton:            "🔌 Mount",
		MountButtonCompact:     "🔌 Mount",
		UnmountButton:          "🔴 Unmount",
		UnmountButtonCompact:   "❌",
		AddImageButton:         "➕",
		RefreshButton:          "🔄",
		LoadingFromCloud:       "Loading from cloud",
		CloudFilesDetected:     "Google Drive files detected among selected files.",
		AndroidBuffering:       "Android is buffering cloud files to the device.",
		PreparingToMount:       "Preparing to mount",
		MayTake30Seconds:       "This may take up to 30 seconds per file.",
		PleaseWait:             "Please wait...",
		MaxDevicesReached:      "Maximum 5 devices can be selected",
		UnmountAllConfirm:      "Unmount all connected devices?",
		UnmountSelectedConfirm: "Unmount selected devices?",
		NoMountedDevices:       "No connected devices to unmount",
		SelectDevicesToMount:   "Select devices to connect",
		StoppingAllDevices:     "Stopping all devices...",
		StoppingNBDServers:     "Stopping NBD servers...",
		AllDevicesUnmounted:    "All devices unmounted",

		// Status Messages
		StatusConnected:    "Connected",
		StatusDisconnected: "Disconnected",
		StatusConnecting:   "Connecting...",
		StatusError:        "Error",

		// QR Scanner
		QRCodeScanned:           "✓ QR code scanned",
		Apply:                   "Apply",
		Cancel:                  "Cancel",
		ServerAddressLabel:      "Server address:",
		TokenLabel:              "Token:",
		ScanSuccess:             "✓ QR code successfully scanned!\n\nCheck the data and press 'Apply' to fill in the fields.",
		ErrorOpeningFile:        "Error opening file: %v",
		ErrorDecodingImage:      "Error decoding image: %v",
		ErrorProcessingImage:    "Error processing image: %v",
		ErrorLaunchingQRScanner: "Error launching QR scanner: %v",
		ImageNotLoaded:          "Image not loaded",
		QRCodeNotFound:          "QR code not found in image.\n\nMake sure that:\n• Image contains QR code\n• QR code is clear and visible\n• Image has sufficient resolution",
		InvalidQRFormat:         "Invalid QR code format.\nExpected: host:token\nReceived: %s",
		HostCannotBeEmpty:       "Host address cannot be empty",
		QRExampleText:           "QR code example:\n\n%s\n\nUse a QR code generator to create an image with this text.",
		CopyText:                "📋 Copy text",
		TextCopiedToClipboard:   "Text copied to clipboard!",
		TestQRCode:              "📱 Test QR code",
		QRCodeForConnection:     "QR code for connection:",
		QRCodeLabel:             "QR code: %s",
		PointCameraAtQR:         "Point the camera at QR code...",
		QRScanning:              "QR code scanning",
		ErrorStartingCamera:     "Failed to start camera: %v",

		// Dialogs
		Yes:                          "Yes",
		No:                           "No",
		Error:                        "Error",
		Information:                  "Information",
		Confirmation:                 "Confirmation",
		Done:                         "Done",
		Success:                      "Success",
		Close:                        "Close",
		VideoSettingsApplied:         "Video quality settings applied",
		StoragePermissionRequired:    "Storage permission required",
		StoragePermissionMessage:     "To select files, storage permission is required.",
		StoragePermissionSteps:       "Please:\n1. Open Android Settings\n2. Apps → USBridge Client\n3. Permissions → Storage\n4. Allow access\n\nThen try again.",
		ErrorSelectingFile:           "Error selecting file: %v",
		UnsupportedFileFormat:        "Unsupported file format. Supported: %s",
		FileAlreadyAdded:             "This file is already in the list",
		SelectDiskImage:              "Select disk image (ISO, IMG, VMDK, VDI)",
		DeleteImageTitle:             "Delete image",
		DeleteImageConfirm:           "Are you sure you want to delete the image:\n%s\n\nThe file on disk will not be deleted.",
		DeleteImageFromDeviceConfirm: "Are you sure you want to delete the image from the device?\n%s\n\nThis action cannot be undone.",
		UploadImageTitle:             "Upload image",
		UploadImageConfirm:           "Do you want to upload the image to the device?\n%s\n\nThis may take some time.",
		ImageUploadedSuccess:         "Image %s successfully uploaded to device",
		ImageDeletedSuccess:          "Image %s successfully deleted from device",
		ErrorOpeningFileForUpload:    "Failed to open file: %v",
		ErrorUploadingImage:          "Error uploading image: %v",
		ErrorDeletingImage:           "Error deleting image: %v",
		ConnectViaLink:               "Connect via link",
		DeepLinkError:                "Error processing link: %v",
		SAFFilePicker:                "SAF File Picker",
		SAFInstructions:              "To select file:\n\n1. Press OK\n2. In Android file manager select .iso/.img file\n3. The app will receive fd and call callback\n\nAfter selecting the file you will see its info here.",
		FileSelected:                 "File selected",
		NBDImageSelectedGB:           "Image: %s\nSize: %.2f GB",
		NBDAllowLAN:                  "Allow LAN access (0.0.0.0)",
		NBDStarted:                   "NBD started",
		NBDStartedInstructions:       "NBD server started on %s\n\nConnect from computer:\nsudo nbd-client PHONE_IP 10809 /dev/nbd0 -read-only\n\nPhone IP can be found in: Settings → Network → Wi-Fi",
		NBDStopped:                   "NBD stopped",
		NBDStoppedSuccess:            "NBD server successfully stopped",
		NBDStatusStopped:             "Status: Stopped",
		NBDStatusRunning:             "Status: Running on %s",
		NBDStatusError:               "Status: Error - %s",
		NBDInstructions:              "Instructions:\n\n1. Select image file (.iso/.img) from microSD via SAF\n2. Configure address (default 127.0.0.1:10809)\n3. Press 'Start NBD server'\n4. On computer connect:\n   sudo nbd-client PHONE_IP 10809 /dev/nbd0 -read-only\n\nSecurity:\n• Default: local access only (127.0.0.1)\n• For network access enable 'LAN mode'\n• Image is mounted read-only\n\nServer runs in background even when screen is off",
		NBDStartFailed:               "Failed to start NBD backend: %v",
		NBDStopError:                 "Error stopping: %v",
		ConnectionTitle:              "Connection",

		// Video Settings/Dialogs
		VideoQualitySettings: "Video Quality Settings",
		Width:                "Width:",
		Height:               "Height:",
		FPS:                  "FPS:",
		FramesPerSecond:      "frames/sec",
		Quality:              "Quality:",
		Bitrate:              "Bitrate:",
		InvalidWidth:         "invalid width value: %v",
		InvalidHeight:        "invalid height value: %v",
		InvalidFPS:           "invalid FPS value: %v",
		InvalidQuality:       "invalid quality value: %v",
		InvalidBitrate:       "invalid bitrate value: %v",
		WidthRange:           "width must be from 320 to 1920 pixels",
		HeightRange:          "height must be from 240 to 1080 pixels",
		FPSRange:             "FPS must be from 1 to 60",
		QualityRange:         "quality must be from 1 to 100 percent",
		BitrateRange:         "bitrate must be from 100 to 10000 kbps",

		// Video Start Dialog
		VideoParameters:             "Video Parameters",
		Resolution:                  "Resolution",
		FrameRate:                   "Frame Rate",
		StreamMode:                  "Streaming Mode",
		StartVideo:                  "Start",
		Starting:                    "Starting...",
		SwitchingDevice:             "Switching device...",
		ConnectingRTP:               "Connecting to RTP/UDP (%d/%d)...",
		VideoLaunchFailed:           "Failed to connect after %d attempts",
		CancelVideoStart:            "Cancel video start",
		CaptureDevice:               "Capture device",
		VideoDevicesNotFound:        "Capture video devices not found",
		VideoDeviceEmpty:            "Video device is empty",
		VideoDeviceSelected:         "Selected",
		VideoDeviceUnavailable:      "Unavailable",
		VideoDeviceCurrent:          "Current",
		SettingsAction:              "Settings",
		FullscreenAction:            "Fullscreen",
		VideoJPEGRTPHint:            "JPEG RTP: for MJPEG sources the server forwards JPEG directly; for YUYV sources it encodes JPEG before sending.",
		VideoRawYUYVHint:            "RAW YUYV: uncompressed video over RTP. Very high bandwidth, use only on fast local links.",
		VideoModeH264Name:           "H.264",
		VideoModeH264Description:    "Capture -> H.264 encode -> RTP/UDP. Best compatibility and bandwidth.",
		VideoModeJPEGName:           "JPEG RTP",
		VideoModeJPEGDescription:    "JPEG over RTP. Direct passthrough for MJPEG sources, server-side JPEG encode for YUYV sources.",
		VideoModeRawYUYVName:        "RAW YUYV",
		VideoModeRawYUYVDescription: "Uncompressed YUYV over RTP with direct client rendering. Highest bandwidth, lowest processing.",

		// GStreamer Status
		GStreamerConnected:    "GStreamer RTP/UDP connection established",
		GStreamerPaused:       "GStreamer paused",
		GStreamerDisconnected: "GStreamer disconnected",
		GStreamerEndOfStream:  "GStreamer: end of stream",
		GStreamerError:        "GStreamer error: %v",
		FramesDropped:         "Dropped",
		LowLatencyMode:        "Low latency mode",

		// Language
		Language:        "Language",
		LanguageEnglish: "English",
		LanguageRussian: "Russian",

		// Connection names
		ConnectionNumber: "Connection %d",
		BackupFlashName:  "Backup Flash",

		// NBD Server (Android)
		NBDServerManagement: "NBD Server - Management",
		NBDServerForAndroid: "NBD Server for Android",
		NBDImageNotSelected: "Image not selected",
		NBDImageSelected:    "Selected: %s\nSize: %d MB",
		NBDSelectImage:      "Select image (.iso/.img)",
		NBDStartServer:      "Start NBD server",
		NBDStopServer:       "Stop NBD server",
		NBDRefreshStatus:    "Refresh status",
		NBDListenAddress:    "Listen address:",

		// Virtual keyboard
		VirtualKeyboard: "Virtual keyboard",

		// Device names
		DeviceKeyboard:    "Keyboard",
		DeviceMouse:       "TouchPad",
		DeviceTouch:       "TouchScreen",
		DeviceAbsolute:    "Absolute",
		DeviceNetworkCard: "Network Card (RNDIS)",

		// Deep link handler
		DeepLinkServerAddress: "Server address:",
		DeepLinkToken:         "Token:",
		DeepLinkConnectPrompt: "Do you want to connect to this server?\n\nChoose action:",
		DeepLinkConnect:       "Connect",
		DeepLinkSave:          "Save",

		// Error messages
		ErrorMessage:               "Error: %v",
		ErrorNotConnectedShort:     "Error: not connected to USBridge",
		ErrorVideoStart:            "Error starting video: %v",
		ErrorVideoInfo:             "Error getting video information",
		VideoWaitingConnection:     "Waiting for connection...",
		VideoInfoReceived:          "Video information received",
		VideoInfoUnavailable:       "Video information unavailable",
		VirtualKeyboardClickToType: "click to type",
		UnitPx:                     "px",
		UnitPercent:                "%",
		UnitKbps:                   "kbps",
		UnitMbps:                   "Mbps",
		DeviceRowTemplateName:      "—",
		DeviceRowTemplateStatus:    "—",
		FullscreenWindowTitle:      "USBridge - Fullscreen",
		SnapshotRowTemplateSize:    "—",
		SnapshotRowTemplateDate:    "—",

		// Time formats
		TimeFormat:     "15:04:05",
		DateFormat:     "02.01.2006",
		DateTimeFormat: "02.01.2006 15:04",

		// PC Panel
		PCPanelPowerTitle:            "Power",
		PCPanelPowerConfirm:          "Press Power button?",
		PCPanelPowerHoldTime:         "Hold time (seconds):",
		PCPanelPowerShortPress:       "Short press",
		PCPanelPowerLongPress:        "Long press (%d s)",
		PCPanelResetTitle:            "Reset",
		PCPanelResetConfirm:          "Press Reset button?",
		PCPanelPowerLED:              "Power LED",
		PCPanelHDDLED:                "HDD LED",
		PCPanelLongPressNotSupported: "Long press: will be supported in future",
	}
}

// RU returns the Russian locale.
// The product requirement is that visible UI text must stay in English,
// so the Russian locale currently reuses the English strings.
func RU() *LocalizedStrings {
	return EN()
}

// Current holds the current active localization
var Current *LocalizedStrings

// Init initializes the localization system
func Init(language string) {
	switch language {
	case "ru", "RU":
		Current = RU()
	default:
		Current = EN()
	}
}

// SetLanguage changes the current language
func SetLanguage(language string) {
	Init(language)
}
