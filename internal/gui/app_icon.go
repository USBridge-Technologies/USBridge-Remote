package gui

import (
	"os"
	"path/filepath"

	"fyne.io/fyne/v2"
	"github.com/sirupsen/logrus"
)

func loadAppIconResource() fyne.Resource {
	for _, candidate := range appIconCandidates() {
		resource, err := fyne.LoadResourceFromPath(candidate)
		if err == nil {
			return resource
		}
	}

	logrus.Warn("app icon not found in assets/icons/Icon.png")
	return nil
}

func appIconCandidates() []string {
	candidates := make([]string, 0, 6)

	if exePath, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exePath)
		candidates = append(candidates,
			filepath.Join(exeDir, "assets", "icons", "Icon.png"),
			filepath.Join(exeDir, "Icon.png"),
		)
	}

	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates,
			filepath.Join(wd, "assets", "icons", "Icon.png"),
			filepath.Join(wd, "..", "assets", "icons", "Icon.png"),
			filepath.Join(wd, "Icon.png"),
			filepath.Join(wd, "..", "Icon.png"),
		)
	}

	return candidates
}
