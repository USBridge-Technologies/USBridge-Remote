//go:build (linux || windows || darwin) && !android && !ios && cgo

package service

/*
#include <opus.h>
*/
import "C"

import (
	"errors"
	"unsafe"
)

// PadOpusDecoder decodes a pad's speaker stream (USBridge pad audio, format
// PadSoundOpus): 48 kHz stereo Opus frames into interleaved samples.
type PadOpusDecoder struct {
	dec *C.OpusDecoder
	pcm []int16
}

// NewPadOpusDecoder makes a decoder; nil, error without libopus support.
func NewPadOpusDecoder() (*PadOpusDecoder, error) {
	var err C.int
	dec := C.opus_decoder_create(48000, 2, &err)
	if err != C.OPUS_OK || dec == nil {
		return nil, errors.New("opus_decoder_create failed")
	}
	return &PadOpusDecoder{dec: dec, pcm: make([]int16, 5760*2)}, nil
}

// Decode returns one frame's samples (a copy), or nil for a frame it cannot
// decode; nil data conceals a lost frame.
func (d *PadOpusDecoder) Decode(data []byte) []int16 {
	var p *C.uchar
	if len(data) > 0 {
		p = (*C.uchar)(unsafe.Pointer(&data[0]))
	}
	n := C.opus_decode(d.dec, p, C.opus_int32(len(data)), (*C.opus_int16)(unsafe.Pointer(&d.pcm[0])), 5760, 0)
	if n <= 0 {
		return nil
	}
	return append([]int16(nil), d.pcm[:int(n)*2]...)
}

// Close frees the decoder.
func (d *PadOpusDecoder) Close() {
	if d.dec != nil {
		C.opus_decoder_destroy(d.dec)
		d.dec = nil
	}
}
