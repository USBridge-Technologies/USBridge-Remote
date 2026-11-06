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
	"sync"

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
	if s.StreamerLauncherInstalled() && installedLauncherProtocol() >= streamerlaunch.Protocol && forkRuleInstalled() {
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

	rule, err := stageForkRule()
	if err != nil {
		s.lastAccessErr = fmt.Sprintf("stage polkit rule: %v", err)
		return false
	}
	defer os.Remove(rule)

	script := buildStreamerLauncherInstallScript(staged, sum, findCapTool("setcap"), os.Getuid()) + "\n" +
		buildForkRuleInstallScript(rule)
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

// kmsTree is a stream host the launcher runs from a root-owned copy of its
// install tree: Sunshine (--run-sunshine) or Vibepollo (--run-vibepollo).
type kmsTree struct {
	name     string // for logs and errors
	bin      string // the executable, relative to the tree root
	dir      string // the root-owned copy the launcher execs
	sourceID string // records which source build dir was installed from
	tmp      string // install staging name under InstallDir
	minProto int    // the oldest launcher that can run it
	fork     string // streamerlaunch.ForkTrees key (--install-tree)
}

var (
	sunshineKMSTree = kmsTree{"Sunshine", "usr/bin/sunshine", streamerlaunch.SunshineDir,
		streamerlaunch.SunshineSourceID, ".sunshine", streamerlaunch.MinProtocol, "sunshine"}
	vibepolloKMSTree = kmsTree{"Vibepollo", "usr/bin/vibepollo", streamerlaunch.VibepolloDir,
		streamerlaunch.VibepolloSourceID, ".vibepollo", streamerlaunch.VibepolloMinProtocol, "vibepollo"}
)

// kmsTreeOf reports which stream host's install-tree root p is (the KMS
// target sunshineBackend.CapExecPath returns), as opposed to RustShine's
// streamerlaunch.InstallPath.
func kmsTreeOf(p string) (kmsTree, bool) {
	for _, t := range []kmsTree{sunshineKMSTree, vibepolloKMSTree} {
		if st, err := os.Stat(filepath.Join(p, t.bin)); err == nil && st.Mode().IsRegular() {
			return t, true
		}
	}
	return kmsTree{}, false
}

// isSunshineTree reports whether p is a Sunshine install-tree root.
func isSunshineTree(p string) bool {
	t, ok := kmsTreeOf(p)
	return ok && t.dir == streamerlaunch.SunshineDir
}

// sunshineIDCache memoizes fileSHA256 of a source tree's executable by
// (path, size, mtime) -- KMSCaptureGranted is polled by the UI and the
// binary is ~25MB.
var sunshineIDCache struct {
	key string
	sum string
}

func sunshineSourceID(srcRoot string) string { return treeSourceID(srcRoot, sunshineKMSTree) }

func treeSourceID(srcRoot string, t kmsTree) string {
	bin := filepath.Join(srcRoot, t.bin)
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
func (s *Service) SunshineLaunchReady() bool { return s.treeLaunchReady(sunshineKMSTree) }

// VibepolloLaunchReady: the same for `--run-vibepollo` (a launcher new
// enough to have it, and the root-owned Vibepollo tree).
func (s *Service) VibepolloLaunchReady() bool { return s.treeLaunchReady(vibepolloKMSTree) }

func (s *Service) treeLaunchReady(t kmsTree) bool {
	return s.StreamerLauncherInstalled() && installedLauncherProtocol() >= t.minProto &&
		streamerlaunch.CheckTreeRootOwned(t.dir) == nil
}

// treeCurrent reports whether the installed root-owned tree was installed
// from srcRoot's exact build.
func treeCurrent(srcRoot string, t kmsTree) bool {
	b, err := os.ReadFile(t.sourceID)
	if err != nil {
		return false
	}
	id := treeSourceID(srcRoot, t)
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
	return buildTreeInstallScript(srcRoot, manifestPath, sourceID, sunshineKMSTree)
}

func buildTreeInstallScript(srcRoot, manifestPath, sourceID string, t kmsTree) string {
	q := shellQuoteUsername
	tmp := streamerlaunch.InstallDir + "/" + t.tmp + ".tmp"
	old := streamerlaunch.InstallDir + "/" + t.tmp + ".old"
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
		"if [ -e " + q(t.dir) + " ]; then mv " + q(t.dir) + " " + q(old) + "; fi",
		"mv " + q(tmp) + " " + q(t.dir),
		"rm -rf " + q(old),
		"echo " + q(sourceID) + " > " + q(t.sourceID),
		"chmod 0644 " + q(t.sourceID),
	}, "\n")
}

// sunshineKMSGranted: launcher installed and the root-owned tree matches
// srcRoot's bundled Sunshine.
func (s *Service) sunshineKMSGranted(srcRoot string) bool {
	return s.treeKMSGranted(srcRoot, sunshineKMSTree)
}

