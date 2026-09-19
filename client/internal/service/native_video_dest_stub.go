//go:build !cgo || (!windows && !linux && !android)

package service

func nativeVideoDestRect() (NativeVideoDest, bool) {
	return NativeVideoDest{}, false
}
