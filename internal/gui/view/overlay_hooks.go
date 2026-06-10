package view

import "sync/atomic"

// OnOverlayShow is called when the first Fyne overlay (menu, dialog) becomes visible.
// Register this to hide the Metal video overlay on macOS.
var OnOverlayShow func()

// OnOverlayHide is called when the last active Fyne overlay is dismissed.
// Register this to restore the Metal video overlay on macOS.
var OnOverlayHide func()

var overlayDepth atomic.Int32

// overlayShow must be called whenever a managed overlay (dropdownPopup, VideoStartDialog) is shown.
func overlayShow() {
	if overlayDepth.Add(1) == 1 && OnOverlayShow != nil {
		OnOverlayShow()
	}
}

// overlayHide must be called whenever a managed overlay is fully dismissed.
func overlayHide() {
	if overlayDepth.Add(-1) <= 0 {
		overlayDepth.Store(0)
		if OnOverlayHide != nil {
			OnOverlayHide()
		}
	}
}
