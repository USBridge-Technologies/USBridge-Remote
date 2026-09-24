package gui

import (
	"image/color"
	"strings"
	"time"

	"usbridge-client/internal/gui/assets"
	"usbridge-client/internal/gui/design"
	"usbridge-client/internal/gui/i18n"
	"usbridge-client/internal/gui/view"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
)

// useMobileControl is the Control/Devices/Snapshots/Scripts chrome fork:
// real phones and the desktop phone preview. Desktop header stays labeled.
func useMobileControl() bool {
	return view.IsMobile()
}

func mobileControlTabGaps() (gap, minGap float32) {
	if useMobileControl() {
		return 10, 6
	}
	// Desktop: inter-tab spacing lives inside headerTabButtonPadX so the
	// clickable boxes meet; icon+text stay 16px apart the way they did
	// when this was a 16px layout gap around tight text-sized widgets.
	return 0, 0
}

func scriptsTabLabel() string {
	if i18n.Current != nil && i18n.Current.TabLabelScripts != "" {
		return i18n.Current.TabLabelScripts
	}
	return "AI & Scripts"
}

func (mw *MainWindow) createMobileConnectedFooter(tabs fyne.CanvasObject) fyne.CanvasObject {
	const btnSize float32 = 32

	kb := newHeaderStatusBadgeButton(assets.KeyboardIcon, func() {
		mw.toggleMobileKeyboardStack()
	})
	kb.SetIconSize(fyne.NewSize(16, 16))
	kb.SetBadgeText("")
	kb.SetHoverStyle(design.ColorAlphaWhite07, btnSize/2)
	kb.SetSelectedStyle(design.ColorAlphaWhite12, assets.KeyboardIconFooterActive)
	mw.mobileKeyboardToggle = kb
	mw.mobileKeyboardBtn = container.NewGridWrap(fyne.NewSize(btnSize, btnSize), kb)

	vid := newHeaderStatusBadgeButton(assets.CameraIcon, func() {
		if mw.videoWidget != nil {
			// Warm the capture-modes cache so FPS/resolution/settings open
			// on the next tap without waiting on the bridge.
			mw.videoWidget.PrefetchCaptureModesAsync()
			mw.videoWidget.ShowCurrentVideoSettings(false)
		}
	})
	vid.SetIconSize(fyne.NewSize(16, 16))
	vid.SetBadgeText("")
	vid.SetHoverStyle(design.ColorAlphaWhite07, btnSize/2)
	mw.mobileVideoSettingsToggle = vid
	mw.mobileVideoSettingsBtn = container.NewGridWrap(fyne.NewSize(btnSize, btnSize), vid)
	mw.mobileVideoSettingsBtn.Hide()

	mon := newHeaderStatusBadgeButton(assets.MonitorTabIconMuted, func() {
		mw.showVideoMonitorMenu(mw.mobileMonitorToggle)
	})
	mon.SetIconSize(fyne.NewSize(16, 16))
	mon.SetBadgeText("")
	mon.SetHoverStyle(design.ColorAlphaWhite07, btnSize/2)
	mon.SetHoverIcon(assets.MonitorTabIconHover)
	mw.mobileMonitorToggle = mon
	mw.mobileMonitorBtn = container.NewGridWrap(fyne.NewSize(btnSize, btnSize), mon)
	mw.mobileMonitorBtn.Hide()

	clip := newHeaderStatusBadgeButton(assets.ClipboardIcon, func() {
		mw.showClipboardMenu()
	})
	clip.SetIconSize(fyne.NewSize(16, 16))
	clip.SetBadgeText("")
	clip.SetHoverStyle(design.ColorAlphaWhite07, btnSize/2)
	clip.SetHoverIcon(assets.ClipboardIconFooterHover)
	mw.mobileClipboardToggle = clip
	mw.mobileClipboardBtn = container.NewGridWrap(fyne.NewSize(btnSize, btnSize), clip)
	mw.mobileClipboardBtn.Hide()

	fs := newHeaderStatusBadgeButton(assets.FullscreenIconFooter, func() {
		if mw.videoWidget != nil {
			mw.videoWidget.ShowFullscreen()
		}
	})
	fs.SetIconSize(fyne.NewSize(16, 16))
	fs.SetBadgeText("")
	fs.SetHoverStyle(design.ColorAlphaWhite07, btnSize/2)
	mw.mobileFullscreenToggle = fs
	mw.mobileFullscreenBtn = container.NewGridWrap(fyne.NewSize(btnSize, btnSize), fs)
	mw.mobileFullscreenBtn.Hide()

	if graph, settings := view.NewNetGraphMobileFooterButtons(); graph != nil {
		mw.mobileNetGraphBtn = container.NewGridWrap(fyne.NewSize(btnSize, btnSize), graph)
		mw.mobileNetGraphBtn.Hide()
		if settings != nil {
			mw.mobileNetGraphSettingsBtn = container.NewGridWrap(fyne.NewSize(btnSize, btnSize), settings)
			mw.mobileNetGraphSettingsBtn.Hide()
		}
	}

	pan := newHeaderStatusBadgeButton(assets.ViewportPanIcon, func() {
		mw.toggleMobileViewportPanMode()
	})
	pan.SetIconSize(fyne.NewSize(16, 16))
	pan.SetBadgeText("")
	pan.SetHoverStyle(design.ColorAlphaWhite07, btnSize/2)
	pan.SetSelectedStyle(design.ColorAlphaWhite12, assets.ViewportPanIconActive)
	mw.mobileViewportPanToggle = pan
	mw.mobileViewportPanBtn = container.NewGridWrap(fyne.NewSize(btnSize, btnSize), pan)

	mouse := newHeaderStatusBadgeButton(assets.MouseIcon, func() {
		mw.showMouseModeMenuAt(mw.mobileMouseToggle)
	})
	mouse.SetIconSize(fyne.NewSize(16, 16))
	mouse.SetBadgeText("")
	mouse.SetHoverStyle(design.ColorAlphaWhite07, btnSize/2)
	mw.mobileMouseToggle = mouse
	mw.mobileMouseBtn = container.NewGridWrap(fyne.NewSize(btnSize, btnSize), mouse)

	mw.mobileFooterGraphDivider = newMobileFooterDivider()
	mw.mobileFooterGraphDivider.Hide()
	mw.mobileFooterHIDDivider = newMobileFooterDivider()

	burger := newHeaderStatusBadgeButton(theme.MenuIcon(), func() {
		mw.openDevicesFromControlBurger()
	})
	burger.SetIconSize(fyne.NewSize(16, 16))
	burger.SetBadgeText("")
	burger.SetIdleStyle(design.ColorGray900, design.ColorStatusBarBorder, 1, 6)
	burger.SetHoverStyle(design.ColorAlphaWhite15, 6)
	mw.mobileControlBurgerBtn = burger
	mw.mobileControlBurgerWrap = container.NewGridWrap(fyne.NewSize(btnSize, btnSize), burger)

	view.AddMenuSwapTargets(
		mw.mobileMonitorToggle,
		mw.mobileVideoSettingsToggle,
		mw.mobileClipboardToggle,
		mw.mobileFullscreenToggle,
		mw.mobileMouseToggle,
		mw.mobileKeyboardToggle,
	)

	mw.mobileTabsRow = tabs
	mw.connectedChromeHost = container.NewMax()
	mw.wireMobileKeyboardStackCallbacks()
	mw.wireMobileViewportPanCallbacks()
	mw.applyConnectedChromeLayout(true)
	return mw.connectedChromeHost
}

