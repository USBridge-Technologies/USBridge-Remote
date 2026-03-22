package main

import (
	"fmt"
	"os"
	"runtime/debug"
	"strings"
	"time"
)

func writeStartupTrace(format string, args ...any) {
	f, err := os.OpenFile(appLogPath(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()

	line := fmt.Sprintf(format, args...)
	_, _ = fmt.Fprintf(f, "%s [startup] %s\n", time.Now().Format("2006-01-02 15:04:05.000"), line)
}

func writeStartupPanicTrace(recovered any) {
	writeStartupTrace("PANIC: %v", recovered)
	stack := strings.TrimSpace(string(debug.Stack()))
	if stack == "" {
		return
	}
	for _, line := range strings.Split(stack, "\n") {
		writeStartupTrace("stack: %s", line)
	}
}
