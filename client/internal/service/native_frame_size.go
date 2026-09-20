package service

import "sync/atomic"

// nativeFrameW/H are the last decoded stream dimensions reported by goVTFrame,
// including the native GPU path that delivers rgba=nil. Absolute mouse mapping
// needs this aspect so letterbox/pillarbox bars are excluded even when Go never
// sees a pixel buffer.
var nativeFrameW atomic.Int32
var nativeFrameH atomic.Int32

func noteNativeFrameSize(w, h int) {
	if w > 0 && h > 0 {
		nativeFrameW.Store(int32(w))
		nativeFrameH.Store(int32(h))
	}
}

// NativeFrameSize returns the last decoded video frame size, or 0,0 if none.
func NativeFrameSize() (int, int) {
	return int(nativeFrameW.Load()), int(nativeFrameH.Load())
}
