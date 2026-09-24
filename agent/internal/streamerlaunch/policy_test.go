//go:build linux

package streamerlaunch

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestSanitizedEnv(t *testing.T) {
	home := t.TempDir()
	os.MkdirAll(filepath.Join(home, ".config", "pulse"), 0o755)
	os.WriteFile(filepath.Join(home, ".config", "pulse", "cookie"), []byte("c"), 0o600)
	in := []string{
		"HOME=" + home, "WAYLAND_DISPLAY=wayland-0", "XDG_RUNTIME_DIR=/run/user/1000",
		"LC_ALL=C", "USBRIDGE_VA_TU=1", "RUST_LOG=info",
		"LD_PRELOAD=/tmp/evil.so", "LD_LIBRARY_PATH=/tmp", "GCONV_PATH=/tmp",
		"VK_LAYER_PATH=/tmp", "VK_ICD_FILENAMES=/tmp/x.json", "LIBVA_DRIVERS_PATH=/tmp",
		"GBM_BACKENDS_PATH=/tmp", "SPA_PLUGIN_DIR=/tmp", "PATH=/tmp/bin:/usr/bin",
		"XDG_CONFIG_HOME=/tmp/cfg", "XDG_DATA_HOME=/tmp/data", "XDG_DATA_DIRS=/tmp",
	}
	got := map[string]string{}
	for _, kv := range SanitizedEnv(in, false) {
		k, v, _ := strings.Cut(kv, "=")
		if _, dup := got[k]; dup {
			t.Fatalf("duplicate %s", k)
		}
		got[k] = v
	}
	for _, k := range []string{"LD_PRELOAD", "LD_LIBRARY_PATH", "GCONV_PATH", "VK_LAYER_PATH", "LIBVA_DRIVERS_PATH", "GBM_BACKENDS_PATH", "SPA_PLUGIN_DIR"} {
		if _, ok := got[k]; ok {
			t.Errorf("%s passed through", k)
		}
	}
	want := map[string]string{
		"PATH": SafePath, "XDG_CONFIG_HOME": EmptyDir, "XDG_DATA_HOME": EmptyDir,
		"XDG_DATA_DIRS": "/usr/local/share:/usr/share", "WAYLAND_DISPLAY": "wayland-0",
		"LC_ALL": "C", "USBRIDGE_VA_TU": "1", "RUST_LOG": "info", "HOME": home,
		"VK_LOADER_LAYERS_DISABLE": "~implicit~",
	}
	for _, k := range []string{"VK_DRIVER_FILES", "VK_ICD_FILENAMES"} {
		if strings.Contains(got[k], "/tmp") || got[k] == "" {
			t.Errorf("%s = %q, want the system ICD list", k, got[k])
		}
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
	// The cookie is resolved against the caller's XDG_CONFIG_HOME
	// (/tmp/cfg here, which has none), not HOME.
	if _, ok := got["PULSE_COOKIE"]; ok {
		t.Errorf("PULSE_COOKIE set from a dir without a cookie")
	}
	got2 := SanitizedEnv([]string{"HOME=" + home}, false)
	found := false
	for _, kv := range got2 {
		if kv == "PULSE_COOKIE="+filepath.Join(home, ".config", "pulse", "cookie") {
			found = true
		}
	}
	if !found {
		t.Errorf("PULSE_COOKIE not carried over: %v", got2)
	}
}

func TestUIDAllowed_RejectsUserOwnedAllowlist(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root")
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "allowed-uids")
	os.WriteFile(p, []byte("0\n1000\n"+strconv.Itoa(os.Getuid())+"\n"), 0o644)
	if err := UIDAllowed(p, os.Getuid()); err == nil {
		t.Fatal("trusted an allowlist the user can write")
	}
	if err := CheckTreeRootOwned(dir); err == nil {
		t.Fatal("trusted a user-owned tree")
	}
}

func TestCheckRootOwned_SystemPaths(t *testing.T) {
	if err := CheckRootOwned("/usr/bin"); err != nil {
		t.Fatalf("/usr/bin: %v", err)
	}
	if err := CheckRootOwned("/tmp"); err == nil {
		t.Fatal("/tmp is world-writable but was accepted")
	}
}

func TestSanitizedEnv_KeepConfigHome(t *testing.T) {
	for _, kv := range SanitizedEnv([]string{"HOME=/home/u"}, true) {
		if kv == "XDG_CONFIG_HOME="+EmptyDir {
			t.Fatal("config home redirected despite keepConfigHome")
		}
		if kv == "XDG_DATA_HOME=/home/u/.local/share" {
			t.Fatal("data home kept")
		}
	}
	found := false
	for _, kv := range SanitizedEnv([]string{"HOME=/home/u"}, true) {
		found = found || kv == "XDG_CONFIG_HOME=/home/u/.config"
	}
	if !found {
		t.Fatal("XDG_CONFIG_HOME not defaulted to ~/.config")
	}
}
