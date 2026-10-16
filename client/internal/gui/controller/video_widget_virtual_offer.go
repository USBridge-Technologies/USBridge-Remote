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

// virtualOfferDefaultFPS is what the offer asks for when the saved config is
// at 60 fps or less: on a Windows host a physical monitor holds capture (and,
// while it stays on, the whole desktop) to its own refresh, so the virtual
// display is the way to the higher rate.
const virtualOfferDefaultFPS = 120

// virtualOfferDeclined holds the device paths the user chose to keep
// streaming as they are on this connection, so the offer doesn't come back
// on every apply. Cleared on each new host connection (UpdateClient).
var virtualOfferDeclined sync.Map

// connectVirtualOffer states: the offer shown once per host connection
// before the first stream start (holdForConnectVirtualOffer).
const (
	connectOfferUnchecked int32 = iota
	connectOfferPending
	connectOfferDone
)

// resetConnectVirtualOffer arms the on-connect offer again for a new host
// connection.
func (vw *VideoWidget) resetConnectVirtualOffer() {
	vw.connectVirtualOffer.Store(connectOfferUnchecked)
	virtualOfferDeclined.Clear()
}

// holdForConnectVirtualOffer runs on the first control bootstrap after a
// connect: when the host is Windows and a physical monitor is selected, it
// asks to stream a virtual display instead (by default the main and the only
// display -- the only way Windows gives a steady 120 fps at low GPU/CPU
// load) before anything starts. It returns true while that check or question
// is open; the answer starts the stream itself.
func (vw *VideoWidget) holdForConnectVirtualOffer() bool {
	switch vw.connectVirtualOffer.Load() {
	case connectOfferDone:
		return false
	case connectOfferPending:
		return true
	}
	if !vw.connectVirtualOffer.CompareAndSwap(connectOfferUnchecked, connectOfferPending) {
		return vw.connectVirtualOffer.Load() == connectOfferPending
	}
	go func() {
		startAsIs := func() {
			vw.connectVirtualOffer.Store(connectOfferDone)
			if vw.isClosing.Load() || vw.userStoppedVideo.Load() {
				return
			}
			vw.setDesiredStreaming(true)
			vw.scheduleVideoReconcile("control-bootstrap")
		}
		cfg, ok := vw.connectVirtualOfferConfig()
		if !ok {
			startAsIs()
			return
		}
		logrus.Infof("🖥️ Windows host streaming %s: offering a virtual display before the first start", cfg.DevicePath)
		fyne.Do(func() {
			vw.showVirtualDisplayOffer(cfg, func(next models.VideoDeviceConfig) {
				vw.connectVirtualOffer.Store(connectOfferDone)
				if vw.isClosing.Load() || vw.userStoppedVideo.Load() {
					return
				}
				if err := vw.applyVideoDeviceConfig(next, true); err != nil {
					logrus.Warnf("⚠️ cannot start video on %s: %v", next.DevicePath, err)
				}
			}, func() { go startAsIs() })
		})
	}()
	return true
}

// connectVirtualOfferConfig resolves the config the stream would start with
// and whether to offer the virtual display for it; never on the UI goroutine.
func (vw *VideoWidget) connectVirtualOfferConfig() (models.VideoDeviceConfig, bool) {
	client := vw.usbClient
	if client == nil || vw.parentWindow == nil {
		return models.VideoDeviceConfig{}, false
	}
	if strings.TrimSpace(vw.agentOS) == "" {
		// The OS arrives with the first HID check, which may not have run
		// yet this early after a connect.
		if info, err := client.GetDeviceInfo(); err == nil && info != nil {
			vw.SetAgentEnvironment(info.AgentOS, info.AgentDisplay)
		}
	}
	if !vw.isWindowsAgent() {
		return models.VideoDeviceConfig{}, false
	}
	cfg, err := localPreferredVideoConfig()
	if err != nil {
		if cfg, err = vw.resolvePreferredVideoConfig(); err != nil {
			return models.VideoDeviceConfig{}, false
		}
	}
	if strings.HasPrefix(cfg.DevicePath, "virtual:") {
		// Already on the virtual display: still asked on every connect, since
		// "only the virtual display" (the agent switches the other monitors
		// off for the session) is chosen here.
		info, err := getVideoInfoData(client)
		return cfg, err == nil && info != nil && info.VirtualDisplaySupported
	}
	return cfg, vw.shouldOfferVirtualDisplay(cfg)
}

