//go:build windows

package update

import (
	"archive/zip"
	"context"
	_ "embed"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

//go:embed assets/update_helper.ps1
var updateHelperScript []byte

// createNoWindow (Windows CREATE_NO_WINDOW) keeps the detached PowerShell
// helper from flashing a console window.
const createNoWindow = 0x08000000

// apply unpacks the downloaded, already SHA-256-verified update zip into a
// staging directory, then hands off the actual file swap to a detached
// PowerShell helper (see assets/update_helper.ps1) — Windows won't let a
// running process overwrite its own loaded exe/DLLs, so the swap has to
// happen from a process that isn't this one, after this one has exited.
func apply(ctx context.Context, artifactPath, version string) error {
	defer os.Remove(artifactPath)

	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve current executable: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(exePath); err == nil {
		exePath = resolved
	}
	exeDir := filepath.Dir(exePath)

	// build_windows.sh places the exe (+ DLLs) in a "bin" subfolder of the
	// portable install root, while the release zip mirrors that whole root
	// (bin/, README.txt, config.yaml, the .lnk shortcut — see
	// DIST_WIN/DIST_WIN_BIN there). If the update were extracted straight
	// into exeDir (== .../bin), it would land one level too deep
	// (.../bin/bin/...) and the exe actually being run would never get
	// overwritten — the update would silently no-op on relaunch. Install
	// into the parent of "bin" instead, so the staging dir's layout lines
	// up with the zip's.
	installDir := exeDir
	if filepath.Base(exeDir) == "bin" {
		installDir = filepath.Dir(exeDir)
	}
	exeName, err := filepath.Rel(installDir, exePath)
	if err != nil {
		exeName = filepath.Base(exePath)
	}

	stagingDir, err := os.MkdirTemp("", "usbridge-client-update-*")
	if err != nil {
		return err
	}
	if err := unzip(artifactPath, stagingDir); err != nil {
		os.RemoveAll(stagingDir)
		return fmt.Errorf("extract update archive: %w", err)
	}

	helperPath := filepath.Join(stagingDir, ".update_helper.ps1")
	if err := os.WriteFile(helperPath, updateHelperScript, 0o644); err != nil {
		os.RemoveAll(stagingDir)
		return fmt.Errorf("write update helper: %w", err)
	}

	cmd := exec.Command("powershell.exe",
		"-NoProfile", "-ExecutionPolicy", "Bypass", "-WindowStyle", "Hidden", "-File", helperPath,
		"-ParentPid", strconv.Itoa(os.Getpid()),
		"-StagingDir", stagingDir,
		"-InstallDir", installDir,
		"-ExeName", exeName,
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNoWindow}
	if err := cmd.Start(); err != nil {
		os.RemoveAll(stagingDir)
		return fmt.Errorf("launch update helper: %w", err)
	}

	os.Exit(0)
	return nil // unreachable
}

// unzip extracts every entry in src into destDir, guarding against
// zip-slip (entries whose name escapes destDir via "..").
func unzip(src, destDir string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		target := filepath.Join(destDir, f.Name)
		if !strings.HasPrefix(target, filepath.Clean(destDir)+string(os.PathSeparator)) && target != filepath.Clean(destDir) {
			return fmt.Errorf("zip entry %q escapes destination directory", f.Name)
		}

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}

		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			rc.Close()
			return err
		}
		_, copyErr := io.Copy(out, rc)
		rc.Close()
		closeErr := out.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}
