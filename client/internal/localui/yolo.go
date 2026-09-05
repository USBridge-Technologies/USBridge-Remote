package localui

import "sort"

const (
	yoloConfThresh = 0.05
	yoloIOUThresh  = 0.1
)

// decodeYOLO mirrors usbridge/modules/ui_parser/yolo.go's: icon_detect's
// raw (1,5,8400) output (cx,cy,w,h,conf, channel-major) decoded into boxes
// then NMS'd. gocv.NMSBoxes is replaced with a plain greedy IoU-based NMS
// (standard algorithm, same confidence/IoU thresholds).
func decodeYOLO(raw []float32) []Icon {
	const numAnchors = 8400
	if len(raw) != 5*numAnchors {
		return nil
	}
	cx := raw[0*numAnchors : 1*numAnchors]
	cy := raw[1*numAnchors : 2*numAnchors]
	w := raw[2*numAnchors : 3*numAnchors]
	h := raw[3*numAnchors : 4*numAnchors]
	conf := raw[4*numAnchors : 5*numAnchors]

	var boxes []Box
	var scores []float32
	for i := 0; i < numAnchors; i++ {
		if conf[i] <= yoloConfThresh {
			continue
		}
		boxes = append(boxes, Box{
			X1: float64(cx[i] - w[i]/2),
			Y1: float64(cy[i] - h[i]/2),
			X2: float64(cx[i] + w[i]/2),
			Y2: float64(cy[i] + h[i]/2),
		})
		scores = append(scores, conf[i])
	}
	if len(boxes) == 0 {
		return nil
	}

	keep := nmsIndices(boxes, scores, yoloIOUThresh)
	icons := make([]Icon, 0, len(keep))
	for _, idx := range keep {
		icons = append(icons, Icon{Bbox: boxes[idx], Confidence: float64(scores[idx])})
	}
	return icons
}

// nmsIndices is standard greedy non-max suppression: sort by score
// descending, then keep a box only if its IoU with every already-kept box
// is below iouThresh.
func nmsIndices(boxes []Box, scores []float32, iouThresh float64) []int {
	order := make([]int, len(boxes))
	for i := range order {
		order[i] = i
	}
	sort.Slice(order, func(a, b int) bool { return scores[order[a]] > scores[order[b]] })

	var kept []int
	for _, idx := range order {
		redundant := false
		for _, k := range kept {
			if boxIoU(boxes[idx], boxes[k]) > iouThresh {
				redundant = true
				break
			}
		}
		if !redundant {
			kept = append(kept, idx)
		}
	}
	return kept
}
