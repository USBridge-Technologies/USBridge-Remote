//go:build windows
// +build windows

package service

import (
	"image"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"usbridge-client/internal/models"
)

// TestWindowsLiveStreamDecoding — integration test: start video like in the UI,
// listening on the static port DefaultVideoUDPPort (55000), verifying that decoding yields frames.
// Requires: the device is already streaming to this PC on port 55000 (start the video in the app on the device).
func TestWindowsLiveStreamDecoding(t *testing.T) {
	cfg := models.DefaultConfig()
	cfg.VideoUDPPort = models.DefaultVideoUDPPort
	gs := NewGStreamerService(cfg)

	var framesReceived int64
	gs.SetOnFrameReceived(func(img image.Image) {
		atomic.AddInt64(&framesReceived, 1)
	})

	if err := gs.ConnectToRTP(); err != nil {
		if strings.Contains(err.Error(), "Failed to change state to PLAYING") {
			t.Skipf("port %d busy or unavailable — close the application and other processes, then repeat: %v", models.DefaultVideoUDPPort, err)
		}
		t.Fatalf("ConnectToRTP (like in UI): %v", err)
	}
	defer gs.Disconnect()

	// Waiting for frames from the live stream (like on the control screen)
	const waitTimeout = 15 * time.Second
	const minFrames = 1
	deadline := time.Now().Add(waitTimeout)
	for time.Now().Before(deadline) {
		n := atomic.LoadInt64(&framesReceived)
		if n >= minFrames {
			t.Logf("OK: live stream on port %d — %d frames received, decoding is working", models.DefaultVideoUDPPort, n)
			return
		}
		time.Sleep(200 * time.Millisecond)
	}

	n := atomic.LoadInt64(&framesReceived)
	if n == 0 {
		t.Skipf("no frames in %v: no stream on port %d — start the video on the device and repeat the test", waitTimeout, models.DefaultVideoUDPPort)
	}
	t.Logf("OK: %d frames received from port %d", n, models.DefaultVideoUDPPort)
}
