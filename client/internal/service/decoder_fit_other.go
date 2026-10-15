//go:build !(android && cgo)

package service

// decoderFitSize: only Android asks its decoder before launching (see the
// android version); elsewhere the requested size goes to the host as is.
func decoderFitSize(videoMode string, w, h int) (fw, fh, maxFPS int, ok bool) {
	return w, h, 0, false
}
