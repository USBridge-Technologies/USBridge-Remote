package view

import (
	"image/color"

	"usbridge-client/internal/gui/design"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)

// ScriptFooterKind is the aggregate script-run chip shown in every
// connected-session tab footer (Control, Devices, Snapshots, Scripts).
type ScriptFooterKind int

const (
	ScriptFooterIdle ScriptFooterKind = iota
	ScriptFooterRunning
	ScriptFooterError
	ScriptFooterDone
)

const (
	scriptFooterRunningText = "running script"
	scriptFooterErrorText   = "error script"
	scriptFooterDoneText    = "done script"
)

var (
	scriptFooterErrorColor = color.NRGBA{R: 0xff, G: 0x5a, B: 0x52, A: 0xff}
	scriptFooterErrorIcon  = fyne.NewStaticResource("script-footer-error.svg", []byte(
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="#ff5a52"><path d="M12 2.6L2.4 20.4h19.2L12 2.6zm0 5.2l.9 7.2h-1.8L12 7.8zm0 10.7a1.15 1.15 0 1 0 0-2.3 1.15 1.15 0 0 0 0 2.3z"/></svg>`))
	scriptFooterDoneIcon = fyne.NewStaticResource("script-footer-done.svg", []byte(
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="#41e0c3" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round"><path d="M5 12.5l5 5L19 7"/></svg>`))
	scriptFooterCloseIcon = fyne.NewStaticResource("script-footer-close.svg", []byte(
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="#8f9381"><path d="M19 6.41 17.59 5 12 10.59 6.41 5 5 6.41 10.59 12 5 17.59 6.41 19 12 13.41 17.59 19 19 17.59 13.41 12z"/></svg>`))
	scriptFooterCloseHoverIcon = fyne.NewStaticResource("script-footer-close-hover.svg", []byte(
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="#f5f5f5"><path d="M19 6.41 17.59 5 12 10.59 6.41 5 5 6.41 10.59 12 5 17.59 6.41 19 12 13.41 17.59 19 19 17.59 13.41 12z"/></svg>`))
)

// ScriptFooterStatus is the left-side footer chip for script runs: a lime
// spinner while a script is running, a red warning while one has failed,
// and a turquoise check after the last run finishes cleanly. Hidden while
// idle so it doesn't leave a hole next to the device-connect spinner.
// Done/error keep an X visible and dismiss on a click anywhere on the chip.
type ScriptFooterStatus struct {
	widget.BaseWidget

	kind      ScriptFooterKind
	hovered   bool
	onDismiss func()
	spinner   *DeviceDashboardBusySpinner
	icon      *canvas.Image
	label     *canvas.Text
	closeBtn  *iconChromeButton
	box       *fyne.Container
}

var (
	_ desktop.Hoverable = (*ScriptFooterStatus)(nil)
	_ fyne.Tappable     = (*ScriptFooterStatus)(nil)
)

func NewScriptFooterStatus() *ScriptFooterStatus {
	s := &ScriptFooterStatus{}
	s.ExtendBaseWidget(s)
	s.Hide()
	return s
}

func (s *ScriptFooterStatus) SetOnDismiss(fn func()) {
	s.onDismiss = fn
}

func (s *ScriptFooterStatus) SetKind(kind ScriptFooterKind) {
	s.kind = kind
	s.applyChildren()
	if kind == ScriptFooterIdle {
		s.Hide()
		return
	}
	s.Show()
}

func (s *ScriptFooterStatus) dismissable() bool {
	return s.kind == ScriptFooterDone || s.kind == ScriptFooterError
}

func (s *ScriptFooterStatus) Tapped(*fyne.PointEvent) {
	if s.dismissable() && s.onDismiss != nil {
		s.onDismiss()
	}
}

func (s *ScriptFooterStatus) TappedSecondary(*fyne.PointEvent) {}

func (s *ScriptFooterStatus) Cursor() desktop.Cursor {
	if s.dismissable() {
		return desktop.PointerCursor
	}
	return desktop.DefaultCursor
}

func (s *ScriptFooterStatus) MouseIn(*desktop.MouseEvent) {
	s.hovered = true
	s.refreshHover()
}

func (s *ScriptFooterStatus) MouseOut() {
	s.hovered = false
	s.refreshHover()
}

func (s *ScriptFooterStatus) MouseMoved(*desktop.MouseEvent) {}

func (s *ScriptFooterStatus) idleLabelColor() color.Color {
	switch s.kind {
	case ScriptFooterError:
		return scriptFooterErrorColor
	case ScriptFooterDone:
		return design.ColorConnectionBadgeText
	default:
		return design.ColorConnectionAddFill
	}
}

func (s *ScriptFooterStatus) refreshHover() {
	if s.label == nil {
		return
	}
	if s.hovered && s.kind != ScriptFooterIdle {
		s.label.Color = design.ColorTextLight
	} else {
		s.label.Color = s.idleLabelColor()
	}
	s.label.Refresh()
}

func (s *ScriptFooterStatus) syncClose() {
	if s.closeBtn == nil {
		return
	}
	if s.dismissable() {
		s.closeBtn.Show()
	} else {
		s.closeBtn.Hide()
	}
	if s.box != nil {
		s.box.Refresh()
	}
}

func (s *ScriptFooterStatus) applyChildren() {
	if s.label == nil {
		return
	}
	switch s.kind {
	case ScriptFooterRunning:
		s.icon.Hide()
		s.label.Text = scriptFooterRunningText
		s.label.Show()
		s.spinner.Start()
	case ScriptFooterError:
		s.spinner.Stop()
		s.icon.Resource = scriptFooterErrorIcon
		s.icon.Show()
		s.label.Text = scriptFooterErrorText
		s.label.Show()
	case ScriptFooterDone:
		s.spinner.Stop()
		s.icon.Resource = scriptFooterDoneIcon
		s.icon.Show()
		s.label.Text = scriptFooterDoneText
		s.label.Show()
	default:
		s.spinner.Stop()
		s.icon.Hide()
		s.label.Hide()
	}
	s.icon.Refresh()
	s.refreshHover()
	s.syncClose()
}

func (s *ScriptFooterStatus) CreateRenderer() fyne.WidgetRenderer {
	if s.spinner != nil {
		s.spinner.Stop()
	}
	s.spinner = NewDeviceDashboardBusySpinner()
	s.icon = canvas.NewImageFromResource(scriptFooterDoneIcon)
	s.icon.FillMode = canvas.ImageFillContain
	s.icon.SetMinSize(fyne.NewSize(deviceDashboardBusySpinnerSize, deviceDashboardBusySpinnerSize))
	s.icon.Hide()
	s.label = canvas.NewText("", design.ColorConnectionAddFill)
	s.label.TextSize = 9
	s.closeBtn = newIconChromeButton(iconChromeButtonSpec{
		NormalFill:   color.Transparent,
		HoverFill:    color.Transparent,
		Stroke:       color.Transparent,
		CornerRadius: 3,
		NormalIcon:   scriptFooterCloseIcon,
		HoverIcon:    scriptFooterCloseHoverIcon,
		IconSize:     fyne.NewSize(10, 10),
		ButtonSize:   fyne.NewSize(deviceDashboardBusySpinnerSize, deviceDashboardBusySpinnerSize),
		OnTapped: func() {
			if s.onDismiss != nil {
				s.onDismiss()
			}
		},
	})
	s.closeBtn.Hide()
	s.box = container.New(&DeviceRowControlsLayout{Gap: 6}, s.spinner, s.icon, s.label, s.closeBtn)
	s.applyChildren()
	return widget.NewSimpleRenderer(s.box)
}
