package clipboard

import (
	"archive/tar"
	"bytes"
	"io"
	"log"
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

// FileSummary is a cheap (no file-content read) description of one
// clipboard file/directory entry — just a name, a best-effort size (0 for
// directories, where computing a real total would mean walking them), and
// whether it's a directory. Used only for the fast pre-announce below.
type FileSummary struct {
	Name  string
	Size  int64
	IsDir bool
}

// FileEnumerator is implemented by backends that can list what's currently
// on the clipboard for a file/directory selection without reading any file
// content — just paths and a cheap stat. Manager uses this, when available,
// to fire a fast "N files incoming" signal the instant a change is detected,
// well before the full Read() (which actually loads everything into memory,
// and can take a while for many/large files) finishes.
type FileEnumerator interface {
	EnumerateFiles() (files []FileSummary, ok bool)
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

// zeroReader is an infinite stream of zero bytes, used to pad a tar entry
// out to its declared size when the real source came up short mid-copy.
type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 0
	}
	return len(p), nil
}

// tarDir archives the directory at path into an uncompressed tar stream
// (Store, not Deflate — clipboard payloads are typically already-compressed
// formats or short-lived, so spending CPU on compression isn't worth it),
// preserving relative structure including empty subdirectories. Symlinks
// are skipped rather than followed, so a folder copy can't be tricked into
// pulling in an arbitrary target outside the copied tree.
//
// A single bad entry (locked Thumbs.db/desktop.ini, a permission error, a
// OneDrive/cloud-placeholder file that fails to open) is skipped rather than
// aborting the whole archive — real-world folders on Windows routinely
// contain at least one such file, and dropping the entire folder copy over
// one unreadable file inside it is worse than delivering everything else.
func tarDir(root string) ([]byte, error) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	walkErr := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			log.Printf("[clipboard] tarDir: skipping %s: %v", path, err)
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			log.Printf("[clipboard] tarDir: skipping %s: %v", path, err)
			return nil
		}
		if rel == "." {
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			log.Printf("[clipboard] tarDir: skipping %s: %v", rel, err)
			return nil
		}
		hdr.Name = filepath.ToSlash(rel)
		if info.IsDir() {
			hdr.Name += "/"
			if err := tw.WriteHeader(hdr); err != nil {
				log.Printf("[clipboard] tarDir: skipping dir %s: %v", rel, err)
			}
			return nil
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			log.Printf("[clipboard] tarDir: skipping %s: %v", rel, err)
			return nil
		}
		defer f.Close()
		if err := tw.WriteHeader(hdr); err != nil {
			log.Printf("[clipboard] tarDir: skipping %s: %v", rel, err)
			return nil
		}
		// A tar entry's size is fixed by the header written above, so a
		// short/failed read partway through can't just be logged and moved
		// past — the writer would desync and corrupt every entry after this
		// one. Pad out to the declared size instead, trading a truncated
		// (zero-filled) tail in this one file for a structurally valid
		// archive overall.
		n, copyErr := io.CopyN(tw, f, hdr.Size)
		if copyErr != nil && copyErr != io.EOF {
			log.Printf("[clipboard] tarDir: %s: partial read (%d/%d bytes): %v", rel, n, hdr.Size, copyErr)
		}
		if n < hdr.Size {
			if _, err := io.CopyN(tw, zeroReader{}, hdr.Size-n); err != nil {
				log.Printf("[clipboard] tarDir: %s: pad failed: %v", rel, err)
			}
		}
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
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
