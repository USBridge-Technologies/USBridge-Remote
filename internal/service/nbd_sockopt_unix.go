//go:build !windows

package service

import (
	"syscall"
	
	"golang.org/x/sys/unix"
)

// setSocketReuseAddr устанавливает SO_REUSEADDR для сокета (Unix-версия)
func setSocketReuseAddr(c syscall.RawConn) error {
	var opErr error
	err := c.Control(func(fd uintptr) {
		// На Unix сокет - это int файловый дескриптор
		opErr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEADDR, 1)
	})
	if err != nil {
		return err
	}
	return opErr
}

