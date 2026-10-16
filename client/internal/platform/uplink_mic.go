package platform

import (
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/sirupsen/logrus"
)

// The microphone half shared by every platform: a platform only opens its
// default microphone as a micSource (48kHz mono S16) -- PulseAudio on Linux
// (uplink_mic_linux.go), WASAPI on Windows, AudioQueue on macOS and iOS,
// AAudio on Android -- and this cuts it into frames. Encoding is the
// caller's (service.StartMicUplink, Opus).

// micSource is an open microphone.
type micSource interface {
	// read fills pcm (48kHz mono S16) completely, blocking until it can;
	// ok=false when no full frame came within about a frame's time (read
	// again -- this lets Stop take effect). An error ends the capture.
	read(pcm []int16) (ok bool, err error)
	close()
}

type micCapture struct {
	stop atomic.Bool
	done chan struct{}
	once sync.Once
}

func (c *micCapture) Stop() {
	c.once.Do(func() {
		c.stop.Store(true)
		<-c.done
	})
}

// StartMicCapture records the default microphone and hands each 20ms frame
// (MicFrameSamples at 48kHz mono) to onFrame, from its own goroutine; pcm
// is reused after onFrame returns. Stop returns once onFrame no longer runs.
func StartMicCapture(onFrame func(pcm []int16)) (UplinkCapture, error) {
	if !MicSupported() {
		return nil, fmt.Errorf("microphone capture is not supported on this platform")
	}
	src, err := openMic()
	if err != nil {
		return nil, err
	}
	c := &micCapture{done: make(chan struct{})}
	go func() {
		defer close(c.done)
		defer src.close()
		pcm := make([]int16, MicFrameSamples)
		for !c.stop.Load() {
			ok, err := src.read(pcm)
			if err != nil {
				logrus.Warnf("🎤 [UPLINK] microphone stopped: %v", err)
				return
			}
			if ok && !c.stop.Load() {
				onFrame(pcm)
			}
		}
	}()
	return c, nil
}
