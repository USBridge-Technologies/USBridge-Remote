//go:build darwin

package controller

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"fyne.io/fyne/v2/storage"
	"github.com/sirupsen/logrus"
)

func (dw *DiskWidget) showPlatformNativeImagePicker() (selectedImage, bool) {
	// macOS osascript choose file
	types := make([]string, 0, len(dw.supportedTypes))
	for _, t := range dw.supportedTypes {
		// remove dot and quote
		types = append(types, fmt.Sprintf("%q", strings.TrimPrefix(t, ".")))
	}
	typeList := "{" + strings.Join(types, ", ") + "}"

	// Script to get POSIX path directly
	script := fmt.Sprintf(`POSIX path of (choose file of type %s with prompt %q)`, typeList, dw.pickerTitle())
	cmd := exec.Command("osascript", "-e", script)
	out, err := cmd.Output()
	if err != nil {
		// User cancelled usually returns non-zero exit code
		logrus.Debugf("macos native image picker info (possibly cancelled): %v", err)
		return selectedImage{}, false
	}

	path := strings.TrimSpace(string(out))
	if path == "" {
		return selectedImage{}, false
	}

	return selectedImage{
		FileName: filepath.Base(path),
		URI:      storage.NewFileURI(path).String(),
	}, true
}
