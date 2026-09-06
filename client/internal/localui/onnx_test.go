package localui

import (
	"runtime"
	"testing"
)

// TestDefaultRuntimeLibNameMatchesCurrentPlatform pins the ".so"-only bug
// this replaced: internal/api/local_ui_init.go and cmd/localui_bench used
// to hardcode "libonnxruntime.so" as the default runtime path, which never
// resolved on macOS (Homebrew's onnxruntime ships "libonnxruntime.dylib").
// Runs on whatever GOOS actually built the test, so it always exercises a
// live case rather than asserting all three branches by inspection alone.
func TestDefaultRuntimeLibNameMatchesCurrentPlatform(t *testing.T) {
	got := DefaultRuntimeLibName()
	var want string
	switch runtime.GOOS {
	case "darwin":
		want = "libonnxruntime.dylib"
	case "windows":
		want = "onnxruntime.dll"
	default:
		want = "libonnxruntime.so"
	}
	if got != want {
		t.Errorf("DefaultRuntimeLibName() on GOOS=%s = %q, want %q", runtime.GOOS, got, want)
	}
}
