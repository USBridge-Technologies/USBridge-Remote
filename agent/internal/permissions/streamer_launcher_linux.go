//go:build linux

package permissions

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"usbridge_agent/internal/streamerlaunch"
)

// launcherBinaryName is cmd/usbridge_streamer_launch's build output name
// (see scripts/build_linux.sh), shipped next to the agent binary.
const launcherBinaryName = "usbridge-streamer-launch"

// bundledStreamerLauncher finds the launcher shipped with this agent build:
// next to the running executable (AppImage usr/bin, or dist/ for a dev
// build), then $APPDIR/usr/bin.
func bundledStreamerLauncher() string {
	var candidates []string
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(exe), launcherBinaryName))
	}
	if appdir := os.Getenv("APPDIR"); appdir != "" {
		candidates = append(candidates, filepath.Join(appdir, "usr", "bin", launcherBinaryName))
	}
	for _, p := range candidates {
		if st, err := os.Stat(p); err == nil && st.Mode().IsRegular() {
			return p
		}
	}
	return ""
}

// installedLauncherProtocol runs the installed launcher's --version and
// returns its protocol number, or 0 if it can't be determined.
func installedLauncherProtocol() int {
	out, err := exec.Command(streamerlaunch.InstallPath, "--version").Output()
	if err != nil {
		return 0
	}
	_, v, ok := strings.Cut(strings.TrimSpace(string(out)), "protocol=")
	if !ok {
		return 0
	}
	n, _ := strconv.Atoi(v)
	return n
}

// StreamerLauncherInstalled reports whether the one-time install is in
// place and current: root-owned, carrying cap_sys_admin, new enough for
// this agent, and allowing the current user. Unlike a setcap on the
// streamer itself, none of that changes when the streamer updates.
func (s *Service) StreamerLauncherInstalled() bool {
	if err := streamerlaunch.CheckRootOwned(streamerlaunch.InstallPath); err != nil {
		return false
	}
	out, err := exec.Command(findCapTool("getcap"), streamerlaunch.InstallPath).CombinedOutput()
	if err != nil || !strings.Contains(string(out), "cap_sys_admin") {
		return false
	}
	if installedLauncherProtocol() < streamerlaunch.MinProtocol {
		return false
	}
	return streamerlaunch.UIDAllowed(streamerlaunch.AllowedUIDsPath, os.Getuid()) == nil
}

// StreamerLauncherVerify asks the installed launcher itself (the exact code
// that will exec the streamer) whether bundleDir verifies and whether its
// capability is actually effective. Returns the bundle's release tag.
func (s *Service) StreamerLauncherVerify(bundleDir string) (version string, err error) {
	out, err := exec.Command(streamerlaunch.InstallPath, "--verify", bundleDir).CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		return "", fmt.Errorf("%v: %s", err, text)
	}
	capOK := false
	for _, line := range strings.Split(text, "\n") {
		if v, ok := strings.CutPrefix(line, "version="); ok {
			version = v
		}
		if line == "cap=1" {
			capOK = true
		}
	}
	if !capOK {
		return version, fmt.Errorf("launcher did not receive CAP_SYS_ADMIN (nosuid mount or no_new_privs?)")
	}
	return version, nil
}

// buildStreamerLauncherInstallScript builds the /bin/sh -c script
// InstallStreamerLauncher runs under pkexec. The source is first copied
// into the root-owned target directory and only then hash-checked against
// wantSHA256, so a user-writable source swapped mid-install can never end
// up carrying the capability. Pure function so its shape is testable
// without root.
func buildStreamerLauncherInstallScript(src, wantSHA256, setcap string, uid int) string {
	q := shellQuoteUsername
	dir := streamerlaunch.InstallDir
	tmp := dir + "/.usbridge-streamer-launch.tmp"
	return strings.Join([]string{
		"set -e",
		"umask 022",
		"install -d -o root -g root -m 0755 " + q(dir),
		"install -d -o root -g root -m 0755 " + q(streamerlaunch.EmptyDir),
		"install -o root -g root -m 0755 " + q(src) + " " + q(tmp),
		"echo " + q(wantSHA256+"  "+tmp) + " | sha256sum -c --status",
		q(setcap) + " cap_sys_admin=ep " + q(tmp),
		"mv -f " + q(tmp) + " " + q(streamerlaunch.InstallPath),
		"touch " + q(streamerlaunch.AllowedUIDsPath),
		"chown root:root " + q(streamerlaunch.AllowedUIDsPath),
		"chmod 0644 " + q(streamerlaunch.AllowedUIDsPath),
		fmt.Sprintf("grep -qx %d %s || echo %d >> %s", uid, q(streamerlaunch.AllowedUIDsPath), uid, q(streamerlaunch.AllowedUIDsPath)),
	}, "\n")
}

