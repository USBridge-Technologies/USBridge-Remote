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

func (vw *VideoWidget) showSpecialKeysOverlay() {
	vw.ensureVirtualKeyboard()
	if vw.virtualKeyboard == nil || vw.keyboardOverlay == nil {
		return
	}
	vw.virtualKeyboard.RegisterAsIMETarget()
	vw.virtualKeyboard.SetVisibleState(true)
	vw.layoutSpecialKeysOverlay()
	vw.keyboardOverlay.Show()
	if vw.contentContainer != nil {
		vw.contentContainer.Hide()
	}
	if vw.container != nil {
		vw.container.Refresh()
	}
	vw.forceCanvasRefresh.Store(true)
	logrus.Info("⌨️ Special-keys overlay shown")
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
	vw.forceCanvasRefresh.Store(true)
	logrus.Info("⌨️ Special-keys overlay hidden")
}

func (vw *VideoWidget) layoutSpecialKeysOverlay() {
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
