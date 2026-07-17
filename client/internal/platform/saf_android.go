//go:build android
// +build android

package platform

/*
#cgo LDFLAGS: -landroid -llog

#include <stdlib.h>

// C function declarations from saf_jni.c
int jni_takePersistableUriPermission(uintptr_t jni_env_ptr, uintptr_t ctx_ptr, const char *uriString);
int jni_openFileDescriptor(uintptr_t jni_env_ptr, uintptr_t ctx_ptr, const char *uriString, const char *mode);
void jni_setContext(uintptr_t jni_vm_ptr, uintptr_t jni_env_ptr, uintptr_t ctx_ptr);
int jni_startSAFPicker();
int jni_hasSAFResult();
char* jni_getSAFFileName();
char* jni_getSAFUri();
int jni_getSAFFd();
long jni_getSAFSize();
void jni_clearSAFResult();
*/
import "C"

import (
	"fmt"
	"os"
	"runtime"
	"sync"
	"unsafe"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver"
	"github.com/sirupsen/logrus"
)

// SAFHelper helps working with Android Storage Access Framework
type SAFHelper struct {
	app       fyne.App
	mu        sync.Mutex
	fdCache   map[string]int      // cache URI -> FD
	fileCache map[string]*os.File // cache URI -> *os.File
}

var (
	safHelper     *SAFHelper
	safHelperOnce sync.Once
)

// GetSAFHelper returns the SAFHelper singleton instance
func GetSAFHelper(app fyne.App) *SAFHelper {
	safHelperOnce.Do(func() {
		logrus.Info("🔧 [SAF] Initializing SAFHelper singleton")
		safHelper = &SAFHelper{
			app:       app,
			fdCache:   make(map[string]int),
			fileCache: make(map[string]*os.File),
		}
	})
	return safHelper
}

// TakePersistableUriPermission saves persistent access to URI
func (sh *SAFHelper) TakePersistableUriPermission(uriString string) error {
	sh.mu.Lock()
	defer sh.mu.Unlock()

	logrus.Infof("📍 [SAF-STEP-1] TakePersistableUriPermission called for URI: %s", uriString)
	logrus.Infof("📍 [SAF-STEP-1] Runtime GOOS: %s, GOARCH: %s", runtime.GOOS, runtime.GOARCH)

	if sh.app == nil {
		err := fmt.Errorf("SAFHelper: app is not initialized")
		logrus.Errorf("❌ [SAF-STEP-1-ERROR] %v", err)
		return err
	}

	logrus.Infof("✅ [SAF-STEP-1] App object is available: %T", sh.app)

	// Channel for receiving the result from native code
	resultChan := make(chan error, 1)

	logrus.Infof("📍 [SAF-STEP-2] Preparing to call driver.RunNative for takePersistableUriPermission")

	// Call the Android JNI via driver.RunNative
	driver.RunNative(func(ctx any) error {
		logrus.Infof("📍 [SAF-STEP-3] Inside RunNative callback, context: %T", ctx)

		// ctx should be an android.app.Activity or Context
		defer func() {
			if r := recover(); r != nil {
				err := fmt.Errorf("panic in RunNative: %v", r)
				logrus.Errorf("❌ [SAF-STEP-3-PANIC] %v", err)
				resultChan <- err
			}
		}()

		// Attempt to obtain the Activity via reflection/JNI
		logrus.Infof("📍 [SAF-STEP-4] Attempting to call JNI for takePersistableUriPermission")

		// A REAL JNI IMPLEMENTATION IS NEEDED HERE
		// For demonstration purposes we use a stub with verbose logging

		if ctx == nil {
			err := fmt.Errorf("RunNative context is nil")
			logrus.Errorf("❌ [SAF-STEP-4-ERROR] %v", err)
			resultChan <- err
			return err
		}

		logrus.Infof("📍 [SAF-STEP-5] Context obtained, attempting to call JNI method")

		// TODO: Actual JNI implementation needed
		// 1. Get ContentResolver via context.getContentResolver()
		// 2. Parse URI via Uri.parse(uriString)
		// 3. Call contentResolver.takePersistableUriPermission(uri,
		//    Intent.FLAG_GRANT_READ_URI_PERMISSION | Intent.FLAG_GRANT_WRITE_URI_PERMISSION)

		err := sh.jniTakePersistableUriPermission(ctx, uriString)
		if err != nil {
			logrus.Errorf("❌ [SAF-STEP-5-ERROR] JNI call failed with error: %v", err)
			resultChan <- err
		} else {
			logrus.Infof("✅ [SAF-STEP-5-SUCCESS] JNI call successful")
			resultChan <- nil
		}
		return err
	})

	logrus.Infof("📍 [SAF-STEP-6] Waiting for result from RunNative...")
	err := <-resultChan

	if err != nil {
		logrus.Errorf("❌ [SAF-STEP-6-ERROR] TakePersistableUriPermission failed with error: %v", err)
		return fmt.Errorf("failed to save permission for URI %s: %v", uriString, err)
	}

	logrus.Infof("✅ [SAF-STEP-6-SUCCESS] Permission for URI %s successfully saved", uriString)
	return nil
}

