//go:build js && wasm

package view

import (
	"syscall/js"

	"fyne.io/fyne/v2"
)

// BrowserHandlesPaste takes over a paste shortcut the browser build must
// not hand to widget.Entry: its paste reads the clipboard synchronously
// (blocking Fyne's goroutine on navigator.clipboard.readText()), which
// freezes the whole UI until the browser settles the read -- forever if
// its permission prompt goes unanswered. Ctrl+V/Cmd+V in a text field
// never gets here (index.html hands it to the native paste event, see
// installClipboardPasteBridge); what does is a menu's Paste, read
// asynchronously instead. Returns true when it took the shortcut.
func BrowserHandlesPaste(shortcut fyne.Shortcut) bool {
	if _, ok := shortcut.(*fyne.ShortcutPaste); !ok {
		return false
	}
	return PasteFromBrowserClipboard()
}

// ReadClipboard calls fn with the clipboard's text on Fyne's goroutine,
// read without blocking it (see BrowserHandlesPaste); "" when there's
// nothing or the read was refused.
func ReadClipboard(_ fyne.Window, fn func(text string)) {
	clipboard := js.Global().Get("navigator").Get("clipboard")
	if clipboard.IsUndefined() || clipboard.Get("readText").IsUndefined() {
		fyne.Do(func() { fn("") })
		return
	}
	var ok, fail js.Func
	release := func() { ok.Release(); fail.Release() }
	ok = js.FuncOf(func(_ js.Value, args []js.Value) any {
		text := ""
		if len(args) > 0 && args[0].Type() == js.TypeString {
			text = args[0].String()
		}
		release()
		fyne.Do(func() { fn(text) })
		return nil
	})
	fail = js.FuncOf(func(js.Value, []js.Value) any {
		release()
		fyne.Do(func() { fn("") })
		return nil
	})
	clipboard.Call("readText").Call("then", ok, fail)
}

// PasteFromBrowserClipboard reads the clipboard asynchronously and types it
// into the focused widget through the same window.usbridgePasteText hand-off
// the Ctrl+V bridge uses; nothing in Go waits on it. Returns true when it
// took over the paste (always in the browser build).
func PasteFromBrowserClipboard() bool {
	clipboard := js.Global().Get("navigator").Get("clipboard")
	paste := js.Global().Get("usbridgePasteText")
	if clipboard.IsUndefined() || clipboard.Get("readText").IsUndefined() || paste.Type() != js.TypeFunction {
		return true
	}
	clipboard.Call("readText").Call("then", paste).Call("catch", js.FuncOf(func(js.Value, []js.Value) any { return nil }))
	return true
}
