package platform

import (
	"errors"
	"runtime"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// The sound a host plays into its virtual pad, played on the real pad: a
// wired PlayStation pad is a USB sound device too (a DualShock 4's headset
// jack, a Razer Raiju's; a DualSense's speaker and jack on channels 1/2 and
// its voice coils -- the haptics -- on 3/4). The host sends the speaker and
// the haptics as two stereo streams (USBridge pad audio, Vibeshine haptics);
// PadAudio lays them out on the pad's channels and plays them at the pad's
// own pace.

// ErrNoPadAudio means the pad has no sound device this client can play on.
var ErrNoPadAudio = errors.New("the pad has no sound device")

const (
	padAudioRate = 48000
	// One block of the writer: 10 ms.
	padAudioBlock = padAudioRate / 100
	// A stream starts playing once this much is queued, and starts over the
	// same way after it ran dry: packets come in bursts.
	padAudioPrime = padAudioRate * 30 / 1000
	// More than this queued and the oldest goes: the latency stays bounded
	// when the host's clock runs ahead of the pad's.
	padAudioMax = padAudioRate * 120 / 1000
)

// padSink is a pad's sound device, opened by the platform on the writer's
// own thread.
type padSink interface {
	// write plays interleaved frames of the sink's channels, blocking while
	// the device has no room.
	write(pcm []int16) error
	close()
}

// padStream is one stereo stream waiting to be played.
type padStream struct {
	samples []int16 // interleaved L/R
	playing bool
	last    time.Time
}

func (s *padStream) push(stereo []int16) {
	s.samples = append(s.samples, stereo...)
	if excess := len(s.samples) - padAudioMax*2; excess > 0 {
		s.samples = s.samples[excess:]
	}
	s.last = time.Now()
}

// take returns one block of the stream, silence while it primes or ran dry.
func (s *padStream) take(frames int) []int16 {
	if !s.playing && len(s.samples) >= padAudioPrime*2 {
		s.playing = true
	}
	if s.playing && len(s.samples) < frames*2 {
		s.playing = false
	}
	out := make([]int16, frames*2)
	if s.playing {
		copy(out, s.samples[:frames*2])
		s.samples = s.samples[frames*2:]
	}
	return out
}

// PadAudio plays a host's pad sound on one captured pad.
type PadAudio struct {
	id       string
	channels int

	mu      sync.Mutex
	speaker padStream
	haptics padStream

	stop chan struct{}
	done chan struct{}
}

// Channels is the pad's: 2 (speaker/headset) or 4 (a DualSense, with its
// voice coils on 3/4).
func (p *PadAudio) Channels() int { return p.channels }

// HasHaptics reports whether the pad plays haptics on voice coils.
func (p *PadAudio) HasHaptics() bool { return p.channels >= 4 }

// PushSpeaker queues 48 kHz stereo for the pad's speaker/headset.
func (p *PadAudio) PushSpeaker(stereo []int16) {
	p.mu.Lock()
	p.speaker.push(stereo)
	p.mu.Unlock()
}

// PushHaptics queues 48 kHz stereo for the voice coils (left, right).
func (p *PadAudio) PushHaptics(stereo []int16) {
	if !p.HasHaptics() {
		return
	}
	p.mu.Lock()
	p.haptics.push(stereo)
	p.mu.Unlock()
}

// HapticsActive reports whether haptics came in the last 200 ms.
func (p *PadAudio) HapticsActive() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return time.Since(p.haptics.last) < 200*time.Millisecond
}

// Close stops playing and releases the device.
func (p *PadAudio) Close() {
	close(p.stop)
	<-p.done
}

// block mixes the next 10 ms for the pad's channels.
func (p *PadAudio) block() []int16 {
	p.mu.Lock()
	spk := p.speaker.take(padAudioBlock)
	var hap []int16
	if p.channels >= 4 {
		hap = p.haptics.take(padAudioBlock)
	}
	p.mu.Unlock()
	if p.channels < 4 {
		return spk
	}
	out := make([]int16, padAudioBlock*p.channels)
	for i := 0; i < padAudioBlock; i++ {
		f := out[i*p.channels:]
		f[0], f[1] = spk[2*i], spk[2*i+1]
		f[2], f[3] = hap[2*i], hap[2*i+1]
	}
	return out
}

// OpenPadAudio opens the sound device of the pad behind deviceID (as the
// capture names it), or returns ErrNoPadAudio. It plays from then on --
// silence until something is pushed -- until Close.
func OpenPadAudio(deviceID string) (*PadAudio, error) {
	channels, err := padAudioChannels(deviceID)
	if err != nil {
		return nil, err
	}
	p := &PadAudio{id: deviceID, channels: channels, stop: make(chan struct{}), done: make(chan struct{})}
	opened := make(chan error, 1)
	go func() {
		defer close(p.done)
		// The sink lives on this one thread (COM on Windows).
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		sink, err := openPadSink(deviceID, channels)
		opened <- err
		if err != nil {
			return
		}
		defer sink.close()
		for {
			select {
			case <-p.stop:
				return
			default:
			}
			if err := sink.write(p.block()); err != nil {
				logrus.Warnf("🎮 [PadAudio] %s: %v -- the pad's sound stops", deviceID, err)
				<-p.stop
				return
			}
		}
	}()
	if err := <-opened; err != nil {
		<-p.done
		return nil, err
	}
	logrus.Infof("🎮 [PadAudio] %s: playing the host's pad sound on the pad (%d channels)", deviceID, channels)
	return p, nil
}
