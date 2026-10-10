//go:build !wasm

package glfw

import "github.com/go-gl/glfw/v3.3/glfw"

// GetClipboardString returns the contents of the system clipboard, if it contains or is convertible to a UTF-8 encoded string.
//
// This function may only be called from the main thread.
func GetClipboardString() string {
	return glfw.GetClipboardString()
}

// SetClipboardString sets the system clipboard to the specified UTF-8 encoded string.
//
// This function may only be called from the main thread.
func SetClipboardString(text string) {
	glfw.SetClipboardString(text)
}
