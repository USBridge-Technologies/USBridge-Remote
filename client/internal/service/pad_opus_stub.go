//go:build !((linux || windows || darwin) && !android && !ios && cgo)

package service

import "errors"

// PadOpusDecoder is unavailable here: a pad's Opus speaker stream is dropped.
type PadOpusDecoder struct{}

func NewPadOpusDecoder() (*PadOpusDecoder, error) {
	return nil, errors.New("no Opus decoder in this build")
}

func (d *PadOpusDecoder) Decode(data []byte) []int16 { return nil }

func (d *PadOpusDecoder) Close() {}
