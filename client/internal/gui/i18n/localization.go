package i18n

// LocalizedStrings contains all UI text strings
type LocalizedStrings struct {
	// Main Window
	AppTitle                string
	ServerAddress           string
	Token                   string
	ConnectButton           string
	DisconnectButton        string
	DisconnectAllButton     string
	VideoStreamActiveButton string
	TabDevices              string
	TabControl              string
	TabSnapshots            string
	TabScripts              string

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
	TailscaleRegisterLabel    string
	// ConnectingToConnection is the bottom toast's message while a Connect
	// attempt is in flight (view.ShowConnectingToast) -- %s is the
	// connection's own saved name.
	ConnectingToConnection string

	// Connections screen copy. Technical labels (LAN, TS, Token, QR, KVM,
	// Agent, OS, USBridge, Firmware, master key, IP) stay in English.
	ConnectionsHeaderSubtitle         string
	ConnectionsHeaderSubtitleMobile   string
	ViewModeGrid                      string
	ViewModeList                      string
	AddNewConnectTitle                string
	AddConnectHintLine1               string
	AddConnectHintLine2               string
	ScanQR                            string
	ScanQRShort                       string
	PasteLink                         string
	ManualEntry                       string
	OrEnterManually                   string
	AddConnectionSubtitle             string
	TailscaleRedirectHint             string
	AutoRegistrationBadge             string
	ConnectionBadgeUnknown            string
	ConnectionBadgeUnknownShort       string
	MenuPowerReset                    string
	MenuInfo                          string
	MenuCommunity                     string
	MenuAccount                       string
	MenuWebsite                       string
	TabLabelControl                   string
	TabLabelDevices                   string
	TabLabelSnapshots                 string
	TabLabelScripts                   string
	AccountTitle                      string
	AccountWaitingGoogle              string
	AccountSignedInAs                 string
	AccountForgotPassphrase           string
	AccountResetIt                    string
	AccountLogOut                     string
	AccountLoginIntro                 string
	AccountLoginGoogle                string
	AccountLicensesLoadErr            string
	AccountNoLicenses                 string
	AccountLoadingLicenses            string
	AccountConnectionsSync            string
	AccountSyncOn                     string
	AccountSyncOff                    string
	AccountSyncOnDesc                 string
	AccountAutoSyncNew                string
	AccountResetWarn                  string
	AccountNewPassphrase              string
	AccountResetOverwrite             string
	AccountResetting                  string
	AccountResetFailed                string
	AccountSetPassphraseHint          string
	AccountPassphrasePlaceholder      string
	AccountSetPassphrase              string
	AccountLoginTimeout               string
	AccountLoginExpired               string
	TailscaleSignInGoogle             string
	TailscaleSignOut                  string
	AgentCatalogSubtitle              string
	AgentCatalogFooterHint            string
	AgentFeatLowLatency               string
	AgentFeatClipboard                string
	AgentFeatMultiMonitor             string
	AgentFeatWebClient                string
	AgentFeatPreLogin                 string
	AgentFeatFastConnect              string
	AgentFeat444                      string
	AgentFeatUSB                      string
	AgentFeatRecording                string
	AgentFeatCompanyRollout           string
	AgentChipBasic                    string
	AgentChipPro                      string
	VideoCodec                        string
	VideoLowLatencyFmt                string
	VideoHighFidelityFmt              string
	DevicesMountNewISO                string
	DevicesMount                      string
	DevicesEmptyAudio                 string
	DevicesEmptyStorage               string
	DevicesEmptyUSB                   string
	DevicesEmptyNetwork               string
	DevicesEmptyBackup                string
	DevicesCardNetwork                string
	DevicesCardBackups                string
	SnapshotsTitle                    string
	SnapshotsHeaderSubtitle           string
	SnapshotsHeaderSubtitleMobile     string
	SnapshotsCountFmt                 string
	SnapshotsColDate                  string
	SnapshotsColSize                  string
	SnapshotsMountBackupFlash         string
	SnapshotsStatusAvailable          string
	SnapshotsStatusMounted            string
	ScriptsMCPSubtitle                string
	ScriptsAutomationTitle            string
	ScriptsAutomationSubtitle         string
	ScriptsCountFmt                   string
	ScriptsColSource                  string
	ScriptsNewEMMC                    string
	ScriptsNewSD                      string
	ScriptsNewEMMCMobile              string
	ScriptsNewSDMobile                string
	PCPanelPowerControls              string
	PCPanelPowerHardwareOnly          string
	PCPanelAction                     string
	PCPanelHoldToConfirm              string
	PCPanelDuration                   string
	PCPanelShortHold                  string
	PCPanelLongHold                   string
	PCPanelPowerOff                   string
	ConnectingDevice                  string
	SnapshotsEmpty                    string
	ScriptsEmpty                      string
	ScriptsLocalEndpoint              string
	ScriptsLocalModels                string
	ScriptsStateIdle                  string
	ScriptsStateStopped               string
	ScriptsStateRunning               string
	QRScanSuccess                     string
	ConnectionColOS                   string
	ConnectionColName                 string
	ConnectionColInfo                 string
	ConnectionColState                string
	ConnectionColNetwork              string
	ConnectionColSync                 string
	ConnectionColRouteBridge          string
	ConnectionColActions              string
	ConnectionSyncLocal               string
	ConnectionSyncCloud               string
	MobileColAction                   string
	ConnectionNameField               string
	ConnectionLANPlaceholder          string
	ConnectionTSPlaceholder           string
	AwaitingConnection                string
	FirmwarePromoTitle                string
	FirmwarePromoSubtitle             string
	FirmwarePromoTrial                string // "Download"
	FirmwarePromoFeatureBIOS          string
	FirmwarePromoFeatureLatency       string
	FirmwarePromoFeatureLatencyMobile string
	FirmwarePromoFeatureScripts       string
	FirmwarePromoFeatureSnapshot      string
	FirmwarePromoFeatureL0            string
	FirmwarePromoSDCardOnly           string
	FirmwarePromoSDCardEMMC           string

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
	FreeDeviceSlotRequired   string // "Free up one device slot to mount a backup/snapshot"
	ErrorLoadingLocalDevices string
	ErrorMountingFlashMsg    string
	ErrorMountingSnapshotMsg string
	ErrorStatusFormat        string

	// Snapshot details dialog
	SnapshotDetailsTitle   string // "Snapshot: %s"
	SnapshotDetailsDate    string // "Date: %s"
	SnapshotDetailsSize    string // "Size: %s"
	SnapshotChangelogTitle string // "Changes (changelog)"
	SnapshotChangelogEmpty string // "No changelog available"
	SnapshotTempFile       string // "temporary file"
	OK                     string // "OK"
	Copy                   string // "Copy"

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
	Devices                           string
	AllAvailableDevices               string
	DevicesSectionStorage             string
	DevicesSectionStorageEyebrow      string
	DevicesSectionStorageHint         string
	DevicesSectionBackup              string
	DevicesSectionBackupEyebrow       string
	DevicesSectionBackupHint          string
	DevicesSectionControl             string
	DevicesSectionControlEyebrow      string
	DevicesSectionControlHint         string
	DevicesSectionConnectivity        string
	DevicesSectionConnectivityEyebrow string
	DevicesSectionConnectivityHint    string
	DevicesSectionAudio               string
	DevicesSectionAudioHint           string
	DevicesSectionPassthrough         string
	DevicesSectionPassthroughHint     string
	USBPassthroughEnterpriseHint      string
	USBPassthroughProtected           string
	LocalDrives                       string
	NetworkDrives                     string
	MountButton                       string
	MountButtonCompact                string
	UnmountButton                     string
	UnmountButtonCompact              string
	AddImageButton                    string
	RefreshButton                     string
	LoadingFromCloud                  string
	CloudFilesDetected                string
	AndroidBuffering                  string // "Android is buffering cloud files"
	PreparingToMount                  string // "Preparing to mount"
	MayTake30Seconds                  string
	PleaseWait                        string
	MaxDevicesReached                 string // "Maximum of 5 devices selected"
	UnmountAllConfirm                 string // "Disconnect all mounted devices?"
	UnmountSelectedConfirm            string // "Disconnect selected devices?"
	NoMountedDevices                  string // "No mounted devices to unmount"
	SelectDevicesToMount              string // "Select devices to mount"
	StoppingAllDevices                string // "Stopping all devices..."
	StoppingNBDServers                string // "Stopping NBD servers..."
	AllDevicesUnmounted               string // "All devices disconnected"

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
	ErrorSunshineNoWebRTC   string

	// Dialogs
	Yes                          string
	No                           string
	Error                        string
	Information                  string
	Confirmation                 string
	TailscaleLogoutConfirm       string
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
	ConnectionLost               string
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

	// Auto-update
	UpdateAvailableTitle     string
	UpdateAvailableMessage   string // %s = new version, %s = current version
	UpdateNowButton          string
	UpdateLaterButton        string
	UpdateDownloadingTitle   string
	UpdateDownloadingMessage string // %s = new version

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
	VideoModeH265Name           string
	VideoModeH265Description    string
	VideoModeAV1Name            string
	VideoModeAV1Description     string
	VideoModeJPEGName           string
	VideoModeJPEGDescription    string
	VideoModeRawYUYVName        string
	VideoModeRawYUYVDescription string

	FramesDropped  string
	LowLatencyMode string

	// Language
	Language          string
	LanguageEnglish   string
	LanguageSpanish   string
	LanguageUkrainian string

	// Connection names
	ConnectionNumber         string // "Connection %d"
	BackupFlashName          string // "Backup Flash"
	PromoBenefitsTitle       string
	PromoFeatureBIOS         string
	PromoFeatureSnapshots    string
	PromoFeaturePowerControl string
	PromoFeatureL0Control    string

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

	// Window size (desktop vs compact widget)
	WindowSizeChip     string
	WindowSizeDesktop  string
	WindowSizeCompact  string
	WindowSizeScale    string
	WindowSizeScaleOff string

	// Device names
	DeviceKeyboard                       string // "Keyboard"
	DeviceTouchPad                       string // "TouchPad" (relative touchpad mode)
	DeviceMouse                          string // "Mouse" (USB pointer device name)
	DeviceTouch                          string // "Touch" (touchscreen mode)
	DeviceAbsolute                       string // "Absolute" (absolute pointing mode)
	DeviceAbsoluteLeft2                  string // "Abs L/2" (absolute, left display of 2)
	DeviceAbsoluteRight2                 string // "Abs R/2" (absolute, right display of 2)
	DeviceVirtualCursor                  string // "Cursor" (virtual cursor mode, Android only)
	DeviceGyroMouse                      string // "GyroMouse" (gyroscope cursor mode, Android only)
	DeviceNetworkCard                    string // "Network Card (RNDIS)"
	DeviceGamepad                        string // "Gamepad"
	DeviceDirectInput                    string // "DirectInput"
	DeviceXInput                         string // "XInput"
	XInputIncompatibleWithHID            string // error: XInput + keyboard/mouse
	GamepadDisconnectHIDConfirm          string // toast: connecting XInput gamepad will drop keyboard/mouse
	HIDDisconnectGamepadConfirm          string // toast: connecting keyboard/mouse will drop the XInput gamepad
	BackupFlashDisconnectSnapshotConfirm string // toast: mounting backup flash will unmount the snapshot
	ShowMouseCursor                      string // "Show Mouse" (show cursor in captured video)
	ClipboardSyncEnabled                 string // "Shared Clipboard" (toggle clipboard sync with the agent)
	EnableVSync                          string // "VSync (Vertical Sync)" checkbox title, video parameters dialog
	EnableVSyncHint                      string // description shown under the VSync checkbox
	EnableVSyncBadge                     string // small badge next to the VSync title, e.g. "RECOMMENDED"
	AIVision                             string // "AI Vision Detection Overlay" checkbox title, video parameters dialog
	AIVisionHint                         string // hint shown under the AI Vision checkbox
	AIVisionBadge                        string // small badge next to the AI Vision title, e.g. "EXPERIMENTAL"
	Color444                             string // "4:4:4 Color (RustShine)" checkbox -- always shown, grayed out when it doesn't currently apply. Doesn't say "Pro" itself -- that's Color444Badge's job, right next to it
	Color444Hint                         string // hint under the checkbox when 4:4:4 is available (checked or not)
	Color444UnavailableHint              string // hint under the (disabled, unchecked) checkbox when H.265 is selected but the agent doesn't currently offer 4:4:4
	Color444RequiresH265Hint             string // hint under the (disabled, unchecked) checkbox when the selected codec isn't H.265
	Color444Badge                        string // badge next to the 4:4:4 title, always shown regardless of codec/availability, e.g. "PRO"
	Hdr                                  string // "HDR Color (RustShine)" checkbox -- mirrors Color444 exactly, independent axis (see rust-shine's docs/COLOR_MODES.md)
	HdrHint                              string // hint under the checkbox when HDR is available (checked or not)
	HdrUnavailableHint                   string // hint under the (disabled, unchecked) checkbox when H.265 is selected but the agent doesn't currently offer HDR
	HdrRequiresH265Hint                  string // hint under the (disabled, unchecked) checkbox when the selected codec isn't H.265
	HdrBadge                             string // badge next to the HDR title, mirrors Color444Badge
	NetGraph                             string // "Net Graph" checkbox title, video parameters dialog -- live network/render HUD, pure local overlay like AI Vision
	NetGraphHint                         string // hint shown under the Net Graph checkbox
	NetGraphBadge                        string // small badge next to the Net Graph title, e.g. "MAC"
	FrameSmoothing                       string // "Smooth Motion" checkbox title, video parameters dialog -- motion-extrapolated stall concealment, pure local rendering fallback like AI Vision
	FrameSmoothingHint                   string // hint shown under the Smooth Motion checkbox
	FrameSmoothingBadge                  string // small badge next to the Smooth Motion title, e.g. "BETA"
	MuteAudio                            string // "Mute Audio"
	UnmuteAudio                          string // "Unmute Audio"
	DeviceAudio                          string // "Audio"
	DeviceUSBAudio                       string // "USB Audio Codec"
	AudioDeviceUAC1                      string // "UAC1"
	AudioDeviceUAC2                      string // "UAC2"
	DriveModeDisk                        string // "USB Stick"
	DriveModeCDROM                       string // "CD-ROM"

	// Deep link handler
	DeepLinkServerAddress string // "Server address:"
	DeepLinkToken         string // "Master Key:"
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
	PCPanelActionConfirm         string // "Confirm action?"
	PCPanelPowerExecuteConfirm   string // "Execute Power Off?"
	PCPanelPowerHoldTime         string // "Hold time (seconds):"
	PCPanelPowerShortPress       string // "Short press"
	PCPanelPowerLongPress        string // "Long press (%d s)"
	PCPanelResetTitle            string // "Reset"
	PCPanelResetConfirm          string // "Press Reset button?"
	PCPanelResetExecuteConfirm   string // "Execute Reset?"
	PCPanelPowerLED              string // "Power LED"
	PCPanelHDDLED                string // "HDD LED"
	PCPanelLongPressNotSupported string // "Long press: will be supported in future"

	// Moonlight manual pairing (shown when the host has no usbridge auto-pair
	// endpoint -- a stock Sunshine or real NVIDIA GameStream host)
	PairingPINTitle   string // "Pairing Required"
	PairingPINMessage string // "Enter this PIN on your host's pairing page:"
	PairingPINWaiting string // "Waiting for the host to accept it..."
}

