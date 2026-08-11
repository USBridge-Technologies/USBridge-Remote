//go:build js && wasm

package service

import "syscall"

// setSocketReuseAddr is a no-op on wasm/browser builds: there is no raw
// socket layer to set SO_REUSEADDR on, and the NBD virtual-disk subsystem
// that calls this never actually runs in a browser (no raw block-device
// access is possible there either).
func setSocketReuseAddr(c syscall.RawConn) error { return nil }
