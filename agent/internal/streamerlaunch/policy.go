//go:build linux

package streamerlaunch

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// SafePath replaces the caller's PATH: the streamer runs with CAP_SYS_ADMIN,
// so it must never resolve a helper out of a user-writable directory.
const SafePath = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"

// allowedEnv is the allowlist of variables passed through to the streamer.
// An allowlist, not a blocklist: a process running with an ambient
// capability is NOT in glibc's secure-execution mode (that only happens
// for setuid/file-capability binaries), so LD_PRELOAD, LD_LIBRARY_PATH,
// GCONV_PATH, LIBVA_DRIVERS_PATH, GBM_BACKENDS_PATH, VK_LAYER_PATH,
// VK_ICD_FILENAMES, SPA_PLUGIN_DIR, ... would all still be honored and
// would each let the caller load its own code into a CAP_SYS_ADMIN
// process. Only session-locating and plain-value variables survive.
var allowedEnv = map[string]bool{
	"HOME": true, "USER": true, "LOGNAME": true, "SHELL": true,
	"LANG": true, "LANGUAGE": true, "TZ": true,
	"XDG_RUNTIME_DIR": true, "XDG_SESSION_TYPE": true, "XDG_SESSION_ID": true,
	"XDG_CURRENT_DESKTOP": true, "XDG_SESSION_DESKTOP": true, "XDG_SEAT": true,
	"WAYLAND_DISPLAY": true, "DISPLAY": true, "XAUTHORITY": true,
	"DBUS_SESSION_BUS_ADDRESS": true,
	"RUST_LOG":                 true, "RUST_BACKTRACE": true,
	// rust-shine's own plain-value tuning knobs (grep env::var in rust-shine).
	"SKIP_NVML": true, "CODEC": true, "CAPTURE_SYNC": true,
	"LIBVA_DRIVER_NAME": true, // a driver name, not a path
}

// SanitizedEnv filters env down to allowedEnv (plus LC_* and USBRIDGE_*),
// forces PATH to SafePath, and points XDG_DATA_HOME (and XDG_CONFIG_HOME
// unless keepConfigHome -- Sunshine keeps its appdata there and refuses to
// start without it) at the root-owned EmptyDir: the Vulkan loader loads "implicit layers" (shared
// libraries) named by JSON files under $XDG_DATA_HOME/vulkan and
// $XDG_CONFIG_HOME/vulkan (defaulting to ~/.local/share and ~/.config),
// and it only skips those for setuid-style processes -- so without this a
// JSON file in the user's home would still inject code. XDG_*_DIRS are
// pinned to the system defaults for the same reason. PULSE_COOKIE keeps
// PulseAudio auth working despite the moved config dir.
func SanitizedEnv(env []string, keepConfigHome bool) []string {
	get := func(k string) string {
		for _, kv := range env {
			if strings.HasPrefix(kv, k+"=") {
				return kv[len(k)+1:]
			}
		}
		return ""
	}
	out := make([]string, 0, len(env))
	for _, kv := range env {
		k, _, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		if allowedEnv[k] || strings.HasPrefix(k, "LC_") || strings.HasPrefix(k, "USBRIDGE_") {
			out = append(out, kv)
		}
	}

	cfgHome := get("XDG_CONFIG_HOME")
	if cfgHome == "" && get("HOME") != "" {
		cfgHome = filepath.Join(get("HOME"), ".config")
	}
	if cookie := filepath.Join(cfgHome, "pulse", "cookie"); cfgHome != "" {
		if st, err := os.Stat(cookie); err == nil && st.Mode().IsRegular() {
			out = append(out, "PULSE_COOKIE="+cookie)
		}
	}
	// Vulkan: pin the ICD list to the root-owned system manifests and turn
	// implicit layers off, so neither can come from the user's config or
	// data dirs even where those stay reachable (keepConfigHome).
	// VK_ICD_FILENAMES is the pre-1.3.207 loader's name for VK_DRIVER_FILES.
	icds := systemVulkanICDs()
	out = append(out,
		"VK_DRIVER_FILES="+icds,
		"VK_ICD_FILENAMES="+icds,
		"VK_LOADER_LAYERS_DISABLE=~implicit~",
		"PATH="+SafePath,
		"XDG_DATA_HOME="+EmptyDir,
		"XDG_CONFIG_DIRS=/etc/xdg",
		"XDG_DATA_DIRS=/usr/local/share:/usr/share",
	)
	if keepConfigHome {
		if cfgHome != "" {
			out = append(out, "XDG_CONFIG_HOME="+cfgHome)
		}
	} else {
		out = append(out, "XDG_CONFIG_HOME="+EmptyDir)
	}
	return out
}

