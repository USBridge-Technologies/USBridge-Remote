//go:build !(js && wasm)

package controller

import "time"

// videoTraceFirstAttemptTimeout is videoTraceFirstAttemptTimeout (the
// package-level constant, 4s) on every platform except wasm -- see
// video_widget_trace_timeout_wasm.go for that override, and its own doc
// comment for why wasm alone needs a longer tolerance. Unchanged from
// before this per-platform split existed; mirrors the same split already
// done for videoSilenceThreshold (video_widget_silence_default.go/
// _wasm.go).
func (vw *VideoWidget) videoTraceFirstAttemptTimeout() time.Duration {
	return videoTraceFirstAttemptTimeout
}
