//go:build linux

package app

import (
	"io/fs"
	"os"
	"path/filepath"
)

// fyneAppIDLegacy is the agent's Fyne ID everywhere else, and on Linux
// before native Wayland.
const fyneAppIDLegacy = "io.usbridge.agent"

// fyneAppIDLinux matches usbridge-agent.desktop. On Wayland the window's
// app_id is the Fyne ID, and the compositor finds the title-bar / task
// manager icon through the .desktop file of that name -- with the legacy ID
// KWin showed its generic Wayland icon.
const fyneAppIDLinux = "usbridge-agent"

// fyneAppID returns the Linux ID, first carrying the settings Fyne kept
// under the legacy ID over (~/.config/fyne/<id>; the old directory stays,
// so an older build still finds its own).
func fyneAppID() string {
	if cfg, err := os.UserConfigDir(); err == nil {
		oldDir := filepath.Join(cfg, "fyne", fyneAppIDLegacy)
		newDir := filepath.Join(cfg, "fyne", fyneAppIDLinux)
		if _, err := os.Stat(newDir); os.IsNotExist(err) {
			if st, err := os.Stat(oldDir); err == nil && st.IsDir() {
				_ = copyDir(oldDir, newDir)
			}
		}
	}
	return fyneAppIDLinux
}

// copyDir copies the regular files and directories under src to dst.
func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o600)
	})
}
