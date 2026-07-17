//go:build !android
// +build !android

package platform

import (
	"fmt"
	"os"

	"fyne.io/fyne/v2"
	"github.com/sirupsen/logrus"
)

// SAFHelper helper for working with the Android Storage Access Framework
// On desktop - a stub
type SAFHelper struct {
	app fyne.App
}

// GetSAFHelper returns a SAFHelper stub for desktop
func GetSAFHelper(app fyne.App) *SAFHelper {
	logrus.Debug("📍 [SAF-DESKTOP] GetSAFHelper called on desktop (stub)")
	return &SAFHelper{app: app}
}

// TakePersistableUriPermission - stub for desktop
func (sh *SAFHelper) TakePersistableUriPermission(uriString string) error {
	logrus.Debug("📍 [SAF-DESKTOP] TakePersistableUriPermission called (not needed on desktop)")
	return nil // Not needed on desktop
}

// OpenFileDescriptor - on desktop we just open the file the normal way
func (sh *SAFHelper) OpenFileDescriptor(uriString string, mode string) (*os.File, error) {
	logrus.Debugf("📍 [SAF-DESKTOP] OpenFileDescriptor called for URI: %s (desktop)", uriString)

	// On desktop the URI is a plain file:// path or just a path
	filePath := uriString
	if len(filePath) > 7 && filePath[:7] == "file://" {
		filePath = filePath[7:]
	}

	logrus.Debugf("📍 [SAF-DESKTOP] Opening file: %s", filePath)

	// Determine open flags from mode
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
		return nil, fmt.Errorf("failed to open file %s: %v", filePath, err)
	}

	logrus.Infof("✅ [SAF-DESKTOP] File opened successfully: %s", filePath)
	return file, nil
}

// CloseFD - stub for desktop
func (sh *SAFHelper) CloseFD(uriString string) error {
	logrus.Debug("📍 [SAF-DESKTOP] CloseFD called (desktop)")
	return nil
}

// GetCachedFile - stub for desktop
func (sh *SAFHelper) GetCachedFile(uriString string) (*os.File, bool) {
	logrus.Debug("📍 [SAF-DESKTOP] GetCachedFile called (desktop)")
	return nil, false
}

// SetContext - stub for desktop (no JNI context needed)
func (sh *SAFHelper) SetContext() {}

// TriggerSAFPicker - stub for desktop
func (sh *SAFHelper) TriggerSAFPicker() error {
	return fmt.Errorf("SAF picker is not available on desktop")
}

// PollSAFResult - stub for desktop (always no result)
func (sh *SAFHelper) PollSAFResult() (uri string, fileName string, fd int, size int64, hasResult bool) {
	return "", "", -1, 0, false
}
