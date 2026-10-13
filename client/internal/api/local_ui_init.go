package api

import (
	"log"
	"os"
	"path/filepath"
	"runtime"

	"usbridge-client/internal/localui"
	"usbridge-client/internal/models"
)

// InitLocalUIParseFromConfig builds and installs the local ui.parse backend
// (see local_ui_intercept.go) if cfg.LocalUIParseEnabled. Callers: only
// LazyInitLocalUIParse below, itself only called the moment a feature that
// actually needs local inference turns on THIS session -- AI Vision's
// checkbox (service.SetAIVisionEnabled), the Scripts&AI tab's "Local
// models" toggle (scripts_tab_widget.go's applyLocalUIParseSetting), or a
// device connection actually completing while that toggle was already on
// from a previous session (scripts_tab_widget.go's SetClient ->
// maybeLazyInitLocalUIParse) -- deliberately NOT called unconditionally at
// app boot (main-window/tab construction) just because LocalUIParseEnabled
// was left persisted true from a previous session. That used to be a real
// bug, not a design choice: build() calling this directly cost ~250MB RSS
// and a measured 1.3s+ Go-scheduler-wide stall on every single launch,
// whether or not the session ever connected to a device or issued one
// ui.parse call.
// Loading the ONNX models is relatively expensive (hundreds of ms to a few
// seconds on desktop; under wasm it's a ~100MB+ network fetch, not a disk
// read -- see internal/localui/stub_wasm.go), so paying that cost at every
// launch whether or not the user ends up using either feature this session
// wasted real bandwidth/CPU for nothing. This runs in the background and
// installs itself via SetLocalUIParser only once ready; until then, ui.parse
// calls forward to the device as normal. A failure here (models/runtime lib
// missing, bad path, etc.) is logged and otherwise harmless -- ui.parse just
// keeps going to the device, matching the "optional accelerator, never a
// hard dependency" pattern used throughout this feature.
func InitLocalUIParseFromConfig(cfg *models.AppConfig) {
	if cfg == nil || !cfg.LocalUIParseEnabled {
		return
	}
	go func() {
		modelDir := cfg.LocalUIParseModelDir
		if modelDir == "" {
			modelDir = localUIDefaultModelDir()
		}
		ortLib := cfg.LocalUIParseORTLib
		if ortLib == "" {
			libName := localui.DefaultRuntimeLibName()
			if p := cudaRuntimeLib(libName); p != "" {
				ortLib = p
			}
		}
		if ortLib == "" {
			libName := localui.DefaultRuntimeLibName()
			ortLib = resolveLocalUIPath(
				filepath.Join("..", "Frameworks", libName), // macOS .app: Contents/MacOS/../Frameworks/<lib>
				libName, // flat layout: next to the executable
				filepath.Join(defaultLocalUIDir("runtime"), libName),
			)
		}

		lcfg := localui.Config{
			IconONNXPath:  filepath.Join(modelDir, "icon_detect.onnx"),
			DBNetONNXPath: filepath.Join(modelDir, "dbnet.onnx"),
			SVTRONNXPath:  filepath.Join(modelDir, "svtr.onnx"),
			SharedLibPath: ortLib,
			UseGPU:        cfg.LocalUIParseGPU,
		}
		parser, err := localui.NewParser(lcfg)
		if err != nil {
			log.Printf("local ui.parse: not available (%v) -- ui.parse will keep forwarding to the device. Run scripts/setup_localui.sh to install models+runtime.", err)
			return
		}
		SetLocalUIParser(parser)
		log.Printf("✅ local ui.parse offload active (models=%s, gpu=%v)", modelDir, cfg.LocalUIParseGPU)
	}()
}

// LazyInitLocalUIParse triggers InitLocalUIParseFromConfig on demand,
// no-op if a parser is already loaded (or already loading -- NewParser's
// own initOnce/session setup makes a second concurrent call harmless, but
// this check skips the redundant work entirely). Call this from the exact
// moment a feature that actually needs local inference turns on, not from
// app startup -- see InitLocalUIParseFromConfig's doc comment for why.
// Uses models.DefaultConfig() rather than the app's persisted config: the
// only thing that matters here is LocalUIParseEnabled=true to make
// InitLocalUIParseFromConfig proceed; model dir/runtime lib path resolution
// (resolveLocalUIPath) already has its own sensible defaults independent of
// anything else in AppConfig.
func LazyInitLocalUIParse() {
	if GetLocalUIParser() != nil {
		return
	}
	cfg := models.DefaultConfig()
	cfg.LocalUIParseEnabled = true
	InitLocalUIParseFromConfig(cfg)
}

