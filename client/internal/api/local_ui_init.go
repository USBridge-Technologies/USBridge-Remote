package api

import (
	"log"
	"os"
	"path/filepath"

	"usbridge-client/internal/localui"
	"usbridge-client/internal/models"
)

// InitLocalUIParseFromConfig builds and installs the local ui.parse backend
// (see local_ui_intercept.go) if cfg.LocalUIParseEnabled -- called once at
// startup. Loading the ONNX models is relatively expensive (hundreds of ms
// to a few seconds), so this runs in the background and installs itself
// via SetLocalUIParser only once ready; until then, ui.parse calls forward
// to the device as normal. A failure here (models/runtime lib missing,
// bad path, etc.) is logged and otherwise harmless -- ui.parse just keeps
// going to the device, matching the "optional accelerator, never a hard
// dependency" pattern used throughout this feature.
func InitLocalUIParseFromConfig(cfg *models.AppConfig) {
	if cfg == nil || !cfg.LocalUIParseEnabled {
		return
	}
	go func() {
		modelDir := cfg.LocalUIParseModelDir
		if modelDir == "" {
			modelDir = resolveLocalUIPath(
				filepath.Join("..", "Resources", "localui", "models"), // macOS .app: Contents/MacOS/../Resources/localui/models
				filepath.Join("localui", "models"),                    // flat layout: next to the executable
				defaultLocalUIDir("models"),
			)
		}
		ortLib := cfg.LocalUIParseORTLib
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
