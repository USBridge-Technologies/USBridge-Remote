package view

import (
	"sync/atomic"

	"github.com/sirupsen/logrus"
)

// OnOverlayShow is called when the first Fyne overlay (menu, dialog) becomes visible.
// Register this to hide the Metal video overlay on macOS.
var OnOverlayShow func()

// OnOverlayHide is called when the last active Fyne overlay is dismissed.
// Register this to restore the Metal video overlay on macOS.
var OnOverlayHide func()

var overlayDepth atomic.Int32

// NotifyOverlayShow is the exported version of overlayShow for callers outside
// the view package (e.g. the Android virtual-keyboard handler).
func NotifyOverlayShow() { overlayShow() }

// NotifyOverlayHide is the exported version of overlayHide for callers outside
// the view package.
func NotifyOverlayHide() { overlayHide() }

// overlayShow must be called whenever a managed overlay (dropdownPopup, VideoStartDialog) is shown.
func overlayShow() {
	depth := overlayDepth.Add(1)
	logrus.Infof("📌 [OVERLAY] show depth=%d", depth)
	if depth == 1 && OnOverlayShow != nil {
		OnOverlayShow()
	}
}

func overlayHide() {
	depth := overlayDepth.Add(-1)
	logrus.Infof("📌 [OVERLAY] hide depth=%d", depth)
	if depth <= 0 {
		overlayDepth.Store(0)
		if OnOverlayHide != nil {
			OnOverlayHide()
		}
	}
}

// OverlayActive returns true when at least one managed overlay is currently visible.
// Use this after creating a native video layer to check whether it should start hidden.
func OverlayActive() bool {
	active := overlayDepth.Load() > 0
	logrus.Infof("📌 [OVERLAY] active check=%v (depth=%d)", active, overlayDepth.Load())
	return active
}
