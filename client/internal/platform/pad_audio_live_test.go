//go:build linux && cgo

package platform

import (
	"math"
	"os"
	"testing"
	"time"
)

// Plays a quiet tone on a real pad's sound device: PAD_AUDIO_LIVE=/dev/input/eventN.
func TestPadAudioLive(t *testing.T) {
	id := os.Getenv("PAD_AUDIO_LIVE")
	if id == "" {
		t.Skip("PAD_AUDIO_LIVE not set")
	}
	p, err := OpenPadAudio(id)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	tone := make([]int16, 480*2)
	for i := 0; i < 480; i++ {
		v := int16(2000 * math.Sin(2*math.Pi*440*float64(i)/48000))
		tone[2*i], tone[2*i+1] = v, v
	}
	for i := 0; i < 50; i++ {
		p.PushSpeaker(tone)
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(100 * time.Millisecond)
}
