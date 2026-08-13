//go:build js && wasm

// TEMPORARY: diagnostic-only instrumentation for the Meta Quest Browser
// drag investigation (see video_gestures_wasm.go's InitTouchGestureBridge
// call site). Delete this whole file once the question it exists to
// answer -- what event type/target a held controller trigger actually
// generates -- has an answer from real device logs.
package controller

import (
	"syscall/js"

	"github.com/sirupsen/logrus"
)

// installDragDiagnostics logs every pointer/mouse/touch/drag-related
// document-level event, with just enough detail (type, pointerType, id,
// coordinates, target) to tell whether a held Quest controller trigger
// reaches the page at all and what shape it takes -- without touching or
// interfering with any real gesture-handling code (capture, passive:true,
// no preventDefault, this file only ever reads).
func installDragDiagnostics(doc js.Value) {
	types := []string{
		"pointerdown", "pointermove", "pointerup", "pointercancel",
		"mousedown", "mousemove", "mouseup",
		"touchstart", "touchmove", "touchend", "touchcancel",
		"dragstart", "dragend",
	}
	for _, t := range types {
		eventType := t
		doc.Call("addEventListener", eventType, js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			defer recoverTouchPanic("drag-diagnostics")
			if len(args) == 0 {
				return nil
			}
			e := args[0]
			target := ""
			if tgt := e.Get("target"); !tgt.IsUndefined() && !tgt.IsNull() {
				if id := tgt.Get("id"); !id.IsUndefined() && id.String() != "" {
					target = id.String()
				} else if tag := tgt.Get("tagName"); !tag.IsUndefined() {
					target = tag.String()
				}
			}
			pointerType := ""
			pointerID := ""
			if pt := e.Get("pointerType"); !pt.IsUndefined() {
				pointerType = pt.String()
			}
			if pid := e.Get("pointerId"); !pid.IsUndefined() {
				pointerID = pid.String()
			}
			x, y := "", ""
			if cx := e.Get("clientX"); !cx.IsUndefined() {
				x = cx.String()
			}
			if cy := e.Get("clientY"); !cy.IsUndefined() {
				y = cy.String()
			}
			logrus.Infof("[drag-diag] %s target=%s pointerType=%s pointerId=%s x=%s y=%s", eventType, target, pointerType, pointerID, x, y)
			return nil
		}), map[string]interface{}{"capture": true, "passive": true})
	}
	logrus.Info("[drag-diag] instrumentation installed")
}
