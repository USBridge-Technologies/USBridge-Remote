//go:build !android

package gui

import "errors"

// Only Android has a system share sheet and a public Downloads folder to
// copy into; elsewhere the native save dialog covers saving.
func fileExportAvailable() bool { return false }

func saveFileToDownloads(string, string, string) (string, error) {
	return "", errors.New("not supported")
}

func shareFile(string, string, string) error { return errors.New("not supported") }
