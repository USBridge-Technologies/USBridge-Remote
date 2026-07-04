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
	fdCache   map[string]int      // кэш URI -> FD
	fileCache map[string]*os.File // кэш URI -> *os.File
}

var (
	safHelper     *SAFHelper
	safHelperOnce sync.Once
)

// GetSAFHelper возвращает singleton instance SAFHelper
func GetSAFHelper(app fyne.App) *SAFHelper {
	safHelperOnce.Do(func() {
		logrus.Info("🔧 [SAF] Инициализация SAFHelper singleton")
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

	logrus.Infof("📍 [SAF-STEP-1] TakePersistableUriPermission вызван для URI: %s", uriString)
	logrus.Infof("📍 [SAF-STEP-1] Runtime GOOS: %s, GOARCH: %s", runtime.GOOS, runtime.GOARCH)

	if sh.app == nil {
		err := fmt.Errorf("SAFHelper: app не инициализирован")
		logrus.Errorf("❌ [SAF-STEP-1-ERROR] %v", err)
		return err
	}

	logrus.Infof("✅ [SAF-STEP-1] App объект доступен: %T", sh.app)

	// Канал для получения результата из нативного кода
	resultChan := make(chan error, 1)

	logrus.Infof("📍 [SAF-STEP-2] Подготовка к вызову driver.RunNative для takePersistableUriPermission")

	// Вызываем Android JNI через driver.RunNative
	driver.RunNative(func(ctx any) error {
		logrus.Infof("📍 [SAF-STEP-3] Внутри RunNative callback, контекст: %T", ctx)

		// ctx должен быть android.app.Activity или Context
		defer func() {
			if r := recover(); r != nil {
				err := fmt.Errorf("panic в RunNative: %v", r)
				logrus.Errorf("❌ [SAF-STEP-3-PANIC] %v", err)
				resultChan <- err
			}
		}()

		// Попытка получить Activity через рефлексию/JNI
		logrus.Infof("📍 [SAF-STEP-4] Попытка вызова JNI для takePersistableUriPermission")

		// ЗДЕСЬ НУЖНА РЕАЛЬНАЯ JNI РЕАЛИЗАЦИЯ
		// Для демонстрации используем заглушку с подробным логированием

		if ctx == nil {
			err := fmt.Errorf("RunNative context is nil")
			logrus.Errorf("❌ [SAF-STEP-4-ERROR] %v", err)
			resultChan <- err
			return err
		}

		logrus.Infof("📍 [SAF-STEP-5] Контекст получен, попытка вызова JNI метода")

		// TODO: Actual JNI implementation needed
		// 1. Get ContentResolver via context.getContentResolver()
		// 2. Parse URI via Uri.parse(uriString)
		// 3. Call contentResolver.takePersistableUriPermission(uri,
		//    Intent.FLAG_GRANT_READ_URI_PERMISSION | Intent.FLAG_GRANT_WRITE_URI_PERMISSION)

		err := sh.jniTakePersistableUriPermission(ctx, uriString)
		if err != nil {
			logrus.Errorf("❌ [SAF-STEP-5-ERROR] JNI вызов завершился с ошибкой: %v", err)
			resultChan <- err
		} else {
			logrus.Infof("✅ [SAF-STEP-5-SUCCESS] JNI вызов успешен")
			resultChan <- nil
		}
		return err
	})

	logrus.Infof("📍 [SAF-STEP-6] Ожидание результата из RunNative...")
	err := <-resultChan

	if err != nil {
		logrus.Errorf("❌ [SAF-STEP-6-ERROR] TakePersistableUriPermission завершился с ошибкой: %v", err)
		return fmt.Errorf("не удалось сохранить разрешение для URI %s: %v", uriString, err)
	}

	logrus.Infof("✅ [SAF-STEP-6-SUCCESS] Разрешение для URI %s успешно сохранено", uriString)
	return nil
}

// OpenFileDescriptor открывает файловый дескриптор через SAF
func (sh *SAFHelper) OpenFileDescriptor(uriString string, mode string) (*os.File, error) {
	sh.mu.Lock()
	defer sh.mu.Unlock()

	logrus.Infof("📍 [SAF-OPENFD-1] OpenFileDescriptor вызван для URI: %s, mode: %s", uriString, mode)

	// Проверяем кэш
	if cachedFile, exists := sh.fileCache[uriString]; exists {
		// Проверяем что файл все еще валиден
		if _, err := cachedFile.Stat(); err == nil {
			logrus.Infof("✅ [SAF-OPENFD-CACHE] Возвращаем закэшированный *os.File для URI: %s", uriString)
			return cachedFile, nil
		} else {
			// Файл закрыт или невалиден, удаляем из кэша
			logrus.Warnf("⚠️ [SAF-OPENFD-CACHE-INVALID] Закэшированный файл невалиден (%v), удаляем из кэша и переоткрываем", err)
			delete(sh.fileCache, uriString)
			delete(sh.fdCache, uriString)
		}
	}

	if sh.app == nil {
		err := fmt.Errorf("SAFHelper: app не инициализирован")
		logrus.Errorf("❌ [SAF-OPENFD-1-ERROR] %v", err)
		return nil, err
	}

	logrus.Infof("📍 [SAF-OPENFD-2] Подготовка к вызову driver.RunNative для openFileDescriptor")

	// Канал для получения FD из нативного кода
	fdChan := make(chan int, 1)
	errChan := make(chan error, 1)

	// Вызываем Android JNI через driver.RunNative
	driver.RunNative(func(ctx any) error {
		logrus.Infof("📍 [SAF-OPENFD-3] Внутри RunNative callback для openFileDescriptor")

		defer func() {
			if r := recover(); r != nil {
				err := fmt.Errorf("panic в RunNative: %v", r)
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

		logrus.Infof("📍 [SAF-OPENFD-4] Попытка вызова JNI для openFileDescriptor")

		// TODO: Actual JNI implementation needed:
		// 1. Получить ContentResolver через context.getContentResolver()
		// 2. Парсим URI через Uri.parse(uriString)
		// 3. ParcelFileDescriptor pfd = contentResolver.openFileDescriptor(uri, mode)
		// 4. int fd = pfd.detachFd()
		// 5. Возвращаем fd

		fd, err := sh.jniOpenFileDescriptor(ctx, uriString, mode)
		if err != nil {
			logrus.Errorf("❌ [SAF-OPENFD-4-ERROR] JNI вызов завершился с ошибкой: %v", err)
			errChan <- err
			return err
		}

		logrus.Infof("✅ [SAF-OPENFD-4-SUCCESS] Получен FD: %d", fd)
		fdChan <- fd
		return nil
	})

	logrus.Infof("📍 [SAF-OPENFD-5] Ожидание результата из RunNative...")

	// Ждем результат
	select {
	case fd := <-fdChan:
		logrus.Infof("✅ [SAF-OPENFD-5] Получен файловый дескриптор: %d", fd)

		// Кэшируем FD
		sh.fdCache[uriString] = fd

		// Преобразуем FD в *os.File
		logrus.Infof("📍 [SAF-OPENFD-6] Преобразование FD %d в *os.File", fd)
		file := os.NewFile(uintptr(fd), fmt.Sprintf("saf:%s", uriString))
		if file == nil {
			err := fmt.Errorf("не удалось создать *os.File из FD %d", fd)
			logrus.Errorf("❌ [SAF-OPENFD-6-ERROR] %v", err)
			return nil, err
		}

		// Кэшируем File
		sh.fileCache[uriString] = file

		logrus.Infof("✅ [SAF-OPENFD-6-SUCCESS] *os.File создан успешно для URI: %s", uriString)
		return file, nil

	case err := <-errChan:
		logrus.Errorf("❌ [SAF-OPENFD-5-ERROR] Ошибка при открытии FD: %v", err)
		return nil, fmt.Errorf("не удалось открыть файловый дескриптор для URI %s: %v", uriString, err)
	}
}

// CloseFD закрывает файловый дескриптор и удаляет из кэша
func (sh *SAFHelper) CloseFD(uriString string) error {
	sh.mu.Lock()
	defer sh.mu.Unlock()

	logrus.Infof("📍 [SAF-CLOSE-1] CloseFD вызван для URI: %s", uriString)

	// Закрываем File если есть
	if file, exists := sh.fileCache[uriString]; exists {
		logrus.Infof("📍 [SAF-CLOSE-2] Закрытие *os.File для URI: %s", uriString)
		if err := file.Close(); err != nil {
			logrus.Warnf("⚠️ [SAF-CLOSE-2-WARN] Ошибка закрытия файла (файл уже закрыт?): %v", err)
			// Не возвращаем ошибку - продолжаем очистку кэша
		}
		// Удаляем из кэша в любом случае (даже если файл уже был закрыт)
		delete(sh.fileCache, uriString)
		logrus.Infof("✅ [SAF-CLOSE-2-SUCCESS] Файл удален из кэша")
	}

	// Удаляем FD из кэша
	if _, exists := sh.fdCache[uriString]; exists {
		delete(sh.fdCache, uriString)
		logrus.Infof("✅ [SAF-CLOSE-3] FD удален из кэша")
	}

	return nil
}

// GetCachedFile возвращает закэшированный *os.File для URI (если есть)
func (sh *SAFHelper) GetCachedFile(uriString string) (*os.File, bool) {
	sh.mu.Lock()
	defer sh.mu.Unlock()

	file, exists := sh.fileCache[uriString]
	if exists {
		logrus.Infof("📍 [SAF-CACHE-HIT] Найден закэшированный файл для URI: %s", uriString)
	} else {
		logrus.Infof("📍 [SAF-CACHE-MISS] Нет закэшированного файла для URI: %s", uriString)
	}
	return file, exists
}

// jniTakePersistableUriPermission - вызов JNI через CGO
func (sh *SAFHelper) jniTakePersistableUriPermission(ctx any, uriString string) error {
	logrus.Infof("📍 [SAF-JNI-CALL] Вызов C функции jni_takePersistableUriPermission")

	// Приводим ctx к *driver.AndroidContext
	androidCtx, ok := ctx.(*driver.AndroidContext)
	if !ok {
		return fmt.Errorf("контекст не является *driver.AndroidContext")
	}

	logrus.Infof("📍 [SAF-JNI-CALL] AndroidContext получен: VM=%v, Env=%v, Ctx=%v",
		androidCtx.VM, androidCtx.Env, androidCtx.Ctx)

	// Конвертируем строку в C string
	cUriString := C.CString(uriString)
	defer C.free(unsafe.Pointer(cUriString))

	// Вызываем C функцию
	result := C.jni_takePersistableUriPermission(
		C.uintptr_t(androidCtx.Env),
		C.uintptr_t(androidCtx.Ctx),
		cUriString,
	)

	if result != 0 {
		return fmt.Errorf("JNI вызов вернул код ошибки: %d", result)
	}

	logrus.Infof("✅ [SAF-JNI-CALL] C функция выполнена успешно")
	return nil
}

// jniOpenFileDescriptor - вызов JNI через CGO
func (sh *SAFHelper) jniOpenFileDescriptor(ctx any, uriString string, mode string) (int, error) {
	logrus.Infof("📍 [SAF-JNI-CALL] Вызов C функции jni_openFileDescriptor")

	// Приводим ctx к *driver.AndroidContext
	androidCtx, ok := ctx.(*driver.AndroidContext)
	if !ok {
		return -1, fmt.Errorf("контекст не является *driver.AndroidContext")
	}

	logrus.Infof("📍 [SAF-JNI-CALL] AndroidContext получен: VM=%v, Env=%v, Ctx=%v",
		androidCtx.VM, androidCtx.Env, androidCtx.Ctx)

	// Конвертируем строки в C strings
	cUriString := C.CString(uriString)
	defer C.free(unsafe.Pointer(cUriString))

	cMode := C.CString(mode)
	defer C.free(unsafe.Pointer(cMode))

	// Вызываем C функцию
	fd := C.jni_openFileDescriptor(
		C.uintptr_t(androidCtx.Env),
		C.uintptr_t(androidCtx.Ctx),
		cUriString,
		cMode,
	)

	if fd < 0 {
		return -1, fmt.Errorf("JNI вызов вернул неверный FD: %d", fd)
	}

	logrus.Infof("✅ [SAF-JNI-CALL] C функция выполнена успешно, FD=%d", fd)
	return int(fd), nil
}

// SetContext сохраняет контекст для JNI (вызывается один раз при старте)
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

// PollSAFResult проверяет наличие результата SAF в Java и возвращает его
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

// TriggerSAFPicker запускает выбор файла через SAF на Android
func (sh *SAFHelper) TriggerSAFPicker() error {
	sh.mu.Lock()
	defer sh.mu.Unlock()

	logrus.Info("🚀 [SAF-PLATFORM] TriggerSAFPicker ENTER")

	if sh.app == nil {
		return fmt.Errorf("SAFHelper: app не инициализирован")
	}

	res := C.jni_startSAFPicker()
	if res != 0 {
		return fmt.Errorf("jni_startSAFPicker failed with code: %d", res)
	}

	logrus.Infof("✅ [SAF-PLATFORM] TriggerSAFPicker EXIT")
	return nil
}
