//go:build !linux && !darwin

package util

import "os"

// Fdatasync: no fdatasync-equivalent on Windows and other non-Unix
// platforms; plain Sync() is the closest available.
func Fdatasync(file *os.File) error {
	return file.Sync()
}

// Fadvise: posix_fadvise has no Windows/other equivalent; no-op (see
// util_linux.go for why this isn't in the shared util.go anymore).
func Fadvise(file *os.File, off, length int64, advise uint32) error {
	return nil
}
