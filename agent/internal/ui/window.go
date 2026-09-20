package ui

import (
	"context"
	"fmt"
	"image/color"
	"log"
	"net"
	"net/url"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/sirupsen/logrus"

	"usbridge_agent/assets"
	"usbridge_agent/internal/account"
	"usbridge_agent/internal/autostart"
	"usbridge_agent/internal/capture"
	"usbridge_agent/internal/config"
	"usbridge_agent/internal/entitlement"
	"usbridge_agent/internal/netutil"
	"usbridge_agent/internal/remotelock"
	"usbridge_agent/internal/streamhost"
	"usbridge_agent/internal/tailscale"
	"usbridge_agent/internal/ui/design"
	"usbridge_agent/internal/ui/i18n"
	"usbridge_agent/internal/update"
	"usbridge_agent/internal/usbpass"
)

// TokenProvider is whatever owns the agent's config/Sunshine lifecycle —
// normally *app.App when the GUI starts its own engine, or *adminapi.Client
// when it's attaching to an already-running headless instance instead.
type TokenProvider interface {
	RegenerateMasterKey() (config.Config, error)
	SaveConfig(config.Config) error
	SunshineCaptureMode() string
	SetSunshineCaptureMode(mode string) error
	KMSCaptureGranted() bool
	RequestKMSCapture() bool
	GPUClockLockSupported() bool
	LockGPUClocksEnabled() bool
	SetLockGPUClocksEnabled(enabled bool) error
	StreamerAutoUpdateEnabled() bool
	SetStreamerAutoUpdate(enabled bool) error
	SnoozeStreamerUpdate(version string) error
	RemoteWindowLockEnabled() bool
	SetRemoteWindowLock(enabled bool) error
	RestartSunshine() error
	ListSunshineClients() ([]streamhost.Client, error)
	UnpairSunshineClient(uniqueID string) error
	SubmitMoonlightPIN(pin string) error
	UpdateListenAddr(host string, port int) (config.Config, error)
	UpdateSunshinePort(port int) (config.Config, error)
	UpdateSunshineStreamAddr(host string, streamPort int) (config.Config, error)
	SunshineStreamHost() string
	AdminUser() string
	AdminPass() string
	StreamerName() string
	// StreamerRunning reports whether the active streaming host's own child
	// process is alive right now -- for the status traffic light next to
	// streamerNameLabel, distinct from whether it's staged/entitled at all.
	StreamerRunning() bool

	// Hardware-bound RustShine entitlement (see internal/entitlement,
	// internal/hwid).
	EntitlementStatus() entitlement.Status
	StartFreeTrial() error
	StartPurchase(tier string) (string, error)
	CancelPurchase()
	ClearLicense() error
	DownloadRustShine(onProgress entitlement.ProgressFunc) error
	CheckRustShineUpdateNow() error
	SetStreamBackend(kind string) error
	SetRustShineWebRTCEnabled(enabled bool) error

	// USB passthrough (see internal/usbpass) -- gated by the same
	// entitlement token as RustShine, staged on the same DownloadRustShine
	// click. USBPassthroughStatus is polled for driver-install UI; see
	// refreshRustShineUI.
	USBPassthroughStatus() usbpass.Status
	InstallUSBDriver() error
	// GrantUSBAttach: Linux one-time polkit grant so usbip attach/detach
	// stop prompting for a password (see usbpass/access_linux.go).
	GrantUSBAttach() error

	// Account login (see internal/account) -- a separate identity from the
	// hardware-bound entitlement above, used only to pick which of the
	// logged-in account's own desktop licenses to rebind onto this
	// machine. See account.Status's own doc comment.
	AccountStatus() account.Status
	StartAccountLogin() (string, error)
	CancelAccountLogin()
	RefreshAccountLicenses()
	RebindLicenseToThisDevice(oldIdentifier string) error
	LogoutAccount() error
}

// PermsProvider is satisfied by *permissions.Service (embedded engine) or
// *adminapi.Client (thin client attaching to a headless instance).
type PermsProvider interface {
	AccessibilityGranted() bool
	ScreenRecordingGranted() bool
	RequestAccessibility() bool
	RequestScreenRecording() bool
	OpenPrivacySettings() error
	OpenScreenRecordingSettings() error

	// ClipboardToolAvailable/RequestClipboardTool: Linux only (a CLI
	// clipboard helper -- xclip/wl-clipboard/xsel -- is what clipboard sync
	// actually shells out to there, see internal/clipboard/backend_linux.go);
	// every other platform's implementation just returns true/true.
	ClipboardToolAvailable() bool
	RequestClipboardTool() bool

	// ClipboardInstallPreview returns the exact command the "Install"
	// button's click would run (via pkexec), for its "?" tooltip -- "" if
	// nothing would actually run (no pkexec / no supported package manager
	// found), in which case the tooltip is skipped and the button's own
	// click surfaces that reason instead.
	ClipboardInstallPreview() string
}

// TailscaleProvider is satisfied by *tailscale.Service (embedded engine) or
// *adminapi.Client (thin client attaching to a headless instance).
type TailscaleProvider interface {
	Status(context.Context) (*tailscale.Status, error)
	StartLogin(context.Context) (string, error)
	Logout(context.Context) error
	SetAuthURLHandler(func(string))
}

// appVersion is set once at startup via SetAppVersion (ldflags -X in each
// platform's build script) and shown as a small "vX.Y.Z" tag in the
// bottom-right corner of the main window.
var appVersion string

// SetAppVersion records the running build's version string.
func SetAppVersion(v string) {
	appVersion = strings.TrimSpace(v)
}

type Window struct {
	app   fyne.App
	cfg   config.Config
	token TokenProvider
	perms PermsProvider
	ts    TailscaleProvider

	// awaitingLocalLogin is true only while the local "Sign In With Google"
	// button has an interactive login in flight. It gates auto-opening a
	// browser from the AuthURL handler: tsnet can also produce an AuthURL from
	// a remote client's sync/register request or from its own first-boot
	// auto-login, and those must NOT pop a browser on this (possibly headless,
	// possibly actively streaming) machine — only the local button's own
	// request should.
	awaitingLocalLogin atomic.Bool

	// UI components
	accessCheck *permStatusChip
	// usbAccessCheck: Linux + USB-passthrough license only -- separate
	// Permissions row whose button does the one-time polkit grant so usbip
	// attach/detach stop prompting; hidden once granted (refreshUSBPassthroughUI).
	usbAccessCheck *permStatusChip

	// Screen Capture: a single unified control for how video gets captured.
	// On Linux this is Sunshine's capture backend, picked automatically from
	// the live session (capture.AutoCaptureMode) — no user choice, since a
	// manual "KMS (root)" pick on an X11 desktop session (esp. NVIDIA) is a
	// known-broken combination; see capture.AutoCaptureMode's doc. On other
	// platforms it's just the OS screen-recording permission.
	screenCaptureCheck *permStatusChip

	// clipboardToolRow: Linux only, shown only while no CLI clipboard helper
	// (xclip/wl-clipboard/xsel) is installed -- see
	// permissions.RequestClipboardTool. Hidden entirely (not just the
	// button) once one is found, since there's nothing actionable left to
	// show once clipboard sync just works.
	clipboardToolRow *fyne.Container
	clipboardToolBtn *widget.Button

	// rustshineWebRTCRow: shown only while RustShine is the active backend
	// -- lets a supporter turn USBridge's browser/WASM web client on or off
	// without needing to reopen the license dialog. Sunshine has no
	// equivalent surface (no WebRTC endpoint of its own).
	rustshineWebRTCRow   *permToggleRow
	rustshineWebRTCCheck *styledCheck

	// usbDriverRow: shown only while RustShine is active and this
	// platform's USB passthrough driver (vhci-hcd+usbip on Linux,
	// usbip-win2 on Windows) isn't present yet -- see
	// refreshUSBPassthroughUI. usbDriverBtn's action differs per OS: on
	// Linux it calls InstallUSBDriver (real pkexec install); on Windows it
	// just opens usbip-win2's latest-release page in the browser, since
	// that project ships its own signed installer.
	usbDriverRow *fyne.Container
	usbDriverBtn *iconActionButton

	// usbBrokerRow: a status-only row (no button) shown whenever RustShine
	// is active, next to streamerLabel -- separate from usbDriverRow (which
	// is about the *driver*, not the broker process). usbBrokerStatusDot is
	// green while usbpass.Status.BrokerAlive (the broker process answered
	// its own control-socket "status" query moments ago), red otherwise --
	// staged-but-not-running and not-staged-at-all both read as red here,
	// distinguished only by usbBrokerStatusLabel's text (see
	// refreshUSBPassthroughUI).
	usbBrokerRow         *fyne.Container
	usbBrokerStatusDot   *canvas.Circle
	usbBrokerStatusLabel *canvas.Text

	// sunWebSunshineRow/sunWebRustshineRow are mutually exclusive: the
	// Status panel's "web UI" row shows Sunshine's local admin UI address
	// while Sunshine is active, or a link to the RustShine web client
	// (rustshineWebURL) while RustShine is active and its WebRTC endpoint
	// is enabled -- and neither (an empty gap) when RustShine is active
	// with WebRTC turned off, since there's nothing reachable to show.
	sunWebSunshineRow  *fyne.Container
	sunWebRustshineRow *fyne.Container

	// moonlightBtn shows the paired-device count; clicking opens the clients dialog.
	moonlightBtn *iconActionButton

	tsMeta    *tsMetaBlock
	tsPeers   *fyne.Container
	tsEmpty   *canvas.Text
	tsAuthBtn *cardHeaderButton
	tsToggle  *tailscaleHeaderToggle

	autostartCheck *styledCheck

	// Lock GPU Clocks: Windows+NVIDIA only, see app.applyGPUClockLock.
	gpuClockCheck *styledCheck

	// supportBtn opens showLicenseDialog -- a single, low-emphasis entry
	// point for the whole license/RustShine flow, deliberately never
	// popped up on its own (unlike promptForUpdate's confirm dialog, which
	// is functionally necessary on every launch) -- this is a monetization
	// affordance, not something the agent should ever interrupt a session
	// to push. Its text/importance reflect entitlement.Status live (see
	// performRefresh), so a supporter sees at a glance that they're one
	// without needing to open the dialog.
	supportBtn *supportButton

	loginAvatar *loginAvatarButton
	themeBtn    *footerTextButton

	permPanel     *themedPanel
	statusPanel   *themedPanel
	protocolPanel *themedPanel
	autostartLang *autostartRow
	gpuClockLang  *permToggleRow
	mlClientsLang *canvas.Text
	usbDriverLang *canvas.Text
	clipboardLang *widget.Label

	// guiWin is the Fyne window ShowAndRun created -- confirm dialogs from
	// the protocol card's Change button need a parent.
	guiWin fyne.Window

	// pendingTierSwitch is a Pro/Enterprise checkout started from the
	// protocol card: once that tier lands and RustShine is staged, the
	// next refresh switches the backend automatically (same as the license
	// dialog's local pendingTierSwitch).
	pendingTierSwitch string

	protocolPick    string
	protocolApplied string
	protocolHover   string
	protocolRows    []*protocolPickRow
	protocolChange  *cardHeaderButton
	protocolBusy    *footerBusyHint
	footerMsgGen    uint64

	// streamerNameLabel / streamerKindLabel show StreamerName() split into
	// the product ("Sunshine" / "USBridge Streamer") and the smaller
	// parenthetical ("(Open Source)" / "(Proprietary)").
	streamerNameLabel *canvas.Text
	streamerKindLabel *canvas.Text

	// streamerStatusDot is a small traffic-light circle next to
	// streamerNameLabel -- green while the active backend's own child
	// process is actually running (StreamerRunning), red while it's staged
	// but not currently alive (crashed, mid-restart, or simply stopped).
	// Kept in sync by performRefresh, same cadence as streamerNameLabel.
	streamerStatusDot *canvas.Circle

	// streamerVersionLabel sits on the right of the Status card header.
	streamerVersionLabel *canvas.Text
	// rustshineUpdateBtn is a small "check for updates" affordance shown
	// only while RustShine is both active and already staged (see
	// refreshRustShineUI) -- there's nothing to "update" before the first
	// "Download RustShine" click in the license dialog, and Sunshine has
	// no manual update path at all (see streamerVersionLabel's doc
	// comment on why), so this button never applies to it.
	rustshineUpdateBtn *iconActionButton
	// streamerUpdateChecking is true from the moment the Status-card
	// refresh glyph is clicked until CheckRustShineUpdateNow finishes
	// (and the footer idle copy is shown). Keeps the button disabled
	// even before EntitlementStatus.RustShineUpdateInProgress flips on.
	streamerUpdateChecking bool
	// streamerBgUpdateWatching is true while a background auto-update is in
	// flight and this window didn't start it (the refresh button uses
	// streamerUpdateChecking instead). Drives the footer spinner.
	streamerBgUpdateWatching bool
	streamerVersionAtBusy    string
	// streamerUpdatePromptedVersion is the tag we already showed the
	// Yes/No toast for this session, so performRefresh doesn't re-pop it.
	streamerUpdatePromptedVersion string

	// ownsEngine is true only when this window's process itself started the
	// engine (App.Run(headless=false)) — as opposed to a thin client
	// attached to an already-running headless instance (runThinClientGUI).
	// Gates the startup update prompt: applying an update from a thin
	// client would replace the on-disk binary and relaunch this attach-only
	// process without ever touching the separate headless instance that's
	// actually running, so it's not offered there. Set via SetOwnsEngine.
	ownsEngine bool

	// tray is non-nil whenever this session has a usable system tray host
	// (see attachTray) -- gates whether the close button minimizes to tray
	// (SetCloseIntercept below) or falls back to actually quitting, and
	// receives status updates from performRefresh.
	tray *trayController

	// startHidden, set via SetStartHidden, skips the initial win.Show() in
	// ShowAndRun -- used by the --tray launch mode (a login-time helper
	// attaching to an already-running headless engine, or this process's
	// own engine) so it comes up as tray-only instead of popping a window.
	// No-op if attachTray fails to find a usable tray host: showing the
	// window anyway is the only way such a session could ever reach it.
	startHidden bool

	// tierBadge is the header chip next to the logo — Opensource / Free /
	// Pro / Enterprise, kept in sync from entitlement.Status. headerLine
	// is the hairline under the header; its color follows the same status.
	tierBadge  *subscriptionBadge
	headerLine *canvas.Rectangle
}

// SetStartHidden marks this window to come up minimized to the tray instead
// of shown -- see the startHidden field doc for why.
func (w *Window) SetStartHidden(hidden bool) {
	w.startHidden = hidden
}

// SetOwnsEngine marks this window as backed by an engine this same process
// started, rather than one it merely attached a GUI to. See the ownsEngine
// field doc for why this matters.
func (w *Window) SetOwnsEngine(owns bool) {
	w.ownsEngine = owns
}

type uiStatus struct {
	tsStatus        *tailscale.Status
	accessGranted   bool
	moonlightCount  int
	usbStatus       usbpass.Status
	streamerRunning bool
}

// accountSnapshot is the comparable (== usable) subset of account.Status --
// that type itself carries a []account.License slice, which Go won't let
// you compare with ==, so showLicenseDialog's poll loop builds one of
// these each tick to detect an actual change cheaply instead of
// unconditionally re-rendering. licensesKey folds the license list into
// one string precisely so a licenses-only change (a fresh
// RefreshAccountLicenses result) still counts as "changed" even though
// none of the scalar fields above it did.
type accountSnapshot struct {
	loggedIn         bool
	email            string
	loginInProgress  bool
	rebindInProgress bool
	lastError        string
	licensesKey      string
}

func newAccountSnapshot(acc account.Status) accountSnapshot {
	var licensesKey strings.Builder
	for _, lic := range acc.Licenses {
		licensesKey.WriteString(lic.Identifier)
		licensesKey.WriteByte(':')
		licensesKey.WriteString(lic.Status)
		licensesKey.WriteByte(':')
		licensesKey.WriteString(lic.Tier)
		if lic.OnThisDevice {
			licensesKey.WriteString(":here")
		}
		licensesKey.WriteByte('|')
	}
	return accountSnapshot{
		loggedIn:         acc.LoggedIn,
		email:            acc.Email,
		loginInProgress:  acc.LoginInProgress,
		rebindInProgress: acc.RebindInProgress,
		lastError:        acc.LastError,
		licensesKey:      licensesKey.String(),
	}
}

func NewWindow(app fyne.App, cfg config.Config, perms PermsProvider, ts TailscaleProvider, tokenManager TokenProvider) *Window {
	lang := "en"
	if app != nil {
		lang = app.Preferences().StringWithFallback(i18n.LanguagePrefKey, "en")
	}
	i18n.Init(lang)
	return &Window{app: app, cfg: cfg, perms: perms, ts: ts, token: tokenManager}
}

// linuxCaptureUIEnabled reports whether the Screen Capture row should use
// the Linux capture-mode status (Sunshine's auto-detected backend) rather
// than the simple OS screen-recording permission toggle used on other
// platforms.
func (w *Window) linuxCaptureUIEnabled() bool {
	return runtime.GOOS == "linux" && w.token != nil
}

// refreshScreenCaptureUI updates the status tick based on whichever capture
// method is currently selected.
func (w *Window) refreshScreenCaptureUI() {
	if w.screenCaptureCheck == nil {
		return
	}

	if w.linuxCaptureUIEnabled() {
		mode := w.token.SunshineCaptureMode()
		if mode == "kms" {
			w.screenCaptureCheck.SetChecked(w.token.KMSCaptureGranted())
			return
		}
		granted := w.perms != nil && w.perms.ScreenRecordingGranted()
		w.screenCaptureCheck.SetChecked(granted)
		return
	}

	if w.perms == nil {
		return
	}
	w.screenCaptureCheck.SetChecked(w.perms.ScreenRecordingGranted())
}

// refreshClipboardToolUI hides the whole clipboard-tool row (not just its
// button) once a CLI clipboard helper is found -- there's nothing left to
// act on at that point, unlike the Accessibility/Screen Capture rows above
// which keep showing a ✅/❌ status permanently.
func (w *Window) refreshClipboardToolUI() {
	if w.clipboardToolRow == nil || w.perms == nil {
		return
	}
	if w.perms.ClipboardToolAvailable() {
		w.clipboardToolRow.Hide()
	} else {
		w.clipboardToolRow.Show()
	}
}

// rustshineWebURL is USBridge's browser/WASM web client -- shown in the
// Status panel's web-UI row in place of Sunshine's local admin address
// whenever RustShine is active and its WebRTC endpoint is enabled (see
// refreshRustShineUI).
const rustshineWebURL = "https://web.usbridge.io"

// formatStreamerVersion keeps the Status-card "v…" tag for both backends.
// Sunshine uses the agent build; USBridge Streamer uses the staged release
// tag, which entitlement stores as "usbridge-streamer-v1.2.3" /
// "gamestream-server-v1.2.3" — those prefixes are stripped so the header
// matches Sunshine's short "v1.2.3" rather than losing the "v" or showing
// the whole tag.
func formatStreamerVersion(appVer, rustshineVer string, rustshineActive bool) string {
	raw := strings.TrimSpace(appVer)
	if rustshineActive {
		tag := strings.TrimSpace(rustshineVer)
		tag = strings.TrimPrefix(tag, "usbridge-streamer-v")
		tag = strings.TrimPrefix(tag, "gamestream-server-v")
		if tag != "" {
			raw = tag
		}
	}
	raw = strings.TrimPrefix(raw, "v")
	if raw == "" {
		return ""
	}
	return "v" + raw
}