// EN returns English localization
func EN() *LocalizedStrings {
	return &LocalizedStrings{
		// Main Window
		AppTitle:                "USBridge Client",
		ServerAddress:           "Server Address",
		Token:                   "Master Key",
		ConnectButton:           "Connect",
		DisconnectButton:        "Disconnect",
		DisconnectAllButton:     "Disconnect All",
		VideoStreamActiveButton: "Open Control",
		TabDevices:              "💽 Device",
		TabControl:              "📺 Control",
		TabSnapshots:            "💾 Snapshots",
		TabScripts:              "AI & Scripts",

		// Connection Manager
		ConnectionManager:                 "🔌 Connection Manager",
		SavedConnections:                  "Connections",
		ConnectionManagement:              "💾 Connection Management",
		AddressAndTokenHint:               "💡 Address and Master Key are entered in the bar above",
		ConnectionHeroEyebrow:             "USBRIDGE ACCESS",
		ConnectionPanelHint:               "Launch a saved profile or create a new one.",
		ConnectionNameLabel:               "Name for saving:",
		ConnectionNamePlaceholder:         "Connection name (e.g.: My PC)",
		SaveButton:                        "💾 Save",
		DeleteButton:                      "Delete",
		EditButton:                        "✏️",
		QRScannerButton:                   "📷 QR",
		EditConnectionTitle:               "Edit connection",
		AddConnectionTitle:                "Add connection",
		AddNewDeviceTitle:                 "Add a new device",
		NoSavedConnections:                "No saved connections yet",
		NoSavedConnectionsHint:            "Use QR or add a connection below to get started.",
		OnboardingStepConnect:             "Plug usb-c into target host for power and hid. Connect video path from target hdmi to capture dongle.",
		OnboardingStepIP:                  "Navigate to settings -> internet to connect your network.",
		OnboardingStepScan:                "Navigate to the Master Key section and scan the QR code. You can also enter the IP and Master Key manually.",
		DeleteConnectionTitle:             "Delete connection",
		DeleteConnectionConfirm:           "Are you sure you want to delete connection \"%s\"?",
		TailscaleRegisterLabel:            "Register in Tailscale",
		ConnectingToConnection:            "Connecting to \"%s\"…",
		ConnectionsHeaderSubtitle:         "Your remote desktop and hardware control sessions.",
		ConnectionsHeaderSubtitleMobile:   "Your remote desktop and hardware control sessions.",
		ViewModeGrid:                      "Grid",
		ViewModeList:                      "List",
		AddNewConnectTitle:                "Add New Connect",
		AddConnectHintLine1:               "Scan a QR code or paste a link",
		AddConnectHintLine2:               "to add a hardware or software agent",
		ScanQR:                            "Scan QR",
		ScanQRShort:                       "QR",
		PasteLink:                         "Paste Link",
		ManualEntry:                       "Manual",
		OrEnterManually:                   "OR ENTER MANUALLY",
		AddConnectionSubtitle:             "Pair a hardware or software agent using its IP address and master key.",
		TailscaleRedirectHint:             "After connection, the redirect will open on the web.",
		AutoRegistrationBadge:             "AUTO-REGISTRATION",
		ConnectionBadgeUnknown:            "Unknown",
		ConnectionBadgeUnknownShort:       "Unk",
		MenuPowerReset:                    "Power Reset",
		MenuInfo:                          "Info",
		MenuCommunity:                     "Community",
		MenuAccount:                       "Account",
		MenuWebsite:                       "Website",
		TabLabelControl:                   "Control",
		TabLabelDevices:                   "Devices",
		TabLabelSnapshots:                 "Snapshots",
		TabLabelScripts:                   "AI & Scripts",
		AccountTitle:                      "Account",
		AccountWaitingGoogle:              "Waiting for Google login to complete in your browser...",
		AccountSignedInAs:                 "Signed in as",
		AccountForgotPassphrase:           "Forgot passphrase? ",
		AccountResetIt:                    "Reset it",
		AccountLogOut:                     "Log out",
		AccountLoginIntro:                 "Log in to see your USBridge licenses and sync your saved connections across devices.",
		AccountLoginGoogle:                "Log in with Google",
		AccountLicensesLoadErr:            "Could not load licenses: %v",
		AccountNoLicenses:                 "No licenses on this account yet.",
		AccountLoadingLicenses:            "Loading your licenses…",
		AccountConnectionsSync:            "Connections sync",
		AccountSyncOn:                     "on",
		AccountSyncOff:                    "off",
		AccountSyncOnDesc:                 "End-to-end encrypted sync of your saved connections across devices.",
		AccountAutoSyncNew:                "Sync new connections to the cloud automatically",
		AccountResetWarn:                  "Resetting starts fresh: this device's own saved connections will overwrite whatever is currently synced on this account under the old passphrase -- that old synced data becomes permanently unreadable the moment you do this. Enter a new passphrase:",
		AccountNewPassphrase:              "New sync passphrase",
		AccountResetOverwrite:             "Reset & overwrite",
		AccountResetting:                  "Resetting...",
		AccountResetFailed:                "Reset failed: %v",
		AccountSetPassphraseHint:          "Set a sync passphrase to sync your saved connections across devices (never sent to our servers):",
		AccountPassphrasePlaceholder:      "Sync passphrase",
		AccountSetPassphrase:              "Set passphrase",
		AccountLoginTimeout:               "Didn't detect a completed login yet — try \"Log in\" again.",
		AccountLoginExpired:               "Login link expired — click \"Log in\" again.",
		TailscaleSignInGoogle:             "Sign In With Google",
		TailscaleSignOut:                  "Sign Out",
		AgentCatalogSubtitle:              "Install the host service on the machine you want to control.",
		AgentCatalogFooterHint:            "The Agent is installed on the target machine, not this client.",
		AgentFeatLowLatency:               "Ultra-low latency streaming",
		AgentFeatClipboard:                "Shared clipboard",
		AgentFeatMultiMonitor:             "Multi-monitor support",
		AgentFeatWebClient:                "Browser web client",
		AgentFeatPreLogin:                 "Windows pre-login access",
		AgentFeatFastConnect:              "Fast connect",
		AgentFeat444:                      "4:4:4 color fidelity",
		AgentFeatUSB:                      "USB device emulation",
		AgentFeatRecording:                "Session recording and audit logs",
		AgentFeatCompanyRollout:           "Built for company-wide rollout",
		AgentChipBasic:                    "+ Basic",
		AgentChipPro:                      "+ Pro",
		VideoCodec:                        "Codec",
		VideoLowLatencyFmt:                "Low Latency (%.1f %s)",
		VideoHighFidelityFmt:              "High Fidelity (%.1f %s)",
		DevicesMountNewISO:                "Mount New ISO",
		DevicesMount:                      "Mount",
		DevicesEmptyAudio:                 "No audio devices",
		DevicesEmptyStorage:               "No storage or ISO media",
		DevicesEmptyUSB:                   "No USB devices",
		DevicesEmptyNetwork:               "No network bridge devices",
		DevicesEmptyBackup:                "No backup devices",
		DevicesCardNetwork:                "Network",
		DevicesCardBackups:                "Backups",
		SnapshotsTitle:                    "Snapshots",
		SnapshotsHeaderSubtitle:           "Immutable restore points of your data. Mount one without changing the original.",
		SnapshotsHeaderSubtitleMobile:     "Restore points. Mount without changing the original.",
		SnapshotsCountFmt:                 "%d Snapshots",
		SnapshotsColDate:                  "DATE",
		SnapshotsColSize:                  "SIZE",
		SnapshotsMountBackupFlash:         "Mount backup flash",
		SnapshotsStatusAvailable:          "Available",
		SnapshotsStatusMounted:            "Mounted",
		ScriptsMCPSubtitle:                "Local signed MCP endpoint.",
		ScriptsAutomationTitle:            "Automation Scripts",
		ScriptsAutomationSubtitle:         "Starlark jobs on the device.",
		ScriptsCountFmt:                   "%d Scripts",
		ScriptsColSource:                  "SOURCE",
		ScriptsNewEMMC:                    "New (eMMC)",
		ScriptsNewSD:                      "New (SD Card)",
		ScriptsNewEMMCMobile:              "(eMMC)",
		ScriptsNewSDMobile:                "(SD Card)",
		PCPanelPowerControls:              "Power controls",
		PCPanelPowerHardwareOnly:          "Power controls are available on USBridge hardware only.",
		PCPanelAction:                     "Action",
		PCPanelHoldToConfirm:              "Hold to Confirm",
		PCPanelDuration:                   "Duration",
		PCPanelShortHold:                  "Short (0s)",
		PCPanelLongHold:                   "Long (10s)",
		PCPanelPowerOff:                   "Power Off",
		ConnectingDevice:                  "connecting device",
		SnapshotsEmpty:                    "No snapshots yet",
		ScriptsEmpty:                      "No scripts yet",
		ScriptsLocalEndpoint:              "Local endpoint",
		ScriptsLocalModels:                "Local models",
		ScriptsStateIdle:                  "Idle",
		ScriptsStateStopped:               "Stopped",
		ScriptsStateRunning:               "Running",
		QRScanSuccess:                     "QR code successfully scanned",
		ConnectionColOS:                   "OS",
		ConnectionColName:                 "NAME",
		ConnectionColInfo:                 "INFO",
		ConnectionColState:                "STATE",
		ConnectionColNetwork:              "NETWORK",
		ConnectionColSync:                 "SYNC",
		ConnectionColRouteBridge:          "ROUTE BRIDGE",
		ConnectionColActions:              "ACTIONS",
		ConnectionSyncLocal:               "Local",
		ConnectionSyncCloud:               "Cloud",
		MobileColAction:                   "ACTION",
		ConnectionNameField:               "Name",
		ConnectionLANPlaceholder:          "LAN address",
		ConnectionTSPlaceholder:           "Tailscale address",
		AwaitingConnection:                "Awaiting connection...",
		FirmwarePromoTitle:                "USBridge Firmware",
		FirmwarePromoSubtitle:             "Turn your board into a hardware KVM",
		FirmwarePromoTrial:                "Download",
		FirmwarePromoFeatureBIOS:          "BIOS-in-terminal (OCR)",
		FirmwarePromoFeatureLatency:       "Ultra-low latency video",
		FirmwarePromoFeatureLatencyMobile: "Ultra-low latency video",
		FirmwarePromoFeatureScripts:       "Automation scripts",
		FirmwarePromoFeatureSnapshot:      "Immutable snapshot",
		FirmwarePromoFeatureL0:            "0-layer host access",
		FirmwarePromoSDCardOnly:           "SD Card only",
		FirmwarePromoSDCardEMMC:           "SD Card / eMMC",

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
		Copy:                   "Copy",

		ChangelogOpSnapshot: "snapshot creation",
		ChangelogOpUtimes:   "time update",
		ChangelogOpMkfile:   "file creation",
		ChangelogOpRename:   "rename",
		ChangelogOpTruncate: "file truncation",
		ChangelogOpClone:    "clone",
		ChangelogOpChown:    "owner change",
		ChangelogOpChmod:    "permissions change",

		// Disk Widget
		Devices:                           "💽 Devices",
		AllAvailableDevices:               "Organized into storage, console control, and connectivity.",
		DevicesSectionStorage:             "Storage",
		DevicesSectionStorageEyebrow:      "VIRTUAL DISK",
		DevicesSectionStorageHint:         "ISO and IMG media for upload, swapping, and on-demand mounting.",
		DevicesSectionBackup:              "Backup Device",
		DevicesSectionBackupEyebrow:       "BACKUP + MTP",
		DevicesSectionBackupHint:          "MTP-connected backup storage exposed by the device service.",
		DevicesSectionControl:             "Control Console",
		DevicesSectionControlEyebrow:      "VIDEO + INPUT",
		DevicesSectionControlHint:         "Capture, keyboard, and pointer devices that operate together as the KVM console.",
		DevicesSectionConnectivity:        "Connectivity",
		DevicesSectionConnectivityEyebrow: "NETWORK",
		DevicesSectionConnectivityHint:    "RNDIS bridge and channel infrastructure used to link the remote host.",
		DevicesSectionAudio:               "Audio",
		DevicesSectionAudioHint:           "Audio capture sources and USB Audio Codec gadget.",
		DevicesSectionPassthrough:         "USB Passthrough",
		DevicesSectionPassthroughHint:     "Redirect a local USB device into the remote Windows session.",
		USBPassthroughEnterpriseHint:      "Requires Enterprise on the Windows agent.",
		USBPassthroughProtected:           "Protected (session input / capture)",
		LocalDrives:                       "Local Drives",
		NetworkDrives:                     "Network Drives",
		MountButton:                       "🔌 Mount",
		MountButtonCompact:                "🔌 Mount",
		UnmountButton:                     "🔴 Unmount",
		UnmountButtonCompact:              "❌",
		AddImageButton:                    "➕",
		RefreshButton:                     "🔄",
		LoadingFromCloud:                  "Loading from cloud",
		CloudFilesDetected:                "Google Drive files detected among selected files.",
		AndroidBuffering:                  "Android is buffering cloud files to the device.",
		PreparingToMount:                  "Preparing to mount",
		MayTake30Seconds:                  "This may take up to 30 seconds per file.",
		PleaseWait:                        "Please wait...",
		MaxDevicesReached:                 "Maximum 5 devices can be selected",
		UnmountAllConfirm:                 "Unmount all connected devices?",
		UnmountSelectedConfirm:            "Unmount selected devices?",
		NoMountedDevices:                  "No connected devices to unmount",
		SelectDevicesToMount:              "Select devices to connect",
		StoppingAllDevices:                "Stopping all devices...",
		StoppingNBDServers:                "Stopping NBD servers...",
		AllDevicesUnmounted:               "All devices unmounted",

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
		TokenLabel:              "Master Key:",
		ScanSuccess:             "✓ QR code successfully scanned!\n\nCheck the data and press 'Apply' to fill in the fields.",
		ErrorOpeningFile:        "Error opening file: %v",
		ErrorDecodingImage:      "Error decoding image: %v",
		ErrorProcessingImage:    "Error processing image: %v",
		ErrorLaunchingQRScanner: "Error launching QR scanner: %v",
		ImageNotLoaded:          "Image not loaded",
		QRCodeNotFound:          "QR code not found in image.\n\nMake sure that:\n• Image contains QR code\n• QR code is clear and visible\n• Image has sufficient resolution",
		InvalidQRFormat:         "Invalid QR code format.\nExpected: host:master_key\nReceived: %s",
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
		ErrorSunshineNoWebRTC:   "This device is running Sunshine, which doesn't support WebRTC video in the browser. Switch it to RustShine (available with a subscription) to watch and control it from the web client.",

		// Dialogs
		Yes:                          "Yes",
		No:                           "No",
		Error:                        "Error",
		Information:                  "Information",
		Confirmation:                 "Confirmation",
		TailscaleLogoutConfirm:       "Do you want to sign out of Tailscale?",
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
		ConnectionLost:               "Connection lost: %v",
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

		// Auto-update
		UpdateAvailableTitle:     "Update Available",
		UpdateAvailableMessage:   "Version %s is ready to install. You're on %s.",
		UpdateNowButton:          "Update",
		UpdateLaterButton:        "Not Now",
		UpdateDownloadingTitle:   "Updating…",
		UpdateDownloadingMessage: "Downloading version %s…",

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
		FPSRange:             "FPS must be from 1 to 120",
		QualityRange:         "quality must be from 1 to 100 percent",
		BitrateRange:         "bitrate must be from 100 to 150000 kbps",

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
		VideoModeH264Description:    "UVC capture → H.264 encode → RTP/UDP",
		VideoModeH265Name:           "H.265",
		VideoModeH265Description:    "HEVC hardware encode — better quality at lower bitrate",
		VideoModeAV1Name:            "AV1",
		VideoModeAV1Description:     "AV1 hardware encode — best compression (requires Apple Silicon)",
		VideoModeJPEGName:           "JPEG RTP",
		VideoModeJPEGDescription:    "MJPEG direct / YUYV encode → RTP",
		VideoModeRawYUYVName:        "RAW YUYV",
		VideoModeRawYUYVDescription: "YUYV (Uncompressed) → RTP → Direct Render",

		FramesDropped:  "Dropped",
		LowLatencyMode: "Low latency mode",

		// Language
		Language:          "Language",
		LanguageEnglish:   "English",
		LanguageSpanish:   "Spanish",
		LanguageUkrainian: "Ukrainian",

		// Connection names
		ConnectionNumber:         "Connection %d",
		BackupFlashName:          "Backup Flash",
		PromoBenefitsTitle:       "Benefits you'll get",
		PromoFeatureBIOS:         "BIOS-in-Terminal",
		PromoFeatureSnapshots:    "Snapshots",
		PromoFeaturePowerControl: "Power Control",
		PromoFeatureL0Control:    "L0 Control",

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

		WindowSizeChip:     "Size",
		WindowSizeDesktop:  "Desktop",
		WindowSizeCompact:  "Compact",
		WindowSizeScale:    "Scale",
		WindowSizeScaleOff: "On in Compact",

		// Device names
		DeviceKeyboard:                       "Keyboard",
		DeviceTouchPad:                       "TouchPad",
		DeviceMouse:                          "Mouse",
		DeviceTouch:                          "TouchScreen",
		DeviceAbsolute:                       "Absolute",
		DeviceAbsoluteLeft2:                  "Abs L/2",
		DeviceAbsoluteRight2:                 "Abs R/2",
		DeviceVirtualCursor:                  "Cursor",
		DeviceGyroMouse:                      "GyroMouse",
		DeviceNetworkCard:                    "Network Card (RNDIS)",
		DeviceGamepad:                        "Gamepad",
		DeviceDirectInput:                    "DirectInput",
		DeviceXInput:                         "XInput",
		XInputIncompatibleWithHID:            "XInput gamepad cannot be used together with keyboard or mouse. Connect gamepad separately.",
		GamepadDisconnectHIDConfirm:          "Connecting the gamepad will disconnect the keyboard and mouse.",
		HIDDisconnectGamepadConfirm:          "Connecting the keyboard or mouse will disconnect the gamepad.",
		BackupFlashDisconnectSnapshotConfirm: "A snapshot is mounted. Mounting the backup flash will unmount it.",
		ShowMouseCursor:                      "Show Mouse",
		ClipboardSyncEnabled:                 "Shared Clipboard",
		EnableVSync:                          "VSync (Vertical Sync)",
		EnableVSyncHint:                      "Synchronizes frame delivery with the host display's refresh rate to eliminate tearing during fast motion.",
		EnableVSyncBadge:                     "Recommended",
		AIVision:                             "AI Vision Detection Overlay",
		AIVisionHint:                         "Overlays live object detection (Set-of-Mark bounding boxes + hex IDs) on the video feed, mirroring an agent's ui.parse() telemetry call.",
		AIVisionBadge:                        "Experimental",
		Color444:                             "4:4:4 Color (RustShine)",
		Color444Hint:                         "Captures full-resolution chroma instead of the usual 4:2:0 subsampling -- sharper on-screen text and fine detail, at a higher bitrate cost.",
		Color444UnavailableHint:              "Requires RustShine Pro and a GPU with HEVC 4:4:4 hardware encode.",
		Color444RequiresH265Hint:             "Only available with the H.265 codec -- select it above to use 4:4:4 color.",
		Color444Badge:                        "Pro",
		Hdr:                                  "HDR Color (RustShine)",
		HdrHint:                              "Captures and streams high dynamic range video (BT.2020 color, PQ curve) instead of standard SDR -- brighter highlights and a wider color range on an HDR-capable display, at a higher bitrate cost.",
		HdrUnavailableHint:                   "Requires RustShine Pro and a Mac with HEVC Main10 hardware encode (Apple Silicon).",
		HdrRequiresH265Hint:                  "Only available with the H.265 codec -- select it above to use HDR.",
		HdrBadge:                             "Pro",
		NetGraph:                             "Net Graph",
		NetGraphHint:                         "Shows a live TF2-style HUD in the corner of the video: latency, packet loss, FEC recovery, and render/decode timing.",
		NetGraphBadge:                        "Mac",
		FrameSmoothing:                       "Smooth Motion",
		FrameSmoothingHint:                   "When the network stalls, fills the gap with a motion-extrapolated frame instead of freezing -- never delays real frames, only bridges a late/lost one.",
		FrameSmoothingBadge:                  "Beta",
		MuteAudio:                            "Mute Audio",
		UnmuteAudio:                          "Unmute Audio",
		DeviceAudio:                          "Audio",
		DeviceUSBAudio:                       "USB Audio Codec",
		AudioDeviceUAC1:                      "UAC1",
		AudioDeviceUAC2:                      "UAC2",
		DriveModeDisk:                        "USB Stick",
		DriveModeCDROM:                       "CD-ROM",

		// Deep link handler
		DeepLinkServerAddress: "Server address:",
		DeepLinkToken:         "Master Key:",
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
		PCPanelActionConfirm:         "Confirm action?",
		PCPanelPowerExecuteConfirm:   "Execute Power Off?",
		PCPanelPowerHoldTime:         "Hold time (seconds):",
		PCPanelPowerShortPress:       "Short press",
		PCPanelPowerLongPress:        "Long press (%d s)",
		PCPanelResetTitle:            "Reset",
		PCPanelResetConfirm:          "Press Reset button?",
		PCPanelResetExecuteConfirm:   "Execute Reset?",
		PCPanelPowerLED:              "Power LED",
		PCPanelHDDLED:                "HDD LED",
		PCPanelLongPressNotSupported: "Long press: will be supported in future",

		// Moonlight manual pairing
		PairingPINTitle:   "Pairing Required",
		PairingPINMessage: "Enter this PIN on your host's pairing page:",
		PairingPINWaiting: "Waiting for the host to accept it...",
	}
}

