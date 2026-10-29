//go:build linux && !android

package gui

import (
	"os"
	"testing"

	"usbridge-client/internal/gui/assets"
	"usbridge-client/internal/gui/i18n"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
)

// TestWLANTuneInSnapLive drives the Control footer's Wi-Fi icon the way a
// snap does, against this machine's real NetworkManager: amber while power
// saving is on, the tune through D-Bus, then the done state. It changes the
// active Wi-Fi connections, so it only runs with USBRIDGE_NM_WIFI_TEST=1
// (e.g. on mac80211_hwsim radios with powersave reset to default).
func TestWLANTuneInSnapLive(t *testing.T) {
	if os.Getenv("USBRIDGE_NM_WIFI_TEST") != "1" {
		t.Skip("set USBRIDGE_NM_WIFI_TEST=1 to change the active Wi-Fi connection")
	}
	t.Setenv("SNAP", "/snap/usbridge-client/x1")
	i18n.Init("en")
	app := test.NewTempApp(t)
	w := app.NewWindow("test")
	w.Resize(fyne.NewSize(900, 700))
	mw := &MainWindow{app: app, window: w}
	mw.wlanIcon = newHeaderStatusBadgeButton(assets.WLANIcon, mw.showWLANMenu)
	mw.wlanIcon.Hide()

	mw.pollWLAN()
	if !mw.wlanIcon.Visible() {
		t.Fatal("Wi-Fi icon hidden with a Wi-Fi connection up")
	}
	if !mw.wifiTuneNeeded.Load() || mw.wlanIcon.iconRes != assets.WLANIconAttention {
		t.Fatalf("before the tune: needed=%v, icon=%v; want amber", mw.wifiTuneNeeded.Load(), mw.wlanIcon.iconRes.Name())
	}

	mw.applyWiFiTune()

	if top := w.Canvas().Overlays().Top(); top != nil {
		t.Fatal("an error dialog opened")
	}
	if mw.wifiTuneNeeded.Load() || !mw.wifiTuneApplicable.Load() {
		t.Fatalf("after the tune: needed=%v applicable=%v", mw.wifiTuneNeeded.Load(), mw.wifiTuneApplicable.Load())
	}
	if !mw.wlanIcon.Visible() || mw.wlanIcon.iconRes != assets.WLANIconActive {
		t.Fatalf("after the tune: visible=%v icon=%v; want the done icon", mw.wlanIcon.Visible(), mw.wlanIcon.iconRes.Name())
	}
}