// OpenFileDescriptor opens a file descriptor via SAF
func (sh *SAFHelper) OpenFileDescriptor(uriString string, mode string) (*os.File, error) {
	sh.mu.Lock()
	defer sh.mu.Unlock()

	logrus.Infof("📍 [SAF-OPENFD-1] OpenFileDescriptor called for URI: %s, mode: %s", uriString, mode)

	// Check the cache
	if cachedFile, exists := sh.fileCache[uriString]; exists {
		// Check that the file is still valid
		if _, err := cachedFile.Stat(); err == nil {
			logrus.Infof("✅ [SAF-OPENFD-CACHE] Returning cached *os.File for URI: %s", uriString)
			return cachedFile, nil
		} else {
			// The file is closed or invalid, remove it from the cache
			logrus.Warnf("⚠️ [SAF-OPENFD-CACHE-INVALID] Cached file is invalid (%v), removing from cache and reopening", err)
			delete(sh.fileCache, uriString)
			delete(sh.fdCache, uriString)
		}
	}

	if sh.app == nil {
		err := fmt.Errorf("SAFHelper: app is not initialized")
		logrus.Errorf("❌ [SAF-OPENFD-1-ERROR] %v", err)
		return nil, err
	}

	logrus.Infof("📍 [SAF-OPENFD-2] Preparing to call driver.RunNative for openFileDescriptor")

	// Channel for receiving the FD from native code
	fdChan := make(chan int, 1)
	errChan := make(chan error, 1)

	// Call the Android JNI via driver.RunNative
	driver.RunNative(func(ctx any) error {
		logrus.Infof("📍 [SAF-OPENFD-3] Inside RunNative callback for openFileDescriptor")

		defer func() {
			if r := recover(); r != nil {
				err := fmt.Errorf("panic in RunNative: %v", r)
				logrus.Errorf("❌ [SAF-OPENFD-3-PANIC] %v", err)
				errChan <- err
			}
		}()

		if ctx == nil {
			err := fmt.Errorf("RunNative context is nil")
			logrus.Errorf("❌ [SAF-OPENFD-3-ERROR] %v", err)
			errChan <- err
			return err
		}

		logrus.Infof("📍 [SAF-OPENFD-4] Attempting to call JNI for openFileDescriptor")

		// TODO: Actual JNI implementation needed:
		// 1. Get the ContentResolver via context.getContentResolver()
		// 2. Parse the URI via Uri.parse(uriString)
		// 3. ParcelFileDescriptor pfd = contentResolver.openFileDescriptor(uri, mode)
		// 4. int fd = pfd.detachFd()
		// 5. Return fd

		fd, err := sh.jniOpenFileDescriptor(ctx, uriString, mode)
		if err != nil {
			logrus.Errorf("❌ [SAF-OPENFD-4-ERROR] JNI call failed with error: %v", err)
			errChan <- err
			return err
		}

		logrus.Infof("✅ [SAF-OPENFD-4-SUCCESS] Obtained FD: %d", fd)
		fdChan <- fd
		return nil
	})

	logrus.Infof("📍 [SAF-OPENFD-5] Waiting for result from RunNative...")

	// Wait for the result
	select {
	case fd := <-fdChan:
		logrus.Infof("✅ [SAF-OPENFD-5] Obtained file descriptor: %d", fd)

		// Cache the FD
		sh.fdCache[uriString] = fd

		// Convert the FD into an *os.File
		logrus.Infof("📍 [SAF-OPENFD-6] Converting FD %d to *os.File", fd)
		file := os.NewFile(uintptr(fd), fmt.Sprintf("saf:%s", uriString))
		if file == nil {
			err := fmt.Errorf("failed to create *os.File from FD %d", fd)
			logrus.Errorf("❌ [SAF-OPENFD-6-ERROR] %v", err)
			return nil, err
		}

		// Cache the File
		sh.fileCache[uriString] = file

		logrus.Infof("✅ [SAF-OPENFD-6-SUCCESS] *os.File successfully created for URI: %s", uriString)
		return file, nil

	case err := <-errChan:
		logrus.Errorf("❌ [SAF-OPENFD-5-ERROR] Error opening FD: %v", err)
		return nil, fmt.Errorf("failed to open file descriptor for URI %s: %v", uriString, err)
	}
}

