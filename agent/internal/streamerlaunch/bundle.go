// Package streamerlaunch is the shared core of cmd/usbridge_streamer_launch,
// the root-owned, capability-carrying launcher that gives usbridge-streamer
// (RustShine) CAP_SYS_ADMIN for KMS capture on Linux without that
// capability ever living on the streamer binary itself.
//
// Why a separate launcher at all: a file capability belongs to one inode.
// usbridge-streamer is re-downloaded on every RustShine update
// (entitlement.StageRustShine), so a `setcap` on it silently vanished with
// each update and the user had to click "Grant" again -- impossible from a
// remote session, since pkexec asks for the password on the physical
// screen. The launcher is installed once into InstallDir (root-owned, so
// the user can't swap it) and never changes when the streamer updates.
//
// Why it only ever runs a signed build: a capability-carrying binary that
// executes an arbitrary path would hand CAP_SYS_ADMIN (close to root) to
// any process running as this user, no password needed. So the launcher
// runs only bytes whose SHA-256 matches rust-shine's Ed25519-signed release
// manifest (the same manifest.json/.sig the entitlement backend already
// verifies, see rust-shine/docs/RELEASE_SIGNING.md), and it runs them from
// a sealed memfd holding exactly the bytes it verified -- so nothing can
// swap the file between the check and the exec.
//
// This package is stdlib-only on purpose: the launcher is a tiny static
// binary and must stay auditable.
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
	"runtime"
	"strings"
)

// ReleasePublicKeyB64 is the public half of rust-shine's release-signing
// keypair (RUSTSHINE_RELEASE_ED25519_PRIVATE_KEY, a rust-shine repo secret)
// -- the same value as usbridge-entitlement-backend's
// RUSTSHINE_RELEASE_PUBKEY_B64. Rotating that key means updating both.
const ReleasePublicKeyB64 = "ixB/m3G9UgxUrhQd2rVRzTBDvA/x9NjT1yTmUbUQl+0="

// Protocol is the launcher's command-line/bundle-layout version. The agent
// refuses an installed launcher older than MinProtocol and asks for a
// one-time re-grant instead -- bump both only when the agent starts
// depending on something an older installed launcher can't do.
const (
	Protocol    = 1
	MinProtocol = 1
)

// InstallDir/InstallPath/AllowedUIDsPath are fixed, root-owned locations
// (see permissions.InstallStreamerLauncher). Fixed rather than configurable
// so the launcher never trusts a path the user could influence.
const (
	InstallDir      = "/usr/local/libexec/usbridge"
	InstallPath     = InstallDir + "/usbridge-streamer-launch"
	AllowedUIDsPath = InstallDir + "/allowed-uids"
	// EmptyDir is the root-owned, always-empty directory the launcher
	// points XDG_CONFIG_HOME/XDG_DATA_HOME at -- see SanitizedEnv.
	EmptyDir = InstallDir + "/empty"
	// SunshineDir is the root-owned copy of the bundled Sunshine install
	// tree (usr/bin/sunshine, usr/lib, usr/local/assets) that --run-sunshine
	// execs. Sunshine isn't signed by us, so instead of a signature the
	// launcher trusts only a tree nobody but root can write -- which also
	// covers its RPATH=$ORIGIN/../lib shared libraries.
	SunshineDir = InstallDir + "/sunshine"
	SunshineBin = SunshineDir + "/usr/bin/sunshine"
	// SunshineSourceID records which bundled Sunshine build SunshineDir was
	// installed from (SHA-256 of its usr/bin/sunshine), so the agent can
	// tell when an agent update shipped a newer Sunshine.
	SunshineSourceID = InstallDir + "/sunshine.sha256"
)

// Bundle layout: BundleDirName lives next to the staged usbridge-streamer
// binary and holds the release archive exactly as downloaded plus its
// signed manifest.
const (
	BundleDirName = "release"
	ArchiveName   = "usbridge-streamer.tar.gz"
	ManifestName  = "manifest.json"
	SigName       = "manifest.json.sig"
	BinaryName    = "usbridge-streamer"
)

// Size limits: generous for today's ~6MB archive / ~16MB binary, but they
// stop a hostile bundle from making the launcher allocate without bound.
const (
	maxManifestBytes = 1 << 20
	maxArchiveBytes  = 512 << 20
	maxBinaryBytes   = 1 << 30
)

// BundleDir returns the bundle directory for a streamer staged in
// stagedDir (the directory holding the usbridge-streamer binary).
func BundleDir(stagedDir string) string {
	return filepath.Join(stagedDir, BundleDirName)
}

