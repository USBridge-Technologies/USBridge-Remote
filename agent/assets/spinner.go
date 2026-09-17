package assets

import (
	"fmt"

	"fyne.io/fyne/v2"
)

// LoadingMutedFrames is the 8-dot footer spinner in the version-tag olive.
var LoadingMutedFrames = buildDotSpinnerFrames("#c5c8b5", "loading-muted")

func buildDotSpinnerFrames(fill, namePrefix string) []fyne.Resource {
	type dot struct{ x, y float32 }
	dots := []dot{
		{8.0, 1.8},
		{11.95, 3.05},
		{14.2, 8.0},
		{11.95, 12.95},
		{8.0, 14.2},
		{4.05, 12.95},
		{1.8, 8.0},
		{4.05, 3.05},
	}
	alphas := []float32{1.0, 0.82, 0.64, 0.46, 0.32, 0.22, 0.16, 0.12}
	frames := make([]fyne.Resource, len(dots))
	for frame := range frames {
		svg := `<svg viewBox="0 0 16 16" xmlns="http://www.w3.org/2000/svg">`
		for idx, point := range dots {
			alpha := alphas[(idx-frame+len(dots))%len(dots)]
			svg += fmt.Sprintf(
				`<circle cx="%.2f" cy="%.2f" r="1.55" fill="%s" fill-opacity="%.2f"/>`,
				point.x, point.y, fill, alpha,
			)
		}
		svg += `</svg>`
		frames[frame] = fyne.NewStaticResource(
			fmt.Sprintf("%s-%02d.svg", namePrefix, frame),
			[]byte(svg),
		)
	}
	return frames
}
