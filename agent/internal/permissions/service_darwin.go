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
)

type Service struct{}

func New() *Service { return &Service{} }

func (s *Service) AccessibilityGranted() bool {
	return C.usbridge_accessibility_trusted() != 0
}

// ScreenRecordingGranted reports whether USBridgeAgent has screen recording
// access. It used to also cross-check Sunshine's own log for "No screen
// capture permission" (sunshineHasScreenCapture, now removed) on the theory
// that Sunshine needed an independent grant — but tccd's own logs show
// kTCCServiceScreenCapture is attributed to USBridgeAgent (the "responsible"
// process), the same finding behind CameraGranted, so that check was
// redundant. Worse, it was actively wrong in practice: sunshine.log is
// written through a buffered sink that Sunshine's process routinely gets
// killed/restarted before flushing, so it can sit stuck on a stale, long-past
// session (sometimes still showing an old denial) for hours after the real
// grant succeeded — which showed the ❌/Request button even while capture
// was actively working.
func (s *Service) ScreenRecordingGranted() bool {
	return C.usbridge_screen_recording_granted() != 0
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

// CameraGranted reports whether USBridgeAgent has camera access. Despite
// Sunshine being the process that actually opens the camera, macOS's
// hardened-runtime "responsible process" resolution attributes the
// kTCCServiceCamera check to USBridgeAgent (its parent, launched via plain
// exec rather than Launch Services) — confirmed via tccd's own logs — so
// usbridge_camera_authorized() (querying this process's own AVFoundation
// authorization status) is authoritative on its own; no need to also
// cross-check Sunshine's log (see ScreenRecordingGranted's doc comment for
// why that log is unreliable — same buffered-sink-never-flushes issue would
// apply here too).
func (s *Service) CameraGranted() bool {
	return C.usbridge_camera_authorized() != 0
}

// RequestCamera requests camera access for USBridgeAgent itself. Because TCC
// attributes Sunshine's camera check to USBridgeAgent (see CameraGranted),
// this — not anything run inside Sunshine — is what actually surfaces the
// system permission dialog; blocks until the user answers it (or returns
// immediately if a decision already exists from a prior run).
func (s *Service) RequestCamera() bool {
	return C.usbridge_request_camera() != 0
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
