package design

import (
	"image/color"

	"fyne.io/fyne/v2"
	fynetheme "fyne.io/fyne/v2/theme"
)

// Palette matches the client (client/internal/gui/design/theme.go) so Agent
// and Client read as one product. Token names that already existed here
// (ColorPanel, ColorHeader, ColorBrandAccent, ColorError, …) are kept as
// aliases so existing UI code keeps compiling.
var (
	ColorBackground      = color.NRGBA{R: 0x00, G: 0x00, B: 0x00, A: 0xFF} // --cs-bg-color
	ColorSurface         = color.NRGBA{R: 0x11, G: 0x11, B: 0x11, A: 0xFF} // --cs-surface-color
	ColorSurfaceLight    = color.NRGBA{R: 0x35, G: 0x35, B: 0x35, A: 0xFF} // --cs-surface-light
	ColorInputBackground = color.NRGBA{R: 0x17, G: 0x17, B: 0x17, A: 0xFF} // --cs-input-bg-color
	ColorAccent          = color.NRGBA{R: 0x93, G: 0xC5, B: 0x72, A: 0xFF} // --cs-accent
	ColorAccentHover     = color.NRGBA{R: 0xB6, G: 0xEA, B: 0x93, A: 0xFF} // --cs-accent-hover
	ColorAccentSoft      = color.NRGBA{R: 0x93, G: 0xC5, B: 0x72, A: 0x38} // --cs-alpha-accent-22
	ColorTextLight       = color.NRGBA{R: 0xF5, G: 0xF5, B: 0xF5, A: 0xFF} // --cs-text-light
	ColorTextMuted       = color.NRGBA{R: 0xC9, G: 0xC9, B: 0xC9, A: 0xFF} // --cs-text-muted
	ColorBorder          = color.NRGBA{R: 0x65, G: 0x65, B: 0x65, A: 0xFF} // --cs-border-color
	ColorGray900         = color.NRGBA{R: 0x18, G: 0x1C, B: 0x1F, A: 0xFF} // headers & cards
	ColorGray950         = color.NRGBA{R: 0x0B, G: 0x0F, B: 0x12, A: 0xFF} // window
	ColorGray400         = color.NRGBA{R: 0xC8, G: 0xC8, B: 0xC8, A: 0xFF}
	ColorWhite           = color.NRGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF}

	// Tailscale header chip — same tokens as the client's header toggle.
	ColorTailscaleChipBorder = color.NRGBA{R: 0x42, G: 0x46, B: 0x38, A: 0xFF}
	ColorTailscaleChipLabel  = color.NRGBA{R: 0xC3, G: 0xC6, B: 0xB4, A: 0xFF}

	// ColorLogoutHover* is the Sign Out chip's hover — rose fill/stroke/label.
	ColorLogoutHoverFill   = color.NRGBA{R: 0x2E, G: 0x20, B: 0x24, A: 0xFF}
	ColorLogoutHoverStroke = color.NRGBA{R: 0x82, G: 0x34, B: 0x37, A: 0xFF}
	ColorLogoutHoverLabel  = color.NRGBA{R: 0xFD, G: 0xA4, B: 0xAF, A: 0xFF}

	// ColorPanel/ColorHeader are the names window.go already paints with.
	ColorPanel  = ColorGray950
	ColorHeader = ColorGray900

	ColorAlert        = color.NRGBA{R: 0xE9, G: 0x8A, B: 0x2B, A: 0xFF}
	ColorDanger       = color.NRGBA{R: 0xD9, G: 0x5C, B: 0x5C, A: 0xFF}
	ColorError        = ColorDanger
	ColorPro          = color.NRGBA{R: 0x9C, G: 0x58, B: 0xF9, A: 0xFF}
	ColorProIdle      = color.NRGBA{R: 0x70, G: 0x3F, B: 0xB4, A: 0xFF} // Buy Pro outline at rest
	ColorProSoft      = color.NRGBA{R: 0xB3, G: 0x9E, B: 0xF1, A: 0xFF} // Pro/Enterprise chrome
	ColorProSoftHover = color.NRGBA{R: 0xC4, G: 0xB4, B: 0xF6, A: 0xFF}
	ColorProLine      = color.NRGBA{R: 0x7B, G: 0x6E, B: 0xA0, A: 0xFF} // Pro header stripe + card hover
	ColorTealLine     = color.NRGBA{R: 0x2A, G: 0x87, B: 0x78, A: 0xFF} // Free header stripe + card hover

	ColorBrandLime       = color.NRGBA{R: 0xE8, G: 0xFC, B: 0xBA, A: 0xFF} // logo lockup
	ColorBrandLimeSoft   = color.NRGBA{R: 0xE9, G: 0xFD, B: 0xBB, A: 0xFF}
	ColorLoginAvatarBg   = color.NRGBA{R: 0x2D, G: 0x2F, B: 0x34, A: 0xFF}
	ColorLoginAvatarText = ColorBrandLimeSoft
	ColorCTA             = color.NRGBA{R: 0xC4, G: 0xE7, B: 0x7A, A: 0xFF}
	ColorCTAHover        = color.NRGBA{R: 0xD6, G: 0xF7, B: 0x9C, A: 0xFF}
	ColorCTALabel        = color.NRGBA{R: 0x4C, G: 0x68, B: 0x03, A: 0xFF}
	ColorTeal            = color.NRGBA{R: 0x41, G: 0xE0, B: 0xC3, A: 0xFF} // Agent identity
	ColorTealHover       = color.NRGBA{R: 0x61, G: 0xF0, B: 0xD3, A: 0xFF}
	ColorChromeOlive     = color.NRGBA{R: 0x42, G: 0x46, B: 0x38, A: 0xFF}
	ColorMutedOlive      = color.NRGBA{R: 0xC5, G: 0xC8, B: 0xB5, A: 0xFF}
	ColorEmptyHint       = color.NRGBA{R: 0x9A, G: 0x9D, B: 0x8C, A: 0xFF} // dimmer empty-state copy
	ColorAddress         = color.NRGBA{R: 0xEB, G: 0xFF, B: 0xBC, A: 0xFF} // LAN/TS host
	ColorSectionTitle    = color.NRGBA{R: 0xE0, G: 0xE3, B: 0xE7, A: 0xFF}
	ColorDivider         = color.NRGBA{R: 0x29, G: 0x2D, B: 0x27, A: 0xFF}
	ColorDialogSep       = color.NRGBA{R: 0x30, G: 0x34, B: 0x2E, A: 0xFF}
	ColorOverlayDim      = color.NRGBA{R: 0x00, G: 0x00, B: 0x00, A: 0x72}
	ColorScrollBar       = color.NRGBA{R: 0x5A, G: 0x5E, B: 0x62, A: 0xFF}

	ColorAlphaWhite15 = color.NRGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0x26}
	ColorAlphaWhite24 = color.NRGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0x3D}

	// ColorShadow is unused by the theme (shadows are off, matching the
	// client) but kept so older paint call sites still compile.
	ColorShadow = color.NRGBA{R: 0x00, G: 0x00, B: 0x00, A: 0x38}

	// ColorHover is the opaque hover fill for custom widgets that set
	// their background directly (iconActionButton). Stock widget.Button
	// hover must stay translucent — see ColorHoverOverlay.
	ColorHover = ColorSurfaceLight

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
	ColorHoverOverlay = ColorAlphaWhite15

	// ColorBrandAccent/ColorBrandAccentHover back the supportButton widget
	// (agent/internal/ui.newSupportButton) — lime CTA matching the client's
	// Connect/Add buttons, dark-olive text, a touch lighter on hover.
	ColorBrandAccent      = ColorCTA
	ColorBrandAccentHover = ColorCTAHover
)

