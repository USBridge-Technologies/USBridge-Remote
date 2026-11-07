package service

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/sirupsen/logrus"
)

// The video renderer the user picked in Client settings (default Auto), and
// on Linux the display server the window runs on. Kept in a file of its own
// rather than Fyne's preferences: the display server has to be known before
// the Fyne app (and GLFW) exists, and Windows reads the renderer before the
// first stream, in InitGPUSafeMode.

// Video renderer IDs. Which ones a platform offers: VideoRenderers.
const (
	VideoRendererAuto     = "auto"
	VideoRendererVulkan   = "vulkan"   // Linux, Windows
	VideoRendererOpenGL   = "opengl"   // Linux: GLX on X11, EGL on Wayland
	VideoRendererD3D11    = "d3d11"    // Windows
	VideoRendererGDI      = "gdi"      // Windows
	VideoRendererMetal    = "metal"    // macOS
	VideoRendererSoftware = "software" // Linux, macOS: drawn by the UI toolkit, no native layer
)

// Display server IDs (Linux only).
const (
	DisplayServerAuto    = "auto"
	DisplayServerWayland = "wayland"
	DisplayServerX11     = "x11"
)

// VideoRendererPrefs is the stored choice.
type VideoRendererPrefs struct {
	Renderer      string `json:"renderer,omitempty"`
	DisplayServer string `json:"display_server,omitempty"`
}

const videoRendererPrefsFile = "video_renderer.json"

var (
	videoRendererMu    sync.Mutex
	videoRendererPath  string
	videoRendererPrefs VideoRendererPrefs
)

// VideoRenderers lists the renderers this platform offers, Auto first; nil
// where there is nothing to choose (mobile, web).
func VideoRenderers() []string {
	switch runtime.GOOS {
	case "linux":
		return []string{VideoRendererAuto, VideoRendererVulkan, VideoRendererOpenGL, VideoRendererSoftware}
	case "windows":
		return []string{VideoRendererAuto, VideoRendererVulkan, VideoRendererD3D11, VideoRendererGDI}
	case "darwin":
		return []string{VideoRendererAuto, VideoRendererMetal, VideoRendererSoftware}
	}
	return nil
}

// DisplayServers lists the display servers to choose from (Linux only).
func DisplayServers() []string {
	if runtime.GOOS != "linux" {
		return nil
	}
	return []string{DisplayServerAuto, DisplayServerWayland, DisplayServerX11}
}

// InitVideoRendererPrefs loads the stored choice and applies the display
// server. Call once at startup, before the Fyne app is created.
func InitVideoRendererPrefs(appName string) {
	videoRendererMu.Lock()
	if base, err := os.UserConfigDir(); err == nil {
		videoRendererPath = filepath.Join(base, appName, videoRendererPrefsFile)
		if data, err := os.ReadFile(videoRendererPath); err == nil {
			_ = json.Unmarshal(data, &videoRendererPrefs)
		}
	}
	p := videoRendererPrefs
	videoRendererMu.Unlock()

	if r := p.Renderer; r != "" && r != VideoRendererAuto {
		logrus.Infof("🎞️ [Renderer] video renderer set to %s in Client settings", r)
	}
	applyDisplayServer(p.DisplayServer)
}

// applyDisplayServer steers GLFW's platform choice (Fyne reads
// FYNE_PLATFORM when it initializes GLFW). An explicit FYNE_PLATFORM from
// the environment wins, and a server that is not running is never forced
// -- the window would not open at all.
func applyDisplayServer(ds string) {
	if runtime.GOOS != "linux" || ds == "" || ds == DisplayServerAuto {
		return
	}
	if os.Getenv("FYNE_PLATFORM") != "" {
		return
	}
	switch ds {
	case DisplayServerWayland:
		if os.Getenv("WAYLAND_DISPLAY") == "" {
			logrus.Warn("🎞️ [Renderer] Wayland chosen but no Wayland session -- automatic")
			return
		}
	case DisplayServerX11:
		if os.Getenv("DISPLAY") == "" {
			logrus.Warn("🎞️ [Renderer] X11 chosen but no X server (DISPLAY) -- automatic")
			return
		}
	default:
		return
	}
	_ = os.Setenv("FYNE_PLATFORM", ds)
	logrus.Infof("🎞️ [Renderer] display server forced to %s in Client settings", ds)
}

// CurrentVideoRendererPrefs returns the stored choice ("" fields = Auto).
func CurrentVideoRendererPrefs() VideoRendererPrefs {
	videoRendererMu.Lock()
	defer videoRendererMu.Unlock()
	return videoRendererPrefs
}

// VideoRenderer is the chosen renderer, VideoRendererAuto when none.
func VideoRenderer() string {
	if r := CurrentVideoRendererPrefs().Renderer; r != "" {
		return r
	}
	return VideoRendererAuto
}

// SaveVideoRendererPrefs stores a new choice. The renderer applies to the
// next stream on Linux and macOS, after a restart on Windows; the display
// server after a restart.
func SaveVideoRendererPrefs(p VideoRendererPrefs) error {
	videoRendererMu.Lock()
	defer videoRendererMu.Unlock()
	videoRendererPrefs = p
	if videoRendererPath == "" {
		return nil
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(videoRendererPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(videoRendererPath, data, 0o644)
}
