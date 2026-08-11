package design

import (
	"image/color"

	"fyne.io/fyne/v2"
	fynetheme "fyne.io/fyne/v2/theme"
)

var (
	ColorBackground      = color.NRGBA{R: 0x00, G: 0x00, B: 0x00, A: 0xFF}
	ColorSurface         = color.NRGBA{R: 0x11, G: 0x11, B: 0x11, A: 0xFF}
	ColorSurfaceLight    = color.NRGBA{R: 0x35, G: 0x35, B: 0x35, A: 0xFF}
	ColorInputBackground = color.NRGBA{R: 0x17, G: 0x17, B: 0x17, A: 0xFF}
	ColorAccent          = color.NRGBA{R: 0x93, G: 0xC5, B: 0x72, A: 0xFF}
	ColorAccentSoft      = color.NRGBA{R: 0x93, G: 0xC5, B: 0x72, A: 0x38}
	ColorTextLight       = color.NRGBA{R: 0xF5, G: 0xF5, B: 0xF5, A: 0xFF}
	ColorTextMuted       = color.NRGBA{R: 0xC9, G: 0xC9, B: 0xC9, A: 0xFF}
	ColorBorder          = color.NRGBA{R: 0x65, G: 0x65, B: 0x65, A: 0xFF}
	ColorPanel           = color.NRGBA{R: 0x1D, G: 0x1D, B: 0x1D, A: 0xFF}
	ColorHeader          = color.NRGBA{R: 0x2C, G: 0x2C, B: 0x2C, A: 0xFF}
	ColorShadow          = color.NRGBA{R: 0x00, G: 0x00, B: 0x00, A: 0x38}
	ColorHover           = color.NRGBA{R: 0x53, G: 0x53, B: 0x53, A: 0xFF}
	ColorError           = color.NRGBA{R: 0xFF, G: 0x4B, B: 0x4B, A: 0xFF}

	// ColorHoverOverlay is what ColorNameHover actually resolves to for
	// stock widget.Button — Fyne blends it over the button's own background
	// with the "over" operator (see fyne's widget/button.go
	// buttonRenderer.applyTheme/blendColor), so it must stay translucent.
	// ColorHover above is fully opaque and is only safe as a hover fill for
	// widgets that set it directly as their background (e.g.
	// iconActionButtonRenderer) rather than blending it over an existing
	// color — reusing ColorHover here would flatten every hovered button
	// (including colored ones like supportButton/HighImportance) to solid
	// gray, masking its real color entirely.
	ColorHoverOverlay = color.NRGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0x1A}

	// ColorBrandAccent/ColorBrandAccentHover back the supportButton widget
	// (agent/internal/ui.newSupportButton) — brand green #bafc81, black
	// text, a touch lighter on hover.
	ColorBrandAccent      = color.NRGBA{R: 0xBA, G: 0xFC, B: 0x81, A: 0xFF}
	ColorBrandAccentHover = color.NRGBA{R: 0xC7, G: 0xFD, B: 0x9B, A: 0xFF}
)

const RadiusMD float32 = 6

type BrandTheme struct {
	fallback fyne.Theme
}

func NewBrandTheme() fyne.Theme {
	return &BrandTheme{fallback: fynetheme.DefaultTheme()}
}

func (t *BrandTheme) Color(name fyne.ThemeColorName, _ fyne.ThemeVariant) color.Color {
	switch name {
	case fynetheme.ColorNameBackground, fynetheme.ColorNameOverlayBackground, fynetheme.ColorNameMenuBackground:
		return ColorPanel
	case fynetheme.ColorNameButton:
		return ColorSurfaceLight
	case fynetheme.ColorNameDisabled:
		return ColorBorder
	case fynetheme.ColorNameDisabledButton:
		return ColorSurface
	case fynetheme.ColorNameForeground:
		return ColorTextLight
	case fynetheme.ColorNameForegroundOnPrimary:
		return ColorBackground
	case fynetheme.ColorNameHeaderBackground:
		return ColorPanel
	case fynetheme.ColorNameHover:
		return ColorHoverOverlay
	case fynetheme.ColorNameInputBackground:
		return ColorInputBackground
	case fynetheme.ColorNameInputBorder:
		return ColorBorder
	case fynetheme.ColorNamePlaceHolder:
		return ColorTextMuted
	case fynetheme.ColorNamePrimary:
		return ColorAccent
	case fynetheme.ColorNameSelection:
		return ColorHover
	case fynetheme.ColorNameSeparator:
		return ColorBorder
	}
	return t.fallback.Color(name, fynetheme.VariantDark)
}

func (t *BrandTheme) Font(style fyne.TextStyle) fyne.Resource {
	return t.fallback.Font(style)
}

func (t *BrandTheme) Icon(name fyne.ThemeIconName) fyne.Resource {
	return t.fallback.Icon(name)
}

func (t *BrandTheme) Size(name fyne.ThemeSizeName) float32 {
	switch name {
	case fynetheme.SizeNamePadding:
		return 8
	case fynetheme.SizeNameInputRadius, fynetheme.SizeNameSelectionRadius, fynetheme.SizeNameWindowButtonRadius:
		return RadiusMD
	}
	return t.fallback.Size(name)
}
