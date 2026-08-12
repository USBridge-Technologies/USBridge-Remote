//go:build !(js && wasm)

package controller

import "time"

// videoSilenceThreshold is videoMidStreamSilenceTimeout+
// videoSilenceGracePeriod (3.5s) on every platform except wasm (see
// video_widget_silence_wasm.go for that override, and its own doc comment
// for why wasm alone needs a longer tolerance) -- unchanged from before
// this per-platform split existed. Every native capture backend (V4L2/
// DXGI/ScreenCaptureKit/AMediaCodec) was already tuned against this exact
// value (see videoMidStreamSilenceTimeout's own doc comment on the
// rust-shine capture-kms crash-recovery case that originally motivated
// it), so it stays exactly as-is here.
func (vw *VideoWidget) videoSilenceThreshold() time.Duration {
	return videoMidStreamSilenceTimeout + videoSilenceGracePeriod
}