// localUIDefaultModelDir resolves the model directory when
// cfg.LocalUIParseModelDir is left unset. Native platforms use
// resolveLocalUIPath's executable-relative/~/.usbridge fallback chain
// below; wasm overrides this var (local_ui_init_wasm.go's init) to a plain
// relative "models" web path instead, since os.Executable()/os.Stat-based
// filesystem resolution means nothing in a browser -- same swappable-var
// pattern ai_vision.go's aiVisionMetalPush uses for its own
// platform-specific override.
var localUIDefaultModelDir = func() string {
	return resolveLocalUIPath(
		filepath.Join("..", "Resources", "localui", "models"), // macOS .app: Contents/MacOS/../Resources/localui/models
		filepath.Join("localui", "models"),                    // flat layout: next to the executable
		defaultLocalUIDir("models"),
	)
}

// defaultLocalUIDir returns ~/.usbridge/localui/<sub>, falling back to
// ./.usbridge/localui/<sub> if the home directory can't be resolved.
func defaultLocalUIDir(sub string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(".usbridge", "localui", sub)
	}
	return filepath.Join(home, ".usbridge", "localui", sub)
}

// resolveLocalUIPath picks a default model dir / ORT runtime lib path when
// the user's config leaves one unset: it prefers whatever a packaged build
// bundled next to the running executable (so a fresh install works without
// the user ever running scripts/setup_localui.sh) over the older
// ~/.usbridge/localui dev-setup convention.
//
// bundleRel is checked relative to a macOS .app's Contents/MacOS/ (i.e.
// "../Resources/..." or "../Frameworks/..." reaches Contents/Resources or
// Contents/Frameworks -- see build_macos.sh, which now populates both with
// fetch_onnxruntime.sh's redistributable runtime lib and the ONNX models
// already committed under internal/localui/models/). flatRel is checked
// directly next to the executable, for a flat layout (Linux AppImage,
// Windows install dir) once those build scripts grow the same bundling
// step. fallback is the pre-existing ~/.usbridge/localui/<sub> path, used
// as-is if neither candidate exists on disk -- e.g. a `go run` dev build
// with no bundle around it at all, where setup_localui.sh's manual flow
// still applies.
//
// Mirrors the "check next to the executable first" pattern
// h264_decoder.go's findFFmpeg already uses for the bundled ffmpeg binary.
func resolveLocalUIPath(bundleRel, flatRel, fallback string) string {
	execPath, err := os.Executable()
	if err == nil {
		execDir := filepath.Dir(execPath)
		for _, rel := range []string{bundleRel, flatRel} {
			candidate := filepath.Join(execDir, rel)
			if _, statErr := os.Stat(candidate); statErr == nil {
				return candidate
			}
		}
	}
	return fallback
}

// cudaRuntimeLib returns ~/.usbridge/localui/runtime-cuda/<libName> when the
// CUDA pack is installed (scripts/fetch_onnxruntime_cuda.sh) and an NVIDIA
// driver is loaded on this machine, "" otherwise. Linux only: the pack is an
// onnxruntime-gpu build plus CUDA/cuDNN libs, and its CUDA EP is what makes
// dbnet/svtr run on NVIDIA -- the OpenVINO GPU plugin in the default runtime
// is Intel-only.
func cudaRuntimeLib(libName string) string {
	if runtime.GOOS != "linux" {
		return ""
	}
	if _, err := os.Stat("/proc/driver/nvidia/version"); err != nil {
		return ""
	}
	dir := defaultLocalUIDir("runtime-cuda")
	lib := filepath.Join(dir, libName)
	if _, err := os.Stat(lib); err != nil {
		return ""
	}
	if _, err := os.Stat(filepath.Join(dir, "libonnxruntime_providers_cuda.so")); err != nil {
		return ""
	}
	return lib
}
