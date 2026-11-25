package gui

import (
	"image/color"
	"runtime"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"usbridge-client/internal/gui/controller"
	"usbridge-client/internal/gui/design"
	"usbridge-client/internal/gui/i18n"
	"usbridge-client/internal/gui/view"
)

// wireVideoHotkeys connects the video widget's Ctrl+Alt+Shift hotkeys
// (controller/video_widget_hotkeys.go) to main-window state.
func (mw *MainWindow) wireVideoHotkeys() {
	if mw.videoWidget == nil {
		return
	}
	mw.videoWidget.SetHotkeyActions(controller.HotkeyActions{
		ToggleMouseMode: func() {
			if mw.diskWidget == nil {
				return
			}
			if view.IsMobile() {
				// Touchpad only exists on mobile (mouseConfigOptions'
				// own doc comment) -- unchanged binary toggle there.
				if mw.diskWidget.GetMouseMode() == controller.MouseModeAbsolute {
					mw.diskWidget.SetMouseMode(controller.MouseModeTouchPad)
				} else {
					mw.diskWidget.SetMouseMode(controller.MouseModeAbsolute)
				}
				return
			}
			// Desktop only ever offers Absolute and Capture -- simple toggle.
			if mw.diskWidget.GetMouseMode() == controller.MouseModeCapture {
				mw.diskWidget.SetMouseMode(controller.MouseModeAbsolute)
			} else {
				mw.diskWidget.SetMouseMode(controller.MouseModeCapture)
			}
		},
		ReleaseMouseCapture: func() {
			if mw.diskWidget == nil {
				return
			}
			if mw.diskWidget.GetMouseMode() == controller.MouseModeCapture {
				mw.diskWidget.SetMouseMode(controller.MouseModeAbsolute)
			}
		},
		ToggleShowMouse: mw.toggleShowMouse,
	})
}

func (mw *MainWindow) toggleShowMouse() {
	next := !mw.app.Preferences().BoolWithFallback("show_mouse_cursor", false)
	mw.app.Preferences().SetBool("show_mouse_cursor", next)
	if mw.videoWidget != nil {
		mw.videoWidget.SetShowMouseCursor(next)
	}
}

func hotkeyKeyChip(label string) fyne.CanvasObject {
	t := canvas.NewText(label, design.ColorConnectionBadgeText)
	t.TextSize = 10
	t.TextStyle = fyne.TextStyle{Bold: true, Monospace: true}
	bg := canvas.NewRectangle(design.ColorGray950)
	bg.CornerRadius = 6
	border := canvas.NewRectangle(color.Transparent)
	border.CornerRadius = 6
	border.StrokeColor = design.ColorTailscaleChipBorder
	border.StrokeWidth = 1
	return container.NewStack(bg, border, view.NewInsetExact(container.NewCenter(t), 8, 8, 4, 4))
}

// hotkeyRowLayout: chip left, description right-aligned, equal vertical pad
// so the first/last rows don't sit higher/lower than the middle ones.
type hotkeyRowLayout struct{}

func (l *hotkeyRowLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	if len(objects) < 2 {
		return fyne.NewSize(0, 0)
	}
	chip := objects[0].MinSize()
	desc := objects[1].MinSize()
	const padY, gap = float32(4), float32(10)
	h := chip.Height
	if desc.Height > h {
		h = desc.Height
	}
	return fyne.NewSize(chip.Width+gap+desc.Width, h+padY*2)
}

func (l *hotkeyRowLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) < 2 {
		return
	}
	const padY, gap = float32(4), float32(10)
	chip, desc := objects[0], objects[1]
	cs := chip.MinSize()
	innerH := size.Height - padY*2
	if innerH < 0 {
		innerH = 0
	}
	chip.Resize(cs)
	chip.Move(fyne.NewPos(0, padY+(innerH-cs.Height)/2))

	descW := size.Width - cs.Width - gap
	if descW < 0 {
		descW = 0
	}
	ds := desc.MinSize()
	descH := ds.Height
	if descH > innerH {
		descH = innerH
	}
	desc.Resize(fyne.NewSize(descW, descH))
	desc.Move(fyne.NewPos(cs.Width+gap, padY+(innerH-descH)/2))
}