// refreshRustShineUI keeps the standalone WebRTC checkbox (moved out of
// showLicenseDialog so it's visible without opening that popup) and the
// Status panel's web-UI row in sync with entitlement.Status on every
// refresh tick.
func (w *Window) refreshRustShineUI(st entitlement.Status) {
	active := st.ActiveBackend == "rustshine"

	if w.streamerVersionLabel != nil {
		version := formatStreamerVersion(appVersion, st.RustShineVersion, active)
		if active && strings.TrimSpace(st.RustShineAvailableVersion) != "" {
			version = loc().UpdateAvailableHint + "  " + version
		}
		if w.streamerVersionLabel.Text != version {
			w.streamerVersionLabel.Text = version
			w.streamerVersionLabel.Refresh()
		}
	}
	if w.rustshineUpdateBtn != nil {
		switch {
		case !active || !st.RustShineStaged:
			// Sunshine has no manual update path (see
			// streamerVersionLabel's own doc comment), and there's
			// nothing to check an update *against* before RustShine has
			// even been downloaded once.
			w.rustshineUpdateBtn.Hide()
		case st.RustShineUpdateInProgress || w.streamerUpdateChecking:
			w.rustshineUpdateBtn.Show()
			w.rustshineUpdateBtn.Disable()
		default:
			w.rustshineUpdateBtn.Show()
			w.rustshineUpdateBtn.Enable()
		}
	}

	if w.rustshineWebRTCRow != nil {
		if active {
			w.rustshineWebRTCRow.Show()
			if w.rustshineWebRTCCheck != nil && w.rustshineWebRTCCheck.Checked != st.WebRTCEnabled {
				w.rustshineWebRTCCheck.Checked = st.WebRTCEnabled
				w.rustshineWebRTCCheck.Refresh()
			}
		} else {
			w.rustshineWebRTCRow.Hide()
		}
	}

	if w.sunWebSunshineRow != nil && w.sunWebRustshineRow != nil {
		switch {
		case active && st.WebRTCEnabled:
			w.sunWebSunshineRow.Hide()
			w.sunWebRustshineRow.Show()
		case active:
			// RustShine active but its WebRTC endpoint is off -- nothing
			// reachable to show here at all.
			w.sunWebSunshineRow.Hide()
			w.sunWebRustshineRow.Hide()
		default:
			w.sunWebRustshineRow.Hide()
			w.sunWebSunshineRow.Show()
		}
	}

	w.syncStreamerUpdateFooter(st)
	w.maybeOfferStreamerUpdate(st)
}

func (w *Window) syncStreamerUpdateFooter(st entitlement.Status) {
	if w.streamerUpdateChecking {
		return
	}
	if st.RustShineUpdateInProgress {
		if !w.streamerBgUpdateWatching {
			w.streamerVersionAtBusy = st.RustShineVersion
			w.startFooterBusy(loc().CheckingUpdates)
			w.streamerBgUpdateWatching = true
		}
		return
	}
	if !w.streamerBgUpdateWatching {
		return
	}
	w.streamerBgUpdateWatching = false
	if st.RustShineVersion != "" && st.RustShineVersion != w.streamerVersionAtBusy {
		w.showFooterIdle(loc().StreamerUpdated, footerIdleMessageDuration)
		return
	}
	w.stopFooterBusy()
}

func (w *Window) maybeOfferStreamerUpdate(st entitlement.Status) {
	if st.ActiveBackend != "rustshine" || !st.RustShineUpdateOffer {
		return
	}
	ver := strings.TrimSpace(st.RustShineAvailableVersion)
	if ver == "" || w.streamerUpdatePromptedVersion == ver {
		return
	}
	if w.guiWin == nil || w.token == nil || w.streamerUpdateChecking || st.RustShineUpdateInProgress {
		return
	}
	w.streamerUpdatePromptedVersion = ver
	showConfirmToast(loc().StreamerUpdateAsk, func(yes bool) {
		if yes {
			w.beginStreamerUpdateCheck()
			return
		}
		go func() {
			if err := w.token.SnoozeStreamerUpdate(ver); err != nil {
				logrus.WithError(err).Warn("could not snooze streamer update")
			}
		}()
	}, w.guiWin)
}

func (w *Window) beginStreamerUpdateCheck() {
	if w.token == nil || w.streamerUpdateChecking {
		return
	}
	w.streamerUpdateChecking = true
	if w.rustshineUpdateBtn != nil {
		w.rustshineUpdateBtn.Disable()
	}
	before := w.token.EntitlementStatus()
	w.startFooterBusy(loc().CheckingUpdates)
	go func() {
		err := w.token.CheckRustShineUpdateNow()
		if err != nil {
			logrus.WithError(err).Warn("rustshine update check failed")
		}
		if !w.ownsEngine && err == nil {
			w.waitUntilRustShineUpdateSettles()
		}
		fyne.Do(func() {
			w.finishStreamerUpdateCheck(before, err)
		})
	}()
}

