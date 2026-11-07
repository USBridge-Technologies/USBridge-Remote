package gui

import (
	"runtime"
	"strconv"
	"sync/atomic"

	"github.com/sirupsen/logrus"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"

	"usbridge-client/internal/gui/controller"
	"usbridge-client/internal/gui/i18n"
	"usbridge-client/internal/gui/view"
	"usbridge-client/internal/service"
)

// Client settings (gear menu): the client's own settings, next to KVM
// settings -- the language, the video renderer (desktop; plus the display
// server on Linux) and, on touch screens, how fast a two-finger swipe
// scrolls the remote PC.

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

	rendererPrefs := service.CurrentVideoRendererPrefs()
	renderer := addChoicePicker(body, i18n.Current.VideoRenderer, service.VideoRenderers(), rendererPrefs.Renderer)
	displayServer := addChoicePicker(body, i18n.Current.DisplayServer, service.DisplayServers(), rendererPrefs.DisplayServer)
	if renderer != nil {
		hint := i18n.Current.RendererHintMac
		switch runtime.GOOS {
		case "linux":
			hint = i18n.Current.RendererHintLinux
		case "windows":
			hint = i18n.Current.RendererHintWindows
		}
		body.Add(view.NewDialogVSpace(4))
		body.Add(view.NewDialogHint(hint, clientSettingsWidth))
	}

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
		if renderer != nil {
			next := service.VideoRendererPrefs{Renderer: renderer(), DisplayServer: rendererPrefs.DisplayServer}
			if displayServer != nil {
				next.DisplayServer = displayServer()
			}
			if next != rendererPrefs {
				if err := service.SaveVideoRendererPrefs(next); err != nil {
					logrus.Warnf("🎞️ [Renderer] saving the renderer choice failed: %v", err)
				}
			}
		}
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
			h := minF(clientSettingsHeight(renderer != nil, displayServer != nil), canvasSize.Height-24)
			return fyne.NewSize(w, h)
		},
	})
}

// rendererLabel is the picker text of a renderer / display server ID.
func rendererLabel(id string) string {
	switch id {
	case service.VideoRendererAuto, "": // == service.DisplayServerAuto
		return i18n.Current.RendererAuto
	case service.VideoRendererVulkan:
		return "Vulkan"
	case service.VideoRendererOpenGL:
		return "OpenGL"
	case service.VideoRendererD3D11:
		return "Direct3D 11"
	case service.VideoRendererGDI:
		return i18n.Current.RendererGDI
	case service.VideoRendererMetal:
		return "Metal"
	case service.VideoRendererSoftware:
		return i18n.Current.RendererSoftware
	case service.DisplayServerWayland:
		return "Wayland"
	case service.DisplayServerX11:
		return "X11"
	}
	return id
}

// addChoicePicker adds a labelled picker over ids (Auto when current is
// empty) and returns a getter of the chosen ID; nil when there are no ids.
func addChoicePicker(body *fyne.Container, title string, ids []string, current string) func() string {
	if len(ids) == 0 {
		return nil
	}
	if current == "" {
		current = ids[0]
	}
	names := make([]string, len(ids))
	selected := names[0]
	for i, id := range ids {
		names[i] = rendererLabel(id)
		if id == current {
			selected = names[i]
		}
	}
	if selected == "" {
		selected = names[0]
	}
	picker := view.NewDialogPicker(names, selected, nil)
	body.Add(view.NewDialogVSpace(10))
	body.Add(view.NewDialogField(title, picker))
	return func() string {
		for i, n := range names {
			if n == picker.Selected {
				return ids[i]
			}
		}
		return ids[0]
	}
}

// clientSettingsHeight fits the panel to the pickers it holds.
func clientSettingsHeight(renderer, displayServer bool) float32 {
	h := float32(320)
	if renderer {
		h += 60
	}
	if displayServer {
		h += 40
	}
	return h
}
