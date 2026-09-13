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
		mw.showMobileKeyboardMenu()
	})
	kb.SetIconSize(fyne.NewSize(16, 16))
	kb.SetBadgeText("")
	kb.SetHoverStyle(design.ColorAlphaWhite07, btnSize/2)
	kb.SetSelectedStyle(design.ColorAlphaWhite12, assets.KeyboardIconFooterActive)
	mw.mobileKeyboardToggle = kb
	mw.mobileKeyboardBtn = container.NewGridWrap(fyne.NewSize(btnSize, btnSize), kb)

	collapse := newHeaderStatusBadgeButton(theme.MoveDownIcon(), func() {
		mw.setConnectedChromeCollapsed(true)
	})
	collapse.SetIconSize(fyne.NewSize(16, 16))
	collapse.SetBadgeText("")
	collapse.SetHoverStyle(design.ColorAlphaWhite07, btnSize/2)
	mw.mobileChromeCollapseBtn = collapse
	mw.mobileChromeCollapseWrap = container.NewGridWrap(fyne.NewSize(btnSize, btnSize), collapse)

	expand := newHeaderStatusBadgeButton(theme.MoveUpIcon(), func() {
		mw.setConnectedChromeCollapsed(false)
	})
	expand.SetIconSize(fyne.NewSize(16, 16))
	expand.SetBadgeText("")
	expand.SetHoverStyle(design.ColorAlphaWhite07, btnSize/2)
	mw.mobileChromeExpandBtn = expand
	mw.mobileChromeExpandWrap = container.NewGridWrap(fyne.NewSize(btnSize, btnSize), expand)

	mw.mobileTabsRow = tabs
	mw.connectedChromeHost = container.NewMax()
	mw.applyConnectedChromeLayout(true)
	return mw.connectedChromeHost
}

func (mw *MainWindow) setConnectedChromeCollapsed(collapsed bool) {
	if mw.connectedChromeCollapsed == collapsed {
		return
	}
	mw.connectedChromeCollapsed = collapsed
	mw.applyConnectedChromeLayout(true)
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
	// observeContentResize runs inside Layout — defer the chrome swap so we
	// do not mutate the tree mid-pass.
	fyne.Do(func() {
		mw.applyConnectedChromeLayout(false)
	})
}