// ES returns the Spanish locale.
func ES() *LocalizedStrings {
	locale := EN()
	locale.AppTitle = "Cliente USBridge"
	locale.ServerAddress = "Direccion del servidor"
	locale.Token = "Master Key"
	locale.ConnectButton = "Conectar"
	locale.DisconnectButton = "Desconectar"
	locale.DisconnectAllButton = "Desconectar todo"
	locale.VideoStreamActiveButton = "Abrir control"
	locale.TabDevices = "Dispositivos"
	locale.TabControl = "Control"
	locale.TabSnapshots = "Instantaneas"
	locale.ConnectionManager = "Administrador de conexiones"
	locale.SavedConnections = "Conexiones"
	locale.ConnectionManagement = "Gestion de conexiones"
	locale.ConnectionHeroEyebrow = "ACCESO USBRIDGE"
	locale.ConnectionPanelHint = "Inicia un perfil guardado o crea uno nuevo."
	locale.EditConnectionTitle = "Editar conexion"
	locale.AddConnectionTitle = "Agregar conexion"
	locale.AddNewDeviceTitle = "Agregar un nuevo dispositivo"
	locale.NoSavedConnections = "Todavia no hay conexiones guardadas"
	locale.NoSavedConnectionsHint = "Usa QR o agrega una conexion para comenzar."
	locale.PromoBenefitsTitle = "Beneficios que recibiras"
	locale.PromoFeatureBIOS = "BIOS-in-Terminal"
	locale.PromoFeatureSnapshots = "Snapshots"
	locale.PromoFeaturePowerControl = "Power Control"
	locale.PromoFeatureL0Control = "L0 Control"
	locale.OnboardingStepConnect = "Conecta USB-C al host de destino para energia y HID. Conecta la salida HDMI al capturador."
	locale.OnboardingStepIP = "Abre configuracion de red y conecta el dispositivo a tu red."
	locale.OnboardingStepScan = "Escanea el codigo QR del Master Key o introduce IP y Master Key manualmente."
	locale.SaveButton = "Guardar"
	locale.DeleteButton = "Eliminar"
	locale.DeleteConnectionTitle = "Eliminar conexion"
	locale.DeleteConnectionConfirm = "Seguro que deseas eliminar la conexion \"%s\"?"
	locale.ConnectingToConnection = "Conectando a \"%s\"…"
	locale.ConnectionsHeaderSubtitle = "Tus sesiones de escritorio remoto y control de hardware."
	locale.ConnectionsHeaderSubtitleMobile = "Tus sesiones de escritorio remoto y control de hardware."
	locale.ViewModeGrid = "Grid"
	locale.ViewModeList = "Lista"
	locale.AddNewConnectTitle = "Nueva conexion"
	locale.AddConnectHintLine1 = "Escanea un codigo QR o pega un enlace"
	locale.AddConnectHintLine2 = "para agregar un agent de hardware o software"
	locale.ScanQR = "Escanear QR"
	locale.PasteLink = "Pegar enlace"
	locale.ManualEntry = "Manual"
	locale.OrEnterManually = "O INTRODUCIR MANUALMENTE"
	locale.AddConnectionSubtitle = "Empareja un agent de hardware o software con su IP y master key."
	locale.TailscaleRedirectHint = "Tras conectar, la redireccion se abrira en el navegador."
	locale.AutoRegistrationBadge = "AUTO-REGISTRO"
	locale.QRScanSuccess = "Codigo QR escaneado"
	locale.ConnectionColOS = "OS"
	locale.ConnectionColName = "NOMBRE"
	locale.ConnectionColInfo = "INFO"
	locale.ConnectionColState = "ESTADO"
	locale.ConnectionColNetwork = "RED"
	locale.ConnectionColSync = "SYNC"
	locale.ConnectionColRouteBridge = "RUTA"
	locale.ConnectionColActions = "ACCIONES"
	locale.ConnectionSyncLocal = "Local"
	locale.ConnectionSyncCloud = "Nube"
	locale.MobileColAction = "ACCION"
	locale.ConnectionNameField = "Nombre"
	locale.ConnectionLANPlaceholder = "Direccion LAN"
	locale.ConnectionTSPlaceholder = "Direccion Tailscale"
	locale.AwaitingConnection = "Esperando conexion..."
	locale.FirmwarePromoTitle = "USBridge Firmware"
	locale.FirmwarePromoSubtitle = "Convierte tu placa en un KVM de hardware"
	locale.FirmwarePromoTrial = "Descargar"
	locale.FirmwarePromoFeatureBIOS = "BIOS-in-terminal (OCR)"
	locale.FirmwarePromoFeatureLatency = "Video de ultra baja latencia"
	locale.FirmwarePromoFeatureLatencyMobile = "Video de ultra baja latencia"
	locale.FirmwarePromoFeatureScripts = "Scripts de automatizacion"
	locale.FirmwarePromoFeatureSnapshot = "Snapshot inmutable"
	locale.FirmwarePromoFeatureL0 = "Acceso host de capa 0"
	locale.FirmwarePromoSDCardOnly = "Solo SD Card"
	locale.FirmwarePromoSDCardEMMC = "SD Card / eMMC"
	locale.Devices = "Dispositivos"
	locale.DevicesSectionStorage = "Almacenamiento"
	locale.DevicesSectionBackup = "Dispositivo de respaldo"
	locale.DevicesSectionControl = "Consola de control"
	locale.DevicesSectionConnectivity = "Conectividad"
	locale.LocalDrives = "Unidades locales"
	locale.NetworkDrives = "Unidades de red"
	locale.MountButton = "Montar"
	locale.UnmountButton = "Desmontar"
	locale.RefreshButton = "Actualizar"
	locale.LoadingFromCloud = "Cargando desde la nube"
	locale.StatusConnected = "Conectado"
	locale.StatusDisconnected = "Desconectado"
	locale.StatusConnecting = "Conectando..."
	locale.StatusError = "Error"
	locale.Apply = "Aplicar"
	locale.Cancel = "Cancelar"
	locale.Yes = "Si"
	locale.No = "No"
	locale.Error = "Error"
	locale.Information = "Informacion"
	locale.Confirmation = "Confirmacion"
	locale.TailscaleLogoutConfirm = "Deseas cerrar sesion en Tailscale?"
	locale.TailscaleRegisterLabel = "Registrar en Tailscale"
	locale.Success = "Exito"
	locale.Done = "Hecho"
	locale.Close = "Cerrar"
	locale.ConnectionTitle = "Conexion"
	locale.UpdateAvailableTitle = "Actualizacion disponible"
	locale.UpdateAvailableMessage = "La version %s esta lista para instalar. Tienes %s."
	locale.UpdateNowButton = "Actualizar"
	locale.UpdateLaterButton = "Ahora no"
	locale.UpdateDownloadingTitle = "Actualizando…"
	locale.UpdateDownloadingMessage = "Descargando la version %s…"
	locale.VideoQualitySettings = "Configuracion de calidad de video"
	locale.Resolution = "Resolucion"
	locale.FrameRate = "Frecuencia"
	locale.StreamMode = "Modo de transmision"
	locale.StartVideo = "Iniciar"
	locale.SettingsAction = "Configuracion"
	locale.FullscreenAction = "Pantalla completa"
	locale.Language = "Idioma"
	locale.LanguageEnglish = "English"
	locale.LanguageSpanish = "Spanish"
	locale.LanguageUkrainian = "Ukrainian"
	locale.DeviceKeyboard = "Teclado"
	locale.DeviceTouchPad = "TouchPad"
	locale.DeviceMouse = "Raton"
	locale.DeviceTouch = "Tactil"
	locale.DeviceAbsolute = "Absoluto"
	locale.DeviceAbsoluteLeft2 = "Abs I/2"
	locale.DeviceVirtualCursor = "Cursor"
	locale.DeviceGyroMouse = "GyroMouse"
	locale.DeviceAbsoluteRight2 = "Abs D/2"
	locale.DeviceNetworkCard = "Tarjeta de red (RNDIS)"
	locale.ShowMouseCursor = "Mostrar ratón"
	locale.ClipboardSyncEnabled = "Portapapeles compartido"
	locale.DeepLinkServerAddress = "Direccion del servidor:"
	locale.DeepLinkToken = "Master Key:"
	locale.DeepLinkConnectPrompt = "Deseas conectarte a este servidor?\n\nElige una accion:"
	locale.DeepLinkConnect = "Conectar"
	locale.DeepLinkSave = "Guardar"
	locale.VirtualKeyboard = "Teclado virtual"
	locale.WindowSizeChip = "Tamano"
	locale.WindowSizeDesktop = "Desktop"
	locale.WindowSizeCompact = "Compacto"
	locale.WindowSizeScale = "Escala"
	locale.WindowSizeScaleOff = "En Compacto"
	locale.FullscreenWindowTitle = "USBridge - Pantalla completa"
	locale.PCPanelPowerTitle = "Encendido"
	locale.PCPanelResetTitle = "Reinicio"
	locale.BackupFlashDisconnectSnapshotConfirm = "Hay un snapshot montado. Al montar el backup flash se desmontara."
	locale.ConnectionBadgeUnknown = "Otro"
	locale.ConnectionBadgeUnknownShort = "Otro"
	locale.MenuPowerReset = "Energia"
	locale.MenuInfo = "Info"
	locale.MenuCommunity = "Comunidad"
	locale.MenuAccount = "Cuenta"
	locale.MenuWebsite = "Sitio"
	locale.TabLabelControl = "Control"
	locale.TabLabelDevices = "Devices"
	locale.TabLabelSnapshots = "Snapshots"
	locale.TabLabelScripts = "AI y Scripts"
	locale.TabScripts = "AI y Scripts"
	locale.AccountTitle = "Cuenta"
	locale.AccountWaitingGoogle = "Espera el login de Google en tu navegador..."
	locale.AccountSignedInAs = "Sesion de"
	locale.AccountForgotPassphrase = "Olvidaste passphrase? "
	locale.AccountResetIt = "Resetear"
	locale.AccountLogOut = "Salir"
	locale.AccountLoginIntro = "Entra para ver tus licencias USBridge y sincronizar conexiones entre devices."
	locale.AccountLoginGoogle = "Entrar con Google"
	locale.AccountLicensesLoadErr = "No se pudieron cargar licencias: %v"
	locale.AccountNoLicenses = "Aun no hay licencias en esta cuenta."
	locale.AccountLoadingLicenses = "Cargando licencias…"
	locale.AccountConnectionsSync = "Sync de conexiones"
	locale.AccountSyncOn = "on"
	locale.AccountSyncOff = "off"
	locale.AccountSyncOnDesc = "Sync cifrado de extremo a extremo de tus conexiones entre devices."
	locale.AccountAutoSyncNew = "Sincronizar conexiones nuevas a la nube automaticamente"
	locale.AccountResetWarn = "El reset empieza de cero: las conexiones de este device pisan lo sincronizado con el passphrase viejo -- esos datos quedan ilegibles. Introduce uno nuevo:"
	locale.AccountNewPassphrase = "Nuevo sync passphrase"
	locale.AccountResetOverwrite = "Reset y reemplazar"
	locale.AccountResetting = "Reseteando..."
	locale.AccountResetFailed = "Reset fallido: %v"
	locale.AccountSetPassphraseHint = "Pon un sync passphrase para sincronizar conexiones entre devices (no se envia a servidores):"
	locale.AccountPassphrasePlaceholder = "Sync passphrase"
	locale.AccountSetPassphrase = "Poner passphrase"
	locale.AccountLoginTimeout = "No se completo el login — prueba \"Entrar\" otra vez."
	locale.AccountLoginExpired = "El enlace de login expiro — pulsa \"Entrar\" otra vez."
	locale.TailscaleSignInGoogle = "Entrar con Google"
	locale.TailscaleSignOut = "Salir"
	locale.AgentCatalogSubtitle = "Instala el host service en la maquina que quieres controlar."
	locale.AgentCatalogFooterHint = "El Agent se instala en la maquina objetivo, no en este client."
	locale.AgentFeatLowLatency = "Streaming de ultra baja latencia"
	locale.AgentFeatClipboard = "Portapapeles compartido"
	locale.AgentFeatMultiMonitor = "Soporte multi-monitor"
	locale.AgentFeatWebClient = "Cliente web en el browser"
	locale.AgentFeatPreLogin = "Acceso Windows pre-login"
	locale.AgentFeatFastConnect = "Fast connect"
	locale.AgentFeat444 = "Fidelidad de color 4:4:4"
	locale.AgentFeatUSB = "Emulacion USB"
	locale.AgentFeatRecording = "Grabacion de sesion y audit logs"
	locale.AgentFeatCompanyRollout = "Pensado para rollout en la empresa"
	locale.AgentChipBasic = "+ Basic"
	locale.AgentChipPro = "+ Pro"
	locale.VideoCodec = "Codec"
	locale.VideoLowLatencyFmt = "Baja latencia (%.1f %s)"
	locale.VideoHighFidelityFmt = "Alta fidelidad (%.1f %s)"
	locale.VideoParameters = "Parametros de video"
	locale.EnableVSync = "VSync"
	locale.EnableVSyncHint = "Sincroniza los fotogramas con la pantalla para evitar tearing en movimiento rapido."
	locale.EnableVSyncBadge = "Recomendado"
	locale.AIVision = "Overlay AI Vision"
	locale.AIVisionHint = "Deteccion en vivo (cajas + IDs hex) sobre el video, como ui.parse() del agent."
	locale.AIVisionBadge = "Prueba"
	locale.Color444Hint = "Croma 4:4:4 completa en vez de 4:2:0: texto mas nitido, mas bitrate."
	locale.Color444UnavailableHint = "Requiere RustShine Pro y GPU con HEVC 4:4:4."
	locale.Color444RequiresH265Hint = "Solo con codec H.265 -- seleccionalo arriba."
	locale.HdrHint = "Flujo HDR (BT.2020, PQ) en vez de SDR: luces mas brillantes en pantalla HDR."
	locale.HdrUnavailableHint = "Requiere RustShine Pro y Mac con HEVC Main10 (Apple Silicon)."
	locale.HdrRequiresH265Hint = "Solo con codec H.265 -- seleccionalo arriba."
	locale.VideoJPEGRTPHint = "JPEG RTP: MJPEG se reenvia; YUYV se codifica a JPEG antes de enviar."
	locale.VideoRawYUYVHint = "RAW YUYV: video sin comprimir por RTP. Solo en enlaces locales rapidos."
	locale.VideoModeH264Description = "UVC capture → H.264 encode → RTP/UDP"
	locale.VideoModeH265Description = "Encode HEVC hardware -- mejor calidad a menor bitrate"
	locale.VideoModeAV1Description = "Encode AV1 hardware -- mejor compresion (Apple Silicon)"
	locale.VideoModeJPEGDescription = "MJPEG directo / YUYV encode → RTP"
	locale.VideoModeRawYUYVDescription = "YUYV (sin comprimir) → RTP → Direct Render"
	locale.DevicesMountNewISO = "Montar ISO"
	locale.DevicesMount = "Montar"
	locale.DevicesEmptyAudio = "Sin audio"
	locale.DevicesEmptyStorage = "Sin storage ni ISO"
	locale.DevicesEmptyUSB = "Sin USB"
	locale.DevicesEmptyNetwork = "Sin bridge de red"
	locale.DevicesEmptyBackup = "Sin backup"
	locale.DevicesCardNetwork = "Network"
	locale.DevicesCardBackups = "Backups"
	locale.SnapshotsTitle = "Snapshots"
	locale.SnapshotsHeaderSubtitle = "Puntos de restore inmutables. Monta uno sin cambiar el original."
	locale.SnapshotsHeaderSubtitleMobile = "Puntos de restore. Monta sin cambiar el original."
	locale.SnapshotsCountFmt = "%d Snapshots"
	locale.SnapshotsColDate = "FECHA"
	locale.SnapshotsColSize = "TAM."
	locale.SnapshotsMountBackupFlash = "Montar backup flash"
	locale.SnapshotsStatusAvailable = "Disponible"
	locale.SnapshotsStatusMounted = "Montado"
	locale.ScriptsMCPSubtitle = "Endpoint MCP firmado local."
	locale.ScriptsAutomationTitle = "Scripts"
	locale.ScriptsAutomationSubtitle = "Jobs Starlark en el device."
	locale.ScriptsCountFmt = "%d Scripts"
	locale.ScriptsColSource = "ORIGEN"
	locale.ScriptsNewEMMC = "Nuevo (eMMC)"
	locale.ScriptsNewSD = "Nuevo (SD Card)"
	locale.PCPanelPowerControls = "Power controls"
	locale.PCPanelPowerHardwareOnly = "Power controls solo en hardware USBridge."
	locale.PCPanelAction = "Accion"
	locale.PCPanelHoldToConfirm = "Mantener"
	locale.PCPanelDuration = "Duracion"
	locale.PCPanelShortHold = "Corto (0s)"
	locale.PCPanelLongHold = "Largo (10s)"
	locale.PCPanelPowerOff = "Apagar"
	locale.ConnectingDevice = "conectando"
	locale.SnapshotsEmpty = "Sin snapshots"
	locale.ScriptsEmpty = "Sin scripts"
	locale.ScriptsLocalEndpoint = "Local endpoint"
	locale.ScriptsLocalModels = "Modelos local"
	locale.ScriptsStateIdle = "Idle"
	locale.ScriptsStateStopped = "Parado"
	locale.ScriptsStateRunning = "Activo"
	return locale
}

