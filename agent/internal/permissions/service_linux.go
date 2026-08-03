//go:build linux

package permissions

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"usbridge_agent/internal/capture"
)

const uinputRulePath = "/etc/udev/rules.d/99-usbridge-input.rules"
const uinputRuleContent = "KERNEL==\"uinput\", SUBSYSTEM==\"misc\", TAG+=\"uaccess\"\n"

type Service struct {
	lastAccessErr string
}

func New() *Service { return &Service{} }

// LastAccessibilityError returns a human-readable reason the last
// RequestAccessibility call failed, or "" if it succeeded (or hasn't run
// yet). Debian's default install neither adds the user to the sudo group
// nor pulls in pkexec (it was split into its own package from policykit-1
// around trixie), unlike Ubuntu where both are present out of the box --
// so the pkexec-based flow below silently does nothing there unless we
// surface why.
func (s *Service) LastAccessibilityError() string { return s.lastAccessErr }

func (s *Service) AccessibilityGranted() bool {
	f, err := os.OpenFile("/dev/uinput", os.O_WRONLY, 0)
	if err != nil {
		return false
	}
	f.Close()
	return true
}

func (s *Service) ScreenRecordingGranted() bool {
	if capture.GetLinuxEnv() == "Wayland" {
		return capture.GetPortalSession() != ""
	}
	return true
}

func (s *Service) RequestAccessibility() bool {
	s.lastAccessErr = ""
	log.Printf("[permissions] RequestAccessibility called, granted=%v", s.AccessibilityGranted())
	if s.AccessibilityGranted() {
		return true
	}

	if _, err := exec.LookPath("pkexec"); err != nil {
		s.lastAccessErr = "pkexec is not installed. Install it and try again:\n" +
			"  su -c 'apt install pkexec'\n" +
			"(on Debian, pkexec ships in its own package and the default user\n" +
			"isn't in the sudo group, so plain \"sudo apt install\" may also fail)"
		log.Printf("[permissions] %s", s.lastAccessErr)
		return false
	}

	tmp, err := os.CreateTemp("", "usbridge-udev-*.rules")
	if err != nil {
		s.lastAccessErr = fmt.Sprintf("could not create temp udev rule: %v", err)
		log.Printf("[permissions] create temp udev rule: %v", err)
		return false
	}
	defer os.Remove(tmp.Name())

	if _, err := tmp.WriteString(uinputRuleContent); err != nil {
		tmp.Close()
		s.lastAccessErr = fmt.Sprintf("could not write temp udev rule: %v", err)
		log.Printf("[permissions] write temp udev rule: %v", err)
		return false
	}
	tmp.Close()
	log.Printf("[permissions] temp rule at %s, running pkexec...", tmp.Name())

	// Install persistent udev rule AND immediately apply chmod for current session
	script := fmt.Sprintf(
		"cp %s %s && chmod 0666 /dev/uinput && udevadm control --reload-rules && udevadm trigger --subsystem-match=misc",
		tmp.Name(), uinputRulePath,
	)
	cmd := exec.Command("pkexec", "/bin/sh", "-c", script)
	out, err := cmd.CombinedOutput()
	log.Printf("[permissions] pkexec exit=%v output=%q", err, string(out))
	if err != nil {
		switch {
		case strings.Contains(string(out), "No authentication agent found"):
			s.lastAccessErr = "no polkit authentication agent is running for this session " +
				"(pkexec needs one to prompt for the password). Log into a full desktop " +
				"session and make sure its polkit agent is running, then retry."
		case strings.Contains(err.Error(), "exit status 126"):
			s.lastAccessErr = "authentication was cancelled or dismissed. Click Request again and approve the prompt."
		default:
			s.lastAccessErr = fmt.Sprintf("pkexec failed: %v (%s)", err, strings.TrimSpace(string(out)))
		}
		return false
	}

	time.Sleep(300 * time.Millisecond)
	granted := s.AccessibilityGranted()
	log.Printf("[permissions] after pkexec granted=%v", granted)
	if !granted {
		s.lastAccessErr = "udev rule was installed but /dev/uinput is still inaccessible; " +
			"try unplugging/replugging, or log out and back in."
	}
	return granted
}

