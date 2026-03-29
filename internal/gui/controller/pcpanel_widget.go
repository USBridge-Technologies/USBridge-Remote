package controller

import (
	"fmt"
	"image/color"
	"strings"
	"sync"
	"time"

	"usbridge-client/internal/api"
	"usbridge-client/internal/gui/assets"
	"usbridge-client/internal/gui/design"
	"usbridge-client/internal/gui/i18n"
	"usbridge-client/internal/gui/view"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/sirupsen/logrus"
)

const (
	pcpanelLedPollInterval = 5 * time.Second
	addressBarButtonSize   = 36 // Квадратные кнопки: ширина = высота = высоте строки
)

var (
	pcpanelIndicatorIdle  = color.NRGBA{R: 0x16, G: 0x16, B: 0x16, A: 0x38}
	pcpanelIndicatorAlert = design.ColorProtocolQUIC
)

// pcpanelFixedWidthLayout фиксирует ширину контента (min=max), чтобы диалог не сужался и не растягивался
type pcpanelFixedWidthLayout struct {
	width float32
}

func (l *pcpanelFixedWidthLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	for _, o := range objects {
		o.Resize(size)
		o.Move(fyne.NewPos(0, 0))
	}
}

func (l *pcpanelFixedWidthLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	min := fyne.NewSize(0, 0)
	for _, o := range objects {
		childMin := o.MinSize()
		if childMin.Height > min.Height {
			min.Height = childMin.Height
		}
	}
	if l.width > 0 {
		min.Width = l.width
	}
	return min
}

type pcpanelActionButton struct {
	widget.BaseWidget

	onTapped   func()
	hovered    bool
	blinking   bool
	blinkPhase bool

	bg        *canvas.Rectangle
	indicator *canvas.Circle
	icon      *canvas.Image

	blinkMu   sync.Mutex
	blinkStop chan struct{}
}

func newPCPanelActionButton(onTapped func()) *pcpanelActionButton {
	btn := &pcpanelActionButton{onTapped: onTapped}
	btn.ExtendBaseWidget(btn)
	return btn
}

func (b *pcpanelActionButton) CreateRenderer() fyne.WidgetRenderer {
	b.bg = canvas.NewRectangle(design.ColorAccent)
	b.bg.CornerRadius = design.RadiusMD

	b.indicator = canvas.NewCircle(pcpanelIndicatorIdle)
	b.icon = canvas.NewImageFromResource(assets.PowerOffIconActive)
	b.icon.FillMode = canvas.ImageFillContain

	r := &pcpanelActionButtonRenderer{
		button:  b,
		objects: []fyne.CanvasObject{b.bg, b.indicator, b.icon},
	}
	r.Refresh()
	return r
}

func (b *pcpanelActionButton) MinSize() fyne.Size {
	return fyne.NewSize(addressBarButtonSize, addressBarButtonSize)
}

func (b *pcpanelActionButton) Tapped(*fyne.PointEvent) {
	if b.onTapped != nil {
		b.onTapped()
	}
}

func (b *pcpanelActionButton) TappedSecondary(*fyne.PointEvent) {}

func (b *pcpanelActionButton) MouseIn(*desktop.MouseEvent) {
	b.hovered = true
	b.Refresh()
}

func (b *pcpanelActionButton) MouseMoved(*desktop.MouseEvent) {}

func (b *pcpanelActionButton) MouseOut() {
	b.hovered = false
	b.Refresh()
}

func (b *pcpanelActionButton) SetBlinking(blinking bool) {
	b.blinkMu.Lock()
	if b.blinking == blinking {
		b.blinkMu.Unlock()
		return
	}
	b.blinking = blinking

	stop := b.blinkStop
	b.blinkStop = nil
	b.blinkPhase = false
	b.blinkMu.Unlock()

	if stop != nil {
		close(stop)
	}

	if !blinking {
		fyne.Do(func() {
			b.Refresh()
		})
		return
	}

	newStop := make(chan struct{})
	b.blinkMu.Lock()
	b.blinkStop = newStop
	b.blinkMu.Unlock()

	go func(stop <-chan struct{}) {
		ticker := time.NewTicker(450 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				fyne.Do(func() {
					b.blinkMu.Lock()
					if b.blinkStop != stop {
						b.blinkMu.Unlock()
						return
					}
					b.blinkPhase = !b.blinkPhase
					b.blinkMu.Unlock()
					b.Refresh()
				})
			}
		}
	}(newStop)

	b.Refresh()
}

