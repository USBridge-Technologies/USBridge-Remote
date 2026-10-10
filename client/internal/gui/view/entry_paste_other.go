//go:build !(js && wasm)

package view

import "fyne.io/fyne/v2"

// BrowserHandlesPaste: only the browser build intercepts paste (see
// entry_paste_wasm.go).
func BrowserHandlesPaste(fyne.Shortcut) bool { return false }

// PasteFromBrowserClipboard: only the browser build; false leaves the paste
// to widget.Entry.
func PasteFromBrowserClipboard() bool { return false }

// ReadClipboard calls fn with the clipboard's text (synchronously outside
// the browser build).
func ReadClipboard(w fyne.Window, fn func(text string)) {
	if w == nil {
		fn("")
		return
	}
	fn(w.Clipboard().Content())
}
