package entitlement

import (
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"

	"usbridge_agent/internal/streamerlaunch"
)

// bundlePublicKey is the key stageLinux checks bundles against --
// streamerlaunch's compiled-in release key; a var only so tests can
// substitute a throwaway keypair.
var bundlePublicKey = streamerlaunch.ReleasePublicKey

// stageLinux extracts archivePath into liveDir+".next", saves the signed
// release bundle alongside it (when the backend supplied the manifest),
// runs verify against that not-yet-live bundle, and only then moves
// everything into liveDir. Any failure before the commit leaves liveDir
// exactly as it was.
func stageLinux(archivePath string, info *DownloadInfo, liveDir, platform string, verify BundleVerifier) error {
	next := liveDir + ".next"
	if err := os.RemoveAll(next); err != nil {
		return fmt.Errorf("entitlement: clear %s: %w", next, err)
	}
	if err := os.MkdirAll(next, 0o755); err != nil {
		return fmt.Errorf("entitlement: create %s: %w", next, err)
	}
	committed := false
	defer func() {
		if !committed {
			os.RemoveAll(next)
		}
	}()

	if err := extractFromTarGz(archivePath, binaryName(), filepath.Join(next, binaryName())); err != nil {
		return err
	}

	haveBundle := false
	if info.Manifest != "" && info.ManifestSig != "" {
		if err := writeBundle(filepath.Join(next, streamerlaunch.BundleDirName), archivePath, info); err != nil {
			return err
		}
		bundle := streamerlaunch.BundleDir(next)
		// The agent's own check with the same code the launcher runs --
		// catches a bad/mismatched manifest even when no launcher is
		// installed yet, so a bundle that could never verify is never
		// kept around as if it would.
		if _, err := streamerlaunch.LoadVerified(bundle, platform, bundlePublicKey()); err != nil {
			log.Printf("[entitlement] signed release bundle for %s does not verify, not keeping it: %v", info.Version, err)
			os.RemoveAll(bundle)
		} else {
			haveBundle = true
		}
	}
	if verify != nil {
		// The launcher is installed, so the current build runs with KMS
		// capture. A build it can't verify would silently lose that on the
		// next restart -- and with it a remote session stuck on a portal
		// prompt -- so such an update is refused, not applied.
		if !haveBundle {
			return fmt.Errorf("entitlement: %s came without a verifiable signed manifest; keeping the current build so KMS capture keeps working", info.Version)
		}
		if err := verify(streamerlaunch.BundleDir(next)); err != nil {
			return fmt.Errorf("entitlement: %s rejected by installed streamer launcher, keeping the current build: %w", info.Version, err)
		}
	}

	// Commit. Each rename is atomic; the bundle goes in before the binary
	// so a crash in between never leaves a new binary next to an old
	// bundle that the launcher would (correctly) run instead.
	liveBundle := streamerlaunch.BundleDir(liveDir)
	if haveBundle {
		old := liveBundle + ".old"
		os.RemoveAll(old)
		if err := os.Rename(liveBundle, old); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("entitlement: retire old bundle: %w", err)
		}
		if err := os.Rename(streamerlaunch.BundleDir(next), liveBundle); err != nil {
			os.Rename(old, liveBundle)
			return fmt.Errorf("entitlement: install bundle: %w", err)
		}
		os.RemoveAll(old)
	} else {
		// A stale bundle from an older release would make the launcher
		// run that older build; drop it so the plain binary is used.
		os.RemoveAll(liveBundle)
	}
	entries, err := os.ReadDir(next)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if err := os.Rename(filepath.Join(next, e.Name()), filepath.Join(liveDir, e.Name())); err != nil {
			return fmt.Errorf("entitlement: install %s: %w", e.Name(), err)
		}
	}
	committed = true
	os.RemoveAll(next)
	return nil
}

func writeBundle(dir, archivePath string, info *DownloadInfo) error {
	manifest, err := base64.StdEncoding.DecodeString(info.Manifest)
	if err != nil {
		return fmt.Errorf("entitlement: backend manifest is not base64: %w", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, streamerlaunch.ManifestName), manifest, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, streamerlaunch.SigName), []byte(info.ManifestSig), 0o644); err != nil {
		return err
	}
	src, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := os.OpenFile(filepath.Join(dir, streamerlaunch.ArchiveName), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		return err
	}
	return dst.Close()
}