// UK returns the Ukrainian locale.
func UK() *LocalizedStrings {
	locale := EN()
	locale.SavedConnections = "З'єднання"
	locale.EditConnectionTitle = "Редагувати з'єднання"
	locale.AddConnectionTitle = "Додати з'єднання"
	locale.AddNewDeviceTitle = "Додати новий пристрій"
	locale.PromoBenefitsTitle = "Переваги, які ви отримаєте"
	locale.PromoFeatureBIOS = "BIOS-in-Terminal"
	locale.PromoFeatureSnapshots = "Snapshots"
	locale.PromoFeaturePowerControl = "Power Control"
	locale.PromoFeatureL0Control = "L0 Control"
	locale.DeleteButton = "Видалити"
	locale.DeepLinkConnect = "Підключити"
	locale.DeepLinkSave = "Зберегти"
	locale.DeleteConnectionTitle = "Видалити з'єднання"
	locale.Language = "Мова"
	locale.LanguageEnglish = "Англійська"
	locale.LanguageSpanish = "Іспанська"
	locale.LanguageUkrainian = "Українська"
	locale.SettingsAction = "Налаштування"
	locale.FullscreenAction = "На весь екран"
	locale.DeviceKeyboard = "Клавіатура"
	locale.DeviceMouse = "Миша"
	locale.DeviceTouch = "Дотик"
	locale.DeviceAbsolute = "Абсолютний"
	locale.DeviceAbsoluteLeft2 = "Абс Л/2"
	locale.DeviceAbsoluteRight2 = "Абс П/2"
	locale.DeviceVirtualCursor = "Курсор"
	locale.DeviceGyroMouse = "ГіроМиша"
	locale.Close = "Закрити"
	locale.Cancel = "Скасувати"
	locale.Yes = "Так"
	locale.No = "Ні"
	locale.TailscaleLogoutConfirm = "Вийти з Tailscale?"
	return locale
}

