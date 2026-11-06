//go:build linux

package streamerlaunch

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// Passwordless refresh of the root-owned Sunshine/Vibepollo trees
// (`usbridge-streamer-launch --install-tree <name> <release-dir>`, run as
// root by pkexec through ForkPolkitRule). The one-time grant installs the
// launcher and that rule; from then on an update of either fork, or a switch
// from one to the other, puts the new build in place without a password.
//
// What makes that safe although any process of the user can ask for it:
//   - only bytes signed with the Streamers-Forks release key are installed
//     (ForksPublicKeyB64 over manifest.json, then the archive's SHA-256 from
//     it), extracted from one in-memory copy of the archive -- never the
//     user-writable unpacked tree;
//   - never an older release than the installed one (the version is kept in
//     a root-owned file), so a signed but outdated build can't be put back;
//   - the release dir is read without following a final symlink and only
//     files owned by the calling uid are accepted, so root can't be pointed
//     at someone else's file;
//   - the caller (PKEXEC_UID) must be in AllowedUIDsPath, like for --run.

// ForksPublicKeyB64 is the public half of the key Streamers-Forks' release
// manifest is signed with (AGENT_UPDATE_ED25519_PRIVATE_KEY, shared with the
// agent's own updates -- update.publicKeyB64).
const ForksPublicKeyB64 = "k2g7jRSwmzq8DzLAxVIr9ztQ/w8P3uAkxiHy7zRlgl8="

// ForkInstallProtocol is the first launcher with --install-tree.
const ForkInstallProtocol = 3

// ForkPolkitRulePath is where the one-time grant installs ForkPolkitRule.
const ForkPolkitRulePath = "/etc/polkit-1/rules.d/49-usbridge-streamer.rules"

// ForkPolkitRule lets pkexec run exactly `<InstallPath> --install-tree
// sunshine|vibepollo <absolute dir>` without a password -- nothing else, and
// only the root-owned launcher at its fixed path.
const ForkPolkitRule = `polkit.addRule(function(action, subject) {
    if (action.id != "org.freedesktop.policykit.exec") return;
    if (action.lookup("program") != "` + InstallPath + `") return;
    var cmd = action.lookup("command_line") || "";
    if (/^\/usr\/local\/libexec\/usbridge\/usbridge-streamer-launch --install-tree (sunshine|vibepollo) \/\S+$/.test(cmd)) return polkit.Result.YES;
});
`

// ForkTree is one of the trees --install-tree fills.
type ForkTree struct {
	Name     string // the command-line name
	Asset    string // the Streamers-Forks asset it comes from
	Dir      string // root-owned destination
	Bin      string // executable, relative to Dir
	SourceID string // SHA-256 of Bin, as permissions.treeCurrent compares it
	tmp      string // staging name under InstallDir
}

// ForkTrees are the trees --install-tree knows. Anything else is refused.
var ForkTrees = map[string]ForkTree{
	"sunshine":  {"sunshine", "Sunshine-Linux-x86_64.tar.gz", SunshineDir, "usr/bin/sunshine", SunshineSourceID, ".sunshine"},
	"vibepollo": {"vibepollo", "Vibepollo-Linux-x86_64.tar.gz", VibepolloDir, "usr/bin/vibepollo", VibepolloSourceID, ".vibepollo"},
}

// VersionFile is the root-owned record of the release t was installed from.
func (t ForkTree) VersionFile() string { return InstallDir + "/" + t.Name + ".version" }

const (
	forkManifestName = "manifest.json"
	forkSigName      = "manifest.json.sig"
	forkManifestApp  = "streamers-forks"
	maxTreeBytes     = 2 << 30 // unpacked, all files together
)

// ForksPublicKey decodes ForksPublicKeyB64.
func ForksPublicKey() ed25519.PublicKey {
	k, err := base64.StdEncoding.DecodeString(ForksPublicKeyB64)
	if err != nil || len(k) != ed25519.PublicKeySize {
		panic("streamerlaunch: bad compiled-in Streamers-Forks public key")
	}
	return ed25519.PublicKey(k)
}

type forkManifest struct {
	App     string `json:"app"`
	Version string `json:"version"`
	Assets  map[string]struct {
		SHA256 string `json:"sha256"`
	} `json:"assets"`
}

