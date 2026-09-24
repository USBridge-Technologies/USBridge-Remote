package controller

import "usbridge-client/internal/gui/view"

// imeCropsVideoOverlay is true only in portrait on phones: landscape soft
// keyboards (Android floating/widget IME, and similarly on iOS) sit over the
// video without reclaiming the bottom band, so Vulkan/Metal must not shrink
// under them — only the Control footer chrome reserves space.
func imeCropsVideoOverlay() bool {
	if !view.IsMobile() {
		return true
	}
	return !view.IsLandscape()
}
