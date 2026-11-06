//go:build windows

package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/sirupsen/logrus"
)

// relaunchAfterEngineUpdate: this GUI runs from the same exe the engine's
// update helper is about to overwrite, and Windows keeps a running exe
// locked, so the GUI has to go before the copy (update_helper.ps1 waits for
// it). A detached PowerShell loop starts it again once the exe has been
// replaced and the service is back.
func relaunchAfterEngineUpdate(_ remoteAgentUpdater) {
	exe, err := os.Executable()
	if err != nil {
		logrus.WithError(err).Warn("relaunch after update: no executable path")
		os.Exit(0)
	}
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	q := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
	args := make([]string, 0, len(os.Args))
	for _, a := range os.Args[1:] {
		args = append(args, q(a))
	}
	argList := ""
	if len(args) > 0 {
		argList = " -ArgumentList " + strings.Join(args, ",")
	}
	script := fmt.Sprintf(`$exe = %s
$t0 = (Get-Item -LiteralPath $exe).LastWriteTimeUtc
$deadline = (Get-Date).AddMinutes(3)
while ((Get-Date) -lt $deadline) {
  Start-Sleep -Seconds 2
  $t = (Get-Item -LiteralPath $exe -ErrorAction SilentlyContinue).LastWriteTimeUtc
  if ($t -and $t -ne $t0) {
    $s = Get-Service -Name 'USBridgeAgent' -ErrorAction SilentlyContinue
    if (-not $s -or $s.Status -eq 'Running') { break }
  }
}
Start-Sleep -Seconds 3
Start-Process -FilePath $exe%s
`, q(exe), argList)
	cmd := exec.Command("powershell.exe", "-NoProfile", "-ExecutionPolicy", "Bypass", "-WindowStyle", "Hidden", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000} // CREATE_NO_WINDOW
	if err := cmd.Start(); err != nil {
		logrus.WithError(err).Warn("relaunch after update: could not start the relauncher")
	}
	logrus.Info("engine is updating -- closing this window, it reopens on the new version")
	os.Exit(0)
}
