//go:build windows

package controller

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"usbridge-client/internal/gui/i18n"

	"fyne.io/fyne/v2/storage"
	"github.com/sirupsen/logrus"
)

func (dw *DiskWidget) showPlatformNativeImagePicker() (selectedImage, bool) {
	filterParts := make([]string, 0, len(dw.supportedTypes))
	for _, ext := range dw.supportedTypes {
		trimmed := strings.TrimSpace(ext)
		if trimmed == "" {
			continue
		}
		if !strings.HasPrefix(trimmed, ".") {
			trimmed = "." + trimmed
		}
		filterParts = append(filterParts, "*"+trimmed)
	}

	filter := "Disk Images|" + strings.Join(filterParts, ";")
	script := fmt.Sprintf(`
Add-Type -AssemblyName System.Windows.Forms
$dialog = New-Object System.Windows.Forms.OpenFileDialog
$dialog.Title = %q
$dialog.Filter = %q
$dialog.CheckFileExists = $true
$dialog.Multiselect = $false
if ($dialog.ShowDialog() -eq [System.Windows.Forms.DialogResult]::OK) {
	[Console]::OutputEncoding = [System.Text.Encoding]::UTF8
	Write-Output $dialog.FileName
}
`, dw.pickerTitle(), filter)

	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-STA", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.Output()
	if err != nil {
		logrus.Errorf("windows native image picker failed: %v", err)
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

func (dw *DiskWidget) pickerTitle() string {
	if strings.TrimSpace(i18n.Current.SelectDiskImage) != "" {
		return i18n.Current.SelectDiskImage
	}
	return "Select disk image"
}
