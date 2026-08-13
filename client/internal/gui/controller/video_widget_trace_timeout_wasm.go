//go:build js && wasm

// See videoTraceFirstAttemptTimeout's own doc comment (video_widget.go) for
// why wasm needs a longer tolerance than every other platform's shared 4s
// default, and video_widget_silence_wasm.go for the analogous split already
// done for the *mid-stream* silence watchdog -- this is the same gap, just
// for the *initial* no-frame timeout: that one was already widened to 6s
// for wasm, but this one was left at the shared 4s default, so a first
// connect slow enough to blow the mid-stream tolerance's own budget could
// still get force-reconnected here first, before ever reaching that widened
// tolerance.
package controller

import "time"

// videoTraceFirstAttemptTimeoutWasm is longer than the 6s
// videoStallToleranceWasm (video_widget_silence_wasm.go) uses for an
// *already-established* stream going silent: a first connect over the web
// client's WebRTC path routes through real ICE candidate gathering/
// connectivity checks (STUN, possibly a relay hairpin) before the first
// keyframe can even be requested, on top of whatever the mid-stream
// tolerance already budgets for a server-side capture stall -- confirmed
// live: reconnecting to web.usbridge.io (off pure-LAN, through Cloudflare
// for signaling + real ICE negotiation for media) took noticeably longer
// to deliver a first frame than the same connect over a bare LAN dev
// server, and the shared 4s default was firing beginVideoTrace's "stuck,
// no frame" reconnect on connections that would have delivered a frame
// within another second or two if just left alone -- a self-inflicted
// reconnect storm indistinguishable from a real stuck stream in the logs.
const videoTraceFirstAttemptTimeoutWasm = 9 * time.Second

func (vw *VideoWidget) videoTraceFirstAttemptTimeout() time.Duration {
	return videoTraceFirstAttemptTimeoutWasm
}
