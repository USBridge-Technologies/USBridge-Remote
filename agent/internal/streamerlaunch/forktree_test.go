//go:build linux

package streamerlaunch

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type tarEntry struct {
	name, link, body string
	typ              byte
	mode             int64
}

func makeTarGz(t *testing.T, entries []tarEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		h := &tar.Header{Name: e.name, Typeflag: e.typ, Mode: e.mode, Linkname: e.link}
		if e.typ == tar.TypeReg {
			h.Size = int64(len(e.body))
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if e.typ == tar.TypeReg {
			if _, err := tw.Write([]byte(e.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

func writeRelease(t *testing.T, dir string, priv ed25519.PrivateKey, version, asset string, archive []byte) {
	t.Helper()
	sum := sha256.Sum256(archive)
	m := map[string]any{"app": "streamers-forks", "version": version,
		"assets": map[string]any{asset: map[string]string{"sha256": hex.EncodeToString(sum[:])}}}
	body, _ := json.Marshal(m)
	sig := base64.StdEncoding.EncodeToString(ed25519.Sign(priv, body))
	for name, b := range map[string][]byte{"manifest.json": body, "manifest.json.sig": []byte(sig + "\n"), asset: archive} {
		if err := os.WriteFile(filepath.Join(dir, name), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestLoadForkRelease(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	tree := ForkTrees["vibepollo"]
	archive := makeTarGz(t, []tarEntry{{name: "usr/bin/vibepollo", body: "elf", typ: tar.TypeReg, mode: 0o755}})
	dir := t.TempDir()
	writeRelease(t, dir, priv, "v2026.1104.1.usbridge", tree.Asset, archive)
	uid := os.Getuid()

	got, version, err := LoadForkRelease(tree, dir, uid, pub)
	if err != nil || version != "v2026.1104.1.usbridge" || !bytes.Equal(got, archive) {
		t.Fatalf("LoadForkRelease = %d bytes, %q, %v", len(got), version, err)
	}

	otherPub, _, _ := ed25519.GenerateKey(rand.Reader)
	if _, _, err := LoadForkRelease(tree, dir, uid, otherPub); err == nil {
		t.Fatal("accepted a manifest signed with another key")
	}
	if _, _, err := LoadForkRelease(ForkTrees["sunshine"], dir, uid, pub); err == nil {
		t.Fatal("accepted a manifest without the tree's asset")
	}
	if _, _, err := LoadForkRelease(tree, dir, uid+1, pub); err == nil {
		t.Fatal("accepted files of another uid")
	}
	if _, _, err := LoadForkRelease(tree, "relative/dir", uid, pub); err == nil {
		t.Fatal("accepted a relative dir")
	}

	// The archive swapped after signing.
	if err := os.WriteFile(filepath.Join(dir, tree.Asset), append(archive, 0), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadForkRelease(tree, dir, uid, pub); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("accepted a changed archive: %v", err)
	}

	// A symlinked manifest is never followed.
	link := t.TempDir()
	writeRelease(t, dir, priv, "v1", tree.Asset, archive)
	for _, n := range []string{"manifest.json.sig", tree.Asset} {
		os.Symlink(filepath.Join(dir, n), filepath.Join(link, n))
	}
	os.Symlink(filepath.Join(dir, "manifest.json"), filepath.Join(link, "manifest.json"))
	if _, _, err := LoadForkRelease(tree, link, uid, pub); err == nil {
		t.Fatal("followed a symlinked manifest")
	}
}

func TestExtractTreeAsRoot(t *testing.T) {
	archive := makeTarGz(t, []tarEntry{
		{name: "./usr/", typ: tar.TypeDir, mode: 0o755},
		{name: "usr/bin/vibepollo", body: "elf", typ: tar.TypeReg, mode: 0o4777},
		{name: "usr/lib/libx.so.1", body: "so", typ: tar.TypeReg, mode: 0o644},
		{name: "usr/lib/libx.so", link: "libx.so.1", typ: tar.TypeSymlink},
		{name: "usr/lib/escape", link: "../../../etc/passwd", typ: tar.TypeSymlink},
		{name: "usr/lib/abs", link: "/etc/passwd", typ: tar.TypeSymlink},
		{name: "../outside", body: "x", typ: tar.TypeReg, mode: 0o644},
		{name: "usr/hard", link: "usr/bin/vibepollo", typ: tar.TypeLink},
	})
	dest := t.TempDir()
	if err := extractTreeAsRoot(archive, dest); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(filepath.Join(dest, "usr/bin/vibepollo"))
	if err != nil || st.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 || st.Mode().Perm()&0o022 != 0 {
		t.Fatalf("bin mode %v, %v", st.Mode(), err)
	}
	if l, err := os.Readlink(filepath.Join(dest, "usr/lib/libx.so")); err != nil || l != "libx.so.1" {
		t.Fatalf("soname link %q, %v", l, err)
	}
	for _, n := range []string{"usr/lib/escape", "usr/lib/abs", "usr/hard"} {
		if _, err := os.Lstat(filepath.Join(dest, n)); err == nil {
			t.Fatalf("%s extracted", n)
		}
	}
	if _, err := os.Lstat(filepath.Join(filepath.Dir(dest), "outside")); err == nil {
		t.Fatal("wrote outside dest")
	}

	// A symlinked directory from an earlier entry can't be written through.
	evil := makeTarGz(t, []tarEntry{
		{name: "usr/d", link: "lib", typ: tar.TypeSymlink},
		{name: "usr/lib/", typ: tar.TypeDir, mode: 0o755},
		{name: "usr/d/x", body: "x", typ: tar.TypeReg, mode: 0o644},
	})
	if err := extractTreeAsRoot(evil, t.TempDir()); err == nil {
		t.Fatal("wrote through a symlinked directory")
	}
}

func TestCompareReleaseVersions(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want int
	}{
		{"v2026.1104.1.usbridge", "v2026.1025.2.usbridge", 1},
		{"v2026.1025.2.usbridge", "v2026.1104.1.usbridge", -1},
		{"v2026.1104.1.usbridge", "v2026.1104.1.usbridge", 0},
		{"v2026.1104.10.usbridge", "v2026.1104.9.usbridge", 1},
	} {
		if got, ok := CompareReleaseVersions(c.a, c.b); !ok || got != c.want {
			t.Errorf("Compare(%q, %q) = %d, %v; want %d", c.a, c.b, got, ok, c.want)
		}
	}
	if _, ok := CompareReleaseVersions("dev", "v1"); ok {
		t.Error("compared a version without numbers")
	}
}

func TestForksPublicKeyMatchesAgentUpdateKey(t *testing.T) {
	_ = ForksPublicKey()
	b, err := os.ReadFile("../update/pubkey.go")
	if err != nil {
		t.Skip(err)
	}
	if !strings.Contains(string(b), `"`+ForksPublicKeyB64+`"`) {
		t.Fatal("ForksPublicKeyB64 differs from update.publicKeyB64 (the Streamers-Forks signing key)")
	}
}

func TestForkPolkitRuleScope(t *testing.T) {
	if !strings.Contains(ForkPolkitRule, `action.lookup("program") != "`+InstallPath+`"`) {
		t.Fatal("rule not pinned to the launcher's path")
	}
	if !strings.Contains(ForkPolkitRule, `--install-tree (sunshine|vibepollo) \/\S+$/`) {
		t.Fatal("rule allows more than --install-tree of the known trees")
	}
}
