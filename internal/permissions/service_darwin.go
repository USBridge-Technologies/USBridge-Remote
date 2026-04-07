//go:build darwin

package permissions

/*
#cgo darwin LDFLAGS: -framework ApplicationServices -framework CoreFoundation
#include <ApplicationServices/ApplicationServices.h>
#include <CoreFoundation/CoreFoundation.h>

static int usbridge_accessibility_trusted() {
	return AXIsProcessTrusted() ? 1 : 0;
}

static int usbridge_request_accessibility() {
	const void *keys[] = { kAXTrustedCheckOptionPrompt };
	const void *values[] = { kCFBooleanTrue };
	CFDictionaryRef options = CFDictionaryCreate(
		kCFAllocatorDefault,
		keys,
		values,
		1,
		&kCFCopyStringDictionaryKeyCallBacks,
		&kCFTypeDictionaryValueCallBacks
	);
	int trusted = AXIsProcessTrustedWithOptions(options) ? 1 : 0;
	CFRelease(options);
	return trusted;
}

static int usbridge_screen_recording_granted() {
	return CGPreflightScreenCaptureAccess() ? 1 : 0;
}

static int usbridge_request_screen_recording() {
	return CGRequestScreenCaptureAccess() ? 1 : 0;
}
*/
import "C"

import (
	"log"
	"os"
	"os/exec"
)

type Service struct{}

func New() *Service { return &Service{} }

func (s *Service) AccessibilityGranted() bool {
	return C.usbridge_accessibility_trusted() != 0
}

func (s *Service) ScreenRecordingGranted() bool {
	return C.usbridge_screen_recording_granted() != 0
}

func (s *Service) RequestAccessibility() bool {
	return C.usbridge_request_accessibility() != 0
}

func (s *Service) RequestScreenRecording() bool {
	return C.usbridge_request_screen_recording() != 0
}

func (s *Service) RequestMissing() {
	accessGranted := s.AccessibilityGranted()
	screenGranted := s.ScreenRecordingGranted()
	if exePath, err := os.Executable(); err == nil {
		log.Printf("[permissions] executable=%s accessibility=%t screen_recording=%t", exePath, accessGranted, screenGranted)
	} else {
		log.Printf("[permissions] executable=<unknown> accessibility=%t screen_recording=%t err=%v", accessGranted, screenGranted, err)
	}
	if !accessGranted {
		_ = s.RequestAccessibility()
	}
	if !screenGranted {
		_ = s.RequestScreenRecording()
	}
}

func (s *Service) OpenPrivacySettings() error {
	urls := []string{
		"x-apple.systempreferences:com.apple.preference.security?Privacy_Accessibility",
		"x-apple.systempreferences:com.apple.preference.security?Privacy_ScreenCapture",
	}
	for _, url := range urls {
		if err := exec.Command("open", url).Run(); err != nil {
			return err
		}
	}
	return nil
}
