package gui

import (
	"runtime"
	"time"

	"usbridge-client/internal/gui/assets"
	"usbridge-client/internal/gui/i18n"
	"usbridge-client/internal/gui/view"
	"usbridge-client/internal/netutil"

	"fyne.io/fyne/v2"
	"github.com/sirupsen/logrus"
)

// The Windows/Linux counterpart of the macOS AWDL toggle (awdl_status.go),
// as one Wi-Fi icon in the Control footer, shown while a Wi-Fi interface
// is connected:
//   - Windows: while a stream runs the client's Wi-Fi is held in media
//     streaming mode with background scans off (netutil/wlan_windows.go).
//     On by default: it needs no admin rights, switches nothing else off,
//     and Windows takes the settings back as soon as the stream ends.
//   - Windows and Linux: a one-time admin tune turning the Wi-Fi driver's
//     own background scans (and on Linux power saving) off for good
//     (netutil/wifitune_*.go). While it is still needed the icon turns
//     amber and the menu says what it does.
const wlanStreamingPrefKey = "wlan_streaming_mode"

// wlanPollInterval: how often the watchdog re-checks the Wi-Fi (the icon
// only shows with one connected), the tune's state, and picks up an
// interface that reconnected mid-stream.
const wlanPollInterval = 2 * time.Second

func wlanSupported() bool { return runtime.GOOS == "windows" || runtime.GOOS == "linux" }

// wlanSessionModeSupported: per-stream streaming mode is a Windows WLAN API
// feature; Linux has only the one-time tune.
func wlanSessionModeSupported() bool { return runtime.GOOS == "windows" }

func (mw *MainWindow) wlanStreamingEnabled() bool {
	if mw.app == nil {
		return true
	}
	return mw.app.Preferences().BoolWithFallback(wlanStreamingPrefKey, true)
}

func (mw *MainWindow) setWLANStreamingEnabled(enabled bool) {
	if mw.app != nil {
		mw.app.Preferences().SetBool(wlanStreamingPrefKey, enabled)
	}
	mw.syncWLANStreamingState(mw.isStreaming)
}

// syncWLANStreamingState holds the Wi-Fi in streaming mode while streaming
// with the setting on and gives it back otherwise. Called from
// updateStatus (the real start/stop transition) and the watchdog tick.
func (mw *MainWindow) syncWLANStreamingState(isStreaming bool) {
	if !wlanSessionModeSupported() {
		return
	}
	want := isStreaming && mw.wlanStreamingEnabled() && mw.wlanPresent.Load()
	if !want && !mw.wlanStreamingOn.Load() {
		return
	}
	if err := netutil.SetWLANStreamingMode(want); err != nil {
		logrus.Warnf("[wlan] streaming mode (on=%v): %v", want, err)
		return
	}
	if mw.wlanStreamingOn.Swap(want) != want {
		logrus.Infof("[wlan] Wi-Fi streaming mode (media streaming, no background scans): %v", want)
	}
}

// startWLANWatchdog runs for the lifetime of the window; a no-op off
// Windows/Linux.
func (mw *MainWindow) startWLANWatchdog() {
	if !wlanSupported() {
		return
	}
	go func() {
		ticker := time.NewTicker(wlanPollInterval)
		defer ticker.Stop()
		for {
			mw.pollWLAN()
			<-ticker.C
			if mw.isClosing.Load() {
				_ = netutil.SetWLANStreamingMode(false)
				return
			}
		}
	}()
}

func (mw *MainWindow) pollWLAN() {
	present := netutil.WLANConnected()
	applicable, done := false, false
	if present {
		applicable, done = netutil.WiFiTuneStatus()
	}
	changed := mw.wlanPresent.Swap(present) != present
	changed = mw.wifiTuneNeeded.Swap(applicable && !done) != (applicable && !done) || changed
	changed = mw.wifiTuneApplicable.Swap(applicable) != applicable || changed
	wasOn := mw.wlanStreamingOn.Load()
	mw.syncWLANStreamingState(mw.isStreaming)
	if changed || wasOn != mw.wlanStreamingOn.Load() {
		fyne.Do(mw.refreshWLANUI)
	}
}

// refreshWLANUI shows the footer icon only with a connected Wi-Fi, and
// picks its state: amber while the one-time tune is needed, lime while a
// stream holds the Wi-Fi in streaming mode, green when that is armed,
// grey when it is off (or, on Linux, nothing is left to do).
func (mw *MainWindow) refreshWLANUI() {
	if mw.wlanIcon == nil {
		return
	}
	if !wlanSupported() || !mw.wlanPresent.Load() || (!wlanSessionModeSupported() && !mw.wifiTuneApplicable.Load()) {
		mw.wlanIcon.Hide()
		return
	}
	mw.wlanIcon.Show()
	switch {
	case mw.wifiTuneNeeded.Load():
		mw.wlanIcon.SetIcon(assets.WLANIconAttention)
	case mw.wlanStreamingOn.Load():
		mw.wlanIcon.SetIcon(assets.WLANIconStatusBar)
	case wlanSessionModeSupported() && mw.wlanStreamingEnabled():
		mw.wlanIcon.SetIcon(assets.WLANIconActive)
	case !wlanSessionModeSupported():
		mw.wlanIcon.SetIcon(assets.WLANIconActive)
	default:
		mw.wlanIcon.SetIcon(assets.WLANIcon)
	}
	mw.wlanIcon.Refresh()
}

// showWLANMenu is the footer icon's click target, same shape as
// showAWDLMenu: the per-stream toggle (Windows) and the one-time tune
// (when this Wi-Fi has one).
func (mw *MainWindow) showWLANMenu() {
	if mw.wlanIcon == nil {
		return
	}
	var items []view.StyledMenuItem
	if wlanSessionModeSupported() {
		enabled := mw.wlanStreamingEnabled()
		items = append(items, view.StyledMenuItem{
			Label:          i18n.Current.WLANStreamingMode,
			SecondaryLabel: i18n.Current.WLANStreamingModeHint,
			Selected:       enabled,
			OnTap: func() {
				next := !enabled
				go func() {
					mw.setWLANStreamingEnabled(next)
					fyne.Do(mw.refreshWLANUI)
				}()
			},
		})
	}
	if mw.wifiTuneApplicable.Load() {
		needed := mw.wifiTuneNeeded.Load()
		hint := i18n.Current.WiFiTuneDoneHint
		if needed {
			hint = i18n.Current.WiFiTuneHint
		}
		items = append(items, view.StyledMenuItem{
			Label:          i18n.Current.WiFiTune,
			SecondaryLabel: hint,
			Selected:       !needed,
			OnTap: func() {
				if !needed {
					return
				}
				go mw.applyWiFiTune()
			},
		})
	}
	if len(items) == 0 {
		return
	}
	view.ShowStyledMenuTealAbove(mw.wlanIcon, items)
}

// applyWiFiTune runs the one-time tune (UAC / pkexec prompt); off the UI
// goroutine.
func (mw *MainWindow) applyWiFiTune() {
	if !mw.wifiTuneBusy.CompareAndSwap(false, true) {
		return
	}
	defer mw.wifiTuneBusy.Store(false)
	err := netutil.ApplyWiFiTune()
	if err != nil {
		logrus.Warnf("[wlan] Wi-Fi tune: %v", err)
	} else {
		logrus.Infof("[wlan] Wi-Fi tune applied (driver background scans off)")
	}
	mw.pollWLAN()
	fyne.Do(func() {
		if err != nil {
			view.ShowErrorDialog(err, mw.window)
		}
		mw.refreshWLANUI()
	})
}
