//go:build !android
// +build !android

package service

import (
	"image"
	"sync"
)

// rgbaToImage converts RGBA data to image.Image (common function for darwin/linux)
func rgbaToImage(data []byte, width, height int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	expectedSize := width * height * 4
	if len(data) < expectedSize {
		return nil
	}
	copy(img.Pix, data[:expectedSize])
	return img
}

// stopSignal — wrapper for a safe one-time stop signal.
// Created anew on each Connect.
type stopSignal struct {
	ch   chan struct{}
	once sync.Once
}

func newStopSignal() *stopSignal {
	return &stopSignal{ch: make(chan struct{})}
}

func (s *stopSignal) signal() {
	if s == nil {
		return
	}
	s.once.Do(func() { close(s.ch) })
}

func (s *stopSignal) done() <-chan struct{} {
	if s == nil {
		return nil
	}
	return s.ch
}
