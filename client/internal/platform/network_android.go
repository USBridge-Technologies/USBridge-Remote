//go:build android

package platform

/*
#include <jni.h>
#include <stdlib.h>

extern void Java_io_usbridge_client_NetworkBridge_onNetworkChanged(JNIEnv* env, jclass clazz);
*/
import "C"
import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/sirupsen/logrus"
)

var (
	onNetworkChangedCallback func()
	lastNotificationTime     time.Time
	notificationMu           sync.Mutex
	appReadyFlag             atomic.Bool
)

// SetAppReady marks the app as fully initialized so network change callbacks are processed.
func SetAppReady() {
	appReadyFlag.Store(true)
}

// SetOnNetworkChangedCallback sets the callback for network change events
func SetOnNetworkChangedCallback(cb func()) {
	notificationMu.Lock()
	defer notificationMu.Unlock()
	onNetworkChangedCallback = cb
}

//export Java_io_usbridge_client_NetworkBridge_onNetworkChanged
func Java_io_usbridge_client_NetworkBridge_onNetworkChanged(env *C.JNIEnv, clazz C.jclass) {
	if !appReadyFlag.Load() {
		return // App not fully initialized yet, ignore early callbacks
	}

	notificationMu.Lock()
	defer notificationMu.Unlock()

	now := time.Now()
	if now.Sub(lastNotificationTime) < 500*time.Millisecond {
		return // Debounce rapid notifications
	}
	lastNotificationTime = now

	logrus.Info("🌐 [JNI] Network change detected from Android")
	if onNetworkChangedCallback != nil {
		go onNetworkChangedCallback() // Run in goroutine to avoid blocking JNI thread
	}
}
