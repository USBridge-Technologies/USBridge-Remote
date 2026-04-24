//go:build !windows && !darwin && (!linux || android)

package controller

func (dw *DiskWidget) showPlatformNativeImagePicker() (selectedImage, bool) {
	return selectedImage{}, false
}
