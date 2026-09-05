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
			modelDir = defaultLocalUIDir("models")
		}
		ortLib := cfg.LocalUIParseORTLib
		if ortLib == "" {
			ortLib = filepath.Join(defaultLocalUIDir("runtime"), "libonnxruntime.so")
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
