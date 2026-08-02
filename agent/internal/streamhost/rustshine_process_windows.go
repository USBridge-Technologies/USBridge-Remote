//go:build rustshine && windows

package streamhost

import (
	"log"
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// configureRustshineProcess hides the console window a spawned
// console-subsystem gamestream-server.exe would otherwise pop up.
func configureRustshineProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}

// rustshineAfterStart assigns the freshly-started process to the same
// kill-on-job-close Job Object sunshineBackend uses (killOnCloseJob,
// sunshine_process_windows.go — shared across backends since it's a
// process-tree-wide singleton, not backend-specific).
func rustshineAfterStart(b *rustshineBackend, cmd *exec.Cmd) {
	job := killOnCloseJob()
	if job == 0 {
		return
	}
	procHandle, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if err != nil {
		log.Printf("[rustshine] OpenProcess failed, gamestream-server may survive an agent crash: %v", err)
		return
	}
	defer windows.CloseHandle(procHandle)
	if err := windows.AssignProcessToJobObject(job, procHandle); err != nil {
		log.Printf("[rustshine] AssignProcessToJobObject failed, gamestream-server may survive an agent crash: %v", err)
	}
}
