package localui

import "sort"

// Ported verbatim from usbridge/modules/ui_parser/labels.go -- pure math,
// no OpenCV involved. See that file for the full rationale.

const (
	labelMaxLineGap = 45.0
	labelMaxHOffset = 70.0
)

func associateLabels(icons []Icon, texts []TextRegion) {
	if len(icons) == 0 || len(texts) == 0 {
		return
	}
	claimed := make([]bool, len(texts))

	type candidate struct {
		iconIdx int
		textIdx int
		dist    float64
	}
	var insideCandidates []candidate
	for i, icon := range icons {
		for j, t := range texts {
			if overlapsH(icon.Bbox, t.Bbox) && overlapsV(icon.Bbox, t.Bbox) {
				insideCandidates = append(insideCandidates, candidate{i, j, hCenterDist(icon.Bbox, t.Bbox)})
			}
		}
	}
	sort.Slice(insideCandidates, func(a, b int) bool { return insideCandidates[a].dist < insideCandidates[b].dist })
	insideLabel := make(map[int]int)
	for _, c := range insideCandidates {
		if claimed[c.textIdx] {
			continue
		}
		if _, have := insideLabel[c.iconIdx]; have {
			continue
		}
		insideLabel[c.iconIdx] = c.textIdx
		claimed[c.textIdx] = true
	}
	for iconIdx, textIdx := range insideLabel {
		icons[iconIdx].Label = texts[textIdx].Text
	}

	for i := range icons {
		if icons[i].Label != "" {
			continue
		}
		cursor := icons[i].Bbox
		var lines []string
		for {
			bestIdx := -1
			bestDist := labelMaxLineGap + 1
			for j, t := range texts {
				if claimed[j] {
					continue
				}
				if t.Bbox.Y1 < cursor.Y2 {
					continue
				}
				gap := t.Bbox.Y1 - cursor.Y2
				if gap > labelMaxLineGap {
					continue
				}
				if hCenterDist(icons[i].Bbox, t.Bbox) > labelMaxHOffset {
					continue
				}
				if gap < bestDist {
					bestDist = gap
					bestIdx = j
				}
			}
			if bestIdx < 0 {
				break
			}
			claimed[bestIdx] = true
			lines = append(lines, texts[bestIdx].Text)
			cursor = texts[bestIdx].Bbox
			if len(lines) >= 3 {
				break
			}
		}
		if len(lines) > 0 {
			icons[i].Label = joinChunks(lines)
		}
	}
}

func overlapsH(a, b Box) bool { return a.X1 < b.X2 && a.X2 > b.X1 }
func overlapsV(a, b Box) bool { return a.Y1 < b.Y2 && a.Y2 > b.Y1 }

func hCenterDist(a, b Box) float64 {
	ac := (a.X1 + a.X2) / 2
	bc := (b.X1 + b.X2) / 2
	if ac > bc {
		return ac - bc
	}
	return bc - ac
}
