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
	"os"
	"path/filepath"
	"testing"
)

func makeArchive(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		tw.Write(body)
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

func writeBundle(t *testing.T, dir string, priv ed25519.PrivateKey, archive []byte, app string) {
	t.Helper()
	sum := sha256.Sum256(archive)
	m, _ := json.Marshal(map[string]any{
		"app": app, "version": "usbridge-streamer-v9.9.9",
		"platforms": map[string]any{"linux-x86_64": map[string]string{"asset": "usbridge-streamer-linux-x86_64.tar.gz", "sha256": hex.EncodeToString(sum[:])}},
	})
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, ManifestName), m, 0o644)
	os.WriteFile(filepath.Join(dir, SigName), []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(priv, m))+"\n"), 0o644)
	os.WriteFile(filepath.Join(dir, ArchiveName), archive, 0o644)
}

func TestLoadVerified_AcceptsSignedBundle(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	dir := t.TempDir()
	writeBundle(t, dir, priv, makeArchive(t, map[string][]byte{"usbridge-streamer/usbridge-streamer": []byte("ELF-bytes")}), "usbridge-streamer")
	v, err := LoadVerified(dir, "linux-x86_64", pub)
	if err != nil {
		t.Fatal(err)
	}
	if v.Version != "usbridge-streamer-v9.9.9" || string(v.Binary) != "ELF-bytes" {
		t.Fatalf("got %q %q", v.Version, v.Binary)
	}
}

func TestLoadVerified_Rejects(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	_, otherPriv, _ := ed25519.GenerateKey(nil)
	good := makeArchive(t, map[string][]byte{"usbridge-streamer": []byte("ELF")})
	cases := map[string]func(dir string){
		"tampered archive": func(dir string) {
			writeBundle(t, dir, priv, good, "usbridge-streamer")
			os.WriteFile(filepath.Join(dir, ArchiveName), makeArchive(t, map[string][]byte{"usbridge-streamer": []byte("EVIL")}), 0o644)
		},
		"edited manifest": func(dir string) {
			writeBundle(t, dir, priv, good, "usbridge-streamer")
			b, _ := os.ReadFile(filepath.Join(dir, ManifestName))
			os.WriteFile(filepath.Join(dir, ManifestName), bytes.Replace(b, []byte("9.9.9"), []byte("9.9.8"), 1), 0o644)
		},
		"foreign key": func(dir string) { writeBundle(t, dir, otherPriv, good, "usbridge-streamer") },
		"wrong app":   func(dir string) { writeBundle(t, dir, priv, good, "something-else") },
		"no binary": func(dir string) {
			writeBundle(t, dir, priv, makeArchive(t, map[string][]byte{"README": []byte("x")}), "usbridge-streamer")
		},
		"missing bundle": func(dir string) {},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			setup(dir)
			if _, err := LoadVerified(dir, "linux-x86_64", pub); err == nil {
				t.Fatal("LoadVerified accepted it")
			}
		})
	}
	dir := t.TempDir()
	writeBundle(t, dir, priv, good, "usbridge-streamer")
	if _, err := LoadVerified(dir, "windows-x86_64", pub); err == nil {
		t.Fatal("accepted a platform the manifest has no entry for")
	}
}

func TestReleasePublicKey_MatchesBackend(t *testing.T) {
	// Keep in lockstep with usbridge-entitlement-backend's
	// RUSTSHINE_RELEASE_PUBKEY_B64 (src/manifest.ts).
	if len(ReleasePublicKey()) != ed25519.PublicKeySize {
		t.Fatal("bad key")
	}
	if ReleasePublicKeyB64 != "ixB/m3G9UgxUrhQd2rVRzTBDvA/x9NjT1yTmUbUQl+0=" {
		t.Fatal("release key changed -- update the backend too")
	}
}
