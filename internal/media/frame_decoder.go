package media

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"math"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"github.com/sirupsen/logrus"
)

// FrameDecoder video frame decoder for Fyne.
type FrameDecoder struct {
	startTime      time.Time
	lastFrameTime  time.Time
	frameCount     int64
	frameTimes     []time.Time   // Frame reception times for sliding window
	windowDuration time.Duration // Sliding window duration (5 seconds)
	mu             sync.Mutex
}

// NewFrameDecoder creates a new video frame decoder.
func NewFrameDecoder() *FrameDecoder {
	return &FrameDecoder{
		frameTimes:     make([]time.Time, 0, 300), // Approx 60 FPS * 5 seconds
		windowDuration: 5 * time.Second,
	}
}

// ImageToResource converts image.Image to fyne.Resource
func (vfd *FrameDecoder) ImageToResource(img image.Image) fyne.Resource {
	if img == nil {
		return nil
	}

	// Convert image to PNG format
	var buf bytes.Buffer
	err := png.Encode(&buf, img)
	if err != nil {
		logrus.Errorf("❌ FrameDecoder: PNG encoding error: %v", err)
		return nil
	}

	// Create resource from buffer
	resource := fyne.NewStaticResource("frame", buf.Bytes())
	return resource
}

// ImageToJPEGResource converts image.Image to JPEG fyne.Resource
func (vfd *FrameDecoder) ImageToJPEGResource(img image.Image, quality int) fyne.Resource {
	if img == nil {
		return nil
	}

	// Convert image to JPEG format
	var buf bytes.Buffer
	err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality})
	if err != nil {
		logrus.Errorf("JPEG encoding error: %v", err)
		return nil
	}

	// Create resource from buffer
	resource := fyne.NewStaticResource("frame", buf.Bytes())
	return resource
}

// CreateTestFrame creates a test image for demonstration
func (vfd *FrameDecoder) CreateTestFrame(width, height int) image.Image {
	// Create RGBA image
	img := image.NewRGBA(image.Rect(0, 0, width, height))

	// Fill with gradient
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			// Create gradient from blue to red
			r := uint8((x * 255) / width)
			g := uint8((y * 255) / height)
			b := uint8(255 - (x*y*255)/(width*height))
			a := uint8(255)

			img.Set(x, y, color.RGBA{r, g, b, a})
		}
	}

	// Add text (simplified version)
	vfd.drawText(img, "USBridge Video Stream", 50, 50, color.White)
	vfd.drawText(img, time.Now().Format("15:04:05"), 50, 80, color.White)

	return img
}

// drawText draws text on the image (simplified version)
func (vfd *FrameDecoder) drawText(img *image.RGBA, text string, x, y int, c color.Color) {
	// Simplified text drawing implementation
	// In a real project, full text rendering should be here
	for i, char := range text {
		if x+i*8 >= img.Bounds().Max.X {
			break
		}
		vfd.drawChar(img, char, x+i*8, y, c)
	}
}

// drawChar draws a character on the image (simplified version)
func (vfd *FrameDecoder) drawChar(img *image.RGBA, char rune, x, y int, c color.Color) {
	// Simplified character drawing implementation
	// Create a simple rectangle for each character
	for dy := 0; dy < 12; dy++ {
		for dx := 0; dx < 8; dx++ {
			if x+dx < img.Bounds().Max.X && y+dy < img.Bounds().Max.Y {
				img.Set(x+dx, y+dy, c)
			}
		}
	}
}

