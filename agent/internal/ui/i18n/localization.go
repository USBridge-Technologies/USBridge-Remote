package i18n

// LocalizedStrings is the agent's UI copy. Technical names stay English in
// every language: Sunshine, Moonlight, Tailscale, Token, PIN, USB, GPU,
// HTTP, WebRTC, Web, Streamer, GitHub, Pro, Enterprise, Free, Open Source.
type LocalizedStrings struct {
	AppTitle string

	// Settings / Info / Theme
	Language     string
	Info         string
	Software     string
	Hardware     string
	Website      string
	Theme        string
	ThemeDefault string

	// Cards
	Permissions string
	Status      string
	Protocol    string
	Change      string

	// Permissions
	Accessibility       string
	InputControl        string
	ScreenCapture       string
	GrantSuffix         string
	AutostartAtBoot     string
	AutostartRebootHint string
	LockGPUClocks       string
	ClipboardTool       string
	Install             string
	ClipboardInstall    string
	ClipboardNoPkgMgr   string
	USBPassthrough      string
	InstallUSBDriver    string
	GetUSBIPDriver      string
	MoonlightClients    string
	RemoveAllMoonlight  string
	WebRTCToggle        string

	// Status rows (technical labels stay English)
	Streamer   string
	USBBroker  string
	HTTP       string
	Sunshine   string
	SunWeb     string
	Web        string
	NotRunning string
	NotStaged  string

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
	CheckingUpdates  string
	AlreadyUpToDate  string
	UpdateFailed     string

	// Common
	Yes      string
	No       string
	Cancel   string
	Save     string
	Update   string
	NotNow   string
	Download string
	LogOut   string
	Copy     string
	Submit   string

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
	YourLicenses            string
	Moving                  string
	LogInWithGoogle         string
	NoDesktopLicenses       string
	UseLicenseOnDevice      string
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
	SunshineWebUI     string
	OpenInBrowser     string
	Login             string
	Password          string
	URL               string
	WebClient         string
	WebClientHint     string
	SunshineStreaming string
	SunshineAdminPort string
	InvalidPortWide   string
	InvalidPort       string
	Host              string
	Port              string
	RestartsSunshine  string
	SetsExternalIP    string

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
	FeatUSB                    string
	FeatUSBSub                 string
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

	// Tray
	TrayOpen         string
	TrayRestart      string
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

func Init(language string) {
	switch language {
	case "es", "ES":
		Current = ES()
	case "uk", "UK", "ua", "UA":
		Current = UK()
	default:
		Current = EN()
	}
}

func SetLanguage(language string) {
	Init(language)
}

func EN() *LocalizedStrings {
	return &LocalizedStrings{
		AppTitle: "USBridge Agent",

		Language:     "Language",
		Info:         "Info",
		Software:     "Software",
		Hardware:     "Hardware",
		Website:      "Website",
		Theme:        "Theme",
		ThemeDefault: "Default",

		Permissions: "Permissions",
		Status:      "Status",
		Protocol:    "Protocol",
		Change:      "Change",

		Accessibility:       "Accessibility",
		InputControl:        "Input Control",
		ScreenCapture:       "Screen Capture",
		GrantSuffix:         " · Grant",
		AutostartAtBoot:     "Autostart at Boot",
		AutostartRebootHint: "(Windows restart required)",
		LockGPUClocks:       "Lock GPU Clocks",
		ClipboardTool:       "Clipboard Tool",
		Install:             "Install",
		ClipboardInstall:    "Clipboard Tool Install",
		ClipboardNoPkgMgr:   "No supported package manager (or pkexec) was found on this system -- clicking Install will show why, instead of a command preview.",
		USBPassthrough:      "USB Passthrough Driver",
		InstallUSBDriver:    "Install USB Driver",
		GetUSBIPDriver:      "Get USB/IP Driver",
		MoonlightClients:    "Moonlight Clients",
		RemoveAllMoonlight:  "Remove all paired Moonlight devices?",
		WebRTCToggle:        "USBridge-streamer Web (WebRTC)",

		Streamer:   "Streamer",
		USBBroker:  "USB Broker",
		HTTP:       "HTTP",
		Sunshine:   "Sunshine",
		SunWeb:     "Sun web",
		Web:        "Web",
		NotRunning: "Not running",
		NotStaged:  "Not staged",

		SignIn:              "Sign In",
		SignOut:             "Sign Out",
		NoRemoteControllers: "No active remote controllers",
		LoginLinkOpened:     "login link opened in browser",
		InvalidLoginURL:     "invalid login URL received",

		BuyPro:        "Buy Pro",
		BuyEnterprise: "Buy Enterprise",
		SupportUs:     "Support us",

		ChangingProtocol: "Changing protocol...",
		CheckingUpdates:  "Checking for updates...",
		AlreadyUpToDate:  "Already up to date",
		UpdateFailed:     "Update check failed",

		Yes:      "Yes",
		No:       "No",
		Cancel:   "Cancel",
		Save:     "Save",
		Update:   "Update",
		NotNow:   "Not Now",
		Download: "Download",
		LogOut:   "Log out",
		Copy:     "Copy",
		Submit:   "Submit",

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
		YourLicenses:            "Your licenses",
		Moving:                  "Moving…",
		LogInWithGoogle:         "Log in with Google",
		NoDesktopLicenses:       "No desktop licenses on this account yet.",
		UseLicenseOnDevice:      "Use here",
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

		SunshineWebUI:     "Sunshine Web UI",
		OpenInBrowser:     "Open in Browser",
		Login:             "Login",
		Password:          "Password",
		URL:               "URL",
		WebClient:         "Web Client",
		WebClientHint:     "Open this link in a browser on any device to stream via USBridge-streamer's built-in WebRTC client — no Moonlight app needed. Uses the same pairing/master key as everything else in this agent.",
		SunshineStreaming: "Sunshine Streaming",
		SunshineAdminPort: "Sunshine Admin Port",
		InvalidPortWide:   "Invalid port (1–65534)",
		InvalidPort:       "Invalid port (1–65535)",
		Host:              "Host",
		Port:              "Port",
		RestartsSunshine:  "Restarts Sunshine to apply",
		SetsExternalIP:    "Sets external_ip + port in sunshine.conf · restarts Sunshine",

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
		FeatUSB:                    "USB device emulation",
		FeatUSBSub:                 "Pass local USB devices through to the host",
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

		TrayOpen:         "Open USBridge Agent",
		TrayRestart:      "Restart Streaming",
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

	locale.Permissions = "Permisos"
	locale.Status = "Estado"
	locale.Protocol = "Protocolo"
	locale.Change = "Cambiar"

	locale.Accessibility = "Accesibilidad"
	locale.InputControl = "Control de entrada"
	locale.ScreenCapture = "Captura de pantalla"
	locale.GrantSuffix = " · Conceder"
	locale.AutostartAtBoot = "Inicio automatico"
	locale.AutostartRebootHint = "(se requiere reinicio de Windows)"
	locale.LockGPUClocks = "Bloquear relojes GPU"
	locale.ClipboardTool = "Portapapeles"
	locale.Install = "Instalar"
	locale.ClipboardInstall = "Instalar herramienta de portapapeles"
	locale.ClipboardNoPkgMgr = "No se encontro un gestor de paquetes (o pkexec) en este sistema -- Instalar mostrara el motivo, no una vista previa del comando."
	locale.USBPassthrough = "Driver USB Passthrough"
	locale.InstallUSBDriver = "Instalar driver USB"
	locale.GetUSBIPDriver = "Obtener driver USB/IP"
	locale.MoonlightClients = "Clientes Moonlight"
	locale.RemoveAllMoonlight = "Quitar todos los dispositivos Moonlight emparejados?"
	locale.WebRTCToggle = "USBridge-streamer Web (WebRTC)"

	locale.NotRunning = "No en ejecucion"
	locale.NotStaged = "No instalado"

	locale.SignIn = "Entrar"
	locale.SignOut = "Salir"
	locale.NoRemoteControllers = "No hay controladores remotos activos"
	locale.LoginLinkOpened = "enlace de login abierto en el navegador"
	locale.InvalidLoginURL = "URL de login invalida"

	locale.BuyPro = "Comprar Pro"
	locale.BuyEnterprise = "Comprar Enterprise"
	locale.SupportUs = "Apoyanos"

	locale.ChangingProtocol = "Cambiando protocolo..."
	locale.CheckingUpdates = "Buscando actualizaciones..."
	locale.AlreadyUpToDate = "Ya esta actualizado"
	locale.UpdateFailed = "Fallo la busqueda de actualizaciones"

	locale.Yes = "Si"
	locale.No = "No"
	locale.Cancel = "Cancelar"
	locale.Save = "Guardar"
	locale.Update = "Actualizar"
	locale.NotNow = "Ahora no"
	locale.Download = "Descargar"
	locale.LogOut = "Salir"
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
	locale.YourLicenses = "Tus licencias"
	locale.Moving = "Moviendo…"
	locale.LogInWithGoogle = "Entrar con Google"
	locale.NoDesktopLicenses = "Esta cuenta aun no tiene licencias desktop."
	locale.UseLicenseOnDevice = "Usar aquí"
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
	locale.FeatUSB = "Emulacion USB"
	locale.FeatUSBSub = "Pasa dispositivos USB locales al host"
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

	locale.TrayOpen = "Abrir USBridge Agent"
	locale.TrayRestart = "Reiniciar streaming"
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

	locale.Permissions = "Дозволи"
	locale.Status = "Статус"
	locale.Protocol = "Протокол"
	locale.Change = "Змінити"

	locale.Accessibility = "Спеціальні можливості"
	locale.InputControl = "Керування введенням"
	locale.ScreenCapture = "Захоплення екрана"
	locale.GrantSuffix = " · Надати"
	locale.AutostartAtBoot = "Автозапуск"
	locale.AutostartRebootHint = "(потрібен перезапуск Windows)"
	locale.LockGPUClocks = "Фіксувати частоти GPU"
	locale.ClipboardTool = "Буфер обміну"
	locale.Install = "Встановити"
	locale.ClipboardInstall = "Встановлення буфера обміну"
	locale.ClipboardNoPkgMgr = "Не знайдено менеджер пакетів (або pkexec) — Встановити покаже причину, а не попередній перегляд команди."
	locale.USBPassthrough = "USB Passthrough Driver"
	locale.InstallUSBDriver = "Встановити USB driver"
	locale.GetUSBIPDriver = "Отримати USB/IP Driver"
	locale.MoonlightClients = "Клієнти Moonlight"
	locale.RemoveAllMoonlight = "Від’єднати всі спарені пристрої Moonlight?"
	locale.WebRTCToggle = "USBridge-streamer Web (WebRTC)"

	locale.NotRunning = "Не запущено"
	locale.NotStaged = "Не встановлено"

	locale.SignIn = "Увійти"
	locale.SignOut = "Вийти"
	locale.NoRemoteControllers = "Немає активних віддалених контролерів"
	locale.LoginLinkOpened = "посилання логіну відкрито в браузері"
	locale.InvalidLoginURL = "отримана некоректна URL логіну"

	locale.BuyPro = "Купити Pro"
	locale.BuyEnterprise = "Купити Enterprise"
	locale.SupportUs = "Підтримати"

	locale.ChangingProtocol = "Зміна протоколу..."
	locale.CheckingUpdates = "Перевірка оновлень..."
	locale.AlreadyUpToDate = "Вже остання версія"
	locale.UpdateFailed = "Не вдалося перевірити оновлення"

	locale.Yes = "Так"
	locale.No = "Ні"
	locale.Cancel = "Скасувати"
	locale.Save = "Зберегти"
	locale.Update = "Оновити"
	locale.NotNow = "Не зараз"
	locale.Download = "Завантажити"
	locale.LogOut = "Вийти"
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
	locale.YourLicenses = "Ваші ліцензії"
	locale.Moving = "Перенесення…"
	locale.LogInWithGoogle = "Увійти з Google"
	locale.NoDesktopLicenses = "На цьому акаунті ще немає desktop-ліцензій."
	locale.UseLicenseOnDevice = "Використати тут"
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
	locale.FeatUSB = "Емуляція USB"
	locale.FeatUSBSub = "Прокидання локальних USB-пристроїв на хост"
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

	locale.TrayOpen = "Відкрити USBridge Agent"
	locale.TrayRestart = "Перезапустити стрім"
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
