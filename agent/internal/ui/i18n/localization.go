package i18n

// LocalizedStrings is the agent's UI copy. Technical names stay English in
// every language: Sunshine, Moonlight, Tailscale, Token, PIN, USB, GPU,
// HTTP, WebRTC, Web, Streamer, GitHub, Pro, Enterprise, Free, Open Source.
type LocalizedStrings struct {
	AppTitle string

	// Settings / Info / Theme
	Language                string
	Info                    string
	Software                string
	Hardware                string
	Website                 string
	Theme                   string
	ThemeDefault            string
	InterfaceScale          string
	GeneralSettings         string
	GeneralSettingsSubtitle string
	AgentAutoUpdate         string
	RemoteWindowLock        string
	RemoteWindowLockHint    string

	// Cards
	Permissions string
	Status      string
	Protocol    string
	Graphics    string
	Change      string

	// Permissions
	Accessibility        string
	InputControl         string
	ScreenCapture        string
	GrantSuffix          string
	PermGrant            string
	PermGranted          string
	PermInfo             string
	PunktfunkCaptureInfo string
	PermDownload         string
	AutostartInfo        string
	// AutostartInfoBody is the Autostart info dialog's how-to; %s is one of
	// AutostartVia* below (the platform's mechanism).
	AutostartInfoBody    string
	AutostartViaService  string
	AutostartViaLaunchd  string
	AutostartViaSystemd  string
	AutostartEntry       string
	AutostartAtBoot      string
	AutostartRebootHint  string
	LockGPUClocks        string
	NvidiaMaxPerformance string
	NvPowerMode          string
	NvPowerMax           string
	NvPowerConsistent    string
	NvPowerAdaptive      string
	NvPowerOptimal       string
	NvPowerDriver        string
	GPUStreaming         string
	GPUNoEncoderSettings string
	NvencTwoPass         string
	ClipboardTool        string
	Install              string
	ClipboardInstall     string
	ClipboardNoPkgMgr    string
	Firewall             string
	OpenPorts            string
	FirewallInfoTitle    string
	FirewallNoChange     string
	AWDLDisable          string
	AWDLDisableInfoTitle string
	USBPassthrough       string
	USBAccess            string
	// USBHardwareDongle is the USB Passthrough chip's granted-state label on
	// macOS specifically, when a physical USB/IP dongle is plugged in and
	// working -- "HW USB", not translated (a technical badge, not prose).
	USBHardwareDongle string
	// USBDongleInfoTitle/Body/CTA: the macOS "Info" tap's dialog (no dongle
	// plugged in yet) -- explains why a hardware dongle exists at all
	// (macOS has no USB/IP driver of its own) and points at a live example
	// instead of just stating the technical cause, see
	// perm_drivers.go's showUSBDongleInfoDialog.
	USBDongleInfoTitle   string
	USBDongleInfoBody    string
	USBDongleInfoCTA     string
	VirtualDisplayAccess string
	WiFiTuneAccess       string // Permissions line: one-time Wi-Fi tune for streaming
	WiFiTuneInfo         string // what that tune does (info dialog)
	InstallUSBDriver     string
	GetUSBIPDriver       string
	MoonlightClients     string
	MoonlightHeader      string
	RemoveAllMoonlight   string
	WebRTCToggle         string

	// Status rows (technical labels stay English)
	Streamer   string
	USBBroker  string
	HTTP       string
	Sunshine   string
	SunWeb     string
	Web        string
	NotRunning string
	NotStaged  string

	// USB broker consent (see ui.Window's usbBrokerRow / EnableUSBBroker) --
	// the proprietary usb-broker binary must never run without this
	// explicit, one-time opt-in.
	EnableUSBBroker       string
	USBBrokerConsentTitle string
	USBBrokerConsentBody  string
	// USB Broker Info button after consent (showUSBBrokerStatusDialog).
	USBBrokerRunningOnPort string // %d = URB port
	USBBrokerFreeTierNote  string
	USBBrokerNotRunning    string
	USBBrokerLastError     string
	// Status panel row with the address the USB broker listens on.
	USBPort         string
	USBPortFallback string // %d = configured port another app holds

	// USBridge streamer consent (download confirmation for closed-source streamer)
	StreamerConsentTitle string
	StreamerConsentBody  string

	// Tailscale
	SignIn              string
	SignOut             string
	NoRemoteControllers string
	LoginLinkOpened     string
	InvalidLoginURL     string

	// Buy / support
	BuyPro        string
	BuyEnterprise string
	SupportUs     string

	// Footer busy
	ChangingProtocol string
	// DownloadingNamed: footer hint while a streamer downloads; %s is its
	// name (Sunshine, Punktfunk, USBridge Streamer).
	DownloadingNamed    string
	CheckingUpdates     string
	AlreadyUpToDate     string
	UpdateFailed        string
	StreamerUpdated     string
	UpdateAvailableHint string
	StreamerUpdateAsk   string

	// Common
	ErrorTitle     string
	Yes            string
	No             string
	OK             string
	Cancel         string
	Save           string
	Update         string
	NotNow         string
	Download       string
	LogOut         string
	LicenseManager string
	Copy           string
	Submit         string

	// Token dialog
	TokenTitle       string
	CopyLink         string
	RegenerateKey    string
	QRUnavailable    string
	QRUnavailableErr string
	TokenUnavail     string

	// Account
	AccountTitle            string
	WaitingGoogleLogin      string
	CouldntOpenBrowserLogin string
	LoginIntro              string
	SignedInAs              string
	Subscription            string
	Plan                    string
	AccountLicense          string // Account window field: this machine's Hardware ID
	YourLicenses            string
	Moving                  string
	LogInWithGoogle         string
	NoDesktopLicenses       string
	UseLicenseOnDevice      string
	LicenseOnThisDevice     string
	LicenseUsedElsewhere    string
	LicenseUsedHere         string
	RebindLicenseConfirm    string
	AlreadyBoughtIntro      string
	USBridgeAccount         string
	SubActive               string
	SubTrial                string
	SubNone                 string

	// Moonlight
	PairMoonlight    string
	MoonlightPINHint string
	EnterPINShown    string
	PINPlaceholder   string
	NoPairedClients  string

	// Status dialogs
	SunshineWebUI      string
	OpenInBrowser      string
	Login              string
	Password           string
	URL                string
	WebClient          string
	WebClientHint      string
	Certificate        string
	CertSelfSigned     string
	CertLetsEncrypt    string
	CertificateTitle   string
	CertificateHint    string
	CertHostnameLabel  string
	CertExpiresLabel   string
	CertPending        string
	CertErrorPrefix    string
	CertRetry          string
	CertSignInRequired string
	CertSignInHint     string
	SunshineStreaming  string
	SunshineAdminPort  string
	InvalidPortWide    string
	InvalidPort        string
	Host               string
	Port               string
	RestartsSunshine   string
	SetsExternalIP     string

	// Tariffs
	TariffsTitle               string
	TariffSubtitle             string
	CapabilitiesIncluded       string
	CouldntOpenBrowserBuy      string
	SubscribeTitle             string
	SubscribeBody              string
	LicenseDialogTitle         string
	WaitingCheckout            string
	DownloadingStreamer        string
	SettingUp                  string
	TierActiveDownloading      string
	RustShineProActive         string
	RustShineEnterpriseActive  string
	PickLicenseBelow           string
	CouldntOpenBrowserCheckout string
	LowerTierNote              string
	ForgetLicenseLocally       string
	ForgetLicenseTitle         string
	ForgetLicenseBody          string
	CheckoutTitle              string
	FeatLowLatency             string
	FeatLowLatencySub          string
	FeatClipboard              string
	FeatClipboardSub           string
	FeatMultiMonitor           string
	FeatMultiMonitorSub        string
	FeatWebClient              string
	FeatWebClientSub           string
	FeatPreLogin               string
	FeatPreLoginSub            string
	FeatFastConnect            string
	FeatFastConnectSub         string
	FeatVirtualDisplay         string
	FeatVirtualDisplaySub      string
	Feat444                    string
	Feat444Sub                 string
	FeatWacom                  string
	FeatWacomSub               string
	FeatRecording              string
	FeatRecordingSub           string
	FeatCompanyRollout         string
	FeatCompanyRolloutSub      string

	// Updates
	UpdateAvailable     string
	UpdateAvailableBody string
	Updating            string
	DownloadingVersion  string
	WhatsNewTitle       string
	WhatsNewGotIt       string
	WhatsNewBadge       string
	WhatsNewSubtitle    string
	WhatsNewGitHub      string

	// Tray
	TrayOpen         string
	TrayRestart      string
	TrayCheckUpdate  string
	TrayQuit         string
	TrayStillRunning string

	// Misc status
	RelayDERP          string
	RelayDERPFmt       string
	NotConnected       string
	SignedOut          string
	SignInRequired     string
	SignInToPublish    string
	Connected          string
	ServiceUnavailable string
	LogoutError        string
	StartingLogin      string
	ErrorFmt           string
}

