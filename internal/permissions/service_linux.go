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

func (s *Service) RequestMissing()                      {}
func (s *Service) OpenPrivacySettings() error           { return nil }
func (s *Service) OpenScreenRecordingSettings() error   { return nil }

// KMSCaptureGranted reports whether the bundled Sunshine binary has the
// CAP_SYS_ADMIN capability needed for direct KMS screen capture (root-level,
// no compositor/portal involved).
func (s *Service) KMSCaptureGranted(binPath string) bool {
	if strings.TrimSpace(binPath) == "" {
		return false
	}
	out, err := exec.Command("getcap", binPath).CombinedOutput()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), "cap_sys_admin")
}

// RequestKMSCapture grants CAP_SYS_ADMIN to the bundled Sunshine binary via
// pkexec setcap, so Sunshine can use its KMS capture backend without running
// as root outright.
func (s *Service) RequestKMSCapture(binPath string) bool {
	if strings.TrimSpace(binPath) == "" {
		return false
	}
	if s.KMSCaptureGranted(binPath) {
		return true
	}
	cmd := exec.Command("pkexec", "setcap", "cap_sys_admin+ep", binPath)
	out, err := cmd.CombinedOutput()
	log.Printf("[permissions] setcap pkexec exit=%v output=%q", err, string(out))
	if err != nil {
		return false
	}
	return s.KMSCaptureGranted(binPath)
}
