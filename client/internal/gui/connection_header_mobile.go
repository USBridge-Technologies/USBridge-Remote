package gui

import (
	"image/color"
	"runtime"

	"usbridge-client/internal/gui/design"
	"usbridge-client/internal/gui/view"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
)

// Phone Connections header is ~10% larger than the desktop compact chrome
// so settings / Tailscale / avatar stay hittable on a real handset.
const mobileConnectionHeaderScale float32 = 1.1

var headerMobileButtonSize = fyne.NewSize(31, 31)

// newMobileConnectionHeader is the phone Connections chrome: settings on
// the left (no USBridge lockup), Tailscale + account avatar on the right.
func newMobileConnectionHeader(actions connectionHeaderActions) (*fyne.Container, *ConnectionHeaderHandle) {
	handle := &ConnectionHeaderHandle{}
	var tailscaleAccessory fyne.CanvasObject
	if runtime.GOOS == "js" {
		tailscaleAccessory = canvas.NewRectangle(color.Transparent)
	} else {
		toggle := newTailscaleHeaderToggle(actions.OnToggleTailscale)
		toggle.scale = mobileConnectionHeaderScale
		toggle.wholeChipTappable = true
		handle.toggle = toggle
		tailscaleAccessory = toggle
	}

	overflow := newMobileConnectionOverflowButton(actions)
	loginBtn := newLoginAvatarButton("U", func() {
		if actions.OnOpenAccount != nil {
			actions.OnOpenAccount()
		}
	})
	loginBtn.side = 26
	loginBtn.letterSize = 12
	handle.avatar = loginBtn

	rightRow := container.New(&centeredInlineLayout{gap: 10, minGap: 8},
		tailscaleAccessory,
		container.NewGridWrap(headerMobileButtonSize, loginBtn),
	)

	row := container.NewHBox(
		container.NewGridWrap(headerMobileButtonSize, overflow),
		layout.NewSpacer(),
		rightRow,
	)
	bg := canvas.NewRectangle(design.ColorGray900)
	paddedRow := view.NewInsetExact(row, 13, 13, 5, 5)

	accentLine := canvas.NewRectangle(design.ColorHeaderAccentLine)
	accentLine.SetMinSize(fyne.NewSize(1, 0.5))
	content := view.NewBottomLine(paddedRow, accentLine)
	return container.NewStack(bg, content), handle
}

func newMobileConnectionOverflowButton(actions connectionHeaderActions) fyne.CanvasObject {
	var btn *headerStatusBadgeButton
	btn = newHeaderStatusBadgeButton(gearIconHeader, func() {
		mode := "grid"
		if actions.ViewMode != nil {
			if current := actions.ViewMode(); current != "" {
				mode = current
			}
		}
		view.ShowMobileSettingsMenu(btn, mode,
			actions.OnViewModeChange,
			actions.OnOpenInfo,
			actions.OnOpenCommunity,
			func() {
				if actions.OnShowLanguageMenu != nil {
					actions.OnShowLanguageMenu(btn)
				}
			},
		)
	})
	btn.SetBadgeText("")
	btn.SetIconSize(fyne.NewSize(17, 17))
	return btn
}