// LoadForkRelease reads releaseDir's manifest, signature and t's archive
// (files owned by ownerUID only), verifies them with pub and returns the
// archive bytes and the release version.
func LoadForkRelease(t ForkTree, releaseDir string, ownerUID int, pub ed25519.PublicKey) ([]byte, string, error) {
	if !filepath.IsAbs(releaseDir) {
		return nil, "", fmt.Errorf("release dir %q is not absolute", releaseDir)
	}
	body, err := readOwned(filepath.Join(releaseDir, forkManifestName), ownerUID, maxManifestBytes)
	if err != nil {
		return nil, "", err
	}
	sigText, err := readOwned(filepath.Join(releaseDir, forkSigName), ownerUID, maxManifestBytes)
	if err != nil {
		return nil, "", err
	}
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sigText)))
	if err != nil {
		return nil, "", fmt.Errorf("manifest signature is not base64: %w", err)
	}
	if !ed25519.Verify(pub, body, sig) {
		return nil, "", errors.New("manifest signature does not verify")
	}
	var m forkManifest
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, "", fmt.Errorf("parse manifest: %w", err)
	}
	if m.App != forkManifestApp {
		return nil, "", fmt.Errorf("manifest is for app %q, want %s", m.App, forkManifestApp)
	}
	entry, ok := m.Assets[t.Asset]
	if !ok || len(entry.SHA256) != 64 {
		return nil, "", fmt.Errorf("signed manifest %s has no %s", m.Version, t.Asset)
	}
	archive, err := readOwned(filepath.Join(releaseDir, t.Asset), ownerUID, maxArchiveBytes)
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(archive)
	if hex.EncodeToString(sum[:]) != strings.ToLower(entry.SHA256) {
		return nil, "", fmt.Errorf("%s does not match signed manifest %s", t.Asset, m.Version)
	}
	return archive, m.Version, nil
}

// readOwned reads a regular file owned by uid, without following a symlink
// in its last component and checking the opened file itself (no race with a
// swap between a stat and the open).
func readOwned(p string, uid int, limit int64) ([]byte, error) {
	fd, err := syscall.Open(p, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", p, err)
	}
	f := os.NewFile(uintptr(fd), p)
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	sys, ok := st.Sys().(*syscall.Stat_t)
	if !st.Mode().IsRegular() || !ok || int(sys.Uid) != uid {
		return nil, fmt.Errorf("%s is not a regular file of uid %d", p, uid)
	}
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", p, err)
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("%s is larger than %d bytes", p, limit)
	}
	return b, nil
}

// CompareReleaseVersions orders Streamers-Forks tags (v2026.1104.1.usbridge)
// by their numeric fields: <0, 0, >0. ok is false when either has none.
func CompareReleaseVersions(a, b string) (cmp int, ok bool) {
	na, nb := versionNumbers(a), versionNumbers(b)
	if len(na) == 0 || len(nb) == 0 {
		return 0, false
	}
	for i := 0; i < len(na) || i < len(nb); i++ {
		var x, y int
		if i < len(na) {
			x = na[i]
		}
		if i < len(nb) {
			y = nb[i]
		}
		if x != y {
			if x < y {
				return -1, true
			}
			return 1, true
		}
	}
	return 0, true
}

func versionNumbers(v string) []int {
	var out []int
	for _, f := range strings.FieldsFunc(v, func(r rune) bool { return r < '0' || r > '9' }) {
		n, err := strconv.Atoi(f)
		if err != nil {
			return nil
		}
		out = append(out, n)
	}
	return out
}