func (mw *MainWindow) wireMobileKeyboardStackCallbacks() {
	if mw.videoWidget == nil {
		return
	}
	mw.videoWidget.SetOnKeyboardStackChanged(func() {
		fyne.Do(func() {
			mw.syncMobileKeyboardToggleLook()
			mw.applyMainHeaderForKeyboardStack()
		})
	})
	mw.videoWidget.SetOnKeyboardChromeSync(func() {
		mw.syncMobileKeyboardToggleLook()
		mw.applyMainHeaderForKeyboardStack()
	})
}

// applyMainHeaderForKeyboardStack replaces the connected header with special
// keys while the keyboard stack is open (Vulkan cannot be drawn over; the
// header sits above the native surface). Dismiss lives after → in the keys.
// Native sticky IME zeros the top safe inset so this band rises into the
// former status-bar / cutout space.
func (mw *MainWindow) applyMainHeaderForKeyboardStack() {
	if !useMobileControl() || mw.mainHeaderHost == nil || mw.mainHeaderNormal == nil {
		return
	}
	open := mw.videoWidget != nil && mw.videoWidget.IsVirtualKeyboardVisible()
	if open {
		mw.showSpecialKeysInMainHeader()
		return
	}
	mw.restoreMainHeader()
}

func (mw *MainWindow) showSpecialKeysInMainHeader() {
	if mw.mainHeaderHost == nil || mw.videoWidget == nil {
		return
	}
	vk := mw.videoWidget.GetVirtualKeyboard()
	if vk == nil {
		mw.restoreMainHeader()
		return
	}
	kl := vk.GetKeyboardLayout()
	if kl == nil {
		mw.restoreMainHeader()
		return
	}
	vk.SetOnDismiss(func() {
		if mw.videoWidget != nil {
			mw.videoWidget.CloseAllKeyboards()
		}
	})

	band := view.NewSpecialKeysHeaderBand(kl)
	mw.mainHeaderHost.Objects = []fyne.CanvasObject{band}
	mw.mainHeaderHost.Refresh()
	mw.refreshMainHeaderLayout()
	reserve := band.MinSize().Height
	if h := mw.mainHeaderHost.Size().Height; h > reserve {
		reserve = h
	}
	mw.videoWidget.SetSpecialKeysHeaderReserve(reserve)
}