// waitUntilRustShineUpdateSettles is the thin-client path: the admin API
// returns before the engine finishes, so we poll RustShineUpdateInProgress
// until it has been seen and then cleared (or a short grace expires).
func (w *Window) waitUntilRustShineUpdateSettles() {
	if w.token == nil {
		return
	}
	start := time.Now()
	seen := false
	for time.Since(start) < 3*time.Minute {
		st := w.token.EntitlementStatus()
		if st.RustShineUpdateInProgress {
			seen = true
		} else if seen || time.Since(start) > 1500*time.Millisecond {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func (w *Window) finishStreamerUpdateCheck(before entitlement.Status, checkErr error) {
	w.streamerUpdateChecking = false
	st := entitlement.Status{}
	if w.token != nil {
		st = w.token.EntitlementStatus()
	}
	if w.rustshineUpdateBtn != nil && !st.RustShineUpdateInProgress {
		w.rustshineUpdateBtn.Enable()
	}

	failed := checkErr != nil || (st.LastError != "" && st.LastError != before.LastError)
	if failed {
		w.showFooterIdle(loc().UpdateFailed, footerIdleMessageDuration)
		return
	}
	if st.RustShineVersion != "" && st.RustShineVersion != before.RustShineVersion {
		w.showFooterIdle(loc().StreamerUpdated, footerIdleMessageDuration)
		return
	}
	w.showFooterIdle(loc().AlreadyUpToDate, footerIdleMessageDuration)
}

// refreshUSBPassthroughUI keeps usbDriverRow in sync -- shown only while
// RustShine is the active backend (Sunshine never gets USB passthrough,
// see internal/usbpass's own doc comment) and this platform's driver isn't
// present yet (st.VhciDriver false). Disappears once the driver install
// actually takes -- InstallUSBDriver's pkexec call updates the real
// vhci-hcd state that usbStatus is read from on the very next tick, no
// separate "installed" signal needed.
func (w *Window) refreshUSBPassthroughUI(st entitlement.Status, usb usbpass.Status) {
	active := st.ActiveBackend == "rustshine"
	if w.usbAccessCheck != nil {
		if (st.Tier == "pro" || st.Tier == "enterprise") && usb.Available {
			w.usbAccessCheck.Show()
			w.usbAccessCheck.SetChecked(usb.AttachGranted)
		} else {
			w.usbAccessCheck.Hide()
		}
	}
	if w.usbDriverRow != nil {
		if active && usb.Available && !usb.VhciDriver {
			w.usbDriverRow.Show()
		} else {
			w.usbDriverRow.Hide()
		}
	}
	if w.usbBrokerRow == nil {
		return
	}
	if !active || !usb.Available {
		w.usbBrokerRow.Hide()
		return
	}
	w.usbBrokerRow.Show()
	setStatusDot(w.usbBrokerStatusDot, usb.BrokerAlive)
	if w.usbBrokerStatusLabel != nil {
		// usb.BrokerError is always non-empty whenever BrokerAlive is false
		// and Available is true (see usbpass.Service.Status: it returns
		// early with BrokerError set the moment resolveBroker()'s "not
		// staged" check or the control-socket dial/query fails, and is the
		// only path that leaves BrokerAlive false here) -- so this just
		// tells "not staged at all" apart from "staged but not answering".
		label := "Running"
		if !usb.BrokerAlive {
			label = loc().NotRunning
			if strings.Contains(usb.BrokerError, "not staged") {
				label = loc().NotStaged
			}
		}
		w.usbBrokerStatusLabel.Text = label
		w.usbBrokerStatusLabel.Refresh()
	}
}

func (w *Window) ShowAndRun(onClose func()) {
	win := w.app.NewWindow(loc().AppTitle)
	w.guiWin = win
	win.SetPadded(false)
	win.Resize(fyne.NewSize(640, 460))
	win.CenterOnScreen()
	w.loadChromePin()

	w.tierBadge = newSubscriptionBadge()
	tokenBtn := newIconActionButton("TOKEN", nil, func() {
		w.showTokenDialog(win)
	})
	tokenBtn.Compact = true
	tokenBtn.Accent = true
	var settingsBtn *headerIconButton
	settingsBtn = newHeaderIconButton(theme.SettingsIcon(), func() {
		w.showSettingsMenu(win, settingsBtn)
	})
	w.tsToggle = newTailscaleHeaderToggle(func() { w.toggleTailscaleAuth() })
	w.loginAvatar = newLoginAvatarButton(func() { w.openAccount(win, w.loginAvatar) })
	if w.token != nil {
		acc := w.token.AccountStatus()
		w.loginAvatar.SetState(acc.LoggedIn, acc.Email)
	}
	headerLeft := container.New(&tightHBoxLayout{gap: 4}, newBrandLockup(), w.tierBadge)
	headerRight := container.New(&tightHBoxLayout{gap: 6}, w.tsToggle, settingsBtn, tokenBtn,
		container.NewGridWrap(fyne.NewSize(loginAvatarHit, loginAvatarHit), w.loginAvatar))
	var header fyne.CanvasObject
	header, w.headerLine = newHeaderBar(headerLeft, headerRight)
	if w.token != nil {
		w.refreshTierBadge(w.token.EntitlementStatus())
	}

	// Column 1: Permissions
	accessLabelBase := loc().Accessibility
	if runtime.GOOS == "linux" {
		accessLabelBase = loc().InputControl
	}

	// Mirrors the pre-redesign dedicated "Request" buttons' own platform/
	// capture-mode gating: Input Control is requestable on macOS/Linux
	// unconditionally (an OS-level grant with no display-server dependency);
	// Screen Capture's own request is only meaningful for macOS's System
	// Settings flow or Linux/Wayland's portal flow -- X11/KMS capture needs
	// no such request (KMS's own capability grant is handled below via
	// linuxCaptureUIEnabled instead, same as before).
	showAccessButton := runtime.GOOS == "darwin" || runtime.GOOS == "linux"
	showScreenCaptureButton := runtime.GOOS == "darwin" || (runtime.GOOS == "linux" && capture.GetLinuxEnv() == "Wayland")
	linuxCapture := w.linuxCaptureUIEnabled()

	var onRequestAccess func()
	if showAccessButton {
		onRequestAccess = func() {
			if w.perms == nil {
				if w.accessCheck != nil {
					w.accessCheck.requestDone()
				}
				return
			}
			go func() {
				granted := w.perms.RequestAccessibility()
				fyne.Do(func() {
					if w.accessCheck != nil {
						w.accessCheck.requestDone()
					}
				})
				if !granted {
					if e, ok := w.perms.(interface{ LastAccessibilityError() string }); ok {
						if msg := e.LastAccessibilityError(); msg != "" {
							fyne.Do(func() { dialog.ShowError(fmt.Errorf("%s", msg), win) })
						}
					}
				}
				w.performRefresh()
			}()
		}
	}

	var onRequestCapture func()
	if showScreenCaptureButton || linuxCapture {
		onRequestCapture = func() {
			go func() {
				switch {
				case w.linuxCaptureUIEnabled() && w.token.SunshineCaptureMode() == "kms":
					w.token.RequestKMSCapture()
				case runtime.GOOS == "darwin" && w.perms != nil:
					// Screen recording must be granted to Sunshine (a
					// separate process) via System Settings -- we can't
					// request it on Sunshine's behalf, so open the pane and
					// restart Sunshine to pick up the new permission once
					// the user grants it there.
					_ = w.perms.OpenScreenRecordingSettings()
					if w.token != nil {
						_ = w.token.RestartSunshine()
					}
				case w.perms != nil:
					_ = w.perms.RequestScreenRecording()
				}
				fyne.Do(func() {
					if w.screenCaptureCheck != nil {
						w.screenCaptureCheck.requestDone()
					}
					w.refreshScreenCaptureUI()
				})
			}()
		}
	}

	w.accessCheck = newPermStatusChip(accessLabelBase, onRequestAccess)
	w.screenCaptureCheck = newPermStatusChip(loc().ScreenCapture, onRequestCapture)
	permStatusRow := container.NewVBox(w.accessCheck, w.screenCaptureCheck)

	// Autostart at Boot: installs the OS-native autostart mechanism (a
	// system-wide systemd unit on Linux — so it starts at boot before any
	// graphical session, which is what KMS capture needs; a LaunchAgent
	// plist on macOS; a LocalSystem AUTO_START service on Windows — see
	// internal/autostart). The registered command always launches with
	// --headless, so a later normal launch of this same binary/AppImage
	// attaches a GUI to that instance instead of starting a second engine —
	// see app.Start. On Linux this shells out via pkexec, same as the KMS
	// capability grant, so expect a polkit prompt on toggle. On Windows the
	// service is not started from this live GUI (that would spawn a second
	// tray icon in the same session); it takes effect on the next reboot,
	// and the row shows a small reboot hint until then.
	w.autostartCheck = newStyledCheck("", autostart.IsEnabled(), func(checked bool) {
		w.autostartCheck.Disable()
		go func() {
			var err error
			if checked {
				err = autostart.Enable()
			} else {
				err = autostart.Disable()
			}
			fyne.Do(func() {
				if w.autostartCheck == nil {
					return
				}
				w.autostartCheck.Enable()
				if err != nil {
					logrus.Errorf("[ui] autostart toggle failed: %v", err)
					w.autostartCheck.SetChecked(!checked)
					dialog.ShowError(err, win)
				}
				w.refreshAutostartChrome()
			})
		}()
	})

	// Autostart at Boot is always shown, regardless of platform.
	autostartRow := newAutostartRow(loc().AutostartAtBoot, w.autostartCheck, win)
	w.autostartLang = autostartRow
	w.refreshAutostartChrome()

	// Lock GPU Clocks: holds an NVML max-clock lock for the life of this
	// agent process (once enabled) so the GPU doesn't idle into a low-power
	// state between frames and stall NVENC on the next one (see
	// app.applyGPUClockLock). Windows+NVIDIA only -- entirely absent from the
	// Permissions block on other platforms, where GPUClockLockSupported()
	// returns false, rather than shown-but-disabled. No separate "Request"
	// button: the checkbox itself triggers the one (UAC-prompting) request
	// for this agent run -- deliberately upfront and one-time, not
	// re-triggered on every stream-host restart, since a UAC prompt can't be
	// dismissed from a remote session (it runs on the secure desktop) and
	// would otherwise strand a remote client switching monitors mid-stream.
	gpuClockSupported := w.token != nil && w.token.GPUClockLockSupported()
	initialGPU := gpuClockSupported && w.token.LockGPUClocksEnabled()
	w.gpuClockCheck = newStyledCheck("", initialGPU, func(checked bool) {
		w.gpuClockCheck.Disable()
		go func() {
			var err error
			if w.token != nil {
				err = w.token.SetLockGPUClocksEnabled(checked)
			}
			fyne.Do(func() {
				if w.gpuClockCheck == nil {
					return
				}
				w.gpuClockCheck.Enable()
				if err != nil {
					logrus.Errorf("[ui] lock GPU clocks toggle failed: %v", err)
					w.gpuClockCheck.SetChecked(!checked)
					dialog.ShowError(err, win)
				}
			})
		}()
	})
	gpuClockRow := newPermToggleRow(loc().LockGPUClocks, w.gpuClockCheck)
	w.gpuClockLang = gpuClockRow

	// Clipboard sync (Linux only): internal/clipboard's Linux backend shells
	// out to xclip/wl-clipboard/xsel, none of which every distro ships by
	// default (confirmed live: a Debian machine with wl-clipboard installed
	// but not xclip still failed every clipboard apply until one was
	// present). Offer a one-click pkexec install instead of a silent,
	// permanent "no clipboard tool available" failure the user has no way
	// to self-diagnose from this UI.
	w.clipboardToolBtn = widget.NewButton(loc().Install, func() {
		if w.perms == nil {
			return
		}
		w.clipboardToolBtn.Disable()
		go func() {
			defer fyne.Do(func() {
				if w.clipboardToolBtn != nil {
					w.clipboardToolBtn.Enable()
				}
			})
			granted := w.perms.RequestClipboardTool()
			if !granted {
				if e, ok := w.perms.(interface{ LastAccessibilityError() string }); ok {
					if msg := e.LastAccessibilityError(); msg != "" {
						fyne.Do(func() { dialog.ShowError(fmt.Errorf("%s", msg), win) })
					}
				}
			}
			fyne.Do(w.refreshClipboardToolUI)
		}()
	})
	w.clipboardToolBtn.Importance = widget.LowImportance

	// "?" info button: shows the literal pkexec command Install would run,
	// before it runs it -- clicking Install itself pops a polkit password
	// prompt, which isn't the moment to first learn what's about to execute
	// as root.
	clipboardInfoBtn := widget.NewButtonWithIcon("", theme.InfoIcon(), func() {
		preview := ""
		if w.perms != nil {
			preview = w.perms.ClipboardInstallPreview()
		}
		if preview == "" {
			preview = loc().ClipboardNoPkgMgr
		}
		dialog.ShowInformation(loc().ClipboardInstall, preview, win)
	})
	clipLabel := widget.NewLabel(loc().ClipboardTool)
	w.clipboardLang = clipLabel
	w.clipboardToolRow = container.NewHBox(clipLabel, layout.NewSpacer(), clipboardInfoBtn, w.clipboardToolBtn)

	// RustShine web client (WebRTC) toggle -- shown only while RustShine is
	// the active backend (see refreshRustShineUI). Built unconditionally
	// here (not gated by OS or entitlement) since it starts hidden and
	// refreshRustShineUI is what actually decides visibility on every tick,
	// same shape as clipboardToolRow above.
	w.rustshineWebRTCCheck = newStyledCheck("", false, func(checked bool) {
		if w.token == nil {
			return
		}
		go func() {
			if err := w.token.SetRustShineWebRTCEnabled(checked); err != nil {
				fyne.Do(func() {
					if w.rustshineWebRTCCheck != nil {
						w.rustshineWebRTCCheck.SetChecked(!checked)
					}
				})
			}
		}()
	})
	w.rustshineWebRTCRow = newPermToggleRow(loc().WebRTCToggle, w.rustshineWebRTCCheck)
	w.rustshineWebRTCRow.Hide()

	// USB passthrough driver install -- shown only while RustShine is
	// active and the driver isn't present yet (refreshUSBPassthroughUI).
	// Windows opens usbip-win2's release page directly (no adminapi round
	// trip: usbip-win2 ships its own signed installer/UAC flow); Linux
	// goes through InstallUSBDriver, a real pkexec-elevated apt+modprobe
	// install (see usbpass/driver_linux.go).
	usbDriverLabel := loc().InstallUSBDriver
	if runtime.GOOS == "windows" {
		usbDriverLabel = loc().GetUSBIPDriver
	}
	w.usbDriverBtn = newIconActionButton(usbDriverLabel, assets.GitHubIcon, func() {
		if runtime.GOOS == "windows" {
			if parsed, err := url.Parse("https://github.com/vadimgrn/usbip-win2/releases/latest"); err == nil {
				_ = w.app.OpenURL(parsed)
			}
			return
		}
		if w.token == nil {
			return
		}
		w.usbDriverBtn.Disable()
		go func() {
			err := w.token.InstallUSBDriver()
			fyne.Do(func() {
				if w.usbDriverBtn != nil {
					w.usbDriverBtn.Enable()
				}
				if err != nil {
					dialog.ShowError(err, win)
				}
			})
		}()
	})
	w.usbDriverBtn.Tiny = true
	usbDriverTitle := canvas.NewText(loc().USBPassthrough, design.ColorSectionTitle)
	w.usbDriverLang = usbDriverTitle
	usbDriverTitle.TextSize = 11
	w.usbDriverRow = newStatusRow(usbDriverTitle, w.usbDriverBtn)
	w.usbDriverRow.Hide()

	if runtime.GOOS == "linux" {
		// Same chip as Input Control / Screen Capture: green check when
		// granted, "· Grant" tap target otherwise.
		w.usbAccessCheck = newPermStatusChip(loc().USBAccess, func() {
			if w.token == nil {
				w.usbAccessCheck.requestDone()
				return
			}
			go func() {
				err := w.token.GrantUSBAttach()
				fyne.Do(func() {
					w.usbAccessCheck.requestDone()
					if err != nil {
						dialog.ShowError(err, win)
					}
				})
				w.performRefresh()
			}()
		})
		w.usbAccessCheck.Hide()
	}

	permRule := canvas.NewRectangle(design.ColorDivider)
	permRule.SetMinSize(fyne.NewSize(0, 1))
	var permTop []fyne.CanvasObject
	permTop = []fyne.CanvasObject{
		permStatusRow,
		permRule,
		autostartRow,
	}
	if gpuClockSupported {
		permTop = append(permTop, gpuClockRow)
	}
	if runtime.GOOS == "linux" {
		permTop = append(permTop, w.clipboardToolRow)
		w.refreshClipboardToolUI()
	}
	permTop = append(permTop, w.rustshineWebRTCRow)
	permTop = append(permTop, w.usbDriverRow)

	// Moonlight Clients — add (+) opens PIN dialog; icon+count opens list; ✕ removes all.
	moonlightAddBtn := newTinyGlyphButtonColored(theme.ContentAddIcon(), design.ColorNameMutedOlive, func() {
		w.showMoonlightPINDialog(win)
	})
	w.moonlightBtn = newIconActionButton("0", theme.NewColoredResource(theme.AccountIcon(), design.ColorNameMutedOlive), func() {
		w.showMoonlightClientsDialog(win)
	})
	w.moonlightBtn.Tiny = true
	moonlightDeleteAllBtn := newDangerGlyphButton(func() {
		showConfirmToast(loc().RemoveAllMoonlight, func(yes bool) {
			if !yes || w.token == nil {
				return
			}
			go func() {
				clients, err := w.token.ListSunshineClients()
				if err != nil {
					return
				}
				for _, c := range clients {
					_ = w.token.UnpairSunshineClient(c.UniqueID)
				}
				// Unpairing only blocks future reconnects — a client
				// already mid-stream keeps going until the stream host
				// itself is restarted (same reasoning as
				// RegenerateMasterKey's own client wipe).
				_ = w.token.RestartSunshine()
				fyne.Do(func() {
					if w.moonlightBtn != nil {
						w.moonlightBtn.SetText("0")
					}
				})
			}()
		}, win)
	})
	mlLabel := canvas.NewText(loc().MoonlightClients, design.ColorSectionTitle)
	w.mlClientsLang = mlLabel
	mlLabel.TextSize = 11
	moonlightRow := newStatusRow(
		mlLabel, container.New(&tightHBoxLayout{gap: 4},
			moonlightAddBtn, w.moonlightBtn, moonlightDeleteAllBtn))
	permTop = append(permTop, moonlightRow)
	permContent := newTightVBox(permTop...)
	permBlock := newPanel(panelIconPermissions, loc().Permissions, nil, permContent)
	if p, ok := permBlock.(*themedPanel); ok {
		w.permPanel = p
	}

	// Column 2: Stats & Tailscale
	sunshinePort := w.cfg.SunshinePort
	if sunshinePort == 0 {
		sunshinePort = 47990
	}

	// supportBtn lives in the Protocol card header next to Change.
	w.supportBtn = newSupportButton(loc().BuyPro, func() {
		w.onProtocolBuyClicked(win)
	})

	w.streamerNameLabel = makeStatusName("")
	w.streamerKindLabel = makeStatusValue("")
	w.streamerKindLabel.TextSize = 9
	w.setStreamerDisplay(w.token.StreamerName())
	w.streamerStatusDot = newStatusDot()
	w.streamerVersionLabel = newTSMetaLabel("")
	// Starts the footer spinner immediately; CheckRustShineUpdateNow is
	// what actually talks to the backend. In-process this call blocks
	// until the check (and any download) finishes; a thin-client GUI
	// gets a fire-and-forget HTTP 200 and polls EntitlementStatus
	// instead (see handleCheckRustShineUpdateNow).
	w.rustshineUpdateBtn = newTinyGlyphButtonColored(theme.ViewRefreshIcon(), design.ColorNameMutedOlive, func() {
		w.beginStreamerUpdateCheck()
	})
	streamerLabel := newStatusRow(
		container.New(&tightHBoxLayout{gap: 6},
			makeStatusLabel(loc().Streamer), statusDotBox(w.streamerStatusDot),
			w.streamerNameLabel, w.streamerKindLabel),
		w.rustshineUpdateBtn,
	)

	// usbBrokerRow -- see its field doc comment. Built unconditionally (like
	// rustshineWebRTCRow/usbDriverRow); refreshUSBPassthroughUI is what
	// actually decides visibility and the dot/label text on every tick.
	w.usbBrokerStatusDot = newStatusDot()
	w.usbBrokerStatusLabel = makeStatusValue("")
	w.usbBrokerRow = newStatusRow(
		container.New(&tightHBoxLayout{gap: 6},
			makeStatusLabel(loc().USBBroker), statusDotBox(w.usbBrokerStatusDot),
			w.usbBrokerStatusLabel),
		nil,
	)
	w.usbBrokerRow.Hide()

	httpVal := makeStatusAddress(fmt.Sprintf("%s:%d", w.cfg.EffectiveListenHost(), w.cfg.HTTPPort))
	httpWarn := makeWarningBadge()
	if !needsWarnBadge(w.cfg.EffectiveListenHost()) {
		httpWarn.Hide()
	}
	httpEditBtn := newTinyGlyphButtonColored(theme.DocumentCreateIcon(), design.ColorNameMutedOlive, func() {
		w.showEditHTTPAddrDialog(win, httpVal, httpWarn)
	})
	httpRow := newStatusRow(
		container.New(&tightHBoxLayout{gap: 6}, makeStatusLabel(loc().HTTP), httpVal, httpWarn),
		httpEditBtn,
	)

	sunWebVal := makeStatusAddress(fmt.Sprintf("127.0.0.1:%d", sunshinePort))

	sunStreamPort := sunshinePort - 1
	sunStreamIP := w.token.SunshineStreamHost()
	if sunStreamIP == "" {
		sunStreamIP = "0.0.0.0"
	}
	sunStreamVal := makeStatusAddress(fmt.Sprintf("%s:%d", sunStreamIP, sunStreamPort))
	sunStreamWarn := makeWarningBadge()
	if !needsWarnBadge(sunStreamIP) {
		sunStreamWarn.Hide()
	}
	sunStreamEditBtn := newTinyGlyphButtonColored(theme.DocumentCreateIcon(), design.ColorNameMutedOlive, func() {
		w.showEditSunStreamDialog(win, sunStreamVal, sunStreamWarn, sunWebVal)
	})
	sunStreamRow := newStatusRow(
		container.New(&tightHBoxLayout{gap: 6}, makeStatusLabel(loc().Sunshine), sunStreamVal, sunStreamWarn),
		sunStreamEditBtn,
	)

	sunWebEyeBtn := newTinyGlyphButtonColored(theme.VisibilityIcon(), design.ColorNameMutedOlive, func() {
		port := w.cfg.SunshinePort
		if port == 0 {
			port = 47990
		}
		w.showSunshineWebDialog(win, port)
	})
	sunWebEditBtn := newTinyGlyphButtonColored(theme.DocumentCreateIcon(), design.ColorNameMutedOlive, func() {
		w.showEditSunPortDialog(win, sunWebVal, sunStreamVal)
	})
	w.sunWebSunshineRow = newStatusRow(
		container.New(&tightHBoxLayout{gap: 6}, makeStatusLabel(loc().SunWeb), sunWebVal),
		container.New(&tightHBoxLayout{gap: 4}, sunWebEyeBtn, sunWebEditBtn),
	)

	var webURL *url.URL
	if parsed, err := url.Parse(rustshineWebURL); err == nil {
		webURL = parsed
	}
	sunWebLinkVal := newStatusLink(rustshineWebURL, design.ColorAddress, 10, func() {
		if webURL != nil && w.app != nil {
			_ = w.app.OpenURL(webURL)
		}
	})
	sunWebLinkCopyBtn := newTinyGlyphButtonColored(theme.ContentCopyIcon(), design.ColorNameMutedOlive, func() {
		win.Clipboard().SetContent(rustshineWebURL)
	})
	sunWebLinkInfoBtn := newTinyGlyphButtonColored(theme.InfoIcon(), design.ColorNameMutedOlive, func() {
		w.showWebClientInfoDialog(win)
	})
	w.sunWebRustshineRow = newStatusRow(
		container.New(&tightHBoxLayout{gap: 6},
			makeStatusLabel(loc().Web), sunWebLinkVal),
		container.New(&tightHBoxLayout{gap: 4}, sunWebLinkInfoBtn, sunWebLinkCopyBtn),
	)
	w.sunWebRustshineRow.Hide()

	statsBlock := newPanel(osHeaderIcon(), loc().Status, w.streamerVersionLabel, container.New(&tightVBoxLayout{gap: 4},
		streamerLabel, w.usbBrokerRow, httpRow, sunStreamRow, w.sunWebSunshineRow, w.sunWebRustshineRow))
	if p, ok := statsBlock.(*themedPanel); ok {
		w.statusPanel = p
	}

	w.tsMeta = newTSMetaBlock()
	w.tsPeers = container.New(&tightVBoxLayout{gap: 8})
	w.tsPeers.Hide()
	w.tsEmpty = canvas.NewText(loc().NoRemoteControllers, design.ColorEmptyHint)
	w.tsEmpty.TextSize = 9
	w.tsEmpty.Alignment = fyne.TextAlignCenter
	wellFloor := canvas.NewRectangle(color.Transparent)
	wellFloor.SetMinSize(fyne.NewSize(0, 72))
	sessionsWell := newDarkWell(container.NewStack(
		wellFloor,
		container.NewCenter(w.tsEmpty),
		newExactInset(w.tsPeers, 8, 8, 8, 8),
	))

	// The actual "open a browser" action is centralized in the AuthURL handler
	// registered below (via SetAuthURLHandler) — it fires whenever tsnet
	// produces a fresh AuthURL, no matter what triggered it. This button only
	// sets awaitingLocalLogin so the handler knows THIS particular AuthURL was
	// asked for locally, and nudges the login so one actually gets generated.
	w.tsAuthBtn = newCardHeaderButton(loc().SignIn, headerLoginIcon, func() {
		w.toggleTailscaleAuth()
	})

	if w.ts != nil {
		w.ts.SetAuthURLHandler(func(authURL string) {
			if !w.awaitingLocalLogin.CompareAndSwap(true, false) {
				// Triggered by something other than this window's own button (a
				// remote client's sync/register request, or tsnet's first-boot
				// auto-login) — don't pop a browser on this machine unasked.
				logrus.Infof("tailscale ui: AuthURL produced by a non-local trigger, not opening: %s", authURL)
				return
			}
			parsed, parseErr := url.Parse(strings.TrimSpace(authURL))
			if parseErr != nil {
				logrus.Errorf("tailscale ui: failed to parse auth URL %q: %v", authURL, parseErr)
				fyne.Do(func() { w.setTailscaleInfo(loc().InvalidLoginURL, "", "") })
				return
			}
			logrus.Infof("tailscale ui: captured auth URL: %s", parsed.String())
			fyne.Do(func() {
				if w.app != nil {
					_ = w.app.OpenURL(parsed)
				}
				w.setTailscaleInfo(loc().LoginLinkOpened, "", "")
			})
		})
	}

	tsPanel := newPanel(panelIconTailscale, "Tailscale", w.tsAuthBtn, container.NewBorder(
		w.tsMeta.root, nil, nil, nil, sessionsWell,
	))

	protocolBlock := w.newProtocolPanel(win)

	// 2x2: Tailscale | Permissions on top, Protocol | Status below.
	// Cards keep their content height; leftover window space stays empty
	// below the grid instead of stretching the cards.
	cards := container.New(&cardGridLayout{gap: 16, topInset: 0, bottomInset: 0},
		tsPanel, permBlock, protocolBlock, statsBlock)

	content := cards

	// Initial refresh
	w.performRefresh()
	w.refreshScreenCaptureUI()

	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			if win.Canvas() == nil {
				return
			}
			w.performRefresh()
		}
	}()

	bg := canvas.NewRectangle(design.ColorPanel)
	w.protocolBusy = newFooterBusyHint(loc().ChangingProtocol)
	w.themeBtn = newFooterTextButton(loc().Theme, func() { w.showThemeMenu(w.themeBtn) })
	footer := newAppFooter(appVersion, w.protocolBusy, w.themeBtn, func() {
		showWhatsNewDialog(win)
	})
	body := container.NewBorder(header, footer, nil, nil, newExactInset(content, 16, 16, 8, 8))
	win.SetContent(container.NewStack(bg, body))

	// attachTray must run before wiring our own close intercept below:
	// desktop.App.SetSystemTrayWindow (called inside attachTray) installs
	// its own SetCloseIntercept(win.Hide), which we then deliberately
	// override with the richer version below (adds the one-time "still
	// running in the tray" hint) -- see attachTray's doc comment.
	w.tray = w.attachTray(win, onClose)
	w.refreshAutostartChrome()

	win.SetCloseIntercept(func() {
		if w.tray != nil {
			log.Printf("[ui] window close intercepted -- minimizing to tray")
			win.Hide()
			w.tray.notifyHiddenOnce()
			return
		}
		// Diagnostic-only log, added to chase a live symptom where the whole
		// engine (tsnet, HTTP, the rustshine child) shuts down cleanly with
		// no OS-signal or Event Log evidence of why. If this line logs right
		// before that shutdown, the trigger is a Fyne/GLFW-level window
		// close request (WM_CLOSE or equivalent) rather than an OS signal
		// (see app.go's Run, which now logs those separately) -- narrowing
		// which of the two `onClose`/`cancel` call sites is actually firing.
		log.Printf("[ui] DIAG: window close intercepted -- calling onClose (engine shutdown)")
		if onClose != nil {
			onClose()
		}
		win.Close()
	})
	if w.ownsEngine {
		go w.promptForUpdate(win)
	}

	// startHidden (the --tray launch mode) only actually starts hidden when
	// a tray is available to bring it back -- otherwise showing it is the
	// only way this session could ever reach the window at all.
	if w.startHidden && w.tray != nil {
		win.Hide()
	} else {
		win.Show()
	}
	if w.token != nil {
		bindRemoteLockWindow(win)
		remotelock.SetEnabled(w.token.RemoteWindowLockEnabled())
		defer remotelock.SetEnabled(false)
	}
	w.app.Run()
}

func bindRemoteLockWindow(win fyne.Window) {
	nw, ok := win.(driver.NativeWindow)
	if !ok {
		return
	}
	nw.RunNative(func(ctx any) {
		if x, ok := ctx.(driver.X11WindowContext); ok && x.WindowHandle != 0 {
			remotelock.SetX11Window(x.WindowHandle)
		}
	})
}

// promptForUpdate runs the mandatory startup update check and, if a newer
// signed version is available, asks before installing it — replacing a
// forced silent update, which is jarring for a window the user is actively
// looking at. Only called for an engine-owning window (see ownsEngine); a
// --headless launch has no window to ask through and applies silently
// instead (see app.Start).
func (w *Window) promptForUpdate(parent fyne.Window) {
	manifest := update.Check(context.Background(), appVersion)
	if manifest == nil {
		return
	}

	fyne.Do(func() {
		showUpdateAvailableDialog(parent, manifest.Version, appVersion, func(confirmed bool) {
			if !confirmed {
				logrus.WithField("component", "update").Info("update declined by user")
				return
			}
			progress := showUpdateProgressDialog(parent, manifest.Version)
			go func() {
				err := update.DownloadAndApply(context.Background(), manifest, progress.Update)
				// A successful apply never returns here at all (it
				// hands off to a helper/relaunch and exits) —
				// reaching this point means it didn't.
				progress.Close()
				if err != nil {
					logrus.WithField("component", "update").WithError(err).Error("failed to apply update")
					fyne.Do(func() { dialog.ShowError(err, parent) })
				}
			}()
		})
	})
}

// updateTrayStatus keeps the tray's status header/icon in sync with the
// same data performRefresh already gathered for the main window's Status
// panel -- no separate polling loop. No-op if this session has no tray
// (w.tray is nil, see attachTray).
func (w *Window) updateTrayStatus(entStatus entitlement.Status, status uiStatus) {
	if w.tray == nil || w.token == nil {
		return
	}
	state := trayIconIdle
	header := "USBridge Agent — Idle"
	switch {
	case status.moonlightCount > 0:
		state = trayIconActive
		plural := "s"
		if status.moonlightCount == 1 {
			plural = ""
		}
		header = fmt.Sprintf("USBridge Agent — Streaming (%d client%s)", status.moonlightCount, plural)
	case !status.accessGranted:
		state = trayIconAttention
		header = "USBridge Agent — Permissions needed"
	}

	streamHost := w.token.SunshineStreamHost()
	if streamHost == "" {
		streamHost = "0.0.0.0"
	}
	sunshinePort := w.cfg.SunshinePort
	if sunshinePort == 0 {
		sunshinePort = 47990
	}
	info := fmt.Sprintf("%s · %s:%d", w.token.StreamerName(), streamHost, sunshinePort-1)
	w.tray.setStatus(header, info, state)
}

// refreshSupportButton keeps the Protocol-card buy chip in sync.
// Pro subscribers hide it unless they pick Enterprise (Buy Enterprise).
// Unpaid machines still see Buy Pro; it accents when that pick needs a purchase.
func (w *Window) refreshSupportButton(st entitlement.Status) {
	if w.supportBtn == nil {
		return
	}
	acc := account.Status{}
	if w.token != nil {
		acc = w.token.AccountStatus()
	}
	paid := protocolPaidTier(st, acc)
	needsBuy := protocolNeedsPurchase(w.protocolPick, st, acc)
	ownsPaid := accountHasPaidLicense(acc) || paid != ""
	switch {
	case needsBuy && w.protocolPick == protocolEnterprise:
		w.supportBtn.SetText(loc().BuyEnterprise)
		w.supportBtn.Show()
	case ownsPaid:
		w.supportBtn.Hide()
	default:
		w.supportBtn.SetText(loc().BuyPro)
		w.supportBtn.Show()
	}
	pending := needsBuy && !st.LinkInProgress && !st.DownloadInProgress &&
		w.protocolPick != "" && w.protocolPick != w.protocolApplied
	w.supportBtn.SetAccent(pending)
	w.supportBtn.Refresh()
}

func (w *Window) refreshTierBadge(st entitlement.Status) {
	w.dropProChromePinIfNeeded(st)
	if w.tierBadge != nil {
		w.tierBadge.SetStatus(st)
	}
	setChromeKind(protocolKeyFromStatus(st))
	if w.headerLine == nil {
		return
	}
	ch := currentChrome()
	if !ch.HeaderLineOn {
		w.headerLine.Hide()
		w.headerLine.Refresh()
		return
	}
	w.headerLine.FillColor = ch.HeaderLine
	w.headerLine.Show()
	w.headerLine.Refresh()
}

const chromeThemePrefKey = "chrome_theme"

func chromePinFromPref(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case protocolOpensource, "white":
		return protocolOpensource
	case protocolFree, "blue", "teal":
		return protocolFree
	case protocolPro, protocolEnterprise:
		return protocolPro
	default:
		return ""
	}
}

func chromePinToPref(pin string) string {
	switch pin {
	case protocolOpensource:
		return "white"
	case protocolFree:
		return "blue"
	case protocolPro:
		return "pro"
	default:
		return "default"
	}
}

func (w *Window) chromeProAllowed() bool {
	st, acc := w.protocolStatus()
	return protocolPaidTier(st, acc) != ""
}

