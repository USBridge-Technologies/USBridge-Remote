package design

import (
	_ "embed"

	"fyne.io/fyne/v2"
)

// Inter 4.1 (SIL OFL, see fonts/OFL-Inter.txt). Fyne's bundled Noto Sans
// applies a GSUB substitute that draws Ukrainian/Belarusian і (U+0456)
// without its tittle; Inter keeps the dotted glyph.

//go:embed fonts/Inter-Regular.ttf
var interRegular []byte

//go:embed fonts/Inter-Bold.ttf
var interBold []byte

//go:embed fonts/Inter-Italic.ttf
var interItalic []byte

//go:embed fonts/Inter-BoldItalic.ttf
var interBoldItalic []byte

var (
	fontRegular    = &fyne.StaticResource{StaticName: "Inter-Regular.ttf", StaticContent: interRegular}
	fontBold       = &fyne.StaticResource{StaticName: "Inter-Bold.ttf", StaticContent: interBold}
	fontItalic     = &fyne.StaticResource{StaticName: "Inter-Italic.ttf", StaticContent: interItalic}
	fontBoldItalic = &fyne.StaticResource{StaticName: "Inter-BoldItalic.ttf", StaticContent: interBoldItalic}
)

func (t *BrandTheme) Font(style fyne.TextStyle) fyne.Resource {
	if style.Monospace || style.Symbol {
		return t.fallback.Font(style)
	}
	switch {
	case style.Bold && style.Italic:
		return fontBoldItalic
	case style.Bold:
		return fontBold
	case style.Italic:
		return fontItalic
	default:
		return fontRegular
	}
}