func (b *pcpanelActionButton) refreshVisuals() {
	if b.bg == nil || b.indicator == nil || b.icon == nil {
		return
	}

	fill := design.ColorAccent
	if b.hovered {
		fill = design.ColorAccentHover
	}

	indicatorFill := pcpanelIndicatorIdle
	b.blinkMu.Lock()
	if b.blinking && b.blinkPhase {
		indicatorFill = pcpanelIndicatorAlert
	}
	b.blinkMu.Unlock()

	b.bg.FillColor = fill
	b.indicator.FillColor = indicatorFill
	b.icon.Resource = assets.PowerOffIconActive

	b.bg.Refresh()
	b.indicator.Refresh()
	b.icon.Refresh()
}

type pcpanelActionButtonRenderer struct {
	button  *pcpanelActionButton
	objects []fyne.CanvasObject
}

func (r *pcpanelActionButtonRenderer) Layout(size fyne.Size) {
	r.button.bg.Resize(size)

	indicatorSize := fyne.NewSize(24, 24)
	r.button.indicator.Resize(indicatorSize)
	r.button.indicator.Move(fyne.NewPos((size.Width-indicatorSize.Width)/2, (size.Height-indicatorSize.Height)/2))

	iconSize := fyne.NewSize(18, 18)
	r.button.icon.Resize(iconSize)
	r.button.icon.Move(fyne.NewPos((size.Width-iconSize.Width)/2, (size.Height-iconSize.Height)/2))
}

func (r *pcpanelActionButtonRenderer) MinSize() fyne.Size {
	return r.button.MinSize()
}

func (r *pcpanelActionButtonRenderer) Refresh() {
	r.button.refreshVisuals()
	r.Layout(r.button.Size())
	canvas.Refresh(r.button)
}

func (r *pcpanelActionButtonRenderer) BackgroundColor() color.Color {
	return color.Transparent
}

func (r *pcpanelActionButtonRenderer) Objects() []fyne.CanvasObject {
	return r.objects
}

func (r *pcpanelActionButtonRenderer) Destroy() {}

// PCPanelWidget кнопка питания с индикатором активности в адресной строке
type PCPanelWidget struct {
	actionBtn *pcpanelActionButton
	container *fyne.Container
	usbClient *api.USBClient
	stopPoll  chan struct{}
	pollMu    sync.Mutex
	powerOn   bool
	hddOn     bool
	window    fyne.Window
}

// NewPCPanelWidget создаёт виджет с объединённой кнопкой Power/Reset.
func NewPCPanelWidget(w fyne.Window) *PCPanelWidget {
	p := &PCPanelWidget{
		window: w,
	}
	p.actionBtn = newPCPanelActionButton(p.onActionClick)
	p.container = container.NewHBox(p.actionBtn)
	p.container.Hide()
	return p
}

// GetContainer возвращает контейнер для размещения в адресной строке
func (p *PCPanelWidget) GetContainer() *fyne.Container {
	return p.container
}

// SetClient устанавливает USB клиент и запускает опрос LEDs
func (p *PCPanelWidget) SetClient(c *api.USBClient) {
	p.pollMu.Lock()
	if p.stopPoll != nil {
		close(p.stopPoll)
		p.stopPoll = nil
	}
	p.usbClient = c
	p.pollMu.Unlock()

	if c == nil {
		p.container.Hide()
		p.updateLEDIcons(false, false)
		return
	}

	p.container.Show()
	p.updateLEDIcons(false, false)
	p.pollLeds()
	// Первый опрос сразу
	go func() {
		resp, err := c.GetPCPanelLeds()
		if err != nil {
			logrus.Debugf("PCPanel LEDs initial poll: %v", err)
			return
		}
		fyne.Do(func() {
			p.updateLEDIcons(resp.Data.Power, resp.Data.HDD)
		})
	}()
}

// pollLeds периодически опрашивает состояние LEDs
func (p *PCPanelWidget) pollLeds() {
	p.pollMu.Lock()
	if p.stopPoll != nil {
		close(p.stopPoll)
	}
	p.stopPoll = make(chan struct{})
	stop := p.stopPoll
	p.pollMu.Unlock()

	go func() {
		ticker := time.NewTicker(pcpanelLedPollInterval)
		defer ticker.Stop()

		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				p.pollMu.Lock()
				c := p.usbClient
				p.pollMu.Unlock()
				if c == nil {
					return
				}
				resp, err := c.GetPCPanelLeds()
				if err != nil {
					logrus.Debugf("PCPanel LEDs poll error: %v", err)
					continue
				}
				fyne.Do(func() {
					p.updateLEDIcons(resp.Data.Power, resp.Data.HDD)
				})
			}
		}
	}()
}

