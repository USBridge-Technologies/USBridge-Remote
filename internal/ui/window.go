package ui

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"

	"usbridge_agent/internal/config"
	"usbridge_agent/internal/permissions"
	"usbridge_agent/internal/ui/design"
)

type Window struct {
	app   fyne.App
	cfg   config.Config
	perms interface {
		AccessibilityGranted() bool
		ScreenRecordingGranted() bool
		RequestAccessibility() bool
		RequestScreenRecording() bool
		OpenPrivacySettings() error
	}
}

func NewWindow(app fyne.App, cfg config.Config, perms *permissions.Service) *Window {
	return &Window{app: app, cfg: cfg, perms: perms}
}

func (w *Window) ShowAndRun(onClose func()) {
	win := w.app.NewWindow("USBridge Agent")
	win.SetPadded(false)
	win.Resize(fyne.NewSize(620, 420))
	win.CenterOnScreen()

	title := canvas.NewText("USBridge Agent", design.ColorTextLight)
	title.TextSize = 22
	title.TextStyle.Bold = true

	subtitle := canvas.NewText("Compact desktop backend for usbridge_client", design.ColorTextMuted)
	subtitle.TextSize = 12

	header := container.NewBorder(nil, nil, nil, newBadge("AGENT", fyne.NewSize(78, 30)), container.NewVBox(title, subtitle))

	httpCard := newStatCard("HTTP", fmt.Sprintf("%s:%d", w.cfg.ListenHost, w.cfg.HTTPPort), "API")
	videoCard := newStatCard("VIDEO", fmt.Sprintf("127.0.0.1:%d", w.cfg.VideoUDPPort), "RTP")
	captureCard := newStatCard("CAPTURE", w.cfg.VideoCapture, strings.ToUpper(runtime.GOOS))

	accessStatus := widget.NewLabel("")
	screenStatus := widget.NewLabel("")

	accessPanel := newPermissionPanel(
		"Accessibility",
		"Needed for keyboard and mouse injection.",
		accessStatus,
		func() {
			if w.perms != nil {
				_ = w.perms.RequestAccessibility()
				refreshPermissionLabels(accessStatus, screenStatus, w.perms)
			}
		},
	)
	screenPanel := newPermissionPanel(
		"Screen Recording",
		"Needed for screen capture and video streaming.",
		screenStatus,
		func() {
			if w.perms != nil {
				_ = w.perms.RequestScreenRecording()
				refreshPermissionLabels(accessStatus, screenStatus, w.perms)
			}
		},
	)

	openSettingsBtn := widget.NewButton("Open Privacy Settings", func() {
		if w.perms != nil {
			_ = w.perms.OpenPrivacySettings()
			refreshPermissionLabels(accessStatus, screenStatus, w.perms)
		}
	})
	closeBtn := widget.NewButton("Close", func() { win.Close() })
	closeBtn.Importance = widget.HighImportance

	infoText := widget.NewRichTextFromMarkdown(strings.TrimSpace(
		"`usbridge_agent` stays compatible with `usbridge_client` and exposes the same control API surface.",
	))
	infoText.Wrapping = fyne.TextWrapWord
	executablePath := "unknown"
	if path, err := os.Executable(); err == nil && strings.TrimSpace(path) != "" {
		executablePath = path
	}
	execText := widget.NewRichTextFromMarkdown(fmt.Sprintf("Executable: `%s`", executablePath))
	execText.Wrapping = fyne.TextWrapWord

	content := container.NewVBox(
		newPanel("", header),
		container.NewGridWithColumns(3, httpCard, videoCard, captureCard),
		container.NewGridWithColumns(2, accessPanel, screenPanel),
		newPanel("Compatibility", infoText),
		newPanel("Runtime", execText),
		container.NewHBox(openSettingsBtn, layout.NewSpacer(), closeBtn),
	)

	refreshPermissionLabels(accessStatus, screenStatus, w.perms)
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			if win.Canvas() == nil {
				return
			}
			fyne.Do(func() {
				refreshPermissionLabels(accessStatus, screenStatus, w.perms)
			})
		}
	}()

	bg := canvas.NewRectangle(design.ColorPanel)
	win.SetContent(container.NewStack(bg, container.NewPadded(content)))
	win.SetCloseIntercept(func() {
		if onClose != nil {
			onClose()
		}
		win.Close()
	})
	win.Show()
	w.app.Run()
}

func newPanel(title string, content fyne.CanvasObject) fyne.CanvasObject {
	bg := canvas.NewRectangle(design.ColorSurface)
	bg.CornerRadius = 12
	bg.StrokeColor = design.ColorBorder
	bg.StrokeWidth = 1

	body := container.NewVBox(content)
	if strings.TrimSpace(title) != "" {
		titleText := canvas.NewText(strings.ToUpper(title), design.ColorTextMuted)
		titleText.TextSize = 11
		titleText.TextStyle.Bold = true
		body = container.NewVBox(titleText, widget.NewSeparator(), content)
	}
	return container.NewStack(bg, container.NewPadded(body))
}

func newStatCard(label, value, hint string) fyne.CanvasObject {
	bg := canvas.NewRectangle(design.ColorSurface)
	bg.CornerRadius = 12
	bg.StrokeColor = design.ColorBorder
	bg.StrokeWidth = 1

	labelText := canvas.NewText(strings.ToUpper(label), design.ColorTextMuted)
	labelText.TextSize = 10
	labelText.TextStyle.Bold = true

	valueText := canvas.NewText(value, design.ColorTextLight)
	valueText.TextSize = 15
	valueText.TextStyle.Bold = true

	hintText := canvas.NewText(hint, design.ColorTextMuted)
	hintText.TextSize = 10

	body := container.NewVBox(labelText, valueText, hintText)
	return container.NewStack(bg, container.NewPadded(body))
}

func newBadge(text string, size fyne.Size) fyne.CanvasObject {
	bg := canvas.NewRectangle(design.ColorAccent)
	bg.CornerRadius = design.RadiusMD

	label := canvas.NewText(text, design.ColorBackground)
	label.TextStyle.Bold = true
	label.TextSize = 14

	return container.NewGridWrap(size, container.NewStack(bg, container.NewCenter(label)))
}

func newPermissionPanel(title, description string, status *widget.Label, onRequest func()) fyne.CanvasObject {
	desc := widget.NewLabel(description)
	desc.Wrapping = fyne.TextWrapWord

	requestBtn := widget.NewButton("Request", onRequest)
	requestBtn.Importance = widget.HighImportance

	return newPanel(title, container.NewVBox(
		status,
		desc,
		requestBtn,
	))
}

func refreshPermissionLabels(accessLabel, screenLabel *widget.Label, perms interface {
	AccessibilityGranted() bool
	ScreenRecordingGranted() bool
}) {
	if accessLabel != nil {
		if perms != nil && perms.AccessibilityGranted() {
			accessLabel.SetText("Status: granted")
		} else {
			accessLabel.SetText("Status: required")
		}
	}
	if screenLabel != nil {
		if perms != nil && perms.ScreenRecordingGranted() {
			screenLabel.SetText("Status: granted")
		} else {
			screenLabel.SetText("Status: required")
		}
	}
}