func (mw *MainWindow) restoreMainHeader() {
	if mw.mainHeaderHost == nil || mw.mainHeaderNormal == nil {
		return
	}
	mw.mainHeaderHost.Objects = []fyne.CanvasObject{mw.mainHeaderNormal}
	mw.mainHeaderHost.Refresh()
	if mw.videoWidget != nil {
		mw.videoWidget.SetSpecialKeysHeaderReserve(0)
	}
	mw.refreshMainHeaderLayout()
}

func (mw *MainWindow) wireMobileViewportPanCallbacks() {
	if mw.videoWidget == nil {
		return
	}
	mw.videoWidget.SetOnViewportPanModeChanged(func(on bool) {
		fyne.Do(func() {
			mw.syncMobileViewportPanToggleLook()
		})
	})
}

func (mw *MainWindow) openDevicesFromControlBurger() {
	if mw.tabs == nil {
		return
	}
	idx := mw.devicesTabIndex()
	if idx < 0 || idx >= len(mw.tabs.Items) {
		return
	}
	mw.tabs.Select(mw.tabs.Items[idx])
}

func (mw *MainWindow) toggleMobileKeyboardStack() {
	if mw.videoWidget == nil {
		return
	}
	mw.videoWidget.ToggleKeyboardStack()
	mw.syncMobileKeyboardToggleLook()
}

func (mw *MainWindow) toggleMobileViewportPanMode() {
	if mw.videoWidget == nil {
		return
	}
	mw.videoWidget.ToggleViewportPanMode()
	mw.syncMobileViewportPanToggleLook()
}

func (mw *MainWindow) controlTabActive() bool {
	return mw.tabs != nil && mw.tabs.SelectedIndex() == mw.controlTabIndex()
}

// noteConnectedChromeForSize updates IsLandscape and reflows the connected
// tab bar / version strip when the window rotates or the phone preview flips.
func (mw *MainWindow) noteConnectedChromeForSize(size fyne.Size) {
	if !useMobileControl() || mw.connectedChromeHost == nil {
		view.NoteCanvasSize(size)
		return
	}
	changed := view.NoteCanvasSize(size)
	landscape := view.IsLandscape()
	if !changed && landscape == mw.connectedLandscape {
		return
	}
	fyne.Do(func() {
		mw.applyConnectedChromeLayout(false)
	})
}

