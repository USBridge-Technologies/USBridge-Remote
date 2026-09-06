package localui

import (
	_ "embed"
	"math"
	"strings"
)

//go:embed cyrillic_dict.txt
var svtrDictRaw string

// loadSVTRDict -- see usbridge/modules/ui_parser/svtr.go's doc comment for
// the full class-count/blank-index rationale. Identical here.
func loadSVTRDict() []string {
	lines := strings.Split(strings.TrimRight(svtrDictRaw, "\n"), "\n")
	chars := make([]string, len(lines))
	for i, l := range lines {
		chars[i] = strings.TrimRight(l, "\r")
	}
	return chars
}

const (
	svtrHeight     = 48
	svtrWidth      = 320
	svtrTimeSteps  = 40
	svtrNumClasses = 165
)

// preprocessSVTRCrop mirrors usbridge/modules/ui_parser/svtr.go's: resize
// to height 48 (aspect-preserving, width capped at 320), then right-pad by
// replicating the last column.
func preprocessSVTRCrop(crop *rgbImage) []float32 {
	w, h := crop.W, crop.H
	resizedW := svtrWidth
	if h > 0 {
		rw := int(math.Ceil(float64(svtrHeight) * float64(w) / float64(h)))
		if rw < svtrWidth {
			resizedW = rw
		}
	}
	if resizedW < 1 {
		resizedW = 1
	}
	resized := crop.resize(resizedW, svtrHeight)

	canvas := newRGBImage(svtrWidth, svtrHeight)
	for y := 0; y < svtrHeight; y++ {
		var lastPixel [3]uint8
		for x := 0; x < svtrWidth; x++ {
			var px [3]uint8
			if x < resizedW {
				srcOff := (y*resizedW + x) * 3
				px = [3]uint8{resized.Pix[srcOff], resized.Pix[srcOff+1], resized.Pix[srcOff+2]}
				lastPixel = px
			} else {
				px = lastPixel
			}
			dstOff := (y*svtrWidth + x) * 3
			canvas.Pix[dstOff] = px[0]
			canvas.Pix[dstOff+1] = px[1]
			canvas.Pix[dstOff+2] = px[2]
		}
	}
	return canvas.toNCHWFloat()
}

// ctcGreedyDecodeSVTR -- identical algorithm and convention to
// usbridge/modules/ui_parser/svtr.go's (blank=index 0, dict[idx-1] for
// idx in [1,163], confidence is the raw per-timestep max since the
// exported graph's softmax is already baked in).
func ctcGreedyDecodeSVTR(logits []float32, dict []string) (string, float64) {
	if len(logits) != svtrTimeSteps*svtrNumClasses {
		return "", 0
	}
	var text []rune
	var confSum float64
	var confCount int
	prevIdx := -1
	for t := 0; t < svtrTimeSteps; t++ {
		row := logits[t*svtrNumClasses : (t+1)*svtrNumClasses]
		best, bestV := 0, row[0]
		for c := 1; c < svtrNumClasses; c++ {
			if row[c] > bestV {
				best, bestV = c, row[c]
			}
		}
		isBlank := best == 0 || best > len(dict)
		if !isBlank && best != prevIdx {
			text = append(text, []rune(dict[best-1])...)
			confSum += float64(bestV)
			confCount++
		}
		prevIdx = best
	}
	avgConf := 0.0
	if confCount > 0 {
		avgConf = confSum / float64(confCount)
	}
	return string(text), avgConf
}
