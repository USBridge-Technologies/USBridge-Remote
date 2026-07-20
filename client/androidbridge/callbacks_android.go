//go:build android
// +build android

package androidbridge

import (
	"runtime"
	"strings"
	"sync"

	"github.com/sirupsen/logrus"
)

// Handlers that can be set from other packages to prevent cyclic dependency
var (
	GlobalSuccessHandler func(uri string, fd int, size int64)
	GlobalErrorHandler   func(err string)
)

var (
	// Callbacks for SAF result
	safSuccessCallback func(uri string, fd int, size int64)
	safErrorCallback   func(error string)

	// Mutex to protect SAF callbacks
	callbackMu sync.RWMutex

	// QR result — polling. Result comes via JNI (QRResultBridge → main app), not via nbdbridge.aar
	qrResultMu    sync.Mutex
	qrResult      *QRScanResult
	qrResultReady bool
)

// QRScanResult contains the result of QR scanning
type QRScanResult struct {
	Contents  string // QR contents (if success)
	ImageData []byte // Image for scanning (if ZXing failed to recognize)
	Cancelled bool   // User cancelled
}

// SetSAFCallbacks sets callbacks for SAF
// Called from Go UI code before opening SAF picker
func SetSAFCallbacks(onSuccess func(uri string, fd int, size int64), onError func(error string)) {
	callbackMu.Lock()
	defer callbackMu.Unlock()
	safSuccessCallback = onSuccess
	safErrorCallback = onError
	logrus.Infof("📍 [NBDBRIDGE] SAF callbacks set (success: %v, error: %v)", onSuccess != nil, onError != nil)
}

// OnSAFSuccess is called from Kotlin when a file is selected
// This function is exported via gomobile and available as Nbdbridge.onSAFSuccess()
func OnSAFSuccess(uri string, fd int64, size int64) {
	logrus.Infof("🏁 [SAF-CALLBACK-ENTRY] OnSAFSuccess triggered: uri=%s, fd=%d, size=%d", uri, fd, size)

	// Lock goroutine to OS thread for JNI call stability
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	defer func() {
		if r := recover(); r != nil {
			logrus.Errorf("❌ [SAF-CALLBACK-PANIC] panic in OnSAFSuccess: %v", r)
		}
	}()

	// Make a copy of the string so it doesn't depend on JNI memory
	uriCopy := strings.Clone(uri)

	callbackMu.RLock()
	callback := safSuccessCallback
	logrus.Infof("📍 [SAF-CALLBACK-STATE] Current callback: %v", callback != nil)
	callbackMu.RUnlock()

	if callback != nil {
		logrus.Infof("📞 [SAF-CALLBACK-INVOKE] Calling Go success callback...")
		// Call callback in goroutine to avoid blocking JNI thread
		go func(c func(string, int, int64), u string, f int, s int64) {
			defer func() {
				if r := recover(); r != nil {
					logrus.Errorf("❌ [SAF-CALLBACK-PANIC] panic in SAF success callback: %v", r)
				}
			}()
			c(u, f, s)
			logrus.Info("✅ [SAF-CALLBACK-INVOKE-DONE] Go success callback completed")
		}(callback, uriCopy, int(fd), size)
	} else {
		logrus.Warn("⚠️ [SAF-CALLBACK-WARN] No success callback registered in nbdbridge!")
	}

	// Attempt to call global handler (injected)
	if GlobalSuccessHandler != nil {
		logrus.Info("📞 [SAF-GLOBAL-INVOKE] Calling injected global success handler...")
		go GlobalSuccessHandler(uriCopy, int(fd), size)
	}
}

func OnSAFError(errorMsg string) {
	logrus.Errorf("🏁 [SAF-ERROR-ENTRY] OnSAFError triggered: %s", errorMsg)

	// Lock goroutine to OS thread for JNI call stability
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	defer func() {
		if r := recover(); r != nil {
			logrus.Errorf("❌ [SAF-CALLBACK-PANIC] panic in OnSAFError: %v", r)
		}
	}()

	// Make a copy of the string so it doesn't depend on JNI memory
	errCopy := strings.Clone(errorMsg)

	callbackMu.RLock()
	callback := safErrorCallback
	callbackMu.RUnlock()

	if callback != nil {
		logrus.Infof("Go: SAF error - %s", errCopy)
		go callback(errCopy)
	}

	if GlobalErrorHandler != nil {
		go GlobalErrorHandler(errCopy)
	}
}

// SetQRResultFromJNI is called from main app via JNI (QRResultBridge.deliverQRResult).
// Does not use nbdbridge.aar — result comes directly to main app.
func SetQRResultFromJNI(contents string) {
	contentsCopy := strings.Clone(contents)
	logrus.Infof("Go: QR result from JNI - %s", contentsCopy)
	qrResultMu.Lock()
	qrResult = &QRScanResult{Contents: contentsCopy}
	qrResultReady = true
	qrResultMu.Unlock()
}

// SetQRResultCancelledFromJNI is called from main app via JNI (QRResultBridge.deliverQRCancel)
func SetQRResultCancelledFromJNI() {
	logrus.Info("Go: QR cancelled from JNI")
	qrResultMu.Lock()
	qrResult = &QRScanResult{Cancelled: true}
	qrResultReady = true
	qrResultMu.Unlock()
}

// ClearQRResult clears QR scan result (call before starting scanner)
func ClearQRResult() {
	qrResultMu.Lock()
	qrResult = nil
	qrResultReady = false
	qrResultMu.Unlock()
	logrus.Info("Go: QR result cleared")
}

// GetQRResult returns QR scan result (polling)
// Returns nil if result is not ready yet
func GetQRResult() *QRScanResult {
	qrResultMu.Lock()
	defer qrResultMu.Unlock()
	if !qrResultReady {
		return nil
	}
	return qrResult
}

// IsQRResultReady checks if result is ready
func IsQRResultReady() bool {
	qrResultMu.Lock()
	defer qrResultMu.Unlock()
	return qrResultReady
}

// OnQRScanSuccess — for nbdbridge.aar (SAF/other scenarios). QR now goes through QRResultBridge/JNI.
func OnQRScanSuccess(contents string) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	contentsCopy := strings.Clone(contents)
	logrus.Infof("Go: QR scan success (AAR) - %s", contentsCopy)
	qrResultMu.Lock()
	qrResult = &QRScanResult{Contents: contentsCopy}
	qrResultReady = true
	qrResultMu.Unlock()
}

// OnCameraImageReceived — for nbdbridge.aar
func OnCameraImageReceived(data []byte) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	logrus.Infof("Go: Camera image received - %d bytes", len(data))
	dataCopy := make([]byte, len(data))
	copy(dataCopy, data)
	qrResultMu.Lock()
	qrResult = &QRScanResult{ImageData: dataCopy}
	qrResultReady = true
	qrResultMu.Unlock()
}

// OnQRScanCancel — for nbdbridge.aar
func OnQRScanCancel() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	logrus.Info("Go: QR scan cancelled (AAR)")
	qrResultMu.Lock()
	qrResult = &QRScanResult{Cancelled: true}
	qrResultReady = true
	qrResultMu.Unlock()
}