var Current *LocalizedStrings

const LanguagePrefKey = "language"

var currentCode = "en"

// Code is the active UI language: "en", "es", or "uk".
func Code() string {
	if currentCode == "" {
		return "en"
	}
	return currentCode
}

func Init(language string) {
	switch language {
	case "es", "ES":
		currentCode = "es"
		Current = ES()
	case "uk", "UK", "ua", "UA":
		currentCode = "uk"
		Current = UK()
	default:
		currentCode = "en"
		Current = EN()
	}
}

func SetLanguage(language string) {
	Init(language)
}

func EN() *LocalizedStrings {
	return &LocalizedStrings{
		AppTitle: "USBridge Agent",

		Language:                "Language",
		Info:                    "Info",
		Software:                "Software",
		Hardware:                "Hardware",
		Website:                 "Website",
		Theme:                   "Theme",
		ThemeDefault:            "Default",
		InterfaceScale:          "Interface scale",
		GeneralSettings:         "General Settings",
		GeneralSettingsSubtitle: "Preferences that apply to the whole agent.",
		AgentAutoUpdate:         "USBridge protocol auto-update",
		RemoteWindowLock:        "Block remote control of this window",
		RemoteWindowLockHint:    "When this is on, a remote session cannot click the agent window. Be careful.",

		Permissions: "Permissions",
		Status:      "Status",
		Protocol:    "Protocol",
		Graphics:    "Graphics",
		Change:      "Change",

		Accessibility:        "Accessibility",
		InputControl:         "Input Control",
		ScreenCapture:        "Screen Capture",
		GrantSuffix:          " · Grant",
		PermGrant:            "Grant",
		PermGranted:          "Granted",
		PermInfo:             "Info",
		PunktfunkCaptureInfo: "Punktfunk has no KMS capture. It takes the picture from the desktop compositor (KWin, GNOME, Sway, Hyprland or gamescope), so it needs a running Wayland session: it cannot stream the login screen or anything before login, and it does not work on X11.\n\nFrames still go to the encoder on the GPU with no CPU copies, but the compositor copies each frame once more on the GPU, which KMS capture does not need. What that costs on this machine has not been measured: the benchmark in the client compares it with the other streamers.",
		PermDownload:         "Download",
		AutostartInfo:        "Autostart",
		AutostartInfoBody:    "1. Put the agent file in any folder you like.\n2. Tick this checkbox: the agent registers itself to start %s.\n\nTo remove it from autostart, just untick the checkbox.\n\nMoved the agent file to another folder? Untick the checkbox and tick it again so autostart picks up the new location.",
		AutostartViaService:  "automatically as a Windows service",
		AutostartViaLaunchd:  "automatically when you log in",
		AutostartViaSystemd:  "automatically at boot (systemd service)",
		AutostartEntry:       "Registered entry:",
		AutostartAtBoot:      "Autostart at Boot",
		AutostartRebootHint:  "(Windows restart required)",
		LockGPUClocks:        "Lock GPU Clocks",
		NvidiaMaxPerformance: "NVIDIA: max performance (lower latency)",
		NvPowerMode:          "NVIDIA power mode",
		NvPowerMax:           "Max performance",
		NvPowerConsistent:    "Consistent performance",
		NvPowerAdaptive:      "Adaptive",
		NvPowerOptimal:       "Optimal power",
		NvPowerDriver:        "Driver setting",
		GPUStreaming:         "streaming",
		GPUNoEncoderSettings: "No encoder settings here: the NVIDIA ones above don't apply to this card",
		NvencTwoPass:         "NVENC two-pass (better picture, more GPU load)",
		ClipboardTool:        "Clipboard Tool",
		Install:              "Install",
		ClipboardInstall:     "Clipboard Tool Install",
		ClipboardNoPkgMgr:    "No supported package manager (or pkexec) was found on this system -- clicking Install will show why, instead of a command preview.",
		Firewall:             "Firewall",
		OpenPorts:            "Open Ports",
		FirewallInfoTitle:    "Open Firewall Ports",
		FirewallNoChange:     "The firewall already allows the agent's and streamer's ports -- nothing to change.",
		AWDLDisable:          "Disable AWDL (recommended for Wi-Fi)",
		AWDLDisableInfoTitle: "AWDL Streaming Optimization",
		USBPassthrough:       "USB Passthrough Driver",
		USBAccess:            "USB Passthrough",
		USBHardwareDongle:    "HW USB",
		USBDongleInfoTitle:   "USB Hardware Passthrough",
		USBDongleInfoBody:    "macOS has no USB/IP driver of its own, so this works through a small hardware dongle instead of software. Plug one into this Mac and a USB device from the other side of the stream -- a graphics tablet, for example -- shows up here with full pressure and tilt, just like it were connected locally.",
		USBDongleInfoCTA:     "See It In Action",
		VirtualDisplayAccess: "Virtual Display",
		WiFiTuneAccess:       "Wi-Fi Without Scans",
		WiFiTuneInfo:         "Wi-Fi drivers keep scanning other channels in the background, taking the radio off your network for a moment every 30-60 s: on a stream that shows as periodic stutters.\n\nGrant once (administrator rights) to turn those scans off for good: on Windows the Intel driver's \"Global BG Scan blocking\" is set to Always; on Linux Wi-Fi power saving and iwd's periodic scans are turned off. Wi-Fi reconnects for a few seconds while this is applied.\n\nDuring a stream the agent also holds Windows' own Wi-Fi scans off by itself, nothing to grant for that.",
		InstallUSBDriver:     "Install USB Driver",
		GetUSBIPDriver:       "Get USB/IP Driver",
		MoonlightClients:     "Moonlight Clients",
		MoonlightHeader:      "Moonlight client",
		RemoveAllMoonlight:   "Remove all paired Moonlight devices?",
		WebRTCToggle:         "USBridge-streamer Web (WebRTC)",

		Streamer:   "Streamer",
		USBBroker:  "USB Broker",
		HTTP:       "HTTP",
		Sunshine:   "Sunshine",
		SunWeb:     "Sun web",
		Web:        "Web",
		NotRunning: "Not running",
		NotStaged:  "Not staged",

		EnableUSBBroker:        "Enable",
		USBBrokerConsentTitle:  "Enable USB passthrough?",
		USBBrokerConsentBody:   "USB passthrough is powered by a separate, closed-source component (not open-source like the rest of this agent). It stays off until you enable it here. Once enabled, every USB device passes through for free, except pen tablets (Wacom and similar), which require a Pro or Enterprise subscription.",
		USBBrokerRunningOnPort: "Running, listening on port %d.",
		USBBrokerFreeTierNote:  "Every USB device passes through without a subscription, except pen tablets, which require Pro or Enterprise.",
		USBBrokerNotRunning:    "The USB broker is not running. The agent retries automatically every 15 seconds.",
		USBBrokerLastError:     "Last error from the broker:",
		USBPort:                "USB",
		USBPortFallback:        "(%d busy)",

		StreamerConsentTitle: "Switch to USBridge protocol?",
		StreamerConsentBody:  "USBridge is powered by a separate, closed-source streaming component (not open-source like Sunshine and the rest of this agent). Switching to this protocol will download and install the USBridge Streamer component. Do you want to proceed?",

		SignIn:              "Sign In",
		SignOut:             "Sign Out",
		NoRemoteControllers: "No active remote controllers",
		LoginLinkOpened:     "login link opened in browser",
		InvalidLoginURL:     "invalid login URL received",

		BuyPro:        "Buy Pro",
		BuyEnterprise: "Buy Enterprise",
		SupportUs:     "Support us",

		ChangingProtocol:    "Changing protocol...",
		DownloadingNamed:    "Downloading %s...",
		CheckingUpdates:     "Checking for updates...",
		AlreadyUpToDate:     "Already up to date",
		UpdateFailed:        "Update check failed",
		StreamerUpdated:     "USBridge-streamer updated",
		UpdateAvailableHint: "Update available",
		StreamerUpdateAsk:   "A USBridge-streamer update is available. Install now?",

		ErrorTitle:     "Error",
		Yes:            "Yes",
		No:             "No",
		OK:             "OK",
		Cancel:         "Cancel",
		Save:           "Save",
		Update:         "Update",
		NotNow:         "Not Now",
		Download:       "Download",
		LogOut:         "Log out",
		LicenseManager: "License Manager",
		Copy:           "Copy",
		Submit:         "Submit",

		TokenTitle:       "Token",
		CopyLink:         "Copy Link",
		RegenerateKey:    "Regenerate Key",
		QRUnavailable:    "QR link unavailable until the agent has a reachable address.",
		QRUnavailableErr: "QR unavailable: %v",
		TokenUnavail:     "unavailable",

		AccountTitle:            "Account",
		WaitingGoogleLogin:      "Waiting for Google login to complete in your browser…",
		CouldntOpenBrowserLogin: "Couldn't open your browser automatically. Login link:",
		LoginIntro:              "Log in to see your USBridge licenses and sync your saved connections across devices.",
		SignedInAs:              "Signed in as",
		Subscription:            "Subscription",
		Plan:                    "Plan",
		AccountLicense:          "Hardware ID",
		YourLicenses:            "Your licenses",
		Moving:                  "Moving…",
		LogInWithGoogle:         "Log in with Google",
		NoDesktopLicenses:       "No desktop licenses on this account yet.",
		UseLicenseOnDevice:      "Use here",
		LicenseOnThisDevice:     "This device",
		LicenseUsedElsewhere:    "Your %s plan is used on another machine. Move it here?",
		LicenseUsedHere:         "Your %s plan is used on this device.",
		RebindLicenseConfirm:    "Changing to %s will move the license to this PC.",
		AlreadyBoughtIntro:      "Already bought a license on another machine? Log in to move it here.",
		USBridgeAccount:         "USBridge account",
		SubActive:               "Active",
		SubTrial:                "Trial",
		SubNone:                 "None",

		PairMoonlight:    "Pair Moonlight",
		MoonlightPINHint: "Open Moonlight → Add PC → enter the PIN shown there.",
		EnterPINShown:    "Enter the PIN shown in Moonlight",
		PINPlaceholder:   "4-digit PIN from Moonlight",
		NoPairedClients:  "No paired clients",

		SunshineWebUI:      "Sunshine Web UI",
		OpenInBrowser:      "Open in Browser",
		Login:              "Login",
		Password:           "Password",
		URL:                "URL",
		WebClient:          "Web Client",
		WebClientHint:      "Open this link in a browser on any device to stream via USBridge-streamer's built-in WebRTC client — no Moonlight app needed. Uses the same pairing/master key as everything else in this agent.",
		Certificate:        "Certificate",
		CertSelfSigned:     "self-signed",
		CertLetsEncrypt:    "Let's Encrypt",
		CertificateTitle:   "HTTPS Certificate",
		CertificateHint:    "This agent's HTTPS listener needs a certificate a browser will trust without a warning — that's the only way the browser-based Web Client (client/web, loaded from https://web.usbridge.io) is allowed to reach it at all; a self-signed certificate can't be used there, only clicked through manually on this device's own LAN address. Once this device registers with USBridge's backend, it gets a real Let's Encrypt-issued certificate for its own <label>.device.usbridge.io hostname, and the Web Client starts working automatically — no action needed here. Until then, or if registration ever fails, this falls back to a self-signed certificate: everything else (Sunshine, Moonlight, the native app) keeps working normally, only the browser-based Web Client is affected.",
		CertHostnameLabel:  "Hostname",
		CertExpiresLabel:   "Valid until",
		CertPending:        "Registering with USBridge's backend for a trusted hostname — this can take a minute after first launch or a network change.",
		CertErrorPrefix:    "Couldn't get a trusted certificate: ",
		CertRetry:          "Retry",
		CertSignInRequired: "sign in to get a free one",
		CertSignInHint:     "Sign in to your USBridge account to get a free trusted certificate for this device. Without it the browser-based Web Client can't reach this device; everything else keeps working.",
		SunshineStreaming:  "Sunshine Streaming",
		SunshineAdminPort:  "Sunshine Admin Port",
		InvalidPortWide:    "Invalid port (1–65534)",
		InvalidPort:        "Invalid port (1–65535)",
		Host:               "Host",
		Port:               "Port",
		RestartsSunshine:   "Restarts Sunshine to apply",
		SetsExternalIP:     "Sets external_ip + port in sunshine.conf · restarts Sunshine",

		TariffsTitle:               "Tariffs & Licenses",
		TariffSubtitle:             "Upgrade your USBridge agent for low-latency streaming, passthrough & mesh networks",
		CapabilitiesIncluded:       "CAPABILITIES INCLUDED IN THIS TIER",
		CouldntOpenBrowserBuy:      "Couldn't open your browser automatically.",
		SubscribeTitle:             "Subscribe to %s?",
		SubscribeBody:              "Opens Stripe checkout in your browser for the %s subscription. Once payment completes, RustShine downloads and switches on automatically.",
		LicenseDialogTitle:         "USBRIDGE STREAMER — FASTER STREAMING",
		WaitingCheckout:            "Waiting for checkout to complete in your browser…",
		DownloadingStreamer:        "Downloading USBridge Streamer…",
		SettingUp:                  "Setting up…",
		TierActiveDownloading:      "**%s active** 🎉\n\nDownloading RustShine…",
		RustShineProActive:         "**RustShine Pro active** — 4:4:4 color unlocked 🎉",
		RustShineEnterpriseActive:  "**RustShine Enterprise active** 🎉",
		PickLicenseBelow:           "Pick a license below.",
		CouldntOpenBrowserCheckout: "Couldn't open your browser automatically. Checkout link:",
		LowerTierNote:              "Picking a lower tier above only switches the active encoder locally -- it doesn't cancel your subscription. Contact support to cancel or change plans.",
		ForgetLicenseLocally:       "Forget this machine's license locally",
		ForgetLicenseTitle:         "Forget license?",
		ForgetLicenseBody:          "Switches back to Sunshine and forgets the cached license token on this machine only -- it does NOT cancel a paid subscription. Re-opening this dialog immediately re-links to your account's real tier (free, or paid if still active).",
		CheckoutTitle:              "Checkout",
		FeatLowLatency:             "Ultra-low latency streaming",
		FeatLowLatencySub:          "Near-zero delay for mouse and video",
		FeatClipboard:              "Shared clipboard",
		FeatClipboardSub:           "Copy text, images, and files both ways",
		FeatMultiMonitor:           "Multi-monitor support",
		FeatMultiMonitorSub:        "Switch which host display you view",
		FeatWebClient:              "Browser web client",
		FeatWebClientSub:           "Connect from any modern browser",
		FeatPreLogin:               "Windows pre-login access",
		FeatPreLoginSub:            "Reach the host before anyone logs in",
		FeatFastConnect:            "Fast connect",
		FeatFastConnectSub:         "A session starts in seconds",
		FeatVirtualDisplay:         "Virtual displays",
		FeatVirtualDisplaySub:      "Extra screens without extra hardware",
		Feat444:                    "4:4:4 color fidelity",
		Feat444Sub:                 "Full chroma for text and color-critical work",
		FeatWacom:                  "Wacom tablet support",
		FeatWacomSub:               "Pen pressure and tilt pass through to the host",
		FeatRecording:              "Session recording and audit logs",
		FeatRecordingSub:           "Keep a record of every remote session",
		FeatCompanyRollout:         "Built for company-wide rollout",
		FeatCompanyRolloutSub:      "Access and policy at company scale",

		UpdateAvailable:     "Update Available",
		UpdateAvailableBody: "USBridge Agent %s is available (you have %s). Update now?",
		Updating:            "Updating…",
		DownloadingVersion:  "Downloading version %s…",
		WhatsNewTitle:       "What's new",
		WhatsNewGotIt:       "Got it",
		WhatsNewBadge:       "New",
		WhatsNewSubtitle:    "The latest agent features, USB passthrough, and streaming updates.",
		WhatsNewGitHub:      "View Full Changelog on GitHub",

		TrayOpen:         "Open USBridge Agent",
		TrayRestart:      "Restart Streaming",
		TrayCheckUpdate:  "Check for Updates",
		TrayQuit:         "Quit",
		TrayStillRunning: "Still running in the tray — click the tray icon to reopen.",

		RelayDERP:          "Relay (DERP)",
		RelayDERPFmt:       "Relay (DERP %s)",
		NotConnected:       "not connected",
		SignedOut:          "signed out",
		SignInRequired:     "sign in required",
		SignInToPublish:    "sign in to publish this agent",
		Connected:          "connected",
		ServiceUnavailable: "service unavailable",
		LogoutError:        "logout error: %v",
		StartingLogin:      "starting login flow...",
		ErrorFmt:           "error: %v",
	}
}

