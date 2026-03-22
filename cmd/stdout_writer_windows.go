//go:build windows

package main

import (
	"io"
	"os"
	"syscall"
)

func validStdoutWriter() io.Writer {
	if os.Stdout == nil {
		return nil
	}
	if _, err := syscall.GetFileType(syscall.Handle(os.Stdout.Fd())); err != nil {
		return nil
	}
	if _, err := os.Stdout.Stat(); err != nil {
		return nil
	}
	return os.Stdout
}
