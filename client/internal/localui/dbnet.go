package localui

const (
	dbnetMapSize     = 960
	dbnetThresh      = 0.2
	dbnetMinSide     = 2
	dbnetUnclipRatio = 1.5
	dbnetTileOverlap = 150
)

// decodeDBNet mirrors usbridge/modules/ui_parser/dbnet.go's decodeDBNet:
// binarize the probability map, dilate with a 21x3 kernel to bridge
// word-gaps, then extract each blob's axis-aligned bounding box and
// "unclip" (expand) it by DB's own area/perimeter*ratio formula. The
// device version uses gocv.FindContours+MinAreaRect; since
// usbridge/modules/ui_parser/types.go's Box is already axis-aligned (no
// rotation stored), a plain connected-components pass + bounding-box
// tracking produces the same boxes without needing real contour tracing.
func decodeDBNet(raw []float32) []Box {
	if len(raw) != dbnetMapSize*dbnetMapSize {
		return nil
	}
	size := dbnetMapSize

	mask := make([]bool, size*size)
	for i, v := range raw {
		mask[i] = v > dbnetThresh
	}

	dilated := dilateRect(mask, size, size, 21, 3)

	boxes := connectedComponentBoxes(dilated, size, size)

	var out []Box
	for _, b := range boxes {
		bw := float64(b.X2 - b.X1)
		bh := float64(b.Y2 - b.Y1)
		if bw < dbnetMinSide || bh < dbnetMinSide {
			continue
		}
		area := bw * bh
		perimeter := 2 * (bw + bh)
		if perimeter == 0 {
			continue
		}
		d := area * dbnetUnclipRatio / perimeter

		x1 := float64(b.X1) - d
		y1 := float64(b.Y1) - d
		x2 := float64(b.X2) + d
		y2 := float64(b.Y2) + d
		if x1 < 0 {
			x1 = 0
		}
		if y1 < 0 {
			y1 = 0
		}
		if x2 > float64(size) {
			x2 = float64(size)
		}
		if y2 > float64(size) {
			y2 = float64(size)
		}
		out = append(out, Box{X1: x1, Y1: y1, X2: x2, Y2: y2})
	}
	return out
}

// dilateRect performs binary dilation with a kw x kh rectangular
// structuring element (separable: horizontal pass then vertical pass,
// equivalent to a single rect-kernel dilation and much cheaper than the
// naive O(w*h*kw*kh)).
func dilateRect(mask []bool, w, h, kw, kh int) []bool {
	halfW, halfH := kw/2, kh/2
	tmp := make([]bool, w*h)
	for y := 0; y < h; y++ {
		row := y * w
		for x := 0; x < w; x++ {
			set := false
			for dx := -halfW; dx <= halfW && !set; dx++ {
				xx := x + dx
				if xx < 0 || xx >= w {
					continue
				}
				if mask[row+xx] {
					set = true
				}
			}
			tmp[row+x] = set
		}
	}
	out := make([]bool, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			set := false
			for dy := -halfH; dy <= halfH && !set; dy++ {
				yy := y + dy
				if yy < 0 || yy >= h {
					continue
				}
				if tmp[yy*w+x] {
					set = true
				}
			}
			out[y*w+x] = set
		}
	}
	return out
}

type intBox struct{ X1, Y1, X2, Y2 int }

// connectedComponentBoxes labels 8-connected true regions of mask (a
// w*h boolean grid, row-major) via BFS flood fill and returns each
// component's bounding box -- the axis-aligned equivalent of
// gocv.FindContours(RetrievalExternal) + boundingRect per contour.
func connectedComponentBoxes(mask []bool, w, h int) []intBox {
	visited := make([]bool, w*h)
	var boxes []intBox
	queue := make([]int, 0, 1024)

	for start := 0; start < w*h; start++ {
		if !mask[start] || visited[start] {
			continue
		}
		visited[start] = true
		queue = queue[:0]
		queue = append(queue, start)
		x1, y1 := start%w, start/w
		x2, y2 := x1, y1

		for len(queue) > 0 {
			p := queue[len(queue)-1]
			queue = queue[:len(queue)-1]
			px, py := p%w, p/w
			if px < x1 {
				x1 = px
			}
			if px > x2 {
				x2 = px
			}
			if py < y1 {
				y1 = py
			}
			if py > y2 {
				y2 = py
			}
			for dy := -1; dy <= 1; dy++ {
				ny := py + dy
				if ny < 0 || ny >= h {
					continue
				}
				for dx := -1; dx <= 1; dx++ {
					nx := px + dx
					if nx < 0 || nx >= w || (dx == 0 && dy == 0) {
						continue
					}
					np := ny*w + nx
					if mask[np] && !visited[np] {
						visited[np] = true
						queue = append(queue, np)
					}
				}
			}
		}
		boxes = append(boxes, intBox{X1: x1, Y1: y1, X2: x2 + 1, Y2: y2 + 1})
	}
	return boxes
}
