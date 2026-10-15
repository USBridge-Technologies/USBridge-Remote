package service

import "sync/atomic"

// The playout (jitter) buffer in our moonlight-common-c fork
// (VideoDepacketizer.c's playoutDelayForFrame) holds each frame back by a
// multiple of the measured network jitter before decoding it. It smooths
// cadence on a jittery link, but it is pure added latency, and the official
// Moonlight clients have nothing like it -- so it is opt-in from the video
// settings dialog wherever PlayoutBufferSupported() is true (Windows,
// macOS, Linux, iOS, Android -- see each platform's own
// playout_buffer_supported_*.go). The zero value here is off on every
// platform, mobile included: phones are latency-bound too, and a bad Wi-Fi
// link can still opt in. See docs/WINDOWS_DECODE_PIPELINE.md for the
// measurements behind the buffer itself.
var playoutBufferEnabled atomic.Bool

// SetPlayoutBufferEnabled turns the buffer on or off for the next stream
// start (the fork reads USBRIDGE_PLAYOUT_BUFFER, driven by this, once per
// session -- see moonlight_cgo_wrapper.go/moonlight_cgo_windows.go/
// moonlight_cgo_android.go). The video settings dialog applies this from
// Apply/Start; Cancel/close leaves the previous state.
func SetPlayoutBufferEnabled(enabled bool) {
	playoutBufferEnabled.Store(enabled)
}

// PlayoutBufferEnabled reports the checkbox's current state.
func PlayoutBufferEnabled() bool {
	return playoutBufferEnabled.Load()
}