// UKProper returns the Ukrainian locale.
func UKProper() *LocalizedStrings {
	locale := EN()
	locale.AppTitle = "Клієнт USBridge"
	locale.ServerAddress = "Адреса сервера"
	locale.Token = "Master Key"
	locale.ConnectButton = "Підключити"
	locale.DisconnectButton = "Відключити"
	locale.DisconnectAllButton = "Відключити все"
	locale.VideoStreamActiveButton = "Відкрити керування"
	locale.TabDevices = "Пристрої"
	locale.TabControl = "Керування"
	locale.TabSnapshots = "Знімки"
	locale.ConnectionManager = "Менеджер з'єднань"
	locale.SavedConnections = "З'єднання"
	locale.ConnectionManagement = "Керування з'єднаннями"
	locale.ConnectionHeroEyebrow = "ДОСТУП USBRIDGE"
	locale.ConnectionPanelHint = "Запустіть збережений профіль або створіть новий."
	locale.EditConnectionTitle = "Редагувати з'єднання"
	locale.AddConnectionTitle = "Додати з'єднання"
	locale.AddNewDeviceTitle = "Додати новий пристрій"
	locale.NoSavedConnections = "Ще немає збережених з'єднань"
	locale.NoSavedConnectionsHint = "Скористайтеся QR або додайте з'єднання вручну."
	locale.PromoBenefitsTitle = "Переваги, які ви отримаєте"
	locale.PromoFeatureBIOS = "BIOS-in-Terminal"
	locale.PromoFeatureSnapshots = "Snapshots"
	locale.PromoFeaturePowerControl = "Power Control"
	locale.PromoFeatureL0Control = "L0 Control"
	locale.OnboardingStepConnect = "Підключіть USB-C до цільового хоста для живлення та HID. Підключіть HDMI до пристрою захоплення."
	locale.OnboardingStepIP = "Відкрийте налаштування мережі та підключіть пристрій до вашої мережі."
	locale.OnboardingStepScan = "Відскануйте QR-код Master Key або введіть IP і Master Key вручну."
	locale.SaveButton = "Зберегти"
	locale.DeleteButton = "Видалити"
	locale.DeleteConnectionTitle = "Видалити з'єднання"
	locale.DeleteConnectionConfirm = "Ви впевнені, що хочете видалити з'єднання \"%s\"?"
	locale.ConnectingToConnection = "Підключення до \"%s\"…"
	locale.ConnectionsHeaderSubtitle = "Ваші сесії віддаленого робочого столу та керування обладнанням."
	locale.ConnectionsHeaderSubtitleMobile = "Ваші сесії віддаленого керування."
	locale.ViewModeGrid = "Сітка"
	locale.ViewModeList = "Список"
	locale.AddNewConnectTitle = "Нове з'єднання"
	locale.AddConnectHintLine1 = "Відскануйте QR-код або вставте посилання"
	locale.AddConnectHintLine2 = "щоб додати hardware або software agent"
	locale.ScanQR = "Сканувати QR"
	locale.PasteLink = "Посилання"
	locale.ManualEntry = "Вручну"
	locale.OrEnterManually = "АБО ВВЕСТИ ВРУЧНУ"
	locale.AddConnectionSubtitle = "Прив'яжіть hardware або software agent за IP та master key."
	locale.TailscaleRedirectHint = "Після конекту редірект відкриється в браузері."
	locale.AutoRegistrationBadge = "АВТОРЕЄСТРАЦІЯ"
	locale.QRScanSuccess = "QR-код відскановано"
	locale.ConnectionColOS = "OS"
	locale.ConnectionColName = "НАЗВА"
	locale.ConnectionColInfo = "ІНФО"
	locale.ConnectionColState = "СТАН"
	locale.ConnectionColNetwork = "МЕРЕЖА"
	locale.ConnectionColSync = "СИНК"
	locale.ConnectionColRouteBridge = "МАРШРУТ"
	locale.ConnectionColActions = "ДІЇ"
	locale.ConnectionSyncLocal = "Локально"
	locale.ConnectionSyncCloud = "Хмара"
	locale.MobileColAction = "ДІЯ"
	locale.ConnectionNameField = "Назва"
	locale.ConnectionLANPlaceholder = "Адреса LAN"
	locale.ConnectionTSPlaceholder = "Адреса Tailscale"
	locale.AwaitingConnection = "Очікування з'єднання..."
	locale.FirmwarePromoTitle = "USBridge Firmware"
	locale.FirmwarePromoSubtitle = "Перетворіть плату на апаратний KVM"
	locale.FirmwarePromoTrial = "Завантажити"
	locale.FirmwarePromoFeatureBIOS = "BIOS-in-terminal (OCR)"
	locale.FirmwarePromoFeatureLatency = "Відео з ультранизькою затримкою"
	locale.FirmwarePromoFeatureLatencyMobile = "Ультранизька затримка"
	locale.FirmwarePromoFeatureScripts = "Скрипти автоматизації"
	locale.FirmwarePromoFeatureSnapshot = "Незмінний snapshot"
	locale.FirmwarePromoFeatureL0 = "Доступ host на шарі 0"
	locale.FirmwarePromoSDCardOnly = "Лише SD Card"
	locale.FirmwarePromoSDCardEMMC = "SD Card / eMMC"
	locale.Devices = "Пристрої"
	locale.DevicesSectionStorage = "Сховище"
	locale.DevicesSectionBackup = "Резервний пристрій"
	locale.DevicesSectionControl = "Консоль керування"
	locale.DevicesSectionConnectivity = "Мережа"
	locale.LocalDrives = "Локальні диски"
	locale.NetworkDrives = "Мережеві диски"
	locale.MountButton = "Підключити"
	locale.UnmountButton = "Відключити"
	locale.RefreshButton = "Оновити"
	locale.LoadingFromCloud = "Завантаження з хмари"
	locale.StatusConnected = "Підключено"
	locale.StatusDisconnected = "Відключено"
	locale.StatusConnecting = "Підключення..."
	locale.StatusError = "Помилка"
	locale.Apply = "Застосувати"
	locale.Cancel = "Скасувати"
	locale.Yes = "Так"
	locale.No = "Ні"
	locale.Error = "Помилка"
	locale.Information = "Інформація"
	locale.Confirmation = "Підтвердження"
	locale.TailscaleLogoutConfirm = "Вийти з Tailscale?"
	locale.TailscaleRegisterLabel = "Зареєструвати в Tailscale"
	locale.Success = "Успіх"
	locale.Done = "Готово"
	locale.Close = "Закрити"
	locale.ConnectionTitle = "З'єднання"
	locale.UpdateAvailableTitle = "Доступне оновлення"
	locale.UpdateAvailableMessage = "Версія %s готова до встановлення. У вас %s."
	locale.UpdateNowButton = "Оновити"
	locale.UpdateLaterButton = "Не зараз"
	locale.UpdateDownloadingTitle = "Оновлення…"
	locale.UpdateDownloadingMessage = "Завантаження версії %s…"
	locale.VideoQualitySettings = "Налаштування якості відео"
	locale.Resolution = "Роздільна здатність"
	locale.FrameRate = "Частота кадрів"
	locale.StreamMode = "Режим потоку"
	locale.StartVideo = "Запустити"
	locale.SettingsAction = "Налаштування"
	locale.FullscreenAction = "На весь екран"
	locale.Language = "Мова"
	locale.LanguageEnglish = "English"
	locale.LanguageSpanish = "Spanish"
	locale.LanguageUkrainian = "Ukrainian"
	locale.DeviceKeyboard = "Клавіатура"
	locale.DeviceTouchPad = "TouchPad"
	locale.DeviceMouse = "Миша"
	locale.DeviceTouch = "Дотик"
	locale.DeviceAbsolute = "Абсолютний"
	locale.DeviceAbsoluteLeft2 = "Абс Л/2"
	locale.DeviceAbsoluteRight2 = "Абс П/2"
	locale.DeviceVirtualCursor = "Курсор"
	locale.DeviceGyroMouse = "ГіроМиша"
	locale.DeviceNetworkCard = "Мережева карта (RNDIS)"
	locale.ShowMouseCursor = "Показувати курсор"
	locale.ClipboardSyncEnabled = "Спільний буфер обміну"
	locale.DeepLinkServerAddress = "Адреса сервера:"
	locale.DeepLinkToken = "Master Key:"
	locale.DeepLinkConnectPrompt = "Хочете підключитися до цього сервера?\n\nВиберіть дію:"
	locale.DeepLinkConnect = "Підключити"
	locale.DeepLinkSave = "Зберегти"
	locale.VirtualKeyboard = "Віртуальна клавіатура"
	locale.WindowSizeChip = "Розмір"
	locale.WindowSizeDesktop = "Десктоп"
	locale.WindowSizeCompact = "Компакт"
	locale.WindowSizeScale = "Масштаб"
	locale.WindowSizeScaleOff = "У Compact"
	locale.FullscreenWindowTitle = "USBridge - Повний екран"
	locale.PCPanelPowerTitle = "Живлення"
	locale.PCPanelResetTitle = "Скидання"
	locale.BackupFlashDisconnectSnapshotConfirm = "Змонтовано знімок. Підключення backup-флешки його розмонтує."
	locale.ConnectionBadgeUnknown = "Інше"
	locale.ConnectionBadgeUnknownShort = "Інше"
	locale.MenuPowerReset = "Живлення"
	locale.MenuInfo = "Інфо"
	locale.MenuCommunity = "Спільнота"
	locale.MenuAccount = "Акаунт"
	locale.MenuWebsite = "Сайт"
	locale.TabLabelControl = "Контроль"
	locale.TabLabelDevices = "Девайси"
	locale.TabLabelSnapshots = "Знімки"
	locale.TabLabelScripts = "AI і скрипти"
	locale.TabScripts = "AI і скрипти"
	locale.AccountTitle = "Акаунт"
	locale.AccountWaitingGoogle = "Чекаємо Google login у браузері..."
	locale.AccountSignedInAs = "Вхід як"
	locale.AccountForgotPassphrase = "Забули passphrase? "
	locale.AccountResetIt = "Скинути"
	locale.AccountLogOut = "Вийти"
	locale.AccountLoginIntro = "Увійдіть, щоб бачити ліцензії USBridge і синкати з'єднання між девайсами."
	locale.AccountLoginGoogle = "Увійти з Google"
	locale.AccountLicensesLoadErr = "Не вдалось завантажити ліцензії: %v"
	locale.AccountNoLicenses = "На цьому акаунті ще немає ліцензій."
	locale.AccountLoadingLicenses = "Завантаження ліцензій…"
	locale.AccountConnectionsSync = "Синк з'єднань"
	locale.AccountSyncOn = "вкл"
	locale.AccountSyncOff = "вимк"
	locale.AccountSyncOnDesc = "Наскрізне шифрування з'єднань між вашими девайсами."
	locale.AccountAutoSyncNew = "Синкати нові з'єднання в хмару автоматично"
	locale.AccountResetWarn = "Скидання з нуля: з'єднання цього девайса замінять синк зі старим passphrase — старі дані стануть нечитабельні. Введіть новий:"
	locale.AccountNewPassphrase = "Новий sync passphrase"
	locale.AccountResetOverwrite = "Скинути й замінити"
	locale.AccountResetting = "Скидання..."
	locale.AccountResetFailed = "Скидання не вдалось: %v"
	locale.AccountSetPassphraseHint = "Задайте sync passphrase, щоб синкати з'єднання між девайсами (на сервер не йде):"
	locale.AccountPassphrasePlaceholder = "Sync passphrase"
	locale.AccountSetPassphrase = "Задати passphrase"
	locale.AccountLoginTimeout = "Логін не завершено — спробуйте «Увійти» ще раз."
	locale.AccountLoginExpired = "Посилання логіну прострочене — натисніть «Увійти» знову."
	locale.TailscaleSignInGoogle = "Увійти з Google"
	locale.TailscaleSignOut = "Вийти"
	locale.AgentCatalogSubtitle = "Встановіть host-сервіс на ПК, яким керуєте."
	locale.AgentCatalogFooterHint = "Agent ставиться на цільову машину, не на цей клієнт."
	locale.AgentFeatLowLatency = "Стрім з ультранизькою затримкою"
	locale.AgentFeatClipboard = "Спільний буфер обміну"
	locale.AgentFeatMultiMonitor = "Підтримка кількох моніторів"
	locale.AgentFeatWebClient = "Веб-клієнт у браузері"
	locale.AgentFeatPreLogin = "Доступ Windows до логіну"
	locale.AgentFeatFastConnect = "Швидкий конект"
	locale.AgentFeat444 = "Колір 4:4:4"
	locale.AgentFeatUSB = "Емуляція USB"
	locale.AgentFeatRecording = "Запис сесій і аудит"
	locale.AgentFeatCompanyRollout = "Для розгортання в компанії"
	locale.AgentChipBasic = "+ Basic"
	locale.AgentChipPro = "+ Pro"
	locale.VideoCodec = "Codec"
	locale.VideoLowLatencyFmt = "Низька затримка (%.1f %s)"
	locale.VideoHighFidelityFmt = "Висока якість (%.1f %s)"
	locale.VideoParameters = "Параметри відео"
	locale.EnableVSync = "VSync"
	locale.EnableVSyncHint = "Синхронізує кадри з екраном, без розривів при швидкому русі."
	locale.EnableVSyncBadge = "Радимо"
	locale.AIVision = "Накладка AI Vision"
	locale.AIVisionHint = "Живе розпізнавання (рамки + hex ID) поверх відео, як ui.parse() агента."
	locale.AIVisionBadge = "Тест"
	locale.Color444Hint = "Повна хрома 4:4:4 замість 4:2:0 — чіткіший текст, вищий бітрейт."
	locale.Color444UnavailableHint = "Потрібні RustShine Pro і GPU з HEVC 4:4:4."
	locale.Color444RequiresH265Hint = "Лише з кодеком H.265 — оберіть його вище."
	locale.HdrHint = "HDR-потік (BT.2020, PQ) замість SDR — яскравіші світла на HDR-екрані."
	locale.HdrUnavailableHint = "Потрібні RustShine Pro і Mac з HEVC Main10 (Apple Silicon)."
	locale.HdrRequiresH265Hint = "Лише з кодеком H.265 — оберіть його вище."
	locale.VideoJPEGRTPHint = "JPEG RTP: MJPEG іде напряму; YUYV кодується в JPEG перед відправкою."
	locale.VideoRawYUYVHint = "RAW YUYV: без стиснення по RTP. Лише на швидкому LAN."
	locale.VideoModeH265Description = "HEVC апаратне кодування — краща якість при нижчому бітрейті"
	locale.VideoModeAV1Description = "AV1 апаратне кодування — краще стиснення (Apple Silicon)"
	locale.VideoModeJPEGDescription = "MJPEG напряму / YUYV encode → RTP"
	locale.VideoModeRawYUYVDescription = "YUYV (без стиснення) → RTP → Direct Render"
	locale.DevicesMountNewISO = "Новий ISO"
	locale.DevicesMount = "Монтувати"
	locale.DevicesEmptyAudio = "Немає аудіо"
	locale.DevicesEmptyStorage = "Немає storage / ISO"
	locale.DevicesEmptyUSB = "Немає USB"
	locale.DevicesEmptyNetwork = "Немає network bridge"
	locale.DevicesEmptyBackup = "Немає backup"
	locale.DevicesCardNetwork = "Network"
	locale.DevicesCardBackups = "Backups"
	locale.SnapshotsTitle = "Знімки"
	locale.SnapshotsHeaderSubtitle = "Незмінні точки відновлення. Монтуй без зміни оригіналу."
	locale.SnapshotsHeaderSubtitleMobile = "Точки відновлення. Монтуй без зміни оригіналу."
	locale.SnapshotsCountFmt = "%d знімків"
	locale.SnapshotsColDate = "ДАТА"
	locale.SnapshotsColSize = "РОЗМІР"
	locale.SnapshotsMountBackupFlash = "Примонтувати backup flash"
	locale.SnapshotsStatusAvailable = "Доступний"
	locale.SnapshotsStatusMounted = "Змонтовано"
	locale.ScriptsMCPSubtitle = "Локальний підписаний MCP."
	locale.ScriptsAutomationTitle = "Скрипти"
	locale.ScriptsAutomationSubtitle = "Starlark-задачі на девайсі."
	locale.ScriptsCountFmt = "%d скриптів"
	locale.ScriptsColSource = "ДЖЕРЕЛО"
	locale.ScriptsNewEMMC = "Новий (eMMC)"
	locale.ScriptsNewSD = "Новий (SD Card)"
	locale.PCPanelPowerControls = "Power controls"
	locale.PCPanelPowerHardwareOnly = "Power controls лише на hardware USBridge."
	locale.PCPanelAction = "Дія"
	locale.PCPanelHoldToConfirm = "Утримуй"
	locale.PCPanelDuration = "Тривалість"
	locale.PCPanelShortHold = "Коротке (0s)"
	locale.PCPanelLongHold = "Довге (10s)"
	locale.PCPanelPowerOff = "Вимкнути"
	locale.ConnectingDevice = "підключення"
	locale.SnapshotsEmpty = "Поки немає знімків"
	locale.ScriptsEmpty = "Поки немає скриптів"
	locale.ScriptsLocalEndpoint = "Local endpoint"
	locale.ScriptsLocalModels = "Локальні моделі"
	locale.ScriptsStateIdle = "Idle"
	locale.ScriptsStateStopped = "Стоп"
	locale.ScriptsStateRunning = "Онлайн"
	return locale
}

// Current holds the current active localization
var Current *LocalizedStrings

// LanguagePrefKey is the Fyne Preferences key for the last chosen UI language.
const LanguagePrefKey = "language"

// Init initializes the localization system
func Init(language string) {
	switch language {
	case "es", "ES":
		Current = ES()
	case "uk", "UK", "ua", "UA":
		Current = UKProper()
	default:
		Current = EN()
	}
}

// SetLanguage changes the current language
func SetLanguage(language string) {
	Init(language)
}