func (mw *MainWindow) applyConnectedChromeLayout(force bool) {
	if mw == nil || mw.connectedChromeHost == nil || mw.mobileTabsRow == nil {
		return
	}
	landscape := view.IsLandscape()
	if !landscape {
		// Collapse is landscape-only; leaving landscape always restores the full chrome.
		mw.connectedChromeCollapsed = false
	}
	if !force && landscape == mw.connectedLandscape {
		mw.refreshVirtualKeyboardCompactLayout()
		return
	}
	mw.connectedLandscape = landscape
	mw.setMobileTabButtonsStacked(!landscape)

	var chrome fyne.CanvasObject
	switch {
	case landscape && mw.connectedChromeCollapsed:
		chrome = mw.buildCollapsedConnectedChrome()
	case landscape:
		chrome = mw.buildLandscapeConnectedChrome()
	default:
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

func (mw *MainWindow) mobileFooterTrailingButtons(includeCollapse bool) fyne.CanvasObject {
	parts := make([]fyne.CanvasObject, 0, 3)
	if mw.mobileKeyboardBtn != nil {
		parts = append(parts, mw.mobileKeyboardBtn)
	}
	if includeCollapse && mw.mobileChromeCollapseWrap != nil {
		parts = append(parts, mw.mobileChromeCollapseWrap)
	}
	if len(parts) == 1 {
		return parts[0]
	}
	if len(parts) == 0 {
		return nil
	}
	return container.NewHBox(parts...)
}

func (mw *MainWindow) buildPortraitConnectedChrome() fyne.CanvasObject {
	// Portrait: keyboard only — no collapse chevron.
	trailing := mw.mobileFooterTrailingButtons(false)
	kbLayer := container.NewBorder(nil, nil, nil, trailing, nil)
	row := container.NewStack(mw.mobileTabsRow, kbLayer)
	heightLock := canvas.NewRectangle(color.Transparent)
	heightLock.SetMinSize(fyne.NewSize(0, 52))
	inner := view.NewInsetExact(container.NewMax(heightLock, row), 8, 8, 6, 10)
	tabBar := newConnectedChromeStrip(inner)

	mw.connectedVersionFooter = view.NewAppFooterNoLine(
		view.AppVersion(), nil, mw.connectedFooterBusy, mw.connectedFooterScript,
	)
	return container.NewVBox(tabBar, mw.connectedVersionFooter)
}

func (mw *MainWindow) buildCollapsedConnectedChrome() fyne.CanvasObject {
	// Landscape collapsed: keyboard + expand chevron — tabs/version hidden.
	parts := make([]fyne.CanvasObject, 0, 2)
	if mw.mobileKeyboardBtn != nil {
		parts = append(parts, mw.mobileKeyboardBtn)
	}
	if mw.mobileChromeExpandWrap != nil {
		parts = append(parts, mw.mobileChromeExpandWrap)
	}
	var right fyne.CanvasObject
	if len(parts) == 1 {
		right = parts[0]
	} else if len(parts) > 1 {
		right = container.NewHBox(parts...)
	}
	row := container.NewBorder(nil, nil, nil, right, nil)
	heightLock := canvas.NewRectangle(color.Transparent)
	heightLock.SetMinSize(fyne.NewSize(0, 36))
	inner := view.NewInsetExact(container.NewMax(heightLock, row), 8, 8, 4, 4)
	return newConnectedChromeStrip(inner)
}

func (mw *MainWindow) buildLandscapeConnectedChrome() fyne.CanvasObject {
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
	if mw.mobileKeyboardBtn != nil {
		rightParts = append(rightParts, mw.mobileKeyboardBtn)
	}
	if mw.mobileChromeCollapseWrap != nil {
		rightParts = append(rightParts, mw.mobileChromeCollapseWrap)
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
		if mw.mobileChromeCollapseBtn != nil {
			mw.mobileChromeCollapseBtn.Show()
		}
		if mw.mobileChromeExpandBtn != nil {
			mw.mobileChromeExpandBtn.Show()
		}
	} else {
		mw.mobileKeyboardToggle.Hide()
		if mw.mobileChromeCollapseBtn != nil {
			mw.mobileChromeCollapseBtn.Hide()
		}
		if mw.mobileChromeExpandBtn != nil {
			mw.mobileChromeExpandBtn.Hide()
		}
		if mw.connectedChromeCollapsed {
			mw.setConnectedChromeCollapsed(false)
		}
		if mw.videoWidget != nil {
			mw.videoWidget.CloseAllKeyboards()
		}
	}
	mw.syncMobileKeyboardToggleLook()
}

func (mw *MainWindow) showMobileKeyboardMenu() {
	if mw.mobileKeyboardBtn == nil || mw.videoWidget == nil {
		return
	}
	specialOn := mw.videoWidget.IsVirtualKeyboardVisible()
	systemOn := mw.videoWidget.IsSystemIMESticky()
	anyOn := specialOn || systemOn
	items := []view.StyledMenuItem{
		{
			Label:    "Special keys",
			Selected: specialOn,
			OnTap: func() {
				if systemOn {
					mw.videoWidget.SetSystemIMESticky(false)
				}
				mw.videoWidget.HandleVirtualKeyboard()
				mw.syncMobileKeyboardToggleLook()
			},
		},
		{
			Label:    "System keyboard",
			Selected: systemOn,
			OnTap: func() {
				if specialOn {
					mw.videoWidget.HandleVirtualKeyboard()
				}
				mw.videoWidget.SetSystemIMESticky(!systemOn)
				mw.syncMobileKeyboardToggleLook()
			},
		},
	}
	if anyOn {
		items = append(items, view.StyledMenuItem{
			Label: "Close",
			OnTap: func() {
				mw.videoWidget.CloseAllKeyboards()
				mw.syncMobileKeyboardToggleLook()
			},
		})
	}
	view.ShowStyledMenuTealAbove(mw.mobileKeyboardBtn, items)
}

func (mw *MainWindow) syncMobileKeyboardToggleLook() {
	if mw.mobileKeyboardToggle == nil {
		return
	}
	on := mw.videoWidget != nil && (mw.videoWidget.IsVirtualKeyboardVisible() || mw.videoWidget.IsSystemIMESticky())
	mw.mobileKeyboardToggle.SetSelected(on)
}