func (mw *MainWindow) applyConnectedChromeLayout(force bool) {
	if mw == nil || mw.connectedChromeHost == nil || mw.mobileTabsRow == nil {
		return
	}
	landscape := view.IsLandscape()
	if !force && landscape == mw.connectedLandscape {
		mw.refreshVirtualKeyboardCompactLayout()
		return
	}
	mw.connectedLandscape = landscape
	mw.setMobileTabButtonsStacked(!landscape)

	var chrome fyne.CanvasObject
	if landscape {
		chrome = mw.buildLandscapeConnectedChrome()
	} else {
		chrome = mw.buildPortraitConnectedChrome()
	}
	mw.connectedChromeHost.Objects = []fyne.CanvasObject{chrome}
	mw.connectedChromeHost.Refresh()
	mw.refreshVirtualKeyboardCompactLayout()

	if mw.videoWidget != nil {
		mw.videoWidget.InvalidateOverlayGeometry()
		time.AfterFunc(120*time.Millisecond, func() {
			fyne.Do(func() {
				if mw.videoWidget != nil {
					mw.videoWidget.InvalidateOverlayGeometry()
				}
			})
		})
	}
}

func (mw *MainWindow) refreshVirtualKeyboardCompactLayout() {
	if mw.videoWidget == nil {
		return
	}
	if vk := mw.videoWidget.GetVirtualKeyboard(); vk != nil {
		vk.RefreshCompactLayout()
	}
	if mw.videoWidget.IsVirtualKeyboardVisible() {
		mw.applyMainHeaderForKeyboardStack()
	}
}

func (mw *MainWindow) setMobileTabButtonsStacked(stacked bool) {
	for _, btn := range mw.tabHeaderButtons {
		if btn == nil {
			continue
		}
		btn.stacked = stacked
		if btn.text != nil {
			if stacked {
				btn.text.TextSize = 8
			} else {
				btn.text.TextSize = headerTabButtonTextSize
			}
		}
		btn.Refresh()
	}
}

func (mw *MainWindow) buildPortraitConnectedChrome() fyne.CanvasObject {
	var tabBar fyne.CanvasObject
	if mw.controlTabActive() {
		tabBar = mw.buildControlFooterStrip(false)
	} else {
		tabBar = mw.buildTabsFooterStrip(false)
	}
	mw.connectedVersionFooter = view.NewAppFooterNoLine(
		view.AppVersion(), nil, mw.connectedFooterBusy, mw.connectedFooterScript,
	)
	return container.NewVBox(tabBar, mw.connectedVersionFooter)
}

func (mw *MainWindow) buildLandscapeConnectedChrome() fyne.CanvasObject {
	if mw.controlTabActive() {
		return mw.buildControlFooterStrip(true)
	}
	return mw.buildTabsFooterStrip(true)
}

// buildControlFooterStrip is Control-only: burger left (fixed), scrollable
// video / fullscreen / net graph / pan / mouse / keyboard / clipboard right.
func (mw *MainWindow) buildControlFooterStrip(landscape bool) fyne.CanvasObject {
	var left fyne.CanvasObject
	if mw.mobileControlBurgerWrap != nil {
		left = mw.mobileControlBurgerWrap
	}
	actions := mw.mobileControlRightActions()
	var scroller fyne.CanvasObject
	if actions != nil {
		mw.mobileFooterActionsScroll = newMobileFooterActionScroller(actions)
		scroller = mw.mobileFooterActionsScroll
	}
	var trailing fyne.CanvasObject
	if landscape {
		var trailParts []fyne.CanvasObject
		if usableConnectedChromeObject(mw.connectedFooterBusy) {
			trailParts = append(trailParts, mw.connectedFooterBusy)
		}
		if usableConnectedChromeObject(mw.connectedFooterScript) {
			trailParts = append(trailParts, mw.connectedFooterScript)
		}
		if label := connectedVersionLabel(view.AppVersion(), false); label != nil {
			trailParts = append(trailParts, label)
		}
		switch len(trailParts) {
		case 1:
			trailing = trailParts[0]
		case 0:
		default:
			trailing = container.New(&view.DeviceRowControlsLayout{Gap: 12}, trailParts...)
		}
	}
	// Border: burger fixed left, optional version/busy fixed right, scroller
	// fills the middle so icons never draw under the menu button.
	var center fyne.CanvasObject = scroller
	if scroller != nil {
		center = view.NewInsetExact(scroller, 8, 0, 0, 0)
	}
	row := container.NewBorder(nil, nil, left, trailing, center)
	minH := float32(52)
	padT, padB := float32(6), float32(10)
	if landscape {
		minH = 36
		padT, padB = 4, 4
	}
	heightLock := canvas.NewRectangle(color.Transparent)
	heightLock.SetMinSize(fyne.NewSize(0, minH))
	inner := view.NewInsetExact(container.NewMax(heightLock, row), 8, 8, padT, padB)
	return newConnectedChromeStrip(inner)
}