func (w *Window) loadChromePin() {
	pin := ""
	if w.app != nil {
		pin = chromePinFromPref(w.app.Preferences().StringWithFallback(chromeThemePrefKey, "default"))
	}
	if pin == protocolPro && !w.chromeProAllowed() {
		pin = ""
	}
	setChromePin(pin)
}

func (w *Window) saveChromePin(pin string) {
	if pin == protocolPro && !w.chromeProAllowed() {
		return
	}
	setChromePin(pin)
	if w.app != nil {
		w.app.Preferences().SetString(chromeThemePrefKey, chromePinToPref(pin))
	}
	st := entitlement.Status{}
	if w.token != nil {
		st = w.token.EntitlementStatus()
	}
	w.refreshTierBadge(st)
}

func (w *Window) dropProChromePinIfNeeded(st entitlement.Status) {
	if chromePinned() != protocolPro {
		return
	}
	acc := account.Status{}
	if w.token != nil {
		acc = w.token.AccountStatus()
	}
	if protocolPaidTier(st, acc) != "" {
		return
	}
	setChromePin("")
	if w.app != nil {
		w.app.Preferences().SetString(chromeThemePrefKey, "default")
	}
}

func (w *Window) showThemeMenu(anchor fyne.CanvasObject) {
	if anchor == nil {
		return
	}
	pin := chromePinned()
	proOK := w.chromeProAllowed()
	showStyledTealMenuAbove(anchor, []styledMenuItem{
		{Label: loc().ThemeDefault, Selected: pin == "", OnTap: func() { w.saveChromePin("") }},
		{Label: "White", Selected: pin == protocolOpensource, OnTap: func() { w.saveChromePin(protocolOpensource) }},
		{Label: "Blue", Selected: pin == protocolFree, OnTap: func() { w.saveChromePin(protocolFree) }},
		{Label: "Pro", Selected: pin == protocolPro, Disabled: !proOK, OnTap: func() { w.saveChromePin(protocolPro) }},
	})
}

func (w *Window) showSettingsMenu(win fyne.Window, anchor fyne.CanvasObject) {
	if win == nil || anchor == nil {
		return
	}
	showStyledLightMenu(anchor, []styledMenuItem{
		{Label: loc().GeneralSettings, Icon: assets.SettingsIconLight, OnTap: func() { w.showGeneralSettingsDialog(win) }},
		{Label: loc().Language, Icon: assets.LanguageIconLight, OnTap: func() { w.showLanguageMenu(anchor) }},
		{Label: loc().Info, Icon: assets.InfoIconLight, OnTap: func() { w.showInfoMenu(anchor) }},
	})
}

func (w *Window) showInfoMenu(anchor fyne.CanvasObject) {
	if anchor == nil {
		return
	}
	showStyledLightMenu(anchor, []styledMenuItem{
		{Label: loc().Software, Icon: assets.GitHubIcon, OnTap: func() {
			w.openExternalLink("https://github.com/USBridge-Technologies/USBridge-Remote")
		}},
		{Label: loc().Hardware, Icon: assets.GitHubIcon, OnTap: func() {
			w.openExternalLink("https://github.com/USBridge-Technologies/USBridge-KVM-2.0/tree/main/docs")
		}},
		{Label: loc().Website, Icon: assets.OpenExternalIconLight, OnTap: func() {
			w.openExternalLink("https://www.usbridge.io/")
		}},
	})
}

func (w *Window) openExternalLink(raw string) {
	if w.app == nil {
		return
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed == nil {
		return
	}
	_ = w.app.OpenURL(parsed)
}

func (w *Window) showLanguageMenu(anchor fyne.CanvasObject) {
	if anchor == nil {
		return
	}
	current := "en"
	if w.app != nil {
		current = w.app.Preferences().StringWithFallback(i18n.LanguagePrefKey, "en")
	}
	setLang := func(code string) {
		if w.app != nil {
			w.app.Preferences().SetString(i18n.LanguagePrefKey, code)
		}
		i18n.SetLanguage(code)
		w.applyLanguage()
	}
	showStyledLightMenu(anchor, []styledMenuItem{
		{Label: "English", Selected: current == "en", OnTap: func() { setLang("en") }},
		{Label: "Español", Selected: current == "es", OnTap: func() { setLang("es") }},
		{Label: "Українська", Selected: current == "uk" || current == "ua", OnTap: func() { setLang("uk") }},
	})
}

func (w *Window) applyLanguage() {
	c := loc()
	if w.guiWin != nil {
		w.guiWin.SetTitle(c.AppTitle)
	}
	access := c.Accessibility
	if runtime.GOOS == "linux" {
		access = c.InputControl
	}
	w.accessCheck.SetBaseLabel(access)
	w.screenCaptureCheck.SetBaseLabel(c.ScreenCapture)
	w.refreshAutostartChrome()

	if w.gpuClockLang != nil {
		w.gpuClockLang.SetLabel(c.LockGPUClocks)
	}
	if w.rustshineWebRTCRow != nil {
		w.rustshineWebRTCRow.SetLabel(c.WebRTCToggle)
	}
	if w.mlClientsLang != nil {
		w.mlClientsLang.Text = c.MoonlightClients
		w.mlClientsLang.Refresh()
	}
	if w.usbDriverLang != nil {
		w.usbDriverLang.Text = c.USBPassthrough
		w.usbDriverLang.Refresh()
	}
	if w.usbDriverBtn != nil {
		label := c.InstallUSBDriver
		if runtime.GOOS == "windows" {
			label = c.GetUSBIPDriver
		}
		w.usbDriverBtn.SetText(label)
	}
	if w.clipboardToolBtn != nil {
		w.clipboardToolBtn.SetText(c.Install)
	}
	if w.clipboardLang != nil {
		w.clipboardLang.SetText(c.ClipboardTool)
	}
	if w.tsEmpty != nil {
		w.tsEmpty.Text = c.NoRemoteControllers
		w.tsEmpty.Refresh()
	}
	if w.themeBtn != nil {
		w.themeBtn.SetText(c.Theme)
	}
	if w.protocolChange != nil {
		w.protocolChange.SetContent(c.Change, headerChangeIcon)
	}
	if w.permPanel != nil {
		w.permPanel.SetTitle(c.Permissions)
	}
	if w.statusPanel != nil {
		w.statusPanel.SetTitle(c.Status)
	}
	if w.protocolPanel != nil {
		w.protocolPanel.SetTitle(c.Protocol)
	}
	if w.token != nil {
		st := w.token.EntitlementStatus()
		w.refreshSupportButton(st)
		w.refreshRustShineUI(st)
	}
	if w.tsAuthBtn != nil {
		if w.tsAuthBtn.logout {
			w.tsAuthBtn.SetContent(c.SignOut, headerLogoutIcon)
		} else {
			w.tsAuthBtn.SetContent(c.SignIn, headerLoginIcon)
		}
	}
	if w.tray != nil {
		w.tray.applyLanguage()
	}
}

func autostartRebootHint() string {
	if !autostart.NeedsReboot() {
		return ""
	}
	return loc().AutostartRebootHint
}

func autostartMenuLabel() string {
	label := loc().AutostartAtBoot
	if hint := autostartRebootHint(); hint != "" {
		return label + " " + hint
	}
	return label
}

// refreshAutostartChrome keeps the Permissions row hint and the tray
// Autostart label in sync. On Windows the hint is shown while the
// AUTO_START service is registered but not yet running this boot
// (autostart.NeedsReboot); after reboot SCM starts the service and the
// parenthetical disappears.
func (w *Window) refreshAutostartChrome() {
	if w == nil {
		return
	}
	hint := autostartRebootHint()
	if w.autostartLang != nil {
		w.autostartLang.SetLabel(loc().AutostartAtBoot)
		w.autostartLang.SetHint(hint)
	}
	if w.autostartCheck != nil && !w.autostartCheck.Disabled() {
		w.autostartCheck.SetChecked(autostart.IsEnabled())
	}
	if w.autostartLang != nil {
		w.autostartLang.SetEnabled(autostart.IsEnabled())
	}
	if w.tray != nil && w.tray.autostartItem != nil {
		label := autostartMenuLabel()
		checked := autostart.IsEnabled()
		if w.autostartCheck != nil && w.autostartCheck.Disabled() {
			checked = w.autostartCheck.Checked
		}
		if w.tray.autostartItem.Label != label || w.tray.autostartItem.Checked != checked {
			w.tray.autostartItem.Label = label
			w.tray.autostartItem.Checked = checked
			w.tray.refreshMenu()
		}
	}
}

// The four global licenses (see usbridge-entitlement-backend's
// desktopLicense.ts tier doc comment) as shown in showLicenseDialog's
// radio group -- exact row labels, used both to build the group and to
// tell rows apart in its OnChanged switch.
const (
	licenseRowSunshine            = "Sunshine (Open Source) — free"
	licenseRowRustShineFree       = "USBridge-streamer — Free"
	licenseRowRustShinePro        = "USBridge-streamer — Pro · $8/mo (4:4:4 color)"
	licenseRowRustShineEnterprise = "USBridge-streamer — Enterprise · $25/mo (session logs, team access)"
)

// tierDisplayName renders a bare tier string ("pro"/"enterprise") as the
// capitalized name used in confirm dialogs and status headlines.
func tierDisplayName(tier string) string {
	switch tier {
	case "pro":
		return "USBridge-streamer Pro"
	case "enterprise":
		return "USBridge-streamer Enterprise"
	default:
		return tier
	}
}

func currentLicenseRow(st entitlement.Status) string {
	if st.ActiveBackend == "rustshine" {
		switch st.Tier {
		case "pro":
			return licenseRowRustShinePro
		case "enterprise":
			return licenseRowRustShineEnterprise
		default:
			return licenseRowRustShineFree
		}
	}
	return licenseRowSunshine
}

// showLicenseDialog is the single entry point for the whole hardware-bound
// RustShine license flow: pick one of the four global licenses, wait for a
// subscription checkout to land, download, switch, clear -- all as one
// dialog that re-renders itself as entitlement.Status changes, rather than
// a sequence of separate popups. Opened only by an explicit click on
// supportBtn — never shown automatically.
func (w *Window) showLicenseDialog(parent fyne.Window) {
	if parent == nil || w.token == nil {
		return
	}

	var dlg *widget.PopUp
	stopPoll := make(chan struct{})
	closeDialog := func() {
		select {
		case <-stopPoll:
		default:
			close(stopPoll)
		}
		if dlg != nil {
			dlg.Hide()
		}
	}

	titleLabel := newDialogTitle(loc().LicenseDialogTitle)
	xBtn := widget.NewButtonWithIcon("", theme.CancelIcon(), func() { closeDialog() })
	titleRow := container.NewBorder(nil, nil, titleLabel, xBtn, nil)

	minWidth := canvas.NewRectangle(color.Transparent)
	minWidth.SetMinSize(fyne.NewSize(420, 1))

	body := container.NewVBox()

	// checkoutURLFallback: see its use in requestTier's click handler
	// below. Declared out here (not local to render) so it survives from
	// the goroutine's fyne.Do callback into the next render() call.
	var checkoutURLFallback string

	// pendingTierSwitch: set by requestTier right before opening a Pro/
	// Enterprise checkout, cleared once that tier actually lands AND
	// RustShine finishes staging -- at which point render auto-switches
	// the active backend to RustShine with no separate button click
	// needed. See render's own use of it below.
	var pendingTierSwitch string

	var render func(st entitlement.Status)
	render = func(st entitlement.Status) {
		body.RemoveAll()

		if pendingTierSwitch != "" && st.Tier == pendingTierSwitch && st.RustShineStaged && st.ActiveBackend != "rustshine" {
			pendingTierSwitch = ""
			go func() {
				_ = w.token.SetStreamBackend("rustshine")
				fyne.Do(func() { render(w.token.EntitlementStatus()) })
			}()
			return // this render call is stale -- the goroutine's own re-render shows the final state
		}

		switch {
		case st.LinkInProgress:
			body.Add(widget.NewLabel(loc().WaitingCheckout))
			body.Add(widget.NewProgressBarInfinite())
			// Previously there was no way out of this screen short of an
			// actual completed purchase or pollForLicenseTimeout (15
			// minutes) -- closing the dialog and reopening it (even via a
			// fresh "Support us" click) landed right back here, since
			// LinkInProgress lives on the App's entStatus, not this dialog.
			// A closed checkout tab with nothing bought had no way back to
			// the trial/buy buttons at all. CancelPurchase abandons the
			// background poll and clears LinkInProgress.
			cancelBtn := widget.NewButton(loc().Cancel, func() {
				go func() {
					w.token.CancelPurchase()
					fyne.Do(func() { render(w.token.EntitlementStatus()) })
				}()
			})
			cancelBtn.Importance = widget.LowImportance
			body.Add(container.NewCenter(cancelBtn))

		case st.DownloadInProgress:
			body.Add(widget.NewLabel(loc().DownloadingStreamer))
			pb := widget.NewProgressBar()
			if st.Progress >= 0 {
				pb.SetValue(st.Progress)
			}
			body.Add(pb)

		case !st.Linked:
			// Only reached in the brief window before the very first
			// background bootstrapFreeTier lands (see app.go's
			// recheckEntitlement), or if this machine's hardware id can't
			// be determined at all -- there is no more manual "start
			// trial"/"buy" step to wait on before showing something real.
			body.Add(widget.NewLabel(loc().SettingUp))
			body.Add(widget.NewProgressBarInfinite())
			if st.LastError != "" {
				errText := canvas.NewText(st.LastError, design.ColorTextMuted)
				errText.TextStyle.Italic = true
				body.Add(errText)
			}

		case pendingTierSwitch != "" && st.Tier == pendingTierSwitch && !st.RustShineStaged:
			// Payment landed (Tier already flipped) but RustShine hasn't
			// finished downloading yet -- app.go's applyIssuedToken already
			// kicked that off in the background the moment the tier
			// landed; the check at the top of render switches to it
			// automatically the instant RustShineStaged flips true, no
			// button needed.
			body.Add(widget.NewRichTextFromMarkdown(fmt.Sprintf(loc().TierActiveDownloading, tierDisplayName(pendingTierSwitch))))
			body.Add(widget.NewProgressBarInfinite())

		default:
			var headline string
			switch st.Tier {
			case "pro":
				headline = loc().RustShineProActive
			case "enterprise":
				headline = loc().RustShineEnterpriseActive
			default:
				headline = loc().PickLicenseBelow
			}
			body.Add(widget.NewRichTextFromMarkdown(headline))

			if st.LastError != "" {
				errText := canvas.NewText(st.LastError, design.ColorTextMuted)
				errText.TextStyle.Italic = true
				body.Add(errText)
			}
			// checkoutURLFallback carries a URL that requestTier's click
			// handler obtained fine but couldn't hand to the OS browser
			// itself (e.g. no default browser registered, a sandboxed/
			// headless environment without xdg-open) -- previously that
			// failure was silently swallowed, so the click visibly did
			// nothing at all once the checkout link had already been
			// fetched. Shown as a copyable link so the purchase can still
			// be completed manually.
			if checkoutURLFallback != "" {
				linkURI, _ := url.Parse(checkoutURLFallback)
				fallback := widget.NewLabel(loc().CouldntOpenBrowserCheckout)
				fallback.Wrapping = fyne.TextWrapWord
				body.Add(fallback)
				if linkURI != nil {
					link := widget.NewHyperlink(checkoutURLFallback, linkURI)
					link.Wrapping = fyne.TextWrapBreak
					copyBtn := newIconActionButton(loc().Copy, theme.ContentCopyIcon(), func() {
						parent.Clipboard().SetContent(checkoutURLFallback)
					})
					body.Add(container.NewBorder(nil, nil, nil, copyBtn, link))
				}
			}

			// The single switch for all four global licenses (see
			// usbridge-entitlement-backend's desktopLicense.ts tier doc
			// comment): which row is selected reflects what's ACTUALLY
			// active right now (backend + tier), not just a purchase
			// intent -- picking Sunshine or free RustShine always takes
			// effect immediately (nothing to buy, nothing to confirm);
			// picking Pro/Enterprise while not already entitled to it
			// opens a subscription checkout instead of switching anything
			// yet (see requestTier).
			options := []string{
				licenseRowSunshine,
				licenseRowRustShineFree,
				licenseRowRustShinePro,
				licenseRowRustShineEnterprise,
			}
			current := licenseRowSunshine
			if st.ActiveBackend == "rustshine" {
				switch st.Tier {
				case "pro":
					current = licenseRowRustShinePro
				case "enterprise":
					current = licenseRowRustShineEnterprise
				default:
					current = licenseRowRustShineFree
				}
			}

			// switchToRustShine ensures RustShine is staged (downloading
			// it first if this is the very first time, reusing the
			// existing st.DownloadInProgress spinner case above -- no
			// separate button needed) and then makes it the active
			// backend. Used both for the plain Free row and for a
			// Pro/Enterprise row that's already paid for.
			switchToRustShine := func() {
				go func() {
					if !st.RustShineStaged {
						if err := w.token.DownloadRustShine(nil); err != nil {
							fyne.Do(func() { render(w.token.EntitlementStatus()) })
							return
						}
					}
					_ = w.token.SetStreamBackend("rustshine")
					fyne.Do(func() { render(w.token.EntitlementStatus()) })
				}()
			}

			// requestTier handles a Pro/Enterprise row click: switches
			// straight to RustShine if this hardware id already carries
			// that tier (or better) -- no need to pay twice -- otherwise
			// confirms, then opens a subscription checkout for it.
			// pollForLicense (app.go) picks up the completed purchase in
			// the background same as before; render's own top-of-function
			// check auto-switches the backend once it lands and RustShine
			// finishes staging.
			requestTier := func(tier string) {
				if st.Tier == tier || (tier == "pro" && st.Tier == "enterprise") {
					switchToRustShine()
					return
				}
				d := dialog.NewConfirm(
					fmt.Sprintf(loc().SubscribeTitle, tierDisplayName(tier)),
					fmt.Sprintf(loc().SubscribeBody, tierDisplayName(tier)),
					func(confirmed bool) {
						if !confirmed {
							render(w.token.EntitlementStatus()) // reset the radio's visual selection back to what's actually active
							return
						}
						checkoutURLFallback = ""
						pendingTierSwitch = tier
						go func() {
							checkoutURL, err := w.token.StartPurchase(tier)
							if err != nil {
								// StartPurchase already recorded st.LastError -- the
								// re-render below picks it up and shows it.
								fyne.Do(func() { render(w.token.EntitlementStatus()) })
								return
							}
							parsed, parseErr := url.Parse(checkoutURL)
							openErr := parseErr
							if parseErr == nil {
								openErr = w.app.OpenURL(parsed)
							}
							fyne.Do(func() {
								if openErr != nil {
									checkoutURLFallback = checkoutURL
								}
								render(w.token.EntitlementStatus())
							})
						}()
					},
					parent,
				)
				d.SetConfirmText(loc().Yes)
				d.SetDismissText(loc().No)
				d.Show()
			}

			radio := widget.NewRadioGroup(options, nil)
			radio.Horizontal = false
			radio.SetSelected(current)
			radio.OnChanged = func(selected string) {
				if selected == current {
					return
				}
				switch selected {
				case licenseRowSunshine:
					go func() {
						_ = w.token.SetStreamBackend("sunshine")
						fyne.Do(func() { render(w.token.EntitlementStatus()) })
					}()
				case licenseRowRustShineFree:
					switchToRustShine()
				case licenseRowRustShinePro:
					requestTier("pro")
				case licenseRowRustShineEnterprise:
					requestTier("enterprise")
				}
			}
			body.Add(radio)
			// The "Web client (WebRTC)" toggle for RustShine lives in the
			// main window's Permissions column now (w.rustshineWebRTCRow),
			// not here -- it's useful to reach without opening this dialog.

			if st.Tier == "pro" || st.Tier == "enterprise" {
				note := widget.NewLabel(loc().LowerTierNote)
				note.Wrapping = fyne.TextWrapWord
				note.TextStyle.Italic = true
				body.Add(note)
			}

			clearBtn := widget.NewButton(loc().ForgetLicenseLocally, func() {
				d := dialog.NewConfirm(
					loc().ForgetLicenseTitle,
					loc().ForgetLicenseBody,
					func(confirmed bool) {
						if !confirmed {
							return
						}
						go func() {
							_ = w.token.ClearLicense()
							fyne.Do(func() { render(w.token.EntitlementStatus()) })
						}()
					},
					parent,
				)
				d.SetConfirmText(loc().Yes)
				d.SetDismissText(loc().No)
				d.Show()
			})
			clearBtn.Importance = widget.LowImportance
			body.Add(container.NewCenter(clearBtn))
		}

		body.Refresh()
	}

	render(w.token.EntitlementStatus())

	// accountBody: the account-login/rebind section (see internal/account),
	// deliberately its own block below the entitlement one above rather
	// than folded into render's switch -- account login is orthogonal to
	// which entitlement state (trial/licensed/unlicensed) this machine is
	// currently in, and stays visible/interactable regardless of it (e.g.
	// logging in to pick up a purchased license while this machine is
	// still shown as "unlicensed" above, right up until the rebind lands).
	accountBody := container.NewVBox()
	var accountLoginURLFallback string
	var renderAccount func(acc account.Status)
	renderAccount = func(acc account.Status) {
		accountBody.RemoveAll()
		accountBody.Add(widget.NewSeparator())

		switch {
		case acc.LoginInProgress:
			accountBody.Add(widget.NewLabel(loc().WaitingGoogleLogin))
			accountBody.Add(widget.NewProgressBarInfinite())
			cancelBtn := widget.NewButton(loc().Cancel, func() {
				w.token.CancelAccountLogin()
				fyne.Do(func() { renderAccount(w.token.AccountStatus()) })
			})
			cancelBtn.Importance = widget.LowImportance
			accountBody.Add(container.NewCenter(cancelBtn))
			if accountLoginURLFallback != "" {
				linkURI, _ := url.Parse(accountLoginURLFallback)
				fallback := widget.NewLabel(loc().CouldntOpenBrowserLogin)
				fallback.Wrapping = fyne.TextWrapWord
				accountBody.Add(fallback)
				if linkURI != nil {
					link := widget.NewHyperlink(accountLoginURLFallback, linkURI)
					link.Wrapping = fyne.TextWrapBreak
					accountBody.Add(link)
				}
			}

		case acc.LoggedIn:
			accountBody.Add(widget.NewLabel(fmt.Sprintf("%s %s", loc().SignedInAs, acc.Email)))
			if acc.LastError != "" {
				errText := canvas.NewText(acc.LastError, design.ColorTextMuted)
				errText.TextStyle.Italic = true
				accountBody.Add(errText)
			}
			if len(acc.Licenses) == 0 {
				accountBody.Add(widget.NewLabel(loc().NoDesktopLicenses))
			}
			for _, lic := range acc.Licenses {
				lic := lic
				row := widget.NewLabel(fmt.Sprintf("%s — %s", lic.Identifier, lic.Status))
				useBtn := widget.NewButton(loc().UseLicenseOnDevice, func() {
					go func() {
						_ = w.token.RebindLicenseToThisDevice(lic.Identifier)
						fyne.Do(func() {
							renderAccount(w.token.AccountStatus())
							render(w.token.EntitlementStatus())
						})
					}()
				})
				useBtn.Importance = widget.LowImportance
				if lic.Status != "licensed" || acc.RebindInProgress {
					useBtn.Disable()
				}
				accountBody.Add(container.NewBorder(nil, nil, nil, useBtn, row))
			}
			logoutBtn := widget.NewButton(loc().LogOut, func() {
				go func() {
					_ = w.token.LogoutAccount()
					fyne.Do(func() { renderAccount(w.token.AccountStatus()) })
				}()
			})
			logoutBtn.Importance = widget.LowImportance
			accountBody.Add(container.NewCenter(logoutBtn))

		default:
			intro := widget.NewLabel(loc().AlreadyBoughtIntro)
			intro.Wrapping = fyne.TextWrapWord
			accountBody.Add(intro)
			if acc.LastError != "" {
				errText := canvas.NewText(acc.LastError, design.ColorTextMuted)
				errText.TextStyle.Italic = true
				accountBody.Add(errText)
			}
			loginBtn := widget.NewButton(loc().LogInWithGoogle, func() {
				accountLoginURLFallback = ""
				go func() {
					loginURL, err := w.token.StartAccountLogin()
					if err != nil {
						fyne.Do(func() { renderAccount(w.token.AccountStatus()) })
						return
					}
					parsed, parseErr := url.Parse(loginURL)
					openErr := parseErr
					if parseErr == nil {
						openErr = w.app.OpenURL(parsed)
					}
					fyne.Do(func() {
						if openErr != nil {
							accountLoginURLFallback = loginURL
						}
						renderAccount(w.token.AccountStatus())
					})
				}()
			})
			loginBtn.Importance = widget.LowImportance
			accountBody.Add(container.NewCenter(loginBtn))
		}

		accountBody.Refresh()
	}
	initialAccStatus := w.token.AccountStatus()
	renderAccount(initialAccStatus)
	if initialAccStatus.LoggedIn {
		w.token.RefreshAccountLicenses()
	}

	content := container.NewVBox(titleRow, minWidth, widget.NewSeparator(), body, accountBody)
	card := wrapDialogCard(container.NewPadded(content))
	dlg = widget.NewModalPopUp(container.NewCenter(card), parent.Canvas())
	dlg.Show()

	// Polls while the dialog is open so LinkInProgress/DownloadInProgress
	// (and the account login/rebind's own in-progress flags) advance to
	// their next state on their own (a link completing in the browser, a
	// download finishing) without the user needing to close and reopen
	// this dialog to see it. Only actually re-renders a section when its
	// own snapshot changed since the last tick -- rebuilding every widget
	// unconditionally every 2s (the original version of this loop) is
	// wasteful and visibly flickers; the client's equivalent dialog had
	// the same pattern and it was actively destructive there (wiped an
	// in-progress sync-passphrase Entry on every tick, see
	// client/internal/gui/main_window_account.go's accountDialogSnapshot)
	// -- this dialog has no text Entry to lose, but the same fix still
	// removes the pointless flicker.
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		lastSt := w.token.EntitlementStatus()
		lastAcc := newAccountSnapshot(w.token.AccountStatus())
		for {
			select {
			case <-stopPoll:
				return
			case <-ticker.C:
			}
			st := w.token.EntitlementStatus()
			acc := w.token.AccountStatus()
			accSnap := newAccountSnapshot(acc)
			stChanged := st != lastSt
			accChanged := accSnap != lastAcc
			if !stChanged && !accChanged {
				continue
			}
			lastSt, lastAcc = st, accSnap
			fyne.Do(func() {
				if stChanged {
					render(st)
				}
				if accChanged {
					renderAccount(acc)
				}
			})
		}
	}()
}

