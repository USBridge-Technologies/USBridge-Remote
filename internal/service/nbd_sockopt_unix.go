//go:build !windows

package service

import (
	"syscall"
	
	"golang.org/x/sys/unix"
)

// setSocketReuseAddr sets SO_REUSEADDR for the socket (Unix version)
func setSocketReuseAddr(c syscall.RawConn) error {
	var opErr error
	err := c.Control(func(fd uintptr) {
		// On Unix, a socket is an int file descriptor
		opErr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEADDR, 1)
	})
	if err != nil {
		return err
	}
	return opErr
}