// InstallStreamerLauncher performs the one-time, password-prompted install
// of the bundled launcher at streamerlaunch.InstallPath. After this,
// RustShine updates never need another grant.
func (s *Service) InstallStreamerLauncher() bool {
	if s.StreamerLauncherInstalled() && installedLauncherProtocol() >= streamerlaunch.Protocol {
		return true
	}
	src := bundledStreamerLauncher()
	if src == "" {
		s.lastAccessErr = "this build does not include usbridge-streamer-launch"
		log.Printf("[permissions] %s", s.lastAccessErr)
		return false
	}
	// Stage a plain copy first: inside an AppImage the source sits on the
	// user's FUSE mount, which root (pkexec) cannot read.
	staged, sum, err := copyAndHash(src)
	if err != nil {
		s.lastAccessErr = fmt.Sprintf("stage launcher: %v", err)
		log.Printf("[permissions] %s", s.lastAccessErr)
		return false
	}
	defer os.Remove(staged)

	script := buildStreamerLauncherInstallScript(staged, sum, findCapTool("setcap"), os.Getuid())
	out, err := exec.Command("pkexec", "/bin/sh", "-c", script).CombinedOutput()
	log.Printf("[permissions] install streamer launcher pkexec exit=%v output=%q", err, string(out))
	if err != nil {
		s.lastAccessErr = fmt.Sprintf("pkexec failed: %v (%s)", err, strings.TrimSpace(string(out)))
		return false
	}
	return s.StreamerLauncherInstalled()
}

func copyAndHash(src string) (path, sum string, err error) {
	in, err := os.Open(src)
	if err != nil {
		return "", "", err
	}
	defer in.Close()
	out, err := os.CreateTemp("", "usbridge-streamer-launch-*")
	if err != nil {
		return "", "", err
	}
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(out, h), in); err != nil {
		out.Close()
		os.Remove(out.Name())
		return "", "", err
	}
	if err := out.Close(); err != nil {
		os.Remove(out.Name())
		return "", "", err
	}
	if err := os.Chmod(out.Name(), 0o755); err != nil {
		os.Remove(out.Name())
		return "", "", err
	}
	return out.Name(), hex.EncodeToString(h.Sum(nil)), nil
}

// isSunshineTree reports whether p is a Sunshine install-tree root (the
// KMS target sunshineBackend.CapExecPath returns), as opposed to
// RustShine's streamerlaunch.InstallPath.
func isSunshineTree(p string) bool {
	st, err := os.Stat(filepath.Join(p, "usr", "bin", "sunshine"))
	return err == nil && st.Mode().IsRegular()
}

// sunshineIDCache memoizes fileSHA256 of a source usr/bin/sunshine by
// (path, size, mtime) -- KMSCaptureGranted is polled by the UI and the
// binary is ~25MB.
var sunshineIDCache struct {
	key string
	sum string
}

func sunshineSourceID(srcRoot string) string {
	bin := filepath.Join(srcRoot, "usr", "bin", "sunshine")
	st, err := os.Stat(bin)
	if err != nil {
		return ""
	}
	key := fmt.Sprintf("%s|%d|%d", bin, st.Size(), st.ModTime().UnixNano())
	if sunshineIDCache.key == key {
		return sunshineIDCache.sum
	}
	sum, err := fileSHA256(bin)
	if err != nil {
		return ""
	}
	sunshineIDCache.key, sunshineIDCache.sum = key, sum
	return sum
}

func fileSHA256(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// SunshineLaunchReady reports whether `usbridge-streamer-launch
// --run-sunshine` can run right now: launcher installed and the root-owned
// Sunshine tree present. Deliberately NOT tied to the tree matching the
// currently bundled Sunshine -- after an agent update ships a newer
// Sunshine, the previously installed one keeps KMS capture (and the remote
// session) alive until the user refreshes the grant locally.
func (s *Service) SunshineLaunchReady() bool {
	return s.StreamerLauncherInstalled() && streamerlaunch.CheckTreeRootOwned(streamerlaunch.SunshineDir) == nil
}

// sunshineTreeCurrent reports whether the installed root-owned tree was
// installed from srcRoot's exact Sunshine build.
func sunshineTreeCurrent(srcRoot string) bool {
	b, err := os.ReadFile(streamerlaunch.SunshineSourceID)
	if err != nil {
		return false
	}
	id := sunshineSourceID(srcRoot)
	return id != "" && strings.TrimSpace(string(b)) == id
}

// buildTreeManifest returns sha256sum-format lines ("<hex>  ./rel") for
// every regular file and symlink under root, for the install script to
// check against the root-owned copy.
func buildTreeManifest(root string) (string, error) {
	var b strings.Builder
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if !info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 {
			return fmt.Errorf("%s: unsupported file type", p)
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		if strings.ContainsAny(rel, "\n\\") {
			return fmt.Errorf("%q: unsupported file name", rel)
		}
		sum, err := fileSHA256(p)
		if err != nil {
			return err
		}
		fmt.Fprintf(&b, "%s  ./%s\n", sum, rel)
		return nil
	})
	return b.String(), err
}

