//go:build !windows

package usbpass

import (
	"errors"
	"syscall"
)

func isConnReset(err error) bool { return errors.Is(err, syscall.ECONNRESET) }
