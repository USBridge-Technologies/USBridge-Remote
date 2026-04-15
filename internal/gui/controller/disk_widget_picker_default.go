//go:build !windows

package controller

func (dw *DiskWidget) showPlatformNativeImagePicker() (selectedImage, bool) {
	return selectedImage{}, false
}
