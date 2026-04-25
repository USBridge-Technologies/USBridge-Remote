package design

import (
	"image/color"

	"fyne.io/fyne/v2"
	fynetheme "fyne.io/fyne/v2/theme"
)

var (
	ColorBackground         = color.NRGBA{R: 0x00, G: 0x00, B: 0x00, A: 0xff} // --cs-bg-color
	ColorSurface            = color.NRGBA{R: 0x11, G: 0x11, B: 0x11, A: 0xff} // --cs-surface-color
	ColorInputBackground    = color.NRGBA{R: 0x17, G: 0x17, B: 0x17, A: 0xff} // --cs-input-bg-color
	ColorAccent             = color.NRGBA{R: 0x93, G: 0xc5, B: 0x72, A: 0xff} // --cs-accent
	ColorAccentHover        = color.NRGBA{R: 0xb6, G: 0xea, B: 0x93, A: 0xff} // --cs-accent-hover
	ColorProtocolQUIC       = color.NRGBA{R: 0xe9, G: 0x8a, B: 0x2b, A: 0xff}
	ColorTextLight          = color.NRGBA{R: 0xf5, G: 0xf5, B: 0xf5, A: 0xff} // --cs-text-light
	ColorTextMuted          = color.NRGBA{R: 0xc9, G: 0xc9, B: 0xc9, A: 0xff} // --cs-text-muted
	ColorBorder             = color.NRGBA{R: 0x65, G: 0x65, B: 0x65, A: 0xff} // --cs-border-color
	ColorSurfaceLight       = color.NRGBA{R: 0x35, G: 0x35, B: 0x35, A: 0xff} // --cs-surface-light
	ColorGray900            = color.NRGBA{R: 0x2c, G: 0x2c, B: 0x2c, A: 0xff} // --cs-gray-900
	ColorGray950            = color.NRGBA{R: 0x1d, G: 0x1d, B: 0x1d, A: 0xff} // --cs-gray-950
	ColorGray400            = color.NRGBA{R: 0xc8, G: 0xc8, B: 0xc8, A: 0xff} // --cs-gray-400
	ColorAlphaWhite15       = color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0x26} // --cs-alpha-white-15
	ColorAlphaWhite24       = color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0x3d} // --cs-alpha-white-24
	ColorAlphaAccent22      = color.NRGBA{R: 0x93, G: 0xc5, B: 0x72, A: 0x38} // --cs-alpha-accent-22
	ColorAlphaAccent55      = color.NRGBA{R: 0x93, G: 0xc5, B: 0x72, A: 0x8c} // --cs-alpha-accent-55
	ColorAlphaAccentHover55 = color.NRGBA{R: 0xb6, G: 0xea, B: 0x93, A: 0x8c} // --cs-alpha-accent-hover-55
)

const RadiusMD float32 = 8

// BrandTheme fixes the application to the current brand dark palette.
// Until a separate light palette is defined, both theme variants use the same colors.
type BrandTheme struct {
	fallback fyne.Theme
}

func NewBrandTheme() fyne.Theme {
	return &BrandTheme{fallback: fynetheme.DefaultTheme()}
}

func (t *BrandTheme) Color(name fyne.ThemeColorName, _ fyne.ThemeVariant) color.Color {
	switch name {
	case fynetheme.ColorNameBackground:
		return ColorGray950
	case fynetheme.ColorNameButton:
		return ColorSurfaceLight
	case fynetheme.ColorNameDisabledButton:
		return ColorGray900
	case fynetheme.ColorNameDisabled:
		return ColorBorder
	case fynetheme.ColorNameFocus:
		return ColorAlphaAccent22
	case fynetheme.ColorNameForeground:
		return ColorTextLight
	case fynetheme.ColorNameForegroundOnPrimary:
		return ColorBackground
	case fynetheme.ColorNameHeaderBackground:
		return ColorGray950
	case fynetheme.ColorNameHover:
		return ColorAlphaWhite15
	case fynetheme.ColorNameHyperlink:
		return ColorAccent
	case fynetheme.ColorNameInputBackground:
		return ColorInputBackground
	case fynetheme.ColorNameInputBorder:
		return ColorBorder
	case fynetheme.ColorNameMenuBackground:
		return ColorGray950
	case fynetheme.ColorNameOverlayBackground:
		return color.Transparent
	case fynetheme.ColorNamePlaceHolder:
		return ColorTextMuted
	case fynetheme.ColorNamePressed:
		return ColorAlphaWhite24
	case fynetheme.ColorNamePrimary:
		return ColorAccent
	case fynetheme.ColorNameScrollBar:
		return ColorBorder
	case fynetheme.ColorNameScrollBarBackground:
		return ColorGray950
	case fynetheme.ColorNameSelection:
		return ColorAlphaAccent22
	case fynetheme.ColorNameSeparator:
		return ColorBorder
	case fynetheme.ColorNameShadow:
		return color.Transparent
	case fynetheme.ColorNameSuccess:
		return ColorAccent
	case fynetheme.ColorNameWarning:
		return ColorAccentHover
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
	case fynetheme.SizeNameInnerPadding:
		return 0
	}
	return t.fallback.Size(name)
}
