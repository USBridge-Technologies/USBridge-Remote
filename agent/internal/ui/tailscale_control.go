package ui

import (
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"usbridge_agent/internal/ui/design"
)

// The owner's own control server (a self-hosted Headscale) for this
// computer's Tailscale. Niche, so nothing on screen shows it: a right click
// or long press on the header's Tailscale chip opens this choice, and
// regular Tailscale is the default.

const (
	controlChoiceTailscale = "Tailscale"
	controlChoiceHeadscale = "My own Headscale"
)

func (w *Window) showControlServerDialog(parent fyne.Window) {
	if parent == nil {
		return
	}
	entry := widget.NewEntry()
	entry.SetPlaceHolder("https://headscale.example.com")
	entry.SetText(w.cfg.TailscaleControlURL)

	hint := widget.NewLabel("Each server keeps its own sign-in: after switching, press Sign In once; switching back resumes the old login. Headscale gives no trusted certificates, so HTTPS here stays self-signed on the IP addresses: the browser Web Client reaches this computer only after you open https://<its IP>:<HTTPS port> in that browser once and accept the certificate. The apps are unaffected.")
	hint.Wrapping = fyne.TextWrapWord
	headscaleBox := container.New(&tightVBoxLayout{gap: 8}, wrapDialogField(entry), wrapDialogLabel(hint, 10, design.ColorMutedOlive))

	choice := widget.NewRadioGroup([]string{controlChoiceTailscale, controlChoiceHeadscale}, func(sel string) {
		if sel == controlChoiceHeadscale {
			headscaleBox.Show()
		} else {
			headscaleBox.Hide()
		}
	})
	if strings.TrimSpace(w.cfg.TailscaleControlURL) != "" {
		choice.SetSelected(controlChoiceHeadscale)
	} else {
		choice.SetSelected(controlChoiceTailscale)
	}

	errLabel := dialogErrorText()
	var popup *widget.PopUp
	closeDialog := func() {
		if popup != nil {
			popup.Hide()
		}
	}
	var saveBtn *iconActionButton
	save := func() {
		if w.token == nil || saveBtn.Disabled() {
			return
		}
		value := ""
		if choice.Selected == controlChoiceHeadscale {
			value = strings.TrimSpace(entry.Text)
			if value == "" {
				setDialogError(errLabel, "Enter your Headscale's address.")
				return
			}
		}
		saveBtn.Disable()
		go func() {
			cfg, err := w.token.SetTailscaleControlURL(value)
			fyne.Do(func() {
				saveBtn.Enable()
				if err != nil {
					setDialogError(errLabel, err.Error())
					return
				}
				w.cfg = cfg
				closeDialog()
				go w.performRefresh()
			})
		}()
	}
	saveBtn = newDialogCTA("Save", save)
	entry.OnSubmitted = func(string) { save() }

	body := container.New(&tightVBoxLayout{gap: 8}, choice, headscaleBox, errLabel)
	panel := newBrandedDialogPanelInsets("Tailscale server", statusDialogWidth, 20, 10, body, container.NewCenter(saveBtn), closeDialog)
	popup = showOverlayPopup(parent, overlayPopupSpec{Panel: panel})
}
