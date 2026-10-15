//go:build ios && cgo

package service

// PlayoutBufferSupported reports whether this build lets the user toggle the
// playout buffer. iOS shares moonlight_cgo_shared.h's DIRECT_SUBMIT path
// with macOS (see playout_buffer_supported_darwin.go), so the same v7
// in-place delay applies here too. Off by default, like everywhere (see
// playout_buffer.go).
func PlayoutBufferSupported() bool {
	return true
}