// CreateAnimatedFrame creates an animated test image
func (vfd *FrameDecoder) CreateAnimatedFrame(width, height int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, width, height))

	// Create animated effect
	timeOffset := time.Now().UnixNano() / 1000000 // milliseconds
	waveOffset := float64(timeOffset%1000) / 1000.0

	// Fill with sinusoidal gradient
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			// Create wave effect
			waveX := float64(x) / float64(width) * 2 * 3.14159
			waveY := float64(y) / float64(height) * 2 * 3.14159

			r := uint8(128 + 127*math.Sin(waveX+waveOffset))
			g := uint8(128 + 127*math.Sin(waveY+waveOffset))
			b := uint8(128 + 127*math.Sin(waveX+waveY+waveOffset))
			a := uint8(255)

			img.Set(x, y, color.RGBA{r, g, b, a})
		}
	}

	// Add time information
	vfd.drawText(img, "RTP/UDP Video Stream", 50, 50, color.White)
	vfd.drawText(img, time.Now().Format("15:04:05.000"), 50, 80, color.White)
	vfd.drawText(img, "Frame: "+fmt.Sprintf("%d", vfd.frameCount+1), 50, 110, color.White)

	return img
}

// ConvertToRGBA converts any image to RGBA
func (vfd *FrameDecoder) ConvertToRGBA(src image.Image) *image.RGBA {
	bounds := src.Bounds()
	dst := image.NewRGBA(bounds)
	draw.Draw(dst, bounds, src, bounds.Min, draw.Src)
	return dst
}

// ResizeImage resizes the image
func (vfd *FrameDecoder) ResizeImage(src image.Image, width, height int) image.Image {
	// Simplified resizing implementation
	// In a real project, a high-quality resize algorithm should be here

	bounds := src.Bounds()
	srcWidth := bounds.Dx()
	srcHeight := bounds.Dy()

	dst := image.NewRGBA(image.Rect(0, 0, width, height))

	// Simple nearest neighbor scaling
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			srcX := (x * srcWidth) / width
			srcY := (y * srcHeight) / height

			if srcX < srcWidth && srcY < srcHeight {
				dst.Set(x, y, src.At(srcX, srcY))
			}
		}
	}

	return dst
}

// GetFrameStats returns frame statistics
func (vfd *FrameDecoder) GetFrameStats() map[string]interface{} {
	vfd.mu.Lock()
	defer vfd.mu.Unlock()

	return map[string]interface{}{
		"frame_count":     vfd.frameCount,
		"last_frame_time": vfd.lastFrameTime,
		"fps":             vfd.calculateFPSLocked(),
	}
}

// calculateFPS calculates moving average FPS for the last 5 seconds
func (vfd *FrameDecoder) calculateFPSLocked() float64 {
	now := time.Now()
	cutoff := now.Add(-vfd.windowDuration)

	// Remove old frames outside the window
	for i, t := range vfd.frameTimes {
		if t.After(cutoff) {
			vfd.frameTimes = vfd.frameTimes[i:]
			goto fpsReady
		}
	}
	vfd.frameTimes = vfd.frameTimes[:0]

fpsReady:
	validFrames := len(vfd.frameTimes)
	if validFrames == 0 {
		return 0
	}

	// Calculate FPS: frame count / duration in seconds
	// Duration = from the first frame in window to the current moment
	duration := now.Sub(vfd.frameTimes[0]).Seconds()
	if duration < 0.1 {
		return 0 // Too little time has passed
	}

	return float64(validFrames) / duration
}

// UpdateFrameTime updates the time of the last frame
func (vfd *FrameDecoder) UpdateFrameTime() {
	vfd.mu.Lock()
	defer vfd.mu.Unlock()
	vfd.lastFrameTime = time.Now()
}

// IncrementFrameCount increments frame counter
func (vfd *FrameDecoder) IncrementFrameCount() {
	vfd.mu.Lock()
	defer vfd.mu.Unlock()

	now := time.Now()
	if vfd.startTime.IsZero() {
		vfd.startTime = now
	}
	vfd.frameCount++
	vfd.lastFrameTime = now

	// Add frame time for sliding window
	vfd.frameTimes = append(vfd.frameTimes, now)
}

func (vfd *FrameDecoder) Reset() {
	vfd.mu.Lock()
	defer vfd.mu.Unlock()

	vfd.startTime = time.Time{}
	vfd.lastFrameTime = time.Time{}
	vfd.frameCount = 0
	vfd.frameTimes = vfd.frameTimes[:0]
}