func (w *Window) performRefresh() {
	go func() {
		status := uiStatus{}
		if w.ts != nil {
			status.tsStatus, _ = w.ts.Status(context.Background())
		}
		if w.perms != nil {
			status.accessGranted = w.perms.AccessibilityGranted()
		}
		var entStatus entitlement.Status
		if w.token != nil {
			if clients, err := w.token.ListSunshineClients(); err == nil {
				status.moonlightCount = len(clients)
			}
			entStatus = w.token.EntitlementStatus()
			status.usbStatus = w.token.USBPassthroughStatus()
			status.streamerRunning = w.token.StreamerRunning()
		}
		fyne.Do(func() {
			w.maybeFinishPendingTierSwitch(entStatus)
			w.refreshSupportButton(entStatus)
			w.refreshTierBadge(entStatus)
			w.syncProtocolPicker(entStatus)
			w.refreshAccountAvatar()
			w.refreshRustShineUI(entStatus)
			w.refreshUSBPassthroughUI(entStatus, status.usbStatus)
			setStatusDot(w.streamerStatusDot, status.streamerRunning)
			if w.token != nil {
				w.setStreamerDisplay(w.token.StreamerName())
			}
			if w.accessCheck != nil {
				w.accessCheck.SetChecked(status.accessGranted)
			}
			if w.moonlightBtn != nil {
				w.moonlightBtn.SetText(fmt.Sprintf("%d", status.moonlightCount))
			}
			w.refreshScreenCaptureUI()
			w.refreshClipboardToolUI()
			w.refreshAutostartChrome()
			w.refreshTailscaleWithStatus(status.tsStatus)
			w.updateTrayStatus(entStatus, status)
		})
	}()
}

func (w *Window) refreshTailscaleWithStatus(status *tailscale.Status) {
	if w.tsPeers == nil || w.tsMeta == nil {
		return
	}
	if w.ts == nil || status == nil {
		w.setTailscaleInfo(loc().TokenUnavail, loc().TokenUnavail, loc().TokenUnavail)
		w.setTailscaleSessions(nil)
		w.setTailscaleLoggedIn(false)
		return
	}

	if !status.LoggedIn {
		w.setTailscaleInfo(loc().SignedOut, loc().SignInRequired, loc().SignInToPublish)
		w.setTailscaleSessions(nil)
		w.setTailscaleLoggedIn(false)
		return
	}

	endpoint := status.Self.IP4
	if endpoint == "127.0.0.1" || strings.TrimSpace(endpoint) == "" {
		endpoint = status.Self.DNSName
	}
	if strings.TrimSpace(endpoint) == "" {
		endpoint = status.Self.HostName
	}

	w.setTailscaleInfo(
		strings.ToLower(status.Backend),
		fallbackValue(status.Self.UserLogin, loc().Connected),
		fmt.Sprintf("%s (embedded)", endpoint),
	)
	w.setTailscaleLoggedIn(true)

	// Update active sessions
	var activePeers []tsActivePeer
	for _, p := range status.Peers {
		if !isActiveTailscalePeer(p) {
			continue
		}
		peer := tsActivePeer{
			name: fallbackValue(p.UserLogin, p.HostName),
			ip4:  p.IP4,
			kind: loc().RelayDERP,
		}
		if p.CurAddr != "" {
			peer.kind = "P2P DIRECT"
			peer.via = p.CurAddr
		} else if p.Relay != "" {
			peer.kind = fmt.Sprintf(loc().RelayDERPFmt, p.Relay)
		}
		activePeers = append(activePeers, peer)
	}

	if len(activePeers) > 0 {
		w.setTailscaleSessions(activePeers)
	} else {
		w.setTailscaleSessions(nil)
	}
}

type tsActivePeer struct {
	name string
	ip4  string
	kind string
	via  string
}

func newTSPeerRow(p tsActivePeer) fyne.CanvasObject {
	name := canvas.NewText(p.name, design.ColorTextLight)
	name.TextSize = 10
	bits := []fyne.CanvasObject{name}
	detail := p.kind
	if p.ip4 != "" {
		if detail != "" {
			detail = fmt.Sprintf("(%s) - %s", p.ip4, detail)
		} else {
			detail = "(" + p.ip4 + ")"
		}
	}
	if detail != "" {
		line := canvas.NewText(detail, design.ColorMutedOlive)
		line.TextSize = 9
		bits = append(bits, line)
	}
	if p.via != "" {
		via := canvas.NewText("("+p.via+")", design.ColorAddress)
		via.TextSize = 9
		via.TextStyle.Monospace = true
		bits = append(bits, via)
	}
	return container.New(&tightVBoxLayout{gap: 1}, bits...)
}

func (w *Window) setTailscaleSessions(peers []tsActivePeer) {
	if w.tsEmpty != nil {
		if len(peers) == 0 {
			w.tsEmpty.Show()
		} else {
			w.tsEmpty.Hide()
		}
		w.tsEmpty.Refresh()
	}
	if w.tsPeers == nil {
		return
	}
	if len(peers) == 0 {
		w.tsPeers.Objects = nil
		w.tsPeers.Hide()
		w.tsPeers.Refresh()
		return
	}
	rows := make([]fyne.CanvasObject, 0, len(peers))
	for _, p := range peers {
		rows = append(rows, newTSPeerRow(p))
	}
	w.tsPeers.Objects = rows
	w.tsPeers.Refresh()
	w.tsPeers.Show()
}

func (w *Window) setTailscaleLoggedIn(on bool) {
	if w.tsAuthBtn != nil {
		w.tsAuthBtn.logout = on
		if on {
			w.tsAuthBtn.SetContent(loc().SignOut, headerLogoutIcon)
		} else {
			w.tsAuthBtn.SetContent(loc().SignIn, headerLoginIcon)
		}
	}
	if w.tsToggle != nil {
		w.tsToggle.SetOn(on)
	}
}

func (w *Window) setTailscaleBusy(busy bool) {
	if w.tsAuthBtn != nil {
		if busy {
			w.tsAuthBtn.Disable()
		} else {
			w.tsAuthBtn.Enable()
		}
	}
	if w.tsToggle != nil {
		w.tsToggle.SetLoading(busy)
	}
}

// toggleTailscaleAuth is the shared Sign In / Sign Out path for both the
// Tailscale card button and the header chip: logged in logs out, otherwise
// it starts the Google login flow (same as the original tsAuthBtn handler).
func (w *Window) toggleTailscaleAuth() {
	if w.ts == nil {
		w.setTailscaleInfo(loc().ServiceUnavailable, "", "")
		return
	}

	w.setTailscaleBusy(true)
	go func() {
		defer fyne.Do(func() { w.setTailscaleBusy(false) })

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		status, statusErr := w.ts.Status(ctx)
		if statusErr == nil && status != nil && status.LoggedIn {
			if err := w.ts.Logout(ctx); err != nil {
				fyne.Do(func() {
					w.setTailscaleInfo(fmt.Sprintf(loc().LogoutError, err), "", "")
				})
			}
			w.performRefresh()
			return
		}
		if !w.awaitingLocalLogin.CompareAndSwap(false, true) {
			return
		}
		fyne.Do(func() { w.setTailscaleInfo(loc().StartingLogin, "", "") })
		authURL, err := w.ts.StartLogin(ctx)
		if err != nil {
			w.awaitingLocalLogin.Store(false)
			fyne.Do(func() { w.setTailscaleInfo(fmt.Sprintf(loc().ErrorFmt, err), "", "") })
			return
		}
		// Open directly from StartLogin's own return value instead of waiting
		// on SetAuthURLHandler alone: that handler is fed by tsnet's
		// printAuthURLLoop, a goroutine tsnet starts exactly once per Server
		// lifetime and permanently exits the moment login first succeeds (see
		// tsnet.Server.printAuthURLLoop — "state is Running; done"). After any
		// Sign Out + Sign In cycle within the same agent run, that loop is
		// already dead, so the handler never fires again and the button looked
		// broken no matter how many times it was clicked. StartLogin's own
		// 500ms status poll doesn't depend on that loop at all, so it keeps
		// working across repeated login cycles. The CompareAndSwap still guards
		// against double-opening if the (still-registered, occasionally still
		// alive) handler also fires for the same click.
		if authURL != "" && w.awaitingLocalLogin.CompareAndSwap(true, false) {
			parsed, parseErr := url.Parse(strings.TrimSpace(authURL))
			if parseErr != nil {
				logrus.Errorf("tailscale ui: failed to parse auth URL %q: %v", authURL, parseErr)
				fyne.Do(func() { w.setTailscaleInfo(loc().InvalidLoginURL, "", "") })
				return
			}
			fyne.Do(func() {
				if w.app != nil {
					_ = w.app.OpenURL(parsed)
				}
				w.setTailscaleInfo(loc().LoginLinkOpened, "", "")
			})
		}
	}()
}

// tsMetaBlock is the Tailscale card's Account / Address chrome — labels
// #c5c8b5, host in the same monospace (dotted zeros) + #ebffbc the client
// uses on LAN/TS, and any "(embedded)" (or other) suffix after the host
// smaller and muted.
type tsMetaBlock struct {
	root          *fyne.Container
	status        *canvas.Text
	details       *fyne.Container
	accountValue  *canvas.Text
	addressValue  *canvas.Text
	addressSuffix *canvas.Text
}

func newTSMetaLabel(s string) *canvas.Text {
	t := canvas.NewText(s, design.ColorMutedOlive)
	t.TextSize = 10
	return t
}

func newTSMetaBlock() *tsMetaBlock {
	accountLbl := newTSMetaLabel("Account")
	addressLbl := newTSMetaLabel("Address")
	labelW := fyne.MeasureText("Address", 10, fyne.TextStyle{}).Width + 6

	accountValue := canvas.NewText("", design.ColorTextLight)
	accountValue.TextSize = 12

	addressValue := canvas.NewText("", design.ColorAddress)
	addressValue.TextSize = 10
	addressValue.TextStyle.Monospace = true

	addressSuffix := canvas.NewText("", design.ColorMutedOlive)
	addressSuffix.TextSize = 9

	accountRow := newTSMetaRow(labelW, 16, accountLbl, accountValue)
	addressRow := newTSMetaRow(labelW, 14, addressLbl,
		container.New(&tightHBoxLayout{gap: 4}, addressValue, addressSuffix))

	status := canvas.NewText("", design.ColorMutedOlive)
	status.TextSize = 10
	details := container.New(&tightVBoxLayout{gap: 5}, accountRow, addressRow)
	heightLock := canvas.NewRectangle(color.Transparent)
	heightLock.SetMinSize(fyne.NewSize(0, 16+5+14))
	b := &tsMetaBlock{
		root: newExactInset(container.NewStack(
			heightLock,
			details,
			container.NewCenter(status),
		), 0, 0, 4, 12),
		status:        status,
		details:       details,
		accountValue:  accountValue,
		addressValue:  addressValue,
		addressSuffix: addressSuffix,
	}
	b.Set("", loc().NotConnected, loc().TokenUnavail)
	return b
}

func newTSMetaRow(labelW, rowH float32, label, value fyne.CanvasObject) fyne.CanvasObject {
	lock := canvas.NewRectangle(color.Transparent)
	lock.SetMinSize(fyne.NewSize(0, rowH))
	inner := container.NewBorder(nil, nil,
		container.NewGridWrap(fyne.NewSize(labelW, rowH), label),
		nil, value)
	return container.NewMax(lock, inner)
}

func splitAddressSuffix(s string) (host, extra string) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", ""
	}
	if i := strings.Index(s, " ("); i >= 0 {
		return strings.TrimSpace(s[:i]), strings.TrimSpace(s[i:])
	}
	return s, ""
}

func looksLikeHost(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" || strings.Contains(s, " ") {
		return false
	}
	if net.ParseIP(s) != nil {
		return true
	}
	return strings.Contains(s, ".")
}

func looksLikeEmail(s string) bool {
	s = strings.TrimSpace(s)
	at := strings.IndexByte(s, '@')
	if at <= 0 || at >= len(s)-1 {
		return false
	}
	return strings.Contains(s[at+1:], ".")
}

func (b *tsMetaBlock) Set(status, account, address string) {
	if b == nil {
		return
	}
	if strings.TrimSpace(account) == "" && strings.TrimSpace(address) == "" {
		b.status.Text = status
		b.status.Show()
		b.details.Hide()
		b.status.Refresh()
		b.root.Refresh()
		return
	}
	b.status.Hide()
	b.details.Show()
	b.accountValue.Text = account
	if looksLikeEmail(account) {
		b.accountValue.Color = design.ColorTextLight
		b.accountValue.TextSize = 12
	} else {
		b.accountValue.Color = design.ColorMutedOlive
		b.accountValue.TextSize = 10
	}
	host, extra := splitAddressSuffix(address)
	b.addressValue.Text = host
	if looksLikeHost(host) || extra != "" {
		b.addressValue.Color = design.ColorAddress
		b.addressValue.TextStyle.Monospace = true
	} else {
		b.addressValue.Color = design.ColorMutedOlive
		b.addressValue.TextStyle.Monospace = false
	}
	if extra != "" {
		b.addressSuffix.Text = extra
		b.addressSuffix.Show()
	} else {
		b.addressSuffix.Text = ""
		b.addressSuffix.Hide()
	}
	b.accountValue.Refresh()
	b.addressValue.Refresh()
	b.addressSuffix.Refresh()
	b.details.Refresh()
	b.root.Refresh()
}

