package color

import (
	"image/color"
)

// ToNRGBA converts a color to RGBA values which are not premultiplied, unlike color.RGBA().
func ToNRGBA(c color.Color) (r, g, b, a uint8) {
	// We use UnmultiplyAlpha with RGBA, RGBA64, and unrecognized implementations of Color.
	// It works for all Colors whose RGBA() method is implemented according to spec, but is only necessary for those.
	// Only RGBA and RGBA64 have components which are already premultiplied.
	switch col := c.(type) {
	// NRGBA and NRGBA64 are not premultiplied
	case color.NRGBA:
		r = col.R
		g = col.G
		b = col.B
		a = col.A
	case *color.NRGBA:
		r = col.R
		g = col.G
		b = col.B
		a = col.A
	case color.NRGBA64:
		r = uint8(col.R >> 8)
		g = uint8(col.G >> 8)
		b = uint8(col.B >> 8)
		a = uint8(col.A >> 8)
	case *color.NRGBA64:
		r = uint8(col.R >> 8)
		g = uint8(col.G >> 8)
		b = uint8(col.B >> 8)
		a = uint8(col.A >> 8)
	// Gray and Gray16 have no alpha component
	case *color.Gray:
		r = col.Y
		g = col.Y
		b = col.Y
		a = 0xff
	case color.Gray:
		r = col.Y
		g = col.Y
		b = col.Y
		a = 0xff
	case *color.Gray16:
		r = uint8(col.Y >> 8)
		g = uint8(col.Y >> 8)
		b = uint8(col.Y >> 8)
		a = 0xff
	case color.Gray16:
		r = uint8(col.Y >> 8)
		g = uint8(col.Y >> 8)
		b = uint8(col.Y >> 8)
		a = 0xff
	// Alpha and Alpha16 contain only an alpha component.
	case color.Alpha:
		r = 0xff
		g = 0xff
		b = 0xff
		a = col.A
	case *color.Alpha:
		r = 0xff
		g = 0xff
		b = 0xff
		a = col.A
	case color.Alpha16:
		r = 0xff
		g = 0xff
		b = 0xff
		a = uint8(col.A >> 8)
	case *color.Alpha16:
		r = 0xff
		g = 0xff
		b = 0xff
		a = uint8(col.A >> 8)
	default:
		n, _ := color.NRGBAModel.Convert(c).(color.NRGBA)
		r, g, b, a = n.R, n.G, n.B, n.A
	}
	return r, g, b, a
}
