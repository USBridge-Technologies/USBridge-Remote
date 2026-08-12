//go:build !js || !wasm

package view

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
)

// attachTouchScroll/detachTouchScroll only do anything under wasm -- see
// popup_scroll_wasm.go's doc comment for why wasm needs a bridge here at
// all. Every other platform already gets working touch/mouse scroll on a
// container.Scroll for free from Fyne's own native gesture dispatch, so
// there's nothing to wire up here.
func attachTouchScroll(*container.Scroll, []fyne.CanvasObject, []func(), fyne.Position, fyne.Size) {}
func detachTouchScroll()                                                                          {}
