//go:build linux && !android

package controller

import (
	"os/exec"
	"path/filepath"
	"strings"

	"fyne.io/fyne/v2/storage"
	"github.com/sirupsen/logrus"
)

func (dw *DiskWidget) showPlatformNativeImagePicker() (selectedImage, bool) {
	// Try zenity
	filters := make([]string, 0, len(dw.supportedTypes))
	for _, ext := range dw.supportedTypes {
		filters = append(filters, "*"+ext)
	}

	// Zenity filter format is usually "--file-filter=Title | *.iso *.img"
	zenityFilter := "Disk Images | " + strings.Join(filters, " ")

	cmd := exec.Command("zenity", "--file-selection", "--title="+dw.pickerTitle(), "--file-filter="+zenityFilter)
	out, err := cmd.Output()
	if err == nil {
		path := strings.TrimSpace(string(out))
		if path != "" {
			return selectedImage{
				FileName: filepath.Base(path),
				URI:      storage.NewFileURI(path).String(),
			}, true
		}
	}

	// Try kdialog if zenity fails or returns empty
	// kdialog filter format: "*.iso *.img | Disk Images"
	cmd = exec.Command("kdialog", "--getopenfilename", ".", strings.Join(filters, " "), "--title", dw.pickerTitle())
	out, err = cmd.Output()
	if err == nil {
		path := strings.TrimSpace(string(out))
		if path != "" {
			return selectedImage{
				FileName: filepath.Base(path),
				URI:      storage.NewFileURI(path).String(),
			}, true
		}
	}

	logrus.Errorf("linux native image picker failed (zenity and kdialog): %v", err)
	return selectedImage{}, false
}
