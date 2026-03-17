package i18n

// LocalizedStrings contains all UI text strings
type LocalizedStrings struct {
	// Main Window
	AppTitle                string
	ServerAddress           string
	Token                   string
	ConnectButton           string
	DisconnectButton        string
	TabDevices              string
	TabControl              string
	TabSnapshots            string

	// Connection Manager
	ConnectionManager       string
	SavedConnections        string
	ConnectionManagement    string
	AddressAndTokenHint     string
	ConnectionNameLabel     string
	ConnectionNamePlaceholder string
	SaveButton              string
	DeleteButton            string
	EditButton              string
	QRScannerButton         string
	EditConnectionTitle     string
	AddConnectionTitle      string
	NoSavedConnections      string
	DeleteConnectionTitle   string
	DeleteConnectionConfirm string

	// Video Widget
	VideoNotStarted         string
	StartVideoButton        string
	StopVideoButton         string
	FullscreenButton        string
	StartingVideoCapture    string
	WaitingServerStart      string
	ConnectingWebRTC        string
	VideoActive             string
	StoppingVideoCapture    string
	VideoStopped            string
	ErrorNoConnection       string
	ErrorWindowNotInit      string

	// Backup Widget
	BackupFlash             string
	CurrentFlashAndSnapshots string
	CurrentFlash            string
	ReadyToWork             string
	LoadingSnapshots        string
	WaitingConnection       string
	ErrorLoadingSnapshots   string
	LoadedSnapshots         string
	MountingFlash           string
	MountingSnapshot        string
	FlashMounted            string
	SnapshotMounted         string
	ErrorMounting           string
	ErrorNotConnected       string
	ErrorFlashNotFound      string
	FreeDeviceSlotRequired  string // Для подключения бэкапа/снапшота освободите одно устройство
	ErrorLoadingLocalDevices string
	ErrorMountingFlashMsg   string
	ErrorMountingSnapshotMsg string
	ErrorStatusFormat       string

	// Snapshot details dialog
	SnapshotDetailsTitle   string // "Снапшот: %s"
	SnapshotDetailsDate    string // "Дата: %s"
	SnapshotDetailsSize   string // "Размер: %s"
	SnapshotChangelogTitle string // "Изменения (changelog)"
	SnapshotChangelogEmpty string // "Changelog отсутствует"
	SnapshotTempFile      string // "временный файл"
	OK                    string // "OK"

	// Changelog operations (btrfs)
	ChangelogOpSnapshot  string
	ChangelogOpUtimes    string
	ChangelogOpMkfile    string
	ChangelogOpRename    string
	ChangelogOpTruncate  string
	ChangelogOpClone     string
	ChangelogOpChown     string
	ChangelogOpChmod     string

	// Disk Widget
	Devices                 string
	AllAvailableDevices     string
	LocalDrives             string
	NetworkDrives           string
	MountButton             string
	MountButtonCompact      string
	UnmountButton           string
	UnmountButtonCompact    string
	AddImageButton          string
	RefreshButton           string
	LoadingFromCloud        string
	CloudFilesDetected      string
	AndroidBuffering        string // Android буферизует облачные файлы
	PreparingToMount        string // Подготовка к монтированию
	MayTake30Seconds        string
	PleaseWait              string
		MaxDevicesReached       string // Выбрано максимум 5 устройств
		UnmountAllConfirm       string // Отключить все подключенные устройства?
		UnmountSelectedConfirm  string // Отключить выбранные устройства?
		NoMountedDevices        string // Нет подключенных устройств для размонтирования
		SelectDevicesToMount    string // Выберите устройства для подключения
		StoppingAllDevices      string // Остановка всех устройств...
		StoppingNBDServers      string // Остановка NBD серверов...
		AllDevicesUnmounted     string // Все устройства отключены

	// Status Messages
	StatusConnected         string
	StatusDisconnected      string
	StatusConnecting        string
	StatusError             string

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
	QRExampleText            string
	CopyText                 string
	TextCopiedToClipboard   string
	TestQRCode              string
	QRCodeForConnection     string
	QRCodeLabel             string
	PointCameraAtQR         string
	QRScanning              string
	ErrorStartingCamera     string

	// Dialogs
	Yes                     string
	No                      string
	Error                   string
	Information             string
	Confirmation            string
	Done                    string
	Success                 string
	Close                   string
	VideoSettingsApplied    string
	StoragePermissionRequired string
	StoragePermissionMessage string
	StoragePermissionSteps  string
	ErrorSelectingFile      string
	UnsupportedFileFormat   string
	FileAlreadyAdded        string
	SelectDiskImage         string
	DeleteImageTitle        string
	DeleteImageConfirm      string
	DeleteImageFromDeviceConfirm string
	UploadImageTitle        string
	UploadImageConfirm      string
	ImageUploadedSuccess    string
	ImageDeletedSuccess     string
	ErrorOpeningFileForUpload string
	ErrorUploadingImage     string
	ErrorDeletingImage      string
	ConnectViaLink          string
	DeepLinkError           string
	SAFFilePicker           string
	SAFInstructions         string
	FileSelected            string
	NBDImageSelectedGB      string
	NBDAllowLAN             string
	NBDStarted              string
	NBDStartedInstructions string
	NBDStopped              string
	NBDStoppedSuccess       string
	NBDStatusStopped        string
	NBDStatusRunning        string
	NBDStatusError          string
	NBDInstructions         string
	NBDStartFailed          string
	NBDStopError            string
	ConnectionTitle         string

	// Video Settings/Dialogs
	VideoQualitySettings    string
	Width                   string
	Height                  string
	FPS                     string
	FramesPerSecond         string
	Quality                 string
	Bitrate                 string
	InvalidWidth            string
	InvalidHeight           string
	InvalidFPS              string
	InvalidQuality          string
	InvalidBitrate          string
	WidthRange              string
	HeightRange             string
	FPSRange                string
	QualityRange            string
	BitrateRange            string

	// Video Start Dialog
	VideoParameters         string
	Resolution              string
	FrameRate               string
	StartVideo              string
	Starting                string
	SwitchingDevice         string
	ConnectingRTSP          string
	VideoLaunchFailed       string
	CancelVideoStart        string

	// GStreamer Status
	GStreamerConnected      string
	GStreamerPaused         string
	GStreamerDisconnected   string
	GStreamerEndOfStream    string
	GStreamerError          string
	FramesDropped           string
	LowLatencyMode          string

	// Language
	Language                string
	LanguageEnglish         string
	LanguageRussian         string

	// Connection names
	ConnectionNumber        string // "Connection %d"
	BackupFlashName         string // "Backup Flash"

	// NBD Server (Android)
	NBDServerManagement     string // "NBD Server - Management"
	NBDServerForAndroid     string // "NBD Server for Android"
	NBDImageNotSelected     string // "Image not selected"
	NBDImageSelected        string // "Selected: %s\nSize: %d MB"
	NBDSelectImage          string // "Select image (.iso/.img)"
	NBDStartServer          string // "Start NBD server"
	NBDStopServer           string // "Stop NBD server"
	NBDRefreshStatus        string // "Refresh status"
	NBDListenAddress        string // "Listen address:"

	// Virtual keyboard
	VirtualKeyboard         string // "Virtual keyboard"

	// Device names
	DeviceKeyboard          string // "Keyboard"
	DeviceMouse             string // "Mouse"
	DeviceTouch             string // "Touch" (touchscreen mode)
	DeviceNetworkCard       string // "Network Card (RNDIS)"

	// Deep link handler
	DeepLinkServerAddress   string // "Server address:"
	DeepLinkToken           string // "Token:"
	DeepLinkConnectPrompt   string // "Do you want to connect to this server?\n\nChoose action:"
	DeepLinkConnect         string // "Connect"
	DeepLinkSave            string // "Save"

	// Error messages
	ErrorMessage            string // "Error: %v"
	ErrorNotConnectedShort  string // "Error: not connected to USB Bridge"
	ErrorVideoStart         string // "Error starting video: %v"
	ErrorVideoInfo          string // "Error getting video information"
	VideoWaitingConnection  string // "Waiting for connection..."
	VideoInfoReceived       string // "Video information received"
	VideoInfoUnavailable    string // "Video information unavailable"
	WebRTCError             string // "WebRTC error: %v"
	WebRTCConnected         string // "WebRTC connection established"
	WebRTCDisconnected      string // "WebRTC connection lost"
	WebRTCFailed            string // "WebRTC connection failed"
	WebRTCStatsFormat       string // "WebRTC: %s | State: %s | Dropped: %d | Low latency: %s"
	FallbackModeTestVideo   string // "Fallback: generating test video..."
	FallbackModeFrame       string // "Fallback: frame %d (WebRTC unavailable)"
	FallbackModeActive      string // "Fallback mode active (WebRTC unavailable)"
	VirtualKeyboardClickToType string // "click to type"
	UnitPx                  string // "px"
	UnitPercent             string // "%"
	UnitKbps                string // "kbps"
	DeviceRowTemplateName   string // placeholder for device list row
	DeviceRowTemplateStatus string // placeholder for device list row
	FullscreenWindowTitle  string // "USB Bridge - Fullscreen"
	SnapshotRowTemplateSize string // placeholder for snapshot list row
	SnapshotRowTemplateDate string // placeholder for snapshot list row

	// Time formats
	TimeFormat              string
	DateFormat              string
	DateTimeFormat          string

	// PC Panel (Power/Reset buttons, LED indicators)
	PCPanelPowerTitle       string // "Power"
	PCPanelPowerConfirm     string // "Press Power button?"
	PCPanelPowerHoldTime    string // "Hold time (seconds):"
	PCPanelPowerShortPress  string // "Short press"
	PCPanelPowerLongPress   string // "Long press (%d s)"
	PCPanelResetTitle       string // "Reset"
	PCPanelResetConfirm     string // "Press Reset button?"
	PCPanelPowerLED         string // "Power LED"
	PCPanelHDDLED           string // "HDD LED"
	PCPanelLongPressNotSupported string // "Long press: will be supported in future"
}

