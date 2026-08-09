//go:build windows

package permissions

import (
	"fmt"
	"path/filepath"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

type Service struct{}

func New() *Service                                      { return &Service{} }
func (s *Service) AccessibilityGranted() bool            { return true }
func (s *Service) ScreenRecordingGranted() bool          { return true }
func (s *Service) RequestAccessibility() bool            { return true }
func (s *Service) RequestScreenRecording() bool          { return true }
func (s *Service) RequestMissing()                       {}
func (s *Service) OpenPrivacySettings() error            { return nil }
func (s *Service) OpenScreenRecordingSettings() error    { return nil }
func (s *Service) KMSCaptureGranted(binPath string) bool { return false }
func (s *Service) RequestKMSCapture(binPath string) bool { return false }

// DiskMountGranted/RequestDiskMount: the Windows iSCSI initiator
// (agent/internal/iscsi/initiator_windows.go) drives the built-in MSiSCSI
// service via PowerShell, which a standard user can normally already
// operate (no elevation prompt to design around here, unlike iscsiadm on
// Linux needing root) — nothing to grant up front today.
func (s *Service) DiskMountSupported() bool { return true }
func (s *Service) DiskMountGranted() bool   { return true }
func (s *Service) RequestDiskMount() bool   { return true }

// GPUClockLockSupported is always true on Windows -- whether it actually
// *works* on a given machine depends on having an NVIDIA GPU and a driver
// new enough for NVML's clock-lock calls, which
// gamestream-server's own --gpu-clock-lock-daemon mode discovers and
// reports (via its own log / this session's own remember.md) at the point
// it actually tries, not something this package can know in advance.
func (s *Service) GPUClockLockSupported() bool { return true }

// GPUClockLockElevated reports whether *this* process (the agent itself)
// is already running elevated. In practice this is almost always false --
// the agent deliberately never requires elevation just to launch normally
// (see RequestGPUClockLock's own docs for why the lock itself always goes
// through a separate, freshly-elevated helper process instead) -- exposed
// mainly so the UI can decide whether to explain *why* a click is about to
// trigger a UAC prompt.
func (s *Service) GPUClockLockElevated() bool {
	var token windows.Token
	process, err := windows.GetCurrentProcess()
	if err != nil {
		return false
	}
	if err := windows.OpenProcessToken(process, windows.TOKEN_QUERY, &token); err != nil {
		return false
	}
	defer token.Close()

	var elevation uint32
	var outLen uint32
	infoErr := windows.GetTokenInformation(token, windows.TokenElevation, (*byte)(unsafe.Pointer(&elevation)), uint32(unsafe.Sizeof(elevation)), &outLen)
	if infoErr != nil {
		return false
	}
	return elevation != 0
}

var (
	shell32           = syscall.NewLazyDLL("shell32.dll")
	procShellExecuteW = shell32.NewProc("ShellExecuteW")
)

// SW_HIDE keeps the elevated helper's own console window (it's a
// console-subsystem binary) from flashing up — it does nothing but poll a
// PID and hold an NVML lock, there's nothing useful to show.
const swHide = 0

// RequestGPUClockLock launches binPath (gamestream-server.exe) as
// `binPath --gpu-clock-lock-daemon --watch-pid <watchPID>` via
// ShellExecuteW's "runas" verb, which is what actually triggers the UAC
// consent prompt. There is no Windows equivalent of Linux's one-time
// `setcap`/CAP_SYS_ADMIN grant (see permissions.Service.RequestKMSCapture on
// Linux for that contrast) -- NVML's clock-lock calls simply require the
// *calling process itself* to be elevated, so this has to launch a fresh
// elevated helper (and thus show a fresh UAC prompt) every time it's armed.
// Callers should pass a long-lived watchPID (the agent's own, not a
// per-session stream host's) so that only happens once per agent run --
// see app.applyGPUClockLock's doc for why: a UAC prompt runs on the secure
// desktop, unreachable from a remote session, so re-arming on every
// stream-host restart would strand a remote client mid-switch. The daemon
// polls watchPID and drops the lock once it exits, see rust-shine's own
// `platform::windows::run_gpu_clock_lock_daemon` docs. Returns once the
// helper has launched, not once it exits.
func RequestGPUClockLock(binPath string, watchPID int) error {
	dir := filepath.Dir(binPath)
	params := fmt.Sprintf("--gpu-clock-lock-daemon --watch-pid %d", watchPID)

	verbPtr, err := syscall.UTF16PtrFromString("runas")
	if err != nil {
		return err
	}
	filePtr, err := syscall.UTF16PtrFromString(binPath)
	if err != nil {
		return err
	}
	paramsPtr, err := syscall.UTF16PtrFromString(params)
	if err != nil {
		return err
	}
	dirPtr, err := syscall.UTF16PtrFromString(dir)
	if err != nil {
		return err
	}

	// ShellExecuteW returns an HINSTANCE-shaped value: > 32 on success,
	// <= 32 on failure (this includes the user clicking "No" on the UAC
	// prompt, which comes back as SE_ERR_ACCESSDENIED).
	ret, _, _ := procShellExecuteW.Call(
		0,
		uintptr(unsafe.Pointer(verbPtr)),
		uintptr(unsafe.Pointer(filePtr)),
		uintptr(unsafe.Pointer(paramsPtr)),
		uintptr(unsafe.Pointer(dirPtr)),
		uintptr(swHide),
	)
	if ret <= 32 {
		return fmt.Errorf("ShellExecuteW(runas) failed with code %d (the UAC prompt may have been declined)", ret)
	}
	return nil
}

// RequestGPUClockLock is the *Service method the rest of the app calls
// (matching every other permission-request method's shape) -- thin wrapper
// around the package-level function above, which is also called directly
// by streamhost.rustshineBackend.Start (no *Service instance handy there).
func (s *Service) RequestGPUClockLock(binPath string, watchPID int) error {
	return RequestGPUClockLock(binPath, watchPID)
}
