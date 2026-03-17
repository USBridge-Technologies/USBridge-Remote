//go:build windows

package service

import (
	"syscall"
	
	"golang.org/x/sys/windows"
)

// setSocketReuseAddr устанавливает SO_REUSEADDR для сокета (Windows-версия)
func setSocketReuseAddr(c syscall.RawConn) error {
	var opErr error
	err := c.Control(func(h uintptr) {
		// На Windows сокет - это HANDLE, используем правильный тип
		s := windows.Handle(h)
		// Устанавливаем SO_REUSEADDR для немедленного переиспользования порта
		opErr = windows.SetsockoptInt(s, windows.SOL_SOCKET, windows.SO_REUSEADDR, 1)
	})
	if err != nil {
		return err
	}
	return opErr
}

