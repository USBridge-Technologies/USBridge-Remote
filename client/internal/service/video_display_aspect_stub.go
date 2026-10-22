//go:build !cgo || !(windows || linux || android)

package service

// No native renderer here takes a display aspect (macOS/iOS lay the picture
// out with the layer's own aspect fit): the picture keeps the frame's shape.
const nativeDisplayAspectSupported = false

func setNativeDisplayAspect(float32) {}
