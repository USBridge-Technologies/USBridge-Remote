//go:build windows

package streamhost

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"

	"usbridge_agent/internal/sessionlaunch"
)

// init wires rustshine_backend.go's platform hooks: on Windows, Start()
// launches gamestream-server via sessionBrokerLaunchImpl (re-homing it into
// the active console session) instead of a plain child process whenever
// this process is itself running as the LocalSystem USBridgeAgent service
// -- see sessionlaunch's package doc for why a LocalSystem service needs
// this at all. svc.IsWindowsService() reflects how this process was
// actually started and never changes for the life of the process, so it's
// safe to cache once instead of re-checking on every Start() call.
func init() {
	isSvc, err := svc.IsWindowsService()
	if err != nil {
		log.Printf("[rustshine] could not determine if running as a Windows service (assuming not): %v", err)
		isSvc = false
	}
	useSessionBroker = func() bool { return isSvc }
	sessionBrokerLaunch = sessionBrokerLaunchImpl
}

// gamestreamServerCompatEnv is the __COMPAT_LAYER override sessionBrokerLaunchImpl
// and ListCaptureDevices' session-broker branch (rustshine_devices_windows.go)
// both pass to every gamestream-server.exe launch through
// internal/sessionlaunch -- see sessionBrokerLaunchImpl's doc comment for
// why. Shared here so the two call sites can't drift out of sync the way
// ListCaptureDevices originally did (it called sessionlaunch.RunAndCaptureOutput
// directly with no extraEnv at all, silently getting the same
// virtualized/wrong resolution list Start() itself used to before this env
// override existed -- confirmed live).
var gamestreamServerCompatEnv = map[string]string{"__COMPAT_LAYER": "HIGHDPIAWARE"}

// sessionBrokerLaunchImpl launches exe inside the active console session
// via internal/sessionlaunch. Always passes __COMPAT_LAYER=HIGHDPIAWARE:
// gamestream-server carries no DPI-awareness manifest of its own, so
// without this Windows reports a virtualized/scaled-down resolution to it
// instead of the real one (e.g. 1280x800 instead of a real 2560x1600 on a
// 200%-scaled display -- confirmed live). Ordinarily that's silently
// papered over by a per-user "HIGHDPIAWARE" compatibility-mode override in
// HKCU\...\AppCompatFlags\Layers for this exact binary path -- but that
// registry-based override is applied by the shell/AppCompat engine at
// CreateProcess time for normal launches, and does NOT get honored for a
// process started via the raw CreateProcessAsUser this package uses (also
// confirmed live: identical user token, same binary path, registry override
// present, still got the virtualized resolution) -- so it must be forced
// explicitly here instead of relying on that fragile, undocumented,
// per-user, possibly-not-even-present-on-a-fresh-profile registry state.
func sessionBrokerLaunchImpl(exe string, args []string, workDir string, stdout, stderr *os.File) (rustshineProcess, error) {
	h, err := sessionlaunch.LaunchInActiveSession(exe, args, workDir, stdout, stderr, gamestreamServerCompatEnv)
	if err != nil {
		if err == sessionlaunch.ErrNoActiveSession {
			return nil, fmt.Errorf("%w: %v", errNoActiveSessionMarker, err)
		}
		return nil, err
	}
	assignToKillOnCloseJob(h.Pid())
	return sessionProcAdapter{h}, nil
}

// sessionProcAdapter adapts *sessionlaunch.Handle to rustshineProcess.
type sessionProcAdapter struct{ h *sessionlaunch.Handle }

func (a sessionProcAdapter) Pid() int    { return a.h.Pid() }
func (a sessionProcAdapter) Kill() error { return a.h.Kill() }
func (a sessionProcAdapter) Wait() error { _, err := a.h.Wait(); return err }

// assignToKillOnCloseJob puts pid into the same kill-on-job-close Job
// Object rustshineAfterStart assigns exec.Cmd-launched processes to
// (killOnCloseJob, sunshine_process_windows.go) -- same reasoning, PID-based
// since CreateProcessAsUser (unlike exec.Cmd) never gives Go's os/exec any
// notion of "child" to hook into.
func assignToKillOnCloseJob(pid int) {
	job := killOnCloseJob()
	if job == 0 {
		return
	}
	procHandle, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		log.Printf("[rustshine] OpenProcess failed, gamestream-server may survive an agent crash: %v", err)
		return
	}
	defer windows.CloseHandle(procHandle)
	if err := windows.AssignProcessToJobObject(job, procHandle); err != nil {
		log.Printf("[rustshine] AssignProcessToJobObject failed, gamestream-server may survive an agent crash: %v", err)
	}
}

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
	assignToKillOnCloseJob(cmd.Process.Pid)
}