// InstallForkTree is --install-tree's work, run as root: verify, refuse a
// downgrade, unpack into a fresh root-owned staging dir and swap it in.
func InstallForkTree(t ForkTree, releaseDir string, callerUID int) (string, error) {
	if os.Geteuid() != 0 {
		return "", errors.New("--install-tree must run as root (through pkexec)")
	}
	if err := UIDAllowed(AllowedUIDsPath, callerUID); err != nil {
		return "", err
	}
	if err := CheckRootOwned(InstallDir); err != nil {
		return "", err
	}
	archive, version, err := LoadForkRelease(t, releaseDir, callerUID, ForksPublicKey())
	if err != nil {
		return "", err
	}
	if b, err := os.ReadFile(t.VersionFile()); err == nil && CheckRootOwned(t.VersionFile()) == nil {
		if c, ok := CompareReleaseVersions(version, strings.TrimSpace(string(b))); ok && c < 0 {
			return "", fmt.Errorf("%s is older than the installed %s", version, strings.TrimSpace(string(b)))
		}
	}
	syscall.Umask(0o022)
	tmp := InstallDir + "/" + t.tmp + ".tmp"
	old := InstallDir + "/" + t.tmp + ".old"
	_ = os.RemoveAll(tmp)
	_ = os.RemoveAll(old)
	if err := os.Mkdir(tmp, 0o755); err != nil {
		return "", err
	}
	if err := extractTreeAsRoot(archive, tmp); err != nil {
		_ = os.RemoveAll(tmp)
		return "", err
	}
	binSum, err := regularFileSHA256(filepath.Join(tmp, t.Bin))
	if err != nil {
		_ = os.RemoveAll(tmp)
		return "", fmt.Errorf("%s has no %s: %w", t.Asset, t.Bin, err)
	}
	if err := CheckTreeRootOwned(tmp); err != nil {
		_ = os.RemoveAll(tmp)
		return "", err
	}
	if _, err := os.Lstat(t.Dir); err == nil {
		if err := os.Rename(t.Dir, old); err != nil {
			_ = os.RemoveAll(tmp)
			return "", err
		}
	}
	if err := os.Rename(tmp, t.Dir); err != nil {
		_ = os.Rename(old, t.Dir)
		_ = os.RemoveAll(tmp)
		return "", err
	}
	_ = os.RemoveAll(old)
	if err := writeRootFile(t.SourceID, binSum+"\n"); err != nil {
		return "", err
	}
	if err := writeRootFile(t.VersionFile(), version+"\n"); err != nil {
		return "", err
	}
	return version, nil
}

func writeRootFile(p, content string) error {
	tmp := p + ".tmp"
	_ = os.Remove(tmp)
	if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

func regularFileSHA256(p string) (string, error) {
	st, err := os.Lstat(p)
	if err != nil {
		return "", err
	}
	if !st.Mode().IsRegular() {
		return "", fmt.Errorf("%s is not a regular file", p)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// treeRelPath turns an archive member name into a clean relative path, or
// false for one that would leave the tree (or is the root itself).
func treeRelPath(name string) (string, bool) {
	name = strings.TrimPrefix(name, "./")
	if name == "" || strings.HasPrefix(name, "/") || strings.Contains(name, "\\") {
		return "", false
	}
	clean := path.Clean(name)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", false
	}
	return clean, true
}

// extractTreeAsRoot unpacks the verified archive into dest (a fresh,
// root-owned dir): directories, regular files (permission bits only, never
// setuid/setgid/sticky or group/world-writable) and symlinks that stay
// inside the tree. Anything else -- hard links, devices, fifos -- is
// skipped. Files are created O_EXCL|O_NOFOLLOW: an entry can't write
// through a symlink an earlier entry made.
func extractTreeAsRoot(archive []byte, dest string) error {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return fmt.Errorf("open gzip: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	var total int64
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read tar: %w", err)
		}
		rel, ok := treeRelPath(hdr.Name)
		if !ok {
			continue
		}
		target := filepath.Join(dest, rel)
		if err := checkNoSymlinkParent(dest, rel); err != nil {
			return err
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			total += hdr.Size
			if hdr.Size < 0 || total > maxTreeBytes {
				return fmt.Errorf("archive unpacks to more than %d bytes", int64(maxTreeBytes))
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			mode := os.FileMode(hdr.Mode).Perm() &^ 0o022
			f, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, mode|0o600)
			if err != nil {
				return err
			}
			_, cerr := io.Copy(f, io.LimitReader(tr, hdr.Size))
			if err := f.Close(); cerr == nil {
				cerr = err
			}
			if cerr != nil {
				return cerr
			}
			if err := os.Chmod(target, mode|0o400); err != nil {
				return err
			}
		case tar.TypeSymlink:
			if path.IsAbs(hdr.Linkname) {
				continue
			}
			if _, ok := treeRelPath(path.Join(path.Dir(rel), hdr.Linkname)); !ok {
				continue
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			if err := os.Symlink(hdr.Linkname, target); err != nil {
				return err
			}
		}
	}
}

// checkNoSymlinkParent fails when a directory on rel's way under dest is a
// symlink (made by an earlier entry): writing through it could leave dest.
func checkNoSymlinkParent(dest, rel string) error {
	dir := dest
	parts := strings.Split(rel, "/")
	for _, p := range parts[:len(parts)-1] {
		dir = filepath.Join(dir, p)
		st, err := os.Lstat(dir)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if st.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("archive writes through the symlink %s", dir)
		}
	}
	return nil
}
