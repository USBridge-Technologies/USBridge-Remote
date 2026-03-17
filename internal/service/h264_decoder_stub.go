// +build android

package service

import (
	"image"
)

// H264Decoder заглушка для Android
type H264Decoder struct {
	onFrameDecoded func(image.Image)
}

// NewH264Decoder создает заглушку H264 декодера для Android
func NewH264Decoder() (*H264Decoder, error) {
	return &H264Decoder{}, nil
}

// SetFrameCallback устанавливает callback
func (d *H264Decoder) SetFrameCallback(callback func(image.Image)) {
	d.onFrameDecoded = callback
}

// DecodeRTPPacket заглушка
func (d *H264Decoder) DecodeRTPPacket(packet []byte) error {
	return nil
}

// Close закрывает декодер
func (d *H264Decoder) Close() error {
	return nil
}

// Start запускает декодер
func (d *H264Decoder) Start() error {
	return nil
}

// Stop останавливает декодер
func (d *H264Decoder) Stop() error {
	return nil
}
