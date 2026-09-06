package localui

// Ported verbatim (pure math, no OpenCV involved) from
// usbridge/modules/ui_parser/tile.go -- see that file for the full
// rationale on why DBNet's fixed 960x960 input gets tiled at native
// resolution instead of downscaled for screenshots larger than 960px.

type rect struct{ X1, Y1, X2, Y2 int }

func tileRects(imgW, imgH, tileSize, overlap int) []rect {
	if imgW < tileSize || imgH < tileSize {
		return nil
	}
	xs := axisStarts(imgW, tileSize, overlap)
	ys := axisStarts(imgH, tileSize, overlap)
	rects := make([]rect, 0, len(xs)*len(ys))
	for _, y := range ys {
		for _, x := range xs {
			rects = append(rects, rect{X1: x, Y1: y, X2: x + tileSize, Y2: y + tileSize})
		}
	}
	return rects
}

func axisStarts(dim, tileSize, overlap int) []int {
	step := tileSize - overlap
	if step <= 0 {
		step = tileSize
	}
	var starts []int
	x := 0
	for {
		if x+tileSize >= dim {
			starts = append(starts, dim-tileSize)
			break
		}
		starts = append(starts, x)
		x += step
	}
	return starts
}

// mergeOverlappingBoxes drops boxes largely redundant with a larger
// already-kept box (IoU > iouThresh) -- collapses tiling's deliberate
// overlap-duplicate detections back to one box.
func mergeOverlappingBoxes(boxes []Box, iouThresh float64) []Box {
	if len(boxes) <= 1 {
		return boxes
	}
	order := make([]int, len(boxes))
	for i := range order {
		order[i] = i
	}
	area := func(b Box) float64 { return (b.X2 - b.X1) * (b.Y2 - b.Y1) }
	for i := 0; i < len(order); i++ {
		for j := i + 1; j < len(order); j++ {
			if area(boxes[order[j]]) > area(boxes[order[i]]) {
				order[i], order[j] = order[j], order[i]
			}
		}
	}

	var kept []Box
	for _, idx := range order {
		b := boxes[idx]
		redundant := false
		for _, k := range kept {
			if boxIoU(b, k) > iouThresh {
				redundant = true
				break
			}
		}
		if !redundant {
			kept = append(kept, b)
		}
	}
	return kept
}

func boxIoU(a, b Box) float64 {
	x1 := max64(a.X1, b.X1)
	y1 := max64(a.Y1, b.Y1)
	x2 := min64(a.X2, b.X2)
	y2 := min64(a.Y2, b.Y2)
	interW := x2 - x1
	interH := y2 - y1
	if interW <= 0 || interH <= 0 {
		return 0
	}
	inter := interW * interH
	areaA := (a.X2 - a.X1) * (a.Y2 - a.Y1)
	areaB := (b.X2 - b.X1) * (b.Y2 - b.Y1)
	union := areaA + areaB - inter
	if union <= 0 {
		return 0
	}
	return inter / union
}

func max64(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func min64(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func joinChunks(parts []string) string {
	out := parts[0]
	for _, p := range parts[1:] {
		out += " " + p
	}
	return out
}

func clampF(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
