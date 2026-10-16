package controller

import (
	"image/color"
	"runtime"
	"sync/atomic"

	"usbridge-client/internal/gui/assets"
	"usbridge-client/internal/gui/design"
	"usbridge-client/internal/gui/i18n"
	"usbridge-client/internal/gui/view"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
	"github.com/sirupsen/logrus"
)

// Android: Wi-Fi settings that cause stream stutters but that no app can
// change (no root) -- location Wi-Fi scanning, Pixel's adaptive
// connectivity, the vendors' switch-to-mobile-data, Android's scan
// throttling. The client already holds the Wi-Fi in high performance / low
// latency mode for a stream (gui/wlan_android.go); this dialog explains
// the rest, each tip with a button straight into the right settings page.
// Reached from the "?" menu, and shown once at the first stream of a run
// while location Wi-Fi scanning (the one setting an app can read) is on.

// wifiTipsDismissedPrefKey: "don't show again" for the automatic showing.
const wifiTipsDismissedPrefKey = "wifi_tips_dismissed"

var wifiTipsShownThisRun atomic.Bool

func wifiTipsSupported() bool { return runtime.GOOS == "android" }

// wifiTip is one row: what to change, where, and the settings pages to
// try (the first one this device has).
type wifiTip struct {
	title   string
	where   string
	actions []string
	status  string
}

// MaybeShowWiFiTipsOnStream shows the tips once per run at stream start
// while location Wi-Fi scanning is on, unless the user said not to. UI
// goroutine.
func MaybeShowWiFiTipsOnStream(parent fyne.Window) {
	if !wifiTipsSupported() || parent == nil || wifiTipsShownThisRun.Load() {
		return
	}
	if app := fyne.CurrentApp(); app != nil && app.Preferences().BoolWithFallback(wifiTipsDismissedPrefKey, false) {
		return
	}
	if androidWiFiScanAlwaysOn() != 1 {
		return
	}
	logrus.Info("📶 location Wi-Fi scanning is on -- showing the Wi-Fi tips")
	ShowWiFiTips(parent, true)
}

// ShowWiFiTips shows the dialog; auto adds "Don't show again".
func ShowWiFiTips(parent fyne.Window, auto bool) {
	if parent == nil {
		return
	}
	wifiTipsShownThisRun.Store(true)
	t := i18n.Current

	scanStatus := ""
	switch androidWiFiScanAlwaysOn() {
	case 1:
		scanStatus = t.WiFiTipsNowOn
	case 0:
		scanStatus = t.WiFiTipsNowOff
	}
	tips := []wifiTip{
		{t.WiFiTipScanTitle, t.WiFiTipScanWhere, []string{"android.settings.LOCATION_SCANNING_SETTINGS", "android.settings.LOCATION_SOURCE_SETTINGS"}, scanStatus},
		{t.WiFiTipAdaptiveTitle, t.WiFiTipAdaptiveWhere, []string{"android.settings.ADAPTIVE_CONNECTIVITY_SETTINGS", "android.settings.WIRELESS_SETTINGS"}, ""},
		{t.WiFiTipMobileDataTitle, t.WiFiTipMobileDataWhere, []string{"android.settings.WIFI_SETTINGS"}, ""},
		{t.WiFiTipThrottleTitle, t.WiFiTipThrottleWhere, []string{"android.settings.APPLICATION_DEVELOPMENT_SETTINGS", "android.settings.DEVICE_INFO_SETTINGS"}, ""},
	}

	rows := []fyne.CanvasObject{}
	for i, tip := range tips {
		if i > 0 {
			sep := canvas.NewRectangle(design.ColorBorder)
			sep.SetMinSize(fyne.NewSize(1, 1))
			rows = append(rows, sep)
		}
		title := widget.NewRichText(&widget.TextSegment{Text: tip.title, Style: widget.RichTextStyle{TextStyle: fyne.TextStyle{Bold: true}, ColorName: "foreground"}})
		title.Wrapping = fyne.TextWrapWord
		texts := []fyne.CanvasObject{title}
		if tip.status != "" {
			st := canvas.NewText(tip.status, design.ColorAccent)
			st.TextSize = 11
			texts = append(texts, st)
		}
		where := widget.NewLabel(tip.where)
		where.Wrapping = fyne.TextWrapWord
		where.Importance = widget.LowImportance
		texts = append(texts, where)

		actions := tip.actions
		open := &connectionDialogSecondaryButton{
			labelText:      t.WiFiTipsOpen,
			compact:        true,
			short:          true,
			fillColor:      color.Transparent,
			borderColor:    design.ColorAccent,
			textColor:      design.ColorAccent,
			hoverFillColor: design.ColorAccent,
			hoverTextColor: design.ColorBackground,
			onTapped: func() {
				if !openAndroidSettings(actions) {
					logrus.Warnf("📶 no settings page for %v on this device", actions)
				}
			},
		}
		open.ExtendBaseWidget(open)
		rows = append(rows, container.NewBorder(nil, nil, nil, container.NewCenter(open), container.NewVBox(texts...)))
	}
	note := widget.NewLabel(t.WiFiTipsAppNote)
	note.Wrapping = fyne.TextWrapWord
	note.Importance = widget.LowImportance
	rows = append(rows, note)
	body := container.NewVScroll(container.NewVBox(rows...))
	body.SetMinSize(fyne.NewSize(0, 320))

	var closePopup func()
	okBtn := newVirtualDisplayAddButton(func() {
		if closePopup != nil {
			closePopup()
		}
	})
	okBtn.labelText = t.WiFiTipsOK
	buttons := []fyne.CanvasObject{okBtn}
	if auto {
		dontShow := &connectionDialogSecondaryButton{
			labelText:      t.WiFiTipsDontShow,
			compact:        true,
			short:          true,
			fillColor:      color.Transparent,
			borderColor:    color.Transparent,
			textColor:      color.NRGBA{R: 0x8f, G: 0x93, B: 0x81, A: 0xff},
			hoverFillColor: color.Transparent,
			hoverTextColor: design.ColorTextLight,
			onTapped: func() {
				if app := fyne.CurrentApp(); app != nil {
					app.Preferences().SetBool(wifiTipsDismissedPrefKey, true)
				}
				if closePopup != nil {
					closePopup()
				}
			},
		}
		dontShow.ExtendBaseWidget(dontShow)
		buttons = []fyne.CanvasObject{dontShow, okBtn}
	}

	_, closePopup = showBrandedOverlayDialog(brandedOverlayDialogSpec{
		parent:          parent,
		title:           t.WiFiTipsTitle,
		subtitle:        t.WiFiTipsSubtitle,
		body:            body,
		rightButtons:    buttons,
		compactFooter:   true,
		hideCancel:      true,
		headerPadBottom: 6,
		footerPadTop:    6,
		footerPadBottom: 8,
		panelSize:       virtualDisplayOfferPanelSize,
	})
}

// wifiTipsMenuItem is the "?" menu's entry (Android only).
func wifiTipsMenuItem(parent fyne.Window) []view.StyledMenuItem {
	if !wifiTipsSupported() {
		return nil
	}
	return []view.StyledMenuItem{{
		Label: i18n.Current.WiFiTipsMenu,
		Icon:  assets.WLANIconActive,
		OnTap: func() { ShowWiFiTips(parent, false) },
	}}
}
