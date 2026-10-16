package assets

import "fyne.io/fyne/v2"

// Wi-Fi streaming mode status icon (Windows): the same Wi-Fi glyph as the
// macOS AWDL icon (awdl_icon.go), never slashed -- here the active state
// means "streaming mode on", not "something switched off".
//
//   - WLANIcon (muted): the setting is off.
//   - WLANIconActive (green): on, idle -- not streaming over Wi-Fi right now.
//   - WLANIconStatusBar (lime): streaming with the Wi-Fi held in streaming
//     mode, background scans off.
var (
	WLANIcon = fyne.NewStaticResource("wlan-status-off.svg", []byte(awdlIconSVG("#8E8E8E", false)))

	WLANIconActive = fyne.NewStaticResource("wlan-status-active.svg", []byte(awdlIconSVG("#93C572", false)))

	WLANIconStatusBar = fyne.NewStaticResource("wlan-status-streaming.svg", []byte(awdlIconSVG("#c4e77a", false)))

	// WLANIconAttention (amber): the one-time Wi-Fi tune is still needed --
	// the footer menu says what and how.
	WLANIconAttention = fyne.NewStaticResource("wlan-status-attention.svg", []byte(awdlIconSVG("#E8A33D", false)))

	WLANIconFooterHover = fyne.NewStaticResource("wlan-status-footer-hover.svg", []byte(awdlIconSVG("#e0e3e7", false)))
)
