//go:build windows

package ui

import (
	"fmt"
	"os/exec"
	"strings"
	"syscall"
)

// pickNativePath opens the system's own file (or folder) dialog, the same one the
// client uses for disk images, and returns the chosen path ("" when cancelled);
// shown is false when the dialog could not be opened (the caller then falls back to
// Fyne's).
func pickNativePath(folder bool, title string) (path string, shown bool) {
	// A folder is picked in the modern file dialog with its file checks off: it
	// returns the opened directory (Windows Forms' own folder dialog is the old tree).
	body := `$dialog = New-Object System.Windows.Forms.OpenFileDialog
$dialog.CheckFileExists = $true
$dialog.Multiselect = $false
$dialog.Title = %q
if ($dialog.ShowDialog() -eq [System.Windows.Forms.DialogResult]::OK) { Write-Output $dialog.FileName }`
	if folder {
		body = `$dialog = New-Object System.Windows.Forms.OpenFileDialog
$dialog.CheckFileExists = $false
$dialog.CheckPathExists = $true
$dialog.ValidateNames = $false
$dialog.FileName = "Select folder"
$dialog.Title = %q
if ($dialog.ShowDialog() -eq [System.Windows.Forms.DialogResult]::OK) { Write-Output ([System.IO.Path]::GetDirectoryName($dialog.FileName)) }`
	}
	script := "Add-Type -AssemblyName System.Windows.Forms\n[Console]::OutputEncoding = [System.Text.Encoding]::UTF8\n" + fmt.Sprintf(body, title)
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-STA", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	out, err := cmd.Output()
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(out)), true
}
