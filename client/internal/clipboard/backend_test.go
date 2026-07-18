package clipboard

import (
	"archive/tar"
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// testTarWriter builds tar fixtures directly (bypassing tarDir's own path
// sanitization) so tests can construct entries a real archive would never
// contain, like a "../" traversal, to prove untarDir rejects them too.
type testTarWriter struct{ tw *tar.Writer }

func newTestTarWriter(buf *bytes.Buffer) *testTarWriter {
	return &testTarWriter{tw: tar.NewWriter(buf)}
}

func (w *testTarWriter) writeFile(name string, data []byte) {
	_ = w.tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(data)), Typeflag: tar.TypeReg})
	_, _ = w.tw.Write(data)
}

func (w *testTarWriter) close() { _ = w.tw.Close() }

func TestTarUntarDirRoundTrip(t *testing.T) {
	src := t.TempDir()
	mustWrite := func(rel string, data []byte) {
		p := filepath.Join(src, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(p, data, 0o600); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	mustWrite("a.txt", []byte("hello"))
	mustWrite("sub/b.txt", []byte("world"))
	if err := os.MkdirAll(filepath.Join(src, "empty"), 0o700); err != nil {
		t.Fatalf("mkdir empty: %v", err)
	}

	archive, err := tarDir(src)
	if err != nil {
		t.Fatalf("tarDir: %v", err)
	}

	dest := filepath.Join(t.TempDir(), "restored")
	if err := untarDir(archive, dest); err != nil {
		t.Fatalf("untarDir: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dest, "a.txt"))
	if err != nil || !bytes.Equal(got, []byte("hello")) {
		t.Fatalf("a.txt mismatch: data=%q err=%v", got, err)
	}
	got, err = os.ReadFile(filepath.Join(dest, "sub", "b.txt"))
	if err != nil || !bytes.Equal(got, []byte("world")) {
		t.Fatalf("sub/b.txt mismatch: data=%q err=%v", got, err)
	}
	if info, err := os.Stat(filepath.Join(dest, "empty")); err != nil || !info.IsDir() {
		t.Fatalf("empty dir not restored: %v", err)
	}
}

func TestUntarDirRejectsPathTraversal(t *testing.T) {
	// A hand-built tar with a "../escape" entry should never write outside
	// destDir, even from a corrupt or hostile peer.
	var buf bytes.Buffer
	tw := newTestTarWriter(&buf)
	tw.writeFile("../escape.txt", []byte("pwned"))
	tw.close()

	dest := filepath.Join(t.TempDir(), "restored")
	if err := untarDir(buf.Bytes(), dest); err != nil {
		t.Fatalf("untarDir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dest), "escape.txt")); err == nil {
		t.Fatalf("path traversal entry escaped destDir")
	}
}

func TestEncodeDecodeFilesPreservesIsDir(t *testing.T) {
	files := []FileItem{
		{Name: "file.txt", Data: []byte("plain"), IsDir: false},
		{Name: "folder", Data: []byte("tar-bytes"), IsDir: true},
	}
	decoded, err := DecodeFiles(EncodeFiles(files))
	if err != nil {
		t.Fatalf("DecodeFiles: %v", err)
	}
	if len(decoded) != 2 {
		t.Fatalf("expected 2 files, got %d", len(decoded))
	}
	if decoded[0].IsDir || decoded[0].Name != "file.txt" || string(decoded[0].Data) != "plain" {
		t.Fatalf("unexpected decoded[0]: %+v", decoded[0])
	}
	if !decoded[1].IsDir || decoded[1].Name != "folder" || string(decoded[1].Data) != "tar-bytes" {
		t.Fatalf("unexpected decoded[1]: %+v", decoded[1])
	}
}
