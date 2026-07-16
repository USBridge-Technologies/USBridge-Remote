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

type Service struct{}

func New() *Service { return &Service{} }

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
	log.Printf("[permissions] RequestAccessibility called, granted=%v", s.AccessibilityGranted())
	if s.AccessibilityGranted() {
		return true
	}

	tmp, err := os.CreateTemp("", "usbridge-udev-*.rules")
	if err != nil {
		log.Printf("[permissions] create temp udev rule: %v", err)
		return false
	}
	defer os.Remove(tmp.Name())

	if _, err := tmp.WriteString(uinputRuleContent); err != nil {
		tmp.Close()
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
		return false
	}

	time.Sleep(300 * time.Millisecond)
	granted := s.AccessibilityGranted()
	log.Printf("[permissions] after pkexec granted=%v", granted)
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
	out, err := exec.Command("getcap", capexecPath).CombinedOutput()
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
	cmd := exec.Command("pkexec", "setcap", "cap_sys_admin=eip", capexecPath)
	out, err := cmd.CombinedOutput()
	log.Printf("[permissions] setcap pkexec exit=%v output=%q", err, string(out))
	if err != nil {
		return false
	}
	return s.KMSCaptureGranted(capexecPath)
}
