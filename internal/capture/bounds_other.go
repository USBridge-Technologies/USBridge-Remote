//go:build !darwin

package capture

import "github.com/kbinani/screenshot"

func physicalDisplaySize(index int) (int, int) {
	if index < 0 || index >= screenshot.NumActiveDisplays() {
		return 0, 0
	}
	b := screenshot.GetDisplayBounds(index)
	return b.Dx(), b.Dy()
}
