package controller

import (
	"sync/atomic"
	"time"

	"usbridge-client/internal/service"

	"fyne.io/fyne/v2"
	"github.com/sirupsen/logrus"
)

// Keep awake (the Control footer's keep-awake menu): while nobody uses the
// mouse or keyboard here, nudge the host every KeepAwakeEvery so it doesn't
// go idle -- a Mac that sleeps its display or locks in the middle of a call
// listened to through the stream cuts the audio too. Two independent
// switches, both off by default (gui keeps them in the preferences):
//   - move the mouse a step and back;
//   - press and release Shift (changes nothing on screen).
//
// Any input the user sends restarts the wait (noteUserInput).
var (
	keepAwakeMouse atomic.Bool
	keepAwakeShift atomic.Bool
	keepAwakeEvery atomic.Int64 // time.Duration
	lastUserInput  atomic.Int64 // unix nanoseconds
)

// KeepAwakeIntervals are the menu's choices; DefaultKeepAwakeEvery the
// first one: below macOS's shortest display sleep (1 minute).
var KeepAwakeIntervals = []time.Duration{30 * time.Second, time.Minute, 2 * time.Minute, 5 * time.Minute, 10 * time.Minute}

const DefaultKeepAwakeEvery = 30 * time.Second

// keepAwakeTick: how often the wait is checked.
const keepAwakeTick = 5 * time.Second

// SetKeepAwake sets both switches and the interval.
func SetKeepAwake(mouse, shift bool, every time.Duration) {
	if every <= 0 {
		every = DefaultKeepAwakeEvery
	}
	keepAwakeMouse.Store(mouse)
	keepAwakeShift.Store(shift)
	keepAwakeEvery.Store(int64(every))
}

// KeepAwake reports the switches and the interval.
func KeepAwake() (mouse, shift bool, every time.Duration) {
	every = time.Duration(keepAwakeEvery.Load())
	if every <= 0 {
		every = DefaultKeepAwakeEvery
	}
	return keepAwakeMouse.Load(), keepAwakeShift.Load(), every
}

// noteUserInput: the user just sent the host something.
func noteUserInput() { lastUserInput.Store(time.Now().UnixNano()) }

// startKeepAwake runs the keep-awake loop for vw's life (NewVideoWidget).
func (vw *VideoWidget) startKeepAwake() {
	go func() {
		t := time.NewTicker(keepAwakeTick)
		defer t.Stop()
		var lastNudge time.Time
		for range t.C {
			mouse, shift, every := KeepAwake()
			if !mouse && !shift {
				continue
			}
			last := time.Unix(0, lastUserInput.Load())
			if lastNudge.After(last) {
				last = lastNudge
			}
			if time.Since(last) < every {
				continue
			}
			mi := vw.moonlightInput()
			if mi == nil || !mi.IsInputActive() {
				continue
			}
			lastNudge = time.Now()
			fyne.Do(func() { vw.keepAwakeNudge(mi, mouse, shift) })
		}
	}()
}

// keepAwakeNudge sends one nudge. UI goroutine (the mouse mode and the last
// absolute position); the sends skip enqueueSend, whose callers count as
// the user's input.
func (vw *VideoWidget) keepAwakeNudge(mi service.MoonlightInputSender, mouse, shift bool) {
	abs := vw.IsAbsoluteLikeInputMode()
	x, y := vw.lastAbsX, vw.lastAbsY
	logrus.Infof("☕ [KeepAwake] idle: nudging the host (mouse=%v shift=%v)", mouse, shift)
	go func() {
		if mouse {
			// (0,0): no absolute position sent yet (the pointer hasn't been
			// over the stream since the widget was made), so where the host
			// cursor is isn't known -- an absolute nudge would throw it to the
			// top-left corner. Move it relatively then: an agent takes that;
			// the KVM's tablet mode drops it (harmless) until the pointer has
			// been over the stream once.
			if abs && (x != 0 || y != 0) {
				// Absolute: a step in host units (~2-3 px) and back. A
				// relative move doesn't reach a KVM target in this mode.
				nx := x + 40
				if nx > 32767 {
					nx = x - 40
				}
				mi.SendMoonlightMousePosition(int16(nx), int16(y), 32767, 32767)
				time.Sleep(30 * time.Millisecond)
				mi.SendMoonlightMousePosition(int16(x), int16(y), 32767, 32767)
			} else {
				mi.SendMoonlightMouseMove(1, 0)
				time.Sleep(30 * time.Millisecond)
				mi.SendMoonlightMouseMove(-1, 0)
			}
		}
		if shift {
			const vkLShift, modShift = 0xA0, 0x01
			mi.SendMoonlightKey(vkLShift, service.LiKeyActionDown, modShift)
			time.Sleep(30 * time.Millisecond)
			mi.SendMoonlightKey(vkLShift, service.LiKeyActionUp, 0)
		}
	}()
}
