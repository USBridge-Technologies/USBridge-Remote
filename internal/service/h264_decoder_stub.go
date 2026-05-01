// +build android

package service

import (
	"image"
)

// H264Decoder stub for Android
type H264Decoder struct {
	onFrameDecoded func(image.Image)
}

// NewH264Decoder creates a stub H264 decoder for Android
func NewH264Decoder() (*H264Decoder, error) {
	return &H264Decoder{}, nil
}

// SetFrameCallback sets the callback
func (d *H264Decoder) SetFrameCallback(callback func(image.Image)) {
	d.onFrameDecoded = callback
}

// DecodeRTPPacket stub
func (d *H264Decoder) DecodeRTPPacket(packet []byte) error {
	return nil
}

// Close closes the decoder
func (d *H264Decoder) Close() error {
	return nil
}

// Start starts the decoder
func (d *H264Decoder) Start() error {
	return nil
}

// Stop stops the decoder
func (d *H264Decoder) Stop() error {
	return nil
}