// updateLEDIcons обновляет индикаторы по состоянию LEDs целевой машины.
func (p *PCPanelWidget) updateLEDIcons(powerOn, hddOn bool) {
	p.powerOn = powerOn
	p.hddOn = hddOn

	if p.actionBtn != nil {
		p.actionBtn.SetBlinking(hddOn)
		p.actionBtn.Refresh()
	}
}

func (p *PCPanelWidget) onActionClick() {
	if p.usbClient == nil {
		return
	}
	p.showPowerActionDialog()
}

func (p *PCPanelWidget) showPowerActionDialog() {
	if p.window == nil {
		return
	}

	actionTitles := map[string]string{
		"power": "Power Off",
		"reset": "Reset",
	}
	actionHints := map[string]string{
		"power": `Type "power" to confirm shutdown.`,
		"reset": `Type "reset" to confirm reboot.`,
	}

	titleLabel := widget.NewLabel("Choose action")
	titleLabel.Wrapping = fyne.TextWrapWord

	hintLabel := widget.NewLabel("")
	hintLabel.Wrapping = fyne.TextWrapWord

	helpLabel := widget.NewLabel("English only. Confirmation is case-insensitive.")
	helpLabel.Wrapping = fyne.TextWrapWord

	actionGroup := widget.NewRadioGroup([]string{"Power Off", "Reset"}, nil)
	actionGroup.Horizontal = true

	confirmEntry := widget.NewEntry()

	powerInfoLabel := widget.NewLabel(i18n.Current.PCPanelPowerConfirm)
	powerInfoLabel.Wrapping = fyne.TextWrapWord

	holdSlider := widget.NewSlider(0, 10)
	holdSlider.Step = 1
	holdSlider.Value = 0

	holdLabel := widget.NewLabel(i18n.Current.PCPanelPowerShortPress)
	holdLabel.Wrapping = fyne.TextWrapWord
	holdSlider.OnChanged = func(v float64) {
		if v <= 0 {
			holdLabel.SetText(i18n.Current.PCPanelPowerShortPress)
			return
		}
		holdLabel.SetText(fmt.Sprintf(i18n.Current.PCPanelPowerLongPress, int(v)) + " - " + i18n.Current.PCPanelLongPressNotSupported)
	}

	powerOptions := container.NewVBox(
		powerInfoLabel,
		widget.NewLabel(i18n.Current.PCPanelPowerHoldTime),
		holdSlider,
		holdLabel,
	)

	resetInfoLabel := widget.NewLabel(i18n.Current.PCPanelResetConfirm)
	resetInfoLabel.Wrapping = fyne.TextWrapWord

	detailsContainer := container.NewVBox()

	var currentAction string
	updateDetails := func(action string) {
		currentAction = action
		hintLabel.SetText(actionHints[action])
		confirmEntry.SetText("")
		confirmEntry.SetPlaceHolder(action)

		if action == "power" {
			detailsContainer.Objects = []fyne.CanvasObject{powerOptions}
		} else {
			detailsContainer.Objects = []fyne.CanvasObject{resetInfoLabel}
		}
		detailsContainer.Refresh()
	}

	contentItems := []fyne.CanvasObject{
		titleLabel,
		actionGroup,
		hintLabel,
		helpLabel,
		widget.NewSeparator(),
		detailsContainer,
		widget.NewSeparator(),
		confirmEntry,
	}
	inner := container.NewVBox(contentItems...)

	var minW float32 = 380
	if fyne.CurrentDevice().IsMobile() {
		sz := p.window.Canvas().Size()
		minW = sz.Width * 0.85
		if minW < 300 {
			minW = 300
		}
	}

	content := container.New(&pcpanelFixedWidthLayout{width: minW}, inner)

	var d dialog.Dialog
	yesBtn := widget.NewButton(i18n.Current.Yes, func() {
		client := p.usbClient
		if client != nil {
			var err error
			switch currentAction {
			case "power":
				err = client.PressPCPanelButton("power", int(holdSlider.Value))
			case "reset":
				err = client.PressPCPanelButton("reset", 0)
			}
			if err != nil {
				logrus.Errorf("PCPanel %s error: %v", actionTitles[currentAction], err)
				dialog.ShowError(err, p.window)
				return
			}
		}
		if d != nil {
			d.Hide()
		}
	})
	yesBtn.Importance = widget.DangerImportance
	yesBtn.SetIcon(theme.ConfirmIcon())
	yesBtn.Disable()

	noBtn := widget.NewButton(i18n.Current.No, func() {
		if d != nil {
			d.Hide()
		}
	})
	noBtn.SetIcon(theme.CancelIcon())

	confirmEntry.OnChanged = func(value string) {
		if strings.EqualFold(strings.TrimSpace(value), currentAction) {
			yesBtn.Enable()
			return
		}
		yesBtn.Disable()
	}

	actionGroup.OnChanged = func(value string) {
		switch value {
		case "Power Off":
			updateDetails("power")
		case "Reset":
			updateDetails("reset")
		}
		yesBtn.Disable()
	}

	buttons := container.NewGridWithColumns(2, yesBtn, noBtn)
	inner.Objects = append(inner.Objects, buttons)

	updateDetails("power")
	actionGroup.SetSelected("Power Off")

	d = dialog.NewCustomWithoutButtons("Power", content, p.window)
	d.Show()
}

