//go:build !windows

package main

import (
	"io"
	"os"
)

func validStdoutWriter() io.Writer {
	if os.Stdout == nil {
		return nil
	}
	if _, err := os.Stdout.Stat(); err != nil {
		return nil
	}
	return os.Stdout
}
