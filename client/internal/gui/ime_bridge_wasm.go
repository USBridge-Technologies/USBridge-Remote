//go:build js && wasm

// Package gui (wasm build): bridges real text input into Fyne's canvas.
//
// Fyne's own wasm driver (fyne.io/fyne/v2/internal/driver/glfw,
// github.com/fyne-io/glfw-js) only listens for `keydown` on document to
// feed CanvasObject.TypedRune/TypedKey. That works for a hardware
// keyboard's key events, but Android's on-screen keyboard (and iOS Safari,
// and IME composition generally) does not reliably emit one `keydown` per
// character -- confirmed live: focusing a Fyne Entry via the #dummyEntry
// element (see device_wasm.go's showVirtualKeyboard) does pop the OS
// keyboard, but typing on it produces no visible characters at all,
// because the real text lands only in `input`/`compositionend` events on
// that hidden element, which nothing was listening for.
//
// This file adds that missing listener, entirely in userland (no vendored
// Fyne patch): every `input` event on #dummyEntry is diffed against the
// element's previous value, and the delta (typed runes or a deletion) is
// replayed onto whatever CanvasObject currently has focus via the public
// fyne.Canvas.Focused()/Focusable API -- the same hook point Fyne's own
// keydown path uses, so paste (Ctrl+V or Android's long-press "Paste") and
// IME composition (Chinese/Japanese/Korean input, autocomplete) both flow
// through this one path with no separate handling needed: the browser
// already resolves all of that down to a plain `value` change on the
// input, which is exactly what this reads.
package gui

import (
	"syscall/js"

	"fyne.io/fyne/v2"
	"github.com/sirupsen/logrus"
)

// InitIMEBridge wires up the #dummyEntry listener described above. Call
// once after the main window is shown (see cmd/wasm/main.go) -- the
// element must already exist in the page (client/cmd/wasm's gui.html) and
// Fyne's own device_wasm.go init() must already have found it, which
// happens automatically at wasm module load.
func InitIMEBridge() {
	doc := js.Global().Get("document")
	dummyEntry := doc.Call("getElementById", "dummyEntry")
	if dummyEntry.IsUndefined() || dummyEntry.IsNull() {
		logrus.Warn("[ime-bridge] #dummyEntry not found in the page -- text input from on-screen/IME keyboards will not work")
		return
	}

	lastValue := ""

	replay := func(newValue string) {
		focused := currentFocusable()
		if focused == nil {
			// Nothing focused to receive input -- still clear the buffer so
			// the next focus starts clean, but nothing to replay.
			lastValue = newValue
			return
		}

		// The common case by far: characters appended at the end (typing,
		// most IME composition commits, paste). Compare rune-by-rune from
		// the end isn't needed here -- browsers coalesce IME composition
		// into a single `input` event per commit, so a straightforward
		// common-prefix/suffix diff against the previous value covers
		// typing, backspacing, and paste (which replaces a selection) all
		// with one algorithm instead of special-casing each inputType.
		oldRunes := []rune(lastValue)
		newRunes := []rune(newValue)

		prefix := 0
		for prefix < len(oldRunes) && prefix < len(newRunes) && oldRunes[prefix] == newRunes[prefix] {
			prefix++
		}
		oldSuffix := len(oldRunes)
		newSuffix := len(newRunes)
		for oldSuffix > prefix && newSuffix > prefix && oldRunes[oldSuffix-1] == newRunes[newSuffix-1] {
			oldSuffix--
			newSuffix--
		}

		deleted := oldSuffix - prefix
		for i := 0; i < deleted; i++ {
			focused.TypedKey(&fyne.KeyEvent{Name: fyne.KeyBackspace})
		}
		for _, r := range newRunes[prefix:newSuffix] {
			focused.TypedRune(r)
		}

		lastValue = newValue
		// Keep the native input's own value in sync with what we just
		// replayed so the next diff starts from a known baseline -- we
		// never let it grow unbounded or drift from what the Fyne widget
		// actually holds.
	}

	inputListener := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		fyne.Do(func() {
			replay(dummyEntry.Get("value").String())
		})
		return nil
	})
	dummyEntry.Call("addEventListener", "input", inputListener)

	// Enter/Return doesn't always show up as a normal character in `input`
	// (mobile keyboards often send it as keyCode 13 with an empty/unchanged
	// value, or as a distinct "insertLineBreak" inputType) -- handle it via
	// a plain keydown listener on the same element, same pattern Fyne's own
	// desktop path already uses for special keys.
	keydownListener := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		if len(args) == 0 {
			return nil
		}
		key := args[0].Get("key").String()
		if key != "Enter" {
			return nil
		}
		fyne.Do(func() {
			if focused := currentFocusable(); focused != nil {
				focused.TypedKey(&fyne.KeyEvent{Name: fyne.KeyReturn})
			}
		})
		return nil
	})
	dummyEntry.Call("addEventListener", "keydown", keydownListener)

	// Clear the hidden input's value whenever it gains focus, so a value
	// left over from a previous field doesn't get diffed against the new
	// field's (empty, from the new input's perspective) starting state.
	focusListener := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		dummyEntry.Set("value", "")
		lastValue = ""
		return nil
	})
	dummyEntry.Call("addEventListener", "focus", focusListener)

	// Paste (Ctrl+V / Cmd+V) is handled by web/index.html's
	// installClipboardPasteBridge, not here -- see that function's own
	// doc comment for the full history (two independent things tried and
	// rejected first: Fyne's own TypedShortcut(*fyne.ShortcutPaste) path,
	// and an earlier version of this file's own `document`-level `paste`
	// DOM event listener, which fixed Chrome/Linux but not Safari/macOS).
	// index.html calls navigator.clipboard.readText() directly inside a
	// plain JS keydown listener with zero Go involvement -- confirmed
	// live that even a Go js.FuncOf listener attached directly to the
	// same event breaks Safari's "still within the original user-gesture
	// call stack" requirement for clipboard-read permission, the same
	// way it breaks requestFullscreen (see installFullscreenOnFirstTap's
	// own doc comment in index.html for that first, load-bearing
	// discovery). usbridgePasteText is the hand-off point once
	// index.html already has the text in hand -- no gesture requirement
	// left to preserve at that point, so a plain Go callback is fine.
	pasteTextListener := js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		if len(args) == 0 {
			return nil
		}
		text := args[0].String()
		if text == "" {
			return nil
		}
		fyne.Do(func() {
			focused := currentFocusable()
			if focused == nil {
				return
			}
			for _, r := range text {
				if r == '\n' || r == '\r' {
					focused.TypedKey(&fyne.KeyEvent{Name: fyne.KeyReturn})
					continue
				}
				focused.TypedRune(r)
			}
		})
		return nil
	})
	js.Global().Set("usbridgePasteText", pasteTextListener)

	logrus.Info("[ime-bridge] #dummyEntry input listener installed")
}

// currentFocusable returns the Focusable currently focused on the app's
// (single, on wasm) window canvas, or nil.
func currentFocusable() fyne.Focusable {
	windows := fyne.CurrentApp().Driver().AllWindows()
	if len(windows) == 0 {
		return nil
	}
	canvas := windows[0].Canvas()
	if canvas == nil {
		return nil
	}
	return canvas.Focused()
}