// systemVulkanICDs lists the Vulkan driver manifests in the standard
// root-owned directories, colon-separated -- or a nonexistent path when
// there are none, so the loader never falls back to searching home dirs.
func systemVulkanICDs() string {
	var files []string
	for _, dir := range []string{"/usr/share/vulkan/icd.d", "/usr/local/share/vulkan/icd.d", "/etc/vulkan/icd.d"} {
		if CheckRootOwned(dir) != nil {
			continue
		}
		matches, _ := filepath.Glob(filepath.Join(dir, "*.json"))
		files = append(files, matches...)
	}
	if len(files) == 0 {
		return EmptyDir + "/no-vulkan-driver.json"
	}
	return strings.Join(files, ":")
}

// CheckRootOwned fails unless p is owned by root and not writable by group
// or others -- the launcher's precondition for trusting InstallDir's
// contents (a directory or file the user could write would let them edit
// the allowlist).
func CheckRootOwned(p string) error {
	st, err := os.Lstat(p)
	if err != nil {
		return err
	}
	sys, ok := st.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("%s: no ownership info", p)
	}
	if sys.Uid != 0 {
		return fmt.Errorf("%s is not owned by root", p)
	}
	if st.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s is a symlink", p)
	}
	if st.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("%s is group/world-writable", p)
	}
	return nil
}

// UIDAllowed reports whether uid appears in the allowlist file at p (one
// decimal uid per line), after checking p and its directory are
// root-owned. The allowlist exists so another local account can't use the
// launcher to KMS-capture the screen of whoever is logged in on the
// console; the one-time install adds the granting user's uid.
func UIDAllowed(p string, uid int) error {
	if err := CheckRootOwned(filepath.Dir(p)); err != nil {
		return err
	}
	if err := CheckRootOwned(p); err != nil {
		return err
	}
	b, err := readLimited(p, 64<<10)
	if err != nil {
		return err
	}
	for _, line := range strings.Split(string(b), "\n") {
		if n, err := strconv.Atoi(strings.TrimSpace(line)); err == nil && n == uid {
			return nil
		}
	}
	return fmt.Errorf("uid %d is not in %s (re-grant screen capture from this account)", uid, p)
}

// CheckTreeRootOwned walks root and fails on the first entry that isn't
// root-owned and non-group/world-writable (see CheckRootOwned). Symlinks
// are allowed inside the tree -- linuxdeploy's usr/lib uses soname links --
// but only ones that resolve to a checked path inside root.
func CheckTreeRootOwned(root string) error {
	if err := CheckRootOwned(filepath.Dir(root)); err != nil {
		return err
	}
	return filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		st, err := os.Lstat(p)
		if err != nil {
			return err
		}
		sys, ok := st.Sys().(*syscall.Stat_t)
		if !ok || sys.Uid != 0 {
			return fmt.Errorf("%s is not owned by root", p)
		}
		if st.Mode()&os.ModeSymlink != 0 {
			target, err := filepath.EvalSymlinks(p)
			if err != nil {
				return err
			}
			if rel, err := filepath.Rel(root, target); err != nil || rel == ".." || strings.HasPrefix(rel, "../") {
				return fmt.Errorf("%s points outside %s", p, root)
			}
			return nil
		}
		if st.Mode().Perm()&0o022 != 0 {
			return fmt.Errorf("%s is group/world-writable", p)
		}
		return nil
	})
}