func hotkeyDesc(text string) fyne.CanvasObject {
	t := canvas.NewText(text, color.NRGBA{R: 0x8f, G: 0x93, B: 0x81, A: 0xff})
	t.TextSize = 10
	t.Alignment = fyne.TextAlignTrailing
	return t
}

type hotkeyListLayout struct{}

func (l *hotkeyListLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	var w, h float32
	for _, o := range objects {
		m := o.MinSize()
		if m.Width > w {
			w = m.Width
		}
		h += m.Height
	}
	return fyne.NewSize(w, h)
}

func (l *hotkeyListLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	var y float32
	for _, o := range objects {
		m := o.MinSize()
		o.Resize(fyne.NewSize(size.Width, m.Height))
		o.Move(fyne.NewPos(0, y))
		y += m.Height
	}
}

func (mw *MainWindow) showHotkeysDialog() {
	rows := []struct{ key, what string }{
		{"Q", i18n.Current.HotkeyQuit},
		{"X", i18n.Current.HotkeyFullscreen},
		{"S", i18n.Current.HotkeyStats},
		{"M", i18n.Current.HotkeyMouseMode},
		{"Z", i18n.Current.HotkeyMouseRelease},
		{"N", i18n.Current.HotkeyCursor},
		{"V", i18n.Current.HotkeyPaste},
		{"F1 … F12", i18n.Current.HotkeyDisplays},
	}

	var list []fyne.CanvasObject
	for _, r := range rows {
		list = append(list, container.New(&hotkeyRowLayout{}, hotkeyKeyChip(hotkeyModsLabel()+r.key), hotkeyDesc(r.what)))
	}
	// Not a Ctrl+Alt+Shift combo: fullscreen's own way out.
	list = append(list, container.New(&hotkeyRowLayout{}, hotkeyKeyChip(leaveFullscreenKeys()), hotkeyDesc(i18n.Current.HotkeyLeaveFullscreen)))
	if runtime.GOOS == "darwin" {
		// The MacBook's top row is media keys; with Fn+Ctrl+Option the F
		// key itself reaches the host.
		list = append(list, container.New(&hotkeyRowLayout{}, hotkeyKeyChip("Fn+Ctrl+Option+F1 … F12"), hotkeyDesc(i18n.Current.HotkeyFKeysMac)))
	}

	body := view.NewDialogSurfaceCard(container.New(&hotkeyListLayout{}, list...))
	subtitle := i18n.Current.HotkeysTitle + ". " + i18n.Current.HotkeysNote

	var popup *widget.PopUp
	hide := func() {
		if popup != nil {
			popup.Hide()
		}
	}
	panel := view.AssembleBrandDialogChromeSub(i18n.Current.MenuHotkeys, subtitle, view.NewInset(body, 18, 18, 10, 10), hide)
	popup = view.ShowOverlayPopup(mw.window, view.OverlayPopupSpec{
		Panel:        panel,
		DimColor:     color.NRGBA{R: 0x00, G: 0x00, B: 0x00, A: 0x72},
		OnOutsideTap: hide,
		PanelSize: func(canvasSize fyne.Size, panel fyne.CanvasObject) fyne.Size {
			margin := float32(24)
			maxW := canvasSize.Width - margin*2
			maxH := canvasSize.Height - margin*2
			min := panel.MinSize()
			if min.Width < 460 {
				min.Width = 460
			}
			if min.Width > maxW && maxW > 0 {
				min.Width = maxW
			}
			if min.Height > maxH && maxH > 0 {
				min.Height = maxH
			}
			return min
		},
	})
}

// leaveFullscreenKeys: Ctrl+Alt+Shift+X has its own row (Moonlight's);
// on a Mac F11 goes to the host like the other F keys.
func leaveFullscreenKeys() string {
	if runtime.GOOS == "darwin" {
		return "Ctrl+Esc"
	}
	return "F11 / Ctrl+Esc"
}

// hotkeyModsLabel is how the hotkeys' Ctrl+Alt+Shift reads on this
// keyboard: Alt is Option on a Mac.
func hotkeyModsLabel() string {
	if runtime.GOOS == "darwin" {
		return "Ctrl+Option+Shift+"
	}
	return "Ctrl+Alt+Shift+"
}
