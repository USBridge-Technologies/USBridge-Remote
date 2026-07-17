package view

import (
	"fmt"
	"image"
	"image/color"
	"math"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/widget"
)

// CircularProgress is a custom circular progress-indicator widget
type CircularProgress struct {
	widget.BaseWidget
	progress float64 // 0-100
	speed    float64 // MB/s
	size     float32
}

// NewCircularProgress creates a new circular progress indicator
func NewCircularProgress(size float32) *CircularProgress {
	cp := &CircularProgress{
		size: size,
	}
	cp.ExtendBaseWidget(cp)
	return cp
}

// SetProgress sets the progress (0-100)
func (cp *CircularProgress) SetProgress(progress float64) {
	cp.progress = progress
	cp.Refresh()
}

// SetSpeed sets the speed (MB/s)
func (cp *CircularProgress) SetSpeed(speed float64) {
	cp.speed = speed
	cp.Refresh()
}

// CreateRenderer creates the renderer for the widget
func (cp *CircularProgress) CreateRenderer() fyne.WidgetRenderer {
	// Background circle
	bgCircle := canvas.NewCircle(color.NRGBA{R: 200, G: 200, B: 200, A: 100})

	// Progress arc
	progressCircle := canvas.NewCircle(color.NRGBA{R: 76, G: 175, B: 80, A: 255})

	// Speed text
	speedText := canvas.NewText("", color.NRGBA{R: 50, G: 50, B: 50, A: 255})
	speedText.Alignment = fyne.TextAlignCenter
	speedText.TextSize = 8
	speedText.TextStyle.Bold = true

	return &circularProgressRenderer{
		cp:             cp,
		bgCircle:       bgCircle,
		progressCircle: progressCircle,
		speedText:      speedText,
		objects:        []fyne.CanvasObject{bgCircle, progressCircle, speedText},
	}
}

// MinSize returns the minimum size of the widget
func (cp *CircularProgress) MinSize() fyne.Size {
	return fyne.NewSize(cp.size, cp.size)
}

type circularProgressRenderer struct {
	cp             *CircularProgress
	bgCircle       *canvas.Circle
	progressCircle *canvas.Circle
	speedText      *canvas.Text
	objects        []fyne.CanvasObject
}

func (r *circularProgressRenderer) Layout(size fyne.Size) {
	// Center the circle
	r.bgCircle.Resize(size)
	r.bgCircle.Move(fyne.NewPos(0, 0))

	r.progressCircle.Resize(size)
	r.progressCircle.Move(fyne.NewPos(0, 0))

	// Position the text in the center
	textSize := r.speedText.MinSize()
	r.speedText.Move(fyne.NewPos((size.Width-textSize.Width)/2, (size.Height-textSize.Height)/2))
}

func (r *circularProgressRenderer) MinSize() fyne.Size {
	return r.cp.MinSize()
}

func (r *circularProgressRenderer) Refresh() {
	// Update the speed text
	if r.cp.speed > 0 {
		r.speedText.Text = fmt.Sprintf("%.1f", r.cp.speed)
	} else {
		r.speedText.Text = ""
	}

	// Update the progress circle's color based on progress
	percent := r.cp.progress / 100.0
	if percent >= 1.0 {
		r.progressCircle.FillColor = color.NRGBA{R: 76, G: 175, B: 80, A: 255} // Green
	} else {
		r.progressCircle.FillColor = color.NRGBA{R: 33, G: 150, B: 243, A: uint8(255 * percent)}
	}

	canvas.Refresh(r.bgCircle)
	canvas.Refresh(r.progressCircle)
	canvas.Refresh(r.speedText)
}

func (r *circularProgressRenderer) Objects() []fyne.CanvasObject {
	return r.objects
}

func (r *circularProgressRenderer) Destroy() {}

// DrawArc draws the arc for the progress indicator
func (r *circularProgressRenderer) DrawArc(circle *canvas.Circle, percent float64) {
	// Simple approximation via changing opacity
	// A more accurate arc would require using canvas.Raster
	alpha := uint8(255 * percent)
	if c, ok := circle.FillColor.(color.NRGBA); ok {
		c.A = alpha
		circle.FillColor = c
	}
}

// ArcRaster renders the progress arc
type ArcRaster struct {
	progress float64
	size     fyne.Size
}

func (ar *ArcRaster) Generator(x, y, w, h int) image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))

	centerX := float64(w) / 2
	centerY := float64(h) / 2
	radius := math.Min(centerX, centerY) - 2

	progressAngle := (ar.progress / 100.0) * 360.0

	for py := 0; py < h; py++ {
		for px := 0; px < w; px++ {
			dx := float64(px) - centerX
			dy := float64(py) - centerY
			distance := math.Sqrt(dx*dx + dy*dy)

			if distance >= radius-3 && distance <= radius+1 {
				angle := math.Atan2(dy, dx) * 180 / math.Pi
				if angle < 0 {
					angle += 360
				}
				// Start from the top (270 degrees)
				angle = angle - 90
				if angle < 0 {
					angle += 360
				}

				if angle <= progressAngle {
					img.Set(px, py, color.NRGBA{R: 76, G: 175, B: 80, A: 255})
				}
			}
		}
	}

	return img
}
