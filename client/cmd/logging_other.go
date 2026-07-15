//go:build !windows

package main

import (
	"io"
	"os"
)

func setupPlatformLogOutput(logFile *os.File) (io.Writer, error) {
	if logFile == nil {
		return os.Stdout, nil
	}
	return io.MultiWriter(os.Stdout, logFile), nil
}