func ES() *LocalizedStrings {
	locale := EN()
	locale.Language = "Idioma"
	locale.Info = "Info"
	locale.Software = "Software"
	locale.Hardware = "Hardware"
	locale.Website = "Sitio web"
	locale.Theme = "Tema"
	locale.ThemeDefault = "Por defecto"
	locale.InterfaceScale = "Escala de la interfaz"
	locale.GeneralSettings = "Ajustes generales"
	locale.GeneralSettingsSubtitle = "Preferencias para todo el agente."
	locale.AgentAutoUpdate = "Actualizacion automatica del protocolo USBridge"
	locale.RemoteWindowLock = "Bloquear el control remoto de esta ventana"
	locale.RemoteWindowLockHint = "Al activarlo, una sesion remota no podra pulsar la ventana del agente. Tenga cuidado."

	locale.Permissions = "Permisos"
	locale.Status = "Estado"
	locale.Protocol = "Protocolo"
	locale.Graphics = "Gráficos"
	locale.Change = "Cambiar"

	locale.Accessibility = "Accesibilidad"
	locale.InputControl = "Control de entrada"
	locale.ScreenCapture = "Captura de pantalla"
	locale.GrantSuffix = " · Conceder"
	locale.PermGrant = "Conceder"
	locale.PermGranted = "Concedido"
	locale.PermInfo = "Info"
	locale.PunktfunkCaptureInfo = "Punktfunk no tiene captura KMS. Toma la imagen del compositor del escritorio (KWin, GNOME, Sway, Hyprland o gamescope), así que necesita una sesión Wayland en marcha: no puede transmitir la pantalla de inicio de sesión ni nada anterior al inicio de sesión, y no funciona en X11.\n\nLos fotogramas siguen llegando al codificador en la GPU sin copias en la CPU, pero el compositor copia cada fotograma una vez más en la GPU, algo que la captura KMS no necesita. No se ha medido cuánto cuesta eso en este equipo: el benchmark del cliente lo compara con los otros streamers."
	locale.PermDownload = "Descargar"
	locale.AutostartInfo = "Inicio automatico"
	locale.AutostartInfoBody = "1. Coloca el archivo del agente en la carpeta que quieras.\n2. Marca esta casilla: el agente se registra para iniciarse %s.\n\nPara quitarlo del inicio automatico, simplemente desmarca la casilla.\n\nHas movido el archivo del agente a otra carpeta? Desmarca la casilla y vuelve a marcarla para que el inicio automatico use la nueva ubicacion."
	locale.AutostartViaService = "automaticamente como servicio de Windows"
	locale.AutostartViaLaunchd = "automaticamente al iniciar sesion"
	locale.AutostartViaSystemd = "automaticamente al arrancar (servicio systemd)"
	locale.AutostartEntry = "Entrada registrada:"
	locale.AutostartAtBoot = "Inicio automatico"
	locale.AutostartRebootHint = "(se requiere reinicio de Windows)"
	locale.LockGPUClocks = "Bloquear relojes GPU"
	locale.NvidiaMaxPerformance = "NVIDIA: máximo rendimiento (menos latencia)"
	locale.NvPowerMode = "Modo de energía NVIDIA"
	locale.NvPowerMax = "Máximo rendimiento"
	locale.NvPowerConsistent = "Rendimiento constante"
	locale.NvPowerAdaptive = "Adaptativo"
	locale.NvPowerOptimal = "Energía óptima"
	locale.NvPowerDriver = "Según el controlador"
	locale.GPUStreaming = "en transmisión"
	locale.GPUNoEncoderSettings = "Sin ajustes de codificador: los de NVIDIA no se aplican a esta tarjeta"
	locale.NvencTwoPass = "NVENC dos pasadas (mejor imagen, más carga de GPU)"
	locale.ClipboardTool = "Portapapeles"
	locale.Install = "Instalar"
	locale.ClipboardInstall = "Instalar herramienta de portapapeles"
	locale.ClipboardNoPkgMgr = "No se encontro un gestor de paquetes (o pkexec) en este sistema -- Instalar mostrara el motivo, no una vista previa del comando."
	locale.Firewall = "Firewall"
	locale.OpenPorts = "Abrir puertos"
	locale.FirewallInfoTitle = "Abrir puertos del firewall"
	locale.FirewallNoChange = "El firewall ya permite los puertos del agente y del streamer -- no hay nada que cambiar."
	locale.USBPassthrough = "Driver USB Passthrough"
	locale.USBAccess = "USB Passthrough"
	locale.VirtualDisplayAccess = "Display virtual"
	locale.WiFiTuneAccess = "Wi-Fi sin escaneos"
	locale.WiFiTuneInfo = "Los drivers Wi-Fi escanean otros canales en segundo plano y sacan la radio de tu red un momento cada 30-60 s: en un stream se ve como tirones periodicos.\n\nConcede una vez (permisos de administrador) para desactivar esos escaneos para siempre: en Windows el \"Global BG Scan blocking\" del driver Intel pasa a Siempre; en Linux se desactivan el ahorro de energia del Wi-Fi y los escaneos periodicos de iwd. El Wi-Fi se reconecta unos segundos mientras se aplica.\n\nDurante un stream el agente tambien detiene por si mismo los escaneos Wi-Fi de Windows, sin conceder nada."
	locale.InstallUSBDriver = "Instalar driver USB"
	locale.GetUSBIPDriver = "Obtener driver USB/IP"
	locale.MoonlightClients = "Clientes Moonlight"
	locale.MoonlightHeader = "Cliente Moonlight"
	locale.RemoveAllMoonlight = "Quitar todos los dispositivos Moonlight emparejados?"
	locale.WebRTCToggle = "USBridge-streamer Web (WebRTC)"

	locale.NotRunning = "No en ejecucion"
	locale.NotStaged = "No instalado"

	locale.EnableUSBBroker = "Habilitar"
	locale.USBBrokerRunningOnPort = "En ejecución, escuchando en el puerto %d."
	locale.USBBrokerFreeTierNote = "El passthrough de cualquier dispositivo USB funciona sin suscripción, salvo las tabletas gráficas, que requieren Pro o Enterprise."
	locale.USBBrokerNotRunning = "El USB broker no está en ejecución. El agente lo reintenta automáticamente cada 15 segundos."
	locale.USBBrokerLastError = "Último error del broker:"
	locale.USBPortFallback = "(%d ocupado)"
	locale.USBBrokerConsentTitle = "¿Habilitar USB passthrough?"
	locale.USBBrokerConsentBody = "El soporte de USB passthrough funciona mediante un componente independiente de código cerrado (no es de código abierto como el resto de este agente). Permanecerá desactivado hasta que lo habilites aquí. Una vez habilitado, el passthrough de cualquier dispositivo USB es gratuito, salvo las tabletas gráficas (Wacom y similares), que requieren una suscripción Pro o Enterprise."

	locale.StreamerConsentTitle = "¿Cambiar al protocolo USBridge?"
	locale.StreamerConsentBody = "El protocolo USBridge funciona mediante un componente independiente de código cerrado (no es de código abierto como Sunshine y el resto de este agente). Cambiar a este protocolo descargará e instalará el componente USBridge Streamer. ¿Deseas continuar?"

	locale.SignIn = "Entrar"
	locale.SignOut = "Salir"
	locale.NoRemoteControllers = "No hay controladores remotos activos"
	locale.LoginLinkOpened = "enlace de login abierto en el navegador"
	locale.InvalidLoginURL = "URL de login invalida"

	locale.BuyPro = "Comprar Pro"
	locale.BuyEnterprise = "Comprar Enterprise"
	locale.SupportUs = "Apoyanos"

	locale.ChangingProtocol = "Cambiando protocolo..."
	locale.DownloadingNamed = "Descargando %s..."
	locale.CheckingUpdates = "Buscando actualizaciones..."
	locale.AlreadyUpToDate = "Ya esta actualizado"
	locale.UpdateFailed = "Fallo la busqueda de actualizaciones"
	locale.StreamerUpdated = "USBridge-streamer actualizado"
	locale.UpdateAvailableHint = "Actualizacion disponible"
	locale.StreamerUpdateAsk = "Hay una actualizacion de USBridge-streamer. Instalar ahora?"

	locale.ErrorTitle = "Error"
	locale.Yes = "Si"
	locale.No = "No"
	locale.OK = "Aceptar"
	locale.Cancel = "Cancelar"
	locale.Save = "Guardar"
	locale.Update = "Actualizar"
	locale.NotNow = "Ahora no"
	locale.Download = "Descargar"
	locale.LogOut = "Salir"
	locale.LicenseManager = "Gestor de licencias"
	locale.Copy = "Copiar"
	locale.Submit = "Enviar"

	locale.CopyLink = "Copiar enlace"
	locale.RegenerateKey = "Regenerar clave"
	locale.QRUnavailable = "El QR no esta disponible hasta que el agent tenga una direccion alcanzable."
	locale.QRUnavailableErr = "QR no disponible: %v"

	locale.AccountTitle = "Cuenta"
	locale.WaitingGoogleLogin = "Esperando a que termine el login de Google en el navegador…"
	locale.CouldntOpenBrowserLogin = "No se pudo abrir el navegador. Enlace de login:"
	locale.LoginIntro = "Inicia sesion para ver tus licencias USBridge y sincronizar conexiones entre dispositivos."
	locale.SignedInAs = "Sesion iniciada como"
	locale.Subscription = "Suscripcion"
	locale.Plan = "Plan"
	locale.AccountLicense = "Hardware ID"
	locale.YourLicenses = "Tus licencias"
	locale.Moving = "Moviendo…"
	locale.LogInWithGoogle = "Entrar con Google"
	locale.NoDesktopLicenses = "Esta cuenta aun no tiene licencias desktop."
	locale.UseLicenseOnDevice = "Usar aquí"
	locale.LicenseOnThisDevice = "Este dispositivo"
	locale.LicenseUsedElsewhere = "Tu plan %s se usa en otro equipo. Moverlo aqui?"
	locale.LicenseUsedHere = "Tu plan %s se usa en este dispositivo."
	locale.RebindLicenseConfirm = "Al pasar a %s la licencia se movera a este PC."
	locale.AlreadyBoughtIntro = "Ya compraste una licencia en otra maquina? Entra para moverla aqui."
	locale.USBridgeAccount = "Cuenta USBridge"
	locale.SubActive = "Activa"
	locale.SubTrial = "Trial"
	locale.SubNone = "Ninguna"

	locale.PairMoonlight = "Emparejar Moonlight"
	locale.MoonlightPINHint = "Abre Moonlight → Add PC → introduce el PIN que aparece alli."
	locale.EnterPINShown = "Introduce el PIN que muestra Moonlight"
	locale.PINPlaceholder = "PIN de 4 digitos de Moonlight"
	locale.NoPairedClients = "No hay clientes emparejados"

	locale.SunshineWebUI = "Sunshine Web UI"
	locale.OpenInBrowser = "Abrir en el navegador"
	locale.Login = "Login"
	locale.Password = "Password"
	locale.WebClient = "Web Client"
	locale.WebClientHint = "Abre este enlace en un navegador para transmitir con el cliente WebRTC de USBridge-streamer — sin la app Moonlight. Usa la misma master key que el resto del agent."
	locale.Certificate = "Certificado"
	locale.CertSelfSigned = "autofirmado"
	locale.CertLetsEncrypt = "Let's Encrypt"
	locale.CertificateTitle = "Certificado HTTPS"
	locale.CertificateHint = "El listener HTTPS de este agente necesita un certificado en el que un navegador confie sin advertencias — es la unica forma de que el Web Client (client/web, cargado desde https://web.usbridge.io) pueda alcanzarlo. Un certificado autofirmado no sirve ahi, solo aceptandolo a mano en la direccion LAN de este dispositivo. En cuanto este dispositivo se registra con el backend de USBridge, recibe un certificado real emitido por Let's Encrypt para su propio nombre <label>.device.usbridge.io, y el Web Client empieza a funcionar solo — no hace falta nada aqui. Hasta entonces, o si el registro falla, se usa un certificado autofirmado: todo lo demas (Sunshine, Moonlight, la app nativa) sigue funcionando normal, solo el Web Client se ve afectado."
	locale.CertHostnameLabel = "Host"
	locale.CertExpiresLabel = "Valido hasta"
	locale.CertPending = "Registrando con el backend de USBridge para obtener un host de confianza — puede tardar un minuto tras el primer arranque o un cambio de red."
	locale.CertErrorPrefix = "No se pudo obtener un certificado de confianza: "
	locale.CertRetry = "Reintentar"
	locale.CertSignInRequired = "inicia sesion para obtener uno gratis"
	locale.CertSignInHint = "Inicia sesion en tu cuenta de USBridge para obtener un certificado de confianza gratuito para este dispositivo. Sin el, el Web Client del navegador no puede alcanzar este dispositivo; todo lo demas sigue funcionando."
	locale.SunshineStreaming = "Sunshine Streaming"
	locale.SunshineAdminPort = "Sunshine Admin Port"
	locale.InvalidPortWide = "Puerto invalido (1–65534)"
	locale.InvalidPort = "Puerto invalido (1–65535)"
	locale.Host = "Host"
	locale.Port = "Port"
	locale.RestartsSunshine = "Reinicia Sunshine para aplicar"
	locale.SetsExternalIP = "Sets external_ip + port in sunshine.conf · restarts Sunshine"

	locale.TariffsTitle = "Tarifas y licencias"
	locale.TariffSubtitle = "Mejora el agent USBridge para streaming de baja latencia, passthrough y redes mesh"
	locale.CapabilitiesIncluded = "CAPACIDADES INCLUIDAS EN ESTE NIVEL"
	locale.CouldntOpenBrowserBuy = "No se pudo abrir el navegador automaticamente."
	locale.SubscribeTitle = "Suscribirse a %s?"
	locale.SubscribeBody = "Abre el checkout de Stripe en el navegador para la suscripcion %s. Cuando el pago termine, RustShine se descarga y se activa solo."
	locale.LicenseDialogTitle = "USBRIDGE STREAMER — STREAMING MAS RAPIDO"
	locale.WaitingCheckout = "Esperando a que termine el checkout en el navegador…"
	locale.DownloadingStreamer = "Descargando USBridge Streamer…"
	locale.SettingUp = "Configurando…"
	locale.TierActiveDownloading = "**%s activo** 🎉\n\nDescargando RustShine…"
	locale.RustShineProActive = "**RustShine Pro activo** — color 4:4:4 desbloqueado 🎉"
	locale.RustShineEnterpriseActive = "**RustShine Enterprise activo** 🎉"
	locale.PickLicenseBelow = "Elige una licencia abajo."
	locale.CouldntOpenBrowserCheckout = "No se pudo abrir el navegador automaticamente. Enlace de checkout:"
	locale.LowerTierNote = "Elegir un nivel inferior solo cambia el encoder activo en esta maquina -- no cancela la suscripcion. Contacta con soporte para cancelar o cambiar de plan."
	locale.ForgetLicenseLocally = "Olvidar la licencia de esta maquina"
	locale.ForgetLicenseTitle = "Olvidar licencia?"
	locale.ForgetLicenseBody = "Vuelve a Sunshine y olvida el token de licencia en esta maquina -- NO cancela una suscripcion de pago. Al reabrir este dialogo se vuelve a enlazar el nivel real de la cuenta (free, o de pago si sigue activa)."
	locale.CheckoutTitle = "Checkout"
	locale.FeatLowLatency = "Streaming de ultra baja latencia"
	locale.FeatLowLatencySub = "Retraso casi nulo para raton y video"
	locale.FeatClipboard = "Portapapeles compartido"
	locale.FeatClipboardSub = "Copia texto, imagenes y archivos en ambos sentidos"
	locale.FeatMultiMonitor = "Soporte multi-monitor"
	locale.FeatMultiMonitorSub = "Elige que pantalla del host ves"
	locale.FeatWebClient = "Cliente web en el browser"
	locale.FeatWebClientSub = "Conecta desde cualquier navegador moderno"
	locale.FeatPreLogin = "Acceso Windows pre-login"
	locale.FeatPreLoginSub = "Llega al host antes de que alguien inicie sesion"
	locale.FeatFastConnect = "Fast connect"
	locale.FeatFastConnectSub = "La sesion arranca en segundos"
	locale.FeatVirtualDisplay = "Displays virtuales"
	locale.FeatVirtualDisplaySub = "Pantallas extra sin hardware extra"
	locale.Feat444 = "Fidelidad de color 4:4:4"
	locale.Feat444Sub = "Croma completo para texto y trabajo de color"
	locale.FeatWacom = "Soporte de tablet Wacom"
	locale.FeatWacomSub = "Presion y tilt del lapiz llegan al host"
	locale.FeatRecording = "Grabacion de sesion y audit logs"
	locale.FeatRecordingSub = "Guarda un registro de cada sesion remota"
	locale.FeatCompanyRollout = "Pensado para rollout en la empresa"
	locale.FeatCompanyRolloutSub = "Acceso y politicas a escala de empresa"

	locale.UpdateAvailable = "Actualizacion disponible"
	locale.UpdateAvailableBody = "USBridge Agent %s esta disponible (tienes %s). Actualizar ahora?"
	locale.Updating = "Actualizando…"
	locale.DownloadingVersion = "Descargando version %s…"
	locale.WhatsNewTitle = "Novedades"
	locale.WhatsNewGotIt = "Entendido"
	locale.WhatsNewBadge = "Nuevo"
	locale.WhatsNewSubtitle = "Las ultimas funciones del agente, passthrough USB y actualizaciones de streaming."
	locale.WhatsNewGitHub = "Ver changelog completo en GitHub"

	locale.TrayOpen = "Abrir USBridge Agent"
	locale.TrayRestart = "Reiniciar streaming"
	locale.TrayCheckUpdate = "Buscar actualizaciones"
	locale.TrayQuit = "Salir"
	locale.TrayStillRunning = "Sigue en la bandeja — pulsa el icono para reabrir."

	locale.RelayDERP = "Relay (DERP)"
	locale.NotConnected = "no conectado"
	locale.SignedOut = "sesion cerrada"
	locale.SignInRequired = "hace falta iniciar sesion"
	locale.SignInToPublish = "inicia sesion para publicar este agent"
	locale.Connected = "conectado"
	locale.ServiceUnavailable = "servicio no disponible"
	locale.LogoutError = "error al salir: %v"
	locale.StartingLogin = "iniciando login..."
	locale.ErrorFmt = "error: %v"
	return locale
}

