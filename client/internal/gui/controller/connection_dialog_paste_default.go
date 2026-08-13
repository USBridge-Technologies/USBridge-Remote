//go:build !(js && wasm)

package controller

import "fyne.io/fyne/v2"

// pasteClipboardInto reads the system clipboard and replaces entry's whole
// content with it, via Fyne's own (synchronous, OS-native) Clipboard API --
// see connection_dialog_paste_wasm.go's counterpart for why the browser
// build needs a different, async path instead.
func pasteClipboardInto(entry *connectionDialogEntry, window fyne.Window) {
	if entry == nil || window == nil {
		return
	}
	text := window.Clipboard().Content()
	if text == "" {
		return
	}
	applyPastedText(entry, text)
}
