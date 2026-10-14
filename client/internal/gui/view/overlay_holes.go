package view

import (
	"reflect"

	"usbridge-client/internal/gui/design"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
)

// OverlayRect is a canvas-absolute rectangle in Fyne units; R is its
// corner radius, so a hole can follow a rounded card.
type OverlayRect struct {
	X, Y, W, H, R float32
}

// OverlayHoleRects returns the on-screen rects of every overlay currently
// open on c: the menu card of a header dropdown, the panel (and footer) of
// an OverlayPopupSpec popup, the content of a Fyne PopUp / PopUpMenu. A
// native video layer that can cut holes punches these out instead of hiding
// itself, so the menu sits "on top" of live video.
//
// ok is false when an overlay of an unknown shape is open; the caller must
// then fall back to hiding the video entirely.
func OverlayHoleRects(c fyne.Canvas) (rects []OverlayRect, ok bool) {
	if c == nil {
		return nil, true
	}
	for _, o := range c.Overlays().List() {
		if o == nil || !o.Visible() {
			continue
		}
		r, known := overlayRectsFor(o)
		if !known {
			return nil, false
		}
		rects = append(rects, r...)
	}
	return rects, true
}

func overlayRectsFor(o fyne.CanvasObject) ([]OverlayRect, bool) {
	if p, isDropdown := o.(*dropdownPopup); isDropdown {
		return []OverlayRect{{p.pos.X, p.pos.Y, p.size.Width, p.size.Height, design.RadiusMD}}, true
	}
	content := overlayContent(o)
	if content == nil {
		return nil, false
	}
	// NewOverlayPopup: the PopUp's content is a full-window dim + panel
	// (+ footer); only the panel and footer are opaque UI.
	if box, isBox := content.(*fyne.Container); isBox {
		if _, isOverlay := box.Layout.(*overlayPopupLayout); isOverlay {
			var out []OverlayRect
			for i, obj := range box.Objects {
				if i == 0 || obj == nil || !obj.Visible() {
					continue // [0] is the dim
				}
				r := absRect(obj)
				if i == 1 {
					r.R = design.RadiusMD // the panel card
				}
				out = append(out, r)
			}
			return out, true
		}
	}
	// ShowDimOverlay: a bare full-window dim rectangle, nothing to keep clear.
	if isFullCanvasDim(o, content) {
		return nil, true
	}
	r := absRect(content)
	r.R = design.RadiusMD // Fyne's popup/menu background uses the input radius
	return []OverlayRect{r}, true
}

// overlayContent reads the exported Content field that widget.PopUp and
// Fyne's internal OverlayContainer (behind PopUpMenu) both carry.
func overlayContent(o fyne.CanvasObject) fyne.CanvasObject {
	v := reflect.ValueOf(o)
	if v.Kind() != reflect.Pointer || v.IsNil() {
		return nil
	}
	v = v.Elem()
	if v.Kind() != reflect.Struct {
		return nil
	}
	f := v.FieldByName("Content")
	if !f.IsValid() || !f.CanInterface() {
		return nil
	}
	obj, _ := f.Interface().(fyne.CanvasObject)
	return obj
}

func isFullCanvasDim(o, content fyne.CanvasObject) bool {
	if _, isRect := content.(*canvas.Rectangle); !isRect {
		return false
	}
	cs, os := content.Size(), o.Size()
	return cs.Width >= os.Width-1 && cs.Height >= os.Height-1
}

func absRect(obj fyne.CanvasObject) OverlayRect {
	pos := fyne.CurrentApp().Driver().AbsolutePositionForObject(obj)
	sz := obj.Size()
	return OverlayRect{X: pos.X, Y: pos.Y, W: sz.Width, H: sz.Height}
}
