//go:build !(android && cgo)

package service

// decoderFitSize: only Android asks its decoder before launching (see the
// android version); elsewhere the requested size goes to the host as is.
func decoderFitSize(videoMode string, w, h, fps int) (fw, fh, maxFPS int, ok bool) {
	return w, h, 0, false
}

// displayMaxFPS: only Android caps the requested rate at the display's.
func displayMaxFPS() int { return 0 }
