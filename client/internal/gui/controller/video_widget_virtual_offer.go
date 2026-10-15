package controller

import (
	"fmt"
	"image/color"
	"strings"
	"sync"

	"usbridge-client/internal/gui/design"
	"usbridge-client/internal/gui/i18n"
	"usbridge-client/internal/gui/view"
	"usbridge-client/internal/models"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"github.com/sirupsen/logrus"
)

// virtualOfferMinFPS: above this a Windows host's physical monitor is the
// bottleneck -- Desktop Duplication only gets a new frame per refresh of the
// captured monitor (60 Hz on most), and DWM composes every attached screen.
const virtualOfferMinFPS = 60

// virtualOfferDeclined holds the device paths the user chose to keep
// streaming as they are this run, so the offer doesn't come back on every
// apply.
var virtualOfferDeclined sync.Map

// applyOfferingVirtualDisplay hands cfg to apply, unless the host is Windows,
// more than 60 fps is asked for and a physical monitor is selected: then it
// first offers to stream a virtual display at the asked resolution and rate
// instead, by default the main and the only display (while a 60 Hz monitor
// stays on, Windows holds the whole desktop at 60 Hz). apply runs off the UI
// goroutine, with cfg as is or pointed at the virtual display; closing the
// offer applies nothing.
func (vw *VideoWidget) applyOfferingVirtualDisplay(cfg models.VideoDeviceConfig, apply func(models.VideoDeviceConfig)) {
	go func() {
		if !vw.shouldOfferVirtualDisplay(cfg) {
			apply(cfg)
			return
		}
		fyne.Do(func() { vw.showVirtualDisplayOffer(cfg, apply) })
	}()
}

// shouldOfferVirtualDisplay asks the agent; never on the UI goroutine.
func (vw *VideoWidget) shouldOfferVirtualDisplay(cfg models.VideoDeviceConfig) bool {
	if vw.usbClient == nil || vw.parentWindow == nil || !vw.isWindowsAgent() || cfg.VideoFPS <= virtualOfferMinFPS {
		return false
	}
	if cfg.DevicePath == "" || strings.HasPrefix(cfg.DevicePath, "virtual:") {
		return false
	}
	if _, declined := virtualOfferDeclined.Load(cfg.DevicePath); declined {
		return false
	}
	info, err := getVideoInfoData(vw.usbClient)
	if err != nil || info == nil || !info.VirtualDisplaySupported {
		return false
	}
	devices, err := vw.GetAvailableVideoDevices()
	if err != nil {
		return false
	}
	for _, d := range devices {
		if d.Path == cfg.DevicePath {
			// The virtual monitor while it is up is also listed as a plain
			// display ("VDD by MTT").
			return d.Bus != "virtual" && !strings.Contains(strings.ToUpper(d.Name), "VDD")
		}
	}
	return true
}

func (vw *VideoWidget) showVirtualDisplayOffer(cfg models.VideoDeviceConfig, apply func(models.VideoDeviceConfig)) {
	width, height := cfg.VideoWidth, cfg.VideoHeight
	if width <= 0 || height <= 0 {
		width, height = 1920, 1080
	}
	fps := cfg.VideoFPS

	primaryRow, primaryChecked := view.NewOptionToggleRow(i18n.Current.VirtualDisplayPrimary, "", i18n.Current.VirtualDisplayPrimaryHint, 340, true, nil)
	exclusiveRow, exclusiveChecked := view.NewOptionToggleRow(i18n.Current.VirtualDisplayExclusive, "", i18n.Current.VirtualDisplayExclusiveHint, 340, true, nil)
	body := container.NewVBox(primaryRow, exclusiveRow)

	var closePopup func()
	skipBtn := &connectionDialogSecondaryButton{
		labelText:      i18n.Current.VirtualDisplayOfferSkip,
		compact:        true,
		short:          true,
		fillColor:      color.Transparent,
		borderColor:    color.Transparent,
		textColor:      color.NRGBA{R: 0x8f, G: 0x93, B: 0x81, A: 0xff},
		hoverFillColor: color.Transparent,
		hoverTextColor: design.ColorTextLight,
		onTapped: func() {
			if closePopup != nil {
				closePopup()
			}
			virtualOfferDeclined.Store(cfg.DevicePath, true)
			go apply(cfg)
		},
	}
	skipBtn.ExtendBaseWidget(skipBtn)

	createBtn := newVirtualDisplayAddButton(func() {
		if closePopup != nil {
			closePopup()
		}
		primary, exclusive := primaryChecked(), exclusiveChecked()
		go func() {
			if _, err := vw.usbClient.AddVirtualDisplay(width, height, fps, primary, exclusive); err != nil {
				logrus.Errorf("Failed to add virtual display: %v", err)
				fyne.Do(func() {
					view.ShowErrorDialog(fmt.Errorf("Failed to add virtual display: %v", err), vw.parentWindow)
				})
				return
			}
			spec := fmt.Sprintf("%dx%d@%d", width, height, fps)
			next := cfg
			next.DevicePath = "virtual:" + spec
			next.DeviceName = "Virtual Display (" + spec + ")"
			logrus.Infof("🖥️ streaming a virtual display %s instead of %s (primary=%v exclusive=%v)", spec, cfg.DevicePath, primary, exclusive)
			apply(next)
		}()
	})
	createBtn.labelText = i18n.Current.VirtualDisplayOfferCreate

	_, closePopup = showBrandedOverlayDialog(brandedOverlayDialogSpec{
		parent:          vw.parentWindow,
		title:           fmt.Sprintf(i18n.Current.VirtualDisplayOfferTitle, fps),
		subtitle:        fmt.Sprintf(i18n.Current.VirtualDisplayOfferSubtitle, fps, width, height),
		body:            body,
		rightButtons:    []fyne.CanvasObject{skipBtn, createBtn},
		compactFooter:   true,
		hideCancel:      true,
		headerPadBottom: 6,
		footerPadTop:    6,
		footerPadBottom: 8,
		panelSize:       virtualDisplayOfferPanelSize,
	})
}

func virtualDisplayOfferPanelSize(canvasSize fyne.Size, panel fyne.CanvasObject) fyne.Size {
	s := virtualDisplayDialogPanelSize(canvasSize, panel)
	if w := minFloat32(420, canvasSize.Width-40); w > s.Width {
		s.Width = w
	}
	return s
}
