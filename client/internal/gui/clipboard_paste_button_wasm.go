//go:build js && wasm

// A small floating "Paste" button rendered as a real DOM element, not a
// Fyne widget -- the fallback for browsers where no native context-menu/
// paste UI is reachable at all. Confirmed live: the Meta Quest Browser's
// flat 2D panel window never fires a 'contextmenu' event for a held
// controller trigger the way a real long-press does on Android/desktop
// (tap events land fine, but there is no equivalent gesture Quest maps to
// "open context menu"), so nothing in this app's other paste paths ever
// fires there either -- not web/index.html's Ctrl+V/native-`paste`-event
// bridge (there's no physical keyboard, and no OS Edit-menu action to
// trigger a genuine `paste` DOM event in the first place), not the
// controller/video_gestures_wasm.go contextmenu passthrough for editable
// fields (nothing to pass through if the browser never offers a menu at
// all). A plain on-page button is the one paste affordance every engine
// with the Clipboard API honors from nothing but a click.
package gui

import (
	"syscall/js"

	"github.com/sirupsen/logrus"
)

func recoverClipboardPanic(where string) {
	if r := recover(); r != nil {
		logrus.Errorf("📋 recovered panic in %s: %v", where, r)
	}
}

// InitClipboardPasteButton creates the floating paste button and wires its
// click handler. Called once at wasm startup (see cmd/wasm/main.go), same
// convention as InitIMEBridge/InitTouchGestureBridge. A no-op (nothing
// shown) on engines without navigator.clipboard.readText (e.g. Firefox for
// arbitrary pages -- see web/index.html's own doc comment on that same
// limitation): a button that can only ever fail isn't worth showing.
func InitClipboardPasteButton() {
	doc := js.Global().Get("document")
	body := doc.Get("body")
	if body.IsNull() || body.IsUndefined() {
		js.Global().Call("setTimeout", js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			defer recoverClipboardPanic("InitClipboardPasteButton retry")
			InitClipboardPasteButton()
			return nil
		}), 200)
		return
	}

	clipboard := js.Global().Get("navigator").Get("clipboard")
	if clipboard.IsUndefined() || clipboard.IsNull() || clipboard.Get("readText").IsUndefined() {
		logrus.Info("[clipboard-btn] navigator.clipboard.readText unavailable, skipping paste button")
		return
	}

	btn := doc.Call("createElement", "button")
	btn.Set("id", "usbridge-paste-btn")
	btn.Set("type", "button")
	btn.Set("textContent", "\U0001F4CB Paste")
	style := btn.Get("style")
	style.Set("position", "fixed")
	style.Set("right", "10px")
	style.Set("bottom", "10px")
	style.Set("zIndex", "1000")
	style.Set("padding", "8px 14px")
	style.Set("fontSize", "16px")
	style.Set("borderRadius", "8px")
	style.Set("border", "1px solid rgba(255,255,255,0.35)")
	style.Set("background", "rgba(0,0,0,0.55)")
	style.Set("color", "#fff")
	style.Set("cursor", "pointer")
	body.Call("appendChild", btn)

	btn.Call("addEventListener", "click", js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		defer recoverClipboardPanic("paste-button click")
		promise := clipboard.Call("readText")
		promise.Call("then",
			js.FuncOf(func(this js.Value, args []js.Value) interface{} {
				defer recoverClipboardPanic("paste-button resolve")
				if len(args) == 0 {
					return nil
				}
				text := args[0].String()
				if text == "" {
					return nil
				}
				// usbridgePasteText is registered by
				// internal/gui/ime_bridge_wasm.go -- injects the text into
				// whatever Fyne widget currently has focus, same hand-off
				// web/index.html's own Ctrl+V bridge uses.
				if paste := js.Global().Get("usbridgePasteText"); !paste.IsUndefined() && !paste.IsNull() {
					paste.Invoke(text)
				}
				return nil
			}),
			js.FuncOf(func(this js.Value, args []js.Value) interface{} {
				defer recoverClipboardPanic("paste-button reject")
				logrus.Warn("[clipboard-btn] navigator.clipboard.readText() rejected (permission denied or empty clipboard)")
				return nil
			}),
		)
		return nil
	}), false)

	logrus.Info("[clipboard-btn] floating paste button installed")
}