const RadiusMD float32 = 8
const RadiusLG float32 = 10

// Extra Color() names so Fyne's NewColoredResource can tint glyphs to our
// palette (info/copy #e9fdbb, edit/refresh #c5c8b5) without string-munging SVGs.
const (
	ColorNameBrandLimeSoft fyne.ThemeColorName = "brandLimeSoft"
	ColorNameMutedOlive    fyne.ThemeColorName = "mutedOlive"
)

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
	case fynetheme.ColorNameMenuBackground:
		return ColorGray950
	case fynetheme.ColorNameOverlayBackground:
		// Translucent dim so widget.PopUp shows the window behind it,
		// matching the client's overlay treatment. Dialog cards paint
		// their own opaque Gray900 fill on top.
		return ColorOverlayDim
	case fynetheme.ColorNameButton:
		return ColorSurfaceLight
	case fynetheme.ColorNameDisabled:
		return ColorBorder
	case fynetheme.ColorNameDisabledButton:
		return ColorGray900
	case fynetheme.ColorNameFocus:
		return ColorAccentSoft
	case fynetheme.ColorNameForeground:
		return ColorTextLight
	case fynetheme.ColorNameForegroundOnPrimary:
		return ColorBackground
	case fynetheme.ColorNameHeaderBackground:
		return ColorGray900
	case fynetheme.ColorNameHover:
		return ColorHoverOverlay
	case fynetheme.ColorNameHyperlink:
		return ColorTeal
	case fynetheme.ColorNameInputBackground:
		return ColorInputBackground
	case fynetheme.ColorNameInputBorder:
		return ColorBorder
	case fynetheme.ColorNamePlaceHolder:
		return ColorTextMuted
	case fynetheme.ColorNamePressed:
		return ColorAlphaWhite24
	case fynetheme.ColorNamePrimary:
		return ColorAccent
	case fynetheme.ColorNameScrollBar:
		return ColorScrollBar
	case fynetheme.ColorNameScrollBarBackground:
		return ColorGray950
	case fynetheme.ColorNameSelection:
		return ColorAccentSoft
	case fynetheme.ColorNameSeparator:
		return ColorDialogSep
	case fynetheme.ColorNameShadow:
		return color.Transparent
	case fynetheme.ColorNameSuccess:
		return ColorAccent
	case fynetheme.ColorNameWarning:
		return ColorAlert
	case fynetheme.ColorNameError:
		return ColorDanger
	case ColorNameBrandLimeSoft:
		return ColorBrandLimeSoft
	case ColorNameMutedOlive:
		return ColorMutedOlive
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
	case fynetheme.SizeNameScrollBar:
		return 6
	case fynetheme.SizeNameScrollBarSmall:
		return 3
	}
	return t.fallback.Size(name)
}
