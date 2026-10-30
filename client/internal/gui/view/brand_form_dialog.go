package view

import (
	"image/color"
	"strings"
	"sync"

	"usbridge-client/internal/gui/design"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// DialogToggle is the Add Connection / Video dialog checkbox, so other
// panels can share that control without depending on unexported types.
type DialogToggle interface {
	fyne.CanvasObject
	SetChecked(bool)
	Enable()
	Disable()
	Disabled() bool
	IsChecked() bool
}

// NewDialogToggle builds the teal check-mark box used in the Video dialog.
func NewDialogToggle(checked bool, onChanged func(bool)) DialogToggle {
	return newVideoDialogCheckbox(checked, onChanged)
}

// NewDialogPicker is the compact teal-bordered dropdown used for Video
// resolution/FPS — same family as Add Connection's protocol chip.
func NewDialogPicker(options []string, selected string, onSelected func(string)) *HeaderDropdown {
	d := newVideoDialogPicker(onSelected)
	d.SetOptions(options)
	d.SetSelected(selected)
	return d
}

// NewDialogField stacks an uppercase muted caption over a control, matching
// the Video dialog's Codec / Resolution labels.
func NewDialogField(label string, control fyne.CanvasObject) fyne.CanvasObject {
	return container.NewVBox(newVideoDialogFieldLabel(label), control)
}

// NewDialogHint is wrapped muted body copy at the Video dialog's caption size.
func NewDialogHint(text string, width float32) fyne.CanvasObject {
	if width <= 0 {
		width = videoDialogPanelWidth - (videoDialogBodyInsetLR+videoDialogBodyInsetQuirk)*2
	}
	return newVideoDialogWrapText(width, 10, false, videoDialogWrapSpan{Text: text, Color: videoDialogHintColor})
}

// NewDialogStepSlider is the Video dialog's compact slider (caption and a
// value pill over the slider) for whole steps lo..hi, starting at value;
// label names a step. current reports where it is now.
func NewDialogStepSlider(caption string, lo, hi, value int, label func(int) string) (row fyne.CanvasObject, current func() int) {
	slider := newVideoDialogBitrateSlider(float64(lo), float64(hi), 1)
	slider.Value = float64(value)
	text := newVideoDialogPillText()
	step := func() int { return max(lo, min(hi, int(slider.Value+0.5))) }
	slider.OnChanged = func(float64) {
		text.Text = label(step())
		text.Refresh()
	}
	slider.OnChanged(slider.Value)
	return newVideoDialogCompactSliderRow(caption, text, slider), step
}

func NewDialogVSpace(height float32) fyne.CanvasObject {
	return videoDialogVSpace(height)
}

// NewDialogToggleRow is a full-row-tappable checkbox + title + optional
// muted description, same layout as Video's Other settings rows.
func NewDialogToggleRow(check DialogToggle, title, description string, descWidth float32) fyne.CanvasObject {
	cb, ok := check.(*videoDialogCheckbox)
	if !ok {
		return container.NewHBox(check, newVideoDialogRowTitle(title))
	}
	badge := canvas.NewRectangle(color.Transparent)
	badge.SetMinSize(fyne.NewSize(0, 0))
	var desc fyne.CanvasObject
	if strings.TrimSpace(description) == "" {
		empty := canvas.NewRectangle(color.Transparent)
		empty.SetMinSize(fyne.NewSize(0, 0))
		desc = empty
	} else {
		if descWidth <= 0 {
			descWidth = videoDialogToggleDescWidthFor(videoDialogPanelWidth)
		}
		desc = newVideoDialogDescription(description, descWidth)
	}
	row := newVideoDialogToggleRow(cb, newVideoDialogRowTitle(title), badge, desc)
	return NewInsetExact(row, videoDialogToggleAlignLeft, 0, 0, 0)
}

// NewDialogSurfaceCard is a rounded #0b/#1e bordered card for grouping
// controls inside a brand dialog body.
func NewDialogSurfaceCard(body fyne.CanvasObject) fyne.CanvasObject {
	bg := canvas.NewRectangle(design.ColorGray950)
	bg.CornerRadius = design.RadiusMD
	border := canvas.NewRectangle(color.Transparent)
	border.CornerRadius = design.RadiusMD
	border.StrokeColor = videoDialogBorderColor
	border.StrokeWidth = 1
	return container.NewStack(bg, border, NewInsetExact(body, 8, 8, 8, 8))
}

// BrandFormDialogSpec is the Add Connection / Video chrome: accent hairline,
// left title, corner X, hairline footer, Cancel left / Apply right pills.
type BrandFormDialogSpec struct {
	Parent     fyne.Window
	Title      string
	Body       fyne.CanvasObject
	CancelText string
	ApplyText  string
	OnCancel   func()
	OnApply    func()
	// MinWidth floors the panel (Add Connection / Video use 408).
	MinWidth float32
	// PanelSize overrides the default content-sized panel (results dialog).
	PanelSize func(canvasSize fyne.Size, panel fyne.CanvasObject) fyne.Size
	// FooterLeft, when set, takes the Cancel button's place on the left of
	// the footer (a dialog whose only way out is Apply/close: KVM settings'
	// Reboot KVM). Close and a tap outside still run OnCancel.
	FooterLeft fyne.CanvasObject
}

// ShowBrandFormDialog opens a branded overlay matching Add Connection.
func ShowBrandFormDialog(spec BrandFormDialogSpec) *widget.PopUp {
	var popup *widget.PopUp
	var once sync.Once
	run := func(ok bool) {
		once.Do(func() {
			if popup != nil {
				popup.Hide()
			}
			if ok {
				if spec.OnApply != nil {
					spec.OnApply()
				}
			} else if spec.OnCancel != nil {
				spec.OnCancel()
			}
		})
	}

	cancel := newVideoDialogCancelButton(spec.CancelText, func() { run(false) })
	apply := newVideoDialogApplyButton(spec.ApplyText, func() { run(true) })
	closeBtn := newIconChromeButton(iconChromeButtonSpec{
		NormalFill: color.Transparent,
		HoverFill:  design.ColorSurfaceLight,
		NormalIcon: videoDialogCancelIconSVG,
		HoverIcon:  videoDialogCancelIconSVG,
		IconSize:   fyne.NewSize(18, 18),
		ButtonSize: fyne.NewSize(28, 28),
		OnTapped:   func() { run(false) },
	})

	title := NewBrandText(spec.Title, 13, design.ColorTextLight, true)
	headerSep := canvas.NewRectangle(color.NRGBA{R: 0x30, G: 0x34, B: 0x2e, A: 0xff})
	headerSep.SetMinSize(fyne.NewSize(0, 1))
	footerSep := canvas.NewRectangle(color.NRGBA{R: 0x30, G: 0x34, B: 0x2e, A: 0xff})
	footerSep.SetMinSize(fyne.NewSize(0, 1))
	headerBlock := container.NewVBox(newVideoDialogTopAccentBar(), NewInset(title, 21, 44, 9, 4), headerSep)

	var footerLeft fyne.CanvasObject = cancel
	if spec.FooterLeft != nil {
		footerLeft = spec.FooterLeft
	}
	footerButtons := container.NewBorder(nil, nil, container.NewCenter(footerLeft), container.New(&DeviceRowControlsLayout{Gap: 12}, apply))
	footerBlock := container.NewVBox(footerSep, NewInsetExact(footerButtons, 12, 18, 6, 0))

	form := container.NewBorder(headerBlock, footerBlock, nil, nil, NewInset(spec.Body, videoDialogBodyInsetLR, videoDialogBodyInsetLR, 12, 0))

	bg := canvas.NewRectangle(design.ColorGray900)
	bg.CornerRadius = design.RadiusMD
	border := canvas.NewRectangle(color.Transparent)
	border.CornerRadius = design.RadiusMD
	border.StrokeColor = design.ColorBorder
	border.StrokeWidth = 1
	cornerBtn := container.New(&videoDialogCornerButtonLayout{Top: 12, Right: 12}, closeBtn)
	panel := container.NewStack(bg, NewInsetExact(form, 0, 0, 0, 8), cornerBtn, border)

	minW := spec.MinWidth
	if minW <= 0 {
		minW = videoDialogPanelWidth
	}
	sizeFn := spec.PanelSize
	if sizeFn == nil {
		sizeFn = func(canvasSize fyne.Size, panel fyne.CanvasObject) fyne.Size {
			margin := clampFloat32(minFloat32(canvasSize.Width, canvasSize.Height)*0.04, 20, 28)
			maxWidth := canvasSize.Width - margin*2
			maxHeight := canvasSize.Height - margin*2
			if maxWidth <= 0 {
				maxWidth = canvasSize.Width
			}
			if maxHeight <= 0 {
				maxHeight = canvasSize.Height
			}
			panelMin := panel.MinSize()
			panelWidth := minFloat32(maxFloat32(panelMin.Width, minW), maxWidth)
			panelHeight := minFloat32(panelMin.Height, maxHeight)
			return fyne.NewSize(panelWidth, panelHeight)
		}
	}

	popup = ShowOverlayPopup(spec.Parent, OverlayPopupSpec{
		Panel:        panel,
		DimColor:     color.NRGBA{R: 0x00, G: 0x00, B: 0x00, A: 0x72},
		PanelSize:    sizeFn,
		OnOutsideTap: func() { run(false) },
	})
	return popup
}

// NewDialogCancelButton / NewDialogApplyButton are the footer pills, exported
// so results (and tests that look up Text()) can reuse them.
func NewDialogCancelButton(text string, onTap func()) fyne.CanvasObject {
	return newVideoDialogCancelButton(text, onTap)
}

func NewDialogApplyButton(text string, onTap func()) fyne.CanvasObject {
	return newVideoDialogApplyButton(text, onTap)
}

func NewDialogIconButton(icon fyne.Resource, onTap func()) fyne.CanvasObject {
	return newIconChromeButton(iconChromeButtonSpec{
		NormalFill: design.ColorConnectionBadgeText,
		HoverFill:  color.NRGBA{R: 0x61, G: 0xf0, B: 0xd3, A: 0xff},
		NormalIcon: icon,
		HoverIcon:  icon,
		IconSize:   fyne.NewSize(16, 16),
		ButtonSize: fyne.NewSize(28, 28),
		OnTapped:   onTap,
	})
}

// AssembleBrandDialogChrome wraps body with the same header/footer as
// ShowBrandFormDialog, for panels that need a custom size policy (results).
// footerButtons may be nil to omit the footer (hotkeys).
func AssembleBrandDialogChrome(title string, body, footerButtons fyne.CanvasObject, onClose func()) fyne.CanvasObject {
	return assembleBrandDialogChrome(title, "", body, footerButtons, onClose, 12, 18, 6, 8)
}

func AssembleBrandDialogChromeTightFooter(title string, body, footerButtons fyne.CanvasObject, onClose func()) fyne.CanvasObject {
	return assembleBrandDialogChrome(title, "", body, footerButtons, onClose, 8, 8, 2, 4)
}

// AssembleBrandDialogChromeSub is AssembleBrandDialogChrome with a muted
// header subtitle and no footer.
func AssembleBrandDialogChromeSub(title, subtitle string, body fyne.CanvasObject, onClose func()) fyne.CanvasObject {
	return assembleBrandDialogChrome(title, subtitle, body, nil, onClose, 12, 18, 6, 8)
}

func assembleBrandDialogChrome(title, subtitle string, body, footerButtons fyne.CanvasObject, onClose func(), footerL, footerR, footerT, bottom float32) fyne.CanvasObject {
	closeBtn := newIconChromeButton(iconChromeButtonSpec{
		NormalFill: color.Transparent,
		HoverFill:  design.ColorSurfaceLight,
		NormalIcon: videoDialogCancelIconSVG,
		HoverIcon:  videoDialogCancelIconSVG,
		IconSize:   fyne.NewSize(18, 18),
		ButtonSize: fyne.NewSize(28, 28),
		OnTapped:   onClose,
	})
	titleText := NewBrandText(title, 13, design.ColorTextLight, true)
	var titleCol fyne.CanvasObject = titleText
	if strings.TrimSpace(subtitle) != "" {
		sub := newVideoDialogWrapText(340, 10, false, videoDialogWrapSpan{Text: subtitle, Color: videoDialogHintColor})
		titleCol = container.NewVBox(titleText, videoDialogVSpace(2), sub)
	}
	headerSep := canvas.NewRectangle(color.NRGBA{R: 0x30, G: 0x34, B: 0x2e, A: 0xff})
	headerSep.SetMinSize(fyne.NewSize(0, 1))
	headerBlock := container.NewVBox(newVideoDialogTopAccentBar(), NewInset(titleCol, 21, 44, 9, 4), headerSep)

	var footerBlock fyne.CanvasObject
	if footerButtons != nil {
		footerSep := canvas.NewRectangle(color.NRGBA{R: 0x30, G: 0x34, B: 0x2e, A: 0xff})
		footerSep.SetMinSize(fyne.NewSize(0, 1))
		footerBlock = container.NewVBox(footerSep, NewInsetExact(footerButtons, footerL, footerR, footerT, 0))
	}
	form := container.NewBorder(headerBlock, footerBlock, nil, nil, body)

	bg := canvas.NewRectangle(design.ColorGray900)
	bg.CornerRadius = design.RadiusMD
	border := canvas.NewRectangle(color.Transparent)
	border.CornerRadius = design.RadiusMD
	border.StrokeColor = design.ColorBorder
	border.StrokeWidth = 1
	cornerBtn := container.New(&videoDialogCornerButtonLayout{Top: 12, Right: 12}, closeBtn)
	if bottom <= 0 {
		bottom = 8
		if footerButtons == nil {
			bottom = 12
		}
	}
	return container.NewStack(bg, NewInsetExact(form, 0, 0, 0, bottom), cornerBtn, border)
}
