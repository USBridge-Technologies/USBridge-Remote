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

	// Tell GStreamer to load plugins from the bundled lib\gstreamer-1.0\ folder,
	// not from the MSYS2 system path baked into the DLLs at compile time.
	gstPluginDir := filepath.Join(dir, "lib", "gstreamer-1.0")
	os.Setenv("GST_PLUGIN_PATH_1_0", gstPluginDir)
	os.Setenv("GST_PLUGIN_SYSTEM_PATH_1_0", gstPluginDir)
	os.Setenv("GST_REGISTRY_1_0", filepath.Join(dir, "lib", ".gstreamer-registry.bin"))

	cmd := exec.Command(filepath.Join(dir, "bin", "USBridge_Client_app.exe"), os.Args[1:]...)
	cmd.Env = os.Environ()
	cmd.Start() //nolint:errcheck // launcher exits immediately; app runs independently
}
