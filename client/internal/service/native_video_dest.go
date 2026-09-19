package service

// NativeVideoDest is the on-screen letterboxed picture inside the native
// overlay, in overlay/swapchain physical pixels. Absolute mouse mapping must
// use this rect — not the Fyne widget — so black bars are excluded.
type NativeVideoDest struct {
	DX, DY, DW, DH int
	SW, SH         int
}

// NativeVideoDestRect returns the current GPU letterbox dest when a native
// overlay is presenting. ok is false before the first frame.
func NativeVideoDestRect() (NativeVideoDest, bool) {
	return nativeVideoDestRect()
}
