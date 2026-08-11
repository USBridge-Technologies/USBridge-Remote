//go:build android || ios || (js && wasm)

package controller

import "sync"

// activeMobileGestureTarget/activeGestureVideoWidget are shared between the
// native Android/iOS gesture bridges (video_gestures_android.go/
// video_gestures_ios.go, driven by real platform gesture recognizers via
// cgo) and the browser build's own gesture bridge
// (video_gestures_wasm.go, driven by raw DOM touch events via syscall/js)
// -- both need the same "which VideoWidget should raw touch/gesture
// updates be delivered to" registration, and there's normally only one
// live VideoWidget per process on any of these platforms. Pulled out of
// video_widget_mobile.go (which is android/ios-only, since the rest of
// that file's virtual-keyboard handling is genuinely native-IME-specific
// and doesn't apply to wasm -- see video_widget_web.go for the browser's
// own, non-native-IME keyboard handling) so the wasm build can share this
// piece without pulling in the native-only parts.
var (
	activeMobileGestureTargetMu sync.RWMutex
	activeMobileGestureTarget   *VideoWidget
)

func activeGestureVideoWidget() *VideoWidget {
	activeMobileGestureTargetMu.RLock()
	defer activeMobileGestureTargetMu.RUnlock()
	return activeMobileGestureTarget
}

func (vw *VideoWidget) platformRegisterGestureTarget() {
	activeMobileGestureTargetMu.Lock()
	activeMobileGestureTarget = vw
	activeMobileGestureTargetMu.Unlock()
}