// EN returns English localization
func EN() *LocalizedStrings {
	return &LocalizedStrings{
		// Main Window
		AppTitle:                "USB Bridge Client",
		ServerAddress:           "Server Address",
		Token:                   "Token",
		ConnectButton:           "Connect",
		DisconnectButton:        "Disconnect",
		TabDevices:              "💽 Device",
		TabControl:              "📺 Control",
		TabSnapshots:            "💾 Snapshots",

		// Connection Manager
		ConnectionManager:       "🔌 Connection Manager",
		SavedConnections:        "Saved Connections:",
		ConnectionManagement:    "💾 Connection Management",
		AddressAndTokenHint:     "💡 Address and token are entered in the bar above",
		ConnectionNameLabel:     "Name for saving:",
		ConnectionNamePlaceholder: "Connection name (e.g.: My PC)",
		SaveButton:              "💾 Save",
		DeleteButton:            "🗑️ Delete",
		EditButton:              "✏️",
		QRScannerButton:         "📷 QR",
		EditConnectionTitle:     "Edit connection",
		AddConnectionTitle:      "Add connection",
		NoSavedConnections:      "No saved connections yet",
		DeleteConnectionTitle:   "Delete connection",
		DeleteConnectionConfirm: "Are you sure you want to delete connection \"%s\"?",

		// Video Widget
		VideoNotStarted:         "Video not started",
		StartVideoButton:        "▶️",
		StopVideoButton:         "⏹️",
		FullscreenButton:        "🔍",
		StartingVideoCapture:    "Starting video capture...",
		WaitingServerStart:      "Waiting for server to start...",
		ConnectingWebRTC:        "Connecting to WebRTC (%d/%d)...",
		VideoActive:             "✅ Video capture active",
		StoppingVideoCapture:    "Stopping video capture...",
		VideoStopped:            "🛑 Video capture stopped",
		ErrorNoConnection:       "Error: not connected to USB Bridge",
		ErrorWindowNotInit:      "Error: window not initialized",

		// Backup Widget
		BackupFlash:             "💾 Backup Flash",
		CurrentFlashAndSnapshots: "Current flash and available snapshots",
		CurrentFlash:            "Current Flash",
		ReadyToWork:             "Ready to work",
		LoadingSnapshots:        "Loading snapshots...",
		WaitingConnection:       "Waiting for connection...",
		ErrorLoadingSnapshots:   "Error loading snapshots",
		LoadedSnapshots:         "Loaded %d snapshots",
		MountingFlash:           "Mounting flash %s...",
		MountingSnapshot:        "Mounting snapshot %s...",
		FlashMounted:            "Flash %s mounted",
		SnapshotMounted:         "Snapshot %s mounted",
		ErrorMounting:           "Mounting error: %v",
		ErrorNotConnected:       "not connected to USB Bridge",
		ErrorFlashNotFound:      "current flash not found",
		FreeDeviceSlotRequired:  "To connect backup or snapshot, free one device slot first (on device screen)",
		ErrorLoadingLocalDevices: "Error loading local devices: %v",
		ErrorMountingFlashMsg:   "Error mounting current flash: %v",
		ErrorMountingSnapshotMsg: "Error mounting snapshot: %v",
		ErrorStatusFormat:       "Error: %v",

		SnapshotDetailsTitle:    "Snapshot: %s",
		SnapshotDetailsDate:     "Date: %s",
		SnapshotDetailsSize:     "Size: %s",
		SnapshotChangelogTitle:  "Changes (changelog)",
		SnapshotChangelogEmpty:  "Changelog unavailable",
		SnapshotTempFile:        "temporary file",
		OK:                      "OK",

		ChangelogOpSnapshot:  "snapshot creation",
		ChangelogOpUtimes:    "time update",
		ChangelogOpMkfile:    "file creation",
		ChangelogOpRename:    "rename",
		ChangelogOpTruncate:  "file truncation",
		ChangelogOpClone:     "clone",
		ChangelogOpChown:     "owner change",
		ChangelogOpChmod:     "permissions change",

		// Disk Widget
		Devices:                 "💽 Devices",
		AllAvailableDevices:     "All available and connected devices",
		LocalDrives:             "Local Drives",
		NetworkDrives:           "Network Drives",
		MountButton:             "🔌 Mount",
		MountButtonCompact:      "🔌 Mount",
		UnmountButton:           "🔴 Unmount",
		UnmountButtonCompact:    "❌",
		AddImageButton:          "➕",
		RefreshButton:           "🔄",
		LoadingFromCloud:        "Loading from cloud",
		CloudFilesDetected:      "Google Drive files detected among selected files.",
		AndroidBuffering:        "Android is buffering cloud files to the device.",
		PreparingToMount:        "Preparing to mount",
		MayTake30Seconds:        "This may take up to 30 seconds per file.",
		PleaseWait:              "Please wait...",
		MaxDevicesReached:       "Maximum 5 devices can be selected",
		UnmountAllConfirm:       "Unmount all connected devices?",
		UnmountSelectedConfirm:  "Unmount selected devices?",
		NoMountedDevices:        "No connected devices to unmount",
		SelectDevicesToMount:    "Select devices to connect",
		StoppingAllDevices:      "Stopping all devices...",
		StoppingNBDServers:      "Stopping NBD servers...",
		AllDevicesUnmounted:     "All devices unmounted",

		// Status Messages
		StatusConnected:         "Connected",
		StatusDisconnected:      "Disconnected",
		StatusConnecting:        "Connecting...",
		StatusError:             "Error",

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
		Yes:                     "Yes",
		No:                      "No",
		Error:                   "Error",
		Information:             "Information",
		Confirmation:            "Confirmation",
		Done:                    "Done",
		Success:                 "Success",
		Close:                   "Close",
		VideoSettingsApplied:    "Video quality settings applied",
		StoragePermissionRequired: "Storage permission required",
		StoragePermissionMessage: "To select files, storage permission is required.",
		StoragePermissionSteps:  "Please:\n1. Open Android Settings\n2. Apps → USB Bridge Client\n3. Permissions → Storage\n4. Allow access\n\nThen try again.",
		ErrorSelectingFile:      "Error selecting file: %v",
		UnsupportedFileFormat:   "Unsupported file format. Supported: %s",
		FileAlreadyAdded:        "This file is already in the list",
		SelectDiskImage:         "Select disk image (ISO, IMG, VMDK, VDI)",
		DeleteImageTitle:        "Delete image",
		DeleteImageConfirm:      "Are you sure you want to delete the image:\n%s\n\nThe file on disk will not be deleted.",
		DeleteImageFromDeviceConfirm: "Are you sure you want to delete the image from the device?\n%s\n\nThis action cannot be undone.",
		UploadImageTitle:        "Upload image",
		UploadImageConfirm:      "Do you want to upload the image to the device?\n%s\n\nThis may take some time.",
		ImageUploadedSuccess:    "Image %s successfully uploaded to device",
		ImageDeletedSuccess:     "Image %s successfully deleted from device",
		ErrorOpeningFileForUpload: "Failed to open file: %v",
		ErrorUploadingImage:     "Error uploading image: %v",
		ErrorDeletingImage:      "Error deleting image: %v",
		ConnectViaLink:          "Connect via link",
		DeepLinkError:           "Error processing link: %v",
		SAFFilePicker:           "SAF File Picker",
		SAFInstructions:        "To select file:\n\n1. Press OK\n2. In Android file manager select .iso/.img file\n3. The app will receive fd and call callback\n\nAfter selecting the file you will see its info here.",
		FileSelected:            "File selected",
		NBDImageSelectedGB:      "Image: %s\nSize: %.2f GB",
		NBDAllowLAN:             "Allow LAN access (0.0.0.0)",
		NBDStarted:             "NBD started",
		NBDStartedInstructions: "NBD server started on %s\n\nConnect from computer:\nsudo nbd-client PHONE_IP 10809 /dev/nbd0 -read-only\n\nPhone IP can be found in: Settings → Network → Wi-Fi",
		NBDStopped:             "NBD stopped",
		NBDStoppedSuccess:       "NBD server successfully stopped",
		NBDStatusStopped:       "Status: Stopped",
		NBDStatusRunning:       "Status: Running on %s",
		NBDStatusError:         "Status: Error - %s",
		NBDInstructions:        "Instructions:\n\n1. Select image file (.iso/.img) from microSD via SAF\n2. Configure address (default 127.0.0.1:10809)\n3. Press 'Start NBD server'\n4. On computer connect:\n   sudo nbd-client PHONE_IP 10809 /dev/nbd0 -read-only\n\nSecurity:\n• Default: local access only (127.0.0.1)\n• For network access enable 'LAN mode'\n• Image is mounted read-only\n\nServer runs in background even when screen is off",
		NBDStartFailed:         "Failed to start NBD backend: %v",
		NBDStopError:            "Error stopping: %v",
		ConnectionTitle:         "Connection",

		// Video Settings/Dialogs
		VideoQualitySettings:    "Video Quality Settings",
		Width:                   "Width:",
		Height:                  "Height:",
		FPS:                     "FPS:",
		FramesPerSecond:         "frames/sec",
		Quality:                 "Quality:",
		Bitrate:                 "Bitrate:",
		InvalidWidth:            "invalid width value: %v",
		InvalidHeight:           "invalid height value: %v",
		InvalidFPS:              "invalid FPS value: %v",
		InvalidQuality:          "invalid quality value: %v",
		InvalidBitrate:          "invalid bitrate value: %v",
		WidthRange:              "width must be from 320 to 1920 pixels",
		HeightRange:             "height must be from 240 to 1080 pixels",
		FPSRange:                "FPS must be from 1 to 60",
		QualityRange:            "quality must be from 1 to 100 percent",
		BitrateRange:            "bitrate must be from 100 to 10000 kbps",

		// Video Start Dialog
		VideoParameters:         "Video Parameters",
		Resolution:              "Resolution",
		FrameRate:               "Frame Rate",
		StartVideo:              "Start",
		Starting:                "Starting...",
		SwitchingDevice:         "Switching device...",
		ConnectingRTSP:          "Connecting to RTSP (%d/%d)...",
		VideoLaunchFailed:       "Failed to connect after %d attempts",
		CancelVideoStart:        "Cancel video start",

		// GStreamer Status
		GStreamerConnected:      "GStreamer RTSP connection established",
		GStreamerPaused:         "GStreamer paused",
		GStreamerDisconnected:   "GStreamer disconnected",
		GStreamerEndOfStream:    "GStreamer: end of stream",
		GStreamerError:          "GStreamer error: %v",
		FramesDropped:           "Dropped",
		LowLatencyMode:          "Low latency mode",

		// Language
		Language:                "Language",
		LanguageEnglish:         "English",
		LanguageRussian:         "Русский",

		// Connection names
		ConnectionNumber:        "Connection %d",
		BackupFlashName:         "Backup Flash",

		// NBD Server (Android)
		NBDServerManagement:     "NBD Server - Management",
		NBDServerForAndroid:     "NBD Server for Android",
		NBDImageNotSelected:     "Image not selected",
		NBDImageSelected:        "Selected: %s\nSize: %d MB",
		NBDSelectImage:          "Select image (.iso/.img)",
		NBDStartServer:          "Start NBD server",
		NBDStopServer:           "Stop NBD server",
		NBDRefreshStatus:        "Refresh status",
		NBDListenAddress:        "Listen address:",

		// Virtual keyboard
		VirtualKeyboard:         "Virtual keyboard",

		// Device names
		DeviceKeyboard:          "Keyboard",
		DeviceMouse:             "Mouse",
		DeviceTouch:             "Touch",
		DeviceNetworkCard:       "Network Card (RNDIS)",

		// Deep link handler
		DeepLinkServerAddress:   "Server address:",
		DeepLinkToken:           "Token:",
		DeepLinkConnectPrompt:   "Do you want to connect to this server?\n\nChoose action:",
		DeepLinkConnect:         "Connect",
		DeepLinkSave:            "Save",

		// Error messages
		ErrorMessage:            "Error: %v",
		ErrorNotConnectedShort:  "Error: not connected to USB Bridge",
		ErrorVideoStart:         "Error starting video: %v",
		ErrorVideoInfo:          "Error getting video information",
		VideoWaitingConnection:  "Waiting for connection...",
		VideoInfoReceived:       "Video information received",
		VideoInfoUnavailable:    "Video information unavailable",
		WebRTCError:             "WebRTC error: %v",
		WebRTCConnected:         "WebRTC connection established",
		WebRTCDisconnected:      "WebRTC connection lost",
		WebRTCFailed:            "WebRTC connection failed",
		WebRTCStatsFormat:       "WebRTC: %s | State: %s | Dropped: %d | Low latency: %s",
		FallbackModeTestVideo:   "Fallback: generating test video...",
		FallbackModeFrame:       "Fallback: frame %d (WebRTC unavailable)",
		FallbackModeActive:      "Fallback mode active (WebRTC unavailable)",
		VirtualKeyboardClickToType: "click to type",
		UnitPx:                  "px",
		UnitPercent:             "%",
		UnitKbps:                "kbps",
		DeviceRowTemplateName:   "—",
		DeviceRowTemplateStatus: "—",
		FullscreenWindowTitle:   "USB Bridge - Fullscreen",
		SnapshotRowTemplateSize: "—",
		SnapshotRowTemplateDate: "—",

		// Time formats
		TimeFormat:              "15:04:05",
		DateFormat:              "02.01.2006",
		DateTimeFormat:          "02.01.2006 15:04",

		// PC Panel
		PCPanelPowerTitle:       "Power",
		PCPanelPowerConfirm:     "Press Power button?",
		PCPanelPowerHoldTime:    "Hold time (seconds):",
		PCPanelPowerShortPress:  "Short press",
		PCPanelPowerLongPress:   "Long press (%d s)",
		PCPanelResetTitle:       "Reset",
		PCPanelResetConfirm:     "Press Reset button?",
		PCPanelPowerLED:         "Power LED",
		PCPanelHDDLED:           "HDD LED",
		PCPanelLongPressNotSupported: "Long press: will be supported in future",
	}
}

