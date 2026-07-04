//go:build windows

package service

import (
	"syscall"

	"golang.org/x/sys/windows"
)

// setSocketReuseAddr sets SO_REUSEADDR for the socket (Windows version)
func setSocketReuseAddr(c syscall.RawConn) error {
	var opErr error
	err := c.Control(func(h uintptr) {
		// On Windows, a socket is a HANDLE, using the correct type
		s := windows.Handle(h)
		// Set SO_REUSEADDR for immediate port reuse
		opErr = windows.SetsockoptInt(s, windows.SOL_SOCKET, windows.SO_REUSEADDR, 1)
	})
	if err != nil {
		return err
	}
	return opErr
}
