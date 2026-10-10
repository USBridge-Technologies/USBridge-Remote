//go:build js && wasm

package api

// A browser has no TCP sockets: the clipboard sync's direct WebSocket dial
// (gorilla over net.Dial) can only fail there.
const clipboardDirectDial = false
