//go:build windows

package streamhost

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"

	"golang.org/x/sys/windows"

	"usbridge_agent/internal/sessionlaunch"
)

func init() {
	punktfunkSessionLaunch = punktfunkSessionLaunchImpl
}

// punktfunkSessionLaunchImpl starts punktfunk-host in the active console session, as
// Sunshine and RustShine are: the agent's service lives in Session 0, which has no
// desktop to capture (the host came up, paired and launched, then sent no video, "No
// video received from host"), and the shared MttVDD monitor it attaches would not be
// the user's either. extraEnv goes on top of that session's environment.
func punktfunkSessionLaunchImpl(exe string, args []string, workDir string, out *os.File, extraEnv map[string]string) (punktfunkProcess, error) {
	h, err := sessionlaunch.LaunchInActiveSession(exe, args, workDir, out, out, extraEnv)
	if err != nil {
		if err == sessionlaunch.ErrNoActiveSession {
			return nil, fmt.Errorf("%w: %v", errSunshineNoActiveSessionMarker, err)
		}
		return nil, err
	}
	assignToKillOnCloseJob(h.Pid())
	return sessionProcAdapter{h}, nil
}

// repairPunktfunkConfigDir gives the agent's user write access back to
// punktfunk-host's config dir when an upstream punktfunk-host has locked it.
//
// Upstream hardens its config dir for the %ProgramData% dir a SYSTEM service
// shares with the user (pf-paths restrict_dir_to_system_admins): inheritance
// off, full control for SYSTEM and Administrators only, Users read-only. The
// agent runs it unelevated with PUNKTFUNK_CONFIG_DIR in the user's own
// %APPDATA%, where that DACL locks the user -- and so the agent and the host
// itself -- out of the dir: the host died on "write native-key.pem: Access is
// denied" and every restart after it too. The Streamers-Forks build skips that
// hardening outside %ProgramData%; this undoes it on a dir an older build
// already locked. The user still owns the dir (re-owning to Administrators
// needs elevation and failed), so resetting it to the inherited %APPDATA%
// ACL needs no elevation.
func repairPunktfunkConfigDir(dir string) {
	probe := filepath.Join(dir, ".agent-write-probe")
	f, err := os.OpenFile(probe, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err == nil {
		f.Close()
		_ = os.Remove(probe)
		return
	}
	if !os.IsPermission(err) {
		return
	}
	icacls := filepath.Join(os.Getenv("SystemRoot"), "System32", "icacls.exe")
	if os.Getenv("SystemRoot") == "" {
		icacls = `C:\Windows\System32\icacls.exe`
	}
	cmd := exec.Command(icacls, dir, "/reset", "/T", "/C", "/Q")
	configureProcess(cmd)
	out, rerr := cmd.CombinedOutput()
	if rerr != nil {
		log.Printf("[punktfunk] %s is not writable and resetting its ACL failed: %v: %s", dir, rerr, out)
		return
	}
	log.Printf("[punktfunk] %s was locked to SYSTEM/Administrators (upstream service hardening) -- reset it to the inherited ACL", dir)
}

// afterStartPunktfunk assigns the freshly-started punktfunk-host process to
// the same kill-on-job-close Job Object sunshine_process_windows.go's own
// afterStart uses, so Windows itself terminates punktfunk-host the instant
// the agent process ends for any reason. killOnCloseJob is package-level and
// backend-agnostic already (sunshine_process_windows.go), so it's reused
// directly rather than duplicated.
func afterStartPunktfunk(cmd *exec.Cmd) {
	job := killOnCloseJob()
	if job == 0 {
		return
	}
	procHandle, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if err != nil {
		log.Printf("[punktfunk] OpenProcess failed, punktfunk-host may survive an agent crash: %v", err)
		return
	}
	defer windows.CloseHandle(procHandle)
	if err := windows.AssignProcessToJobObject(job, procHandle); err != nil {
		log.Printf("[punktfunk] AssignProcessToJobObject failed, punktfunk-host may survive an agent crash: %v", err)
	}
}