func (p *PCPanelWidget) showPowerDialog() {
	{
		label := widget.NewLabel(i18n.Current.PCPanelPowerConfirm)
		label.Wrapping = fyne.TextWrapWord

		holdSlider := widget.NewSlider(0, 10)
		holdSlider.Step = 1
		holdSlider.Value = 0

		holdLabel := widget.NewLabel(i18n.Current.PCPanelPowerShortPress)
		holdLabel.Wrapping = fyne.TextWrapWord
		holdSlider.OnChanged = func(v float64) {
			if v <= 0 {
				holdLabel.SetText(i18n.Current.PCPanelPowerShortPress)
				return
			}
			holdLabel.SetText(fmt.Sprintf(i18n.Current.PCPanelPowerLongPress, int(v)) + " - " + i18n.Current.PCPanelLongPressNotSupported)
		}

		extra := container.NewVBox(
			label,
			widget.NewLabel(i18n.Current.PCPanelPowerHoldTime),
			holdSlider,
			holdLabel,
		)

		p.showProtectedActionDialog(
			i18n.Current.PCPanelPowerTitle,
			`Type "power" to confirm shutdown.`,
			"power",
			extra,
			func() {
				client := p.usbClient
				if client != nil {
					if err := client.PressPCPanelButton("power", int(holdSlider.Value)); err != nil {
						logrus.Errorf("PCPanel Power error: %v", err)
						dialog.ShowError(err, p.window)
					}
				}
			},
		)
		return
	}

	label := widget.NewLabel(i18n.Current.PCPanelPowerConfirm)
	label.Wrapping = fyne.TextWrapWord

	// Ползунок: 0 = короткое нажатие, 1–10 = длительность зажатия в секундах
	holdSlider := widget.NewSlider(0, 10)
	holdSlider.Step = 1
	holdSlider.Value = 0
	holdLabel := widget.NewLabel(i18n.Current.PCPanelPowerShortPress)
	holdLabel.Wrapping = fyne.TextWrapWord
	holdSlider.OnChanged = func(v float64) {
		if v <= 0 {
			holdLabel.SetText(i18n.Current.PCPanelPowerShortPress)
		} else {
			holdLabel.SetText(fmt.Sprintf(i18n.Current.PCPanelPowerLongPress, int(v)) + " — " + i18n.Current.PCPanelLongPressNotSupported)
		}
	}

	form := container.NewVBox(
		label,
		widget.NewLabel(i18n.Current.PCPanelPowerHoldTime),
		holdSlider,
		holdLabel,
	)

	// Фиксированная ширина диалога — не узко на short press, не растягивается при движении ползунка
	var minW float32 = 360
	if p.window != nil && fyne.CurrentDevice().IsMobile() {
		sz := p.window.Canvas().Size()
		minW = sz.Width * 0.85
		if minW < 280 {
			minW = 280
		}
	}
	inner := container.NewVBox(form, widget.NewSeparator())
	content := container.New(&pcpanelFixedWidthLayout{width: minW}, inner)

	var d dialog.Dialog
	yesBtn := widget.NewButton(i18n.Current.Yes, func() {
		durationSec := int(holdSlider.Value)
		client := p.usbClient
		if client != nil {
			if err := client.PressPCPanelButton("power", durationSec); err != nil {
				logrus.Errorf("PCPanel Power error: %v", err)
				dialog.ShowError(err, p.window)
			}
		}
		if d != nil {
			d.Hide()
		}
	})
	yesBtn.Importance = widget.DangerImportance
	yesBtn.SetIcon(theme.ConfirmIcon())

	noBtn := widget.NewButton(i18n.Current.No, func() {
		if d != nil {
			d.Hide()
		}
	})
	noBtn.SetIcon(theme.CancelIcon())

	buttons := container.NewGridWithColumns(2, yesBtn, noBtn)
	inner.Objects = append(inner.Objects, buttons)

	d = dialog.NewCustomWithoutButtons(i18n.Current.PCPanelPowerTitle, content, p.window)
	d.Show()
}