func UK() *LocalizedStrings {
	locale := EN()
	locale.Language = "Мова"
	locale.Info = "Інфо"
	locale.Software = "Software"
	locale.Hardware = "Hardware"
	locale.Website = "Сайт"
	locale.Theme = "Тема"
	locale.ThemeDefault = "За замовчуванням"
	locale.InterfaceScale = "Масштаб інтерфейсу"
	locale.GeneralSettings = "Загальні налаштування"
	locale.GeneralSettingsSubtitle = "Параметри для всього агента."
	locale.AgentAutoUpdate = "Автооновлення протоколу USBridge"
	locale.RemoteWindowLock = "Блокувати віддалене керування цим вікном"
	locale.RemoteWindowLockHint = "Коли ввімкнено, з віддаленої сесії не можна натискати вікно агента. Будьте обережні."

	locale.Permissions = "Дозволи"
	locale.Status = "Статус"
	locale.Protocol = "Протокол"
	locale.Graphics = "Графіка"
	locale.Change = "Змінити"

	locale.Accessibility = "Спеціальні можливості"
	locale.InputControl = "Керування введенням"
	locale.ScreenCapture = "Захоплення екрана"
	locale.GrantSuffix = " · Надати"
	locale.PermGrant = "Надати"
	locale.PermGranted = "Надано"
	locale.PermInfo = "Інфо"
	locale.PunktfunkCaptureInfo = "Punktfunk не має захоплення через KMS. Він бере зображення з композитора робочого столу (KWin, GNOME, Sway, Hyprland або gamescope), тому потребує запущеного сеансу Wayland: не може транслювати екран входу чи будь-що до входу в систему і не працює на X11.\n\nКадри, як і раніше, потрапляють до кодувальника на GPU без копіювань на CPU, але композитор ще раз копіює кожен кадр на GPU, чого захоплення через KMS не потребує. Скільки це коштує на цій машині, не виміряно: бенчмарк у клієнті порівнює його з іншими стримерами."
	locale.PermDownload = "Завантажити"
	locale.AutostartInfo = "Автозапуск"
	locale.AutostartInfoBody = "1. Покладіть файл агента в будь-яку папку.\n2. Поставте цю галочку: агент зареєструється, щоб запускатися %s.\n\nЩоб прибрати з автозапуску, просто зніміть галочку.\n\nПеренесли файл агента в іншу папку? Зніміть галочку й поставте її знову, щоб автозапуск підхопив нове розташування."
	locale.AutostartViaService = "автоматично як служба Windows"
	locale.AutostartViaLaunchd = "автоматично під час входу в систему"
	locale.AutostartViaSystemd = "автоматично під час завантаження (служба systemd)"
	locale.AutostartEntry = "Зареєстрований запис:"
	locale.AutostartAtBoot = "Автозапуск"
	locale.AutostartRebootHint = "(потрібен перезапуск Windows)"
	locale.LockGPUClocks = "Фіксувати частоти GPU"
	locale.NvidiaMaxPerformance = "NVIDIA: максимальна продуктивність (менша затримка)"
	locale.NvPowerMode = "Режим живлення NVIDIA"
	locale.NvPowerMax = "Максимальна продуктивність"
	locale.NvPowerConsistent = "Стабільна продуктивність"
	locale.NvPowerAdaptive = "Адаптивний"
	locale.NvPowerOptimal = "Оптимальне енергоспоживання"
	locale.NvPowerDriver = "Як у драйвері"
	locale.GPUStreaming = "стрім"
	locale.GPUNoEncoderSettings = "Налаштувань енкодера немає: налаштування NVIDIA на цю карту не впливають"
	locale.NvencTwoPass = "NVENC два проходи (краща картинка, більше навантаження на GPU)"
	locale.ClipboardTool = "Буфер обміну"
	locale.Install = "Встановити"
	locale.ClipboardInstall = "Встановлення буфера обміну"
	locale.ClipboardNoPkgMgr = "Не знайдено менеджер пакетів (або pkexec) — Встановити покаже причину, а не попередній перегляд команди."
	locale.Firewall = "Фаєрвол"
	locale.OpenPorts = "Відкрити порти"
	locale.FirewallInfoTitle = "Відкриття портів фаєрвола"
	locale.FirewallNoChange = "Фаєрвол уже пропускає порти агента і стрімера — змінювати нічого."
	locale.USBPassthrough = "USB Passthrough Driver"
	locale.USBAccess = "USB Passthrough"
	locale.VirtualDisplayAccess = "Віртуальний дисплей"
	locale.WiFiTuneAccess = "Wi-Fi без сканувань"
	locale.WiFiTuneInfo = "Драйвери Wi-Fi постійно сканують інші канали у фоні й щоразу на мить відводять радіо від вашої мережі, кожні 30-60 с: на стрімі це періодичні ривки.\n\nНадайте один раз (права адміністратора), щоб вимкнути ці сканування назавжди: у Windows параметр драйвера Intel \"Global BG Scan blocking\" стає Always; у Linux вимикаються енергозбереження Wi-Fi і періодичні сканування iwd. Поки це застосовується, Wi-Fi перепідключиться на кілька секунд.\n\nПід час стріму агент і сам вимикає власні сканування Wi-Fi у Windows, для цього нічого надавати не треба."
	locale.InstallUSBDriver = "Встановити USB driver"
	locale.GetUSBIPDriver = "Отримати USB/IP Driver"
	locale.MoonlightClients = "Клієнти Moonlight"
	locale.MoonlightHeader = "Клієнт Moonlight"
	locale.RemoveAllMoonlight = "Від’єднати всі спарені пристрої Moonlight?"
	locale.WebRTCToggle = "USBridge-streamer Web (WebRTC)"

	locale.NotRunning = "Не запущено"
	locale.NotStaged = "Не встановлено"

	locale.EnableUSBBroker = "Увімкнути"
	locale.USBBrokerRunningOnPort = "Запущено, слухає порт %d."
	locale.USBBrokerFreeTierNote = "Прокидання будь-яких USB-пристроїв працює без підписки, окрім графічних планшетів, які потребують Pro або Enterprise."
	locale.USBBrokerNotRunning = "USB broker не запущено. Агент автоматично повторює спробу кожні 15 секунд."
	locale.USBBrokerLastError = "Остання помилка брокера:"
	locale.USBPortFallback = "(%d зайнятий)"
	locale.USBBrokerConsentTitle = "Увімкнути USB passthrough?"
	locale.USBBrokerConsentBody = "Прокидання USB (USB passthrough) працює на окремому компоненті із закритим вихідним кодом (не open-source, на відміну від решти агента). Він залишається вимкненим, доки ви не увімкнете його тут. Після увімкнення прокидання будь-яких USB-пристроїв безкоштовне, окрім графічних планшетів (Wacom тощо), які потребують підписки Pro або Enterprise."

	locale.StreamerConsentTitle = "Перемкнути на протокол USBridge?"
	locale.StreamerConsentBody = "Протокол USBridge працює на окремому компоненті із закритим вихідним кодом (не open-source, на відміну від Sunshine та решти агента). Перемикання на цей протокол завантажить та встановить USBridge Streamer. Бажаєте продовжити?"

	locale.SignIn = "Увійти"
	locale.SignOut = "Вийти"
	locale.NoRemoteControllers = "Немає активних віддалених контролерів"
	locale.LoginLinkOpened = "посилання логіну відкрито в браузері"
	locale.InvalidLoginURL = "отримана некоректна URL логіну"

	locale.BuyPro = "Купити Pro"
	locale.BuyEnterprise = "Купити Enterprise"
	locale.SupportUs = "Підтримати"

	locale.ChangingProtocol = "Зміна протоколу..."
	locale.DownloadingNamed = "Завантаження %s..."
	locale.CheckingUpdates = "Перевірка оновлень..."
	locale.AlreadyUpToDate = "Вже остання версія"
	locale.UpdateFailed = "Не вдалося перевірити оновлення"
	locale.StreamerUpdated = "USBridge-streamer оновлено"
	locale.UpdateAvailableHint = "Доступне оновлення"
	locale.StreamerUpdateAsk = "Доступне оновлення USBridge-streamer. Встановити зараз?"

	locale.ErrorTitle = "Помилка"
	locale.Yes = "Так"
	locale.No = "Ні"
	locale.OK = "OK"
	locale.Cancel = "Скасувати"
	locale.Save = "Зберегти"
	locale.Update = "Оновити"
	locale.NotNow = "Не зараз"
	locale.Download = "Завантажити"
	locale.LogOut = "Вийти"
	locale.LicenseManager = "Менеджер ліцензій"
	locale.Copy = "Копіювати"
	locale.Submit = "Надіслати"

	locale.CopyLink = "Копіювати посилання"
	locale.RegenerateKey = "Перегенерувати ключ"
	locale.QRUnavailable = "QR недоступний, доки agent не матиме досяжної адреси."
	locale.QRUnavailableErr = "QR недоступний: %v"

	locale.AccountTitle = "Акаунт"
	locale.WaitingGoogleLogin = "Чекаємо завершення логіну Google у браузері…"
	locale.CouldntOpenBrowserLogin = "Не вдалося відкрити браузер. Посилання логіну:"
	locale.LoginIntro = "Увійдіть, щоб бачити ліцензії USBridge і синхронізувати з’єднання між пристроями."
	locale.SignedInAs = "Увійшли як"
	locale.Subscription = "Підписка"
	locale.Plan = "План"
	locale.AccountLicense = "Hardware ID"
	locale.YourLicenses = "Ваші ліцензії"
	locale.Moving = "Перенесення…"
	locale.LogInWithGoogle = "Увійти з Google"
	locale.NoDesktopLicenses = "На цьому акаунті ще немає desktop-ліцензій."
	locale.UseLicenseOnDevice = "Використати тут"
	locale.LicenseOnThisDevice = "Цей пристрій"
	locale.LicenseUsedElsewhere = "Ваш план %s використовується на іншому комп’ютері. Перенести сюди?"
	locale.LicenseUsedHere = "Ваш план %s використовується на цьому пристрої."
	locale.RebindLicenseConfirm = "Перехід на %s перенесе ліцензію на цей ПК."
	locale.AlreadyBoughtIntro = "Вже купили ліцензію на іншій машині? Увійдіть, щоб перенести її сюди."
	locale.USBridgeAccount = "Акаунт USBridge"
	locale.SubActive = "Активна"
	locale.SubTrial = "Trial"
	locale.SubNone = "Немає"

	locale.PairMoonlight = "Спарити Moonlight"
	locale.MoonlightPINHint = "Відкрийте Moonlight → Add PC → введіть PIN, показаний там."
	locale.EnterPINShown = "Введіть PIN, який показує Moonlight"
	locale.PINPlaceholder = "4-значний PIN з Moonlight"
	locale.NoPairedClients = "Немає спарених клієнтів"

	locale.SunshineWebUI = "Sunshine Web UI"
	locale.OpenInBrowser = "Відкрити в браузері"
	locale.Login = "Login"
	locale.Password = "Password"
	locale.WebClient = "Web Client"
	locale.WebClientHint = "Відкрийте це посилання в браузері, щоб стрімити через WebRTC-клієнт USBridge-streamer — без застосунку Moonlight. Той самий master key, що й у решти agent."
	locale.Certificate = "Сертифікат"
	locale.CertSelfSigned = "самопідписаний"
	locale.CertLetsEncrypt = "Let's Encrypt"
	locale.CertificateTitle = "HTTPS-сертифікат"
	locale.CertificateHint = "HTTPS-слухачу цього агента потрібен сертифікат, якому браузер довіряє без попередження — лише так Web Client (client/web, завантажений з https://web.usbridge.io) може взагалі до нього достукатися. Самопідписаний сертифікат тут не підходить — його можна прийняти вручну лише за LAN-адресою цього пристрою. Щойно пристрій зареєструється в бекенді USBridge, він отримує справжній сертифікат Let's Encrypt для власного імені <label>.device.usbridge.io, і Web Client запрацює сам — тут нічого робити не треба. До того часу, або якщо реєстрація не вдасться, використовується самопідписаний сертифікат: усе інше (Sunshine, Moonlight, нативний застосунок) працює як завжди, лише Web Client буде недоступний."
	locale.CertHostnameLabel = "Хост"
	locale.CertExpiresLabel = "Дійсний до"
	locale.CertPending = "Реєстрація в бекенді USBridge для отримання довіреного хоста — це може зайняти хвилину після першого запуску або зміни мережі."
	locale.CertErrorPrefix = "Не вдалося отримати довірений сертифікат: "
	locale.CertRetry = "Повторити"
	locale.CertSignInRequired = "увійдіть, щоб отримати безкоштовний"
	locale.CertSignInHint = "Увійдіть в обліковий запис USBridge, щоб отримати безкоштовний довірений сертифікат для цього пристрою. Без нього Web Client у браузері не може достукатися до пристрою; усе інше працює як завжди."
	locale.SunshineStreaming = "Sunshine Streaming"
	locale.SunshineAdminPort = "Sunshine Admin Port"
	locale.InvalidPortWide = "Некоректний порт (1–65534)"
	locale.InvalidPort = "Некоректний порт (1–65535)"
	locale.Host = "Host"
	locale.Port = "Port"
	locale.RestartsSunshine = "Перезапускає Sunshine, щоб застосувати"
	locale.SetsExternalIP = "Sets external_ip + port in sunshine.conf · restarts Sunshine"

	locale.TariffsTitle = "Тарифи та ліцензії"
	locale.TariffSubtitle = "Оновіть USBridge agent для стріму з низькою затримкою, passthrough і mesh-мереж"
	locale.CapabilitiesIncluded = "МОЖЛИВОСТІ ЦЬОГО РІВНЯ"
	locale.CouldntOpenBrowserBuy = "Не вдалося відкрити браузер автоматично."
	locale.SubscribeTitle = "Підписатися на %s?"
	locale.SubscribeBody = "Відкриває Stripe checkout у браузері для підписки %s. Після оплати RustShine завантажиться і ввімкнеться сам."
	locale.LicenseDialogTitle = "USBRIDGE STREAMER — ШВИДШИЙ СТРІМ"
	locale.WaitingCheckout = "Чекаємо завершення checkout у браузері…"
	locale.DownloadingStreamer = "Завантаження USBridge Streamer…"
	locale.SettingUp = "Налаштування…"
	locale.TierActiveDownloading = "**%s активний** 🎉\n\nЗавантаження RustShine…"
	locale.RustShineProActive = "**RustShine Pro активний** — колір 4:4:4 розблоковано 🎉"
	locale.RustShineEnterpriseActive = "**RustShine Enterprise активний** 🎉"
	locale.PickLicenseBelow = "Оберіть ліцензію нижче."
	locale.CouldntOpenBrowserCheckout = "Не вдалося відкрити браузер автоматично. Посилання checkout:"
	locale.LowerTierNote = "Нижчий рівень лише перемикає активний encoder на цій машині — підписку не скасовує. Щоб скасувати або змінити план, зверніться в підтримку."
	locale.ForgetLicenseLocally = "Забути локальну ліцензію цієї машини"
	locale.ForgetLicenseTitle = "Забути ліцензію?"
	locale.ForgetLicenseBody = "Повертає Sunshine і забуває кешований license token лише на цій машині — платну підписку НЕ скасовує. Якщо знову відкрити цей діалог, знову підтягнеться реальний рівень акаунта (free або платний, якщо він ще активний)."
	locale.CheckoutTitle = "Checkout"
	locale.FeatLowLatency = "Стрім з ультранизькою затримкою"
	locale.FeatLowLatencySub = "Майже нульова затримка миші та відео"
	locale.FeatClipboard = "Спільний буфер обміну"
	locale.FeatClipboardSub = "Копіювання тексту, зображень і файлів в обидва боки"
	locale.FeatMultiMonitor = "Підтримка кількох моніторів"
	locale.FeatMultiMonitorSub = "Оберіть, який екран хоста дивитесь"
	locale.FeatWebClient = "Веб-клієнт у браузері"
	locale.FeatWebClientSub = "Підключення з будь-якого сучасного браузера"
	locale.FeatPreLogin = "Доступ Windows до логіну"
	locale.FeatPreLoginSub = "Доступ до хоста до входу користувача"
	locale.FeatFastConnect = "Fast connect"
	locale.FeatFastConnectSub = "Сесія стартує за секунди"
	locale.FeatVirtualDisplay = "Віртуальні дисплеї"
	locale.FeatVirtualDisplaySub = "Додаткові екрани без зайвого заліза"
	locale.Feat444 = "Колір 4:4:4"
	locale.Feat444Sub = "Повна хрома для тексту і роботи з кольором"
	locale.FeatWacom = "Підтримка планшета Wacom"
	locale.FeatWacomSub = "Тиск і нахил пера передаються на хост"
	locale.FeatRecording = "Запис сесій і аудит"
	locale.FeatRecordingSub = "Запис кожної віддаленої сесії"
	locale.FeatCompanyRollout = "Для розгортання в компанії"
	locale.FeatCompanyRolloutSub = "Доступ і політики в масштабі компанії"

	locale.UpdateAvailable = "Доступне оновлення"
	locale.UpdateAvailableBody = "USBridge Agent %s доступний (у вас %s). Оновити зараз?"
	locale.Updating = "Оновлення…"
	locale.DownloadingVersion = "Завантаження версії %s…"
	locale.WhatsNewTitle = "Що нового"
	locale.WhatsNewGotIt = "Зрозуміло"
	locale.WhatsNewBadge = "Нове"
	locale.WhatsNewSubtitle = "Нові функції агента, прокидання USB і оновлення стрімінгу."
	locale.WhatsNewGitHub = "Повний changelog на GitHub"

	locale.TrayOpen = "Відкрити USBridge Agent"
	locale.TrayRestart = "Перезапустити стрім"
	locale.TrayCheckUpdate = "Перевірити оновлення"
	locale.TrayQuit = "Вийти"
	locale.TrayStillRunning = "Працює в треї — натисніть іконку, щоб відкрити знову."

	locale.RelayDERP = "Relay (DERP)"
	locale.NotConnected = "не підключено"
	locale.SignedOut = "вийшли"
	locale.SignInRequired = "потрібен вхід"
	locale.SignInToPublish = "увійдіть, щоб опублікувати цей agent"
	locale.Connected = "підключено"
	locale.ServiceUnavailable = "сервіс недоступний"
	locale.LogoutError = "помилка виходу: %v"
	locale.StartingLogin = "запуск логіну..."
	locale.ErrorFmt = "помилка: %v"
	return locale
}