// virtualSpecMode parses a "virtual:WxH@FPS" device path.
func virtualSpecMode(path string) (w, h, fps int, ok bool) {
	spec, found := strings.CutPrefix(path, "virtual:")
	if !found {
		return 0, 0, 0, false
	}
	if n, err := fmt.Sscanf(spec, "%dx%d@%d", &w, &h, &fps); err != nil || n != 3 || w <= 0 || h <= 0 || fps <= 0 {
		return 0, 0, 0, false
	}
	return w, h, fps, true
}

// applyOfferingVirtualDisplay hands cfg to apply, unless the host is Windows
// and a physical monitor is selected: then it first offers to stream a
// virtual display at the asked resolution instead, by default the main and
// the only display (while a 60 Hz monitor stays on, Windows holds the whole
// desktop at 60 Hz). apply runs off the UI goroutine, with cfg as is or
// pointed at the virtual display; closing the offer applies nothing.
func (vw *VideoWidget) applyOfferingVirtualDisplay(cfg models.VideoDeviceConfig, apply func(models.VideoDeviceConfig)) {
	go func() {
		if !vw.shouldOfferVirtualDisplay(cfg) {
			apply(cfg)
			return
		}
		fyne.Do(func() { vw.showVirtualDisplayOffer(cfg, apply, nil) })
	}()
}

// shouldOfferVirtualDisplay asks the agent; never on the UI goroutine.
func (vw *VideoWidget) shouldOfferVirtualDisplay(cfg models.VideoDeviceConfig) bool {
	if vw.usbClient == nil || vw.parentWindow == nil || !vw.isWindowsAgent() {
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

// showVirtualDisplayOffer asks; apply gets the chosen config off the UI
// goroutine. dismissed, when set, runs when the offer is closed with X;
// otherwise closing applies nothing.
func (vw *VideoWidget) showVirtualDisplayOffer(cfg models.VideoDeviceConfig, apply func(models.VideoDeviceConfig), dismissed func()) {
	width, height := cfg.VideoWidth, cfg.VideoHeight
	if width <= 0 || height <= 0 {
		width, height = 1920, 1080
	}
	fps := cfg.VideoFPS
	if fps <= 60 {
		fps = virtualOfferDefaultFPS
	}
	if w, h, f, ok := virtualSpecMode(cfg.DevicePath); ok {
		// The virtual display already streamed: its own mode, as it is.
		width, height, fps = w, h, f
	}

	primaryRow, primaryChecked := view.NewOptionToggleRow(i18n.Current.VirtualDisplayPrimary, "", i18n.Current.VirtualDisplayPrimaryHint, 340, true, nil)
	exclusiveRow, exclusiveChecked := view.NewOptionToggleRow(i18n.Current.VirtualDisplayExclusive, "", i18n.Current.VirtualDisplayExclusiveHint, 340, true, nil)
	body := container.NewVBox(primaryRow, exclusiveRow)

	var closePopup func()
	answered := false
	answer := func() {
		answered = true
		if closePopup != nil {
			closePopup()
		}
	}
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
			answer()
			virtualOfferDeclined.Store(cfg.DevicePath, true)
			if dismissed != nil {
				dismissed()
				return
			}
			go apply(cfg)
		},
	}
	skipBtn.ExtendBaseWidget(skipBtn)

	createBtn := newVirtualDisplayAddButton(func() {
		answer()
		primary, exclusive := primaryChecked(), exclusiveChecked()
		go func() {
			if _, err := vw.usbClient.AddVirtualDisplay(width, height, fps, primary, exclusive); err != nil {
				logrus.Errorf("Failed to add virtual display: %v", err)
				fyne.Do(func() {
					view.ShowErrorDialog(fmt.Errorf("Failed to add virtual display: %v", err), vw.parentWindow)
				})
				if dismissed != nil {
					dismissed()
				}
				return
			}
			spec := fmt.Sprintf("%dx%d@%d", width, height, fps)
			next := cfg
			next.DevicePath = "virtual:" + spec
			next.DeviceName = "Virtual Display (" + spec + ")"
			next.VideoWidth, next.VideoHeight, next.VideoFPS = width, height, fps
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
		onClose: func() {
			if !answered && dismissed != nil {
				answered = true
				dismissed()
			}
		},
	})
}

func virtualDisplayOfferPanelSize(canvasSize fyne.Size, panel fyne.CanvasObject) fyne.Size {
	s := virtualDisplayDialogPanelSize(canvasSize, panel)
	if w := minFloat32(420, canvasSize.Width-40); w > s.Width {
		s.Width = w
	}
	return s
}
