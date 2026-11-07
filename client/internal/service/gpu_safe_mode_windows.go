//go:build windows

package service

/*
extern void win_gpu_safe_mode_raise(int level);
extern int  win_gpu_safe_mode(void);
extern int  vk_video_is_gpu_stuck(void);
extern void vk_video_mark_gpu_stuck(void);
extern int  d3dx_unblock(void);
extern int  vk_video_backend(void);
*/
import "C"

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// GPU safe mode (Windows): how much of the GPU video pipeline this process
// avoids. Levels (vk_hwdev_bridge_windows.c):
//
//	0  normal: D3D11VA decode -> Vulkan zero-copy interop -> Vulkan overlay
//	1  no D3D11VA->Vulkan interop: Vulkan Video decode, or D3D11VA with a CPU
//	   copy, still shown by the Vulkan overlay
//	2  no Vulkan: D3D11VA decode sampled by the D3D11 overlay backend
//	   (dx_present_windows.c), software decode uploaded to it otherwise
//	3  no GPU presentation: GDI blit of CPU frames -- slowest, but needs
//	   nothing beyond a basic display driver
//
// Each overlay also falls down this list by itself when a backend fails to
// start (no Vulkan driver in a VM, no D3D11 device), without a level.
//
// Sources, highest wins:
//   - USBRIDGE_RENDERER=gdi (3), =d3d11 (2) or =vulkan-nointerop (1), for
//     testing/support.
//   - A GPU stall remembered from an earlier run of this same client
//     version (gpu_safe_mode.json); a new version tries the full pipeline
//     again, so a fix or driver update is not masked forever.
//   - A stall detected right now (HandleGPUStuck): a Vulkan stall puts this
//     process on 2, since the stuck VkDevice is shared by decode and
//     presentation and cannot be recreated; the remembered level is 1 if the
//     D3D11 interop was in use (the next run then still gets Vulkan, minus
//     the interop), else 2. A D3D11 stall puts this process, and the next
//     run, on 3.
//
// None of this touches the per-frame path: on hardware that works, the
// pipeline is exactly what it was.

const gpuSafeModeFile = "gpu_safe_mode.json"

type gpuSafeModeRecord struct {
	Level   int    `json:"level"`
	Version string `json:"version"`
	GPU     string `json:"gpu,omitempty"`
	Reason  string `json:"reason,omitempty"`
	Time    string `json:"time,omitempty"`
}

var (
	gpuSafeModeMu      sync.Mutex
	gpuSafeModePath    string
	gpuSafeModeVersion string
)

// InitGPUSafeMode applies USBRIDGE_RENDERER and any remembered stall. Call
// once at startup, before the first stream.
func InitGPUSafeMode(appName, version string) {
	gpuSafeModeMu.Lock()
	defer gpuSafeModeMu.Unlock()
	gpuSafeModeVersion = version
	if base, err := os.UserConfigDir(); err == nil {
		gpuSafeModePath = filepath.Join(base, appName, gpuSafeModeFile)
	}

	switch strings.ToLower(strings.TrimSpace(os.Getenv("USBRIDGE_RENDERER"))) {
	case "gdi":
		C.win_gpu_safe_mode_raise(3)
		logrus.Warn("🛟 [GPU safe mode] USBRIDGE_RENDERER=gdi -- level 3 (GDI overlay)")
	case "d3d11":
		C.win_gpu_safe_mode_raise(2)
		logrus.Warn("🛟 [GPU safe mode] USBRIDGE_RENDERER=d3d11 -- level 2 (no Vulkan, D3D11 overlay)")
	case "vulkan-nointerop":
		C.win_gpu_safe_mode_raise(1)
		logrus.Warn("🛟 [GPU safe mode] USBRIDGE_RENDERER=vulkan-nointerop -- level 1 (no D3D11->Vulkan interop)")
	}

	if gpuSafeModePath == "" {
		return
	}
	data, err := os.ReadFile(gpuSafeModePath)
	if err != nil {
		return
	}
	var rec gpuSafeModeRecord
	if json.Unmarshal(data, &rec) != nil || rec.Level <= 0 {
		return
	}
	if rec.Version != version {
		logrus.Infof("🛟 [GPU safe mode] stall remembered from client %s (level %d, %s) -- new version, trying the full pipeline again",
			rec.Version, rec.Level, rec.Reason)
		_ = os.Remove(gpuSafeModePath)
		return
	}
	C.win_gpu_safe_mode_raise(C.int(rec.Level))
	logrus.Warnf("🛟 [GPU safe mode] level %d from a GPU stall remembered at %s (gpu=%q: %s)",
		rec.Level, rec.Time, rec.GPU, rec.Reason)
}

// GPUSafeModeLevel reports the current level (see InitGPUSafeMode).
func GPUSafeModeLevel() int { return int(C.win_gpu_safe_mode()) }

// VKVideoIsGPUStuck reports that the Vulkan renderer gave up on a GPU that
// stopped completing work (vk_video_impl_windows.c, g_gpu_stuck).
func VKVideoIsGPUStuck() bool { return C.vk_video_is_gpu_stuck() != 0 }

