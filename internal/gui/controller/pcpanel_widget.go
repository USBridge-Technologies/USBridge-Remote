package controller

import (
	"fmt"
	"sync"
	"time"

	"usbridge-client/internal/api"
	"usbridge-client/internal/gui/assets"
	"usbridge-client/internal/gui/i18n"
	"usbridge-client/internal/gui/view"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/sirupsen/logrus"
)

const (
	pcpanelLedPollInterval = 5 * time.Second
	addressBarButtonSize   = 36 // Квадратные кнопки: ширина = высота = высоте строки
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

// PCPanelWidget кнопки Power/HDD LED в адресной строке
type PCPanelWidget struct {
	powerBtn  *widget.Button
	hddBtn    *widget.Button
	container *fyne.Container
	usbClient *api.USBClient
	stopPoll  chan struct{}
	pollMu    sync.Mutex
	powerOn   bool
	hddOn     bool
	window    fyne.Window
}

// NewPCPanelWidget создаёт виджет с кнопками Power и HDD LED
func NewPCPanelWidget(w fyne.Window) *PCPanelWidget {
	p := &PCPanelWidget{
		window: w,
	}
	p.powerBtn = widget.NewButton("", p.onPowerClick)
	p.powerBtn.Importance = widget.LowImportance
	p.powerBtn.SetIcon(assets.PowerOffIcon)
	p.hddBtn = widget.NewButton("", p.onHDDClick)
	p.hddBtn.Importance = widget.LowImportance
	p.hddBtn.SetIcon(assets.ResetIcon)

	// Задаём минимальный размер как квадрат (высота строки)
	p.powerBtn.Resize(fyne.NewSize(addressBarButtonSize, addressBarButtonSize))
	p.hddBtn.Resize(fyne.NewSize(addressBarButtonSize, addressBarButtonSize))

	p.container = container.NewHBox(p.powerBtn, p.hddBtn)
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

// updateLEDIcons обновляет иконки кнопок по состоянию LEDs (светодиодов)
func (p *PCPanelWidget) updateLEDIcons(powerOn, hddOn bool) {
	p.powerOn = powerOn
	p.hddOn = hddOn

	// Power LED: ● = горит, ○ = погашен
	if powerOn {
		p.powerBtn.SetIcon(assets.PowerOffIconActive)
		p.powerBtn.SetText("")
		p.powerBtn.Importance = widget.HighImportance
	} else {
		p.powerBtn.SetIcon(assets.PowerOffIcon)
		p.powerBtn.SetText("")
		p.powerBtn.Importance = widget.LowImportance
	}

	// HDD LED: ● = горит, ○ = погашен
	if hddOn {
		p.hddBtn.SetIcon(assets.ResetIconActive)
		p.hddBtn.SetText("")
		p.hddBtn.Importance = widget.HighImportance
	} else {
		p.hddBtn.SetIcon(assets.ResetIcon)
		p.hddBtn.SetText("")
		p.hddBtn.Importance = widget.LowImportance
	}

	p.powerBtn.Refresh()
	p.hddBtn.Refresh()
}

func (p *PCPanelWidget) onPowerClick() {
	if p.usbClient == nil {
		return
	}
	p.showPowerDialog()
}

func (p *PCPanelWidget) onHDDClick() {
	if p.usbClient == nil {
		return
	}
	p.showResetDialog()
}

func (p *PCPanelWidget) showPowerDialog() {
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