// RU returns Russian localization
func RU() *LocalizedStrings {
	return &LocalizedStrings{
		// Main Window
		AppTitle:                "USB Bridge Client",
		ServerAddress:           "Адрес сервера",
		Token:                   "Токен",
		ConnectButton:           "Подключиться",
		DisconnectButton:        "Отключиться",
		TabDevices:              "💽 Устройство",
		TabControl:              "📺 Управление",
		TabSnapshots:            "💾 Снапшоты",

		// Connection Manager
		ConnectionManager:       "🔌 Менеджер подключений",
		SavedConnections:        "Сохраненные подключения:",
		ConnectionManagement:    "💾 Управление подключениями",
		AddressAndTokenHint:     "💡 Адрес и токен вводятся в строке сверху",
		ConnectionNameLabel:     "Название для сохранения:",
		ConnectionNamePlaceholder: "Название подключения (например: Мой ПК)",
		SaveButton:              "💾 Сохранить",
		DeleteButton:            "🗑️",
		EditButton:              "✏️",
		QRScannerButton:         "📷 QR",
		EditConnectionTitle:     "Изменить подключение",
		AddConnectionTitle:      "Добавить подключение",
		NoSavedConnections:      "Нет сохранённых подключений",
		DeleteConnectionTitle:   "Удалить подключение",
		DeleteConnectionConfirm: "Вы уверены, что хотите удалить подключение «%s»?",

		// Video Widget
		VideoNotStarted:         "Видео не запущено",
		StartVideoButton:        "▶️",
		StopVideoButton:         "⏹️",
		FullscreenButton:        "🔍",
		StartingVideoCapture:    "Запуск видео захвата...",
		WaitingServerStart:      "Ожидание запуска сервера...",
		ConnectingWebRTC:        "Подключение к WebRTC (%d/%d)...",
		VideoActive:             "✅ Видео захват активен",
		StoppingVideoCapture:    "Остановка видео захвата...",
		VideoStopped:            "🛑 Видео захват остановлен",
		ErrorNoConnection:       "Ошибка: не подключен к USB Bridge",
		ErrorWindowNotInit:      "Ошибка: окно не инициализировано",

		// Backup Widget
		BackupFlash:             "💾 Backup Flash",
		CurrentFlashAndSnapshots: "Актуальная флешка и доступные снапшоты",
		CurrentFlash:            "Актуальная флешка",
		ReadyToWork:             "Готов к работе",
		LoadingSnapshots:        "Загрузка снапшотов...",
		WaitingConnection:       "Ожидание подключения...",
		ErrorLoadingSnapshots:   "Ошибка загрузки снапшотов",
		LoadedSnapshots:         "Загружено %d снапшотов",
		MountingFlash:           "Монтирование флешки %s...",
		MountingSnapshot:        "Монтирование снапшота %s...",
		FlashMounted:            "Флешка %s смонтирована",
		SnapshotMounted:         "Снапшот %s смонтирован",
		ErrorMounting:           "Ошибка монтирования: %v",
		ErrorNotConnected:       "не подключен к USB Bridge",
		ErrorFlashNotFound:      "актуальная флешка не найдена",
		FreeDeviceSlotRequired:  "Для подключения бэкапа или снапшота необходимо освободить одно устройство (на экране устройства)",
		ErrorLoadingLocalDevices: "Ошибка загрузки локальных устройств: %v",
		ErrorMountingFlashMsg:   "ошибка монтирования актуальной флешки: %v",
		ErrorMountingSnapshotMsg: "ошибка монтирования снапшота: %v",
		ErrorStatusFormat:       "Ошибка: %v",

		SnapshotDetailsTitle:    "Снапшот: %s",
		SnapshotDetailsDate:     "Дата: %s",
		SnapshotDetailsSize:     "Размер: %s",
		SnapshotChangelogTitle:  "Изменения (changelog)",
		SnapshotChangelogEmpty:  "Changelog отсутствует",
		SnapshotTempFile:        "временный файл",
		OK:                      "OK",

		ChangelogOpSnapshot:  "создание снапшота",
		ChangelogOpUtimes:    "обновление времени",
		ChangelogOpMkfile:    "создание файла",
		ChangelogOpRename:    "переименование",
		ChangelogOpTruncate:  "обрезка файла",
		ChangelogOpClone:     "клонирование",
		ChangelogOpChown:     "смена владельца",
		ChangelogOpChmod:     "смена прав",

		// Disk Widget
		Devices:                 "💽 Устройства",
		AllAvailableDevices:     "Все доступные и подключенные устройства",
		LocalDrives:             "Локальные диски",
		NetworkDrives:           "Сетевые диски",
		MountButton:             "🔌 Подключить",
		MountButtonCompact:      "🔌 Подключить",
		UnmountButton:           "🔴 Отключить",
		UnmountButtonCompact:    "❌",
		AddImageButton:          "➕",
		RefreshButton:           "🔄",
		LoadingFromCloud:        "Загрузка из облака",
		CloudFilesDetected:      "Среди выбранных файлов есть файлы из Google Drive.",
		AndroidBuffering:        "Android буферизует облачные файлы на устройстве.",
		PreparingToMount:        "Подготовка к монтированию",
		MayTake30Seconds:        "Это может занять до 30 секунд для каждого файла.",
		PleaseWait:              "Пожалуйста, подождите...",
		MaxDevicesReached:       "Можно выбрать не более 5 устройств",
		UnmountAllConfirm:       "Отключить все подключенные устройства?",
		UnmountSelectedConfirm:  "Отключить выбранные устройства?",
		NoMountedDevices:        "Нет подключенных устройств для размонтирования",
		SelectDevicesToMount:    "Выберите устройства для подключения",
		StoppingAllDevices:      "Остановка всех устройств...",
		StoppingNBDServers:      "Остановка NBD серверов...",
		AllDevicesUnmounted:     "Все устройства отключены",

		// Status Messages
		StatusConnected:         "Подключен",
		StatusDisconnected:      "Отключен",
		StatusConnecting:        "Подключение...",
		StatusError:             "Ошибка",

		// QR Scanner
		QRCodeScanned:           "✓ QR-код отсканирован",
		Apply:                   "Применить",
		Cancel:                  "Отмена",
		ServerAddressLabel:      "Адрес сервера:",
		TokenLabel:              "Токен:",
		ScanSuccess:             "✓ QR-код успешно отсканирован!\n\nПроверьте данные и нажмите 'Применить' для заполнения полей.",
		ErrorOpeningFile:        "Ошибка открытия файла: %v",
		ErrorDecodingImage:      "Ошибка декодирования изображения: %v",
		ErrorProcessingImage:    "Ошибка обработки изображения: %v",
		ErrorLaunchingQRScanner: "Ошибка запуска QR-сканера: %v",
		ImageNotLoaded:          "Изображение не загружено",
		QRCodeNotFound:          "QR-код не найден на изображении.\n\nУбедитесь что:\n• Изображение содержит QR-код\n• QR-код четкий и хорошо виден\n• Изображение имеет достаточное разрешение",
		InvalidQRFormat:         "Неверный формат QR-кода.\nОжидается: host:token\nПолучено: %s",
		HostCannotBeEmpty:       "Адрес хоста не может быть пустым",
		QRExampleText:           "Пример QR-кода:\n\n%s\n\nИспользуйте генератор QR-кодов для создания изображения с этим текстом.",
		CopyText:                "📋 Скопировать текст",
		TextCopiedToClipboard:   "Текст скопирован в буфер обмена!",
		TestQRCode:              "📱 Тестовый QR-код",
		QRCodeForConnection:     "QR-код для подключения:",
		QRCodeLabel:             "QR-код: %s",
		PointCameraAtQR:         "Наведите камеру на QR-код...",
		QRScanning:              "Сканирование QR-кода",
		ErrorStartingCamera:     "Не удалось запустить камеру: %v",

		// Dialogs
		Yes:                     "Да",
		No:                      "Нет",
		Error:                   "Ошибка",
		Information:             "Информация",
		Confirmation:            "Подтверждение",
		Done:                    "Готово",
		Success:                 "Успех",
		Close:                   "Закрыть",
		VideoSettingsApplied:    "Настройки качества видео применены",
		StoragePermissionRequired: "Требуется разрешение",
		StoragePermissionMessage: "Для выбора файлов нужно разрешение на хранилище.",
		StoragePermissionSteps:  "Пожалуйста:\n1. Откройте Настройки Android\n2. Приложения → USB Bridge Client\n3. Разрешения → Хранилище\n4. Разрешите доступ\n\nПосле этого попробуйте снова.",
		ErrorSelectingFile:      "Ошибка при выборе файла: %v",
		UnsupportedFileFormat:   "Неподдерживаемый формат файла. Поддерживаются: %s",
		FileAlreadyAdded:        "Этот файл уже добавлен в список",
		SelectDiskImage:         "Выберите образ диска (ISO, IMG, VMDK, VDI)",
		DeleteImageTitle:        "Удалить образ",
		DeleteImageConfirm:      "Вы уверены, что хотите удалить образ:\n%s\n\nФайл на диске не будет удален.",
		DeleteImageFromDeviceConfirm: "Вы уверены, что хотите удалить образ с устройства?\n%s\n\nЭто действие нельзя отменить.",
		UploadImageTitle:        "Загрузить образ",
		UploadImageConfirm:      "Вы хотите загрузить образ на устройство?\n%s\n\nЭто может занять некоторое время.",
		ImageUploadedSuccess:    "Образ %s успешно загружен на устройство",
		ImageDeletedSuccess:     "Образ %s успешно удален с устройства",
		ErrorOpeningFileForUpload: "Не удалось открыть файл: %v",
		ErrorUploadingImage:     "Ошибка загрузки образа: %v",
		ErrorDeletingImage:      "Ошибка удаления образа: %v",
		ConnectViaLink:          "Подключение через ссылку",
		DeepLinkError:           "Ошибка обработки ссылки: %v",
		SAFFilePicker:           "SAF File Picker",
		SAFInstructions:        "Для выбора файла:\n\n1. Нажмите OK\n2. В Android файловом менеджере выберите .iso/.img файл\n3. Приложение получит fd и вызовет callback\n\nПосле выбора файла вы увидите информацию о нем здесь.",
		FileSelected:            "Файл выбран",
		NBDImageSelectedGB:      "Образ: %s\nРазмер: %.2f ГБ",
		NBDAllowLAN:             "Разрешить доступ из LAN (0.0.0.0)",
		NBDStarted:              "NBD запущен",
		NBDStartedInstructions:  "NBD сервер запущен на %s\n\nПодключитесь с компьютера:\nsudo nbd-client PHONE_IP 10809 /dev/nbd0 -read-only\n\nIP телефона можно узнать в: Настройки → Сеть → Wi-Fi",
		NBDStopped:              "NBD остановлен",
		NBDStoppedSuccess:       "NBD сервер успешно остановлен",
		NBDStatusStopped:       "Статус: Остановлен",
		NBDStatusRunning:       "Статус: Работает на %s",
		NBDStatusError:         "Статус: Ошибка - %s",
		NBDInstructions:        "Инструкция:\n\n1. Выберите файл образа (.iso/.img) с microSD через SAF\n2. Настройте адрес (по умолчанию 127.0.0.1:10809)\n3. Нажмите 'Запустить NBD сервер'\n4. На компьютере подключитесь:\n   sudo nbd-client PHONE_IP 10809 /dev/nbd0 -read-only\n\nБезопасность:\n• По умолчанию доступ только локально (127.0.0.1)\n• Для доступа из сети включите 'LAN mode'\n• Образ монтируется read-only\n\nСервер работает в фоне даже при выключенном экране",
		NBDStartFailed:          "Не удалось запустить NBD backend: %v",
		NBDStopError:            "Ошибка остановки: %v",
		ConnectionTitle:         "Подключение",

		// Video Settings/Dialogs
		VideoQualitySettings:    "Настройки качества видео",
		Width:                   "Ширина:",
		Height:                  "Высота:",
		FPS:                     "FPS:",
		FramesPerSecond:         "кадров/сек",
		Quality:                 "Качество:",
		Bitrate:                 "Битрейт:",
		InvalidWidth:            "неверное значение ширины: %v",
		InvalidHeight:           "неверное значение высоты: %v",
		InvalidFPS:              "неверное значение FPS: %v",
		InvalidQuality:          "неверное значение качества: %v",
		InvalidBitrate:          "неверное значение битрейта: %v",
		WidthRange:              "ширина должна быть от 320 до 1920 пикселей",
		HeightRange:             "высота должна быть от 240 до 1080 пикселей",
		FPSRange:                "FPS должен быть от 1 до 60",
		QualityRange:            "качество должно быть от 1 до 100 процентов",
		BitrateRange:            "битрейт должен быть от 100 до 10000 kbps",

		// Video Start Dialog
		VideoParameters:         "Параметры видео",
		Resolution:              "Разрешение",
		FrameRate:               "Частота кадров",
		StartVideo:              "Запустить",
		Starting:                "Запуск...",
		SwitchingDevice:         "Переключение устройства...",
		ConnectingRTSP:          "Подключение к RTSP (%d/%d)...",
		VideoLaunchFailed:       "Не удалось подключиться после %d попыток",
		CancelVideoStart:        "Отмена запуска видео",

		// GStreamer Status
		GStreamerConnected:      "GStreamer RTSP соединение установлено",
		GStreamerPaused:         "GStreamer приостановлен",
		GStreamerDisconnected:   "GStreamer отключен",
		GStreamerEndOfStream:    "GStreamer: конец потока",
		GStreamerError:          "GStreamer ошибка: %v",
		FramesDropped:           "Сброшено",
		LowLatencyMode:          "Низкая задержка",

		// Language
		Language:                "Язык",
		LanguageEnglish:         "English",
		LanguageRussian:         "Русский",

		// Connection names
		ConnectionNumber:        "Подключение %d",
		BackupFlashName:         "Backup Flash",

		// NBD Server (Android)
		NBDServerManagement:     "NBD Сервер - Управление",
		NBDServerForAndroid:     "NBD Server для Android",
		NBDImageNotSelected:     "Образ не выбран",
		NBDImageSelected:        "Выбран: %s\nРазмер: %d МБ",
		NBDSelectImage:          "Выбрать образ (.iso/.img)",
		NBDStartServer:          "Запустить NBD сервер",
		NBDStopServer:           "Остановить NBD сервер",
		NBDRefreshStatus:        "Обновить статус",
		NBDListenAddress:        "Адрес прослушивания:",

		// Virtual keyboard
		VirtualKeyboard:         "Виртуальная клавиатура",

		// Device names
		DeviceKeyboard:          "Клавиатура",
		DeviceMouse:             "Мышь",
		DeviceTouch:             "Тачскрин",
		DeviceNetworkCard:       "Сетевая карта (RNDIS)",

		// Deep link handler
		DeepLinkServerAddress:   "Адрес сервера:",
		DeepLinkToken:           "Токен:",
		DeepLinkConnectPrompt:   "Хотите подключиться к этому серверу?\n\nВыберите действие:",
		DeepLinkConnect:         "Подключиться",
		DeepLinkSave:            "Сохранить",

		// Error messages
		ErrorMessage:            "Ошибка: %v",
		ErrorNotConnectedShort:  "Ошибка: не подключен к USB Bridge",
		ErrorVideoStart:         "Ошибка запуска видео: %v",
		ErrorVideoInfo:          "Ошибка получения информации о видео",
		VideoWaitingConnection:  "Ожидание подключения...",
		VideoInfoReceived:       "Информация о видео получена",
		VideoInfoUnavailable:    "Информация о видео недоступна",
		WebRTCError:             "WebRTC ошибка: %v",
		WebRTCConnected:         "WebRTC соединение установлено",
		WebRTCDisconnected:      "WebRTC соединение потеряно",
		WebRTCFailed:            "WebRTC соединение не удалось",
		WebRTCStatsFormat:       "WebRTC: %s | Состояние: %s | Сброшено кадров: %d | Режим низкой задержки: %s",
		FallbackModeTestVideo:   "Fallback: генерация тестового видео...",
		FallbackModeFrame:       "Fallback: кадр %d (WebRTC недоступен)",
		FallbackModeActive:      "Fallback режим активен (WebRTC недоступен)",
		VirtualKeyboardClickToType: "нажмите для ввода",
		UnitPx:                  "px",
		UnitPercent:             "%",
		UnitKbps:                "kbps",
		DeviceRowTemplateName:   "—",
		DeviceRowTemplateStatus: "—",
		FullscreenWindowTitle:   "USB Bridge - Полноэкранный режим",
		SnapshotRowTemplateSize: "—",
		SnapshotRowTemplateDate: "—",

		// Time formats
		TimeFormat:              "15:04:05",
		DateFormat:              "02.01.2006",
		DateTimeFormat:          "02.01.2006 15:04",

		// PC Panel
		PCPanelPowerTitle:       "Power",
		PCPanelPowerConfirm:     "Нажать кнопку Power?",
		PCPanelPowerHoldTime:    "Время зажатия (сек):",
		PCPanelPowerShortPress:  "Короткое нажатие",
		PCPanelPowerLongPress:   "Длинное нажатие (%d с)",
		PCPanelResetTitle:       "Reset",
		PCPanelResetConfirm:     "Нажать кнопку Reset?",
		PCPanelPowerLED:         "Power LED",
		PCPanelHDDLED:           "HDD LED",
		PCPanelLongPressNotSupported: "Long press: будет поддерживаться в будущем",
	}
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
