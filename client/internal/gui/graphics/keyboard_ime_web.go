//go:build js && wasm

package graphics

// RegisterAsIMETarget is a no-op under wasm -- there is no native OS IME
// to register with the way Android's KeyboardBridge JNI or iOS's own IME
// notifications do (see keyboard_ime_android.go/keyboard_ime_ios.go).
// virtual_keyboard_mobile.go's FocusInput/BlurInput already trigger the
// browser's real on-screen keyboard on their own: they call Fyne's plain
// Canvas.Focus()/Focus(nil), and Fyne's own wasm device code
// (internal/driver/glfw/device_wasm.go's showVirtualKeyboard, via the
// #dummyEntry hidden <input>) already hooks focus changes on a
// text-focusable widget to pop the browser's IME -- no wasm-specific
// wiring needed here beyond satisfying this one method virtual_keyboard_
// mobile.go expects to exist (the IME *height* reporting the Android/iOS
// versions get via native callbacks has no wasm equivalent, so
// adjustForIME's small fallback padding is what applies here instead).
func (vk *VirtualKeyboard) RegisterAsIMETarget() {}

// GetLastIMEH mirrors virtual_keyboard_desktop.go's own stub -- no native
// IME height reporting exists under wasm either, see this file's top doc
// comment.
func GetLastIMEH() float32 {
	return 0
}
