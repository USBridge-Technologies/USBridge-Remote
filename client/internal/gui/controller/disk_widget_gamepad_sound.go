package controller

import (
	"errors"
	"sync"

	"usbridge-client/internal/platform"
	"usbridge-client/internal/service"

	"github.com/sirupsen/logrus"
)

// What a game plays into the host's virtual PlayStation pad, played on the
// captured pad: its speaker/headset (USBridge pad audio, PCM or Opus) and a
// DualSense's voice-coil haptics (Vibeshine haptics PCM), through the pad's
// own sound device (platform.PadAudio). The host only sends them for a pad
// whose arrival said the client plays them (padArrivalCapabilities), so the
// sound device is opened when the capture starts, before the arrival.
//
// And a DualSense's adaptive triggers: the host's effects go into the pad's
// output report (platform.SetGamepadTriggers).

// padAudio is one captured pad's sound device and its speaker decoder.
type padAudio struct {
	out *platform.PadAudio

	mu       sync.Mutex
	opus     *service.PadOpusDecoder
	lastSeq  uint32
	hasSeq   bool
	reported bool // a bad stream was logged
}

// startPadAudio opens a captured PlayStation pad's sound device, if it has one.
func (dw *DiskWidget) startPadAudio(id string) {
	out, err := platform.OpenPadAudio(id)
	if err != nil {
		if !errors.Is(err, platform.ErrNoPadAudio) {
			logrus.Warnf("🎮 [PadAudio] %s: %v", id, err)
		} else {
			logrus.Infof("🎮 [PadAudio] %s: %v -- the host's pad sound stays on the host", id, err)
		}
		return
	}
	dw.padAudios.Store(id, &padAudio{out: out})
}

// stopPadAudio closes a pad's sound device.
func (dw *DiskWidget) stopPadAudio(id string) {
	v, ok := dw.padAudios.LoadAndDelete(id)
	if !ok {
		return
	}
	a := v.(*padAudio)
	a.out.Close()
	a.mu.Lock()
	if a.opus != nil {
		a.opus.Close()
		a.opus = nil
	}
	a.mu.Unlock()
}

func (dw *DiskWidget) padAudioOf(id string) *padAudio {
	v, ok := dw.padAudios.Load(id)
	if !ok {
		return nil
	}
	return v.(*padAudio)
}

// padAudioAt is the sound device of the pad holding a controller number.
func (dw *DiskWidget) padAudioAt(controller uint16) (string, *padAudio) {
	id, ok := dw.padSlots.idAt(int(controller))
	if !ok {
		return "", nil
	}
	return id, dw.padAudioOf(id)
}

// samplesOf reads little-endian 16-bit samples.
func samplesOf(b []byte) []int16 {
	s := make([]int16, len(b)/2)
	for i := range s {
		s[i] = int16(uint16(b[2*i]) | uint16(b[2*i+1])<<8)
	}
	return s
}

// onHostPadHaptics plays a DualSense's haptics on its voice coils. Runs on
// Moonlight's control receive thread: only queues.
func (dw *DiskWidget) onHostPadHaptics(controller uint16, _ uint32, _ uint8, pcm []byte, _ int) {
	id, a := dw.padAudioAt(controller)
	if a == nil {
		return
	}
	a.out.PushHaptics(samplesOf(pcm))
	platform.NoteGamepadHaptics(id)
}

// onHostPadAudio plays the pad's speaker/headset sound. Runs on Moonlight's
// control receive thread: decodes one frame and queues it.
func (dw *DiskWidget) onHostPadAudio(controller uint16, seq uint32, format uint8, data []byte, frames int) {
	id, a := dw.padAudioAt(controller)
	if a == nil {
		return
	}
	switch format {
	case service.PadSoundPCM:
		a.out.PushSpeaker(samplesOf(data))
	case service.PadSoundOpus:
		a.mu.Lock()
		defer a.mu.Unlock()
		if a.opus == nil {
			dec, err := service.NewPadOpusDecoder()
			if err != nil {
				if !a.reported {
					a.reported = true
					logrus.Warnf("🎮 [PadAudio] %s: %v", id, err)
				}
				return
			}
			a.opus = dec
		}
		// A lost frame or two is concealed; a longer gap is a new start.
		if a.hasSeq {
			if gap := seq - a.lastSeq - 1; gap > 0 && gap <= 2 {
				for i := uint32(0); i < gap; i++ {
					a.out.PushSpeaker(a.opus.Decode(nil))
				}
			}
		}
		a.lastSeq, a.hasSeq = seq, true
		a.out.PushSpeaker(a.opus.Decode(data))
	default:
		if !a.reported {
			a.reported = true
			logrus.Warnf("🎮 [PadAudio] %s: unknown pad sound format %d", id, format)
		}
	}
}

// onHostPadTriggers applies a DualSense's adaptive-trigger effects. Runs on
// Moonlight's callback thread.
func (dw *DiskWidget) onHostPadTriggers(controller uint16, flags, typeLeft, typeRight uint8, left, right [10]byte) {
	id, ok := dw.padSlots.idAt(int(controller))
	if !ok {
		return
	}
	logrus.Debugf("🎮 [GAMEPAD] host triggers controller=%d flags=%#x left=%#x right=%#x -> %s", controller, flags, typeLeft, typeRight, id)
	platform.SetGamepadTriggers(id, flags, typeLeft, typeRight, left, right)
}
