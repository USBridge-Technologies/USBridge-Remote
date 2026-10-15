package controller

import (
	"math"
	"slices"
	"time"

	"fyne.io/fyne/v2"

	"usbridge-client/internal/gui/view"
)

// Native video surfaces (Vulkan on Linux/Windows/Android, Metal on
// macOS/iOS) sit above the Fyne canvas, so a Fyne menu or popup opened over
// the video would be covered by it. Instead of hiding the video, each
// platform cuts the open overlays' rects out of the surface (XShape,
// SetWindowRgn, alpha 0 in the swapchain, a CALayer mask). The flow around
// that is shared here; only mapping a rect into the surface's coordinates
// and pushing it is per platform.

// overlayOcclusion is one native video surface's hole state. Fyne main
// goroutine only: the overlay stack isn't thread-safe.
type overlayOcclusion[H comparable] struct {
	setHidden func(bool)
	setHoles  func([]H)
	applied   []H // last list pushed to the surface (nil = none)
}

// reset forgets the pushed holes: a new native surface starts without any.
func (o *overlayOcclusion[H]) reset() { o.applied = nil }

// sync keeps Fyne overlays on canvas c visible over the surface: open
// menus/popups become holes (toHoles maps them into the surface's
// coordinates) so the rest of the video keeps playing; navigation away from
// Control, no canvas, or an overlay of unknown shape hides it outright.
// Reports whether the video shows and whether the hole list changed (the
// Fyne pixels under a fresh hole are only as fresh as its last paint).
func (o *overlayOcclusion[H]) sync(c fyne.Canvas, toHoles func([]view.OverlayRect) []H) (shown, changed bool) {
	if c == nil || view.NavVideoHidden() {
		o.setHidden(true)
		return false, false
	}
	rects, ok := view.OverlayHoleRects(c)
	if !ok {
		o.setHidden(true)
		return false, false
	}
	var holes []H
	if len(rects) > 0 {
		holes = toHoles(rects)
	}
	if !slices.Equal(holes, o.applied) {
		o.setHoles(holes)
		o.applied = holes
		changed = true
	}
	o.setHidden(false)
	return true, changed
}

// scheduleOcclusionSync runs sync on the Fyne main goroutine now and again
// shortly after: overlay show/hide fires from popup watchers off the main
// goroutine, often before the popup's panel has its first layout, and a
// paused stream has no frame tick to pick the final rect up.
func scheduleOcclusionSync(sync func()) {
	fyne.Do(sync)
	for _, d := range []time.Duration{60 * time.Millisecond, 250 * time.Millisecond} {
		time.AfterFunc(d, func() { fyne.Do(sync) })
	}
}

// overlayHolePx maps overlay rect r to surface pixels: shifted by (dx, dy)
// canvas units, scaled, then made relative to the surface's pixel origin
// (px, py), grown to whole pixels so no sliver of video covers the menu's
// edge. ok is false when it misses the w x h surface.
func overlayHolePx(r view.OverlayRect, dx, dy, scale float32, px, py, w, h int) (x, y, hw, hh int, ok bool) {
	x0 := int(math.Floor(float64((r.X+dx)*scale))) - px
	y0 := int(math.Floor(float64((r.Y+dy)*scale))) - py
	x1 := int(math.Ceil(float64((r.X+r.W+dx)*scale))) - px
	y1 := int(math.Ceil(float64((r.Y+r.H+dy)*scale))) - py
	if x1 <= 0 || y1 <= 0 || x0 >= w || y0 >= h {
		return 0, 0, 0, 0, false
	}
	return x0, y0, x1 - x0, y1 - y0, true
}

// appendRoundedHole adds a w×h hole at (x,y) with corners of radius r
// (pixels) as plain rectangles: one-pixel rows for the top and bottom r rows,
// each inset by the circle, plus one block for the straight middle. The
// video then fills the corners a rounded menu card leaves bare.
func appendRoundedHole(holes [][4]int, x, y, w, h, r int) [][4]int {
	if r > w/2 {
		r = w / 2
	}
	if r > h/2 {
		r = h / 2
	}
	if r <= 0 {
		return append(holes, [4]int{x, y, w, h})
	}
	rf := float64(r)
	for row := 0; row < r; row++ {
		dy := rf - float64(row) - 0.5
		inset := int(math.Floor(rf - math.Sqrt(rf*rf-dy*dy)))
		holes = append(holes,
			[4]int{x + inset, y + row, w - 2*inset, 1},
			[4]int{x + inset, y + h - 1 - row, w - 2*inset, 1})
	}
	return append(holes, [4]int{x, y + r, w, h - 2*r})
}
