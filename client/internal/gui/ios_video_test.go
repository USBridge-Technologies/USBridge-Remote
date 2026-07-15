package gui

import (
	"testing"
	"time"

	"fyne.io/fyne/v2/test"
	"usbridge-client/internal/models"
)

func TestIOSVideoStreamingLaunch(t *testing.T) {
	// Initialize a headless Fyne app
	testApp := test.NewApp()
	defer testApp.Quit()

	cfg := models.DefaultConfig()
	mw := NewMainWindow(cfg)

	if mw == nil {
		t.Fatal("MainWindow was not initialized")
	}

	t.Log("MainWindow initialized. Forcing connection to a dummy host...")

	// Simulate clicking connect from the manager with a dummy host that doesn't exist.
	// This will trigger the connection logic.
	mw.handleConnectionFromManager("192.0.2.1", "dummy_secret", "", "Direct", 0, false)

	// Wait for the connection logic to process. In a real app this takes some time.
	// We wait 3 seconds to let goroutines try connecting.
	time.Sleep(3 * time.Second)

	// According to the requirement: the interface should load and video stream should be shown,
	// but since it's a dummy host (or video is broken on iOS), it should fail.

	if mw.isStreaming {
		t.Error("Test failed: isStreaming is true, but video should not be working (or we connected to a dummy host)")
	} else {
		t.Log("Test passed: Caught the fact that video is NOT streaming.")
	}

	if mw.videoWidget == nil {
		t.Error("Test failed: videoWidget is nil, interface did not load properly")
	} else {
		t.Log("videoWidget is present in the UI.")
	}
}
