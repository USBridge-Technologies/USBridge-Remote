package main

import (
	"os"
	"os/exec"
	"path/filepath"
)

func main() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	dir := filepath.Dir(exe)

	// Prepend lib\ to PATH so the Windows PE loader finds all runtime DLLs.
	os.Setenv("PATH", filepath.Join(dir, "lib")+";"+os.Getenv("PATH"))

	cmd := exec.Command(filepath.Join(dir, "bin", "USBridge_Client_app.exe"), os.Args[1:]...)
	cmd.Env = os.Environ()
	cmd.Start() //nolint:errcheck // launcher exits immediately; app runs independently
}
