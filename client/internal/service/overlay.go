// Package service: creating a qcow2 overlay for read-write disk exports
// without corrupting the base image. Transport-agnostic (used by the iSCSI
// target). Desktop only (not Android); requires qemu-img in PATH.

package service

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/sirupsen/logrus"
)

const overlaySuffix = ".overlay.qcow2"

// backingFormat returns the backing file format for qemu-img by extension.
func backingFormat(ext string) string {
	switch strings.ToLower(ext) {
	case ".qcow2", ".qcow":
		return "qcow2"
	case ".vmdk":
		return "vmdk"
	case ".vdi":
		return "vdi"
	case ".raw", ".img", ".vmi":
		return "raw"
	default:
		return "raw"
	}
}

// IsOverlayCapableExtension returns true if an overlay can be created for the format (qcow2 with backing).
// Used in UI to show RO/RW switch for vdi, vmdk, qcow2, etc. images.
func IsOverlayCapableExtension(ext string) bool {
	switch strings.ToLower(ext) {
	case ".qcow2", ".qcow", ".vmdk", ".vdi", ".raw", ".img", ".vmi":
		return true
	default:
		return false
	}
}

// lookQemuTool finds a QEMU binary by name. Checks PATH first, then common
// installation prefixes (Homebrew on macOS, /usr/local on Linux) so the app
// works even when launched outside a full shell environment.
func lookQemuTool(name string) string {
	if path, err := exec.LookPath(name); err == nil {
		return path
	}
	for _, prefix := range []string{"/opt/homebrew/bin", "/usr/local/bin", "/usr/bin"} {
		candidate := prefix + "/" + name
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return ""
}

// qemuImgPath looks for qemu-img in PATH and known prefixes.
func qemuImgPath() string {
	return lookQemuTool("qemu-img")
}

// qemuImgVirtualSize returns the virtual size of the image in bytes (qemu-img info --output=json).
func qemuImgVirtualSize(imagePath string) (int64, error) {
	qemuImg := qemuImgPath()
	if qemuImg == "" {
		return 0, fmt.Errorf("qemu-img not found in PATH")
	}
	cmd := exec.Command(qemuImg, "info", "--output=json", imagePath)
	maybeHideWindow(cmd)
	cmd.Env = os.Environ()
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return 0, fmt.Errorf("qemu-img info: %s: %w", string(ee.Stderr), err)
		}
		return 0, fmt.Errorf("qemu-img info: %w", err)
	}
	var info struct {
		VirtualSize int64 `json:"virtual-size"`
	}
	if err := json.Unmarshal(out, &info); err != nil {
		return 0, fmt.Errorf("parsing qemu-img info: %w", err)
	}
	if info.VirtualSize <= 0 {
		return 0, fmt.Errorf("qemu-img info: virtual-size not found or 0")
	}
	return info.VirtualSize, nil
}

// createOverlay creates a qcow2 overlay with backing on basePath (next to base: base.overlay.qcow2).
// Returns the path to the overlay and virtual size. Desktop only; basePath must be absolute.
func createOverlay(basePath string) (overlayPath string, virtualSize int64, err error) {
	qemuImg := qemuImgPath()
	if qemuImg == "" {
		return "", 0, fmt.Errorf("qemu-img not found in PATH: for RW-export via overlay, install QEMU")
	}
	baseAbs, err := filepath.Abs(basePath)
	if err != nil {
		return "", 0, fmt.Errorf("absolute base path: %w", err)
	}
	ext := strings.ToLower(filepath.Ext(baseAbs))
	if !IsOverlayCapableExtension(ext) {
		return "", 0, fmt.Errorf("format %s does not support overlay", ext)
	}
	dir := filepath.Dir(baseAbs)
	baseName := strings.TrimSuffix(filepath.Base(baseAbs), filepath.Ext(baseAbs))
	// One overlay per image: baseName.overlay.qcow2 - on restart we use the already created one
	overlayPath = filepath.Join(dir, baseName+overlaySuffix)
	if _, err := os.Stat(overlayPath); err == nil {
		virtualSize, err = qemuImgVirtualSize(overlayPath)
		if err != nil {
			return "", 0, fmt.Errorf("getting size of existing overlay: %w", err)
		}
		logrus.Infof("✅ [OVERLAY] Using existing overlay %s (backing %s)", overlayPath, baseAbs)
		return overlayPath, virtualSize, nil
	}
	format := backingFormat(ext)
	// qemu-img create -f qcow2 -b <base> -F <format> <overlay>
	cmd := exec.Command(qemuImg, "create", "-f", "qcow2", "-b", baseAbs, "-F", format, overlayPath)
	maybeHideWindow(cmd)
	cmd.Env = os.Environ()
	if out, err := cmd.CombinedOutput(); err != nil {
		// We do not delete Overlay even on error - do not touch existing files
		return "", 0, fmt.Errorf("qemu-img create overlay: %s: %w", string(out), err)
	}
	logrus.Infof("✅ [OVERLAY] Overlay created %s (backing %s, format %s)", overlayPath, baseAbs, format)
	virtualSize, err = qemuImgVirtualSize(overlayPath)
	if err != nil {
		// We do not delete Overlay - leave the created file
		return "", 0, fmt.Errorf("getting size of overlay: %w", err)
	}
	return overlayPath, virtualSize, nil
}