func (p *PCPanelWidget) showResetDialog() {
	{
		label := widget.NewLabel(i18n.Current.PCPanelResetConfirm)
		label.Wrapping = fyne.TextWrapWord

		p.showProtectedActionDialog(
			i18n.Current.PCPanelResetTitle,
			`Type "reset" to confirm reboot.`,
			"reset",
			label,
			func() {
				client := p.usbClient
				if client != nil {
					if err := client.PressPCPanelButton("reset", 0); err != nil {
						logrus.Errorf("PCPanel Reset error: %v", err)
						dialog.ShowError(err, p.window)
					}
				}
			},
		)
		return
	}

	view.ShowConfirmYesLeftDanger(
		i18n.Current.PCPanelResetTitle,
		i18n.Current.PCPanelResetConfirm,
		func(ok bool) {
			if !ok {
				return
			}
			client := p.usbClient
			if client != nil {
				if err := client.PressPCPanelButton("reset", 0); err != nil {
					logrus.Errorf("PCPanel Reset error: %v", err)
					dialog.ShowError(err, p.window)
				}
			}
		},
		p.window,
	)
}

func (p *PCPanelWidget) showProtectedActionDialog(title, hint, expectedWord string, extra fyne.CanvasObject, onConfirm func()) {
	if p.window == nil {
		return
	}

	expectedWord = strings.TrimSpace(strings.ToLower(expectedWord))

	titleLabel := widget.NewLabel(title)
	titleLabel.Wrapping = fyne.TextWrapWord

	hintLabel := widget.NewLabel(hint)
	hintLabel.Wrapping = fyne.TextWrapWord

	helpLabel := widget.NewLabel("English only. Confirmation is case-insensitive.")
	helpLabel.Wrapping = fyne.TextWrapWord

	confirmEntry := widget.NewEntry()
	confirmEntry.SetPlaceHolder(expectedWord)

	contentItems := []fyne.CanvasObject{titleLabel, hintLabel, helpLabel}
	if extra != nil {
		contentItems = append(contentItems, widget.NewSeparator(), extra)
	}
	contentItems = append(contentItems, widget.NewSeparator(), confirmEntry)

	inner := container.NewVBox(contentItems...)

	var minW float32 = 360
	if fyne.CurrentDevice().IsMobile() {
		sz := p.window.Canvas().Size()
		minW = sz.Width * 0.85
		if minW < 280 {
			minW = 280
		}
	}

	content := container.New(&pcpanelFixedWidthLayout{width: minW}, inner)

	var d dialog.Dialog
	yesBtn := widget.NewButton(i18n.Current.Yes, func() {
		if onConfirm != nil {
			onConfirm()
		}
		if d != nil {
			d.Hide()
		}
	})
	yesBtn.Importance = widget.DangerImportance
	yesBtn.SetIcon(theme.ConfirmIcon())
	yesBtn.Disable()

	noBtn := widget.NewButton(i18n.Current.No, func() {
		if d != nil {
			d.Hide()
		}
	})
	noBtn.SetIcon(theme.CancelIcon())

	confirmEntry.OnChanged = func(value string) {
		if strings.EqualFold(strings.TrimSpace(value), expectedWord) {
			yesBtn.Enable()
			return
		}
		yesBtn.Disable()
	}

	buttons := container.NewGridWithColumns(2, yesBtn, noBtn)
	inner.Objects = append(inner.Objects, buttons)

	d = dialog.NewCustomWithoutButtons(title, content, p.window)
	d.Show()
}
