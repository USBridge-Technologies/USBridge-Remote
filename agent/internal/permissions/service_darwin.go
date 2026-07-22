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
	"strings"

	"usbridge_agent/internal/sunshine"
)

type Service struct{}

func New() *Service { return &Service{} }

func (s *Service) AccessibilityGranted() bool {
	return C.usbridge_accessibility_trusted() != 0
}

// ScreenRecordingGranted returns true only when both USBridgeAgent (for
// screencapture snapshots) and Sunshine (for actual video streaming) have
// screen recording permission. Sunshine's status is detected by reading its
// log: if the most recent startup contains "No screen capture permission"
// then Sunshine was denied and we report false so the UI shows ❌.
func (s *Service) ScreenRecordingGranted() bool {
	if C.usbridge_screen_recording_granted() == 0 {
		return false
	}
	return sunshineHasScreenCapture()
}

// sunshineHasScreenCapture reads Sunshine's log and checks whether the most
// recent Sunshine startup succeeded in getting screen capture access.
// Returns true (optimistic) if the log is absent or contains no startup yet.
func sunshineHasScreenCapture() bool {
	logPath := sunshine.LogPath()
	if logPath == "" {
		return true
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		return true // Sunshine hasn't started yet
	}
	content := string(data)
	// Find the last startup entry so we only check the current session.
	lastStart := strings.LastIndex(content, "Sunshine version:")
	if lastStart == -1 {
		lastStart = 0
	}
	return !strings.Contains(content[lastStart:], "No screen capture permission")
}

func (s *Service) RequestAccessibility() bool {
	return C.usbridge_request_accessibility() != 0
}

// RequestScreenRecording requests screen recording for USBridgeAgent itself
// (needed for screencapture snapshots). Sunshine — a separate process — must
// be granted screen recording independently via System Preferences; use
// OpenScreenRecordingSettings to send the user there.
func (s *Service) RequestScreenRecording() bool {
	return C.usbridge_request_screen_recording() != 0
}

func (s *Service) RequestMissing() {
	accessGranted := s.AccessibilityGranted()
	screenGranted := C.usbridge_screen_recording_granted() != 0 // agent-only check here
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

// OpenScreenRecordingSettings opens only the Screen Recording pane so the
// user can grant Sunshine access without being distracted by Accessibility.
func (s *Service) OpenScreenRecordingSettings() error {
	return exec.Command("open",
		"x-apple.systempreferences:com.apple.preference.security?Privacy_ScreenCapture",
	).Run()
}

// CameraGranted infers whether Sunshine (a separate process — camera capture
// happens entirely inside it, USBridgeAgent never touches the camera itself)
// has camera access, the same way ScreenRecordingGranted infers Sunshine's
// screen-capture status: by reading its most recent startup log. Unlike
// screen capture, Sunshine only touches the camera lazily — once a client
// actually picks a "cam:" device — so this is optimistic (true) until that
// has happened at least once this session; macOS has no
// CGPreflightScreenCaptureAccess equivalent for camera that would let us
// check another process's TCC grant ahead of time.
func (s *Service) CameraGranted() bool {
	return sunshineHasCameraCapture()
}

func sunshineHasCameraCapture() bool {
	logPath := sunshine.LogPath()
	if logPath == "" {
		return true
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		return true
	}
	content := string(data)
	lastStart := strings.LastIndex(content, "Sunshine version:")
	if lastStart == -1 {
		lastStart = 0
	}
	return !strings.Contains(content[lastStart:], "Camera setup failed")
}

// OpenCameraSettings opens the Camera privacy pane so the user can grant
// Sunshine access — same reasoning as OpenScreenRecordingSettings: camera
// capture runs inside the Sunshine process, so USBridgeAgent cannot request
// it on Sunshine's behalf, only point the user at the right Settings pane.
func (s *Service) OpenCameraSettings() error {
	return exec.Command("open",
		"x-apple.systempreferences:com.apple.preference.security?Privacy_Camera",
	).Run()
}

// KMSCaptureGranted and RequestKMSCapture are Linux-only (direct KMS
// capture); not applicable on macOS.
func (s *Service) KMSCaptureGranted(binPath string) bool { return false }
func (s *Service) RequestKMSCapture(binPath string) bool { return false }