func (s *Service) RequestScreenRecording() bool {
	if capture.GetLinuxEnv() == "Wayland" {
		err := capture.InitPortalSession()
		if err != nil {
			logrus.Errorf("Failed to initiate Wayland portal: %v", err)
		}
		return true
	}
	return true
}

func (s *Service) RequestMissing()                    {}
func (s *Service) OpenPrivacySettings() error         { return nil }
func (s *Service) OpenScreenRecordingSettings() error { return nil }

// findCapTool resolves getcap/setcap to an absolute path. Both live in
// /usr/sbin (libcap2-bin), which many non-login shells -- and pkexec's own
// sanitized environment -- don't include in PATH, so a bare exec.LookPath
// (or handing the bare name to pkexec) can fail with "not found" even
// though the binary is installed.
func findCapTool(name string) string {
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	for _, dir := range []string{"/usr/sbin", "/sbin"} {
		p := dir + "/" + name
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p
		}
	}
	return name
}

// KMSCaptureGranted reports whether the bundled sunshine_capexec launcher
// has the CAP_SYS_ADMIN capability needed for Sunshine's direct KMS screen
// capture (root-level, no compositor/portal involved).
//
// capexecPath is the path to sunshine_capexec, NOT to sunshine itself — see
// RequestKMSCapture for why the capability lives on a separate launcher.
func (s *Service) KMSCaptureGranted(capexecPath string) bool {
	if strings.TrimSpace(capexecPath) == "" {
		return false
	}
	out, err := exec.Command(findCapTool("getcap"), capexecPath).CombinedOutput()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), "cap_sys_admin")
}

// RequestKMSCapture grants CAP_SYS_ADMIN to the bundled sunshine_capexec
// launcher via pkexec setcap, so Sunshine can use its KMS capture backend
// without running as root outright.
//
// This deliberately targets sunshine_capexec, a tiny statically-linked
// (zero dynamic deps) launcher — never the sunshine binary itself. Setting a
// file capability puts the dynamic linker into "secure execution" mode for
// that binary (same as setuid): glibc ignores RPATH/RUNPATH and
// LD_LIBRARY_PATH entirely, the same protection that stops a setuid binary
// from being tricked into loading an attacker-controlled library. Since
// Sunshine resolves its bundled dependencies (e.g. libminiupnpc.so.17) via
// RPATH=$ORIGIN/../lib, setting the capability directly on it would break
// that resolution the moment it's granted. sunshine_capexec instead raises
// CAP_SYS_ADMIN into its own ambient capability set and execs the real,
// perfectly ordinary (no file capability of its own) sunshine binary —
// ambient capabilities are preserved across exec of a non-privileged binary
// without ever placing it into secure-execution mode, so its RPATH keeps
// resolving normally. See cmd/sunshine_capexec.
func (s *Service) RequestKMSCapture(capexecPath string) bool {
	if strings.TrimSpace(capexecPath) == "" {
		return false
	}
	if s.KMSCaptureGranted(capexecPath) {
		return true
	}
	cmd := exec.Command("pkexec", findCapTool("setcap"), "cap_sys_admin=eip", capexecPath)
	out, err := cmd.CombinedOutput()
	log.Printf("[permissions] setcap pkexec exit=%v output=%q", err, string(out))
	if err != nil {
		return false
	}
	return s.KMSCaptureGranted(capexecPath)
}

// GPU clock locking is Windows-only (NVML clock lock via an elevated
// gamestream-server --gpu-clock-lock-daemon helper -- see
// service_windows.go's own docs); not applicable on Linux.
func (s *Service) GPUClockLockSupported() bool                            { return false }
func (s *Service) GPUClockLockElevated() bool                             { return false }
func (s *Service) RequestGPUClockLock(binPath string, watchPID int) error { return nil }
