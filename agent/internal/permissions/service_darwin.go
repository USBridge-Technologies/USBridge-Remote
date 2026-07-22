//go:build darwin

package permissions

/*
#cgo darwin LDFLAGS: -framework ApplicationServices -framework CoreFoundation -framework AVFoundation
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

// usbridge_camera_authorized/usbridge_request_camera are implemented in
// camera_darwin.m (Objective-C — AVCaptureDevice's authorization API has no
// plain-C equivalent like the CoreGraphics screen-capture calls above).
extern int usbridge_camera_authorized(void);
extern int usbridge_request_camera(void);
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

// CameraGranted reports whether the camera TCC grant USBridgeAgent and
// Sunshine share is in place. Despite Sunshine being the process that
// actually opens the camera, macOS's hardened-runtime "responsible process"
// resolution attributes the kTCCServiceCamera check to USBridgeAgent (its
// parent, launched via plain exec rather than Launch Services) — confirmed
// via tccd's own logs — so usbridge_camera_authorized() (querying this
// process's own AVFoundation authorization status) is authoritative, the
// same way usbridge_screen_recording_granted() is for screen capture.
// sunshineHasCameraCapture() is kept as a second, independent signal in case
// Sunshine still fails to open the camera for some other reason (in use by
// another app, hardware fault, etc.) even once authorized.
func (s *Service) CameraGranted() bool {
	if C.usbridge_camera_authorized() == 0 {
		return false
	}
	return sunshineHasCameraCapture()
}

// RequestCamera requests camera access for USBridgeAgent itself. Because TCC
// attributes Sunshine's camera check to USBridgeAgent (see CameraGranted),
// this — not anything run inside Sunshine — is what actually surfaces the
// system permission dialog; blocks until the user answers it (or returns
// immediately if a decision already exists from a prior run).
func (s *Service) RequestCamera() bool {
	return C.usbridge_request_camera() != 0
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

// OpenCameraSettings opens the Camera privacy pane directly — used as a
// fallback if RequestCamera() reports denied (a prompt only fires once; a
// prior denial needs a manual toggle in Settings, same as Accessibility).
func (s *Service) OpenCameraSettings() error {
	return exec.Command("open",
		"x-apple.systempreferences:com.apple.preference.security?Privacy_Camera",
	).Run()
}

// KMSCaptureGranted and RequestKMSCapture are Linux-only (direct KMS
// capture); not applicable on macOS.
func (s *Service) KMSCaptureGranted(binPath string) bool { return false }
func (s *Service) RequestKMSCapture(binPath string) bool { return false }