// buildSunshineTreeInstallScript copies srcRoot into a root-owned temp dir
// under InstallDir, makes it root:root and non-group/world-writable, then
// checks it against manifestPath -- same content hashes AND the same file
// list, so nothing swapped or added mid-copy survives -- before atomically
// replacing SunshineDir. Run after the launcher install script, in the
// same pkexec invocation.
func buildSunshineTreeInstallScript(srcRoot, manifestPath, sourceID string) string {
	q := shellQuoteUsername
	tmp := streamerlaunch.InstallDir + "/.sunshine.tmp"
	old := streamerlaunch.InstallDir + "/.sunshine.old"
	return strings.Join([]string{
		"rm -rf " + q(tmp) + " " + q(old),
		"cp -a " + q(srcRoot) + " " + q(tmp),
		"chown -R -h root:root " + q(tmp),
		// go+rX too: the source root may be 0700 (stageSunshineRuntime
		// builds it via os.MkdirTemp), and the user's launcher must be
		// able to read the tree it runs.
		"chmod -R go-w,go+rX " + q(tmp),
		"(cd " + q(tmp) + " && sha256sum -c --status --strict " + q(manifestPath) + ")",
		"(cd " + q(tmp) + " && find . \\( -type f -o -type l \\) | LC_ALL=C sort) > " + q(tmp+".list"),
		"sed 's/^[0-9a-f]*  //' " + q(manifestPath) + " | LC_ALL=C sort | cmp -s - " + q(tmp+".list"),
		"rm -f " + q(tmp+".list"),
		"if [ -e " + q(streamerlaunch.SunshineDir) + " ]; then mv " + q(streamerlaunch.SunshineDir) + " " + q(old) + "; fi",
		"mv " + q(tmp) + " " + q(streamerlaunch.SunshineDir),
		"rm -rf " + q(old),
		"echo " + q(sourceID) + " > " + q(streamerlaunch.SunshineSourceID),
		"chmod 0644 " + q(streamerlaunch.SunshineSourceID),
	}, "\n")
}

// sunshineKMSGranted: launcher installed and the root-owned tree matches
// srcRoot's bundled Sunshine.
func (s *Service) sunshineKMSGranted(srcRoot string) bool {
	return s.SunshineLaunchReady() && sunshineTreeCurrent(srcRoot)
}

// installSunshineKMS is the one-time (per bundled Sunshine version) grant:
// installs/refreshes the launcher and the root-owned Sunshine tree in a
// single pkexec prompt.
func (s *Service) installSunshineKMS(srcRoot string) bool {
	if s.sunshineKMSGranted(srcRoot) {
		return true
	}
	src := bundledStreamerLauncher()
	if src == "" {
		s.lastAccessErr = "this build does not include usbridge-streamer-launch"
		log.Printf("[permissions] %s", s.lastAccessErr)
		return false
	}
	staged, sum, err := copyAndHash(src)
	if err != nil {
		s.lastAccessErr = fmt.Sprintf("stage launcher: %v", err)
		return false
	}
	defer os.Remove(staged)

	manifest, err := buildTreeManifest(srcRoot)
	if err != nil {
		s.lastAccessErr = fmt.Sprintf("hash Sunshine tree: %v", err)
		log.Printf("[permissions] %s", s.lastAccessErr)
		return false
	}
	mf, err := os.CreateTemp("", "usbridge-sunshine-manifest-*")
	if err != nil {
		return false
	}
	defer os.Remove(mf.Name())
	if _, err := mf.WriteString(manifest); err != nil {
		mf.Close()
		return false
	}
	mf.Close()

	script := buildStreamerLauncherInstallScript(staged, sum, findCapTool("setcap"), os.Getuid()) + "\n" +
		buildSunshineTreeInstallScript(srcRoot, mf.Name(), sunshineSourceID(srcRoot))
	out, err := exec.Command("pkexec", "/bin/sh", "-c", script).CombinedOutput()
	log.Printf("[permissions] install sunshine KMS pkexec exit=%v output=%q", err, string(out))
	if err != nil {
		s.lastAccessErr = fmt.Sprintf("pkexec failed: %v (%s)", err, strings.TrimSpace(string(out)))
		return false
	}
	return s.sunshineKMSGranted(srcRoot)
}

// HasFileCapability reports whether p carries any file capability.
func HasFileCapability(p string) bool {
	out, err := exec.Command(findCapTool("getcap"), p).Output()
	return err == nil && strings.Contains(string(out), "cap_")
}