func (w *Window) setTailscaleInfo(status, account, address string) {
	if w.tsMeta != nil {
		w.tsMeta.Set(status, account, address)
	}
}

func fallbackValue(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func isActiveTailscalePeer(p tailscale.Peer) bool {
	if p.Active {
		return true
	}
	if strings.TrimSpace(p.CurAddr) != "" {
		return true
	}
	return false
}

func buildQuickConnectLink(internalHost, tailscaleHost, masterKey, protocol string) string {
	masterKey = strings.TrimSpace(masterKey)
	if masterKey == "" || masterKey == "unavailable" {
		return ""
	}
	if strings.TrimSpace(internalHost) == "" && strings.TrimSpace(tailscaleHost) == "" {
		return ""
	}

	values := url.Values{}
	if strings.TrimSpace(internalHost) != "" {
		values.Set("internal_host", strings.TrimSpace(internalHost))
	}
	if strings.TrimSpace(tailscaleHost) != "" {
		values.Set("tailscale_host", strings.TrimSpace(tailscaleHost))
	}
	values.Set("master_key", masterKey)
	if strings.TrimSpace(protocol) != "" {
		values.Set("protocol", strings.TrimSpace(protocol))
	}
	return fmt.Sprintf("usbridge://connect?%s", values.Encode())
}

func (w *Window) quickConnectTargets() (internalHost string, tailscaleHost string, protocol string) {
	internalHost = localQuickConnectIPv4()
	if w.ts != nil {
		if status, err := w.ts.Status(context.Background()); err == nil && status != nil && status.LoggedIn {
			switch {
			case strings.TrimSpace(status.Self.IP4) != "":
				tailscaleHost = strings.TrimSpace(status.Self.IP4)
			case strings.TrimSpace(status.Self.DNSName) != "":
				tailscaleHost = strings.TrimSpace(status.Self.DNSName)
			}
		}
	}

	if tailscaleHost != "" {
		return internalHost, tailscaleHost, "tailscale"
	}
	if internalHost != "" {
		return internalHost, "", "direct"
	}
	return "", "", ""
}

func localQuickConnectIPv4() string {
	return netutil.PreferredIPv4()
}

// needsWarnBadge reports whether a host binding warrants a ⚠ warning.
// Warns for all-interfaces (0.0.0.0/"") and LAN IPs — not for Tailscale or loopback.
func needsWarnBadge(host string) bool {
	if host == "" || host == "0.0.0.0" {
		return true
	}
	p := net.ParseIP(host)
	if p == nil {
		return false
	}
	if p.IsLoopback() || isTailscaleIP(p) {
		return false
	}
	return true // LAN, public, or unknown — show warning
}

// makeWarningBadge returns a yellow ⚠ badge for rows that listen on 0.0.0.0.
func makeWarningBadge() *canvas.Text {
	t := canvas.NewText("⚠", design.ColorAlert)
	t.TextSize = 13
	return t
}

// makeStatusLabel matches Tailscale's "Account" / "Address" labels:
// 10px muted olive, not bold.
func makeStatusLabel(text string) *canvas.Text {
	t := canvas.NewText(text, design.ColorMutedOlive)
	t.TextSize = 10
	return t
}

// makeStatusName matches the Tailscale email: 12px light.
func makeStatusName(text string) *canvas.Text {
	t := canvas.NewText(text, design.ColorTextLight)
	t.TextSize = 12
	return t
}

// makeStatusAddress matches the Tailscale host: 10px #ebffbc monospace.
func makeStatusAddress(text string) *canvas.Text {
	t := canvas.NewText(text, design.ColorAddress)
	t.TextSize = 10
	t.TextStyle.Monospace = true
	return t
}

func makeStatusValue(text string) *canvas.Text {
	t := canvas.NewText(text, design.ColorMutedOlive)
	t.TextSize = 10
	return t
}

func splitStreamerKind(s string) (name, kind string) {
	s = strings.TrimSpace(s)
	if i := strings.LastIndex(s, " ("); i >= 0 && strings.HasSuffix(s, ")") {
		return strings.TrimSpace(s[:i]), strings.TrimSpace(s[i:])
	}
	return s, ""
}

func (w *Window) setStreamerDisplay(full string) {
	name, kind := splitStreamerKind(full)
	if w.streamerNameLabel != nil && w.streamerNameLabel.Text != name {
		w.streamerNameLabel.Text = name
		w.streamerNameLabel.Refresh()
	}
	if w.streamerKindLabel == nil {
		return
	}
	if kind == "" {
		w.streamerKindLabel.Hide()
		return
	}
	if w.streamerKindLabel.Text != kind {
		w.streamerKindLabel.Text = kind
		w.streamerKindLabel.Refresh()
	}
	w.streamerKindLabel.Show()
}

func newStatusRow(left, right fyne.CanvasObject) *fyne.Container {
	lock := canvas.NewRectangle(color.Transparent)
	lock.SetMinSize(fyne.NewSize(0, tinyActionSize))
	inner := left
	if right != nil {
		inner = container.New(&flushEndsLayout{}, left, right)
	} else {
		inner = container.New(&flushEndsLayout{}, left)
	}
	return container.NewMax(lock, inner)
}

type statusLink struct {
	widget.BaseWidget
	label *canvas.Text
	onTap func()
}

func newStatusLink(text string, clr color.Color, size float32, onTap func()) *statusLink {
	t := canvas.NewText(text, clr)
	t.TextSize = size
	t.TextStyle.Monospace = true
	l := &statusLink{label: t, onTap: onTap}
	l.ExtendBaseWidget(l)
	return l
}

func (l *statusLink) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(l.label)
}

func (l *statusLink) MinSize() fyne.Size {
	if l.label != nil {
		return l.label.MinSize()
	}
	return fyne.NewSize(0, 0)
}

func (l *statusLink) Tapped(*fyne.PointEvent) {
	if l.onTap != nil {
		l.onTap()
	}
}

func (l *statusLink) TappedSecondary(*fyne.PointEvent) {}

func (l *statusLink) Cursor() desktop.Cursor { return desktop.PointerCursor }

func newTinyGlyphButton(icon fyne.Resource, tapped func()) *iconActionButton {
	return newTinyGlyphButtonColored(icon, "", tapped)
}

func newTinyGlyphButtonColored(icon fyne.Resource, fill fyne.ThemeColorName, tapped func()) *iconActionButton {
	if fill != "" {
		icon = theme.NewColoredResource(icon, fill)
	}
	b := newIconActionButton("", icon, tapped)
	b.Tiny = true
	return b
}

func osHeaderIcon() fyne.Resource {
	switch runtime.GOOS {
	case "windows":
		return assets.WindowsIcon
	case "darwin":
		return assets.MacOSIcon
	default:
		return assets.LinuxIcon
	}
}

// newStatusDot returns a small filled circle for a running/stopped traffic
// light next to a status row -- starts red (design.ColorStatusOff); callers
// set the actual state via setStatusDot on the next refresh tick rather than
// guessing an initial value here. Wrapped in a fixed-size GridWrap, not
// placed directly in an HBox/Center: canvas.Circle.MinSize() is hardcoded to
// (1,1) with no setter (see fyne's own canvas/circle.go), so any layout that
// sizes children by their own MinSize collapses it to a 1px speck otherwise.
func newStatusDot() *canvas.Circle {
	dot := canvas.NewCircle(design.ColorStatusOff)
	dot.StrokeWidth = 0
	return dot
}

// statusDotBox wraps the Streamer/USB traffic light at a fixed 8x8, nudged
// 1px left and 2px up so it sits on the label baseline. See newStatusDot
// for why a bare Circle can't size itself.
func statusDotBox(dot *canvas.Circle) fyne.CanvasObject {
	return container.New(&checkNudgeLayout{dx: -1, dy: -2},
		container.NewGridWrap(fyne.NewSize(8, 8), dot))
}

// setStatusDot recolors dot to design.ColorStatusOn (lime, running) or
// design.ColorStatusOff (rose, not running) and refreshes it -- nil-safe so
// callers don't need their own guard when a row's widgets haven't been
// built yet (e.g. a thin-client GUI attached before its first poll).
func setStatusDot(dot *canvas.Circle, running bool) {
	if dot == nil {
		return
	}
	want := design.ColorStatusOff
	if running {
		want = design.ColorStatusOn
	}
	if dot.FillColor != want {
		dot.FillColor = want
		dot.Refresh()
	}
}

// ipOption pairs a display string (annotated) with the actual IP value.
type ipOption struct{ display, value string }

func isTailscaleIP(ip net.IP) bool {
	_, cidr, _ := net.ParseCIDR("100.64.0.0/10")
	return cidr != nil && cidr.Contains(ip)
}

func isPrivateLANIP(ip net.IP) bool {
	for _, s := range []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"} {
		_, lan, _ := net.ParseCIDR(s)
		if lan != nil && lan.Contains(ip) {
			return true
		}
	}
	return false
}

func annotateIP(ipStr string) string {
	if ipStr == "0.0.0.0" {
		return "0.0.0.0  (all interfaces)"
	}
	p := net.ParseIP(ipStr)
	if p == nil {
		return ipStr
	}
	switch {
	case p.IsLoopback():
		return ipStr + "  (loopback)"
	case isTailscaleIP(p):
		return ipStr + "  (Tailscale)"
	case isPrivateLANIP(p):
		return ipStr + "  (LAN)"
	default:
		return ipStr
	}
}

// localIPOptions returns annotated IP options for host-binding dropdowns.
// Always includes 0.0.0.0 and 127.0.0.1 first, then active interface IPs.
func localIPOptions() []ipOption {
	raw := []string{"0.0.0.0", "127.0.0.1"}
	seen := map[string]bool{"0.0.0.0": true, "127.0.0.1": true}
	if ifaces, err := net.Interfaces(); err == nil {
		for _, iface := range ifaces {
			if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
				continue
			}
			addrs, _ := iface.Addrs()
			for _, addr := range addrs {
				ip, _, err := net.ParseCIDR(addr.String())
				if err != nil || ip.To4() == nil {
					continue
				}
				s := ip.String()
				if !seen[s] {
					seen[s] = true
					raw = append(raw, s)
				}
			}
		}
	}
	opts := make([]ipOption, 0, len(raw))
	for _, ip := range raw {
		opts = append(opts, ipOption{display: annotateIP(ip), value: ip})
	}
	return opts
}

// ipSelectOptions builds display list + value lookup for a widget.Select,
// and finds the display string matching currentVal.
func ipSelectOptions(currentVal string) (displays []string, valueFor map[string]string, currentDisplay string) {
	opts := localIPOptions()
	displays = make([]string, len(opts))
	valueFor = make(map[string]string, len(opts))
	for i, o := range opts {
		displays[i] = o.display
		valueFor[o.display] = o.value
		if o.value == currentVal {
			currentDisplay = o.display
		}
	}
	if currentDisplay == "" {
		currentDisplay = currentVal
	}
	return
}

func newDialogTitle(text string) *canvas.Text {
	title := canvas.NewText(text, design.ColorSectionTitle)
	title.TextSize = 12
	title.TextStyle.Bold = true
	return title
}

func newDialogTopAccentBar() fyne.CanvasObject {
	teal := design.ColorTeal
	lime := design.ColorCTA
	tealTransparent := color.NRGBA{R: 0x41, G: 0xe0, B: 0xc3, A: 0}
	limeTransparent := color.NRGBA{R: 0xc4, G: 0xe7, B: 0x7a, A: 0}
	accentLeftFade := canvas.NewHorizontalGradient(tealTransparent, teal)
	accentLeftFade.SetMinSize(fyne.NewSize(70, 2))
	accentRightFade := canvas.NewHorizontalGradient(lime, limeTransparent)
	accentRightFade.SetMinSize(fyne.NewSize(70, 2))
	accentMid := canvas.NewHorizontalGradient(teal, lime)
	return container.NewBorder(nil, nil, accentLeftFade, accentRightFade, accentMid)
}

func wrapDialogCard(inner fyne.CanvasObject) fyne.CanvasObject {
	bg := canvas.NewRectangle(design.ColorGray900)
	bg.CornerRadius = design.RadiusLG
	bg.StrokeColor = design.ColorChromeOlive
	bg.StrokeWidth = 1
	return container.NewStack(bg, container.NewBorder(newDialogTopAccentBar(), nil, nil, nil, inner))
}

func newBrandLockup() fyne.CanvasObject {
	logo := canvas.NewImageFromResource(currentChrome().Logo)
	logo.FillMode = canvas.ImageFillContain
	const logoAspectRatio = 1284.0 / 236.0
	const logoHeight float32 = 30
	logo.SetMinSize(fyne.NewSize(logoHeight*logoAspectRatio, logoHeight))
	brandLockup = logo
	return logo
}

func newPanel(icon fyne.Resource, title string, headerRight fyne.CanvasObject, content fyne.CanvasObject) fyne.CanvasObject {
	return newPanelHeader(icon, title, nil, headerRight, content)
}

func newPanelHeader(icon fyne.Resource, title string, afterTitle, headerRight fyne.CanvasObject, content fyne.CanvasObject) fyne.CanvasObject {
	iconImg := canvas.NewImageFromResource(icon)
	iconImg.FillMode = canvas.ImageFillContain
	iconImg.SetMinSize(fyne.NewSize(16, 16))

	titleText := canvas.NewText(title, design.ColorTextLight)
	titleText.TextSize = 11
	titleText.TextStyle.Bold = true

	titleBits := []fyne.CanvasObject{iconImg, titleText}
	if afterTitle != nil {
		titleBits = append(titleBits, afterTitle)
	}
	titleRow := container.New(&headerTitleLayout{gap: 8, yNudge: 3}, titleBits...)
	var headerInner fyne.CanvasObject = titleRow
	if headerRight != nil {
		headerInner = container.New(&flushEndsLayout{}, titleRow, container.New(&headerTitleLayout{yNudge: 5}, headerRight))
	}

	headerBand := canvas.NewRectangle(color.Transparent)
	headerBand.SetMinSize(fyne.NewSize(0, 36))
	header := container.NewStack(
		headerBand,
		newExactInset(headerInner, 14, 10, 4, 2),
	)

	sep := canvas.NewRectangle(design.ColorDivider)
	sep.SetMinSize(fyne.NewSize(0, 1))

	denseContent := container.NewThemeOverride(content, &compactPanelTheme{Theme: design.NewBrandTheme()})
	fillLay := &viewportFillLayout{inner: denseContent}
	fill := container.New(fillLay, denseContent)
	scrolled := container.NewVScroll(fill)
	body := container.NewBorder(
		container.New(&tightVBoxLayout{gap: 0}, header, sep),
		nil, nil, nil,
		newExactInset(container.New(&contentMinScrollLayout{content: denseContent, fill: fillLay}, scrolled), 14, 14, 3, 12),
	)

	bg := canvas.NewRectangle(design.ColorGray900)
	bg.CornerRadius = design.RadiusLG
	bg.StrokeColor = design.ColorTailscaleChipBorder
	bg.StrokeWidth = 1
	bg.SetMinSize(fyne.NewSize(0, 1))

	p := &themedPanel{
		bg:       bg,
		body:     body,
		icon:     iconImg,
		iconBase: icon,
		title:    titleText,
	}
	p.stack = container.NewStack(bg, body)
	p.ExtendBaseWidget(p)
	registerThemedPanel(p)
	registerChromeWidget(p)
	p.applyChrome()
	return p
}

func newDarkWell(content fyne.CanvasObject) fyne.CanvasObject {
	bg := canvas.NewRectangle(design.ColorGray950)
	bg.CornerRadius = design.RadiusMD
	if content == nil {
		return container.NewStack(bg)
	}
	return container.NewStack(bg, content)
}

func newPermToggleRow(label string, check *styledCheck) *permToggleRow {
	return newPermToggleRowWidget(label, check)
}

// Teal 16px header glyphs for agent cards — same #41e0c3 the client's
// Devices cards use on HID / Video / Audio titles.
var (
	panelIconTailscale = fyne.NewStaticResource("panel-tailscale.svg", []byte(
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path fill="#41e0c3" d="M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm-1 17.93c-3.95-.49-7-3.85-7-7.93 0-.62.08-1.21.21-1.79L9 15v1c0 1.1.9 2 2 2v1.93zm6.9-2.54c-.26-.81-1-1.39-1.9-1.39h-1v-3c0-.55-.45-1-1-1H8v-2h2c.55 0 1-.45 1-1V7h2c1.1 0 2-.9 2-2v-.41c2.93 1.19 5 4.06 5 7.41 0 2.08-.8 3.97-2.1 5.39z"/></svg>`))
	panelIconPermissions = fyne.NewStaticResource("panel-permissions.svg", []byte(
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path fill="#41e0c3" d="M12 1L3 5v6c0 5.55 3.84 10.74 9 12 5.16-1.26 9-6.45 9-12V5l-9-4zm0 10.99h7c-.53 4.12-3.28 7.79-7 8.94V12H5V6.3l7-3.11v8.8z"/></svg>`))
	panelIconProtocol = fyne.NewStaticResource("panel-protocol.svg", []byte(
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path fill="#41e0c3" d="M12 2L2 7l10 5 10-5-10-5zM2 17l10 5 10-5v-2.5L12 19 2 14.5V17zm0-5 10 5 10-5V9.5L12 14 2 9.5V12z"/></svg>`))
	panelIconStatus = fyne.NewStaticResource("panel-status.svg", []byte(
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path fill="#41e0c3" d="M3.5 18.49l6-6.01 4 4L22 6.92l-1.41-1.41-7.09 7.97-4-4L2 16.99z"/></svg>`))
	headerLogoutIcon = fyne.NewStaticResource("header-logout.svg", []byte(
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path fill="#c3c6b4" d="M17 7l-1.41 1.41L18.17 11H8v2h10.17l-2.58 2.58L17 17l5-5zM4 5h8V3H4c-1.1 0-2 .9-2 2v14c0 1.1.9 2 2 2h8v-2H4V5z"/></svg>`))
	headerLogoutIconHover = fyne.NewStaticResource("header-logout-hover.svg", []byte(
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path fill="#fda4af" d="M17 7l-1.41 1.41L18.17 11H8v2h10.17l-2.58 2.58L17 17l5-5zM4 5h8V3H4c-1.1 0-2 .9-2 2v14c0 1.1.9 2 2 2h8v-2H4V5z"/></svg>`))
	headerLoginIcon = fyne.NewStaticResource("header-login.svg", []byte(
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path fill="#c3c6b4" d="M11 7L9.59 8.41 12.17 11H2v2h10.17l-2.58 2.59L11 17l5-5zM20 19h-8v2h8c1.1 0 2-.9 2-2V5c0-1.1-.9-2-2-2h-8v2h8v14z"/></svg>`))
	headerLoginIconWaiting = fyne.NewStaticResource("header-login-waiting.svg", []byte(
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path fill="#9a9d8c" d="M11 7L9.59 8.41 12.17 11H2v2h10.17l-2.58 2.59L11 17l5-5zM20 19h-8v2h8c1.1 0 2-.9 2-2V5c0-1.1-.9-2-2-2h-8v2h8v14z"/></svg>`))
	headerChangeIcon = fyne.NewStaticResource("header-change.svg", []byte(
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path fill="#c3c6b4" d="M6.99 11 3 15l3.99 4v-3H14v-2H6.99v-3zM21 9l-3.99-4v3H10v2h7.01v3L21 9z"/></svg>`))
	headerChangeIconOnTeal = fyne.NewStaticResource("header-change-on-teal.svg", []byte(
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path fill="#000000" d="M6.99 11 3 15l3.99 4v-3H14v-2H6.99v-3zM21 9l-3.99-4v3H10v2h7.01v3L21 9z"/></svg>`))
	headerChangeIconOnLight = fyne.NewStaticResource("header-change-on-light.svg", []byte(
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path fill="#f5f5f5" d="M6.99 11 3 15l3.99 4v-3H14v-2H6.99v-3zM21 9l-3.99-4v3H10v2h7.01v3L21 9z"/></svg>`))
	headerCancelIcon = fyne.NewStaticResource("header-cancel.svg", []byte(
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path fill="#d95c5c" d="M19 6.41L17.59 5 12 10.59 6.41 5 5 6.41 10.59 12 5 17.59 6.41 19 12 13.41 17.59 19 19 17.59 13.41 12z"/></svg>`))
)

