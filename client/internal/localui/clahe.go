package localui

// claheGray is a from-scratch CLAHE (Contrast Limited Adaptive Histogram
// Equalization) implementation matching gocv.NewCLAHEWithParams(2.0,
// image.Pt(8,8))'s parameters (clipLimit=2.0, an 8x8 tile grid) -- the same
// values usbridge/modules/ui_parser/draw.go's applyGrayCLAHE/applyCLAHE use
// on the device. Standard algorithm: per-tile histogram, clip at
// clipLimit*(tile pixel count/256) with the excess redistributed evenly
// across all 256 bins, then bilinear-interpolate each pixel's output value
// between its 4 nearest tiles' equalization curves.
func claheGray(gray []uint8, w, h int, clipLimit float64, tilesX, tilesY int) []uint8 {
	tileW := (w + tilesX - 1) / tilesX
	tileH := (h + tilesY - 1) / tilesY

	// mappings[ty][tx] is a 256-entry LUT mapping input gray value -> CLAHE
	// output value for that tile.
	mappings := make([][][256]uint8, tilesY)
	for ty := 0; ty < tilesY; ty++ {
		mappings[ty] = make([][256]uint8, tilesX)
		for tx := 0; tx < tilesX; tx++ {
			x0, y0 := tx*tileW, ty*tileH
			x1, y1 := minInt(x0+tileW, w), minInt(y0+tileH, h)

			var hist [256]int
			count := 0
			for y := y0; y < y1; y++ {
				row := y * w
				for x := x0; x < x1; x++ {
					hist[gray[row+x]]++
					count++
				}
			}
			if count == 0 {
				for i := 0; i < 256; i++ {
					mappings[ty][tx][i] = uint8(i)
				}
				continue
			}

			clipAt := int(clipLimit * float64(count) / 256.0)
			if clipAt < 1 {
				clipAt = 1
			}
			excess := 0
			for i := 0; i < 256; i++ {
				if hist[i] > clipAt {
					excess += hist[i] - clipAt
					hist[i] = clipAt
				}
			}
			redist := excess / 256
			rem := excess - redist*256
			for i := 0; i < 256; i++ {
				hist[i] += redist
				if i < rem {
					hist[i]++
				}
			}

			var cdf [256]int
			running := 0
			for i := 0; i < 256; i++ {
				running += hist[i]
				cdf[i] = running
			}
			scale := 255.0 / float64(count)
			for i := 0; i < 256; i++ {
				mappings[ty][tx][i] = clampU8(float64(cdf[i]) * scale)
			}
		}
	}

	out := make([]uint8, w*h)
	// Tile centers, used as the bilinear interpolation grid.
	centerX := func(tx int) float64 { return float64(tx)*float64(tileW) + float64(tileW)/2 }
	centerY := func(ty int) float64 { return float64(ty)*float64(tileH) + float64(tileH)/2 }

	for y := 0; y < h; y++ {
		fy := float64(y)
		ty0 := int((fy - float64(tileH)/2) / float64(tileH))
		if ty0 < 0 {
			ty0 = 0
		}
		ty1 := ty0 + 1
		if ty1 >= tilesY {
			ty1 = tilesY - 1
		}
		if ty0 >= tilesY {
			ty0 = tilesY - 1
		}
		denomY := centerY(ty1) - centerY(ty0)
		wy := 0.0
		if denomY > 0 {
			wy = (fy - centerY(ty0)) / denomY
		}
		wy = clampF(wy, 0, 1)

		for x := 0; x < w; x++ {
			fx := float64(x)
			tx0 := int((fx - float64(tileW)/2) / float64(tileW))
			if tx0 < 0 {
				tx0 = 0
			}
			tx1 := tx0 + 1
			if tx1 >= tilesX {
				tx1 = tilesX - 1
			}
			if tx0 >= tilesX {
				tx0 = tilesX - 1
			}
			denomX := centerX(tx1) - centerX(tx0)
			wx := 0.0
			if denomX > 0 {
				wx = (fx - centerX(tx0)) / denomX
			}
			wx = clampF(wx, 0, 1)

			v := gray[y*w+x]
			v00 := float64(mappings[ty0][tx0][v])
			v01 := float64(mappings[ty0][tx1][v])
			v10 := float64(mappings[ty1][tx0][v])
			v11 := float64(mappings[ty1][tx1][v])
			top := v00 + (v01-v00)*wx
			bot := v10 + (v11-v10)*wx
			out[y*w+x] = clampU8(top + (bot-top)*wy)
		}
	}
	return out
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// applyGrayCLAHE mirrors usbridge/modules/ui_parser/draw.go's
// applyGrayCLAHE: grayscale + CLAHE, replicated back to 3 channels --
// paddle_dbnet's actual input.
func applyGrayCLAHE(src *rgbImage) *rgbImage {
	gray := src.toGray()
	eq := claheGray(gray, src.W, src.H, 2.0, 8, 8)
	return grayToRGB3(eq, src.W, src.H)
}

// applyCLAHE approximates usbridge/modules/ui_parser/draw.go's applyCLAHE
// (LAB-space CLAHE on the L channel only, color preserved) without a full
// RGB<->LAB round trip: apply CLAHE to luma and rescale each RGB channel by
// the same per-pixel gain the luma channel got. Close enough for
// icon_detect's input -- YOLO here runs at a permissive 0.05 confidence
// threshold (see yolo.go) and CLAHE's whole role is boosting local
// contrast, not exact color fidelity.
func applyCLAHE(src *rgbImage) *rgbImage {
	gray := src.toGray()
	eq := claheGray(gray, src.W, src.H, 2.0, 8, 8)
	out := newRGBImage(src.W, src.H)
	for i := 0; i < src.W*src.H; i++ {
		g := float64(gray[i])
		gain := 1.0
		if g > 1 {
			gain = float64(eq[i]) / g
		}
		out.Pix[i*3] = clampU8(float64(src.Pix[i*3]) * gain)
		out.Pix[i*3+1] = clampU8(float64(src.Pix[i*3+1]) * gain)
		out.Pix[i*3+2] = clampU8(float64(src.Pix[i*3+2]) * gain)
	}
	return out
}
