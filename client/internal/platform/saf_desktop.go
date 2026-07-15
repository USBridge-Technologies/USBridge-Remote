//go:build !android
// +build !android

package platform

import (
	"fmt"
	"os"

	"fyne.io/fyne/v2"
	"github.com/sirupsen/logrus"
)

// SAFHelper помощник для работы с Android Storage Access Framework
// На десктопе - заглушка
type SAFHelper struct {
	app fyne.App
}

// GetSAFHelper возвращает заглушку SAFHelper для десктопа
func GetSAFHelper(app fyne.App) *SAFHelper {
	logrus.Debug("📍 [SAF-DESKTOP] GetSAFHelper вызван на десктопе (заглушка)")
	return &SAFHelper{app: app}
}

// TakePersistableUriPermission - заглушка для десктопа
func (sh *SAFHelper) TakePersistableUriPermission(uriString string) error {
	logrus.Debug("📍 [SAF-DESKTOP] TakePersistableUriPermission вызван (не требуется на десктопе)")
	return nil // На десктопе не требуется
}

// OpenFileDescriptor - на десктопе просто открываем файл обычным способом
func (sh *SAFHelper) OpenFileDescriptor(uriString string, mode string) (*os.File, error) {
	logrus.Debugf("📍 [SAF-DESKTOP] OpenFileDescriptor вызван для URI: %s (десктоп)", uriString)

	// На десктопе URI это обычный file:// путь или просто путь
	filePath := uriString
	if len(filePath) > 7 && filePath[:7] == "file://" {
		filePath = filePath[7:]
	}

	logrus.Debugf("📍 [SAF-DESKTOP] Открытие файла: %s", filePath)

	// Определяем флаги открытия по mode
	var flags int
	switch mode {
	case "r":
		flags = os.O_RDONLY
	case "rw", "w":
		flags = os.O_RDWR
	default:
		flags = os.O_RDONLY
	}

	file, err := os.OpenFile(filePath, flags, 0644)
	if err != nil {
		return nil, fmt.Errorf("не удалось открыть файл %s: %v", filePath, err)
	}

	logrus.Infof("✅ [SAF-DESKTOP] Файл успешно открыт: %s", filePath)
	return file, nil
}

// CloseFD - заглушка для десктопа
func (sh *SAFHelper) CloseFD(uriString string) error {
	logrus.Debug("📍 [SAF-DESKTOP] CloseFD вызван (десктоп)")
	return nil
}

// GetCachedFile - заглушка для десктопа
func (sh *SAFHelper) GetCachedFile(uriString string) (*os.File, bool) {
	logrus.Debug("📍 [SAF-DESKTOP] GetCachedFile вызван (десктоп)")
	return nil, false
}

// SetContext - заглушка для десктопа (JNI контекст не нужен)
func (sh *SAFHelper) SetContext() {}

// TriggerSAFPicker - заглушка для десктопа
func (sh *SAFHelper) TriggerSAFPicker() error {
	return fmt.Errorf("SAF picker недоступен на десктопе")
}

// PollSAFResult - заглушка для десктопа (всегда нет результата)
func (sh *SAFHelper) PollSAFResult() (uri string, fileName string, fd int, size int64, hasResult bool) {
	return "", "", -1, 0, false
}
