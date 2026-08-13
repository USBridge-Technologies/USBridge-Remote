//go:build js && wasm

package controller

import (
	"syscall/js"

	"fyne.io/fyne/v2"
)

// pasteClipboardInto reads the system clipboard and replaces entry's whole
// content with it -- the browser build's counterpart to
// connection_dialog_paste_default.go's synchronous Fyne Clipboard().Content()
// call, which this project has already confirmed is unreliable under wasm
// (see internal/gui/ime_bridge_wasm.go's doc comment on its Ctrl+V bridge):
// it routes through navigator.clipboard.readText(), whose Promise-based
// permission model doesn't tolerate being blocked on synchronously from Go
// the way Fyne's own path tries to.
//
// This exists as an explicit "Paste" button next to each address/key
// field's existing Copy button because it's the only paste affordance that
// works at all in some browsers: confirmed live, the Meta Quest Browser's
// flat 2D panel window never offers a native context menu (no
// 'contextmenu' event for a held controller trigger the way a real
// long-press fires one on Android/desktop), so there's no OS-level Paste
// to fall back on there.
func pasteClipboardInto(entry *connectionDialogEntry, _ fyne.Window) {
	if entry == nil {
		return
	}
	clipboard := js.Global().Get("navigator").Get("clipboard")
	if clipboard.IsUndefined() || clipboard.IsNull() || clipboard.Get("readText").IsUndefined() {
		return
	}
	promise := clipboard.Call("readText")
	promise.Call("then",
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			defer recoverTouchPanic("paste-field resolve")
			if len(args) == 0 {
				return nil
			}
			text := args[0].String()
			if text == "" {
				return nil
			}
			fyne.Do(func() {
				applyPastedText(entry, text)
			})
			return nil
		}),
		js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			defer recoverTouchPanic("paste-field reject")
			return nil
		}),
	)
}
