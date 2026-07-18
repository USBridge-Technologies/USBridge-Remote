package clipboard

import (
	"archive/tar"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Backend is native system-clipboard access for one platform. Read/Write
// operate on whichever representation (text/image/file) is currently on the
// clipboard; ChangeStamp is polled frequently so it must be cheap — it
// should never perform a full content read.
type Backend interface {
	// ChangeStamp returns a token that changes whenever the system
	// clipboard's content changes. Implementations without a native
	// sequence counter may hash a cheap summary instead (e.g. a Linux
	// TARGETS listing) rather than reading the full payload.
	ChangeStamp() (string, error)

	// Read returns the current clipboard content. ok=false means the
	// clipboard holds nothing, or a format this backend doesn't understand.
	Read() (content Content, ok bool, err error)

	// Write sets the system clipboard to content.
	Write(content Content) error
}

// sanitizeFileName strips path separators from a peer-supplied file name so
// it can't escape the temp directory files are materialized into before
// being registered on the native clipboard.
func sanitizeFileName(name string) string {
	out := make([]rune, 0, len(name))
	for _, r := range name {
		switch r {
		case '/', '\\', 0:
			out = append(out, '_')
		default:
			out = append(out, r)
		}
	}
	if len(out) == 0 {
		return "file"
	}
	return string(out)
}

// tarDir archives the directory at path into an uncompressed tar stream
// (Store, not Deflate — clipboard payloads are typically already-compressed
// formats or short-lived, so spending CPU on compression isn't worth it),
// preserving relative structure including empty subdirectories. Symlinks
// are skipped rather than followed, so a folder copy can't be tricked into
// pulling in an arbitrary target outside the copied tree.
func tarDir(root string) ([]byte, error) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = filepath.ToSlash(rel)
		if info.IsDir() {
			hdr.Name += "/"
			return tw.WriteHeader(hdr)
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(tw, f)
		return err
	})
	if err != nil {
		return nil, err
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// untarDir extracts a tar archive produced by tarDir into destDir (created
// if missing). Entries that would escape destDir (an absolute path, or a
// "../" component — from a corrupt or hostile peer) are skipped rather than
// followed, and only regular files/directories are materialized.
func untarDir(data []byte, destDir string) error {
	if err := os.MkdirAll(destDir, 0o700); err != nil {
		return err
	}
	tr := tar.NewReader(bytes.NewReader(data))
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		name := filepath.Clean(hdr.Name)
		if name == "." || name == ".." || strings.HasPrefix(name, ".."+string(filepath.Separator)) || filepath.IsAbs(name) {
			continue
		}
		target := filepath.Join(destDir, name)
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o700); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
				return err
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return err
			}
			f.Close()
		}
	}
}
