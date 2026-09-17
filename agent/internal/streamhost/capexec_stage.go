package streamhost

import (
	"os"
	"path/filepath"
)

// sharedCapExecRuntimeDir is the stateDir subdirectory both sunshineBackend
// and rustshineBackend stage cmd/sunshine_capexec's writable copy into (see
// stageCapExecBinary). It's the identical launcher binary either way, so
// both backends deliberately share one staged file/inode: setcap grants
// CAP_SYS_ADMIN to a specific inode, and staging each backend into its own
// directory used to mean a grant made while one backend was active never
// applied to the other — see rustshineBackend.runtimeCapExecPath's doc
// comment for the confirmed symptom.
const sharedCapExecRuntimeDir = "capexec-runtime"

// stageCapExecBinary copies the bundled sunshine-capexec launcher (a single
// static file — cmd/sunshine_capexec) from src into destDir/sunshine-capexec,
// skipping the copy if a matching one (by size + mtime) is already staged
// there. Shared by sunshineBackend and rustshineBackend's runtimeCapExecPath:
// both need a writable copy to setcap when the bundled original lives on a
// read-only AppImage squashfs mount.
func stageCapExecBinary(src, destDir string) (string, error) {
	dst := filepath.Join(destDir, "sunshine-capexec")

	srcInfo, err := os.Stat(src)
	if err != nil {
		return "", err
	}
	if dstInfo, err := os.Stat(dst); err == nil &&
		dstInfo.Size() == srcInfo.Size() && dstInfo.ModTime().Equal(srcInfo.ModTime()) {
		return dst, nil
	}

	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", err
	}
	tmp := dst + ".tmp"
	if err := copyFile(src, tmp, srcInfo.Mode()); err != nil {
		return "", err
	}
	if err := os.Chtimes(tmp, srcInfo.ModTime(), srcInfo.ModTime()); err != nil {
		os.Remove(tmp)
		return "", err
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		return "", err
	}
	return dst, nil
}