// treeKMSGranted: launcher installed (new enough for t) and the root-owned
// tree matches srcRoot's build.
func (s *Service) treeKMSGranted(srcRoot string, t kmsTree) bool {
	return s.treeLaunchReady(t) && treeCurrent(srcRoot, t)
}

// installSunshineKMS is the one-time (per bundled Sunshine version) grant:
// installs/refreshes the launcher and the root-owned Sunshine tree in a
// single pkexec prompt.
func (s *Service) installSunshineKMS(srcRoot string) bool {
	return s.installTreeKMS(srcRoot, sunshineKMSTree)
}

// installTreeKMS is the screen-capture grant for a Sunshine/Vibepollo tree:
// one pkexec prompt that installs the launcher and the polkit rule for
// --install-tree, then -- when srcRoot came with its signed release -- the
// tree itself goes in through --install-tree (no second prompt), and the
// other fork's tree too if it is downloaded. Only a tree without a signed
// release (an older agent's bundled Sunshine) is copied in by that one
// prompt's script instead.
func (s *Service) installTreeKMS(srcRoot string, t kmsTree) bool {
	if s.treeKMSGranted(srcRoot, t) && forkAutoReady() {
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
	rule, err := stageForkRule()
	if err != nil {
		s.lastAccessErr = fmt.Sprintf("stage polkit rule: %v", err)
		return false
	}
	defer os.Remove(rule)

	script := buildStreamerLauncherInstallScript(staged, sum, findCapTool("setcap"), os.Getuid()) + "\n" +
		buildForkRuleInstallScript(rule)
	signed := hasSignedRelease(srcRoot, t)
	if !signed {
		manifest, err := buildTreeManifest(srcRoot)
		if err != nil {
			s.lastAccessErr = fmt.Sprintf("hash %s tree: %v", t.name, err)
			log.Printf("[permissions] %s", s.lastAccessErr)
			return false
		}
		mf, err := os.CreateTemp("", "usbridge-"+strings.ToLower(t.name)+"-manifest-*")
		if err != nil {
			return false
		}
		defer os.Remove(mf.Name())
		if _, err := mf.WriteString(manifest); err != nil {
			mf.Close()
			return false
		}
		mf.Close()
		script += "\n" + buildTreeInstallScript(srcRoot, mf.Name(), treeSourceID(srcRoot, t), t)
	}
	out, err := exec.Command("pkexec", "/bin/sh", "-c", script).CombinedOutput()
	log.Printf("[permissions] install %s KMS pkexec exit=%v output=%q", t.name, err, string(out))
	if err != nil {
		s.lastAccessErr = fmt.Sprintf("pkexec failed: %v (%s)", err, strings.TrimSpace(string(out)))
		return false
	}
	if signed {
		if err := installTreeFromRelease(srcRoot, t); err != nil {
			s.lastAccessErr = err.Error()
			return false
		}
	}
	s.refreshOtherForkTrees(srcRoot, t)
	return s.treeKMSGranted(srcRoot, t)
}

// refreshOtherForkTrees brings the other fork's root-owned tree up to date
// right after a grant, when that fork is downloaded beside srcRoot (both
// live in the agent's state dir), so a later switch starts with KMS at once.
func (s *Service) refreshOtherForkTrees(srcRoot string, done kmsTree) {
	stateDir := filepath.Dir(srcRoot)
	for _, t := range []kmsTree{sunshineKMSTree, vibepolloKMSTree} {
		if t.dir == done.dir {
			continue
		}
		other := filepath.Join(stateDir, t.fork+"-host")
		if hasSignedRelease(other, t) && !s.treeKMSGranted(other, t) {
			if err := installTreeFromRelease(other, t); err != nil {
				log.Printf("[permissions] %v", err)
			}
		}
	}
}

// --- passwordless refresh from a signed release (launcher --install-tree) ---

// releaseDirOf is forkrelease.ReleaseDir: the signed release kept beside a
// downloaded tree.
func releaseDirOf(srcRoot string) string { return srcRoot + ".release" }

// hasSignedRelease: srcRoot came with the signed release --install-tree
// needs (a download by this agent version or later).
func hasSignedRelease(srcRoot string, t kmsTree) bool {
	ft, ok := streamerlaunch.ForkTrees[t.fork]
	if !ok {
		return false
	}
	dir := releaseDirOf(srcRoot)
	for _, n := range []string{"manifest.json", "manifest.json.sig", ft.Asset} {
		if st, err := os.Stat(filepath.Join(dir, n)); err != nil || !st.Mode().IsRegular() {
			return false
		}
	}
	return true
}

// forkRuleInstalled: the polkit rule for --install-tree is in place, as is.
func forkRuleInstalled() bool {
	b, err := os.ReadFile(streamerlaunch.ForkPolkitRulePath)
	return err == nil && string(b) == streamerlaunch.ForkPolkitRule
}

// forkAutoReady: an installed launcher with --install-tree and its polkit
// rule -- trees refresh without a password from here on.
func forkAutoReady() bool {
	if _, err := exec.LookPath("pkexec"); err != nil {
		return false
	}
	return installedLauncherProtocol() >= streamerlaunch.ForkInstallProtocol && forkRuleInstalled()
}

func stageForkRule() (string, error) {
	f, err := os.CreateTemp("", "usbridge-streamer-*.rules")
	if err != nil {
		return "", err
	}
	if _, err := f.WriteString(streamerlaunch.ForkPolkitRule); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	if err := os.Chmod(f.Name(), 0o644); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

// buildForkRuleInstallScript installs the --install-tree polkit rule; run in
// the same pkexec invocation as the launcher install. The staged rule is
// copied root-owned first and only then compared, like the launcher.
func buildForkRuleInstallScript(tmpRule string) string {
	q := shellQuoteUsername
	tmp := streamerlaunch.ForkPolkitRulePath + ".tmp"
	want := sha256.Sum256([]byte(streamerlaunch.ForkPolkitRule))
	return strings.Join([]string{
		"install -d -m 0755 /etc/polkit-1/rules.d",
		"install -o root -g root -m 0644 " + q(tmpRule) + " " + q(tmp),
		"echo " + q(hex.EncodeToString(want[:])+"  "+tmp) + " | sha256sum -c --status",
		"mv -f " + q(tmp) + " " + q(streamerlaunch.ForkPolkitRulePath),
	}, "\n")
}

// installTreeFromRelease runs `pkexec <launcher> --install-tree` for
// srcRoot's signed release: no password (the polkit rule), root-side
// signature check (streamerlaunch.InstallForkTree).
func installTreeFromRelease(srcRoot string, t kmsTree) error {
	dir, err := filepath.Abs(releaseDirOf(srcRoot))
	if err != nil {
		return err
	}
	if strings.ContainsAny(dir, " \t\n") {
		return fmt.Errorf("%s: a path with spaces can't go through the polkit rule", dir)
	}
	out, err := exec.Command("pkexec", streamerlaunch.InstallPath, "--install-tree", t.fork, dir).CombinedOutput()
	log.Printf("[permissions] %s tree from signed release: exit=%v output=%q", t.name, err, strings.TrimSpace(string(out)))
	if err != nil {
		return fmt.Errorf("install %s tree: %v (%s)", t.name, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// refreshFailed remembers the build a passwordless refresh last failed for,
// so a broken release isn't retried (one pkexec each) on every start.
var refreshFailed struct {
	sync.Mutex
	ids map[string]bool
}

// treeAutoGranted: target's tree isn't in place yet, but EnsureTreeKMS will
// put it there without a password (and hasn't failed for this build).
func (s *Service) treeAutoGranted(target string, t kmsTree) bool {
	if !s.StreamerLauncherInstalled() || !forkAutoReady() || !hasSignedRelease(target, t) {
		return false
	}
	refreshFailed.Lock()
	defer refreshFailed.Unlock()
	return !refreshFailed.ids[t.fork+"|"+treeSourceID(target, t)]
}

// EnsureTreeKMS brings the root-owned tree for target (a Sunshine/Vibepollo
// tree root, as KMSCaptureGranted takes it) up to date without a password
// when it can: once granted (forkAutoReady) and the tree came with its
// signed release. True when the root-owned tree now matches target.
// Anything else (RustShine, no grant yet, a bundled unsigned tree) is a
// no-op returning whether it already matches.
func (s *Service) EnsureTreeKMS(target string) bool {
	t, ok := kmsTreeOf(target)
	if !ok {
		return false
	}
	if s.treeKMSGranted(target, t) {
		return true
	}
	if !s.StreamerLauncherInstalled() || !forkAutoReady() || !hasSignedRelease(target, t) {
		return false
	}
	id := t.fork + "|" + treeSourceID(target, t)
	refreshFailed.Lock()
	failed := refreshFailed.ids[id]
	refreshFailed.Unlock()
	if failed {
		return false
	}
	if err := installTreeFromRelease(target, t); err != nil || !s.treeKMSGranted(target, t) {
		refreshFailed.Lock()
		if refreshFailed.ids == nil {
			refreshFailed.ids = map[string]bool{}
		}
		refreshFailed.ids[id] = true
		refreshFailed.Unlock()
		return false
	}
	return true
}

// HasFileCapability reports whether p carries any file capability.
func HasFileCapability(p string) bool {
	out, err := exec.Command(findCapTool("getcap"), p).Output()
	return err == nil && strings.Contains(string(out), "cap_")
}
