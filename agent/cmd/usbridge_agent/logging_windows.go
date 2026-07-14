//go:build windows

package main

import (
	"fmt"
	"io"
	"os"

	"golang.org/x/sys/windows"
)

func setupPlatformLogOutput(logFile *os.File) (io.Writer, error) {
	if logFile == nil {
		return os.Stdout, nil
	}

	handle := windows.Handle(logFile.Fd())
	if err := windows.SetStdHandle(windows.STD_OUTPUT_HANDLE, handle); err != nil {
		return nil, fmt.Errorf("redirect stdout: %w", err)
	}
	if err := windows.SetStdHandle(windows.STD_ERROR_HANDLE, handle); err != nil {
		return nil, fmt.Errorf("redirect stderr: %w", err)
	}

	os.Stdout = logFile
	os.Stderr = logFile

	return os.Stdout, nil
}
