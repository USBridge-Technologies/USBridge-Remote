//go:build windows

package sunshine

import (
	"log"
	"os/exec"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// configureProcess hides the console window Windows would otherwise pop up
// for Sunshine (a console-subsystem exe) when spawned from the agent, which
// has no console of its own.
func configureProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}

var (
	jobOnce   sync.Once
	jobHandle windows.Handle
)

// killOnCloseJob lazily creates a single Windows Job Object configured with
// JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE. The handle is intentionally never
// closed by us: Windows closes every handle owned by a process when that
// process terminates for any reason (normal exit, crash, or being killed),
// and closing the last handle to a KILL_ON_JOB_CLOSE job terminates every
// process still assigned to it. That makes this an OS-enforced guarantee
// instead of relying on our own Go code getting a chance to run at exit
// time — which previously let Sunshine survive the agent on Windows.
func killOnCloseJob() windows.Handle {
	jobOnce.Do(func() {
		h, err := windows.CreateJobObject(nil, nil)
		if err != nil {
			log.Printf("[sunshine] CreateJobObject failed, Sunshine may survive an agent crash: %v", err)
			return
		}
		info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
			BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
				LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
			},
		}
		if _, err := windows.SetInformationJobObject(
			h,
			windows.JobObjectExtendedLimitInformation,
			uintptr(unsafe.Pointer(&info)),
			uint32(unsafe.Sizeof(info)),
		); err != nil {
			log.Printf("[sunshine] SetInformationJobObject failed, Sunshine may survive an agent crash: %v", err)
			windows.CloseHandle(h)
			return
		}
		jobHandle = h
	})
	return jobHandle
}

// afterStart assigns the freshly-started Sunshine process to the
// kill-on-job-close Job Object, so Windows itself terminates Sunshine the
// instant the agent process ends for any reason — crash, task-kill, normal
// exit — instead of leaving it orphaned.
func afterStart(p *Process, cmd *exec.Cmd) {
	job := killOnCloseJob()
	if job == 0 {
		return
	}
	procHandle, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if err != nil {
		log.Printf("[sunshine] OpenProcess failed, Sunshine may survive an agent crash: %v", err)
		return
	}
	defer windows.CloseHandle(procHandle)
	if err := windows.AssignProcessToJobObject(job, procHandle); err != nil {
		log.Printf("[sunshine] AssignProcessToJobObject failed, Sunshine may survive an agent crash: %v", err)
	}
}
