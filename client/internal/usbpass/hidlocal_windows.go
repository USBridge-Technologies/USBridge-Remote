//go:build windows

package usbpass

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"github.com/sirupsen/logrus"
	"golang.org/x/sys/windows"
)

// While a tablet is exported to a remote host it must not also drive the local
// pointer. Windows moves the cursor from the tablet's mouse/pen/digitizer HID
// nodes, so those nodes are disabled for the duration of the export (the vendor
// node the bridge reads its data from stays on) and brought back afterwards.
//
// Two Windows quirks shape this (checked on a CTL-4100 under Wacom's driver):
//   - pnputil /disable-device takes effect at once but exits with 3010 ("restart
//     needed"), because the Wacom services still hold the nodes; that is success.
//   - Such a node cannot be enabled again ("pending system reboot"). Clearing the
//     ConfigFlags "disabled" bit and restarting the parent USB device does it
//     without a reboot.
//
// Both need administrator rights: unless the client already has them, the user
// is asked through the normal UAC prompt (once when the export starts, once when it
// ends). If they decline, the export goes on and the tablet simply also works
// locally.
//
// The disabled nodes are recorded in a state file so that a crash cannot leave
// the tablet dead: the next run restores them (RestoreLocalInput).

var (
	localInputMu    sync.Mutex
	shell32         = windows.NewLazySystemDLL("shell32.dll")
	procShellExecEx = shell32.NewProc("ShellExecuteExW")
)

func localInputStateFile() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "USBridge", "disabled_hid_nodes.txt")
}

func processElevated() bool {
	return windows.GetCurrentProcessToken().IsElevated()
}

type shellExecuteInfo struct {
	cbSize       uint32
	fMask        uint32
	hwnd         windows.Handle
	lpVerb       *uint16
	lpFile       *uint16
	lpParameters *uint16
	lpDirectory  *uint16
	nShow        int32
	hInstApp     windows.Handle
	lpIDList     uintptr
	lpClass      *uint16
	hkeyClass    windows.Handle
	dwHotKey     uint32
	hIcon        windows.Handle
	hProcess     windows.Handle
}

// runBatch runs a batch file as administrator (one UAC prompt unless the client is
// elevated already) and returns its exit code.
func runBatch(path string) error {
	if processElevated() {
		cmd := exec.Command("cmd.exe", "/c", path)
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
		}
		return nil
	}
	verb, _ := windows.UTF16PtrFromString("runas")
	file, _ := windows.UTF16PtrFromString("cmd.exe")
	args, _ := windows.UTF16PtrFromString(`/c "` + path + `"`)
	const seeMaskNoCloseProcess = 0x40
	info := shellExecuteInfo{fMask: seeMaskNoCloseProcess, lpVerb: verb, lpFile: file, lpParameters: args, nShow: 0}
	info.cbSize = uint32(unsafe.Sizeof(info))
	if r, _, e := procShellExecEx.Call(uintptr(unsafe.Pointer(&info))); r == 0 {
		return e // ERROR_CANCELLED when the user declines the prompt
	}
	defer windows.CloseHandle(info.hProcess)
	if ev, err := windows.WaitForSingleObject(info.hProcess, 90_000); err != nil || ev != windows.WAIT_OBJECT_0 {
		return fmt.Errorf("elevated command did not finish")
	}
	var code uint32
	if err := windows.GetExitCodeProcess(info.hProcess, &code); err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("exit code %d", code)
	}
	return nil
}

func runScript(lines []string) error {
	dir := filepath.Dir(localInputStateFile())
	os.MkdirAll(dir, 0o755)
	f, err := os.CreateTemp(dir, "hid-*.cmd")
	if err != nil {
		return err
	}
	f.WriteString("@echo off\r\n" + strings.Join(lines, "\r\n") + "\r\n")
	f.Close()
	defer os.Remove(f.Name())
	return runBatch(f.Name())
}

// disableScript disables each node; 3010 (restart needed) counts as success.
func disableScript(ids []string) []string {
	lines := []string{"set BAD=0"}
	for _, id := range ids {
		lines = append(lines,
			fmt.Sprintf(`pnputil /disable-device "%s" >nul 2>&1`, id),
			"if errorlevel 1 if not errorlevel 3010 set BAD=1", // 1..3009
			"if errorlevel 3011 set BAD=1",
		)
	}
	return append(lines, "exit /b %BAD%")
}

// restoreScript clears the "disabled" flag of each node and restarts the parent
// USB device, which brings every node back without a reboot.
func restoreScript(parent string, ids []string) []string {
	var lines []string
	for _, id := range ids {
		lines = append(lines, fmt.Sprintf(`reg add "HKLM\SYSTEM\CurrentControlSet\Enum\%s" /v ConfigFlags /t REG_DWORD /d 0 /f >nul 2>&1`, id))
	}
	return append(lines, fmt.Sprintf(`pnputil /restart-device "%s" >nul 2>&1`, parent), "exit /b %ERRORLEVEL%")
}

// RestoreLocalInput brings back HID nodes an earlier run disabled and did not get
// to restore (crash, declined prompt at the end). Cheap when there is nothing to do.
func RestoreLocalInput() {
	localInputMu.Lock()
	defer localInputMu.Unlock()
	restoreLocalInputLocked()
}

func readLocalInputState() (parent string, ids []string) {
	raw, err := os.ReadFile(localInputStateFile())
	if err != nil {
		return "", nil
	}
	for i, l := range strings.Split(string(raw), "\n") {
		if l = strings.TrimSpace(l); l == "" {
			continue
		} else if i == 0 {
			parent = l
		} else {
			ids = append(ids, l)
		}
	}
	return parent, ids
}

func restoreLocalInputLocked() {
	parent, ids := readLocalInputState()
	if parent == "" || len(ids) == 0 {
		os.Remove(localInputStateFile())
		return
	}
	if err := runScript(restoreScript(parent, ids)); err != nil {
		logrus.Warnf("usbpass: could not bring the tablet's local input back (%v); replug the tablet or run the export again", err)
		return
	}
	os.Remove(localInputStateFile())
	logrus.Infof("usbpass: local input of the tablet is back (%d node(s))", len(ids))
}

// suppressLocalInput disables the nodes of the tablet (parent is its USB instance
// id) and returns the function that brings them back. It never blocks the caller
// (the prompt may wait for the user) and never fails the export: a declined prompt
// just leaves local input on.
func suppressLocalInput(parent string, ids []string) (restore func()) {
	if len(ids) == 0 {
		return func() {}
	}
	done := make(chan struct{})
	var disabled bool
	go func() {
		defer close(done)
		localInputMu.Lock()
		defer localInputMu.Unlock()
		restoreLocalInputLocked() // whatever a crashed run left behind
		path := localInputStateFile()
		os.MkdirAll(filepath.Dir(path), 0o755)
		// Record first: if the process dies mid-way the nodes are still restored later.
		os.WriteFile(path, []byte(parent+"\n"+strings.Join(ids, "\n")+"\n"), 0o644)
		if err := runScript(disableScript(ids)); err != nil {
			logrus.Warnf("usbpass: the tablet stays active locally too (disabling its local input nodes failed: %v)", err)
			if !errors.Is(err, windows.ERROR_CANCELLED) {
				// Some nodes may have been switched off before the failure.
				runScript(restoreScript(parent, ids))
			}
			os.Remove(path)
			return
		}
		disabled = true
		logrus.Infof("usbpass: local input of the tablet is off while it is exported (%d node(s))", len(ids))
	}()
	return func() {
		go func() {
			<-done
			if disabled {
				RestoreLocalInput()
			}
		}()
	}
}
