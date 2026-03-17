package ui

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"math"
	"time"

	"fyne.io/fyne/v2"
	"github.com/sirupsen/logrus"
)

// VideoFrameDecoder декодер видео кадров для Fyne
type VideoFrameDecoder struct {
	startTime      time.Time
	lastFrameTime  time.Time
	frameCount     int64
	frameTimes     []time.Time // Времена получения кадров для скользящего окна
	windowDuration time.Duration // Длительность скользящего окна (5 секунд)
}

// NewVideoFrameDecoder создает новый декодер видео кадров
func NewVideoFrameDecoder() *VideoFrameDecoder {
	return &VideoFrameDecoder{
		frameTimes:     make([]time.Time, 0, 300), // Примерно 60 FPS * 5 секунд
		windowDuration: 5 * time.Second,
	}
}

// ImageToResource конвертирует image.Image в fyne.Resource
func (vfd *VideoFrameDecoder) ImageToResource(img image.Image) fyne.Resource {
	if img == nil {
		return nil
	}

	// Конвертируем изображение в PNG формат
	var buf bytes.Buffer
	err := png.Encode(&buf, img)
	if err != nil {
		logrus.Errorf("❌ VideoFrameDecoder: ошибка кодирования PNG: %v", err)
		return nil
	}

	// Создаем ресурс из буфера
	resource := fyne.NewStaticResource("frame", buf.Bytes())
	return resource
}

// ImageToJPEGResource конвертирует image.Image в JPEG fyne.Resource
func (vfd *VideoFrameDecoder) ImageToJPEGResource(img image.Image, quality int) fyne.Resource {
	if img == nil {
		return nil
	}

	// Конвертируем изображение в JPEG формат
	var buf bytes.Buffer
	err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality})
	if err != nil {
		logrus.Errorf("Ошибка кодирования JPEG: %v", err)
		return nil
	}

	// Создаем ресурс из буфера
	resource := fyne.NewStaticResource("frame", buf.Bytes())
	return resource
}

// CreateTestFrame создает тестовое изображение для демонстрации
func (vfd *VideoFrameDecoder) CreateTestFrame(width, height int) image.Image {
	// Создаем RGBA изображение
	img := image.NewRGBA(image.Rect(0, 0, width, height))

	// Заполняем градиентом
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			// Создаем градиент от синего к красному
			r := uint8((x * 255) / width)
			g := uint8((y * 255) / height)
			b := uint8(255 - (x*y*255)/(width*height))
			a := uint8(255)

			img.Set(x, y, color.RGBA{r, g, b, a})
		}
	}

	// Добавляем текст (упрощенная версия)
	vfd.drawText(img, "USB Bridge Video Stream", 50, 50, color.White)
	vfd.drawText(img, time.Now().Format("15:04:05"), 50, 80, color.White)

	return img
}

// drawText рисует текст на изображении (упрощенная версия)
func (vfd *VideoFrameDecoder) drawText(img *image.RGBA, text string, x, y int, c color.Color) {
	// Упрощенная реализация рисования текста
	// В реальном проекте здесь должен быть полноценный рендеринг текста
	for i, char := range text {
		if x+i*8 >= img.Bounds().Max.X {
			break
		}
		vfd.drawChar(img, char, x+i*8, y, c)
	}
}

// drawChar рисует символ на изображении (упрощенная версия)
func (vfd *VideoFrameDecoder) drawChar(img *image.RGBA, char rune, x, y int, c color.Color) {
	// Упрощенная реализация рисования символа
	// Создаем простой прямоугольник для каждого символа
	for dy := 0; dy < 12; dy++ {
		for dx := 0; dx < 8; dx++ {
			if x+dx < img.Bounds().Max.X && y+dy < img.Bounds().Max.Y {
				img.Set(x+dx, y+dy, c)
			}
		}
	}
}