func newMobileFooterDivider() fyne.CanvasObject {
	line := canvas.NewRectangle(design.ColorStatusBarDivider)
	return container.NewGridWrap(fyne.NewSize(1, 20), line)
}

func (mw *MainWindow) mobileControlRightActions() fyne.CanvasObject {
	var parts []fyne.CanvasObject
	if mw.mobileVideoSettingsBtn != nil {
		parts = append(parts, mw.mobileVideoSettingsBtn)
	}
	if mw.mobileMonitorBtn != nil {
		parts = append(parts, mw.mobileMonitorBtn)
	}
	if mw.mobileFullscreenBtn != nil {
		parts = append(parts, mw.mobileFullscreenBtn)
	}
	if mw.mobileViewportPanBtn != nil {
		parts = append(parts, mw.mobileViewportPanBtn)
	}
	if mw.mobileFooterGraphDivider != nil {
		parts = append(parts, mw.mobileFooterGraphDivider)
	}
	if mw.mobileNetGraphBtn != nil {
		parts = append(parts, mw.mobileNetGraphBtn)
	}
	if mw.mobileNetGraphSettingsBtn != nil {
		parts = append(parts, mw.mobileNetGraphSettingsBtn)
	}
	if mw.mobileFooterHIDDivider != nil {
		parts = append(parts, mw.mobileFooterHIDDivider)
	}
	if mw.mobileMouseBtn != nil {
		parts = append(parts, mw.mobileMouseBtn)
	}
	if mw.mobileKeyboardBtn != nil {
		parts = append(parts, mw.mobileKeyboardBtn)
	}
	// Clipboard sits rightmost next to mouse/keyboard (HID cluster).
	if mw.mobileClipboardBtn != nil {
		parts = append(parts, mw.mobileClipboardBtn)
	}
	switch len(parts) {
	case 0:
		return nil
	case 1:
		return parts[0]
	default:
		return container.New(&view.DeviceRowControlsLayout{Gap: 4}, parts...)
	}
}

// buildTabsFooterStrip is Devices/Snapshots/Scripts: existing tab buttons.
func (mw *MainWindow) buildTabsFooterStrip(landscape bool) fyne.CanvasObject {
	if landscape {
		var rightParts []fyne.CanvasObject
		if usableConnectedChromeObject(mw.connectedFooterBusy) {
			rightParts = append(rightParts, mw.connectedFooterBusy)
		}
		if usableConnectedChromeObject(mw.connectedFooterScript) {
			rightParts = append(rightParts, mw.connectedFooterScript)
		}
		if label := connectedVersionLabel(view.AppVersion(), true); label != nil {
			rightParts = append(rightParts, label)
		}
		var right fyne.CanvasObject
		if len(rightParts) == 1 {
			right = rightParts[0]
		} else if len(rightParts) > 1 {
			right = container.New(&view.DeviceRowControlsLayout{Gap: 12}, rightParts...)
		}
		row := container.NewBorder(nil, nil, nil, right, mw.mobileTabsRow)
		heightLock := canvas.NewRectangle(color.Transparent)
		heightLock.SetMinSize(fyne.NewSize(0, 36))
		inner := view.NewInsetExact(container.NewMax(heightLock, row), 8, 8, 4, 4)
		return newConnectedChromeStrip(inner)
	}
	row := mw.mobileTabsRow
	heightLock := canvas.NewRectangle(color.Transparent)
	heightLock.SetMinSize(fyne.NewSize(0, 52))
	inner := view.NewInsetExact(container.NewMax(heightLock, row), 8, 8, 6, 10)
	return newConnectedChromeStrip(inner)
}

