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

// AndroidLogWriter реализует io.Writer и пишет в Android logcat
type AndroidLogWriter struct {
	level int // ANDROID_LOG_INFO, WARN, ERROR
}

// Write пишет данные в logcat (разбивает по строкам, max 4KB на вызов)
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
		// logcat ограничение ~4KB, обрезаем длинные строки
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

// NewAndroidLogWriter создаёт writer для logcat (level: 3=DEBUG, 4=INFO, 5=WARN, 6=ERROR)
func NewAndroidLogWriter(level int) io.Writer {
	return &AndroidLogWriter{level: level}
}

// SetupLogrusForAndroid перенаправляет logrus в Android logcat
// На Android os.Stdout часто не попадает в adb logcat — используем __android_log_print
func SetupLogrusForAndroid() {
	mw := io.MultiWriter(
		NewAndroidLogWriter(4), // INFO — пишем в logcat с тегом USBridge
	)
	logrus.SetOutput(mw)
}
