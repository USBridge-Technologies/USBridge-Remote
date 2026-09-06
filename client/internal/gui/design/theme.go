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
	ColorAlert              = color.NRGBA{R: 0xe9, G: 0x8a, B: 0x2b, A: 0xff}
	ColorTextLight          = color.NRGBA{R: 0xf5, G: 0xf5, B: 0xf5, A: 0xff} // --cs-text-light
	ColorTextMuted          = color.NRGBA{R: 0xc9, G: 0xc9, B: 0xc9, A: 0xff} // --cs-text-muted
	ColorBorder             = color.NRGBA{R: 0x65, G: 0x65, B: 0x65, A: 0xff} // --cs-border-color
	ColorSurfaceLight       = color.NRGBA{R: 0x35, G: 0x35, B: 0x35, A: 0xff} // --cs-surface-light
	ColorGray900            = color.NRGBA{R: 0x16, G: 0x1b, B: 0x22, A: 0xff} // --cs-gray-900 (Header)
	ColorGray950            = color.NRGBA{R: 0x0d, G: 0x11, B: 0x17, A: 0xff} // --cs-gray-950 (Window)
	ColorGray400            = color.NRGBA{R: 0xc8, G: 0xc8, B: 0xc8, A: 0xff} // --cs-gray-400
	ColorAlphaWhite07       = color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0x12} // --cs-alpha-white-07
	ColorAlphaWhite12       = color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0x1f} // --cs-alpha-white-12
	ColorAlphaWhite15       = color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0x26} // --cs-alpha-white-15
	ColorAlphaWhite24       = color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0x3d} // --cs-alpha-white-24
	ColorAlphaAccent22      = color.NRGBA{R: 0x93, G: 0xc5, B: 0x72, A: 0x38} // --cs-alpha-accent-22
	ColorAlphaAccent55      = color.NRGBA{R: 0x93, G: 0xc5, B: 0x72, A: 0x8c} // --cs-alpha-accent-55
	ColorAlphaAccentHover55 = color.NRGBA{R: 0xb6, G: 0xea, B: 0x93, A: 0x8c} // --cs-alpha-accent-hover-55
	ColorWhite              = color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}

	// Tailscale header chip: a slightly desaturated accent pair, distinct from
	// the main ColorAccent/ColorAccentHover used everywhere else, used only by
	// the Tailscale toggle border/label in its default (off, enabled) state.
	ColorTailscaleChipBorder = color.NRGBA{R: 0xe5, G: 0xf5, B: 0xb4, A: 0xff}
	ColorTailscaleChipLabel  = color.NRGBA{R: 0xc3, G: 0xc6, B: 0xb4, A: 0xff}

	// ColorLogoWordmark is the "USBridge" wordmark color next to the logo
	// mark in the app's header bars.
	ColorLogoWordmark = color.NRGBA{R: 0xe7, G: 0xfb, B: 0xba, A: 0xff}
)

const RadiusMD float32 = 8

const (
	ColorNameCodeKeyword fyne.ThemeColorName = "code-keyword"
	ColorNameCodeBuiltin fyne.ThemeColorName = "code-builtin"
	ColorNameCodeString  fyne.ThemeColorName = "code-string"
	ColorNameCodeComment fyne.ThemeColorName = "code-comment"
	ColorNameCodeNumber  fyne.ThemeColorName = "code-number"
	ColorNameCodeDefault fyne.ThemeColorName = "code-default"
)

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
		return ColorGray950
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
	case ColorNameCodeKeyword:
		return color.NRGBA{R: 0x56, G: 0x9C, B: 0xD6, A: 0xFF} // blue
	case ColorNameCodeBuiltin:
		return color.NRGBA{R: 0x4E, G: 0xC9, B: 0xB0, A: 0xFF} // teal
	case ColorNameCodeString:
		return color.NRGBA{R: 0xCE, G: 0x91, B: 0x78, A: 0xFF} // orange
	case ColorNameCodeComment:
		return color.NRGBA{R: 0x6A, G: 0x99, B: 0x55, A: 0xFF} // green
	case ColorNameCodeNumber:
		return color.NRGBA{R: 0xB5, G: 0xCE, B: 0xA8, A: 0xFF} // light green
	case ColorNameCodeDefault:
		return color.NRGBA{R: 0xD4, G: 0xD4, B: 0xD4, A: 0xFF} // light gray
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