// CloseFD closes the file descriptor and removes it from the cache
func (sh *SAFHelper) CloseFD(uriString string) error {
	sh.mu.Lock()
	defer sh.mu.Unlock()

	logrus.Infof("📍 [SAF-CLOSE-1] CloseFD called for URI: %s", uriString)

	// Close the File if present
	if file, exists := sh.fileCache[uriString]; exists {
		logrus.Infof("📍 [SAF-CLOSE-2] Closing *os.File for URI: %s", uriString)
		if err := file.Close(); err != nil {
			logrus.Warnf("⚠️ [SAF-CLOSE-2-WARN] Error closing file (already closed?): %v", err)
			// Don't return the error - continue cleaning up the cache
		}
		// Remove from the cache regardless (even if the file was already closed)
		delete(sh.fileCache, uriString)
		logrus.Infof("✅ [SAF-CLOSE-2-SUCCESS] File removed from cache")
	}

	// Remove the FD from the cache
	if _, exists := sh.fdCache[uriString]; exists {
		delete(sh.fdCache, uriString)
		logrus.Infof("✅ [SAF-CLOSE-3] FD removed from cache")
	}

	return nil
}

// GetCachedFile returns the cached *os.File for a URI (if present)
func (sh *SAFHelper) GetCachedFile(uriString string) (*os.File, bool) {
	sh.mu.Lock()
	defer sh.mu.Unlock()

	file, exists := sh.fileCache[uriString]
	if exists {
		logrus.Infof("📍 [SAF-CACHE-HIT] Found cached file for URI: %s", uriString)
	} else {
		logrus.Infof("📍 [SAF-CACHE-MISS] No cached file for URI: %s", uriString)
	}
	return file, exists
}

// jniTakePersistableUriPermission - JNI call via CGO
func (sh *SAFHelper) jniTakePersistableUriPermission(ctx any, uriString string) error {
	logrus.Infof("📍 [SAF-JNI-CALL] Calling C function jni_takePersistableUriPermission")

	// Cast ctx to *driver.AndroidContext
	androidCtx, ok := ctx.(*driver.AndroidContext)
	if !ok {
		return fmt.Errorf("context is not *driver.AndroidContext")
	}

	logrus.Infof("📍 [SAF-JNI-CALL] AndroidContext obtained: VM=%v, Env=%v, Ctx=%v",
		androidCtx.VM, androidCtx.Env, androidCtx.Ctx)

	// Convert the string to a C string
	cUriString := C.CString(uriString)
	defer C.free(unsafe.Pointer(cUriString))

	// Call the C function
	result := C.jni_takePersistableUriPermission(
		C.uintptr_t(androidCtx.Env),
		C.uintptr_t(androidCtx.Ctx),
		cUriString,
	)

	if result != 0 {
		return fmt.Errorf("JNI call returned error code: %d", result)
	}

	logrus.Infof("✅ [SAF-JNI-CALL] C function executed successfully")
	return nil
}