func newConnectedChromeStrip(inner fyne.CanvasObject) fyne.CanvasObject {
	accent := canvas.NewRectangle(design.ColorHeaderAccentLine)
	accent.SetMinSize(fyne.NewSize(1, 1))
	bg := canvas.NewRectangle(design.ColorGray950)
	bg.CornerRadius = 0
	return container.NewStack(bg, view.NewTopLine(inner, accent))
}

// mobileControlFooterAlignLayout keeps the burger and the video/fullscreen/
// net-graph/pan/mouse/keyboard cluster on one baseline: left stays left, the rest pack
// to the right, all vertically centered. Border+GridWrap used to top-align the burger.
type mobileControlFooterAlignLayout struct {
	gap float32
}

func (l *mobileControlFooterAlignLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	var w, h float32
	n := 0
	for _, obj := range objects {
		if obj == nil || !obj.Visible() {
			continue
		}
		s := obj.MinSize()
		w += s.Width
		if s.Height > h {
			h = s.Height
		}
		n++
	}
	if n > 1 {
		w += l.gap * float32(n-1)
	}
	return fyne.NewSize(w, h)
}

func (l *mobileControlFooterAlignLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) == 0 {
		return
	}
	left := objects[0]
	if left != nil && left.Visible() {
		ls := left.MinSize()
		y := (size.Height - ls.Height) / 2
		if y < 0 {
			y = 0
		}
		left.Move(fyne.NewPos(0, y))
		left.Resize(ls)
	}
	x := size.Width
	for i := len(objects) - 1; i >= 1; i-- {
		obj := objects[i]
		if obj == nil || !obj.Visible() {
			continue
		}
		s := obj.MinSize()
		x -= s.Width
		y := (size.Height - s.Height) / 2
		if y < 0 {
			y = 0
		}
		obj.Move(fyne.NewPos(x, y))
		obj.Resize(s)
		x -= l.gap
	}
}

func usableConnectedChromeObject(obj fyne.CanvasObject) bool {
	return obj != nil
}

func connectedVersionLabel(version string, digitsVisible bool) fyne.CanvasObject {
	v := strings.TrimSpace(version)
	if v == "" {
		return nil
	}
	var col color.Color = design.ColorTextMuted
	if !digitsVisible {
		col = color.Transparent
	}
	label := canvas.NewText("v"+v, col)
	label.TextSize = 9
	return label
}

func (mw *MainWindow) syncMobileKeyboardButton(controlActive bool) {
	if mw.mobileKeyboardToggle == nil {
		return
	}
	if controlActive {
		mw.mobileKeyboardToggle.Show()
		if mw.mobileViewportPanToggle != nil {
			mw.mobileViewportPanToggle.Show()
		}
		if mw.mobileControlBurgerBtn != nil {
			mw.mobileControlBurgerBtn.Show()
		}
		if mw.mobileMouseToggle != nil {
			mw.mobileMouseToggle.Show()
		}
	} else {
		mw.mobileKeyboardToggle.Hide()
		if mw.mobileViewportPanToggle != nil {
			mw.mobileViewportPanToggle.Hide()
		}
		if mw.mobileControlBurgerBtn != nil {
			mw.mobileControlBurgerBtn.Hide()
		}
		if mw.mobileMouseToggle != nil {
			mw.mobileMouseToggle.Hide()
		}
		if mw.videoWidget != nil {
			mw.videoWidget.CloseAllKeyboards()
			mw.videoWidget.SetViewportPanMode(false)
		}
	}
	mw.applyConnectedChromeLayout(true)
	mw.syncMobileKeyboardToggleLook()
	mw.syncMobileViewportPanToggleLook()
}

func (mw *MainWindow) syncMobileKeyboardToggleLook() {
	if mw.mobileKeyboardToggle == nil {
		return
	}
	on := mw.videoWidget != nil && (mw.videoWidget.IsVirtualKeyboardVisible() || mw.videoWidget.IsSystemIMESticky())
	mw.mobileKeyboardToggle.SetSelected(on)
}

func (mw *MainWindow) syncMobileViewportPanToggleLook() {
	if mw.mobileViewportPanToggle == nil {
		return
	}
	on := mw.videoWidget != nil && mw.videoWidget.IsViewportPanMode()
	mw.mobileViewportPanToggle.SetSelected(on)
}
