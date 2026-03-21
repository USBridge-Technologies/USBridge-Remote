package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"time"
)

func writeStartupTrace(format string, args ...any) {
	logPath := startupTracePath()
	if logPath == "" {
		return
	}

	if err := os.MkdirAll(filepath.Dir(logPath), 0755); err != nil {
		return
	}

	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()

	line := fmt.Sprintf(format, args...)
	_, _ = fmt.Fprintf(f, "%s %s\n", time.Now().Format("2006-01-02 15:04:05.000"), line)
}

func writeStartupPanicTrace(recovered any) {
	writeStartupTrace("PANIC: %v", recovered)
	_ = os.WriteFile(startupTracePath()+".panic.txt", debug.Stack(), 0644)
}

func startupTracePath() string {
	if wd, err := os.Getwd(); err == nil {
		return filepath.Join(wd, "logs", "startup_trace.log")
	}

	if exePath, err := os.Executable(); err == nil {
		return filepath.Join(filepath.Dir(exePath), "logs", "startup_trace.log")
	}

	return ""
}
