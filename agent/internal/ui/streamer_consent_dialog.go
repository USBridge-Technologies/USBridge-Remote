package ui

import (
	"fyne.io/fyne/v2"
)

// showStreamerConsentDialog displays the branded confirmation modal when switching
// to a USBridge protocol tier if the closed-source USBridge Streamer is not yet staged.
func (w *Window) showStreamerConsentDialog(parent fyne.Window, onResult func(bool)) {
	showStreamerConsentDialog(parent, onResult)
}

// showStreamerConsentDialog displays the modal popup with the application design.
func showStreamerConsentDialog(parent fyne.Window, onResult func(bool)) {
	title := loc().StreamerConsentTitle
	if title == "" {
		title = "Switch to USBridge protocol?"
	}

	bodyText := loc().StreamerConsentBody
	if bodyText == "" {
		bodyText = "USBridge is powered by a separate, closed-source streaming component (not open-source like Sunshine and the rest of this agent). Switching to this protocol will download and install the USBridge Streamer component. Do you want to proceed?"
	}

	showConfirmDialog(title, bodyText, onResult, parent)
}
