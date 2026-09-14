package gui

import (
	"image/color"
	"strings"
	"time"

	"usbridge-client/internal/gui/assets"
	"usbridge-client/internal/gui/design"
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
	return 16, 8
}

func scriptsTabLabel() string {
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

	pan := newHeaderStatusBadgeButton(assets.ViewportPanIcon, func() {
		mw.toggleMobileViewportPanMode()
	})
	pan.SetIconSize(fyne.NewSize(16, 16))
	pan.SetBadgeText("")
	pan.SetHoverStyle(design.ColorAlphaWhite07, btnSize/2)
	pan.SetSelectedStyle(design.ColorAlphaWhite12, assets.ViewportPanIconActive)
	mw.mobileViewportPanToggle = pan
	mw.mobileViewportPanBtn = container.NewGridWrap(fyne.NewSize(btnSize, btnSize), pan)

	burger := newHeaderStatusBadgeButton(theme.MenuIcon(), func() {
		mw.openDevicesFromControlBurger()
	})
	burger.SetIconSize(fyne.NewSize(16, 16))
	burger.SetBadgeText("")
	burger.SetHoverStyle(design.ColorAlphaWhite07, btnSize/2)
	mw.mobileControlBurgerBtn = burger
	mw.mobileControlBurgerWrap = container.NewGridWrap(fyne.NewSize(btnSize, btnSize), burger)

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
}

// applyMainHeaderForKeyboardStack replaces the connected header with special
// keys while the keyboard stack is open (Vulkan cannot be drawn over; the
// header sits above the native surface). Dismiss lives after → in the keys.
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
	mw.mainHeaderHost.Objects = []fyne.CanvasObject{view.NewHeaderBand("", kl)}
	mw.mainHeaderHost.Refresh()
	mw.videoWidget.InvalidateOverlayGeometry()
	mw.refreshMainHeaderLayout()
}

func (mw *MainWindow) restoreMainHeader() {
	if mw.mainHeaderHost == nil || mw.mainHeaderNormal == nil {
		return
	}
	mw.mainHeaderHost.Objects = []fyne.CanvasObject{mw.mainHeaderNormal}
	mw.mainHeaderHost.Refresh()
	if mw.videoWidget != nil {
		mw.videoWidget.InvalidateOverlayGeometry()
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

// buildControlFooterStrip is Control-only: burger → Devices, pan + Keyboard.
func (mw *MainWindow) buildControlFooterStrip(landscape bool) fyne.CanvasObject {
	var left fyne.CanvasObject
	if mw.mobileControlBurgerWrap != nil {
		left = mw.mobileControlBurgerWrap
	}
	right := mw.mobileControlRightActions()
	if landscape {
		var rightParts []fyne.CanvasObject
		if usableConnectedChromeObject(mw.connectedFooterBusy) {
			rightParts = append(rightParts, mw.connectedFooterBusy)
		}
		if usableConnectedChromeObject(mw.connectedFooterScript) {
			rightParts = append(rightParts, mw.connectedFooterScript)
		}
		if label := connectedVersionLabel(view.AppVersion()); label != nil {
			rightParts = append(rightParts, label)
		}
		if actions := mw.mobileControlRightActions(); actions != nil {
			rightParts = append(rightParts, actions)
		}
		if len(rightParts) == 1 {
			right = rightParts[0]
		} else if len(rightParts) > 1 {
			right = container.New(&view.DeviceRowControlsLayout{Gap: 12}, rightParts...)
		} else {
			right = nil
		}
	}
	row := container.NewBorder(nil, nil, left, right, nil)
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

func (mw *MainWindow) mobileControlRightActions() fyne.CanvasObject {
	var parts []fyne.CanvasObject
	if mw.mobileViewportPanBtn != nil {
		parts = append(parts, mw.mobileViewportPanBtn)
	}
	if mw.mobileKeyboardBtn != nil {
		parts = append(parts, mw.mobileKeyboardBtn)
	}
	switch len(parts) {
	case 0:
		return nil
	case 1:
		return parts[0]
	default:
		return container.New(&view.DeviceRowControlsLayout{Gap: 8}, parts...)
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
		if label := connectedVersionLabel(view.AppVersion()); label != nil {
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

func usableConnectedChromeObject(obj fyne.CanvasObject) bool {
	return obj != nil
}

func connectedVersionLabel(version string) fyne.CanvasObject {
	v := strings.TrimSpace(version)
	if v == "" {
		return nil
	}
	label := canvas.NewText("v"+v, design.ColorTextMuted)
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
	} else {
		mw.mobileKeyboardToggle.Hide()
		if mw.mobileViewportPanToggle != nil {
			mw.mobileViewportPanToggle.Hide()
		}
		if mw.mobileControlBurgerBtn != nil {
			mw.mobileControlBurgerBtn.Hide()
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