// Overlay presentation backends (vk_video_impl_windows.c, VK_BACKEND_*).
const (
	overlayBackendVulkan = 0
	overlayBackendD3D11  = 1
	overlayBackendGDI    = 2
)

// VKVideoBackend names the active overlay's presentation backend ("" when
// no overlay is up).
func VKVideoBackend() string {
	switch C.vk_video_backend() {
	case overlayBackendVulkan:
		return "Vulkan"
	case overlayBackendD3D11:
		return "D3D11"
	case overlayBackendGDI:
		return "GDI"
	}
	return ""
}

// HandleGPUStuck moves this process off the renderer that stalled (Vulkan
// -> D3D11, D3D11 -> GDI) and remembers it for the next run. Returns false
// if it was already handled. The caller restarts the stream so the decoder
// and overlay are picked again.
func HandleGPUStuck() bool {
	gpuSafeModeMu.Lock()
	defer gpuSafeModeMu.Unlock()
	d3d11 := C.vk_video_backend() == overlayBackendD3D11
	level, remember, reason, next := 2, 2, "Vulkan renderer stalled", "D3D11"
	if d3d11 {
		level, remember, reason, next = 3, 3, "D3D11 renderer stalled", "GDI"
	} else if usingD3DXDecode() {
		remember, reason = 1, "Vulkan renderer stalled on the D3D11VA->Vulkan interop"
	}
	if GPUSafeModeLevel() >= level {
		return false
	}
	gpu, _ := VKVideoGetGPUName()
	C.win_gpu_safe_mode_raise(C.int(level))
	logrus.Errorf("🛟 [GPU safe mode] %s (gpu=%q) -- this session continues on %s; next start uses level %d",
		reason, gpu, next, remember)

	saveGPUSafeModeLocked(remember, gpu, reason)
	return true
}

// goGPUSafeModeDecoderRejected: the decoder in moonlight_cgo_windows.go
// dropped a hardware tier that rejected every frame and raised this
// process to level; remember it for the next start of this version.
//
//export goGPUSafeModeDecoderRejected
func goGPUSafeModeDecoderRejected(level C.int, reason *C.char) {
	r := C.GoString(reason)
	gpu, _ := VKVideoGetGPUName()
	logrus.Errorf("🛟 [GPU safe mode] %s (gpu=%q) -- level %d (D3D11 renderer) for this process and the next start", r, gpu, int(level))
	gpuSafeModeMu.Lock()
	defer gpuSafeModeMu.Unlock()
	saveGPUSafeModeLocked(int(level), gpu, r)
}

// saveGPUSafeModeLocked writes gpu_safe_mode.json; gpuSafeModeMu held.
func saveGPUSafeModeLocked(level int, gpu, reason string) {
	if gpuSafeModePath == "" {
		return
	}
	rec := gpuSafeModeRecord{
		Level:   level,
		Version: gpuSafeModeVersion,
		GPU:     gpu,
		Reason:  reason,
		Time:    time.Now().Format(time.RFC3339),
	}
	if data, err := json.MarshalIndent(rec, "", "  "); err == nil {
		_ = os.MkdirAll(filepath.Dir(gpuSafeModePath), 0o755)
		if err := os.WriteFile(gpuSafeModePath, data, 0o644); err != nil {
			logrus.Warnf("🛟 [GPU safe mode] could not save %s: %v", gpuSafeModePath, err)
		}
	}
}

var gpuStallWatchdogOnce sync.Once

// StartGPUStallWatchdog watches the overlay's render thread (Vulkan or
// D3D11) from its own
// goroutine. A render thread parked for 5s in acquire/fence-wait/submit/
// present means the driver is blocking there rather than honouring the
// timeouts (seen on an Intel UHD with a GPU wait that never got its
// signal: vkWaitForFences did not return, and Fyne's GL main loop froze with
// it), so the in-thread stall counter never fires. Then, or when the render
// thread latched g_gpu_stuck itself, this releases the stuck GPU work
// (d3dx_unblock) so the driver lets go of the render thread and the GL main
// loop; the main loop then switches the session to GDI (HandleGPUStuck via
// the video widget). Must not depend on the Fyne main loop: that is what
// freezes.
func StartGPUStallWatchdog() {
	gpuStallWatchdogOnce.Do(func() {
		go func() {
			var lastHB int64 = -1
			since := time.Now()
			released := map[C.int]bool{} // per backend: each is given up once
			for range time.Tick(time.Second) {
				backend := C.vk_video_backend()
				if backend < 0 || backend == overlayBackendGDI || released[backend] {
					lastHB = -1
					continue
				}
				hb, stage := VKVideoGetDiag()
				if hb != lastHB {
					lastHB, since = hb, time.Now()
				}
				blocked := stage >= 3 && stage <= 6 && time.Since(since) >= 5*time.Second
				stuck := VKVideoIsGPUStuck()
				if !blocked && !stuck {
					continue
				}
				if blocked && !stuck {
					logrus.Errorf("🛟 [GPU safe mode] render thread blocked %s in stage %d -- releasing the GPU",
						time.Since(since).Round(time.Second), stage)
					C.vk_video_mark_gpu_stuck()
				}
				if backend == overlayBackendVulkan {
					C.d3dx_unblock()
				}
				released[backend] = true
			}
		}()
	})
}
