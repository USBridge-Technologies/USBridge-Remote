// +build android

package nbdbridge

import (
	"runtime"
	"strings"
	"sync"

	"github.com/sirupsen/logrus"
)

// Обработчики, которые могут быть установлены из других пакетов для предотвращения циклической зависимости
var (
	GlobalSuccessHandler func(uri string, fd int, size int64)
	GlobalErrorHandler   func(err string)
)

var (
	// Callbacks для SAF результата
	safSuccessCallback func(uri string, fd int, size int64)
	safErrorCallback   func(error string)

	// Mutex для защиты SAF callbacks
	callbackMu sync.RWMutex

	// QR результат — polling. Результат приходит через JNI (QRResultBridge → main app), не через nbdbridge.aar
	qrResultMu    sync.Mutex
	qrResult      *QRScanResult
	qrResultReady bool
)

// QRScanResult содержит результат сканирования QR
type QRScanResult struct {
	Contents  string // QR содержимое (если успех)
	ImageData []byte // Изображение для сканирования (если ZXing не распознал)
	Cancelled bool   // Пользователь отменил
}

// SetSAFCallbacks устанавливает callbacks для SAF
// Вызывается из Go UI кода перед открытием SAF пикера
func SetSAFCallbacks(onSuccess func(uri string, fd int, size int64), onError func(error string)) {
	callbackMu.Lock()
	defer callbackMu.Unlock()
	safSuccessCallback = onSuccess
	safErrorCallback = onError
	logrus.Infof("📍 [NBDBRIDGE] SAF callbacks установлены (success: %v, error: %v)", onSuccess != nil, onError != nil)
}

// OnSAFSuccess вызывается из Kotlin когда файл выбран
// Эта функция экспортируется через gomobile и доступна как Nbdbridge.onSAFSuccess()
func OnSAFSuccess(uri string, fd int64, size int64) {
	logrus.Infof("🏁 [SAF-CALLBACK-ENTRY] OnSAFSuccess triggered: uri=%s, fd=%d, size=%d", uri, fd, size)
	
	// Закрепляем горутину на OS потоке для стабильности JNI вызова
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	defer func() {
		if r := recover(); r != nil {
			logrus.Errorf("❌ [SAF-CALLBACK-PANIC] panic в OnSAFSuccess: %v", r)
		}
	}()

	// Делаем копию строки чтобы она не зависели от JNI памяти
	uriCopy := strings.Clone(uri)

	callbackMu.RLock()
	callback := safSuccessCallback
	logrus.Infof("📍 [SAF-CALLBACK-STATE] Current callback: %v", callback != nil)
	callbackMu.RUnlock()

	if callback != nil {
		logrus.Infof("📞 [SAF-CALLBACK-INVOKE] Calling Go success callback...")
		// Вызываем callback в горутине чтобы не блокировать JNI поток
		go func(c func(string, int, int64), u string, f int, s int64) {
			defer func() {
				if r := recover(); r != nil {
					logrus.Errorf("❌ [SAF-CALLBACK-PANIC] panic в SAF success callback: %v", r)
				}
			}()
			c(u, f, s)
			logrus.Info("✅ [SAF-CALLBACK-INVOKE-DONE] Go success callback completed")
		}(callback, uriCopy, int(fd), size)
	} else {
		logrus.Warn("⚠️ [SAF-CALLBACK-WARN] No success callback registered in nbdbridge!")
	}

	// Попытка вызвать глобальный обработчик (внедренный)
	if GlobalSuccessHandler != nil {
		logrus.Info("📞 [SAF-GLOBAL-INVOKE] Calling injected global success handler...")
		go GlobalSuccessHandler(uriCopy, int(fd), size)
	}
}
func OnSAFError(errorMsg string) {
	logrus.Errorf("🏁 [SAF-ERROR-ENTRY] OnSAFError triggered: %s", errorMsg)

	// Закрепляем горутину на OS потоке для стабильности JNI вызова
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	defer func() {
		if r := recover(); r != nil {
			logrus.Errorf("❌ [SAF-CALLBACK-PANIC] panic в OnSAFError: %v", r)
		}
	}()

	// Делаем копию строки чтобы она не зависели от JNI памяти
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

// SetQRResultFromJNI вызывается из main app через JNI (QRResultBridge.deliverQRResult).
// Не использует nbdbridge.aar — результат приходит напрямую в main app.
func SetQRResultFromJNI(contents string) {
	contentsCopy := strings.Clone(contents)
	logrus.Infof("Go: QR result from JNI - %s", contentsCopy)
	qrResultMu.Lock()
	qrResult = &QRScanResult{Contents: contentsCopy}
	qrResultReady = true
	qrResultMu.Unlock()
}

// SetQRResultCancelledFromJNI вызывается из main app через JNI (QRResultBridge.deliverQRCancel)
func SetQRResultCancelledFromJNI() {
	logrus.Info("Go: QR cancelled from JNI")
	qrResultMu.Lock()
	qrResult = &QRScanResult{Cancelled: true}
	qrResultReady = true
	qrResultMu.Unlock()
}

// ClearQRResult очищает результат QR сканирования (вызывать перед запуском сканера)
func ClearQRResult() {
	qrResultMu.Lock()
	qrResult = nil
	qrResultReady = false
	qrResultMu.Unlock()
	logrus.Info("Go: QR result cleared")
}

// GetQRResult возвращает результат QR сканирования (polling)
// Возвращает nil если результат ещё не готов
func GetQRResult() *QRScanResult {
	qrResultMu.Lock()
	defer qrResultMu.Unlock()
	if !qrResultReady {
		return nil
	}
	return qrResult
}

// IsQRResultReady проверяет готов ли результат
func IsQRResultReady() bool {
	qrResultMu.Lock()
	defer qrResultMu.Unlock()
	return qrResultReady
}

// OnQRScanSuccess — для nbdbridge.aar (SAF/другие сценарии). QR теперь идёт через QRResultBridge/JNI.
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

// OnCameraImageReceived — для nbdbridge.aar
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

// OnQRScanCancel — для nbdbridge.aar
func OnQRScanCancel() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	logrus.Info("Go: QR scan cancelled (AAR)")
	qrResultMu.Lock()
	qrResult = &QRScanResult{Cancelled: true}
	qrResultReady = true
	qrResultMu.Unlock()
}
