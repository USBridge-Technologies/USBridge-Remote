//go:build wasm

package glfw

import (
	"syscall/js"
	"time"
)

var clipboard = js.Global().Get("navigator").Get("clipboard")

// clipboardReadTimeout bounds GetClipboardString's wait.
//
// USBridge patch (upstream v0.3.0 waits forever): Fyne calls this
// synchronously on its main goroutine (widget.Entry's paste), and
// navigator.clipboard.readText() can stay pending indefinitely -- Chrome's
// "see the clipboard" permission prompt, left unanswered, keeps it open --
// which froze the whole web client. Past the timeout the read counts as
// empty; the client's own paste paths (index.html's paste-event bridge,
// view.PasteFromBrowserClipboard) read asynchronously instead.
const clipboardReadTimeout = 2 * time.Second

// GetClipboardString returns the contents of the system clipboard, if it contains or is convertible to a UTF-8 encoded string.
//
// This function may only be called from the main thread.
func GetClipboardString() string {
	if clipboard.IsUndefined() || clipboard.Get("readText").IsUndefined() {
		return ""
	}
	// Buffered: a read that settles after the timeout must not block the
	// JS callback that delivers it.
	text := make(chan string, 1)

	var read, handleError js.Func
	read = js.FuncOf(func(this js.Value, p []js.Value) any {
		defer read.Release()
		defer handleError.Release()
		if len(p) == 0 || !p[0].Truthy() {
			text <- ""
			return nil
		}
		text <- p[0].String()
		return nil
	})
	handleError = js.FuncOf(func(this js.Value, args []js.Value) any {
		defer read.Release()
		defer handleError.Release()
		text <- ""
		return nil
	})

	clipboard.Call("readText").Call("then", read).Call("catch", handleError)
	select {
	case s := <-text:
		return s
	case <-time.After(clipboardReadTimeout):
		return ""
	}
}

// SetClipboardString sets the system clipboard to the specified UTF-8 encoded string.
//
// This function may only be called from the main thread.
func SetClipboardString(str string) {
	clipboard.Call("writeText", str)
}