func newTightVBox(items ...fyne.CanvasObject) fyne.CanvasObject {
	visible := make([]fyne.CanvasObject, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		visible = append(visible, item)
	}
	return container.New(&tightVBoxLayout{gap: 2}, visible...)
}

func spacerSize(width, height float32) fyne.CanvasObject {
	spacer := canvas.NewRectangle(color.Transparent)
	spacer.SetMinSize(fyne.NewSize(width, height))
	return spacer
}

func newKeyValueRow(label string, value *widget.Label) fyne.CanvasObject {
	title := canvas.NewText(label+":", design.ColorTextLight)
	title.TextStyle.Bold = true
	title.TextSize = 14

	if value == nil {
		value = widget.NewLabel("")
	}
	value.Wrapping = fyne.TextWrapWord

	titleSlot := container.NewGridWrap(fyne.NewSize(72, title.MinSize().Height), title)
	return container.NewBorder(nil, nil, titleSlot, nil, value)
}

func newHeaderBar(left fyne.CanvasObject, right fyne.CanvasObject) (fyne.CanvasObject, *canvas.Rectangle) {
	bg := canvas.NewRectangle(design.ColorGray900)
	bg.SetMinSize(fyne.NewSize(0, 38))

	hairline := canvas.NewRectangle(design.ColorChromeOlive)
	hairline.SetMinSize(fyne.NewSize(0, 1))

	leftBox := container.NewHBox()
	if left != nil {
		leftBox.Add(left)
	}
	rightBox := container.NewHBox()
	if right != nil {
		rightBox.Add(right)
	}

	row := container.NewBorder(nil, nil, leftBox, rightBox, nil)
	inner := newExactInset(row, 8, 10, 4, 4)
	return container.New(&overlayEdgeLineLayout{}, container.NewStack(bg, inner), hairline), hairline
}

func newAppFooter(version string, busy fyne.CanvasObject, themeBtn fyne.CanvasObject, onVersion func()) fyne.CanvasObject {
	bg := canvas.NewRectangle(design.ColorGray950)
	hairline := canvas.NewRectangle(design.ColorChromeOlive)
	hairline.SetMinSize(fyne.NewSize(0, 1))

	var rightBits []fyne.CanvasObject
	if themeBtn != nil {
		rightBits = append(rightBits, themeBtn)
	}
	if v := strings.TrimSpace(version); v != "" {
		if !strings.HasPrefix(strings.ToLower(v), "v") {
			v = "v" + v
		}
		if onVersion != nil {
			rightBits = append(rightBits, newFooterTextButton(v, onVersion))
		} else {
			label := canvas.NewText(v, design.ColorMutedOlive)
			label.TextSize = 9
			rightBits = append(rightBits, label)
		}
	}
	var right fyne.CanvasObject
	switch len(rightBits) {
	case 1:
		right = rightBits[0]
	case 0:
	default:
		right = container.New(&tightHBoxLayout{gap: 10}, rightBits...)
	}
	heightLock := canvas.NewRectangle(color.Transparent)
	heightLock.SetMinSize(fyne.NewSize(0, 14))
	row := container.NewMax(heightLock, container.New(&footerPinEndsLayout{}, busy, right))
	inner := newExactInset(row, 18, 18, 3, 4)
	return container.New(&overlayEdgeLineLayout{top: true}, container.NewStack(bg, inner), hairline)
}

type overlayEdgeLineLayout struct {
	top bool
}

func (l *overlayEdgeLineLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) < 2 {
		return
	}
	content, line := objects[0], objects[1]
	lineH := line.MinSize().Height
	if lineH < 1 {
		lineH = 1
	}
	content.Move(fyne.NewPos(0, 0))
	content.Resize(size)
	y := float32(0)
	if !l.top {
		y = size.Height - lineH
	}
	line.Move(fyne.NewPos(0, y))
	line.Resize(fyne.NewSize(size.Width, lineH))
}

func (l *overlayEdgeLineLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	if len(objects) == 0 {
		return fyne.NewSize(0, 0)
	}
	return objects[0].MinSize()
}

type exactInsetLayout struct {
	left, right, top, bottom float32
}

func (l *exactInsetLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) == 0 {
		return
	}
	w := size.Width - l.left - l.right
	h := size.Height - l.top - l.bottom
	if w < 0 {
		w = 0
	}
	if h < 0 {
		h = 0
	}
	objects[0].Move(fyne.NewPos(l.left, l.top))
	objects[0].Resize(fyne.NewSize(w, h))
}

func (l *exactInsetLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	if len(objects) == 0 {
		return fyne.NewSize(0, 0)
	}
	min := objects[0].MinSize()
	return fyne.NewSize(min.Width+l.left+l.right, min.Height+l.top+l.bottom)
}

// contentMinScrollLayout wraps a VScroll so the card still reports the
// content's natural height (cards don't collapse), while the scroll can
// shrink below that when the window is too short. When the card is taller
// than its content, leftover space stays below the last row.
type contentMinScrollLayout struct {
	content fyne.CanvasObject
	fill    *viewportFillLayout
}

func (l *contentMinScrollLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) == 0 {
		return
	}
	if l.fill != nil && l.fill.inner != nil {
		natural := l.fill.inner.MinSize().Height
		if size.Height > natural {
			l.fill.minHeight = size.Height
		} else {
			l.fill.minHeight = 0
		}
	}
	objects[0].Move(fyne.NewPos(0, 0))
	objects[0].Resize(size)
}

func (l *contentMinScrollLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	if l.fill != nil && l.fill.inner != nil {
		return l.fill.inner.MinSize()
	}
	if l.content != nil {
		return l.content.MinSize()
	}
	if len(objects) == 0 {
		return fyne.NewSize(0, 0)
	}
	return objects[0].MinSize()
}

// viewportFillLayout reports a taller MinSize when the card body is given
// extra height, so VScroll lays its content out at the viewport size
// instead of leaving empty space below the last row.
type viewportFillLayout struct {
	inner     fyne.CanvasObject
	minHeight float32
}

func (l *viewportFillLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) == 0 {
		return
	}
	objects[0].Move(fyne.NewPos(0, 0))
	objects[0].Resize(size)
}

func (l *viewportFillLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	var s fyne.Size
	if l.inner != nil {
		s = l.inner.MinSize()
	} else if len(objects) > 0 {
		s = objects[0].MinSize()
	}
	if l.minHeight > s.Height {
		s.Height = l.minHeight
	}
	return s
}

func newExactInset(content fyne.CanvasObject, left, right, top, bottom float32) *fyne.Container {
	return container.New(&exactInsetLayout{left: left, right: right, top: top, bottom: bottom}, content)
}

// headerTitleLayout is tightHBox with a downward optical nudge — Fyne
// canvas.Text's MinSize sits the glyph high in the box, so a true vertical
// center reads as "pulled up" in the card header.
type headerTitleLayout struct {
	gap    float32
	yNudge float32
}

func (l *headerTitleLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	x := float32(0)
	for _, o := range objects {
		if o == nil || !o.Visible() {
			continue
		}
		min := o.MinSize()
		h := min.Height
		if h > size.Height {
			h = size.Height
		}
		y := (size.Height-h)/2 + l.yNudge
		if y < 0 {
			y = 0
		}
		if y+h > size.Height {
			y = size.Height - h
			if y < 0 {
				y = 0
			}
		}
		o.Resize(fyne.NewSize(min.Width, h))
		o.Move(fyne.NewPos(x, y))
		x += min.Width + l.gap
	}
}

func (l *headerTitleLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	var w, h float32
	n := 0
	for _, o := range objects {
		if o == nil || !o.Visible() {
			continue
		}
		min := o.MinSize()
		w += min.Width
		if min.Height > h {
			h = min.Height
		}
		n++
	}
	if n > 1 {
		w += l.gap * float32(n-1)
	}
	return fyne.NewSize(w, h)
}

type tightHBoxLayout struct {
	gap float32
}

func (l *tightHBoxLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	x := float32(0)
	for _, o := range objects {
		if o == nil || !o.Visible() {
			continue
		}
		min := o.MinSize()
		h := min.Height
		if h > size.Height {
			h = size.Height
		}
		y := (size.Height - h) / 2
		if y < 0 {
			y = 0
		}
		o.Resize(fyne.NewSize(min.Width, h))
		o.Move(fyne.NewPos(x, y))
		x += min.Width + l.gap
	}
}

func (l *tightHBoxLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	var w, h float32
	n := 0
	for _, o := range objects {
		if o == nil || !o.Visible() {
			continue
		}
		min := o.MinSize()
		w += min.Width
		if min.Height > h {
			h = min.Height
		}
		n++
	}
	if n > 1 {
		w += l.gap * float32(n-1)
	}
	return fyne.NewSize(w, h)
}

type tightVBoxLayout struct {
	gap float32
}

func (l *tightVBoxLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	y := float32(0)
	for _, o := range objects {
		if o == nil || !o.Visible() {
			continue
		}
		min := o.MinSize()
		w := size.Width
		if w < min.Width {
			w = min.Width
		}
		o.Resize(fyne.NewSize(w, min.Height))
		o.Move(fyne.NewPos(0, y))
		y += min.Height + l.gap
	}
}

func (l *tightVBoxLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	var w, h float32
	n := 0
	for _, o := range objects {
		if o == nil || !o.Visible() {
			continue
		}
		min := o.MinSize()
		if min.Width > w {
			w = min.Width
		}
		h += min.Height
		n++
	}
	if n > 1 {
		h += l.gap * float32(n-1)
	}
	return fyne.NewSize(w, h)
}

// flushEndsLayout pins the first child to the left and the last to the
// right at their own MinSize — no theme.Padding(), so DPI/scale changes
// don't invent extra inset the way container.NewBorder does.
type flushEndsLayout struct{}

func (l *flushEndsLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	var vis []fyne.CanvasObject
	for _, o := range objects {
		if o == nil || !o.Visible() {
			continue
		}
		vis = append(vis, o)
	}
	if len(vis) == 0 {
		return
	}
	left := vis[0]
	ls := left.MinSize()
	lh := ls.Height
	if lh > size.Height {
		lh = size.Height
	}
	left.Resize(fyne.NewSize(ls.Width, lh))
	left.Move(fyne.NewPos(0, (size.Height-lh)/2))
	if len(vis) == 1 {
		return
	}
	right := vis[len(vis)-1]
	rs := right.MinSize()
	rh := rs.Height
	if rh > size.Height {
		rh = size.Height
	}
	right.Resize(fyne.NewSize(rs.Width, rh))
	x := size.Width - rs.Width
	if x < ls.Width {
		x = ls.Width
	}
	right.Move(fyne.NewPos(x, (size.Height-rh)/2))
}

func (l *flushEndsLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	var w, h float32
	n := 0
	for _, o := range objects {
		if o == nil || !o.Visible() {
			continue
		}
		min := o.MinSize()
		w += min.Width
		if min.Height > h {
			h = min.Height
		}
		n++
	}
	return fyne.NewSize(w, h)
}

// centerHLayout sizes each child to its MinSize and centers it in the
// allocated slot — used for the Protocol card's Buy Pro chip.
type centerHLayout struct{}

func (l *centerHLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	for _, o := range objects {
		if o == nil || !o.Visible() {
			continue
		}
		min := o.MinSize()
		h := min.Height
		if h > size.Height {
			h = size.Height
		}
		w := min.Width
		if w > size.Width {
			w = size.Width
		}
		o.Resize(fyne.NewSize(w, h))
		o.Move(fyne.NewPos((size.Width-w)/2, (size.Height-h)/2))
	}
}

func (l *centerHLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	var w, h float32
	for _, o := range objects {
		if o == nil || !o.Visible() {
			continue
		}
		min := o.MinSize()
		if min.Width > w {
			w = min.Width
		}
		if min.Height > h {
			h = min.Height
		}
	}
	return fyne.NewSize(w, h)
}

func newBadge(text string, size fyne.Size) fyne.CanvasObject {
	bg := canvas.NewRectangle(design.ColorAccent)
	bg.CornerRadius = design.RadiusMD

	label := canvas.NewText(text, design.ColorBackground)
	label.TextStyle.Bold = true
	label.TextSize = 14

	return container.NewGridWrap(size, container.NewStack(bg, container.NewCenter(label)))
}

func wrapDialogButton(btn *widget.Button) fyne.CanvasObject {
	if btn == nil {
		return layout.NewSpacer()
	}
	slot := canvas.NewRectangle(color.Transparent)
	slot.SetMinSize(fyne.NewSize(0, 48))
	return container.NewStack(slot, btn)
}

func minFloat32(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}

type compactTheme struct {
	fyne.Theme
}

func (t *compactTheme) Size(name fyne.ThemeSizeName) float32 {
	if name == theme.SizeNamePadding {
		return 4
	}
	return t.Theme.Size(name)
}

func newLabelValue(labelText string, valueText *canvas.Text) fyne.CanvasObject {
	title := canvas.NewText(strings.ToUpper(labelText)+":", design.ColorMutedOlive)
	title.TextSize = 10
	title.TextStyle.Bold = true

	// Container for the title with a fixed width
	titleBox := container.NewGridWrap(fyne.NewSize(55, 16), container.NewCenter(title))

	return container.NewHBox(titleBox, valueText)
}

type labelTheme struct {
	fyne.Theme
	textColor color.Color
	textSize  float32
}

func (t *labelTheme) Color(name fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	if name == theme.ColorNameForeground {
		return t.textColor
	}
	return t.Theme.Color(name, v)
}

func (t *labelTheme) Size(name fyne.ThemeSizeName) float32 {
	if name == theme.SizeNameText {
		return t.textSize
	}
	if name == theme.SizeNamePadding {
		return 0
	}
	return t.Theme.Size(name)
}

type compactPanelTheme struct {
	fyne.Theme
}

func (t *compactPanelTheme) Size(name fyne.ThemeSizeName) float32 {
	switch name {
	case theme.SizeNamePadding:
		return 2
	case theme.SizeNameInnerPadding:
		return 4
	case theme.SizeNameText:
		return 12
	case theme.SizeNameCaptionText:
		return 10
	}
	return t.Theme.Size(name)
}

type headerButtonTheme struct {
	fyne.Theme
	padding float32
}

func (t *headerButtonTheme) Size(name fyne.ThemeSizeName) float32 {
	if name == theme.SizeNamePadding {
		return t.padding
	}
	return t.Theme.Size(name)
}

// newDangerGlyphButton returns a compact cancel-icon (✕) chip with a red
// glyph — use instead of DangerImportance when only the icon should be red.
func newDangerGlyphButton(tapped func()) fyne.CanvasObject {
	btn := newIconActionButton("", headerCancelIcon, tapped)
	btn.Tiny = true
	return btn
}

type iconActionButton struct {
	widget.DisableableWidget
	Text     string
	Icon     fyne.Resource
	OnTapped func()
	Compact  bool
	Tiny     bool
	Accent   bool
	CTA      bool
	Danger   bool
	hovered  bool
}

func newIconActionButton(label string, icon fyne.Resource, tapped func()) *iconActionButton {
	b := &iconActionButton{Text: label, Icon: icon, OnTapped: tapped}
	b.ExtendBaseWidget(b)
	registerChromeWidget(b)
	return b
}

func (b *iconActionButton) SetText(text string) {
	b.Text = text
	b.Refresh()
}

func (b *iconActionButton) CreateRenderer() fyne.WidgetRenderer {
	bg := canvas.NewRectangle(color.Transparent)
	bg.CornerRadius = design.RadiusMD
	bg.StrokeWidth = 1
	bg.StrokeColor = design.ColorChromeOlive

	var icon *canvas.Image
	if b.Icon != nil {
		icon = canvas.NewImageFromResource(b.Icon)
		side, _, _ := iconActionMetrics(b)
		icon.FillMode = canvas.ImageFillStretch
		icon.SetMinSize(fyne.NewSize(side, side))
	} else {
		icon = canvas.NewImageFromResource(nil)
		icon.Hide()
	}

	textSize := float32(12)
	if b.Tiny {
		textSize = 10
		bg.CornerRadius = 4
	} else if b.Compact {
		textSize = 11
		bg.CornerRadius = 6
	}
	text := canvas.NewText(b.Text, design.ColorMutedOlive)
	text.TextStyle.Bold = true
	text.TextSize = textSize

	objects := []fyne.CanvasObject{bg, icon, text}
	return &iconActionButtonRenderer{
		bg:      bg,
		icon:    icon,
		text:    text,
		button:  b,
		objects: objects,
	}
}

func (b *iconActionButton) MouseIn(ev *desktop.MouseEvent) {
	if ev != nil {
		noteChromeHoverIn(ev.AbsolutePosition)
	}
	b.hovered = true
	b.Refresh()
}

func (b *iconActionButton) MouseOut() {
	noteChromeHoverOut()
	b.hovered = false
	b.Refresh()
}

func (b *iconActionButton) MouseMoved(ev *desktop.MouseEvent) {
	if ev != nil {
		noteChromeHoverIn(ev.AbsolutePosition)
	}
}

func (b *iconActionButton) Tapped(*fyne.PointEvent) {
	if b.Disabled() {
		return
	}
	if b.OnTapped != nil {
		b.OnTapped()
	}
}

type iconActionButtonRenderer struct {
	bg      *canvas.Rectangle
	icon    *canvas.Image
	text    *canvas.Text
	button  *iconActionButton
	objects []fyne.CanvasObject
}

func (r *iconActionButtonRenderer) Destroy() {}

// placeSquareIcon pins a canvas.Image to an exact square. Fyne SVG
// resources (theme icons are 24×24, svgrepo assets often 800×800) keep
// that native size unless SetMinSize is set; ImageFillContain then
// letterboxes the huge bitmap inside the chip, so the glyph looks oversized
// and off-center. Stretch + an explicit min/size keeps the glyph in the
// square we actually laid out.
func placeSquareIcon(img *canvas.Image, pos fyne.Position, side float32) {
	if img == nil {
		return
	}
	img.FillMode = canvas.ImageFillStretch
	sz := fyne.NewSize(side, side)
	img.SetMinSize(sz)
	img.Resize(sz)
	img.Move(pos)
}

func (r *iconActionButtonRenderer) Layout(size fyne.Size) {
	r.bg.Resize(size)

	hasIcon := r.button.Icon != nil
	hasText := strings.TrimSpace(r.button.Text) != ""
	iconSize, gap, paddingX := iconActionMetrics(r.button)
	textSize := r.text.MinSize()
	contentWidth := float32(0)
	if hasIcon {
		contentWidth += iconSize
	}
	if hasText {
		if hasIcon {
			contentWidth += gap
		}
		contentWidth += textSize.Width
	}

	startX := (size.Width - contentWidth) / 2
	if startX < paddingX && hasText {
		startX = paddingX
	}
	if startX < 0 {
		startX = 0
	}

	x := startX
	if hasIcon {
		r.icon.Show()
		placeSquareIcon(r.icon, fyne.NewPos(x, (size.Height-iconSize)/2), iconSize)
		x += iconSize
		if hasText {
			x += gap
		}
	} else {
		r.icon.Hide()
	}
	if hasText {
		r.text.Show()
		r.text.Move(fyne.NewPos(x, (size.Height-textSize.Height)/2-0.5))
		r.text.Resize(textSize)
	} else {
		r.text.Hide()
	}
}

func iconActionMetrics(b *iconActionButton) (iconSize, gap, paddingX float32) {
	iconSize, gap, paddingX = 20, 8, 14
	if b.Tiny {
		return 10, 3, 6
	}
	if b.Compact {
		return 12, 4, 10
	}
	return
}

const tinyActionSize float32 = 22

