//go:build android
// +build android

package platform

/*
#cgo LDFLAGS: -llog

#include <android/log.h>
#include <stdlib.h>
#include <string.h>

#define LOG_TAG "USBridge"

void android_log_info(const char* msg) {
    __android_log_print(ANDROID_LOG_INFO, LOG_TAG, "%s", msg);
}

void android_log_warn(const char* msg) {
    __android_log_print(ANDROID_LOG_WARN, LOG_TAG, "%s", msg);
}

void android_log_error(const char* msg) {
    __android_log_print(ANDROID_LOG_ERROR, LOG_TAG, "%s", msg);
}
*/
import "C"

import (
	"io"
	"strings"
	"unsafe"

	"github.com/sirupsen/logrus"
)

// AndroidLogWriter implements io.Writer and writes to the Android logcat
type AndroidLogWriter struct {
	level int // ANDROID_LOG_INFO, WARN, ERROR
}

// Write writes data to logcat (splits by lines, max 4KB per call)
func (w *AndroidLogWriter) Write(p []byte) (n int, err error) {
	if len(p) == 0 {
		return 0, nil
	}
	s := strings.TrimRight(string(p), "\r\n")
	lines := strings.Split(s, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// logcat limit is ~4KB, truncate long lines
		if len(line) > 4000 {
			line = line[:4000] + "..."
		}
		cstr := C.CString(line)
		switch w.level {
		case 4: // ANDROID_LOG_WARN
			C.android_log_warn(cstr)
		case 6: // ANDROID_LOG_ERROR
			C.android_log_error(cstr)
		default:
			C.android_log_info(cstr)
		}
		C.free(unsafe.Pointer(cstr))
	}
	return len(p), nil
}

// NewAndroidLogWriter creates a writer for logcat (level: 3=DEBUG, 4=INFO, 5=WARN, 6=ERROR)
func NewAndroidLogWriter(level int) io.Writer {
	return &AndroidLogWriter{level: level}
}

// SetupLogrusForAndroid redirects logrus to the Android logcat
// On Android, os.Stdout often doesn't reach adb logcat — we use __android_log_print instead
func SetupLogrusForAndroid() {
	mw := io.MultiWriter(
		NewAndroidLogWriter(4), // INFO — write to logcat with the USBridge tag
	)
	logrus.SetOutput(mw)
}
