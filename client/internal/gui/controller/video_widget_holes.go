package controller

import "math"

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
