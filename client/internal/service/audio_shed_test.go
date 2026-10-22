package service

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestAudioShedRule runs audio_shed.h through the host C compiler: the queue
// bound every platform's audio output relies on to keep capture-card clock
// drift and Wi-Fi bursts from growing into lag.
func TestAudioShedRule(t *testing.T) {
	cc, err := exec.LookPath("cc")
	if err != nil {
		t.Skip("no C compiler")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "shed.c")
	abs, _ := filepath.Abs("audio_shed.h")
	prog := `#include "` + abs + `"
#include <stdio.h>
int main(void) {
    int fails = 0;
#define EXPECT(c) do { if (!(c)) { printf("FAIL: %s\n", #c); fails++; } } while (0)
    EXPECT(!audio_should_shed(3840, 48000, 0));   /* exactly 80 ms: keep */
    EXPECT( audio_should_shed(3841, 48000, 0));   /* past 80 ms: shed */
    EXPECT(!audio_should_shed(0, 48000, 0));
    EXPECT(!audio_should_shed(5000, 48000, 5760)); /* device floor wins */
    EXPECT( audio_should_shed(5761, 48000, 5760));
    EXPECT(!audio_should_shed(3840, 48000, 960));  /* smaller floor ignored */
    return fails;
}
`
	if err := os.WriteFile(src, []byte(prog), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "shed")
	if out, err := exec.Command(cc, "-o", bin, src).CombinedOutput(); err != nil {
		t.Fatalf("compile: %v\n%s", err, out)
	}
	if out, err := exec.Command(bin).CombinedOutput(); err != nil {
		t.Fatalf("audio_should_shed: %v\n%s", err, out)
	}
}

func readSource(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestAudioPlatformsUseShedRule: every platform output goes through the
// shared rule (Linux and Apple via moonlight_cgo_shared.h).
func TestAudioPlatformsUseShedRule(t *testing.T) {
	for _, f := range []string{"moonlight_cgo_android.go", "moonlight_cgo_windows.go", "moonlight_cgo_shared.h"} {
		if !strings.Contains(readSource(t, f), `#include "audio_shed.h"`) {
			t.Errorf("%s does not include audio_shed.h", f)
		}
	}
	for _, f := range []string{"moonlight_cgo_android.go", "moonlight_cgo_windows.go", "moonlight_cgo_linux.go", "moonlight_cgo_apple.go"} {
		if !strings.Contains(readSource(t, f), "audio_should_shed(") {
			t.Errorf("%s no longer sheds audio past the queue bound", f)
		}
	}
}

// TestAndroidAudioNoCrackleRegressions pins the three AAudio settings whose
// loss was audible:
//   - LOW_LATENCY as the starting mode: ~32 ms capacity underran 5-15x/s over
//     Wi-Fi (387 XRuns in a session's first 35 s) -> crackle.
//   - shedding before the stream has played anything, or below its buffer
//     size: a shared stream never starts -> silence right after connecting.
func TestAndroidAudioNoCrackleRegressions(t *testing.T) {
	src := readSource(t, "moonlight_cgo_android.go")
	if !regexp.MustCompile(`g_aa_perf_mode\s*=\s*AAUDIO_PERFORMANCE_MODE_NONE;`).MatchString(src) {
		t.Error("g_aa_perf_mode must start at AAUDIO_PERFORMANCE_MODE_NONE (LOW_LATENCY crackles over Wi-Fi)")
	}
	if !strings.Contains(src, "read > 0 && audio_should_shed(") {
		t.Error("Android shed must wait until the stream has played (framesRead > 0)")
	}
	if !regexp.MustCompile(`audio_should_shed\([^;]*AAudioStream_getBufferSizeInFrames\(g_aa_stream\)\)`).MatchString(src) {
		t.Error("Android shed floor must be the stream's buffer size")
	}
}

// TestRtpAudioQueueWaitsForLateAudio: the fork waits 60 ms for out-of-order
// audio; upstream's 10 ms gave up on Wi-Fi-delayed packets that were still
// coming, turning them into concealment.
func TestRtpAudioQueueWaitsForLateAudio(t *testing.T) {
	b, err := os.ReadFile("../../moonlight-common-c/src/RtpAudioQueue.h")
	if err != nil {
		t.Skip("moonlight-common-c submodule not checked out")
	}
	if !regexp.MustCompile(`#define\s+RTPQ_OOS_WAIT_TIME_MS\s+60\b`).Match(b) {
		t.Error("RTPQ_OOS_WAIT_TIME_MS must stay 60")
	}
}