// jniOpenFileDescriptor - JNI call via CGO
func (sh *SAFHelper) jniOpenFileDescriptor(ctx any, uriString string, mode string) (int, error) {
	logrus.Infof("📍 [SAF-JNI-CALL] Calling C function jni_openFileDescriptor")

	// Cast ctx to *driver.AndroidContext
	androidCtx, ok := ctx.(*driver.AndroidContext)
	if !ok {
		return -1, fmt.Errorf("context is not *driver.AndroidContext")
	}

	logrus.Infof("📍 [SAF-JNI-CALL] AndroidContext obtained: VM=%v, Env=%v, Ctx=%v",
		androidCtx.VM, androidCtx.Env, androidCtx.Ctx)

	// Convert the strings to C strings
	cUriString := C.CString(uriString)
	defer C.free(unsafe.Pointer(cUriString))

	cMode := C.CString(mode)
	defer C.free(unsafe.Pointer(cMode))

	// Call the C function
	fd := C.jni_openFileDescriptor(
		C.uintptr_t(androidCtx.Env),
		C.uintptr_t(androidCtx.Ctx),
		cUriString,
		cMode,
	)

	if fd < 0 {
		return -1, fmt.Errorf("JNI call returned invalid FD: %d", fd)
	}

	logrus.Infof("✅ [SAF-JNI-CALL] C function executed successfully, FD=%d", fd)
	return int(fd), nil
}

// SetContext saves the context for JNI (called once at startup)
func (sh *SAFHelper) SetContext() {
	sh.mu.Lock()
	defer sh.mu.Unlock()

	driver.RunNative(func(ctx any) error {
		androidCtx, ok := ctx.(*driver.AndroidContext)
		if !ok {
			return nil
		}
		C.jni_setContext(C.uintptr_t(androidCtx.VM), C.uintptr_t(androidCtx.Env), C.uintptr_t(androidCtx.Ctx))
		return nil
	})
}

// PollSAFResult checks whether Java has a SAF result available and returns it
func (sh *SAFHelper) PollSAFResult() (uri string, fileName string, fd int, size int64, hasResult bool) {
	sh.mu.Lock()
	defer sh.mu.Unlock()

	if C.jni_hasSAFResult() != 0 {
		logrus.Info("🔔 [SAF-JNI-POLL] Java reports new SAF result!")
		cUri := C.jni_getSAFUri()
		if cUri != nil {
			uri = C.GoString(cUri)
			C.free(unsafe.Pointer(cUri))

			cName := C.jni_getSAFFileName()
			if cName != nil {
				fileName = C.GoString(cName)
				C.free(unsafe.Pointer(cName))
			}

			fd = int(C.jni_getSAFFd())
			size = int64(C.jni_getSAFSize())
			C.jni_clearSAFResult()
			hasResult = true
			logrus.Infof("✅ [SAF-JNI-POLL-GOT] uri=%s, fileName=%s, fd=%d, size=%d", uri, fileName, fd, size)
		} else {
			logrus.Warn("⚠️ [SAF-JNI-POLL-EMPTY] Java said hasResult, but URI is null")
		}
	}

	return
}

// TriggerSAFPicker launches the SAF file picker on Android
func (sh *SAFHelper) TriggerSAFPicker() error {
	sh.mu.Lock()
	defer sh.mu.Unlock()

	logrus.Info("🚀 [SAF-PLATFORM] TriggerSAFPicker ENTER")

	if sh.app == nil {
		return fmt.Errorf("SAFHelper: app is not initialized")
	}

	res := C.jni_startSAFPicker()
	if res != 0 {
		return fmt.Errorf("jni_startSAFPicker failed with code: %d", res)
	}

	logrus.Infof("✅ [SAF-PLATFORM] TriggerSAFPicker EXIT")
	return nil
}
