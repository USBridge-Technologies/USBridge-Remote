package controller

import (
	"image/color"

	"usbridge-client/internal/gui/design"
	"usbridge-client/internal/gui/graphics"
	"usbridge-client/internal/gui/view"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/sirupsen/logrus"
)

func (vw *VideoWidget) ensureVirtualKeyboard() {
	if vw.virtualKeyboard != nil {
		return
	}
	if vw.parentWindow == nil {
		logrus.Warn("⚠️ Parent window is not set")
		return
	}
	vw.virtualKeyboard = graphics.NewVirtualKeyboard(vw.parentWindow, vw.handleVirtualKeyPress, vw.handlePhysicalRunePress)
	vw.virtualKeyboard.SetOnDismiss(func() {
		vw.CloseAllKeyboards()
	})
	vw.virtualKeyboard.SetOnIMEChanged(func(imeHeightDp float32) {
		fyne.Do(func() {
			vw.onIMEHeightChanged(imeHeightDp)
			if vw.IsVirtualKeyboardVisible() {
				vw.layoutSpecialKeysOverlay()
			}
		})
	})
}

func (vw *VideoWidget) ensureMobileVirtualKeyboard() {
	vw.ensureVirtualKeyboard()
}

// specialKeysInMainHeader reports whether special keys replace the app
// header instead of floating over the video. Mobile Vulkan/Metal use
// z-order-on-top surfaces that cover all Fyne drawing in the video rect.
func (vw *VideoWidget) specialKeysInMainHeader() bool {
	return view.IsMobile()
}

func (vw *VideoWidget) showSpecialKeysOverlay() {
	vw.ensureVirtualKeyboard()
	if vw.virtualKeyboard == nil {
		return
	}
	if !vw.specialKeysInMainHeader() && vw.keyboardOverlay == nil {
		return
	}
	vw.virtualKeyboard.RegisterAsIMETarget()
	vw.virtualKeyboard.SetVisibleState(true)
	if vw.specialKeysInMainHeader() {
		// MainWindow swaps the header band; keep the video overlay empty so
		// the keyboard layout has a single parent.
		if vw.keyboardOverlay != nil {
			vw.keyboardOverlay.Objects = nil
			vw.keyboardOverlay.Hide()
			vw.keyboardOverlay.Refresh()
		}
	} else {
		vw.layoutSpecialKeysOverlay()
		vw.keyboardOverlay.Show()
	}
	if vw.contentContainer != nil {
		vw.contentContainer.Hide()
	}
	if vw.container != nil {
		vw.container.Refresh()
	}
	vw.InvalidateOverlayGeometry()
	vw.forceCanvasRefresh.Store(true)
	logrus.Info("⌨️ Special-keys shown")
}

func (vw *VideoWidget) hideSpecialKeysOverlay() {
	if vw.virtualKeyboard != nil {
		vw.virtualKeyboard.SetVisibleState(false)
	}
	if vw.keyboardOverlay != nil {
		vw.keyboardOverlay.Objects = nil
		vw.keyboardOverlay.Hide()
		vw.keyboardOverlay.Refresh()
	}
	if vw.contentContainer != nil {
		vw.contentContainer.Hide()
	}
	if vw.container != nil {
		vw.container.Refresh()
	}
	vw.InvalidateOverlayGeometry()
	vw.forceCanvasRefresh.Store(true)
	logrus.Info("⌨️ Special-keys hidden")
}

func (vw *VideoWidget) layoutSpecialKeysOverlay() {
	if vw.specialKeysInMainHeader() {
		vw.InvalidateOverlayGeometry()
		return
	}
	if vw.virtualKeyboard == nil || vw.keyboardOverlay == nil || vw.parentWindow == nil {
		return
	}
	kl := vw.virtualKeyboard.GetKeyboardLayout()
	canvasW := vw.parentWindow.Canvas().Size().Width
	if vw.container != nil {
		if w := vw.container.Size().Width; w > 0 {
			canvasW = w
		}
	}
	h := kl.MinSize().Height
	kl.Resize(fyne.NewSize(canvasW, h))
	vw.keyboardOverlay.Objects = []fyne.CanvasObject{kl}
	vw.keyboardOverlay.Refresh()
	vw.InvalidateOverlayGeometry()
}

// specialKeysOverlayHeightDp is the top inset reserved when special keys
// float over the video (desktop/web). On mobile the keys replace the main
// header, so the native surface needs no keys inset.
func (vw *VideoWidget) specialKeysOverlayHeightDp() float32 {
	if vw == nil || vw.specialKeysInMainHeader() || !vw.IsVirtualKeyboardVisible() || vw.virtualKeyboard == nil {
		return 0
	}
	const minKeysBand = float32(72)
	kl := vw.virtualKeyboard.GetKeyboardLayout()
	if kl == nil {
		return minKeysBand
	}
	h := kl.MinSize().Height
	if sz := kl.Size().Height; sz > h {
		h = sz
	}
	if h < minKeysBand {
		return minKeysBand
	}
	return h
}

func (vw *VideoWidget) initKeyboardCollapseFAB() {
	if vw.collapseFAB == nil {
		return
	}
	const size float32 = 44
	bg := canvas.NewCircle(design.ColorConnectionBadgeText)
	icon := canvas.NewImageFromResource(theme.MoveDownIcon())
	icon.FillMode = canvas.ImageFillContain
	icon.SetMinSize(fyne.NewSize(20, 20))
	hit := &keyboardCollapseButton{onTap: func() {
		vw.CloseAllKeyboards()
	}}
	hit.ExtendBaseWidget(hit)
	face := container.NewStack(bg, container.NewCenter(icon), hit)
	wrap := container.NewGridWrap(fyne.NewSize(size, size), face)
	vw.collapseFAB.Objects = []fyne.CanvasObject{
		view.NewInsetExact(wrap, 0, 12, 88, 0),
	}
	vw.collapseFAB.Hide()
}

func (vw *VideoWidget) setKeyboardCollapseFABVisible(on bool) {
	if vw.collapseFAB == nil {
		return
	}
	// Mobile dismiss lives after → in the special-keys header strip.
	if vw.specialKeysInMainHeader() {
		on = false
	}
	if on {
		vw.collapseFAB.Show()
	} else {
		vw.collapseFAB.Hide()
	}
	vw.collapseFAB.Refresh()
}

type keyboardCollapseButton struct {
	widget.BaseWidget
	onTap func()
}

func (b *keyboardCollapseButton) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(canvas.NewRectangle(color.Transparent))
}

func (b *keyboardCollapseButton) Tapped(_ *fyne.PointEvent) {
	if b.onTap != nil {
		b.onTap()
	}
}

func (b *keyboardCollapseButton) MinSize() fyne.Size {
	return fyne.NewSize(44, 44)
}
