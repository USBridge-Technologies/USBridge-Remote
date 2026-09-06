package localui

// Ported verbatim (pure math) from usbridge/modules/ui_parser/density.go --
// see that file's doc comment for the full rationale. Kept identical so a
// caller sees the same zoom_hints behavior regardless of which backend
// (device NPU or this local offload) answered ui.parse.
const (
	zoomHintMaxIconSide  = 56.0
	zoomHintGapRatio     = 0.6
	zoomHintMinCluster   = 3
	zoomHintPadding      = 12.0
	zoomHintMaxHintCount = 6
)

func findZoomHints(icons []Icon) []Box {
	type node struct {
		box     Box
		visited bool
	}
	var candidates []node
	for _, ic := range icons {
		w := ic.Bbox.X2 - ic.Bbox.X1
		h := ic.Bbox.Y2 - ic.Bbox.Y1
		if w <= 0 || h <= 0 || w > zoomHintMaxIconSide || h > zoomHintMaxIconSide {
			continue
		}
		candidates = append(candidates, node{box: ic.Bbox})
	}
	if len(candidates) < zoomHintMinCluster {
		return nil
	}

	packed := func(a, b Box) bool {
		gapX := gapBetween(a.X1, a.X2, b.X1, b.X2)
		gapY := gapBetween(a.Y1, a.Y2, b.Y1, b.Y2)
		if gapX < 0 {
			gapX = 0
		}
		if gapY < 0 {
			gapY = 0
		}
		avgSize := ((a.X2 - a.X1) + (a.Y2 - a.Y1) + (b.X2 - b.X1) + (b.Y2 - b.Y1)) / 4
		if avgSize <= 0 {
			return false
		}
		return (gapX <= avgSize*zoomHintGapRatio && gapY <= avgSize*2) ||
			(gapY <= avgSize*zoomHintGapRatio && gapX <= avgSize*2)
	}

	var hints []Box
	for i := range candidates {
		if candidates[i].visited {
			continue
		}
		queue := []int{i}
		candidates[i].visited = true
		var cluster []Box
		for len(queue) > 0 {
			cur := queue[len(queue)-1]
			queue = queue[:len(queue)-1]
			cluster = append(cluster, candidates[cur].box)
			for j := range candidates {
				if candidates[j].visited {
					continue
				}
				if packed(candidates[cur].box, candidates[j].box) {
					candidates[j].visited = true
					queue = append(queue, j)
				}
			}
		}
		if len(cluster) < zoomHintMinCluster {
			continue
		}
		x1, y1, x2, y2 := cluster[0].X1, cluster[0].Y1, cluster[0].X2, cluster[0].Y2
		for _, b := range cluster[1:] {
			x1 = min64(x1, b.X1)
			y1 = min64(y1, b.Y1)
			x2 = max64(x2, b.X2)
			y2 = max64(y2, b.Y2)
		}
		hints = append(hints, Box{X1: x1 - zoomHintPadding, Y1: y1 - zoomHintPadding, X2: x2 + zoomHintPadding, Y2: y2 + zoomHintPadding})
		if len(hints) >= zoomHintMaxHintCount {
			break
		}
	}
	return hints
}

func gapBetween(a1, a2, b1, b2 float64) float64 {
	if b1 > a2 {
		return b1 - a2
	}
	if a1 > b2 {
		return a1 - b2
	}
	return -1
}
