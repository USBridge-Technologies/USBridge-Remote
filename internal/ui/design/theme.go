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
)

const RadiusMD float32 = 10

type BrandTheme struct {
	fallback fyne.Theme
}

func NewBrandTheme() fyne.Theme {
	return &BrandTheme{fallback: fynetheme.DefaultTheme()}
}

func (t *BrandTheme) Color(name fyne.ThemeColorName, _ fyne.ThemeVariant) color.Color {
	switch name {
	case fynetheme.ColorNameBackground:
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
		return ColorAccentSoft
	case fynetheme.ColorNameInputBackground:
		return ColorInputBackground
	case fynetheme.ColorNameInputBorder:
		return ColorBorder
	case fynetheme.ColorNamePlaceHolder:
		return ColorTextMuted
	case fynetheme.ColorNamePrimary:
		return ColorAccent
	case fynetheme.ColorNameSelection:
		return ColorAccentSoft
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
	case fynetheme.SizeNameInputRadius, fynetheme.SizeNameSelectionRadius, fynetheme.SizeNameWindowButtonRadius:
		return RadiusMD
	}
	return t.fallback.Size(name)
}