// CreateAnimatedFrame создает анимированное тестовое изображение
func (vfd *VideoFrameDecoder) CreateAnimatedFrame(width, height int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, width, height))

	// Создаем анимированный эффект
	timeOffset := time.Now().UnixNano() / 1000000 // миллисекунды
	waveOffset := float64(timeOffset%1000) / 1000.0

	// Заполняем синусоидальным градиентом
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			// Создаем волновой эффект
			waveX := float64(x) / float64(width) * 2 * 3.14159
			waveY := float64(y) / float64(height) * 2 * 3.14159

			r := uint8(128 + 127*math.Sin(waveX+waveOffset))
			g := uint8(128 + 127*math.Sin(waveY+waveOffset))
			b := uint8(128 + 127*math.Sin(waveX+waveY+waveOffset))
			a := uint8(255)

			img.Set(x, y, color.RGBA{r, g, b, a})
		}
	}

	// Добавляем информацию о времени
	vfd.drawText(img, "WebRTC Video Stream", 50, 50, color.White)
	vfd.drawText(img, time.Now().Format("15:04:05.000"), 50, 80, color.White)
	vfd.drawText(img, "Frame: "+fmt.Sprintf("%d", vfd.frameCount+1), 50, 110, color.White)

	return img
}

// ConvertToRGBA конвертирует любое изображение в RGBA
func (vfd *VideoFrameDecoder) ConvertToRGBA(src image.Image) *image.RGBA {
	bounds := src.Bounds()
	dst := image.NewRGBA(bounds)
	draw.Draw(dst, bounds, src, bounds.Min, draw.Src)
	return dst
}

// ResizeImage изменяет размер изображения
func (vfd *VideoFrameDecoder) ResizeImage(src image.Image, width, height int) image.Image {
	// Упрощенная реализация изменения размера
	// В реальном проекте здесь должен быть качественный алгоритм ресайза

	bounds := src.Bounds()
	srcWidth := bounds.Dx()
	srcHeight := bounds.Dy()

	dst := image.NewRGBA(image.Rect(0, 0, width, height))

	// Простое масштабирование методом ближайшего соседа
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

// GetFrameStats возвращает статистику кадров
func (vfd *VideoFrameDecoder) GetFrameStats() map[string]interface{} {
	return map[string]interface{}{
		"frame_count":     vfd.frameCount,
		"last_frame_time": vfd.lastFrameTime,
		"fps":             vfd.calculateFPS(),
	}
}

// calculateFPS вычисляет скользящее среднее FPS за последние 5 секунд
func (vfd *VideoFrameDecoder) calculateFPS() float64 {
	now := time.Now()
	cutoff := now.Add(-vfd.windowDuration)

	// Удаляем старые кадры за пределами окна
	validFrames := 0
	for i, t := range vfd.frameTimes {
		if t.After(cutoff) {
			vfd.frameTimes = vfd.frameTimes[i:]
			validFrames = len(vfd.frameTimes)
			break
		}
	}

	// Если нет кадров в окне
	if validFrames == 0 {
		vfd.frameTimes = vfd.frameTimes[:0]
		return 0
	}

	// Вычисляем FPS: количество кадров / длительность в секундах
	// Длительность = от первого кадра в окне до текущего момента
	duration := now.Sub(vfd.frameTimes[0]).Seconds()
	if duration < 0.1 {
		return 0 // Слишком мало времени прошло
	}

	return float64(validFrames) / duration
}

// UpdateFrameTime обновляет время последнего кадра
func (vfd *VideoFrameDecoder) UpdateFrameTime() {
	vfd.lastFrameTime = time.Now()
}

// IncrementFrameCount увеличивает счетчик кадров
func (vfd *VideoFrameDecoder) IncrementFrameCount() {
	now := time.Now()
	if vfd.startTime.IsZero() {
		vfd.startTime = now
	}
	vfd.frameCount++
	vfd.lastFrameTime = now

	// Добавляем время кадра для скользящего окна
	vfd.frameTimes = append(vfd.frameTimes, now)
}