// Platform mirrors entitlement.Platform() -- duplicated so the launcher
// doesn't pull in net/http.
func Platform() string {
	switch {
	case runtime.GOOS == "linux" && runtime.GOARCH == "amd64":
		return "linux-x86_64"
	default:
		return ""
	}
}

type asset struct {
	Asset  string `json:"asset"`
	SHA256 string `json:"sha256"`
}

// Manifest mirrors rust-shine's scripts/sign_usbridge_manifest.go schema
// (only the fields the launcher needs).
type Manifest struct {
	App       string           `json:"app"`
	Version   string           `json:"version"`
	Platforms map[string]asset `json:"platforms"`
}

// VerifyManifest checks sigB64 (standard base64, as the signing script
// writes it) over the exact manifest bytes, then parses them.
func VerifyManifest(manifestBytes, sigB64 []byte, pub ed25519.PublicKey) (*Manifest, error) {
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sigB64)))
	if err != nil {
		return nil, fmt.Errorf("manifest signature is not base64: %w", err)
	}
	if !ed25519.Verify(pub, manifestBytes, sig) {
		return nil, errors.New("manifest signature does not verify")
	}
	var m Manifest
	if err := json.Unmarshal(manifestBytes, &m); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	if m.App != "usbridge-streamer" {
		return nil, fmt.Errorf("manifest is for app %q, want usbridge-streamer", m.App)
	}
	return &m, nil
}

// ReleasePublicKey decodes ReleasePublicKeyB64.
func ReleasePublicKey() ed25519.PublicKey {
	k, err := base64.StdEncoding.DecodeString(ReleasePublicKeyB64)
	if err != nil || len(k) != ed25519.PublicKeySize {
		panic("streamerlaunch: bad compiled-in release public key")
	}
	return ed25519.PublicKey(k)
}

// Verified is a bundle whose archive matched the signed manifest.
type Verified struct {
	Version string
	Binary  []byte // the usbridge-streamer ELF, extracted from the verified archive bytes
}

// LoadVerified reads bundleDir, verifies the manifest signature with pub,
// checks the archive's SHA-256 against the manifest's entry for platform,
// and extracts the streamer binary -- all from one in-memory copy of the
// archive, so the bytes returned are exactly the bytes that were hashed.
func LoadVerified(bundleDir, platform string, pub ed25519.PublicKey) (*Verified, error) {
	if platform == "" {
		return nil, fmt.Errorf("no usbridge-streamer build for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	manifestBytes, err := readLimited(filepath.Join(bundleDir, ManifestName), maxManifestBytes)
	if err != nil {
		return nil, err
	}
	sig, err := readLimited(filepath.Join(bundleDir, SigName), maxManifestBytes)
	if err != nil {
		return nil, err
	}
	m, err := VerifyManifest(manifestBytes, sig, pub)
	if err != nil {
		return nil, err
	}
	entry, ok := m.Platforms[platform]
	if !ok || len(entry.SHA256) != 64 {
		return nil, fmt.Errorf("signed manifest %s has no entry for %s", m.Version, platform)
	}
	archive, err := readLimited(filepath.Join(bundleDir, ArchiveName), maxArchiveBytes)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(archive)
	if hex.EncodeToString(sum[:]) != strings.ToLower(entry.SHA256) {
		return nil, fmt.Errorf("archive does not match signed manifest %s", m.Version)
	}
	bin, err := extractBinary(archive)
	if err != nil {
		return nil, err
	}
	return &Verified{Version: m.Version, Binary: bin}, nil
}

func readLimited(p string, limit int64) ([]byte, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", p, err)
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("%s is larger than %d bytes", p, limit)
	}
	return b, nil
}

// extractBinary returns the regular-file entry named BinaryName (at any
// depth, like entitlement.extractFromTarGz) from a .tar.gz held in memory.
func extractBinary(archive []byte) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, fmt.Errorf("open gzip: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil, fmt.Errorf("archive has no %s entry", BinaryName)
		}
		if err != nil {
			return nil, fmt.Errorf("read tar: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg || path.Base(hdr.Name) != BinaryName {
			continue
		}
		if hdr.Size > maxBinaryBytes {
			return nil, fmt.Errorf("%s entry is too large (%d bytes)", BinaryName, hdr.Size)
		}
		return io.ReadAll(io.LimitReader(tr, maxBinaryBytes))
	}
}