func (r *iconActionButtonRenderer) MinSize() fyne.Size {
	hasIcon := r.button.Icon != nil
	hasText := strings.TrimSpace(r.button.Text) != ""
	iconSize, gap, paddingX := iconActionMetrics(r.button)
	paddingY := float32(10)
	if r.button.Tiny {
		paddingY = 0
	} else if r.button.Compact {
		paddingY = 6
	}
	textSize := r.text.MinSize()
	width := paddingX * 2
	if hasIcon {
		width += iconSize
	}
	if hasText {
		if hasIcon {
			width += gap
		}
		width += textSize.Width
	}
	height := paddingY * 2
	if hasText {
		height = textSize.Height + paddingY*2
	}
	if hasIcon && iconSize+paddingY*2 > height {
		height = iconSize + paddingY*2
	}
	if r.button.Tiny {
		height = tinyActionSize
		if !hasText {
			width = tinyActionSize
		} else if width < tinyActionSize {
			width = tinyActionSize
		}
	}
	return fyne.NewSize(width, height)
}

func (r *iconActionButtonRenderer) Objects() []fyne.CanvasObject {
	return r.objects
}

func (r *iconActionButtonRenderer) Refresh() {
	r.text.Text = r.button.Text
	switch {
	case r.button.Tiny:
		r.text.TextSize = 10
	case r.button.Compact:
		r.text.TextSize = 11
	}
	if r.button.Disabled() {
		r.bg.FillColor = design.ColorSurface
		r.bg.StrokeColor = design.ColorChromeOlive
		r.text.Color = design.ColorBorder
	} else if r.button.Accent {
		ch := currentChrome()
		r.bg.StrokeColor = color.Transparent
		r.text.Color = ch.OnAccent
		if r.button.hovered {
			r.bg.FillColor = ch.AccentHover
		} else {
			r.bg.FillColor = ch.Accent
		}
	} else if r.button.CTA {
		r.text.Color = design.ColorCTALabel
		if r.button.hovered {
			r.bg.FillColor = design.ColorCTAHover
			r.bg.StrokeColor = design.ColorCTAHover
		} else {
			r.bg.FillColor = design.ColorCTA
			r.bg.StrokeColor = design.ColorCTA
		}
	} else if r.button.Danger && r.button.hovered {
		r.bg.FillColor = design.ColorLogoutHoverFill
		r.bg.StrokeColor = design.ColorLogoutHoverStroke
		r.text.Color = design.ColorLogoutHoverLabel
	} else if r.button.hovered {
		ch := currentChrome()
		r.bg.FillColor = design.ColorSurfaceLight
		r.bg.StrokeColor = ch.Accent
		r.text.Color = design.ColorTextLight
	} else {
		r.bg.FillColor = color.Transparent
		r.bg.StrokeColor = design.ColorChromeOlive
		r.text.Color = design.ColorMutedOlive
	}
	if r.icon != nil && r.button.Icon != nil && r.button.Compact {
		tint := design.ColorNameMutedOlive
		switch {
		case r.button.Disabled():
			tint = theme.ColorNameDisabled
		case r.button.Danger && r.button.hovered:
			tint = design.ColorNameLogoutHoverLabel
		case r.button.hovered:
			tint = theme.ColorNameForeground
		}
		r.icon.Resource = theme.NewColoredResource(r.button.Icon, tint)
	}
	r.bg.Refresh()
	r.text.Refresh()
	r.icon.Refresh()
}

type closeButton struct {
	widget.BaseWidget
	Text     string
	OnTapped func()
	hovered  bool
}

func newDangerButton(label string, tapped func()) fyne.CanvasObject {
	b := &closeButton{Text: label, OnTapped: tapped}
	b.ExtendBaseWidget(b)
	return b
}

func (b *closeButton) CreateRenderer() fyne.WidgetRenderer {
	bg := canvas.NewRectangle(color.Transparent)
	bg.CornerRadius = design.RadiusMD
	bg.StrokeWidth = 1
	bg.StrokeColor = design.ColorError

	text := canvas.NewText(b.Text, design.ColorError)
	text.Alignment = fyne.TextAlignCenter
	text.TextStyle.Bold = true
	text.TextSize = 13

	content := container.NewStack(bg, container.NewPadded(text))
	return &closeButtonRenderer{
		bg:      bg,
		text:    text,
		button:  b,
		objects: []fyne.CanvasObject{content},
	}
}

type closeButtonRenderer struct {
	bg      *canvas.Rectangle
	text    *canvas.Text
	button  *closeButton
	objects []fyne.CanvasObject
}

func (r *closeButtonRenderer) Destroy() {}
func (r *closeButtonRenderer) Layout(size fyne.Size) {
	r.objects[0].Resize(size)
}
func (r *closeButtonRenderer) MinSize() fyne.Size {
	return fyne.NewSize(80, 32)
}
func (r *closeButtonRenderer) Objects() []fyne.CanvasObject {
	return r.objects
}
func (r *closeButtonRenderer) Refresh() {
	if r.button.hovered {
		r.bg.FillColor = design.ColorError
		r.bg.StrokeColor = color.Transparent
		r.text.Color = color.Black
	} else {
		r.bg.FillColor = color.Transparent
		r.bg.StrokeColor = design.ColorError
		r.text.Color = design.ColorError
	}
	r.bg.Refresh()
	r.text.Refresh()
}

func (b *closeButton) MouseIn(*desktop.MouseEvent) {
	b.hovered = true
	b.Refresh()
}
func (b *closeButton) MouseOut() {
	b.hovered = false
	b.Refresh()
}
func (b *closeButton) MouseMoved(*desktop.MouseEvent) {}
func (b *closeButton) Tapped(*fyne.PointEvent) {
	if b.OnTapped != nil {
		b.OnTapped()
	}
}

// supportButton is the Protocol header's Buy Pro chip — same chrome as
// Change (dark fill, 22px, muted label) with a dim purple outline at rest,
// filling ColorProSoft when hovered or accented (pending purchase).
type supportButton struct {
	widget.BaseWidget
	Text     string
	OnTapped func()
	hovered  bool
	accent   bool
}

func newSupportButton(label string, tapped func()) *supportButton {
	b := &supportButton{Text: label, OnTapped: tapped}
	b.ExtendBaseWidget(b)
	return b
}

func (b *supportButton) SetText(text string) {
	b.Text = text
	b.Refresh()
}

func (b *supportButton) SetAccent(on bool) {
	if b.accent == on {
		return
	}
	b.accent = on
	b.Refresh()
}

func (b *supportButton) CreateRenderer() fyne.WidgetRenderer {
	bg := canvas.NewRectangle(design.ColorGray950)
	bg.CornerRadius = 6
	bg.StrokeWidth = 1
	bg.StrokeColor = design.ColorProSoft

	icon := canvas.NewImageFromResource(assets.StarProIcon)
	icon.FillMode = canvas.ImageFillContain

	text := canvas.NewText(b.Text, design.ColorTailscaleChipLabel)
	text.TextStyle.Bold = true
	text.TextSize = 10

	return &supportButtonRenderer{
		bg:      bg,
		icon:    icon,
		text:    text,
		button:  b,
		objects: []fyne.CanvasObject{bg, icon, text},
	}
}

func (b *supportButton) MouseIn(ev *desktop.MouseEvent) {
	if ev != nil {
		noteChromeHoverIn(ev.AbsolutePosition)
	}
	b.hovered = true
	b.Refresh()
}
func (b *supportButton) MouseOut() {
	noteChromeHoverOut()
	b.hovered = false
	b.Refresh()
}
func (b *supportButton) MouseMoved(ev *desktop.MouseEvent) {
	if ev != nil {
		noteChromeHoverIn(ev.AbsolutePosition)
	}
}
func (b *supportButton) Cursor() desktop.Cursor { return desktop.PointerCursor }
func (b *supportButton) Tapped(*fyne.PointEvent) {
	if b.OnTapped != nil {
		b.OnTapped()
	}
}

type supportButtonRenderer struct {
	bg      *canvas.Rectangle
	icon    *canvas.Image
	text    *canvas.Text
	button  *supportButton
	objects []fyne.CanvasObject
}

func (r *supportButtonRenderer) Destroy() {}

func (r *supportButtonRenderer) Layout(size fyne.Size) {
	r.bg.Resize(size)

	iconSize := float32(12)
	gap := float32(4)
	textSize := r.text.MinSize()
	contentWidth := iconSize + gap + textSize.Width
	startX := (size.Width - contentWidth) / 2

	placeSquareIcon(r.icon, fyne.NewPos(startX, (size.Height-iconSize)/2), iconSize)
	r.text.Resize(textSize)
	r.text.Move(fyne.NewPos(startX+iconSize+gap, (size.Height-textSize.Height)/2-0.5))
}

func (r *supportButtonRenderer) MinSize() fyne.Size {
	paddingX := float32(8)
	iconSize := float32(12)
	gap := float32(4)
	textSize := r.text.MinSize()
	return fyne.NewSize(iconSize+gap+textSize.Width+paddingX*2, 22)
}

func (r *supportButtonRenderer) Objects() []fyne.CanvasObject {
	return r.objects
}

func (r *supportButtonRenderer) Refresh() {
	r.text.Text = r.button.Text
	if r.button.hovered || r.button.accent {
		r.bg.FillColor = design.ColorProSoft
		r.bg.StrokeColor = design.ColorProSoft
		r.text.Color = design.ColorGray950
		r.icon.Resource = assets.StarOnProIcon
	} else {
		r.bg.FillColor = design.ColorGray950
		r.bg.StrokeColor = design.ColorProSoft
		r.text.Color = design.ColorTailscaleChipLabel
		r.icon.Resource = assets.StarProIcon
	}
	r.bg.Refresh()
	r.text.Refresh()
	r.icon.Refresh()
	r.Layout(r.button.Size())
}

type subscriptionBadge struct {
	widget.BaseWidget
	label  string
	fg     color.Color
	stroke color.Color
}

func newSubscriptionBadge() *subscriptionBadge {
	b := &subscriptionBadge{
		label:  "Opensource",
		fg:     design.ColorMutedOlive,
		stroke: design.ColorChromeOlive,
	}
	b.ExtendBaseWidget(b)
	return b
}

func (b *subscriptionBadge) SetStatus(st entitlement.Status) {
	label, fg, stroke := subscriptionBadgeStyle(st)
	if b.label == label && b.fg == fg && b.stroke == stroke {
		return
	}
	b.label = label
	b.fg = fg
	b.stroke = stroke
	b.Refresh()
}

func subscriptionBadgeStyle(st entitlement.Status) (string, color.Color, color.Color) {
	if st.ActiveBackend != "rustshine" {
		return "Opensource", design.ColorMutedOlive, design.ColorChromeOlive
	}
	switch strings.ToLower(st.Tier) {
	case "pro":
		return "Pro", design.ColorProSoft, design.ColorProSoft
	case "enterprise":
		return "Enterprise", design.ColorProSoft, design.ColorProSoft
	default:
		return "Free", design.ColorTeal, design.ColorTeal
	}
}

func (b *subscriptionBadge) CreateRenderer() fyne.WidgetRenderer {
	bg := canvas.NewRectangle(color.Transparent)
	bg.StrokeWidth = 1
	bg.StrokeColor = b.stroke
	text := canvas.NewText(b.label, b.fg)
	text.TextSize = 8
	text.TextStyle.Bold = true
	text.Alignment = fyne.TextAlignCenter
	return &subscriptionBadgeRenderer{badge: b, bg: bg, text: text, objects: []fyne.CanvasObject{bg, text}}
}

type subscriptionBadgeRenderer struct {
	badge   *subscriptionBadge
	bg      *canvas.Rectangle
	text    *canvas.Text
	objects []fyne.CanvasObject
}

func (r *subscriptionBadgeRenderer) Destroy() {}

func (r *subscriptionBadgeRenderer) Layout(size fyne.Size) {
	r.bg.Resize(size)
	r.bg.CornerRadius = size.Height / 2
	ts := r.text.MinSize()
	r.text.Resize(ts)
	// canvas.Text's MinSize sits the glyph low in the box; nudge up so it
	// reads optically centered in the pill.
	y := (size.Height-ts.Height)/2 - 0.5
	if y < 0 {
		y = 0
	}
	r.text.Move(fyne.NewPos((size.Width-ts.Width)/2, y))
}

func (r *subscriptionBadgeRenderer) MinSize() fyne.Size {
	ts := r.text.MinSize()
	h := fyne.Max(16, ts.Height+6)
	return fyne.NewSize(ts.Width+14, h)
}

func (r *subscriptionBadgeRenderer) Objects() []fyne.CanvasObject {
	return r.objects
}

func (r *subscriptionBadgeRenderer) Refresh() {
	r.text.Text = r.badge.label
	r.text.Color = r.badge.fg
	r.bg.StrokeColor = r.badge.stroke
	r.bg.CornerRadius = r.badge.Size().Height / 2
	r.bg.Refresh()
	r.text.Refresh()
	r.Layout(r.badge.Size())
}

type cardHeaderButton struct {
	widget.DisableableWidget
	Text     string
	Icon     fyne.Resource
	OnTapped func()
	hovered  bool
	logout   bool
	accent   bool
}

func newCardHeaderButton(label string, icon fyne.Resource, tapped func()) *cardHeaderButton {
	b := &cardHeaderButton{Text: label, Icon: icon, OnTapped: tapped}
	b.ExtendBaseWidget(b)
	registerChromeWidget(b)
	return b
}

func (b *cardHeaderButton) SetContent(text string, icon fyne.Resource) {
	b.Text = text
	b.Icon = icon
	b.Refresh()
}

func (b *cardHeaderButton) SetAccent(on bool) {
	if b.accent == on {
		return
	}
	b.accent = on
	b.Refresh()
}

func (b *cardHeaderButton) CreateRenderer() fyne.WidgetRenderer {
	bg := canvas.NewRectangle(design.ColorGray950)
	bg.CornerRadius = 6
	bg.StrokeWidth = 1
	bg.StrokeColor = design.ColorTailscaleChipBorder
	icon := canvas.NewImageFromResource(b.Icon)
	icon.FillMode = canvas.ImageFillContain
	text := canvas.NewText(b.Text, design.ColorTailscaleChipLabel)
	text.TextStyle.Bold = true
	text.TextSize = 10
	return &cardHeaderButtonRenderer{btn: b, bg: bg, icon: icon, text: text, objects: []fyne.CanvasObject{bg, icon, text}}
}

func (b *cardHeaderButton) MouseIn(ev *desktop.MouseEvent) {
	if ev != nil {
		noteChromeHoverIn(ev.AbsolutePosition)
	}
	if b.Disabled() {
		return
	}
	b.hovered = true
	b.Refresh()
}
func (b *cardHeaderButton) MouseOut() {
	noteChromeHoverOut()
	b.hovered = false
	b.Refresh()
}
func (b *cardHeaderButton) MouseMoved(ev *desktop.MouseEvent) {
	if ev != nil {
		noteChromeHoverIn(ev.AbsolutePosition)
	}
}
func (b *cardHeaderButton) Cursor() desktop.Cursor { return desktop.PointerCursor }

func (b *cardHeaderButton) Tapped(*fyne.PointEvent) {
	if b.Disabled() || b.OnTapped == nil {
		return
	}
	b.OnTapped()
}

type cardHeaderButtonRenderer struct {
	btn     *cardHeaderButton
	bg      *canvas.Rectangle
	icon    *canvas.Image
	text    *canvas.Text
	objects []fyne.CanvasObject
}

func (r *cardHeaderButtonRenderer) Destroy() {}

func (r *cardHeaderButtonRenderer) Layout(size fyne.Size) {
	r.bg.Resize(size)
	iconSize := float32(12)
	gap := float32(4)
	pad := float32(8)
	ts := r.text.MinSize()
	contentW := iconSize + gap + ts.Width
	start := (size.Width - contentW) / 2
	if start < pad {
		start = pad
	}
	placeSquareIcon(r.icon, fyne.NewPos(start, (size.Height-iconSize)/2), iconSize)
	r.text.Resize(ts)
	r.text.Move(fyne.NewPos(start+iconSize+gap, (size.Height-ts.Height)/2))
}

func (r *cardHeaderButtonRenderer) MinSize() fyne.Size {
	ts := r.text.MinSize()
	return fyne.NewSize(12+4+ts.Width+16, 22)
}

func (r *cardHeaderButtonRenderer) Objects() []fyne.CanvasObject { return r.objects }

func (r *cardHeaderButtonRenderer) Refresh() {
	r.text.Text = r.btn.Text
	icon := r.btn.Icon
	label := design.ColorTailscaleChipLabel
	fill := design.ColorGray950
	stroke := design.ColorTailscaleChipBorder
	if r.btn.Disabled() {
		fill = color.NRGBA{}
		stroke = design.ColorTailscaleChipBorder
		label = design.ColorEmptyHint
		if r.btn.Icon == headerLoginIcon {
			icon = headerLoginIconWaiting
		}
	} else if r.btn.hovered && r.btn.logout {
		fill = design.ColorLogoutHoverFill
		stroke = design.ColorLogoutHoverStroke
		label = design.ColorLogoutHoverLabel
		icon = headerLogoutIconHover
	} else if r.btn.accent {
		ch := currentChrome()
		fill = ch.Accent
		stroke = ch.Accent
		label = ch.OnAccent
		if r.btn.hovered {
			fill = ch.AccentHover
			stroke = ch.AccentHover
		}
		if r.btn.Icon == headerChangeIcon {
			if ch.OnAccent == design.ColorTextLight || ch.OnAccent == design.ColorWhite {
				icon = headerChangeIconOnLight
			} else {
				icon = headerChangeIconOnTeal
			}
		}
	} else if r.btn.hovered {
		ch := currentChrome()
		fill = design.ColorGray900
		stroke = ch.Accent
	}
	r.bg.FillColor = fill
	r.bg.StrokeColor = stroke
	r.text.Color = label
	r.icon.Resource = icon
	r.bg.Refresh()
	r.text.Refresh()
	r.icon.Refresh()
	r.Layout(r.btn.Size())
}

type headerIconButton struct {
	widget.BaseWidget
	icon     fyne.Resource
	onTapped func()
	hovered  bool
}

func newHeaderIconButton(icon fyne.Resource, tapped func()) *headerIconButton {
	b := &headerIconButton{icon: icon, onTapped: tapped}
	b.ExtendBaseWidget(b)
	return b
}

func (b *headerIconButton) CreateRenderer() fyne.WidgetRenderer {
	bg := canvas.NewRectangle(color.Transparent)
	bg.CornerRadius = 4
	img := canvas.NewImageFromResource(b.icon)
	img.FillMode = canvas.ImageFillContain
	return &headerIconButtonRenderer{btn: b, bg: bg, icon: img, objects: []fyne.CanvasObject{bg, img}}
}

func (b *headerIconButton) MouseIn(ev *desktop.MouseEvent) {
	if ev != nil {
		noteChromeHoverIn(ev.AbsolutePosition)
	}
	b.hovered = true
	b.Refresh()
}
func (b *headerIconButton) MouseOut() {
	noteChromeHoverOut()
	b.hovered = false
	b.Refresh()
}
func (b *headerIconButton) MouseMoved(*desktop.MouseEvent) {}
func (b *headerIconButton) Tapped(*fyne.PointEvent) {
	if b.onTapped != nil {
		b.onTapped()
	}
}

type headerIconButtonRenderer struct {
	btn     *headerIconButton
	bg      *canvas.Rectangle
	icon    *canvas.Image
	objects []fyne.CanvasObject
}

func (r *headerIconButtonRenderer) Destroy() {}

func (r *headerIconButtonRenderer) Layout(size fyne.Size) {
	r.bg.Resize(size)
	iconSize := float32(14)
	placeSquareIcon(r.icon, fyne.NewPos((size.Width-iconSize)/2, (size.Height-iconSize)/2), iconSize)
}

func (r *headerIconButtonRenderer) MinSize() fyne.Size {
	return fyne.NewSize(20, 20)
}

func (r *headerIconButtonRenderer) Objects() []fyne.CanvasObject {
	return r.objects
}

func (r *headerIconButtonRenderer) Refresh() {
	if r.btn.hovered {
		r.bg.FillColor = design.ColorSurfaceLight
	} else {
		r.bg.FillColor = color.Transparent
	}
	r.bg.Refresh()
	r.icon.Refresh()
}
