package gui

import (
	"strconv"
	"sync/atomic"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"

	"usbridge-client/internal/gui/controller"
	"usbridge-client/internal/gui/i18n"
	"usbridge-client/internal/gui/view"
	"usbridge-client/internal/service"
)

// Client settings (gear menu): the client's own settings, next to KVM
// settings -- the language and, on touch screens, how fast a two-finger
// swipe scrolls the remote PC.

var clientSettingsOpen atomic.Bool

const clientSettingsWidth = 420

func (mw *MainWindow) showClientSettingsDialog() {
	if mw.connectionManager == nil || !clientSettingsOpen.CompareAndSwap(false, true) {
		return
	}
	cm := mw.connectionManager

	current := cm.CurrentLanguage()
	var names []string
	selected := ""
	for _, l := range controller.ClientLanguages {
		names = append(names, l.Name)
		if l.Code == current {
			selected = l.Name
		}
	}
	language := view.NewDialogPicker(names, selected, nil)
	body := container.NewVBox(view.NewDialogField(i18n.Current.Language, language))

	var scrollSpeed func() int
	if view.IsMobile() {
		var row fyne.CanvasObject
		row, scrollSpeed = view.NewDialogStepSlider(i18n.Current.ScrollSpeed,
			service.ScrollSpeedMinLevel, service.ScrollSpeedMaxLevel, service.ScrollSpeedLevel(), strconv.Itoa)
		body.Add(view.NewDialogVSpace(10))
		body.Add(row)
	}

	apply := func() {
		clientSettingsOpen.Store(false)
		if scrollSpeed != nil {
			level := scrollSpeed()
			service.SetScrollSpeedLevel(level)
			view.SaveScrollSpeedPreference(level)
		}
		for _, l := range controller.ClientLanguages {
			if l.Name == language.Selected && l.Code != current {
				// Rebuilds the UI: last, after the dialog is gone.
				fyne.Do(func() { cm.SetLanguage(l.Code) })
			}
		}
	}
	view.ShowBrandFormDialog(view.BrandFormDialogSpec{
		Parent:     mw.window,
		Title:      i18n.Current.MenuClientSettings,
		Body:       container.NewVScroll(body),
		CancelText: i18n.Current.Cancel,
		ApplyText:  i18n.Current.Apply,
		OnCancel:   func() { clientSettingsOpen.Store(false) },
		OnApply:    apply,
		MinWidth:   clientSettingsWidth,
		PanelSize: func(canvasSize fyne.Size, panel fyne.CanvasObject) fyne.Size {
			w := minF(clientSettingsWidth+40, canvasSize.Width-24)
			h := minF(320, canvasSize.Height-24)
			return fyne.NewSize(w, h)
		},
	})
}
