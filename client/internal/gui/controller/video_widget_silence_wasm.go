//go:build js && wasm

// See videoSilenceThreshold's own doc comment (video_widget.go) for why
// wasm needs a longer tolerance than every other platform's shared 3.5s
// default.
package controller

import "time"

// videoStallToleranceWasm is long enough to ride out the ~4.5-5s
// capture-kms stall confirmed live via RTCPeerConnection.getStats()
// (packetsReceived/framesDecoded both flat, packetsLost=0 -- a genuine
// server-side encode stall, not packet loss) while still catching a
// truly-dead stream in well under moonlight-common-c's own 10s watchdogs
// (not applicable here, but keeps the same order of magnitude the rest of
// this file's reasoning assumes).
const videoStallToleranceWasm = 6 * time.Second

func (vw *VideoWidget) videoSilenceThreshold() time.Duration {
	return videoStallToleranceWasm
}
