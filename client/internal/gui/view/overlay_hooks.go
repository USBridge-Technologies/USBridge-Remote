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

// navVideoHidden is a plain last-write-wins flag for "app-level navigation
// (tab switch, connection-manager screen) currently wants the video hidden"
// -- deliberately separate from overlayDepth above. That counter composes
// contributions from independently-tracked call sites (nav, dropdownPopup,
// VideoStartDialog) and is only as correct as every one of those pairings;
// a single lost Hide call (e.g. a caller's hook getting nil'd out between a
// Show and its matching Hide -- see MainWindow.stopMetalVideo callers on
// wasm) leaves it stuck above zero indefinitely, forcing whatever reads it
// permanently hidden until something else happens to reset it. Nav state
// itself never has that failure mode: SetNavVideoHidden is called with the
// live boolean on every navigation transition (see
// MainWindow.syncVideoOverlayForNav), so a reader that polls NavVideoHidden
// on every tick (rather than caching a copy pushed via a callback) is
// always at most one tick stale and self-heals the moment nav state
// changes again -- no registration/teardown pairing to get wrong.
var navVideoHidden atomic.Bool

// SetNavVideoHidden records whether app-level navigation currently wants the
// video (and anything layered on top of it, e.g. wasm's touch/cursor
// overlays) hidden. Call on every navigation transition, unconditionally --
// see navVideoHidden's doc comment for why this is safe to call redundantly
// and why it's the drift-proof alternative to NotifyOverlayShow/Hide for
// this specific purpose.
func SetNavVideoHidden(hidden bool) {
	navVideoHidden.Store(hidden)
}

// NavVideoHidden reports the current value set by SetNavVideoHidden. Poll
// this directly on every check rather than caching the result across ticks.
func NavVideoHidden() bool {
	return navVideoHidden.Load()
}

// overlayDepthActive is the unlogged twin of OverlayActive, for callers that
// poll several times a second (VideoShouldBeHidden, PopupActive below)
// where OverlayActive's own Infof on every call would flood the log for no
// diagnostic benefit.
func overlayDepthActive() bool {
	return overlayDepth.Load() > 0
}

// PopupActive is the nav-free half of VideoShouldBeHidden below: true only
// when a managed overlay (dropdownPopup, VideoStartDialog, an
// OverlayPopupSpec-based popup/settings screen) is open, regardless of
// which tab is selected. Exists as its own call because the real <video>
// element (video_widget_dom_overlay_wasm.go's syncVideoOverlay) needs to
// treat "off the Control tab" and "a popup is open" differently -- see
// that function's own doc comment: coupling both to one visibility:hidden
// brought back a reconnect-every-few-seconds regression -- the exact
// class of bug this package already paid once to fix.
func PopupActive() bool {
	return overlayDepthActive()
}

// VideoShouldBeHidden reports whether wasm's video/touch/cursor overlays
// should currently be hidden, for any reason: app-level navigation
// (NavVideoHidden, off the Control tab) OR a managed overlay -- the
// header dropdown, VideoStartDialog, or any OverlayPopupSpec-based
// popup/settings screen -- currently shown on top of it (the same
// overlayDepth counter those already maintain via overlayShow/overlayHide
// above). Poll this fresh on every tick rather than caching it, same
// reasoning as NavVideoHidden's own doc comment: nothing here is a
// registered callback that can be nil'd out and lost mid-pairing, so even
// a momentarily wrong depth or nav flag self-heals on the very next
// ~150ms poll instead of getting stuck.
func VideoShouldBeHidden() bool {
	return navVideoHidden.Load() || overlayDepthActive()
}
